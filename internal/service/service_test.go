package service

import (
	"errors"
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDetectBackendKnownPlatforms(t *testing.T) {
	b := DetectBackend()
	if b != BackendLaunchd && b != BackendSystemdUser && b != BackendWindowsSCM && b != BackendUnsupported {
		t.Errorf("unexpected backend: %q", b)
	}
}

func TestUnitNameFormat(t *testing.T) {
	if got := UnitName(BackendLaunchd); got != "jbcom.radioactive-ralph.supervisor" {
		t.Errorf("launchd UnitName = %q", got)
	}
	if got := UnitName(BackendSystemdUser); got != "radioactive_ralph-supervisor" {
		t.Errorf("systemd UnitName = %q", got)
	}
	if got := UnitName(BackendWindowsSCM); got != "radioactive_ralph-supervisor" {
		t.Errorf("windows UnitName = %q", got)
	}
}

func TestUnitPathLaunchd(t *testing.T) {
	got := UnitPath(BackendLaunchd, "/tmp/home")
	if got != "/tmp/home/Library/LaunchAgents/jbcom.radioactive-ralph.supervisor.plist" {
		t.Errorf("UnitPath = %q", got)
	}
}

func TestUnitPathSystemd(t *testing.T) {
	got := UnitPath(BackendSystemdUser, "/tmp/home")
	if got != "/tmp/home/.config/systemd/user/radioactive_ralph-supervisor.service" {
		t.Errorf("UnitPath = %q", got)
	}
}

func TestUnitPathWindows(t *testing.T) {
	got := UnitPath(BackendWindowsSCM, `C:\Users\me`)
	normalized := strings.ReplaceAll(got, `\`, `/`)
	if !strings.Contains(normalized, `AppData/Local/radioactive-ralph/services/radioactive_ralph-supervisor`) || !strings.HasSuffix(normalized, ".json") {
		t.Errorf("UnitPath = %q", got)
	}
}

func TestUnitPathUnsupported(t *testing.T) {
	if got := UnitPath(BackendUnsupported, "/tmp/home"); got != "" {
		t.Errorf("UnitPath(unsupported) = %q, want empty", got)
	}
}

func TestInstallLaunchdWritesPlist(t *testing.T) {
	home := t.TempDir()
	plistPath, err := Install(InstallOptions{
		Backend:  BackendLaunchd,
		HomeDir:  home,
		RalphBin: "/usr/local/bin/radioactive_ralph",
	})
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	raw, err := os.ReadFile(plistPath) //nolint:gosec // test-controlled path
	if err != nil {
		t.Fatalf("read plist: %v", err)
	}
	content := string(raw)
	// The plist is a macOS artifact and always uses forward-slash paths
	// (launchd's XML), so match with path.Join, not filepath.Join — the
	// latter would use backslashes when this test runs on a Windows host and
	// spuriously fail to find the (correct, slash-separated) log path.
	wantLogPath := path.Join(home, "Library", "Logs", "radioactive-ralph", "supervisor.log")
	for _, needle := range []string{
		`<key>Label</key>`, `jbcom.radioactive-ralph.supervisor`,
		`<key>ProgramArguments</key>`, `/usr/local/bin/radioactive_ralph`,
		`<string>--supervisor</string>`,
		`<key>KeepAlive</key>`,
		`LAUNCHED_BY`, `launchd`,
		wantLogPath,
	} {
		if !strings.Contains(content, needle) {
			t.Errorf("plist missing %q\n%s", needle, content)
		}
	}
	// The old per-repo args must NOT be present, and the log path must be
	// a resolved absolute path — a literal "${HOME}/..." string here
	// (launchd does not expand it) previously made launchd refuse to
	// spawn the job at all (EX_CONFIG/78) with no log ever written.
	for _, absent := range []string{`<string>service</string>`, `<string>start</string>`, `--repo-root`, `${HOME}`} {
		if strings.Contains(content, absent) {
			t.Errorf("plist unexpectedly contains %q\n%s", absent, content)
		}
	}
	logDir := filepath.Join(home, "Library", "Logs", "radioactive-ralph")
	if info, err := os.Stat(logDir); err != nil || !info.IsDir() {
		t.Errorf("Install did not create the launchd log dir %s: %v", logDir, err)
	}
}

func TestInstallSystemdWritesUnit(t *testing.T) {
	home := t.TempDir()
	path, err := Install(InstallOptions{
		Backend:  BackendSystemdUser,
		HomeDir:  home,
		RalphBin: "/usr/local/bin/radioactive_ralph",
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
		"[Unit]", "Description=radioactive-ralph durable supervisor",
		"[Service]", "Type=simple",
		`ExecStart="/usr/local/bin/radioactive_ralph" --supervisor`,
		"Restart=on-failure",
		"[Install]", "WantedBy=default.target",
	} {
		if !strings.Contains(content, needle) {
			t.Errorf("systemd unit missing %q\n%s", needle, content)
		}
	}
	if strings.Contains(content, "--repo-root") || strings.Contains(content, "WorkingDirectory=") {
		t.Errorf("systemd unit unexpectedly contains stale per-repo directives\n%s", content)
	}
}

func TestInstallSystemdQuotesExecutableAndEnvironment(t *testing.T) {
	home := t.TempDir()
	unitPath, err := Install(InstallOptions{
		Backend:  BackendSystemdUser,
		HomeDir:  home,
		RalphBin: "/opt/Ralph Studio/bin/radioactive_ralph",
		ExtraEnv: map[string]string{"RALPH_PATH": `/opt/Provider Tools/100%/bin`},
	})
	if err != nil {
		t.Fatalf("Install(systemd quoted): %v", err)
	}
	raw, err := os.ReadFile(unitPath) //nolint:gosec // test-controlled path
	if err != nil {
		t.Fatalf("read unit: %v", err)
	}
	content := string(raw)
	for _, want := range []string{
		`ExecStart="/opt/Ralph Studio/bin/radioactive_ralph" --supervisor`,
		`Environment="RALPH_PATH=/opt/Provider Tools/100%%/bin"`,
	} {
		if !strings.Contains(content, want) {
			t.Errorf("systemd unit missing %q:\n%s", want, content)
		}
	}
}

func TestInstallMissingFields(t *testing.T) {
	if _, err := Install(InstallOptions{Backend: BackendLaunchd}); err != ErrMissingRalphBin {
		t.Errorf("expected ErrMissingRalphBin, got %v", err)
	}
}

func TestWindowsSCMDisabledErrorIsStableAndActionable(t *testing.T) {
	home := filepath.Join(t.TempDir(), "must-not-be-created")
	_, err := Install(InstallOptions{
		Backend: BackendWindowsSCM,
		HomeDir: home,
		// The disabled backend must win before validation of the unsafe
		// definition, including a missing executable and malformed environment.
		ExtraEnv: map[string]string{"INVALID NAME": "line one\nline two"},
	})
	if !errors.Is(err, ErrWindowsSCMDisabled) {
		t.Fatalf("Install(windows-scm) error = %v, want ErrWindowsSCMDisabled", err)
	}
	var typed *WindowsSCMDisabledError
	if !errors.As(err, &typed) {
		t.Fatalf("Install(windows-scm) error type = %T, want *WindowsSCMDisabledError", err)
	}
	if typed.Operation != WindowsSCMOperationInstall {
		t.Fatalf("Install operation = %q, want %q", typed.Operation, WindowsSCMOperationInstall)
	}
	for _, clause := range []string{
		"native Windows SCM service installation is disabled",
		"radioactive_ralph --supervisor",
		"WSL2",
		"systemd --user",
	} {
		if !strings.Contains(err.Error(), clause) {
			t.Fatalf("Install(windows-scm) error %q missing %q", err, clause)
		}
	}
	if _, statErr := os.Stat(home); !os.IsNotExist(statErr) {
		t.Fatalf("Install(windows-scm) mutated %s: %v", home, statErr)
	}

	err = Start(InstallOptions{Backend: BackendWindowsSCM, HomeDir: home})
	if !errors.Is(err, ErrWindowsSCMDisabled) {
		t.Fatalf("Start(windows-scm) error = %v, want ErrWindowsSCMDisabled", err)
	}
	if !errors.As(err, &typed) || typed.Operation != WindowsSCMOperationStart {
		t.Fatalf("Start(windows-scm) typed error = %#v (%v)", typed, err)
	}
}

func TestWindowsSCMDeletionPendingErrorIsStableAndActionable(t *testing.T) {
	err := &WindowsSCMDeletionPendingError{
		ServiceName: UnitName(BackendWindowsSCM),
		Operation:   "uninstall",
	}
	if !errors.Is(err, ErrWindowsSCMDeletionPending) {
		t.Fatalf("error = %v, want ErrWindowsSCMDeletionPending", err)
	}
	var typed *WindowsSCMDeletionPendingError
	if !errors.As(err, &typed) {
		t.Fatalf("error type = %T, want *WindowsSCMDeletionPendingError", err)
	}
	if typed.ServiceName != UnitName(BackendWindowsSCM) || typed.Operation != "uninstall" {
		t.Fatalf("typed error = %#v", typed)
	}
	for _, clause := range []string{
		"marked for deletion",
		"process and registration may still exist",
		"wait for SCM deletion to finish",
		"reboot Windows",
		"retry",
	} {
		if !strings.Contains(err.Error(), clause) {
			t.Fatalf("deletion-pending error %q missing %q", err, clause)
		}
	}
}

func TestInstallRejectsAmbiguousExecutableAndEnvironment(t *testing.T) {
	tests := []struct {
		name string
		opts InstallOptions
	}{
		{
			name: "relative executable",
			opts: InstallOptions{Backend: BackendLaunchd, HomeDir: t.TempDir(), RalphBin: "radioactive_ralph"},
		},
		{
			name: "invalid environment name",
			opts: InstallOptions{
				Backend: BackendLaunchd, HomeDir: t.TempDir(), RalphBin: "/bin/radioactive_ralph",
				ExtraEnv: map[string]string{"NOT VALID": "value"},
			},
		},
		{
			name: "environment newline",
			opts: InstallOptions{
				Backend: BackendLaunchd, HomeDir: t.TempDir(), RalphBin: "/bin/radioactive_ralph",
				ExtraEnv: map[string]string{"VALID": "first\nsecond"},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := Install(tt.opts); err == nil {
				t.Fatal("Install accepted an ambiguous service definition")
			}
		})
	}
}

func TestAbsoluteServicePathUsesTargetBackendGrammar(t *testing.T) {
	tests := []struct {
		name    string
		backend Backend
		value   string
		want    bool
	}{
		{name: "launchd POSIX", backend: BackendLaunchd, value: "/usr/local/bin/radioactive_ralph", want: true},
		{name: "systemd POSIX", backend: BackendSystemdUser, value: "/opt/Ralph Studio/bin/radioactive_ralph", want: true},
		{name: "POSIX rejects Windows", backend: BackendLaunchd, value: `C:\radioactive_ralph.exe`, want: false},
		{name: "relative", backend: BackendSystemdUser, value: "radioactive_ralph", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isAbsoluteServicePath(tt.backend, tt.value); got != tt.want {
				t.Fatalf("isAbsoluteServicePath(%q, %q) = %t, want %t", tt.backend, tt.value, got, tt.want)
			}
		})
	}
}

func TestUninstallRemovesUnit(t *testing.T) {
	home := t.TempDir()
	path, err := Install(InstallOptions{
		Backend:  BackendLaunchd,
		HomeDir:  home,
		RalphBin: "/bin/radioactive_ralph",
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
	}); err != nil {
		t.Errorf("Uninstall: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("plist still present: %v", err)
	}
}

func TestUninstallMissingIsNoOp(t *testing.T) {
	err := Uninstall(InstallOptions{
		Backend: BackendLaunchd,
		HomeDir: t.TempDir(),
	})
	if err != nil {
		t.Errorf("Uninstall(missing) should be no-op, got %v", err)
	}
}

func TestUninstallUnsupportedBackend(t *testing.T) {
	err := Uninstall(InstallOptions{Backend: BackendUnsupported})
	if err == nil {
		t.Fatal("expected error for unsupported backend")
	}
}

func TestInstallUnsupportedBackend(t *testing.T) {
	_, err := Install(InstallOptions{RalphBin: "/bin/radioactive_ralph", Backend: BackendUnsupported})
	if err == nil {
		t.Fatal("expected error for unsupported backend")
	}
}

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

func TestIsServiceContextFalseWhenUnset(t *testing.T) {
	t.Setenv("RALPH_SERVICE_CONTEXT", "")
	t.Setenv("LAUNCHED_BY", "")
	t.Setenv("INVOCATION_ID", "")
	if IsServiceContext() {
		t.Error("expected false with no service-context env vars set")
	}
}

func TestInspectReportsNotInstalled(t *testing.T) {
	home := t.TempDir()
	status, err := Inspect(InstallOptions{Backend: BackendLaunchd, HomeDir: home})
	if err != nil {
		t.Fatalf("Inspect: %v", err)
	}
	if status.Installed {
		t.Error("expected Installed=false before Install")
	}
	if status.Backend != BackendLaunchd {
		t.Errorf("Backend = %q, want launchd", status.Backend)
	}
}

func TestInspectReportsInstalled(t *testing.T) {
	home := t.TempDir()
	path, err := Install(InstallOptions{
		Backend:  BackendLaunchd,
		HomeDir:  home,
		RalphBin: "/bin/radioactive_ralph",
	})
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	status, err := Inspect(InstallOptions{Backend: BackendLaunchd, HomeDir: home})
	if err != nil {
		t.Fatalf("Inspect: %v", err)
	}
	if !status.Installed {
		t.Error("expected Installed=true after Install")
	}
	if status.UnitPath != path {
		t.Errorf("UnitPath = %q, want %q", status.UnitPath, path)
	}
}

func TestInspectUnsupportedBackend(t *testing.T) {
	if _, err := Inspect(InstallOptions{Backend: BackendUnsupported}); err == nil {
		t.Fatal("expected error for unsupported backend")
	}
}

// TestStartLaunchdRunsBootstrap verifies Start dispatches the launchd
// load/start commands (via the stubbable execCommand) without needing a real
// launchd. Covers the gap CodeRabbit flagged: Install writes the unit but
// Start is what actually brings the supervisor up.
func TestStartLaunchdRunsBootstrap(t *testing.T) {
	var calls [][]string
	orig := execCommand
	execCommand = func(name string, args ...string) (string, error) {
		calls = append(calls, append([]string{name}, args...))
		return "", nil
	}
	t.Cleanup(func() { execCommand = orig })

	if err := Start(InstallOptions{Backend: BackendLaunchd, HomeDir: t.TempDir()}); err != nil {
		t.Fatalf("Start(launchd): %v", err)
	}
	if len(calls) != 2 {
		t.Fatalf("launchd start calls = %v, want bootout/bootstrap", calls)
	}
	if calls[0][0] != "launchctl" || calls[0][1] != "bootout" ||
		calls[1][1] != "bootstrap" {
		t.Fatalf("expected launchctl bootout/bootstrap, got %v", calls)
	}
}

// TestStartLaunchdRetriesAsynchronousBootout proves the live-macOS failure
// mode: bootstrap may transiently return launchd exit 5/EIO after bootout.
// Start owns that bounded transition and succeeds without asking the operator
// to rerun the install command.
func TestStartLaunchdRetriesAsynchronousBootout(t *testing.T) {
	var calls [][]string
	bootstrapCalls := 0
	origExec := execCommand
	origSleep := serviceRetrySleep
	execCommand = func(name string, args ...string) (string, error) {
		calls = append(calls, append([]string{name}, args...))
		if len(args) > 0 && args[0] == "bootstrap" {
			bootstrapCalls++
			if bootstrapCalls == 1 {
				return "Bootstrap failed: 5: Input/output error", errors.New("exit status 5")
			}
		}
		return "", nil
	}
	serviceRetrySleep = func(time.Duration) {}
	t.Cleanup(func() {
		execCommand = origExec
		serviceRetrySleep = origSleep
	})

	if err := Start(InstallOptions{Backend: BackendLaunchd, HomeDir: t.TempDir()}); err != nil {
		t.Fatalf("Start(launchd retry): %v", err)
	}
	if bootstrapCalls != 2 {
		t.Fatalf("bootstrap calls = %d, want 2; all calls = %v", bootstrapCalls, calls)
	}
}

// TestStartSystemdReconcilesOnce verifies the systemd path reloads the
// definition, enables it for login, and performs one restart that works for
// both first install and changed-definition reinstall.
func TestStartSystemdReconcilesOnce(t *testing.T) {
	var calls [][]string
	orig := execCommand
	execCommand = func(name string, args ...string) (string, error) {
		calls = append(calls, append([]string{name}, args...))
		return "", nil
	}
	t.Cleanup(func() { execCommand = orig })

	if err := Start(InstallOptions{Backend: BackendSystemdUser, HomeDir: t.TempDir()}); err != nil {
		t.Fatalf("Start(systemd): %v", err)
	}
	joined := ""
	for _, c := range calls {
		joined += strings.Join(c, " ") + "\n"
	}
	if !strings.Contains(joined, "systemctl --user daemon-reload") ||
		!strings.Contains(joined, "systemctl --user enable radioactive_ralph-supervisor") ||
		!strings.Contains(joined, "systemctl --user restart") {
		t.Errorf("expected daemon-reload + enable + restart, got:\n%s", joined)
	}
	if strings.Contains(joined, "enable --now") {
		t.Errorf("systemd reconciliation starts twice via enable --now + restart:\n%s", joined)
	}
}

func TestStopLaunchdBootsOutLoadedService(t *testing.T) {
	var calls [][]string
	orig := execCommand
	execCommand = func(name string, args ...string) (string, error) {
		calls = append(calls, append([]string{name}, args...))
		return "", nil
	}
	t.Cleanup(func() { execCommand = orig })

	if err := Stop(InstallOptions{Backend: BackendLaunchd}); err != nil {
		t.Fatalf("Stop(launchd): %v", err)
	}
	if len(calls) != 2 || calls[0][1] != "print" || calls[1][1] != "bootout" {
		t.Fatalf("launchd stop calls = %v, want print then bootout", calls)
	}
}

func TestStopLaunchdIsIdempotentWhenNotLoaded(t *testing.T) {
	var calls [][]string
	orig := execCommand
	execCommand = func(name string, args ...string) (string, error) {
		calls = append(calls, append([]string{name}, args...))
		return "not found", errors.New("exit status 113")
	}
	t.Cleanup(func() { execCommand = orig })

	if err := Stop(InstallOptions{Backend: BackendLaunchd}); err != nil {
		t.Fatalf("Stop(launchd absent): %v", err)
	}
	if len(calls) != 1 || calls[0][1] != "print" {
		t.Fatalf("launchd absent stop calls = %v, want only print", calls)
	}
}

func TestStopSystemdDisablesLoadedService(t *testing.T) {
	var calls [][]string
	orig := execCommand
	execCommand = func(name string, args ...string) (string, error) {
		calls = append(calls, append([]string{name}, args...))
		if len(args) > 1 && args[1] == "show" {
			return "loaded\n", nil
		}
		return "", nil
	}
	t.Cleanup(func() { execCommand = orig })

	if err := Stop(InstallOptions{Backend: BackendSystemdUser}); err != nil {
		t.Fatalf("Stop(systemd): %v", err)
	}
	joined := ""
	for _, call := range calls {
		joined += strings.Join(call, " ") + "\n"
	}
	if !strings.Contains(joined, "systemctl --user show") ||
		!strings.Contains(joined, "systemctl --user disable --now radioactive_ralph-supervisor") {
		t.Fatalf("systemd stop calls:\n%s", joined)
	}
}

func TestUninstallSystemdRemovesDefinitionThenReloads(t *testing.T) {
	home := t.TempDir()
	unitPath, err := Install(InstallOptions{
		Backend:  BackendSystemdUser,
		HomeDir:  home,
		RalphBin: "/usr/local/bin/radioactive_ralph",
	})
	if err != nil {
		t.Fatalf("Install(systemd): %v", err)
	}

	var calls [][]string
	orig := execCommand
	execCommand = func(name string, args ...string) (string, error) {
		calls = append(calls, append([]string{name}, args...))
		return "", nil
	}
	t.Cleanup(func() { execCommand = orig })

	if err := Uninstall(InstallOptions{Backend: BackendSystemdUser, HomeDir: home}); err != nil {
		t.Fatalf("Uninstall(systemd): %v", err)
	}
	if _, err := os.Stat(unitPath); !os.IsNotExist(err) {
		t.Fatalf("systemd unit remains after Uninstall: %v", err)
	}
	if len(calls) != 1 || strings.Join(calls[0], " ") != "systemctl --user daemon-reload" {
		t.Fatalf("systemd uninstall calls = %v, want daemon-reload after remove", calls)
	}
}
