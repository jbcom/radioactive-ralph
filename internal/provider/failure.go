package provider

import (
	"context"
	"errors"

	"github.com/jbcom/radioactive-ralph/internal/agent"
)

// FailureCategory is a closed privacy-safe durable provider failure code.
type FailureCategory string

const (
	// FailureProcessCleanup means Ralph could not prove process convergence.
	FailureProcessCleanup FailureCategory = "process_cleanup"
	// FailureOutputLimit means a bounded output/evidence ceiling was crossed.
	FailureOutputLimit FailureCategory = "output_limit"
	// FailureInteractivePrompt means the CLI requested operator input.
	FailureInteractivePrompt FailureCategory = "interactive_prompt"
	// FailureStall means the renewable progress lease expired.
	FailureStall FailureCategory = "stall_timeout"
	// FailureTurnDeadline means the absolute turn deadline expired.
	FailureTurnDeadline FailureCategory = "turn_deadline"
	// FailureCanceled means an external caller canceled the turn.
	FailureCanceled FailureCategory = "canceled"
	// FailureProviderRejected means the provider reported an unsuccessful turn.
	FailureProviderRejected FailureCategory = "provider_rejected"
	// FailureRuntime is the fail-closed category for unrecognized failures.
	FailureRuntime FailureCategory = "provider_runtime"
)

// Failure is the privacy-safe durable representation of a provider error.
// Cause remains available transiently for errors.Is/debug logging, but Summary
// and Category are the only fields suitable for evidence or event persistence.
type Failure struct {
	Category FailureCategory
	Summary  string
	Cause    error
}

func (f Failure) Error() string { return f.Summary }
func (f Failure) Unwrap() error { return f.Cause }

// ClassifyFailure converts a transient provider error to its durable,
// provider-output-free category and summary.
func ClassifyFailure(err error) Failure {
	if err == nil {
		return Failure{}
	}
	switch {
	case errors.Is(err, agent.ErrProcessSessionCleanup),
		errors.Is(err, agent.ErrProcessTermination),
		errors.Is(err, agent.ErrProcessExitObservation):
		return Failure{Category: FailureProcessCleanup, Summary: "provider process cleanup failed", Cause: err}
	case errors.Is(err, agent.ErrObservedOutputTooLarge),
		errors.Is(err, agent.ErrOutputLineTooLong),
		errors.Is(err, ErrAuthoritativeResultTooLarge),
		errors.Is(err, ErrStructuredEvidenceTooLarge),
		errors.Is(err, ErrProviderOutputTooLarge),
		errors.Is(err, ErrStreamJSONLineTooLong):
		return Failure{Category: FailureOutputLimit, Summary: "provider output exceeded a safety limit", Cause: err}
	}
	var blocked *BlockedError
	if errors.As(err, &blocked) {
		if blocked.Reason == BlockReasonPrompt {
			return Failure{Category: FailureInteractivePrompt, Summary: "provider requested interactive input", Cause: err}
		}
		return Failure{Category: FailureStall, Summary: "provider made no progress before its stall timeout", Cause: err}
	}
	switch {
	case errors.Is(err, ErrProviderStalled):
		return Failure{Category: FailureStall, Summary: "provider made no progress before its stall timeout", Cause: err}
	case errors.Is(err, context.DeadlineExceeded):
		return Failure{Category: FailureTurnDeadline, Summary: "provider exceeded its total turn deadline", Cause: err}
	case errors.Is(err, context.Canceled):
		return Failure{Category: FailureCanceled, Summary: "provider turn was canceled", Cause: err}
	case errors.Is(err, ErrClaudeResultFailed),
		errors.Is(err, ErrClaudeMaximumTurns),
		errors.Is(err, ErrClaudeMissingResult),
		errors.Is(err, ErrCodexTurnFailed),
		errors.Is(err, ErrCodexOversizeSchema),
		errors.Is(err, ErrOpencodeReportedError),
		errors.Is(err, ErrOpencodeFinalReason),
		errors.Is(err, ErrOpencodeMissingFinish),
		errors.Is(err, ErrOpencodeInvalidUsage):
		return Failure{Category: FailureProviderRejected, Summary: "provider reported an unsuccessful turn", Cause: err}
	default:
		return Failure{Category: FailureRuntime, Summary: "provider execution failed", Cause: err}
	}
}
