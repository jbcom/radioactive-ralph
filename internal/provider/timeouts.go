package provider

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// ErrProviderStalled is returned when a provider produces no observable
// progress before its renewable stall lease expires.
var ErrProviderStalled = errors.New("provider: progress stalled")

const (
	// DefaultTurnTimeout is deliberately much longer than the progress lease:
	// a productive provider may run for a substantial bounded turn.
	DefaultTurnTimeout = 30 * time.Minute
	// MaxTurnTimeout and MaxStallTimeout are safety ceilings. Configuration can
	// lengthen normal work, but cannot create an unbounded provider process.
	MaxTurnTimeout = 24 * time.Hour
	// MaxStallTimeout is the hard ceiling for a renewable no-progress lease.
	MaxStallTimeout = time.Hour
)

// TurnLimits are the two independent runtime bounds for a provider turn.
type TurnLimits struct {
	TurnTimeout  time.Duration
	StallTimeout time.Duration
}

// ResolveTurnLimits resolves request overrides over binding configuration
// over safe defaults. Every result is positive and bounded.
func ResolveTurnLimits(binding Binding, req Request) (TurnLimits, error) {
	turn, err := parseBoundedTimeout(binding.Name, "turn_timeout", binding.Config.TurnTimeout, DefaultTurnTimeout, MaxTurnTimeout)
	if err != nil {
		return TurnLimits{}, err
	}
	stall, err := parseBoundedTimeout(binding.Name, "stall_timeout", binding.Config.StallTimeout, DefaultStallTimeout, MaxStallTimeout)
	if err != nil {
		return TurnLimits{}, err
	}
	if req.TurnTimeout != 0 {
		if err := validateBoundedTimeout("request turn timeout", req.TurnTimeout, MaxTurnTimeout); err != nil {
			return TurnLimits{}, err
		}
		turn = req.TurnTimeout
	}
	if req.StallTimeout != 0 {
		if err := validateBoundedTimeout("request stall timeout", req.StallTimeout, MaxStallTimeout); err != nil {
			return TurnLimits{}, err
		}
		stall = req.StallTimeout
	}
	return TurnLimits{TurnTimeout: turn, StallTimeout: stall}, nil
}

// ValidateConfiguredTimeout checks a configured duration string against the same
// bounds ResolveTurnLimits enforces, so callers that assemble a Binding can fail
// at configuration-resolution time instead of at dispatch.
//
// This matters because dispatch admission claims the task BEFORE the runner
// resolves limits: a value like "banana" or "25h" would otherwise let the
// orchestrator claim the task, launch its goroutine, fail in the runner, and
// leave the task running until stale reclamation — which then repeats the same
// cycle forever without progressing. An empty string means "use the default"
// and is valid.
//
// field must be "turn_timeout" or "stall_timeout"; any other value is rejected
// so a caller cannot silently validate against the wrong ceiling.
func ValidateConfiguredTimeout(field, raw string) error {
	if raw == "" {
		return nil
	}
	var maxBound time.Duration
	switch field {
	case "turn_timeout":
		maxBound = MaxTurnTimeout
	case "stall_timeout":
		maxBound = MaxStallTimeout
	default:
		return fmt.Errorf("provider: unknown timeout field %q", field)
	}
	value, err := time.ParseDuration(raw)
	if err != nil {
		return fmt.Errorf("invalid %s %q: %w", field, raw, err)
	}
	return validateBoundedTimeout(field, value, maxBound)
}

func parseBoundedTimeout(providerName, field, raw string, fallback, maxBound time.Duration) (time.Duration, error) {
	if raw == "" {
		if err := validateBoundedTimeout(field, fallback, maxBound); err != nil {
			return 0, fmt.Errorf("provider %q: invalid default: %w", providerName, err)
		}
		return fallback, nil
	}
	value, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("provider %q: invalid %s %q: %w", providerName, field, raw, err)
	}
	if err := validateBoundedTimeout(field, value, maxBound); err != nil {
		return 0, fmt.Errorf("provider %q: %w", providerName, err)
	}
	return value, nil
}

func validateBoundedTimeout(name string, value, maxBound time.Duration) error {
	if value <= 0 {
		return fmt.Errorf("%s must be positive, got %s", name, value)
	}
	if value > maxBound {
		return fmt.Errorf("%s must not exceed %s, got %s", name, maxBound, value)
	}
	return nil
}

// WithTurnDeadline creates the absolute turn deadline. The returned cause is
// stable and privacy-safe; raw provider output is never incorporated.
func WithTurnDeadline(parent context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeoutCause(parent, timeout, context.DeadlineExceeded)
}
