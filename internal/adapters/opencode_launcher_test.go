//go:build !windows

package adapters

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jbcom/radioactive-ralph/internal/ipc"
)

func TestOpenCodeLauncherUnmanagedIsTransparent(t *testing.T) {
	binary := writeOpenCodeLauncherFixture(t, "#!/bin/sh\nprintf 'provider-stdout'\nprintf 'provider-stderr' >&2\nexit 17\n")
	var stdout, stderr bytes.Buffer
	code := RunOpenCodeLauncher(OpenCodeLaunchOptions{
		Binary: binary, Env: []string{"HOME=" + t.TempDir()},
		Stdout: &stdout, Stderr: &stderr,
	})
	if code != 17 || stdout.String() != "provider-stdout" || stderr.String() != "provider-stderr" {
		t.Fatalf("unmanaged launch = code:%d stdout:%q stderr:%q", code, stdout.String(), stderr.String())
	}
}

func TestOpenCodeLauncherPreservesManagedProviderFailureWithoutStop(t *testing.T) {
	server, handler, endpoint := startLauncherHookServer(t, ipc.HookEventReply{Allow: true})
	defer func() { _ = server.Stop() }()
	binary := writeOpenCodeLauncherFixture(t, "#!/bin/sh\nexit 23\n")
	code := RunOpenCodeLauncher(launcherOptions(t, binary, endpoint))
	if code != 23 {
		t.Fatalf("managed provider exit = %d, want 23", code)
	}
	if _, calls := handler.observed(); calls != 0 {
		t.Fatalf("provider failure submitted %d Stop events", calls)
	}
}

func TestOpenCodeLauncherFinalStopIsFiniteAndFailClosed(t *testing.T) {
	binary := writeOpenCodeLauncherFixture(t, "#!/bin/sh\nexit 0\n")
	for _, tc := range []struct {
		name       string
		reply      ipc.HookEventReply
		wantCode   int
		wantOutput string
	}{
		{name: "allowed", reply: ipc.HookEventReply{Allow: true}, wantCode: 0},
		{name: "pending", reply: ipc.HookEventReply{Allow: false, Reason: "verification_pending"}, wantCode: 2, wantOutput: "{\"status\":\"verification_pending\"}\n"},
		{name: "failed", reply: ipc.HookEventReply{Allow: false, Reason: "verification_failed"}, wantCode: 2, wantOutput: "{\"status\":\"verification_failed\"}\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server, handler, endpoint := startLauncherHookServer(t, tc.reply)
			defer func() { _ = server.Stop() }()
			var stdout, stderr bytes.Buffer
			opts := launcherOptions(t, binary, endpoint)
			opts.Stdout, opts.Stderr = &stdout, &stderr
			if code := runOpenCodeLauncher(opts, openCodeStopPolling{attempts: 1}); code != tc.wantCode {
				t.Fatalf("exit = %d, want %d", code, tc.wantCode)
			}
			if stdout.String() != tc.wantOutput || stderr.Len() != 0 {
				t.Fatalf("protocol = stdout:%q stderr:%q", stdout.String(), stderr.String())
			}
			got, calls := handler.observed()
			if calls != 1 || got.Event != ipc.HookEventStop ||
				got.Adapter != "opencode" || got.SessionID != "managed-session" {
				t.Fatalf("normalized Stop = calls:%d args:%+v", calls, got)
			}
		})
	}
}

