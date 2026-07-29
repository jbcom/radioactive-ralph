package doctor

import (
	"context"
	"strings"
	"testing"
)

// TestReportsTheProgressLease surfaces the bound that silently starves long
// quiet steps.
//
// A provider turn is governed by two limits and the SHORTER one bites: the turn
// deadline is 30m, but the progress lease (DefaultStallTimeout) is 3m and is
// renewed by OUTPUT. A command that runs a long time while printing nothing is
// indistinguishable from a hung provider, so the watchdog kills it and the
// reaper takes the claim back.
//
// Measured on a real self-test: `go test -race ./internal/store/` prints one
// line after 138s (30s warm, 62s cold-cache), against that 180s lease. It
// reclaimed FOUR times in one run. Nothing warned first -- the value appears in
// no CLI output, so an operator learns it from a task that keeps dying.
//
// doctor is where a bound you are about to violate belongs: it is the command
// people run when something is wrong, and this is the setting least likely to
// be guessed.
func TestReportsTheProgressLease(t *testing.T) {
	report := Run(context.Background(), WithRunner(func(context.Context, string, ...string) (string, error) {
		return "", nil
	}))

	var found *Check
	for i := range report.Checks {
		if strings.Contains(strings.ToLower(report.Checks[i].Name), "lease") ||
			strings.Contains(strings.ToLower(report.Checks[i].Name), "stall") {
			found = &report.Checks[i]
			break
		}
	}
	if found == nil {
		names := make([]string, 0, len(report.Checks))
		for _, c := range report.Checks {
			names = append(names, c.Name)
		}
		t.Fatalf("no check reports the progress lease; an operator cannot learn "+
			"the bound their long quiet step is about to violate. checks=%v", names)
	}
	// The NUMBER is the point. A check that says "a lease exists" without
	// stating it leaves the reader exactly as unable to size a step as before.
	if !strings.Contains(found.Detail, "3m") {
		t.Errorf("lease check detail = %q, want it to state the actual default "+
			"(3m) -- naming the setting without its value does not help anyone "+
			"decide whether their step fits", found.Detail)
	}

	// Everything actionable must be in Detail, because Report.WriteText prints
	// Remediate ONLY when Severity != OK -- and this check is deliberately
	// always OK. The first version parked a careful two-fix explanation in
	// Remediate, where no operator would ever see it.
	if found.Severity == OK && found.Remediate != "" {
		t.Errorf("an always-OK check carries a Remediate line (%q) that "+
			"WriteText suppresses; put it in Detail or it is written for nobody",
			found.Remediate)
	}

	// It must also NOT send a reader chasing the wrong knob. Acceptance
	// commands run after the turn, under a separate budget, so blaming a slow
	// `accept:` on the stall lease is the exact confusion that cost a long
	// misdiagnosis.
	if !strings.Contains(found.Detail, "AFTER the turn") {
		t.Errorf("lease check detail = %q, want it to distinguish acceptance "+
			"commands from provider stalls -- the lease does not govern them",
			found.Detail)
	}

	// The rendered artifact, not the struct: this whole finding came from
	// checking the field and not the output.
	var out strings.Builder
	report.WriteText(&out)
	if !strings.Contains(out.String(), "AFTER the turn") {
		t.Errorf("the rendered doctor output omits the acceptance distinction:\n%s",
			out.String())
	}
}
