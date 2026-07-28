package provider

import (
	"errors"
	"testing"
)

// TestClaudeUnstatusedAPIErrorIsCategorized pins that a real upstream failure
// with no api_error_status still gets a category.
//
// Verified against claude 2.1.220: a CLI that is not logged in emits
// {"is_error":true,"subtype":"success","api_error_status":null,
// "terminal_reason":"api_error"}. Every status case missed it, so the operator
// got "claude reported an unsuccessful result" -- which says nothing -- for a
// problem whose remedy is one command.
//
// This cost real debugging time: a live E2E failure read as "containment breaks
// real provider turns" and was only correctly diagnosed by reproducing the raw
// CLI invocation by hand. The frame had a usable signal the whole time.
//
// terminal_reason is a structured enum, not the frame's operator-facing prose,
// so reading it does not cross the never-scrape boundary that deliberately
// keeps provider text out of Ralph's error surface.
func TestClaudeUnstatusedAPIErrorIsCategorized(t *testing.T) {
	frame := claudeResultFrame{
		IsError:        true,
		Subtype:        "success", // yes, "success" on a hard failure.
		APIErrorStatus: 0,         // null in the wire frame.
		TerminalReason: "api_error",
	}

	err := frame.failure()
	if err == nil {
		t.Fatal("failure() = nil for a frame with is_error:true")
	}
	if errors.Is(err, ErrClaudeResultFailed) {
		t.Fatalf("failure() = %v, the generic sentinel, for a frame that explicitly "+
			"reports terminal_reason=api_error; the turn is known to have died against "+
			"the API and saying only \"unsuccessful result\" discards that", err)
	}
	if !errors.Is(err, ErrClaudeServiceUnavailable) {
		t.Fatalf("failure() = %v, want ErrClaudeServiceUnavailable", err)
	}
}

// TestClaudeStatusStillWinsOverTerminalReason keeps the new fallback subordinate
// to the precise signal. A frame carrying BOTH must categorize on the status,
// which names the actual failure; terminal_reason only says "against the API".
func TestClaudeStatusStillWinsOverTerminalReason(t *testing.T) {
	frame := claudeResultFrame{
		IsError:        true,
		Subtype:        "success",
		APIErrorStatus: 401,
		TerminalReason: "api_error",
	}
	if err := frame.failure(); !errors.Is(err, ErrClaudeAuthentication) {
		t.Fatalf("failure() = %v, want ErrClaudeAuthentication: a populated "+
			"api_error_status is the more precise signal and must not be shadowed "+
			"by the terminal_reason fallback", err)
	}
}

// TestClaudeCompletedTerminalReasonIsNotAFailure guards the other direction:
// the fallback must not fire for a turn that ended normally.
func TestClaudeCompletedTerminalReasonIsNotAFailure(t *testing.T) {
	frame := claudeResultFrame{IsError: false, Subtype: "success", TerminalReason: "completed"}
	if err := frame.failure(); err != nil {
		t.Fatalf("failure() = %v for a completed successful frame, want nil", err)
	}
}

// TestClaudeUnknownFailureStaysGeneric keeps the fallback narrow. A failure with
// neither a status nor an api_error terminal_reason is genuinely uncategorized,
// and inventing a category for it would be a confident wrong answer.
func TestClaudeUnknownFailureStaysGeneric(t *testing.T) {
	frame := claudeResultFrame{IsError: true, Subtype: "success", TerminalReason: "something_else"}
	if err := frame.failure(); !errors.Is(err, ErrClaudeResultFailed) {
		t.Fatalf("failure() = %v, want the generic ErrClaudeResultFailed for a "+
			"failure the frame does not describe", err)
	}
}