func TestOpenCodeLauncherPollsStartedAndPendingUntilAcceptancePasses(t *testing.T) {
	server, handler, endpoint := startLauncherHookSequenceServer(t, []ipc.HookEventReply{
		{Allow: false, Reason: "verification_started"},
		{Allow: false, Reason: "verification_pending"},
		{Allow: true, Reason: "acceptance_passed"},
	})
	defer func() { _ = server.Stop() }()
	binary := writeOpenCodeLauncherFixture(t, "#!/bin/sh\nexit 0\n")
	opts := launcherOptions(t, binary, endpoint)
	var stdout, stderr bytes.Buffer
	opts.Stdout, opts.Stderr = &stdout, &stderr
	code := runOpenCodeLauncher(opts, openCodeStopPolling{
		interval:         time.Millisecond,
		progressInterval: time.Hour,
		attempts:         3,
	})
	if code != 0 || stdout.Len() != 0 {
		t.Fatalf("polling launch = code:%d stdout:%q", code, stdout.String())
	}
	if got := handler.callCount(); got != 3 {
		t.Fatalf("Stop calls = %d, want 3", got)
	}
	if stderr.Len() != 0 {
		t.Fatalf("short polling emitted premature progress: %q", stderr.String())
	}
}

func TestOpenCodeLauncherPendingExhaustionIsBoundedAndFailClosed(t *testing.T) {
	server, handler, endpoint := startLauncherHookServer(t, ipc.HookEventReply{
		Allow: false, Reason: "verification_pending",
	})
	defer func() { _ = server.Stop() }()
	binary := writeOpenCodeLauncherFixture(t, "#!/bin/sh\nexit 0\n")
	opts := launcherOptions(t, binary, endpoint)
	var stdout, stderr bytes.Buffer
	opts.Stdout, opts.Stderr = &stdout, &stderr
	code := runOpenCodeLauncher(opts, openCodeStopPolling{
		interval:         time.Millisecond,
		progressInterval: time.Hour,
		attempts:         3,
	})
	if code != 2 || stdout.String() != "{\"status\":\"verification_pending\"}\n" {
		t.Fatalf("exhausted polling = code:%d stdout:%q", code, stdout.String())
	}
	if _, calls := handler.observed(); calls != 3 {
		t.Fatalf("Stop calls = %d, want bounded 3", calls)
	}
	if stderr.Len() != 0 {
		t.Fatalf("short bounded polling emitted premature progress: %q", stderr.String())
	}
}

func TestOpenCodeLauncherProgressRemainsInsideShortStallLease(t *testing.T) {
	server, _, endpoint := startLauncherHookSequenceServer(t, []ipc.HookEventReply{
		{Allow: false, Reason: "verification_started"},
		{Allow: true, Reason: "acceptance_passed"},
	})
	defer func() { _ = server.Stop() }()
	binary := writeOpenCodeLauncherFixture(t, "#!/bin/sh\nexit 0\n")
	opts := launcherOptions(t, binary, endpoint)
	var stdout, stderr bytes.Buffer
	opts.Stdout, opts.Stderr = &stdout, &stderr
	code := runOpenCodeLauncher(opts, openCodeStopPolling{
		interval:         20 * time.Millisecond,
		progressInterval: 2 * time.Millisecond,
		attempts:         2,
	})
	if code != 0 || stdout.Len() != 0 {
		t.Fatalf("short-lease polling = code:%d stdout:%q", code, stdout.String())
	}
	if beats := strings.Count(stderr.String(), managedOpenCodeVerificationWait); beats < 2 {
		t.Fatalf("progress beats = %d in %q, want repeated progress inside poll sleep",
			beats, stderr.String())
	}
}

func TestOpenCodeLauncherContextCancellationDuringPollingFailsClosed(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	server, handler, endpoint := startLauncherHookSequenceServer(t, []ipc.HookEventReply{
		{Allow: false, Reason: "verification_started"},
	})
	defer func() { _ = server.Stop() }()
	binary := writeOpenCodeLauncherFixture(t, "#!/bin/sh\nexit 0\n")
	opts := launcherOptions(t, binary, endpoint)
	opts.Context = ctx
	var stdout, stderr bytes.Buffer
	opts.Stdout, opts.Stderr = &stdout, &stderr
	handler.afterFirst = cancel
	code := runOpenCodeLauncher(opts, openCodeStopPolling{
		interval: time.Hour,
		attempts: 2,
	})
	if code != 2 || stdout.String() != "{\"status\":\"supervisor_unavailable\"}\n" {
		t.Fatalf("canceled polling = code:%d stdout:%q stderr:%q",
			code, stdout.String(), stderr.String())
	}
}

