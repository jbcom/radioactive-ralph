package service

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jbcom/radioactive-ralph/internal/variant"
)

func TestDetectBackendKnownPlatforms(t *testing.T) {
	b := DetectBackend()
	// On the dev laptop and CI, this is always darwin or linux.
	if b != BackendLaunchd && b != BackendSystemdUser && b != BackendUnsupported {
		t.Errorf("unexpected backend: %q", b)
	}
}

func TestUnitNameFormat(t *testing.T) {
	if got := UnitName(BackendLaunchd, "green"); got != "jbcom.radioactive-ralph.green" {
		t.Errorf("launchd UnitName(green) = %q", got)
	}
	if got := UnitName(BackendSystemdUser, "green"); got != "radioactive_ralph-green" {
		t.Errorf("systemd UnitName(green) = %q", got)
	}
}

func TestUnitPathLaunchd(t *testing.T) {
	got := UnitPath(BackendLaunchd, "/tmp/home", "green")
	want := "/tmp/home/Library/LaunchAgents/jbcom.radioactive-ralph.green.plist"
	if got != want {
		t.Errorf("UnitPath = %q, want %q", got, want)
	}
}

func TestUnitPathSystemd(t *testing.T) {
	got := UnitPath(BackendSystemdUser, "/tmp/home", "green")
	want := "/tmp/home/.config/systemd/user/radioactive_ralph-green.service"
	if got != want {
		t.Errorf("UnitPath = %q, want %q", got, want)
	}
}

// ── Install success paths --------------------------------------------

func TestInstallLaunchdWritesPlist(t *testing.T) {
	home := t.TempDir()
	p, _ := variant.Lookup("green")
	path, err := Install(InstallOptions{
		Backend:  BackendLaunchd,
		HomeDir:  home,
		RalphBin: "/usr/local/bin/radioactive_ralph",
		RepoPath: "/Users/me/src/repo",
		Variant:  p,
	})
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	raw, err := os.ReadFile(path) //nolint:gosec // test-controlled path
	if err != nil {
		t.Fatalf("read plist: %v", err)
	}
	content := string(raw)
	// Sanity-check the essential plist keys.
	for _, needle := range []string{
		`<key>Label</key>`, `jbcom.radioactive-ralph.green`,
		`<key>ProgramArguments</key>`, `/usr/local/bin/radioactive_ralph`,
		`<key>WorkingDirectory</key>`, `/Users/me/src/repo`,
		`<key>KeepAlive</key>`, `<true/>`,
		`LAUNCHED_BY`, `launchd`,
	} {
		if !strings.Contains(content, needle) {
			t.Errorf("plist missing %q\n%s", needle, content)
		}
	}
}

func TestInstallSystemdWritesUnit(t *testing.T) {
	home := t.TempDir()
	p, _ := variant.Lookup("green")
	path, err := Install(InstallOptions{
		Backend:  BackendSystemdUser,
		HomeDir:  home,
		RalphBin: "/usr/local/bin/radioactive_ralph",
		RepoPath: "/home/me/repo",
		Variant:  p,
	})
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	raw, err := os.ReadFile(path) //nolint:gosec // test-controlled path
	if err != nil {
		t.Fatalf("read service: %v", err)
	}
	content := string(raw)
	for _, needle := range []string{
		"[Unit]", "Description=radioactive-ralph supervisor (green)",
		"[Service]", "Type=simple",
		"ExecStart=/usr/local/bin/radioactive_ralph run --variant green --foreground",
		"WorkingDirectory=/home/me/repo",
		"Restart=on-failure",
		"[Install]", "WantedBy=default.target",
	} {
		if !strings.Contains(content, needle) {
			t.Errorf("systemd unit missing %q\n%s", needle, content)
		}
	}
}

// ── Refusal paths ----------------------------------------------------

func TestInstallRefusesServiceContextVariants(t *testing.T) {
	for _, name := range []string{"savage", "old-man", "world-breaker"} {
		t.Run(name, func(t *testing.T) {
			p, _ := variant.Lookup(name)
			_, err := Install(InstallOptions{
				Backend:       BackendLaunchd,
				HomeDir:       t.TempDir(),
				RalphBin:      "/bin/radioactive_ralph",
				Variant:       p,
				GateConfirmed: true, // pass gate to isolate the service-refusal check
			})
			if !errors.Is(err, ErrRefuseServiceContext) {
				t.Errorf("%s: expected ErrRefuseServiceContext, got %v", name, err)
			}
		})
	}
}

