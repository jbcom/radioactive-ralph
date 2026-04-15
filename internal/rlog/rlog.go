// Package rlog is a thin slog wrapper that emits records shaped like
// Claude's stream-json events when the caller opts into JSON output.
// Default (text) output is operator-facing human logging on stderr.
//
// The shape is intentionally aligned with claude's stream-json so
// operator-facing and subprocess-facing log streams can be
// multiplexed through the same tooling:
//
//	{"type":"ralph","event":"init.start","ts":"2026-04-15T...","repo":"..."}
//
// `type: ralph` disambiguates ralph-emitted records from
// claude-emitted `type: assistant` / `type: user` records when an
// operator tails a merged log stream.
package rlog

import (
	"context"
	"io"
	"log/slog"
	"os"
)

// Mode controls the output format selected by the CLI.
type Mode string

const (
	// ModeText is the default human-readable stderr output.
	ModeText Mode = "text"
	// ModeJSON emits one JSON record per log call, shaped like
	// claude stream-json events with type=ralph.
	ModeJSON Mode = "json"
)

// New returns a *slog.Logger configured for the requested mode.
// Writer defaults to os.Stderr when nil.
func New(mode Mode, w io.Writer) *slog.Logger {
	if w == nil {
		w = os.Stderr
	}
	switch mode {
	case ModeJSON:
		h := slog.NewJSONHandler(w, &slog.HandlerOptions{
			Level:       slog.LevelInfo,
			ReplaceAttr: replaceForStreamJSON,
		})
		return slog.New(h).With(slog.String("type", "ralph"))
	default:
		h := slog.NewTextHandler(w, &slog.HandlerOptions{
			Level: slog.LevelInfo,
		})
		return slog.New(h)
	}
}

// replaceForStreamJSON renames slog's default keys to match the
// stream-json convention used by claude (ts, msg → event).
func replaceForStreamJSON(_ []string, a slog.Attr) slog.Attr {
	switch a.Key {
	case slog.TimeKey:
		a.Key = "ts"
	case slog.MessageKey:
		a.Key = "event"
	case slog.LevelKey:
		// drop — stream-json has no level field; severity is implied
		// by event type.
		return slog.Attr{}
	}
	return a
}

// ctxKey is the private type for rlog's context key.
type ctxKey struct{}

// WithLogger attaches a logger to ctx for downstream callers.
func WithLogger(ctx context.Context, logger *slog.Logger) context.Context {
	return context.WithValue(ctx, ctxKey{}, logger)
}

// FromContext retrieves the logger attached by WithLogger, or returns
// the default slog logger if none. Callers can use this without
// threading a *slog.Logger through every function signature.
func FromContext(ctx context.Context) *slog.Logger {
	if l, ok := ctx.Value(ctxKey{}).(*slog.Logger); ok && l != nil {
		return l
	}
	return slog.Default()
}