func TestParseOpenCodeStatusIsStrictAndFinite(t *testing.T) {
	for _, tc := range []struct {
		name string
		raw  string
		want string
	}{
		{name: "pending", raw: `{"status":"verification_pending"}`, want: "verification_pending"},
		{name: "unknown status", raw: `{"status":"secret future reason"}`, want: "supervisor_unavailable"},
		{name: "unknown field", raw: `{"status":"verification_pending","detail":"secret"}`, want: "supervisor_unavailable"},
		{name: "trailing value", raw: `{"status":"verification_pending"}{}`, want: "supervisor_unavailable"},
		{name: "malformed", raw: `not-json`, want: "supervisor_unavailable"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := parseOpenCodeStatus([]byte(tc.raw)); got != tc.want {
				t.Fatalf("parseOpenCodeStatus(%q) = %q, want %q", tc.raw, got, tc.want)
			}
		})
	}
}

func TestOpenCodeLauncherUnavailableSupervisorIsStaticSecretBlindAndPathIndependent(t *testing.T) {
	binary := writeOpenCodeLauncherFixture(t, "#!/bin/sh\nexit 0\n")
	opts := launcherOptions(t, binary, filepath.Join(t.TempDir(), "missing.sock"))
	const secret = "ghp_launcher-secret-canary"
	opts.Env = append(opts.Env, "PATH="+t.TempDir(), "SECRET_CANARY="+secret)
	var stdout, stderr bytes.Buffer
	opts.Stdout, opts.Stderr = &stdout, &stderr
	if code := RunOpenCodeLauncher(opts); code != 2 {
		t.Fatalf("exit = %d, want fail-closed 2", code)
	}
	combined := stdout.String() + stderr.String()
	if stdout.String() != "{\"status\":\"supervisor_unavailable\"}\n" || stderr.Len() != 0 {
		t.Fatalf("unavailable protocol = stdout:%q stderr:%q", stdout.String(), stderr.String())
	}
	if strings.Contains(combined, secret) || strings.Contains(combined, opts.Binary) {
		t.Fatalf("launcher leaked protected input: %q", combined)
	}
}

func TestOpenCodeLauncherDoesNotEchoMalformedSupervisorReason(t *testing.T) {
	const secret = "malformed-supervisor-secret-canary"
	server, _, endpoint := startLauncherHookServer(t, ipc.HookEventReply{
		Allow: false, Reason: secret,
	})
	defer func() { _ = server.Stop() }()
	binary := writeOpenCodeLauncherFixture(t, "#!/bin/sh\nexit 0\n")
	opts := launcherOptions(t, binary, endpoint)
	var stdout, stderr bytes.Buffer
	opts.Stdout, opts.Stderr = &stdout, &stderr
	if code := RunOpenCodeLauncher(opts); code != 2 {
		t.Fatalf("malformed-reason exit = %d, want 2", code)
	}
	if got := stdout.String(); got != "{\"status\":\"supervisor_unavailable\"}\n" ||
		strings.Contains(got+stderr.String(), secret) {
		t.Fatalf("malformed supervisor reason escaped finite protocol: stdout:%q stderr:%q",
			got, stderr.String())
	}
}

func TestOpenCodeLauncherRejectsPartialCoordinatesBeforeProviderStart(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "launched")
	binary := writeOpenCodeLauncherFixture(t, "#!/bin/sh\n: > \"$RALPH_TEST_MARKER\"\n")
	control := exec.Command(binary) //nolint:gosec // test-owned executable fixture
	control.Env = append(os.Environ(), "RALPH_TEST_MARKER="+marker)
	if err := control.Run(); err != nil {
		t.Fatalf("control launch: %v", err)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("control launch did not create marker: %v", err)
	}
	if err := os.Remove(marker); err != nil {
		t.Fatalf("remove control marker: %v", err)
	}
	opts := launcherOptions(t, binary, "/tmp/unused.sock")
	opts.Env = []string{
		"HOME=" + t.TempDir(), ManagedSessionEnv + "=partial",
		"RALPH_TEST_MARKER=" + marker,
	}
	var stdout bytes.Buffer
	opts.Stdout = &stdout
	if code := RunOpenCodeLauncher(opts); code != 2 {
		t.Fatalf("partial-coordinate exit = %d", code)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("provider launched with partial coordinates: %v", err)
	}
}

