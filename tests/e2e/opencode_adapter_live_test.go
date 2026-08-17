//go:build !windows

package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jbcom/radioactive-ralph/internal/adapters"
	"github.com/jbcom/radioactive-ralph/internal/ipc"
)

const reviewedOpenCodeAdapterVersion = "1.18.18"

// TestLiveOpenCodeAdapterCompletionAuthority is an explicit, local-only
// feature probe. It proves the installed OpenCode version still loads Ralph's
// one reviewed progress plugin while the absolute launcher owns process-level
// Stop authority for both no-tool and tool turns.
func TestLiveOpenCodeAdapterCompletionAuthority(t *testing.T) {
	if os.Getenv("RALPH_OPENCODE_ADAPTER_LIVE") != "1" {
		t.Skip("RALPH_OPENCODE_ADAPTER_LIVE != 1; skipping authenticated OpenCode adapter probe")
	}
	openCode, err := exec.LookPath("opencode")
	if err != nil {
		t.Fatalf("find OpenCode: %v", err)
	}
	openCode, err = filepath.Abs(openCode)
	if err != nil {
		t.Fatalf("absolute OpenCode path: %v", err)
	}
	versionCtx, cancelVersion := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancelVersion()
	version, err := exec.CommandContext(versionCtx, openCode, "--version").Output() //nolint:gosec // operator-installed binary selected by explicit live gate
	if err != nil {
		if versionCtx.Err() != nil {
			t.Fatalf("OpenCode version probe exceeded live timeout: %v", versionCtx.Err())
		}
		t.Fatalf("OpenCode version: %v", err)
	}
	if got := strings.TrimSpace(string(version)); got != reviewedOpenCodeAdapterVersion {
		t.Fatalf("OpenCode version = %q, want reviewed %s; re-review the adapter before updating the pin",
			got, reviewedOpenCodeAdapterVersion)
	}

	ralph := BuildBinary(t)
	target := t.TempDir()
	if _, err := adapters.Install(ralph, target); err != nil {
		t.Fatalf("install isolated adapter bundle: %v", err)
	}
	bundle, err := adapters.ResolveCurrentBundle(target)
	if err != nil {
		t.Fatalf("resolve isolated adapter bundle: %v", err)
	}
	handler := &liveOpenCodeHookHandler{}
	endpoint, heartbeat := ipc.ServiceEndpoint(t.TempDir())
	server, err := ipc.NewServer(ipc.ServerOptions{
		SocketPath: endpoint, HeartbeatPath: heartbeat, Handler: handler,
	})
	if err != nil {
		t.Fatalf("create hook server: %v", err)
	}
	if err := server.Start(); err != nil {
		t.Fatalf("start hook server: %v", err)
	}
	defer func() { _ = server.Stop() }()

	t.Run("unmanaged transparent passthrough under sanitized PATH", func(t *testing.T) {
		out, exitCode := runLiveOpenCodeLauncher(t, bundle, openCode, "", "", "--version")
		if exitCode != 0 || strings.TrimSpace(out) != reviewedOpenCodeAdapterVersion {
			t.Fatalf("unmanaged version = exit:%d output:%q", exitCode, out)
		}
	})

	t.Run("supervisor unavailable no-tool run fails closed and secret blind", func(t *testing.T) {
		const secret = "opencode-live-secret-canary"
		missing := filepath.Join(t.TempDir(), "missing.sock")
		out, exitCode := runLiveOpenCodeLauncherWithEnv(
			t, bundle, openCode, "live-unavailable", missing,
			[]string{"RALPH_LIVE_SECRET_CANARY=" + secret},
			"run", "Reply with exactly OK and do not use tools.", "--format", "json", "--dir", t.TempDir(),
		)
		if exitCode != 2 || !strings.Contains(out, `{"status":"supervisor_unavailable"}`) {
			t.Fatalf("unavailable completion = exit:%d output:%q", exitCode, out)
		}
		if strings.Contains(out, secret) {
			t.Fatal("unavailable completion echoed a secret canary")
		}
	})

	t.Run("no-tool run submits one final Stop", func(t *testing.T) {
		handler.reset()
		out, exitCode := runLiveOpenCodeLauncher(
			t, bundle, openCode, "live-no-tool", endpoint,
			"run", "Reply with exactly OK and do not use tools.", "--format", "json", "--dir", t.TempDir(),
		)
		if exitCode != 0 {
			t.Fatalf("no-tool completion = exit:%d output:%q", exitCode, out)
		}
		if post, stop := handler.counts(); post != 0 || stop != 1 {
			t.Fatalf("no-tool hook counts = PostToolUse:%d Stop:%d, want 0/1", post, stop)
		}
	})

	t.Run("tool turn reports progress then one final Stop", func(t *testing.T) {
		handler.reset()
		out, exitCode := runLiveOpenCodeLauncher(
			t, bundle, openCode, "live-tool", endpoint,
			"run", "Use the shell tool to run /usr/bin/printf RALPH_TOOL_PROBE, then reply briefly.",
			"--format", "json", "--dir", t.TempDir(),
		)
		if exitCode != 0 {
			t.Fatalf("tool completion = exit:%d output:%q", exitCode, out)
		}
		if post, stop := handler.counts(); post < 1 || stop != 1 {
			t.Fatalf("tool hook counts = PostToolUse:%d Stop:%d, want at least 1/1", post, stop)
		}
	})
}

