package provider

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

// realClaudeAuthFailureFrame is a VERBATIM result frame captured from
// claude 2.1.220 by running it against an invalid API key. It is here because
// it refutes the shape the code assumed.
//
// Note `"subtype":"success"` alongside `"is_error":true`. Ralph's failure()
// keyed on subtype, so a real authentication failure arrived as the generic
// ErrClaudeResultFailed and an operator saw "claude reported an unsuccessful
// result" for what is actually "your key is invalid, go fix it".
//
// The structured signal is api_error_status, not prose.
const realClaudeAuthFailureFrame = `{"is_error":true,"duration_api_ms":0,"num_turns":1,` +
	`"stop_reason":"stop_sequence","session_id":"eba86727-e932-48b1-ad44-f0c17bec0755",` +
	`"total_cost_usd":0,"usage":{"input_tokens":0,"output_tokens":0},` +
	`"terminal_reason":"api_error","subtype":"success","api_error_status":401,` +
	`"result":"Invalid API key · Fix external API key","type":"result",` +
	`"duration_ms":692,"uuid":"2eb6660d-bf81-4ec7-94a2-dd6846629a66"}`

func decodeClaudeFrame(t *testing.T, raw string) claudeResultFrame {
	t.Helper()
	var frame claudeResultFrame
	if err := json.Unmarshal([]byte(raw), &frame); err != nil {
		t.Fatalf("decode frame: %v", err)
	}
	return frame
}

// TestClaudeAuthFailureIsClassified is the regression for the real capture: an
// invalid key must produce the authentication category, not a generic failure.
func TestClaudeAuthFailureIsClassified(t *testing.T) {
	frame := decodeClaudeFrame(t, realClaudeAuthFailureFrame)

	err := frame.failure()
	if err == nil {
		t.Fatal("a frame with is_error:true reported success")
	}
	if !errors.Is(err, ErrClaudeAuthentication) {
		t.Fatalf("err = %v, want ErrClaudeAuthentication — the frame carries "+
			"api_error_status 401", err)
	}
	// The failure must stay a fixed constant: no provider prose crosses the
	// boundary. "Invalid API key · Fix external API key" is operator-visible
	// text from an external process and must not be laundered into Ralph's
	// error surface.
	if strings.Contains(err.Error(), "Invalid API key") ||
		strings.Contains(err.Error(), "Fix external") {
		t.Fatalf("err %q leaks provider prose; the category must be a fixed constant", err)
	}
}

// TestClaudeSubtypeSuccessWithIsErrorIsAFailure pins the specific trap the real
// capture exposed. Claude reports subtype "success" on an API error, so any
// logic keying on subtype alone reads a hard failure as a completed turn.
func TestClaudeSubtypeSuccessWithIsErrorIsAFailure(t *testing.T) {
	frame := decodeClaudeFrame(t, realClaudeAuthFailureFrame)
	if frame.Subtype != "success" {
		t.Fatalf("fixture drifted: subtype = %q, want success (that pairing IS the trap)", frame.Subtype)
	}
	if !frame.IsError {
		t.Fatal("fixture drifted: is_error must be true")
	}
	if frame.failure() == nil {
		t.Fatal("subtype 'success' with is_error true was treated as a successful turn")
	}
}

// TestClaudeFailureCategories covers the status codes an operator most needs
// told apart. Each is a distinct remediation: re-auth, wait, top up, or retry.
func TestClaudeFailureCategories(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		want   error
	}{
		{"unauthorized", 401, ErrClaudeAuthentication},
		{"forbidden", 403, ErrClaudeModelAccess},
		{"rate limited", 429, ErrClaudeRateLimit},
		{"server error", 500, ErrClaudeServiceUnavailable},
		{"bad gateway", 502, ErrClaudeServiceUnavailable},
		{"unavailable", 503, ErrClaudeServiceUnavailable},
		{"gateway timeout", 504, ErrClaudeServiceUnavailable},
		{"bad request", 400, ErrClaudeInvalidRequest},
	} {
		t.Run(tc.name, func(t *testing.T) {
			frame := claudeResultFrame{IsError: true, Subtype: "success", APIErrorStatus: tc.status}
			err := frame.failure()
			if !errors.Is(err, tc.want) {
				t.Fatalf("status %d -> %v, want %v", tc.status, err, tc.want)
			}
		})
	}
}

