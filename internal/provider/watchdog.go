package provider

import (
	"context"
	"errors"
	"regexp"
	"time"

	"github.com/jbcom/radioactive-ralph/internal/agent"
)

// ErrAgentBlocked is returned by superviseAgent (and wrapped with the
// triggering reason) when the control invariant fires: the agent produced a
// signal (an interactive prompt or a stall) that means it can no longer be
// trusted to make forward progress non-interactively. superviseAgent ALWAYS
// kills the agent before returning this error — callers must never wait on it
// themselves. Prompt failures use a static reason and never interpolate the
// observed terminal line, which may contain prompts or credentials.
var ErrAgentBlocked = errors.New("provider: agent blocked (killed by watchdog)")

// BlockReason is the provider-output-free reason a watchdog blocked a turn.
type BlockReason string

const (
	// BlockReasonPrompt means the provider requested interactive input.
	BlockReasonPrompt BlockReason = "interactive_prompt"
	// BlockReasonStall means the provider stopped producing progress.
	BlockReasonStall BlockReason = "stall"
)

// BlockedError retains a typed, provider-output-free reason while preserving
// errors.Is(err, ErrAgentBlocked) compatibility.
type BlockedError struct {
	Reason BlockReason
	// Kind is the closed-taxonomy kind of an interactive prompt, empty for any
	// other reason. It answers "what was it asking for" without carrying the
	// prompt text, which the content-safety boundary keeps off operator
	// surfaces.
	Kind PromptKind
}

func (e *BlockedError) Error() string {
	switch e.Reason {
	case BlockReasonPrompt:
		return ErrAgentBlocked.Error() + ": interactive prompt detected"
	case BlockReasonStall:
		return ErrAgentBlocked.Error() + ": no output before stall timeout"
	default:
		return ErrAgentBlocked.Error()
	}
}

func (e *BlockedError) Unwrap() error { return ErrAgentBlocked }

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
	// An OPEN QUESTION about the task. Without this the detector never fires on
	// "Which database should I target?", so the clarification KIND was
	// classifiable but unreachable -- a taxonomy branch nothing could produce.
	//
	// Anchored to a question word at line start and requiring a "?", so it
	// matches a question ASKED rather than any sentence mentioning one.
	regexp.MustCompile(`(?im)^\s*(which|what|where|how|who|should i)\b[^?]*\?`),
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
		ClassifyPrompt: classifyPromptLine,
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
		ClassifyPrompt:             classifyPromptLine,
		SkipPromptMatchOnJSONLines: true,
	}
}

func streamJSONWatchdogConfigWithStall(stall time.Duration) agent.WatchdogConfig {
	cfg := StreamJSONWatchdogConfig()
	cfg.StallTimeout = stall
	return cfg
}

// superviseAgent is the shared enforcement point for the control invariant
// (spec §1: an agent CLI must NEVER block the system). It consumes
// a.Output() AND concurrently runs agent.Watch(ctx, a, cfg): every real
// output line is handed to onLine (so the caller's own result-framing/JSON
// parsing keeps working exactly as before), while agent.Watch classifies
// each line and watches for a stall.
//
// The moment agent.Watch emits Prompt or Stall, superviseAgent synchronously
// terminates, reaps, and joins the Agent before returning an error wrapping
// ErrAgentBlocked with a fixed reason. This is the
// enforcement the orchestrator's ctx-timeout wrapper (dispatchWorker) could
// not provide on its own: that timeout only bounds total wall-clock time,
// it cannot detect an interactive prompt and kill early, nor tell a
// stalled-but-not-yet-timed-out CLI apart from one still working.
//
// onLine returns true to tell superviseAgent the caller parsed a terminal
// result frame. That is only a candidate success: superviseAgent terminates and
// joins the Agent, and returns nil only if reclamation succeeded. Passing a nil onLine, or
// one that never returns true, makes superviseAgent run until a.Output()
// closes naturally (the agent exited on its own).
//
// superviseAgent returns nil when a.Output() closes normally (the agent
// exited on its own) before any blocking signal fires, or once onLine
// signals it is done. It returns ctx.Err() if ctx is canceled first (also
// killing the agent, so a caller-side timeout/cancel still results in a
// dead process rather than an orphan).
type agentConvergence struct {
	terminateAndWait func(*agent.Agent) error
	wait             func(*agent.Agent) error
}

func defaultAgentConvergence() agentConvergence {
	return agentConvergence{
		terminateAndWait: func(a *agent.Agent) error { return a.TerminateAndWait() },
		wait:             func(a *agent.Agent) error { return a.Wait() },
	}
}