func TestInstallRequiresGateConfirmation(t *testing.T) {
	// Build a fake gated variant that is NOT service-refusing so we
	// isolate the gate check.
	prof := variant.Profile{
		Name:                 "gated-test",
		Description:          "test",
		Isolation:            variant.IsolationMirrorSingle,
		MaxParallelWorktrees: 1,
		Models:               map[variant.Stage]variant.Model{variant.StageExecute: variant.ModelSonnet},
		ToolAllowlist:        []string{variant.ToolBash, variant.ToolRead},
		Termination:          variant.TerminationSinglePass,
		ConfirmationGate:     "--confirm-test",
	}
	// Validate first to ensure the synthetic profile is sane.
	if err := prof.Validate(); err != nil {
		t.Fatalf("synthetic profile invalid: %v", err)
	}

	_, err := Install(InstallOptions{
		Backend:  BackendLaunchd,
		HomeDir:  t.TempDir(),
		RalphBin: "/bin/radioactive_ralph",
		Variant:  prof,
	})
	if !errors.Is(err, ErrGateNotConfirmed) {
		t.Errorf("expected ErrGateNotConfirmed, got %v", err)
	}

	if _, err := Install(InstallOptions{
		Backend:       BackendLaunchd,
		HomeDir:       t.TempDir(),
		RalphBin:      "/bin/radioactive_ralph",
		Variant:       prof,
		GateConfirmed: true,
	}); err != nil {
		t.Errorf("with GateConfirmed=true, expected success, got %v", err)
	}
}

func TestInstallMissingRalphBin(t *testing.T) {
	p, _ := variant.Lookup("green")
	_, err := Install(InstallOptions{
		Backend: BackendLaunchd,
		HomeDir: t.TempDir(),
		Variant: p,
	})
	if !errors.Is(err, ErrMissingRalphBin) {
		t.Errorf("expected ErrMissingRalphBin, got %v", err)
	}
}

// ── Uninstall --------------------------------------------------------

func TestUninstallRemovesUnit(t *testing.T) {
	home := t.TempDir()
	p, _ := variant.Lookup("green")
	path, err := Install(InstallOptions{
		Backend:  BackendLaunchd,
		HomeDir:  home,
		RalphBin: "/bin/radioactive_ralph",
		Variant:  p,
	})
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("plist missing: %v", err)
	}
	if err := Uninstall(InstallOptions{
		Backend: BackendLaunchd,
		HomeDir: home,
		Variant: p,
	}); err != nil {
		t.Errorf("Uninstall: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("plist still present: %v", err)
	}
}

func TestUninstallMissingIsNoOp(t *testing.T) {
	p, _ := variant.Lookup("green")
	err := Uninstall(InstallOptions{
		Backend: BackendLaunchd,
		HomeDir: t.TempDir(), // empty — unit was never installed
		Variant: p,
	})
	if err != nil {
		t.Errorf("Uninstall(missing) should be no-op, got %v", err)
	}
}

// ── Service-context detection ----------------------------------------

func TestIsServiceContextDetectsEnvVars(t *testing.T) {
	cases := map[string]string{
		"RALPH_SERVICE_CONTEXT": "1",
		"LAUNCHED_BY":           "launchd",
		"INVOCATION_ID":         "abcdef-0123",
	}
	for k, v := range cases {
		t.Run(k, func(t *testing.T) {
			t.Setenv(k, v)
			if !IsServiceContext() {
				t.Errorf("expected true with %s=%q", k, v)
			}
		})
	}
}

func TestIsServiceContextDefaultsFalse(t *testing.T) {
	// Clear all three markers.
	t.Setenv("RALPH_SERVICE_CONTEXT", "")
	t.Setenv("LAUNCHED_BY", "")
	t.Setenv("INVOCATION_ID", "")
	if IsServiceContext() {
		t.Error("expected false when no service markers are set")
	}
}

// ── Plist renderer escapes correctly ---------------------------------

func TestLaunchdRendererEscapesXML(t *testing.T) {
	p, _ := variant.Lookup("green")
	out, err := Install(InstallOptions{
		Backend:  BackendLaunchd,
		HomeDir:  t.TempDir(),
		RalphBin: "/path/with <angle> & ampersand/radioactive_ralph",
		Variant:  p,
	})
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	raw, _ := os.ReadFile(out) //nolint:gosec // test-controlled path
	if strings.Contains(string(raw), "<angle>") {
		t.Errorf("plist leaked literal <angle>: %s", raw)
	}
	if !strings.Contains(string(raw), "&amp;") {
		t.Errorf("plist did not escape ampersand: %s", raw)
	}
}

// ── Stable env ordering ----------------------------------------------

func TestSystemdEnvOrderIsStable(t *testing.T) {
	p, _ := variant.Lookup("green")
	extras := map[string]string{"B": "2", "A": "1", "C": "3"}

	var paths []string
	for range 2 {
		home := t.TempDir()
		out, err := Install(InstallOptions{
			Backend:  BackendSystemdUser,
			HomeDir:  home,
			RalphBin: "/bin/radioactive_ralph",
			Variant:  p,
			ExtraEnv: extras,
		})
		if err != nil {
			t.Fatalf("Install: %v", err)
		}
		paths = append(paths, out)
	}
	a, _ := os.ReadFile(paths[0]) //nolint:gosec
	b, _ := os.ReadFile(paths[1]) //nolint:gosec
	if string(a) != string(b) {
		t.Errorf("systemd unit not stable across runs:\n--- a ---\n%s\n--- b ---\n%s", a, b)
	}
	// Sanity — A should appear before B in alphabetical order.
	content := string(a)
	idxA := strings.Index(content, "Environment=A=1")
	idxB := strings.Index(content, "Environment=B=2")
	if idxA < 0 || idxB < 0 || idxA > idxB {
		t.Errorf("env not sorted: A@%d B@%d\n%s", idxA, idxB, content)
	}
	_ = filepath.Base // silence import
}
