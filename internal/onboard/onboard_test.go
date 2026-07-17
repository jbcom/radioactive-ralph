package onboard

import (
	"bytes"
	"errors"
	"strings"
	"testing"
	"time"
)

// scriptedPrompter answers Confirm from a fixed queue of responses.
type scriptedPrompter struct {
	answers []any // each is bool (yes/no) or error (e.g. ErrQuit)
	i       int
}

func (s *scriptedPrompter) Confirm(_ string, _ bool) (bool, error) {
	if s.i >= len(s.answers) {
		return false, errors.New("scriptedPrompter: out of answers")
	}
	a := s.answers[s.i]
	s.i++
	switch v := a.(type) {
	case bool:
		return v, nil
	case error:
		return false, v
	default:
		return false, errors.New("scriptedPrompter: bad answer type")
	}
}

func baseDeps(prompter Prompter) (Deps, *bytes.Buffer) {
	var out bytes.Buffer
	return Deps{
		Out:            &out,
		Prompt:         prompter,
		Plan:           Plan{StateDir: "/s", DBPath: "/s/state.db", ServiceUnit: "unit", ServiceUnitPath: "/u/unit"},
		InstallService: func() error { return nil },
		WaitReachable:  func(time.Duration) bool { return true },
		ForegroundCmd:  "radioactive_ralph --supervisor",
		ManualCommands: "MANUAL: run these commands",
	}, &out
}

func TestRun_InstallSucceedsAndSupervisorComesUp(t *testing.T) {
	d, out := baseDeps(&scriptedPrompter{answers: []any{true}}) // Y install
	installed := false
	d.InstallService = func() error { installed = true; return nil }
	d.WaitReachable = func(time.Duration) bool { return true }

	outcome, err := Run(d)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if outcome != SupervisorReady {
		t.Errorf("outcome = %v, want SupervisorReady", outcome)
	}
	if !installed {
		t.Error("service was not installed")
	}
	if !strings.Contains(out.String(), "Supervisor is up") {
		t.Errorf("missing success message: %q", out.String())
	}
}

func TestRun_InstallFailsFallsBackToForeground(t *testing.T) {
	// Y (install, which fails) → y (run foreground).
	d, out := baseDeps(&scriptedPrompter{answers: []any{true, true}})
	d.InstallService = func() error { return errors.New("permission denied") }

	outcome, err := Run(d)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if outcome != PrintedForegroundHint {
		t.Errorf("outcome = %v, want PrintedForegroundHint", outcome)
	}
	if !strings.Contains(out.String(), "Could not install the service") {
		t.Errorf("missing install-failure message: %q", out.String())
	}
	if !strings.Contains(out.String(), "radioactive_ralph --supervisor") {
		t.Errorf("missing foreground command: %q", out.String())
	}
}

func TestRun_InstallSucceedsButNotReachableFallsBack(t *testing.T) {
	// Y install (ok) but supervisor never becomes reachable → n foreground → N manual.
	d, out := baseDeps(&scriptedPrompter{answers: []any{true, false}})
	d.WaitReachable = func(time.Duration) bool { return false }

	outcome, err := Run(d)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if outcome != PrintedCommands {
		t.Errorf("outcome = %v, want PrintedCommands", outcome)
	}
	if !strings.Contains(out.String(), "not reachable yet") {
		t.Errorf("missing not-reachable message: %q", out.String())
	}
	if !strings.Contains(out.String(), "MANUAL:") {
		t.Errorf("missing manual commands: %q", out.String())
	}
}

func TestRun_DeclineInstallChooseForeground(t *testing.T) {
	// n install → y foreground.
	d, out := baseDeps(&scriptedPrompter{answers: []any{false, true}})
	called := false
	d.InstallService = func() error { called = true; return nil }

	outcome, err := Run(d)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if called {
		t.Error("InstallService must NOT be called when the user declined")
	}
	if outcome != PrintedForegroundHint {
		t.Errorf("outcome = %v, want PrintedForegroundHint", outcome)
	}
	_ = out
}

func TestRun_QuitAtFirstPromptPrintsManual(t *testing.T) {
	d, out := baseDeps(&scriptedPrompter{answers: []any{ErrQuit}})
	outcome, err := Run(d)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if outcome != PrintedCommands {
		t.Errorf("outcome = %v, want PrintedCommands", outcome)
	}
	if !strings.Contains(out.String(), "MANUAL:") {
		t.Errorf("missing manual commands after quit: %q", out.String())
	}
}

func TestRun_DeclineEverythingPrintsManual(t *testing.T) {
	// n install → N foreground.
	d, _ := baseDeps(&scriptedPrompter{answers: []any{false, false}})
	outcome, err := Run(d)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if outcome != PrintedCommands {
		t.Errorf("outcome = %v, want PrintedCommands", outcome)
	}
}

func TestStdinPrompter(t *testing.T) {
	cases := []struct {
		in         string
		defaultYes bool
		want       bool
		wantErr    error
	}{
		{"y\n", false, true, nil},
		{"yes\n", false, true, nil},
		{"n\n", true, false, nil},
		{"\n", true, true, nil},   // empty → default
		{"\n", false, false, nil}, // empty → default
		{"q\n", true, false, ErrQuit},
		{"", true, false, ErrQuit}, // EOF → quit
	}
	for _, c := range cases {
		p := NewStdinPrompter(strings.NewReader(c.in), &bytes.Buffer{})
		got, err := p.Confirm("?", c.defaultYes)
		if !errors.Is(err, c.wantErr) {
			t.Errorf("Confirm(%q) err = %v, want %v", c.in, err, c.wantErr)
		}
		if err == nil && got != c.want {
			t.Errorf("Confirm(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

// recordingPrompter captures the defaultYes it was asked with for each
// question, so a test can assert the consent rule (install must not default
// to yes).
type recordingPrompter struct {
	defaults []bool
	answers  []bool
	i        int
}

func (r *recordingPrompter) Confirm(_ string, defaultYes bool) (bool, error) {
	r.defaults = append(r.defaults, defaultYes)
	if r.i >= len(r.answers) {
		return false, nil
	}
	a := r.answers[r.i]
	r.i++
	return a, nil
}

// TestRun_InstallPromptDefaultsToNo is the consent-rule regression: bare Enter
// must NOT approve installing a persistent background service. The FIRST
// prompt (install) must be asked with defaultYes=false.
func TestRun_InstallPromptDefaultsToNo(t *testing.T) {
	rp := &recordingPrompter{answers: []bool{false, false}} // decline both
	d, _ := baseDeps(rp)
	if _, err := Run(d); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(rp.defaults) == 0 {
		t.Fatal("no prompts were issued")
	}
	if rp.defaults[0] {
		t.Error("the service-install prompt defaulted to YES; consent rule requires an explicit y (defaultYes=false)")
	}
}
