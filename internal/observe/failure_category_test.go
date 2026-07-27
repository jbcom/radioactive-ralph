package observe_test

import (
	"strings"
	"testing"

	"github.com/jbcom/radioactive-ralph/internal/observe"
	"github.com/jbcom/radioactive-ralph/internal/store"
)

// TestEventCarriesTheProviderFailureCategory closes the loop from
// classification to the operator's screen. Deriving the failure from the event
// KIND alone means every failed turn reads the same regardless of cause, so an
// invalid credential is indistinguishable from a rate limit — which is exactly
// the ambiguity classification exists to remove.
//
// The provider category is a CLOSED SET of fixed constants, never provider
// prose, so carrying it does not weaken the content-safety boundary that keeps
// payload_json off this DTO.
func TestEventCarriesTheProviderFailureCategory(t *testing.T) {
	event := observe.EventFromMetadata(store.OperatorEvent{
		ID: 1, PlanID: "p", TaskID: "t", Kind: "task.failed",
		FailureCategory: "provider_auth",
	})
	if event.Failure == nil {
		t.Fatal("no failure summary on a task.failed event")
	}
	if !strings.Contains(string(event.Failure.Category), "provider_auth") {
		t.Fatalf("category = %q, want the provider category to reach the operator",
			event.Failure.Category)
	}
	if event.Failure.Retryable {
		t.Error("an authentication failure was reported as retryable; the operator " +
			"would wait for a retry that cannot succeed")
	}
}

// TestEventFailureSummaryMatchesWhatActuallyHappened pins the wording. The
// task.failed summary hardcoded "requeued", which became a lie once terminal
// categories stopped being requeued — an operator reading it would wait for a
// retry that is never coming.
func TestEventFailureSummaryMatchesWhatActuallyHappened(t *testing.T) {
	terminal := observe.EventFromMetadata(store.OperatorEvent{
		ID: 1, Kind: "task.failed", FailureCategory: "provider_auth",
	})
	if terminal.Failure == nil {
		t.Fatal("no failure summary")
	}
	if strings.Contains(terminal.Failure.Summary, "requeued") {
		t.Fatalf("summary = %q, but a non-retryable failure is NOT requeued",
			terminal.Failure.Summary)
	}

	retryable := observe.EventFromMetadata(store.OperatorEvent{
		ID: 2, Kind: "task.failed", FailureCategory: "provider_throttled",
	})
	if retryable.Failure == nil || !retryable.Failure.Retryable {
		t.Fatalf("a throttled failure must stay retryable: %+v", retryable.Failure)
	}
}

// TestEventWithoutAProviderCategoryKeepsTheGenericSummary is the compatibility
// control: events predating the category, and every non-provider failure, must
// still produce the summary they always did.
func TestEventWithoutAProviderCategoryKeepsTheGenericSummary(t *testing.T) {
	event := observe.EventFromMetadata(store.OperatorEvent{ID: 1, Kind: "task.failed"})
	if event.Failure == nil {
		t.Fatal("no failure summary")
	}
	if !event.Failure.Retryable {
		t.Error("an uncategorized task.failed must stay retryable, as before")
	}
}
