package agent

import (
	"context"
	"encoding/json"
	"regexp"
	"time"
)

// SignalKind classifies a watchdog observation.
type SignalKind int

// The recognized SignalKind values.
const (
	Progress SignalKind = iota
	Stall
	Prompt
	Exited
	ResourceExceeded
)

// Signal is one watchdog observation about an agent.
type Signal struct {
	Kind   SignalKind
	Detail string
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
	out := make(chan Signal, 16)
	go func() {
		defer close(out)
		timer := time.NewTimer(cfg.StallTimeout)
		defer timer.Stop()
		emit := func(s Signal) {
			select {
			case out <- s:
			case <-ctx.Done():
			}
		}
		for {
			if cfg.StallTimeout > 0 {
				timer.Reset(cfg.StallTimeout)
			}
			select {
			case <-ctx.Done():
				return
			case line, ok := <-a.Output():
				if !ok {
					emit(Signal{Kind: Exited})
					return
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
						emit(Signal{Kind: Prompt, Detail: string(line)})
						matched = true
						break
					}
				}
				if !matched {
					emit(Signal{Kind: Progress, Detail: string(line)})
				}
			case <-timer.C:
				emit(Signal{Kind: Stall})
			}
		}
	}()
	return out
}
