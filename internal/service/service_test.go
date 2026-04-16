package service

import (
	"encoding/json"
	"os"
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
	repo := "/Users/me/src/radioactive-ralph"
	if got := UnitName(BackendLaunchd, repo); !strings.HasPrefix(got, "jbcom.radioactive-ralph.radioactive-ralph.") {
		t.Errorf("launchd UnitName = %q", got)
	}
	if got := UnitName(BackendSystemdUser, repo); !strings.HasPrefix(got, "radioactive_ralph-radioactive-ralph-") {
		t.Errorf("systemd UnitName = %q", got)
	}
}

func TestUnitPathLaunchd(t *testing.T) {
	got := UnitPath(BackendLaunchd, "/tmp/home", "/tmp/repo")
	if !strings.HasPrefix(got, "/tmp/home/Library/LaunchAgents/jbcom.radioactive-ralph.") || !strings.HasSuffix(got, ".plist") {
		t.Errorf("UnitPath = %q", got)
	}
}

func TestUnitPathSystemd(t *testing.T) {
	got := UnitPath(BackendSystemdUser, "/tmp/home", "/tmp/repo")
	if !strings.HasPrefix(got, "/tmp/home/.config/systemd/user/radioactive_ralph-") || !strings.HasSuffix(got, ".service") {
		t.Errorf("UnitPath = %q", got)
	}
}

func TestUnitPathWindows(t *testing.T) {
	got := UnitPath(BackendWindowsSCM, `C:\Users\me`, `C:\src\repo`)
	normalized := strings.ReplaceAll(got, `\`, `/`)
	if !strings.Contains(normalized, `AppData/Local/radioactive-ralph/services/radioactive_ralph-`) || !strings.HasSuffix(normalized, ".json") {
		t.Errorf("UnitPath = %q", got)
	}
}

func TestMarshalWindowsServiceConfigRoundTrip(t *testing.T) {
	opts := InstallOptions{
		RepoPath: `C:\src\repo`,
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
	if got := onDisk["repo_path"]; got != opts.RepoPath {
		t.Fatalf("repo_path = %#v, want %q", got, opts.RepoPath)
	}
	cfg, err := ParseWindowsServiceConfig(raw)
	if err != nil {
		t.Fatalf("ParseWindowsServiceConfig: %v", err)
	}
	if cfg.RepoPath != opts.RepoPath {
		t.Fatalf("RepoPath = %q, want %q", cfg.RepoPath, opts.RepoPath)
	}
	if !reflect.DeepEqual(cfg.ExtraEnv, opts.ExtraEnv) {
		t.Fatalf("ExtraEnv = %#v, want %#v", cfg.ExtraEnv, opts.ExtraEnv)
	}
}

func TestBuildWindowsServiceConfigClonesEnv(t *testing.T) {
	opts := InstallOptions{
		RepoPath: `C:\src\repo`,
		ExtraEnv: map[string]string{"FOO": "bar"},
	}
	cfg := BuildWindowsServiceConfig(opts)
	opts.ExtraEnv["FOO"] = "mutated"
	if cfg.ExtraEnv["FOO"] != "bar" {
		t.Fatalf("BuildWindowsServiceConfig did not clone ExtraEnv: %#v", cfg.ExtraEnv)
	}
}

func TestWindowsServiceArgs(t *testing.T) {
	repo := `C:\src\repo`
	args := WindowsServiceArgs(repo, "", `C:\svc\repo.json`)
	wantPrefix := []string{
		"service",
		"run-windows",
		"--repo-root", repo,
		"--service-name", UnitName(BackendWindowsSCM, repo),
		"--config-path", `C:\svc\repo.json`,
	}
	if !reflect.DeepEqual(args, wantPrefix) {
		t.Fatalf("WindowsServiceArgs() = %#v, want %#v", args, wantPrefix)
	}
}

func TestInstallLaunchdWritesPlist(t *testing.T) {
	home := t.TempDir()
	repo := "/Users/me/src/repo"
	path, err := Install(InstallOptions{
		Backend:  BackendLaunchd,
		HomeDir:  home,
		RalphBin: "/usr/local/bin/radioactive_ralph",
		RepoPath: repo,
	})
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	raw, err := os.ReadFile(path) //nolint:gosec // test-controlled path
	if err != nil {
		t.Fatalf("read plist: %v", err)
	}
	content := string(raw)
	for _, needle := range []string{
		`<key>Label</key>`, `jbcom.radioactive-ralph.`,
		`<key>ProgramArguments</key>`, `/usr/local/bin/radioactive_ralph`,
		`<string>service</string>`,
		`<string>start</string>`,
		`<string>--foreground</string>`,
		`<string>--repo-root</string>`,
		repo,
		`<key>WorkingDirectory</key>`,
		`<key>KeepAlive</key>`,
		`LAUNCHED_BY`, `launchd`,
	} {
		if !strings.Contains(content, needle) {
			t.Errorf("plist missing %q\n%s", needle, content)
		}
	}
}

func TestInstallSystemdWritesUnit(t *testing.T) {
	home := t.TempDir()
	repo := "/home/me/repo"
	path, err := Install(InstallOptions{
		Backend:  BackendSystemdUser,
		HomeDir:  home,
		RalphBin: "/usr/local/bin/radioactive_ralph",
		RepoPath: repo,
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
		"[Unit]", "Description=radioactive-ralph durable runtime",
		"[Service]", "Type=simple",
		"ExecStart=/usr/local/bin/radioactive_ralph service start --foreground --repo-root /home/me/repo",
		"WorkingDirectory=/home/me/repo",
		"Restart=on-failure",
		"[Install]", "WantedBy=default.target",
	} {
		if !strings.Contains(content, needle) {
			t.Errorf("systemd unit missing %q\n%s", needle, content)
		}
	}
}

func TestInstallMissingFields(t *testing.T) {
	if _, err := Install(InstallOptions{RepoPath: "/tmp/repo"}); err != ErrMissingRalphBin {
		t.Errorf("expected ErrMissingRalphBin, got %v", err)
	}
	if _, err := Install(InstallOptions{RalphBin: "/bin/radioactive_ralph"}); err != ErrMissingRepoPath {
		t.Errorf("expected ErrMissingRepoPath, got %v", err)
	}
}

func TestUninstallRemovesUnit(t *testing.T) {
	home := t.TempDir()
	repo := "/Users/me/src/repo"
	path, err := Install(InstallOptions{
		Backend:  BackendLaunchd,
		HomeDir:  home,
		RalphBin: "/bin/radioactive_ralph",
		RepoPath: repo,
	})
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("plist missing: %v", err)
	}
	if err := Uninstall(InstallOptions{
		Backend:  BackendLaunchd,
		HomeDir:  home,
		RepoPath: repo,
	}); err != nil {
		t.Errorf("Uninstall: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("plist still present: %v", err)
	}
}

func TestUninstallMissingIsNoOp(t *testing.T) {
	err := Uninstall(InstallOptions{
		Backend:  BackendLaunchd,
		HomeDir:  t.TempDir(),
		RepoPath: "/tmp/repo",
	})
	if err != nil {
		t.Errorf("Uninstall(missing) should be no-op, got %v", err)
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