func superviseAgent(ctx context.Context, a *agent.Agent, cfg agent.WatchdogConfig, onLine func([]byte) (done bool)) error {
	return superviseAgentWithConvergence(ctx, a, cfg, onLine, defaultAgentConvergence())
}

func superviseAgentWithDiscarded(
	ctx context.Context,
	a *agent.Agent,
	cfg agent.WatchdogConfig,
	onLine func([]byte) (done bool),
	onDiscarded func([]byte) (done bool),
) error {
	return superviseAgentWithCallbacks(
		ctx,
		a,
		cfg,
		onLine,
		onDiscarded,
		defaultAgentConvergence(),
	)
}

func superviseAgentWithConvergence(
	ctx context.Context,
	a *agent.Agent,
	cfg agent.WatchdogConfig,
	onLine func([]byte) (done bool),
	convergence agentConvergence,
) error {
	return superviseAgentWithCallbacks(ctx, a, cfg, onLine, nil, convergence)
}

func superviseAgentWithCallbacks(
	ctx context.Context,
	a *agent.Agent,
	cfg agent.WatchdogConfig,
	onLine func([]byte) (done bool),
	onDiscarded func([]byte) (done bool),
	convergence agentConvergence,
) error {
	if cfg.StallTimeout <= 0 {
		cfg.StallTimeout = DefaultStallTimeout
	}
	// NOTE: no nil->DefaultPromptPatterns fallback here. Callers pass a
	// deliberate config (DefaultWatchdogConfig for pane-text providers,
	// StreamJSONWatchdogConfig for structured-output ones), and a nil pattern
	// set legitimately means "match nothing" — silently substituting the
	// defaults would re-introduce the false-kill-on-JSON-content bug for
	// stream-json providers.

	// Run Watch under a child context we cancel on EVERY return. Its signal
	// channel is deliberately unbuffered to avoid a second retained-output
	// queue, so cancellation is also the release path if we return while Watch
	// is waiting for downstream admission.
	watchCtx, cancelWatch := context.WithCancel(ctx)
	defer cancelWatch()

	sigs := agent.Watch(watchCtx, a, cfg)
	terminate := func(primary error) error {
		convergenceErr := convergence.terminateAndWait(a)
		// Cancellation can race the terminal frame or occur inside a slow
		// convergence implementation. Recheck only after the process has
		// converged so neither race can be laundered into success.
		contextErr := ctx.Err()
		if contextErr != nil && errors.Is(primary, contextErr) {
			contextErr = nil
		}
		return errors.Join(primary, convergenceErr, contextErr)
	}
	wait := func(primary error) error {
		convergenceErr := convergence.wait(a)
		contextErr := ctx.Err()
		if contextErr != nil && errors.Is(primary, contextErr) {
			contextErr = nil
		}
		return errors.Join(primary, convergenceErr, contextErr)
	}
	for {
		select {
		case sig, ok := <-sigs:
			// watchCtx is a child of ctx, so caller cancellation may close sigs
			// in the same scheduler turn as this receive. Preserve the caller's
			// context error instead of misreading that close as a clean agent
			// exit and letting a runner consume a missing/partial result file.
			if ctxErr := ctx.Err(); ctxErr != nil {
				return terminate(ctxErr)
			}
			if !ok {
				// agent.Watch's channel closes only after it observes
				// a.Output() close (Exited) or ctx cancellation; either way
				// there is nothing left to supervise.
				return wait(nil)
			}
			switch sig.Kind {
			case agent.Prompt, agent.Stall:
				return terminate(&BlockedError{Reason: blockReason(sig), Kind: PromptKind(sig.PromptKind)})
			case agent.Progress:
				callback := onLine
				if sig.Discarded {
					callback = onDiscarded
				}
				if callback != nil && len(sig.Line) > 0 {
					done := callback(sig.Line)
					// The callback is application code and may take long enough
					// for cancellation to win, or cancel ctx itself. Check before
					// accepting its terminal-frame decision.
					if ctxErr := ctx.Err(); ctxErr != nil {
						return terminate(ctxErr)
					}
					if done {
						return terminate(nil)
					}
				}
			case agent.Exited:
				return wait(nil)
			}
		case <-ctx.Done():
			return terminate(ctx.Err())
		}
	}
}

// blockedReason renders a human-readable reason for the ErrAgentBlocked
// wrap based on which signal triggered the kill.
func blockReason(sig agent.Signal) BlockReason {
	switch sig.Kind {
	case agent.Prompt:
		return BlockReasonPrompt
	case agent.Stall:
		return BlockReasonStall
	default:
		return ""
	}
}