func TestOpenCodeLauncherIsolatesOneReviewedPlugin(t *testing.T) {
	server, _, endpoint := startLauncherHookServer(t, ipc.HookEventReply{Allow: true})
	defer func() { _ = server.Stop() }()
	opts := launcherOptions(t, "", endpoint)
	script := "#!/bin/sh\n" +
		"[ \"$HOME\" = " + shellQuote(opts.Home) + " ] || exit 31\n" +
		"[ \"$XDG_CONFIG_HOME\" = " + shellQuote(opts.ConfigDir) + " ] || exit 32\n" +
		"[ \"$OPENCODE_CONFIG_DIR\" = " + shellQuote(opts.ConfigDir) + " ] || exit 33\n" +
		"[ \"$OPENCODE_DISABLE_PROJECT_CONFIG\" = 1 ] || exit 34\n" +
		"[ -z \"$OPENCODE_PURE\" ] || exit 35\n" +
		"case \"$OPENCODE_CONFIG_CONTENT\" in *opencode-plugin.js*) ;; *) exit 36;; esac\n" +
		"[ \"$XDG_DATA_HOME\" = " + shellQuote(filepath.Join(opts.Home, ".local", "share")) + " ] || exit 37\n" +
		"[ \"$XDG_CACHE_HOME\" = " + shellQuote(filepath.Join(opts.Home, ".cache")) + " ] || exit 38\n" +
		"[ \"$XDG_STATE_HOME\" = " + shellQuote(filepath.Join(opts.Home, ".local", "state")) + " ] || exit 39\n"
	opts.Binary = writeOpenCodeLauncherFixture(t, script)
	opts.Env = append(opts.Env,
		"OPENCODE_CONFIG=/secret/config",
		"OPENCODE_CONFIG_CONTENT=secret",
		"OPENCODE_CONFIG_DIR=/secret/dir",
		"OPENCODE_DISABLE_PROJECT_CONFIG=0",
		"OPENCODE_PURE=1",
	)
	if code := RunOpenCodeLauncher(opts); code != 0 {
		t.Fatalf("isolated managed launch exit = %d", code)
	}
}

func TestOpenCodeLauncherDoesNotExposeCallerStateRoots(t *testing.T) {
	server, _, endpoint := startLauncherHookServer(t, ipc.HookEventReply{Allow: true})
	defer func() { _ = server.Stop() }()
	opts := launcherOptions(t, "", endpoint)
	callerHome := environmentLookup(opts.Env)("HOME")
	canaries := []string{
		filepath.Join(callerHome, ".local", "share", "opencode", "auth.json"),
		filepath.Join(callerHome, ".cache", "opencode", "skills", "cached-skill"),
		filepath.Join(callerHome, ".local", "state", "opencode", "model.json"),
	}
	for _, path := range canaries {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatalf("mkdir caller canary: %v", err)
		}
		if err := os.WriteFile(path, []byte("secret caller state\n"), 0o600); err != nil {
			t.Fatalf("write caller canary: %v", err)
		}
	}
	opts.Env = append(opts.Env,
		"XDG_DATA_HOME="+filepath.Join(callerHome, ".local", "share"),
		"XDG_CACHE_HOME="+filepath.Join(callerHome, ".cache"),
		"XDG_STATE_HOME="+filepath.Join(callerHome, ".local", "state"),
	)
	script := "#!/bin/sh\n" +
		"case \"$HOME:$XDG_DATA_HOME:$XDG_CACHE_HOME:$XDG_STATE_HOME\" in *" +
		shellQuote(callerHome) + "*) exit 41;; esac\n"
	opts.Binary = writeOpenCodeLauncherFixture(t, script)
	if code := RunOpenCodeLauncher(opts); code != 0 {
		t.Fatalf("caller state isolation exit = %d", code)
	}
}