// TestClaudeUnknownStatusStaysGeneric keeps the classifier honest: a status it
// does not recognize must fall back to the generic failure rather than be
// forced into the nearest category. A wrong category is worse than none — it
// sends an operator to fix the wrong thing.
func TestClaudeUnknownStatusStaysGeneric(t *testing.T) {
	frame := claudeResultFrame{IsError: true, Subtype: "success", APIErrorStatus: 418}
	err := frame.failure()
	if !errors.Is(err, ErrClaudeResultFailed) {
		t.Fatalf("unrecognized status 418 -> %v, want the generic failure", err)
	}
	for _, narrower := range []error{
		ErrClaudeAuthentication, ErrClaudeRateLimit, ErrClaudeModelAccess,
		ErrClaudeServiceUnavailable, ErrClaudeInvalidRequest,
	} {
		if errors.Is(err, narrower) {
			t.Fatalf("status 418 was forced into %v", narrower)
		}
	}
}

// TestClaudeMaxTurnsStillClassified guards the one category that already
// existed. It comes from the subtype, not a status code, so the new
// status-based path must not displace it.
func TestClaudeMaxTurnsStillClassified(t *testing.T) {
	frame := claudeResultFrame{IsError: true, Subtype: "error_max_turns"}
	if err := frame.failure(); !errors.Is(err, ErrClaudeMaximumTurns) {
		t.Fatalf("err = %v, want ErrClaudeMaximumTurns", err)
	}
}

// TestClaudeGenuineSuccessIsStillSuccess is the control. Over-eager
// classification that failed a real successful turn would be far worse than
// leaving auth failures generic.
func TestClaudeGenuineSuccessIsStillSuccess(t *testing.T) {
	frame := claudeResultFrame{IsError: false, Subtype: "success"}
	if err := frame.failure(); err != nil {
		t.Fatalf("a genuine success reported %v", err)
	}
}

// TestClaudeAuthCategoryIsNotRetryable ties classification to behavior. An
// invalid key does not fix itself, so retrying burns the retry budget and
// delays the operator seeing the real problem — whereas a 429 or 503 is exactly
// what retries are for.
func TestClaudeAuthCategoryIsNotRetryable(t *testing.T) {
	if ClaudeFailureRetryable(ErrClaudeAuthentication) {
		t.Error("authentication failure marked retryable; an invalid key does not fix itself")
	}
	if ClaudeFailureRetryable(ErrClaudeModelAccess) {
		t.Error("model-access failure marked retryable")
	}
	if ClaudeFailureRetryable(ErrClaudeInvalidRequest) {
		t.Error("invalid-request failure marked retryable")
	}
	if !ClaudeFailureRetryable(ErrClaudeRateLimit) {
		t.Error("rate limit marked non-retryable; waiting is exactly the remedy")
	}
	if !ClaudeFailureRetryable(ErrClaudeServiceUnavailable) {
		t.Error("service-unavailable marked non-retryable")
	}
}

// TestClaudeCategoriesReachTheDurableFailureSurface is what makes the
// classification useful rather than decorative. ClassifyFailure produces the
// category persisted on the task and shown to an operator; if the new
// sentinels collapsed into the generic "provider reported an unsuccessful
// turn" there, an operator would still be told nothing.
func TestClaudeCategoriesReachTheDurableFailureSurface(t *testing.T) {
	for _, tc := range []struct {
		err  error
		want FailureCategory
	}{
		{ErrClaudeAuthentication, FailureProviderAuth},
		{ErrClaudeModelAccess, FailureProviderAuth},
		{ErrClaudeRateLimit, FailureProviderThrottled},
		{ErrClaudeServiceUnavailable, FailureProviderUnavailable},
		{ErrClaudeInvalidRequest, FailureProviderRejected},
		// Unchanged: the pre-existing categories must not shift.
		{ErrClaudeResultFailed, FailureProviderRejected},
		{ErrClaudeMaximumTurns, FailureProviderRejected},
	} {
		t.Run(string(tc.want)+"/"+tc.err.Error(), func(t *testing.T) {
			got := ClassifyFailure(tc.err)
			if got.Category != tc.want {
				t.Fatalf("category = %q, want %q", got.Category, tc.want)
			}
			// The durable summary must stay a fixed phrase — never provider prose.
			if strings.Contains(got.Summary, "Invalid API key") {
				t.Fatalf("summary %q leaks provider text", got.Summary)
			}
		})
	}
}

// TestClaudeAuthFailureSummaryTellsTheOperatorWhatToDo checks the summary is
// actionable. "provider reported an unsuccessful turn" sends an operator to
// read logs; naming the credential sends them to fix the credential.
func TestClaudeAuthFailureSummaryTellsTheOperatorWhatToDo(t *testing.T) {
	got := ClassifyFailure(ErrClaudeAuthentication)
	if !strings.Contains(got.Summary, "authentication") {
		t.Fatalf("summary = %q, want it to name authentication", got.Summary)
	}
}
