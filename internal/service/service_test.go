package service

import (
	"encoding/json"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
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

func TestMarshalWindowsServiceConfigRoundTrip(t *testing.T) {
	opts := InstallOptions{
		ExtraEnv: map[string]string{
			"FOO": "bar",
			"BAZ": "qux",
		},
	}
	raw, err := MarshalWindowsServiceConfig(opts)
	if err != nil {
		t.Fatalf("MarshalWindowsServiceConfig: %v", err)
	}
	var onDisk map[string]any
	if err := json.Unmarshal(raw, &onDisk); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	cfg, err := ParseWindowsServiceConfig(raw)
	if err != nil {
		t.Fatalf("ParseWindowsServiceConfig: %v", err)
	}
	if !reflect.DeepEqual(cfg.ExtraEnv, opts.ExtraEnv) {
		t.Fatalf("ExtraEnv = %#v, want %#v", cfg.ExtraEnv, opts.ExtraEnv)
	}
}

func TestBuildWindowsServiceConfigClonesEnv(t *testing.T) {
	opts := InstallOptions{
		ExtraEnv: map[string]string{"FOO": "bar"},
	}
	cfg := BuildWindowsServiceConfig(opts)
	opts.ExtraEnv["FOO"] = "mutated"
	if cfg.ExtraEnv["FOO"] != "bar" {
		t.Fatalf("BuildWindowsServiceConfig did not clone ExtraEnv: %#v", cfg.ExtraEnv)
	}
}

func TestWindowsServiceArgs(t *testing.T) {
	got := WindowsServiceArgs()
	want := []string{"--supervisor"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("WindowsServiceArgs() = %#v, want %#v", got, want)
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
		"ExecStart=/usr/local/bin/radioactive_ralph --supervisor",
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

func TestInstallMissingFields(t *testing.T) {
	if _, err := Install(InstallOptions{}); err != ErrMissingRalphBin {
		t.Errorf("expected ErrMissingRalphBin, got %v", err)
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
