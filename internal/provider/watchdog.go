package provider

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"time"

	"github.com/jbcom/radioactive-ralph/internal/agent"
)

// ErrAgentBlocked is returned by superviseAgent (and wrapped with the
// triggering reason) when the control invariant fires: the agent produced a
// signal (an interactive prompt, a stall, or a resource-exceeded condition)
// that means it can no longer be trusted to make forward progress
// non-interactively. superviseAgent ALWAYS kills the agent before returning
// this error — callers must never wait on it themselves.
var ErrAgentBlocked = errors.New("provider: agent blocked (killed by watchdog)")

// DefaultStallTimeout is the default ceiling on how long superviseAgent will
// wait for output from an agent before treating it as stalled and killing
// it. Individual callers may override via WatchdogConfig.StallTimeout. It is
// a var (not a const) solely so tests can shrink it to keep watchdog tests
// fast without threading a StallTimeout override through every runner call
// site; production code should never reassign it outside of tests.
var DefaultStallTimeout = 3 * time.Minute

// DefaultPromptPatterns are the regexes superviseAgent uses out of the box
// to recognize an interactive permission/clarification prompt in a CLI's
// output — the shapes seen from Claude Code, Codex, opencode, and generic
// POSIX-confirmation prompts ("(y/n)", "[Y/n]", etc.). Callers with a
// provider-specific prompt shape should extend, not replace, this list.
var DefaultPromptPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)\(y/n\)`),
	regexp.MustCompile(`(?i)\[y/n\]`),
	regexp.MustCompile(`(?i)continue\?`),
	regexp.MustCompile(`(?i)proceed\?`),
	regexp.MustCompile(`(?i)permission`),
	regexp.MustCompile(`(?i)approve`),
	regexp.MustCompile(`(?i)allow this`),
	regexp.MustCompile(`(?i)do you want to`),
	regexp.MustCompile(`(?i)waiting for`),
	regexp.MustCompile(`(?i)press enter`),
}

// DefaultWatchdogConfig returns a WatchdogConfig seeded with
// DefaultStallTimeout and DefaultPromptPatterns. Runners call this (rather
// than constructing agent.WatchdogConfig{} directly) so every provider gets
// the same baseline prompt/stall detection unless a caller has a reason to
// override it. Use this ONLY for providers whose output is free-form pane
// text where a raw interactive prompt could actually appear (see
// StreamJSONWatchdogConfig for the structured-output case).
func DefaultWatchdogConfig() agent.WatchdogConfig {
	return agent.WatchdogConfig{
		StallTimeout:   DefaultStallTimeout,
		PromptPatterns: DefaultPromptPatterns,
	}
}

// StreamJSONWatchdogConfig is the watchdog config for providers driven in a
// structured stream-json mode (claude/opencode: `--output-format
// stream-json`). Their normal output is JSON frames whose text can innocently
// contain prompt-like words ("permission", "continue?"), which content-blind
// matching would misread and KILL a valid turn. It keeps the prompt patterns
// but sets SkipPromptMatchOnJSONLines: patterns are matched ONLY on lines
// that are NOT valid JSON, so a legitimate JSON frame is never a false prompt
// while a GENUINE raw interactive prompt (never valid JSON) is still caught
// immediately — not merely by the slower stall timeout.
func StreamJSONWatchdogConfig() agent.WatchdogConfig {
	return agent.WatchdogConfig{
		StallTimeout:               DefaultStallTimeout,
		PromptPatterns:             DefaultPromptPatterns,
		SkipPromptMatchOnJSONLines: true,
	}
}

// superviseAgent is the shared enforcement point for the control invariant
// (spec §1: an agent CLI must NEVER block the system). It consumes
// a.Output() AND concurrently runs agent.Watch(ctx, a, cfg): every real
// output line is handed to onLine (so the caller's own result-framing/JSON
// parsing keeps working exactly as before), while agent.Watch classifies
// each line and watches for a stall.
//
// The moment agent.Watch emits Prompt, Stall, or ResourceExceeded,
// superviseAgent immediately calls a.Kill() and returns an error wrapping
// ErrAgentBlocked with the triggering detail — it NEVER waits for the
// agent to finish on its own once one of those signals fires. This is the
// enforcement the orchestrator's ctx-timeout wrapper (dispatchWorker) could
// not provide on its own: that timeout only bounds total wall-clock time,
// it cannot detect an interactive prompt and kill early, nor tell a
// stalled-but-not-yet-timed-out CLI apart from one still working.
//
// onLine returns true to tell superviseAgent the caller is done (e.g. it
// just parsed the CLI's own terminal result frame and has no reason to keep
// reading further pane output): superviseAgent then kills the agent — the
// turn is already complete, so there's no reason to keep the process or the
// watchdog goroutine running — and returns nil. Passing a nil onLine, or
// one that never returns true, makes superviseAgent run until a.Output()
// closes naturally (the agent exited on its own).
//
// superviseAgent returns nil when a.Output() closes normally (the agent
// exited on its own) before any blocking signal fires, or once onLine
// signals it is done. It returns ctx.Err() if ctx is canceled first (also
// killing the agent, so a caller-side timeout/cancel still results in a
// dead process rather than an orphan).
func superviseAgent(ctx context.Context, a *agent.Agent, cfg agent.WatchdogConfig, onLine func([]byte) (done bool)) error {
	if cfg.StallTimeout <= 0 {
		cfg.StallTimeout = DefaultStallTimeout
	}
	// NOTE: no nil->DefaultPromptPatterns fallback here. Callers pass a
	// deliberate config (DefaultWatchdogConfig for pane-text providers,
	// StreamJSONWatchdogConfig for structured-output ones), and a nil pattern
	// set legitimately means "match nothing" — silently substituting the
	// defaults would re-introduce the false-kill-on-JSON-content bug for
	// stream-json providers.

	sigs := agent.Watch(ctx, a, cfg)
	for {
		select {
		case sig, ok := <-sigs:
			if !ok {
				// agent.Watch's channel closes only after it observes
				// a.Output() close (Exited) or ctx cancellation; either way
				// there is nothing left to supervise.
				return nil
			}
			switch sig.Kind {
			case agent.Prompt, agent.Stall, agent.ResourceExceeded:
				_ = a.Kill()
				return fmt.Errorf("%w: %s", ErrAgentBlocked, blockedReason(sig))
			case agent.Progress:
				if onLine != nil && len(sig.Detail) > 0 {
					if onLine([]byte(sig.Detail)) {
						_ = a.Kill()
						return nil
					}
				}
			case agent.Exited:
				return nil
			}
		case <-ctx.Done():
			_ = a.Kill()
			return ctx.Err()
		}
	}
}

// blockedReason renders a human-readable reason for the ErrAgentBlocked
// wrap based on which signal triggered the kill.
func blockedReason(sig agent.Signal) string {
	switch sig.Kind {
	case agent.Prompt:
		return fmt.Sprintf("interactive prompt detected: %q", sig.Detail)
	case agent.Stall:
		return "no output before stall timeout"
	case agent.ResourceExceeded:
		return "resource limit exceeded"
	default:
		return "unknown blocking signal"
	}
}