func runLiveOpenCodeLauncher(
	t *testing.T,
	bundle adapters.BundlePaths,
	openCode, session, endpoint string,
	args ...string,
) (string, int) {
	t.Helper()
	return runLiveOpenCodeLauncherWithEnv(t, bundle, openCode, session, endpoint, nil, args...)
}

func runLiveOpenCodeLauncherWithEnv(
	t *testing.T,
	bundle adapters.BundlePaths,
	openCode, session, endpoint string,
	extraEnv []string,
	args ...string,
) (string, int) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	launcherArgs := make([]string, 0, 7+len(args))
	launcherArgs = append(launcherArgs,
		"hook", "launch-opencode", "--binary", openCode,
		"--adapter-root", bundle.Target,
	)
	if session != "" && endpoint != "" {
		runtimePaths, err := adapters.PrepareOpenCodeRuntime(bundle)
		if err != nil {
			t.Fatalf("prepare isolated live OpenCode runtime: %v", err)
		}
		defer runtimePaths.Cleanup()
		launcherArgs = append(launcherArgs, "--runtime-root", runtimePaths.Root)
	}
	launcherArgs = append(launcherArgs, "--")
	launcherArgs = append(launcherArgs, args...)
	cmd := exec.CommandContext(ctx, bundle.Executable, launcherArgs...) //nolint:gosec // exact verified test bundle and operator-installed provider
	env := liveOpenCodeEnvironment(os.Environ(), map[string]string{
		"PATH": "/usr/bin:/bin",
	})
	if session != "" || endpoint != "" {
		env = liveOpenCodeEnvironment(env, map[string]string{
			adapters.ManagedSessionEnv: session,
			adapters.HookEndpointEnv:   endpoint,
		})
	} else {
		env = liveOpenCodeEnvironment(env, map[string]string{
			adapters.ManagedSessionEnv: "",
			adapters.HookEndpointEnv:   "",
		})
	}
	for _, entry := range extraEnv {
		key, value, ok := strings.Cut(entry, "=")
		if !ok {
			t.Fatalf("invalid live environment fixture")
		}
		env = liveOpenCodeEnvironment(env, map[string]string{key: value})
	}
	cmd.Env = env
	var output boundedLiveOutput
	cmd.Stdout, cmd.Stderr = &output, &output
	err := cmd.Run()
	if err == nil {
		return output.String(), 0
	}
	if ctx.Err() != nil {
		t.Fatalf("OpenCode launcher exceeded live timeout: %v", ctx.Err())
	}
	if exitErr, ok := err.(*exec.ExitError); ok {
		return output.String(), exitErr.ExitCode()
	}
	t.Fatalf("run OpenCode launcher: %v", err)
	return "", -1
}

func liveOpenCodeEnvironment(env []string, replacements map[string]string) []string {
	result := make([]string, 0, len(env)+len(replacements))
	for _, entry := range env {
		key, _, _ := strings.Cut(entry, "=")
		if _, replace := replacements[key]; !replace {
			result = append(result, entry)
		}
	}
	for key, value := range replacements {
		if value != "" {
			result = append(result, key+"="+value)
		}
	}
	return result
}

type boundedLiveOutput struct {
	buffer bytes.Buffer
}

func (w *boundedLiveOutput) Write(p []byte) (int, error) {
	const limit = 1 << 20
	remaining := limit - w.buffer.Len()
	if remaining > 0 {
		_, _ = w.buffer.Write(p[:min(remaining, len(p))])
	}
	return len(p), nil
}

func (w *boundedLiveOutput) String() string { return w.buffer.String() }

type liveOpenCodeHookHandler struct {
	mu                sync.Mutex
	postToolUse, stop int
}

func (h *liveOpenCodeHookHandler) HandleHookEvent(
	_ context.Context, args ipc.HookEventArgs,
) (ipc.HookEventReply, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if args.Adapter != "opencode" {
		return ipc.HookEventReply{}, fmt.Errorf("unexpected adapter")
	}
	switch args.Event {
	case ipc.HookEventPostToolUse:
		h.postToolUse++
	case ipc.HookEventStop:
		h.stop++
	default:
		return ipc.HookEventReply{}, fmt.Errorf("unexpected hook event")
	}
	return ipc.HookEventReply{Allow: true}, nil
}

func (h *liveOpenCodeHookHandler) reset() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.postToolUse, h.stop = 0, 0
}

func (h *liveOpenCodeHookHandler) counts() (int, int) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.postToolUse, h.stop
}

func (*liveOpenCodeHookHandler) HandleStatus(context.Context) (ipc.StatusReply, error) {
	return ipc.StatusReply{}, nil
}

func (*liveOpenCodeHookHandler) HandleEnqueue(context.Context, ipc.EnqueueArgs) (ipc.EnqueueReply, error) {
	return ipc.EnqueueReply{}, nil
}

func (*liveOpenCodeHookHandler) HandleStop(context.Context, ipc.StopArgs) error { return nil }

func (*liveOpenCodeHookHandler) HandleReloadConfig(context.Context) error { return nil }

func (*liveOpenCodeHookHandler) HandleAttach(
	context.Context, ipc.AttachArgs, func(json.RawMessage) error,
) error {
	return nil
}
