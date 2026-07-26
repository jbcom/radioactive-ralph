package agent

import (
	"errors"
	"testing"
)

func TestOutputRetentionArithmeticIncludesAllDiscardedPrefixOwners(t *testing.T) {
	fixedBytes := outputReadBufferBytes +
		discardedPrefixRetentionSlots*maxDiscardedOutputPrefixBytes
	for _, lineBytes := range []int{1, 16, defaultRetainedLineBytes, maximumRetainedLineBytes} {
		want := fixedBytes + outputRetentionSlots*(lineBytes+2)
		if got := RetentionBudgetForLineBytes(lineBytes); got != want {
			t.Errorf("RetentionBudgetForLineBytes(%d) = %d, want %d", lineBytes, got, want)
		}
		opts := Options{MaxOutputRetentionBytes: want}
		gotLineBytes, err := normalizeOutputRetention(&opts)
		if err != nil {
			t.Fatalf("normalizeOutputRetention(%d): %v", want, err)
		}
		if gotLineBytes != lineBytes {
			t.Errorf("normalizeOutputRetention(%d) = %d, want %d", want, gotLineBytes, lineBytes)
		}
	}

	if got, want := DefaultMaxOutputRetentionBytes,
		RetentionBudgetForLineBytes(defaultRetainedLineBytes); got != want {
		t.Errorf("DefaultMaxOutputRetentionBytes = %d, want %d", got, want)
	}
	if got, want := MaximumMaxOutputRetentionBytes,
		RetentionBudgetForLineBytes(maximumRetainedLineBytes); got != want {
		t.Errorf("MaximumMaxOutputRetentionBytes = %d, want %d", got, want)
	}
}

func TestOutputRetentionArithmeticRejectsOutsideExactBoundaries(t *testing.T) {
	minimum := RetentionBudgetForLineBytes(1)
	if _, err := normalizeOutputRetention(&Options{
		MaxOutputRetentionBytes: minimum - 1,
	}); !errors.Is(err, ErrInvalidOutputRetention) {
		t.Fatalf("minimum budget minus one error = %v, want ErrInvalidOutputRetention", err)
	}

	maximumOpts := Options{MaxOutputRetentionBytes: MaximumMaxOutputRetentionBytes}
	lineBytes, err := normalizeOutputRetention(&maximumOpts)
	if err != nil {
		t.Fatalf("maximum budget: %v", err)
	}
	if lineBytes != maximumRetainedLineBytes {
		t.Fatalf("maximum budget line bytes = %d, want %d", lineBytes, maximumRetainedLineBytes)
	}
	if _, err := normalizeOutputRetention(&Options{
		MaxOutputRetentionBytes: MaximumMaxOutputRetentionBytes + 1,
	}); !errors.Is(err, ErrInvalidOutputRetention) {
		t.Fatalf("maximum budget plus one error = %v, want ErrInvalidOutputRetention", err)
	}
}
