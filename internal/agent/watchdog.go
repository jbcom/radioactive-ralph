package agent

import (
	"context"
	"encoding/json"
	"regexp"
	"time"
)

// SignalKind classifies a watchdog observation.
type SignalKind int

// The recognized SignalKind values. (There is deliberately no
// resource-exceeded signal: Watch does no RSS/CPU sampling, so it could never
// emit one — a runaway agent is bounded by the stall timeout, not a resource
// ceiling. A real resource limiter would be its own feature.)
const (
	Progress SignalKind = iota
	Stall
	Prompt
	Exited
)

// Signal is one watchdog observation about an agent.
type Signal struct {
	Kind SignalKind
	// Line is present only for Progress. It shares the transport's retained
	// byte slice; Watch neither converts nor copies arbitrary provider output.
	// Prompt, Stall, and Exited signals are content-free.
	Line []byte
	// Discarded marks Line as a bounded prefix of a record drained under
	// DiscardOversizeOutput. It is for provider framing only and was not
	// retained as ordinary pane output.
	Discarded bool
}

// WatchdogConfig tunes stall and prompt detection.
type WatchdogConfig struct {
	StallTimeout   time.Duration
	PromptPatterns []*regexp.Regexp

	// SkipPromptMatchOnJSONLines, when true, suppresses prompt-pattern
	// matching for any output line that is a valid JSON value. Stream-json
	// providers (claude/opencode) emit structured frames whose text can
	// innocently contain prompt-like words ("permission", "continue?"),
	// which would false-match and kill a valid turn — but a GENUINE raw
	// interactive prompt from such a CLI is NOT valid JSON, so it still gets
	// matched. This keeps real prompt detection while eliminating the
	// false-kill-on-structured-content bug.
	SkipPromptMatchOnJSONLines bool
}

// Watch observes an agent and emits Signals. It NEVER blocks waiting on the
// agent: a prompt pattern or a stall is surfaced immediately so the caller
// can kill-and-reclaim. The channel closes when the agent exits.
func Watch(ctx context.Context, a *Agent, cfg WatchdogConfig) <-chan Signal {
	// Unbuffered admission prevents a second queue of retained output. Together
	// with Agent.Output's unbuffered channel, this keeps the complete
	// reader-to-supervisor pipeline inside Options.MaxOutputRetentionBytes.
	out := make(chan Signal)
	go func() {
		defer close(out)
		// A non-positive StallTimeout means "no stall detection". Leave the timer
		// nil so its receive in the select below blocks forever (a nil channel is
		// never ready) rather than creating a timer that fires immediately and
		// emits a spurious Stall on the very first iteration.
		var timerC <-chan time.Time
		var timer *time.Timer
		lastActivity := time.Now()
		if cfg.StallTimeout > 0 {
			timer = time.NewTimer(cfg.StallTimeout)
			timerC = timer.C
			defer timer.Stop()
		}
		observeActivity := func(observedAt time.Time) {
			if observedAt.After(lastActivity) {
				lastActivity = observedAt
			}
		}
		resetTimerToDeadline := func() {
			if timer == nil {
				return
			}
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			remaining := time.Until(lastActivity.Add(cfg.StallTimeout))
			if remaining < 0 {
				remaining = 0
			}
			timer.Reset(remaining)
		}
		emit := func(s Signal) {
			select {
			case out <- s:
			case <-ctx.Done():
			}
		}
		handleLine := func(line []byte, ok bool) (exited bool) {
			if !ok {
				emit(Signal{Kind: Exited})
				return true
			}
			matched := false
			// Skip prompt-matching on structured JSON frames when
			// configured — a raw interactive prompt is never valid JSON,
			// so this preserves real detection while not misreading
			// prompt-like WORDS inside a legitimate JSON frame as a prompt.
			skipPromptMatch := cfg.SkipPromptMatchOnJSONLines && json.Valid(line)
			for _, re := range cfg.PromptPatterns {
				if skipPromptMatch {
					break
				}
				if re.Match(line) {
					emit(Signal{Kind: Prompt})
					matched = true
					break
				}
			}
			if !matched {
				emit(Signal{Kind: Progress, Line: line})
			}
			return false
		}
		discardedOutput := a.DiscardedOutput()
		handleDiscarded := func(prefix []byte, ok bool) {
			if !ok {
				discardedOutput = nil
				return
			}
			emit(Signal{Kind: Progress, Line: prefix, Discarded: true})
		}
		for {
			resetTimerToDeadline()
			select {
			case <-ctx.Done():
				return
			case observedAt := <-a.Activity():
				// Content-free progress from an underlying pty read while
				// readLoop drains a partial or discarded line. Returning to the
				// loop advances the deadline from READ time, not consumption
				// time, without exposing partial content.
				observeActivity(observedAt)
				continue
			case prefix, ok := <-discardedOutput:
				handleDiscarded(prefix, ok)
				continue
			case line, ok := <-a.Output():
				if handleLine(line, ok) {
					return
				}
			case <-timerC:
				// Drain the coalesced read timestamp before judging the
				// deadline. A stale timestamp remains stale: downstream
				// backpressure cannot grant it a fresh full timeout.
				for {
					select {
					case observedAt := <-a.Activity():
						observeActivity(observedAt)
					default:
						goto activityDrained
					}
				}
			activityDrained:
				if time.Now().Before(lastActivity.Add(cfg.StallTimeout)) {
					continue
				}
				// A deadline and newly available output can become ready in the
				// same scheduler turn. Give the ready line/exit one admission
				// before declaring the stall; its queued read timestamp still
				// governs whether the next deadline is already expired.
				select {
				case <-ctx.Done():
					return
				case prefix, ok := <-discardedOutput:
					handleDiscarded(prefix, ok)
					continue
				case line, ok := <-a.Output():
					if handleLine(line, ok) {
						return
					}
					continue
				default:
				}
				// Stall is TERMINAL: the consumer (superviseAgent) kills the agent
				// and stops reading on the first Stall, so continuing the loop would
				// only block the next emit() on an abandoned channel until ctx is
				// cancelled — a per-turn goroutine leak on every stall-kill. Return.
				// timerC is nil when StallTimeout<=0, so this case never fires then.
				emit(Signal{Kind: Stall})
				return
			}
		}
	}()
	return out
}
