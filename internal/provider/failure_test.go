package provider

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jbcom/radioactive-ralph/internal/agent"
)

func TestClassifyFailureUsesTypedPrivacySafeCategories(t *testing.T) {
	secret := errors.New("provider diagnostic contains SECRET-TOKEN")
	tests := []struct {
		name     string
		err      error
		category FailureCategory
	}{
		{name: "turn", err: errors.Join(context.DeadlineExceeded, secret), category: FailureTurnDeadline},
		{name: "stall", err: errors.Join(ErrProviderStalled, secret), category: FailureStall},
		{name: "prompt", err: errors.Join(&BlockedError{Reason: BlockReasonPrompt}, secret), category: FailureInteractivePrompt},
		{name: "cleanup", err: errors.Join(agent.ErrProcessSessionCleanup, secret), category: FailureProcessCleanup},
		{name: "output", err: errors.Join(ErrAuthoritativeResultTooLarge, secret), category: FailureOutputLimit},
		{name: "unknown", err: secret, category: FailureRuntime},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ClassifyFailure(tt.err)
			if got.Category != tt.category {
				t.Fatalf("category = %q, want %q", got.Category, tt.category)
			}
			if strings.Contains(got.Summary, "SECRET") {
				t.Fatalf("summary leaked provider diagnostic: %q", got.Summary)
			}
			if !errors.Is(got, secret) {
				t.Fatal("transient cause was not retained for errors.Is")
			}
		})
	}
}

func TestBlockedErrorRetainsCompatibilitySentinel(t *testing.T) {
	err := &BlockedError{Reason: BlockReasonStall}
	if !errors.Is(err, ErrAgentBlocked) {
		t.Fatal("BlockedError no longer matches ErrAgentBlocked")
	}
}
