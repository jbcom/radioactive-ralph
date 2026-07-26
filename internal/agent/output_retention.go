package agent

import (
	"errors"
	"fmt"
)

// OversizeOutputPolicy controls what happens when one newline-delimited output
// record exceeds its share of the aggregate retention budget.
type OversizeOutputPolicy uint8

const (
	// RejectOversizeOutput terminates the agent with ErrOutputLineTooLong.
	// It is the safe default for providers whose result itself is line framed.
	RejectOversizeOutput OversizeOutputPolicy = iota

	// DiscardOversizeOutput continues draining the record without retaining or
	// emitting it as ordinary Output. A separately bounded classifier prefix is
	// exposed through DiscardedOutput. Use only for observational streams with a
	// separate authoritative result channel.
	DiscardOversizeOutput
)

// Output-stream errors are static so terminal contents never leak through the
// control path.
var (
	ErrOutputLineTooLong          = errors.New("agent: output line exceeded retention limit")
	ErrObservedOutputTooLarge     = errors.New("agent: observed output exceeded cumulative limit")
	ErrOutputRead                 = errors.New("agent: output stream read failed")
	ErrInvalidOutputRetention     = errors.New("agent: invalid output retention budget")
	ErrInvalidObservedOutputLimit = errors.New("agent: invalid observed output limit")
	ErrInvalidOversizePolicy      = errors.New("agent: invalid oversize output policy")
)

const (
	defaultRetainedLineBytes = 1 << 20
	maximumRetainedLineBytes = 8 << 20

	// DefaultMaxOutputRetentionBytes is the single aggregate bound for provider
	// bytes retained by the fixed read buffer, callback-owned line, line awaiting
	// Watch admission, current line, bounded slice-growth overlap, and three
	// concurrently pipeline-owned discarded prefixes. It yields the historical
	// 1 MiB retained-line threshold.
	DefaultMaxOutputRetentionBytes = outputReadBufferBytes +
		discardedPrefixRetentionSlots*maxDiscardedOutputPrefixBytes +
		outputRetentionSlots*(defaultRetainedLineBytes+2)

	// MaximumMaxOutputRetentionBytes is the hard process-local aggregate
	// retention ceiling accepted through Options.
	MaximumMaxOutputRetentionBytes = outputReadBufferBytes +
		discardedPrefixRetentionSlots*maxDiscardedOutputPrefixBytes +
		outputRetentionSlots*(maximumRetainedLineBytes+2)
)

// RetentionBudgetForLineBytes returns the aggregate transport budget needed
// for a valid retained-line threshold, or zero when the threshold is outside
// the supported range.
func RetentionBudgetForLineBytes(lineBytes int) int {
	if lineBytes < 1 || lineBytes > maximumRetainedLineBytes {
		return 0
	}
	return outputReadBufferBytes +
		discardedPrefixRetentionSlots*maxDiscardedOutputPrefixBytes +
		outputRetentionSlots*(lineBytes+2)
}

func normalizeOutputRetention(opts *Options) (int, error) {
	switch {
	case opts.MaxOutputRetentionBytes < 0:
		return 0, fmt.Errorf("%w: budget must be non-negative", ErrInvalidOutputRetention)
	case opts.MaxOutputRetentionBytes == 0:
		opts.MaxOutputRetentionBytes = DefaultMaxOutputRetentionBytes
	case opts.MaxOutputRetentionBytes > MaximumMaxOutputRetentionBytes:
		return 0, fmt.Errorf(
			"%w: %d exceeds hard maximum %d",
			ErrInvalidOutputRetention,
			opts.MaxOutputRetentionBytes,
			MaximumMaxOutputRetentionBytes,
		)
	}
	if opts.OversizeOutputPolicy > DiscardOversizeOutput {
		return 0, fmt.Errorf("%w: %d", ErrInvalidOversizePolicy, opts.OversizeOutputPolicy)
	}
	lineBytes := (opts.MaxOutputRetentionBytes-
		outputReadBufferBytes-
		discardedPrefixRetentionSlots*maxDiscardedOutputPrefixBytes)/outputRetentionSlots - 2
	if lineBytes < 1 {
		return 0, fmt.Errorf(
			"%w: %d leaves no retained-line capacity",
			ErrInvalidOutputRetention,
			opts.MaxOutputRetentionBytes,
		)
	}
	return lineBytes, nil
}
