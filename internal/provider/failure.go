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
	// FailureInteractivePrompt means the CLI requested operator input of an
	// unrecognised shape. The three kind-specific codes below are preferred
	// when the prompt classifier recognises one, because they carry the only
	// thing an operator can act on: WHICH response the block needs.
	//
	// These are CATEGORIES rather than summary text on purpose. The operator
	// projection rebuilds a failure from the category alone
	// (observe.failureForEvent), so a specialised summary never crosses -- only
	// a new constant does.
	FailureInteractivePrompt FailureCategory = "interactive_prompt"
	// FailureInteractivePromptPermission means the provider asked to be ALLOWED
	// to act. Usually answered by a write-path or binding grant, not a
	// keystroke.
	FailureInteractivePromptPermission FailureCategory = "interactive_prompt_permission"
	// FailureInteractivePromptConfirm means a yes/no on an action the CLI
	// already intended. Usually answered by a flag that suppresses the prompt.
	FailureInteractivePromptConfirm FailureCategory = "interactive_prompt_confirm"
	// FailureInteractivePromptClarification means an open question about the
	// task. The step's scoped context was insufficient, so the PLAN needs work.
	FailureInteractivePromptClarification FailureCategory = "interactive_prompt_clarification"
	// FailureStall means the renewable progress lease expired.
	FailureStall FailureCategory = "stall_timeout"
	// FailureTurnDeadline means the absolute turn deadline expired.
	FailureTurnDeadline FailureCategory = "turn_deadline"
	// FailureCanceled means an external caller canceled the turn.
	FailureCanceled FailureCategory = "canceled"
	// FailureProviderRejected means the provider reported an unsuccessful turn.
	FailureProviderRejected FailureCategory = "provider_rejected"
	// FailureProviderAuth means the provider rejected the credential or denied
	// access to the requested model. Separate from provider_rejected because
	// the remediation is an operator action (fix the key, grant the model), and
	// because it is NOT worth retrying — a credential does not fix itself.
	FailureProviderAuth FailureCategory = "provider_auth"
	// FailureProviderThrottled means the provider rate-limited the turn.
	// Retrying after a wait is precisely the remedy.
	FailureProviderThrottled FailureCategory = "provider_throttled"
	// FailureProviderUnavailable means an upstream provider fault (HTTP 5xx).
	// Transient by nature, so worth retrying.
	FailureProviderUnavailable FailureCategory = "provider_unavailable"
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

// Retryable reports whether re-running the turn could plausibly succeed.
//
// This lives on the classification rather than in dispatch because the answer
// is a property of WHY the turn failed, and every dispatch path needs the same
// answer. The split is operational: retrying an invalid credential burns the
// retry budget on turns that cannot succeed and DELAYS the operator seeing a
// terminal error, while a 429 or a 503 is precisely what retries exist for.
//
// The default is RETRYABLE, matching the pre-classification behavior — a
// category nobody has reasoned about should not silently become terminal.
func (f Failure) Retryable() bool {
	switch f.Category {
	case FailureProviderAuth,
		// The provider rejected the request as written; the same request will
		// be rejected again.
		FailureProviderRejected,
		// The CLI is asking for an operator, not for another turn.
		FailureInteractivePrompt,
		// The kind-specific prompt codes are non-retryable for the same reason
		// as the generic one: the CLI is asking for an operator, not another
		// turn. Omitting them here would make a CLASSIFIED prompt retry three
		// times where an unclassified one fails immediately -- a worse outcome
		// for the better-diagnosed case.
		FailureInteractivePromptPermission,
		FailureInteractivePromptConfirm,
		FailureInteractivePromptClarification,
		// The same turn will cross the same ceiling.
		FailureOutputLimit,
		// Ralph could not prove process convergence; retrying compounds it.
		FailureProcessCleanup,
		// An external caller asked for this; do not fight it.
		FailureCanceled:
		return false
	default:
		return true
	}
}

// RetryBudget returns the retry count dispatch should hand to the store for
// this failure: the caller's budget when the failure is worth retrying, and
// ZERO when it is not, which makes the task fail terminally on this attempt.
func (f Failure) RetryBudget(configured int) int {
	if f.Retryable() {
		return configured
	}
	return 0
}

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
			category, summary := promptBlockFailure(blocked.Kind)
			return Failure{Category: category, Summary: summary, Cause: err}
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
	case errors.Is(err, ErrClaudeAuthentication),
		errors.Is(err, ErrClaudeModelAccess):
		return Failure{
			Category: FailureProviderAuth,
			Summary:  "provider authentication or model access was denied",
			Cause:    err,
		}
	case errors.Is(err, ErrClaudeRateLimit):
		return Failure{
			Category: FailureProviderThrottled,
			Summary:  "provider rate limited the turn",
			Cause:    err,
		}
	case errors.Is(err, ErrClaudeServiceUnavailable):
		return Failure{
			Category: FailureProviderUnavailable,
			Summary:  "provider service was unavailable",
			Cause:    err,
		}
	case errors.Is(err, ErrClaudeResultFailed),
		errors.Is(err, ErrClaudeAPIFailure),
		errors.Is(err, ErrClaudeMaximumTurns),
		errors.Is(err, ErrClaudeInvalidRequest),
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

// promptBlockFailure names WHAT an interactive block wanted, from the closed
// prompt taxonomy. Every branch returns a fixed constant -- no provider text --
// so this stays inside the content-safety boundary while telling an operator
// which response the block actually needs.
//
// The three kinds want different things, which is the whole reason to
// distinguish them: a permission request usually wants a policy change, a
// confirmation usually wants a flag, and a clarification means the step's
// scoped context was insufficient -- the plan needs work, not the config.
func promptBlockFailure(kind PromptKind) (FailureCategory, string) {
	switch kind {
	case PromptKindPermission:
		return FailureInteractivePromptPermission,
			"provider requested permission to act (usually a write-path or binding grant, not a keystroke)"
	case PromptKindConfirm:
		return FailureInteractivePromptConfirm,
			"provider requested confirmation of an action it already intended (usually suppressible with a flag)"
	case PromptKindClarification:
		return FailureInteractivePromptClarification,
			"provider requested clarification about the task (the step's scoped context was insufficient)"
	default:
		// Unclassified: keep the original category and wording rather than
		// inventing a kind.
		return FailureInteractivePrompt, "provider requested interactive input"
	}
}