func launcherOptions(t *testing.T, binary, endpoint string) OpenCodeLaunchOptions {
	t.Helper()
	root := t.TempDir()
	plugin := filepath.Join(root, "opencode-plugin.js")
	if err := os.WriteFile(plugin, []byte("export const probe = true;\n"), 0o600); err != nil {
		t.Fatalf("write plugin: %v", err)
	}
	home := filepath.Join(root, "home")
	config := filepath.Join(root, "config")
	if err := os.Mkdir(home, 0o700); err != nil {
		t.Fatalf("mkdir home: %v", err)
	}
	if err := os.Mkdir(config, 0o700); err != nil {
		t.Fatalf("mkdir config: %v", err)
	}
	return OpenCodeLaunchOptions{
		Context: context.Background(), Binary: binary, Plugin: plugin,
		Home: home, ConfigDir: config,
		Env: []string{
			"HOME=" + t.TempDir(),
			"PATH=" + t.TempDir(),
			ManagedSessionEnv + "=managed-session",
			HookEndpointEnv + "=" + endpoint,
		},
		Stdin: strings.NewReader("provider-input"),
	}
}

func startLauncherHookServer(
	t *testing.T, reply ipc.HookEventReply,
) (*ipc.Server, *recordingHookHandler, string) {
	t.Helper()
	endpoint, heartbeat := ipc.ServiceEndpoint(t.TempDir())
	handler := &recordingHookHandler{reply: reply}
	server, err := ipc.NewServer(ipc.ServerOptions{
		SocketPath: endpoint, HeartbeatPath: heartbeat, Handler: handler,
	})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	if err := server.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	return server, handler, endpoint
}

type launcherHookSequenceHandler struct {
	mu         sync.Mutex
	replies    []ipc.HookEventReply
	calls      int
	afterFirst func()
}

func (h *launcherHookSequenceHandler) HandleHookEvent(
	_ context.Context, _ ipc.HookEventArgs,
) (ipc.HookEventReply, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	index := h.calls
	h.calls++
	if index == 0 && h.afterFirst != nil {
		h.afterFirst()
	}
	if index >= len(h.replies) {
		return ipc.HookEventReply{Allow: false, Reason: "verification_pending"}, nil
	}
	return h.replies[index], nil
}

func (*launcherHookSequenceHandler) HandleStatus(context.Context) (ipc.StatusReply, error) {
	return ipc.StatusReply{}, nil
}

func (*launcherHookSequenceHandler) HandleEnqueue(
	context.Context, ipc.EnqueueArgs,
) (ipc.EnqueueReply, error) {
	return ipc.EnqueueReply{}, nil
}

func (*launcherHookSequenceHandler) HandleStop(context.Context, ipc.StopArgs) error { return nil }

func (*launcherHookSequenceHandler) HandleReloadConfig(context.Context) error { return nil }

func (*launcherHookSequenceHandler) HandleAttach(
	context.Context, ipc.AttachArgs, func(json.RawMessage) error,
) error {
	return nil
}

func (h *launcherHookSequenceHandler) callCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.calls
}

func startLauncherHookSequenceServer(
	t *testing.T, replies []ipc.HookEventReply,
) (*ipc.Server, *launcherHookSequenceHandler, string) {
	t.Helper()
	endpoint, heartbeat := ipc.ServiceEndpoint(t.TempDir())
	handler := &launcherHookSequenceHandler{replies: replies}
	server, err := ipc.NewServer(ipc.ServerOptions{
		SocketPath: endpoint, HeartbeatPath: heartbeat, Handler: handler,
	})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	if err := server.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	return server, handler, endpoint
}

func writeOpenCodeLauncherFixture(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "fake-opencode")
	if err := os.WriteFile(path, []byte(body), 0o700); err != nil {
		t.Fatalf("write fake OpenCode: %v", err)
	}
	return path
}
