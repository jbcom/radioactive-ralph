//go:build !windows

package adapters

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

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
	if handler.calls != 0 {
		t.Fatalf("provider failure submitted %d Stop events", handler.calls)
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
			if code := RunOpenCodeLauncher(opts); code != tc.wantCode {
				t.Fatalf("exit = %d, want %d", code, tc.wantCode)
			}
			if stdout.String() != tc.wantOutput || stderr.Len() != 0 {
				t.Fatalf("protocol = stdout:%q stderr:%q", stdout.String(), stderr.String())
			}
			if handler.calls != 1 || handler.got.Event != ipc.HookEventStop ||
				handler.got.Adapter != "opencode" || handler.got.SessionID != "managed-session" {
				t.Fatalf("normalized Stop = calls:%d args:%+v", handler.calls, handler.got)
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
	binary := writeOpenCodeLauncherFixture(t, "#!/bin/sh\n/usr/bin/touch "+shellQuote(marker)+"\n")
	opts := launcherOptions(t, binary, "/tmp/unused.sock")
	opts.Env = []string{"HOME=" + t.TempDir(), ManagedSessionEnv + "=partial"}
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
		"case \"$OPENCODE_CONFIG_CONTENT\" in *opencode-plugin.js*) ;; *) exit 36;; esac\n"
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

func writeOpenCodeLauncherFixture(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "fake-opencode")
	if err := os.WriteFile(path, []byte(body), 0o700); err != nil {
		t.Fatalf("write fake OpenCode: %v", err)
	}
	return path
}
