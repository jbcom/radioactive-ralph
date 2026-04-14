package multiplexer

import (
	"errors"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"testing"
)

// stubLookPath returns a function that reports only the named binaries
// as installed. Binaries not in the set return exec.ErrNotFound.
func stubLookPath(installed ...string) func(string) (string, error) {
	set := make(map[string]struct{}, len(installed))
	for _, name := range installed {
		set[name] = struct{}{}
	}
	return func(name string) (string, error) {
		if _, ok := set[name]; ok {
			return "/stub/bin/" + name, nil
		}
		return "", exec.ErrNotFound
	}
}

// stubGetenv returns a function that reports only the given vars as set.
func stubGetenv(vars map[string]string) func(string) string {
	return func(key string) string { return vars[key] }
}

func TestBackendString(t *testing.T) {
	cases := map[Backend]string{
		BackendTmux:    "tmux",
		BackendScreen:  "screen",
		BackendSetsid:  "setsid",
		BackendUnknown: "unknown",
		Backend(99):    "unknown",
	}
	for b, want := range cases {
		if got := b.String(); got != want {
			t.Errorf("Backend(%d).String() = %q, want %q", b, got, want)
		}
	}
}

func TestDetectPrefersTmux(t *testing.T) {
	d, err := Detect(
		WithLookPath(stubLookPath("tmux", "screen")),
		WithGetenv(stubGetenv(nil)),
	)
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if d.Backend() != BackendTmux {
		t.Errorf("Backend = %v, want tmux", d.Backend())
	}
}

func TestDetectHonorsTmuxEnvVar(t *testing.T) {
	d, err := Detect(
		WithLookPath(stubLookPath()), // nothing on PATH
		WithGetenv(stubGetenv(map[string]string{"TMUX": "/tmp/tmux-1000/default,1234,0"})),
	)
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if d.Backend() != BackendTmux {
		t.Errorf("Backend = %v, want tmux (via $TMUX)", d.Backend())
	}
}

func TestDetectFallsThroughToScreen(t *testing.T) {
	d, err := Detect(
		WithLookPath(stubLookPath("screen")), // no tmux
		WithGetenv(stubGetenv(nil)),
	)
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if d.Backend() != BackendScreen {
		t.Errorf("Backend = %v, want screen", d.Backend())
	}
}

func TestDetectFallsThroughToSetsid(t *testing.T) {
	d, err := Detect(
		WithLookPath(stubLookPath()), // nothing external
		WithGetenv(stubGetenv(nil)),
	)
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if d.Backend() != BackendSetsid {
		t.Errorf("Backend = %v, want setsid", d.Backend())
	}
}

func TestDetectPreferredBackendHonored(t *testing.T) {
	d, err := Detect(
		WithLookPath(stubLookPath("tmux", "screen")),
		WithGetenv(stubGetenv(nil)),
		WithPreferredBackend(BackendScreen),
	)
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if d.Backend() != BackendScreen {
		t.Errorf("Backend = %v, want screen (preferred)", d.Backend())
	}
}

func TestDetectPreferredBackendFailsIfUnavailable(t *testing.T) {
	_, err := Detect(
		WithLookPath(stubLookPath()), // no tmux
		WithGetenv(stubGetenv(nil)),
		WithPreferredBackend(BackendTmux),
	)
	if err == nil {
		t.Fatal("expected error when preferred backend unavailable")
	}
	if !strings.Contains(err.Error(), "tmux") {
		t.Errorf("error should mention tmux: %v", err)
	}
}

func TestDetectPreferredSetsidAlwaysAvailable(t *testing.T) {
	d, err := Detect(
		WithLookPath(stubLookPath()),
		WithPreferredBackend(BackendSetsid),
	)
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if d.Backend() != BackendSetsid {
		t.Errorf("Backend = %v, want setsid", d.Backend())
	}
}

func TestSpawnDetachedValidatesRequest(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("SpawnDetached isn't supported on windows")
	}
	d, err := Detect(WithLookPath(stubLookPath()), WithGetenv(stubGetenv(nil)))
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}

	// Name required.
	if _, err := d.SpawnDetached(SpawnRequest{
		SessionName: "s", LogPath: "/tmp/l",
	}); err == nil {
		t.Error("expected error for missing Name")
	}
	// SessionName required.
	if _, err := d.SpawnDetached(SpawnRequest{
		Name: "/bin/true", LogPath: "/tmp/l",
	}); err == nil {
		t.Error("expected error for missing SessionName")
	}
	// LogPath required.
	if _, err := d.SpawnDetached(SpawnRequest{
		Name: "/bin/true", SessionName: "s",
	}); err == nil {
		t.Error("expected error for missing LogPath")
	}
}

func TestSpawnDetachedSetsidRunsRealBinary(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("setsid not supported on windows")
	}

	// /bin/true exits 0 immediately — we're testing that SpawnDetached
	// successfully invokes it without hanging. The grandchild will exit
	// before we check, which is fine: we just want to see no error and
	// a non-zero PID returned.
	trueBin, err := exec.LookPath("true")
	if err != nil {
		t.Skipf("`true` not on PATH: %v", err)
	}

	dir := t.TempDir()
	logPath := dir + "/test.log"

	d, err := Detect(
		WithLookPath(stubLookPath()), // force setsid backend
		WithGetenv(stubGetenv(nil)),
	)
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if d.Backend() != BackendSetsid {
		t.Fatalf("expected setsid backend, got %v", d.Backend())
	}

	spawned, err := d.SpawnDetached(SpawnRequest{
		Name:        trueBin,
		SessionName: "test-session",
		LogPath:     logPath,
	})
	if err != nil {
		t.Fatalf("SpawnDetached: %v", err)
	}
	if spawned.PID == 0 {
		t.Error("expected non-zero PID")
	}
	// Log file should exist (OpenFile with O_CREATE).
	if _, err := os.Stat(logPath); err != nil {
		t.Errorf("log file should exist after spawn: %v", err)
	}
}

func TestSpawnDetachedTmuxRejectsNoRequest(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("tmux path only exists on unix build")
	}
	// Force tmux backend via preferred+stub; but don't actually run
	// tmux. We just check that the validation path fires.
	d, err := Detect(
		WithLookPath(stubLookPath("tmux")),
		WithGetenv(stubGetenv(nil)),
		WithPreferredBackend(BackendTmux),
	)
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	_, err = d.SpawnDetached(SpawnRequest{})
	if err == nil {
		t.Error("expected validation error")
	}
}

func TestDetectUnknownBackend(t *testing.T) {
	// BackendUnknown is zero value — used as "not pinned" marker.
	// Detect's probe rejects BackendUnknown as a preferred target.
	// Construct by explicit manipulation since the public ctor
	// treats zero as "no preference". Test internal probe.
	d := &Detacher{
		backend:  BackendUnknown,
		lookPath: stubLookPath(),
		getenv:   stubGetenv(nil),
	}
	if d.Backend() != BackendUnknown {
		t.Errorf("Backend() = %v, want unknown", d.Backend())
	}
}

func TestErrNoBackend(t *testing.T) {
	// setsid is always available on unix; to hit ErrNoBackend you have to
	// be on an unsupported platform (windows, plan9). On the unit test
	// host we can't really trigger it. We just verify the error value
	// exists and has a useful message.
	if !strings.Contains(ErrNoBackend.Error(), "multiplexer") {
		t.Error("ErrNoBackend message should mention multiplexer")
	}
	if !errors.Is(ErrNoBackend, ErrNoBackend) {
		t.Error("errors.Is should match itself")
	}
}
