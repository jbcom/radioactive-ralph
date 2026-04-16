package voice

import (
	"strings"
	"testing"
)

func TestSayGreenStartup(t *testing.T) {
	msg := Say(VariantGreen, EventStartup, Fields{})
	if !strings.Contains(msg, "Ralph") {
		t.Errorf("green startup should mention Ralph: %q", msg)
	}
}

func TestSayInterpolation(t *testing.T) {
	msg := Say(VariantGreen, EventPRMerge, Fields{PRNumber: 42, Repo: "org/repo"})
	if !strings.Contains(msg, "42") {
		t.Errorf("PR number should interpolate: %q", msg)
	}
	if !strings.Contains(msg, "org/repo") {
		t.Errorf("repo should interpolate: %q", msg)
	}
}

func TestSayFallsBackToFallback(t *testing.T) {
	// grey has no bespoke templates registered yet, so fallback fires.
	msg := Say(VariantGrey, EventStartup, Fields{})
	if msg == "" {
		t.Error("fallback should not produce empty output")
	}
	if !strings.Contains(msg, "Ralph") {
		t.Errorf("fallback startup should mention Ralph: %q", msg)
	}
}

func TestSayUnknownVariantUsesFallback(t *testing.T) {
	msg := Say(Variant("made-up"), EventShutdown, Fields{})
	if !strings.Contains(msg, "Ralph") {
		t.Errorf("unknown variant should use fallback: %q", msg)
	}
}

func TestSayNoTemplateAtAll(t *testing.T) {
	// Use an event name that has no template anywhere — Say returns a
	// debug marker rather than panicking.
	msg := Say(VariantGreen, Event("totally-made-up"), Fields{})
	if !strings.HasPrefix(msg, "[green/totally-made-up]") {
		t.Errorf("expected debug marker, got %q", msg)
	}
}

func TestRegisterOverrides(t *testing.T) {
	Register(VariantGreen, EventStartup, "custom startup: {extra}")
	t.Cleanup(ResetForTesting)
	got := Say(VariantGreen, EventStartup, Fields{Extra: "yep"})
	if got != "custom startup: yep" {
		t.Errorf("override failed: %q", got)
	}
}

func TestRegisterFallbackOverrides(t *testing.T) {
	RegisterFallback(EventCycleEnd, "custom cycle end: {count}")
	t.Cleanup(ResetForTesting)
	// VariantGrey has no EventCycleEnd template, so fallback fires.
	got := Say(VariantGrey, EventCycleEnd, Fields{Count: 5})
	if got != "custom cycle end: 5" {
		t.Errorf("fallback override failed: %q", got)
	}
}

func TestInterpolateAllFields(t *testing.T) {
	tpl := "{repo} {branch} PR#{pr} task={task} count={count} {reason} {usd} extra={extra}"
	got := interpolate(tpl, Fields{
		Repo: "org/r", Branch: "main", PRNumber: 7, TaskID: "t1",
		Count: 3, Reason: "why", USD: "$5.00", Extra: "ok",
	})
	want := "org/r main PR#7 task=t1 count=3 why $5.00 extra=ok"
	if got != want {
		t.Errorf("interpolate:\n got %q\nwant %q", got, want)
	}
}

func TestInterpolateEmptyFields(t *testing.T) {
	tpl := "{repo} {branch}"
	got := interpolate(tpl, Fields{})
	if got != " " {
		t.Errorf("empty fields should produce empty substitutions, got %q", got)
	}
}

func TestBlueSayDoesNotReferenceEdits(t *testing.T) {
	// Sanity check: blue is the read-only variant; its voice
	// shouldn't have merge messages. (Reviewer voice only.)
	msg := Say(VariantBlue, EventReviewApproved, Fields{PRNumber: 100})
	if !strings.Contains(msg, "100") {
		t.Errorf("blue review should include PR number: %q", msg)
	}
}

func TestSayCheckAllGreenEvents(t *testing.T) {
	// Every event green has registered should produce non-empty,
	// non-debug-marker output.
	events := []Event{
		EventStartup, EventShutdown, EventCycleStart,
		EventSessionSpawn, EventSessionDeath, EventSessionResume,
		EventTaskClaim, EventTaskDone, EventPRMerge, EventPRMergeFailed,
		EventReviewApproved, EventReviewChanges, EventSpendCapHit,
	}
	for _, ev := range events {
		msg := Say(VariantGreen, ev, Fields{})
		if msg == "" {
			t.Errorf("green.%s produced empty message", ev)
		}
		if strings.HasPrefix(msg, "[green/") {
			t.Errorf("green.%s fell through to debug marker: %q", ev, msg)
		}
	}
}
