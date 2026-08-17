package adapters

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/jbcom/radioactive-ralph/internal/ipc"
)

const (
	// ManagedSessionEnv carries Ralph's opaque, non-secret store session id.
	ManagedSessionEnv = "RALPH_MANAGED_SESSION_ID"
	// HookEndpointEnv carries the local supervisor socket path.
	HookEndpointEnv   = "RALPH_HOOK_ENDPOINT"
	maxHookInputBytes = 1 << 20
)

// ErrBlocked is intentionally static. Callers translate it to exit code 2 for
// providers whose blocking protocol uses a nonzero exit; it never wraps
// provider input, an environment value, or a socket error.
var ErrBlocked = errors.New("managed hook blocked")

// Environment resolves one environment key for hook ingress.
type Environment func(string) string

// RunHook normalizes one provider event and asks the supervisor for a verdict.
// Unmanaged sessions are a strict no-op, allowing globally configured hooks to
// coexist with ordinary user sessions. Managed failures block without echoing
// any raw JSON or environment value.
func RunHook(
	ctx context.Context,
	adapter, event string,
	input io.Reader,
	stdout, stderr io.Writer,
	getenv Environment,
) error {
	sessionID := getenv(ManagedSessionEnv)
	if sessionID == "" {
		return nil
	}
	if !validAdapter(adapter) || (event != ipc.HookEventPostToolUse && event != ipc.HookEventStop) {
		return block(adapter, stdout, stderr, "invalid_event")
	}
	endpoint := getenv(HookEndpointEnv)
	if endpoint == "" || !validPayload(input, event) {
		return block(adapter, stdout, stderr, "invalid_event")
	}

	client, err := ipc.Dial(endpoint, 2*time.Second)
	if err != nil {
		return block(adapter, stdout, stderr, "supervisor_unavailable")
	}
	defer func() { _ = client.Close() }()
	hookCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	reply, err := client.HookEvent(hookCtx, ipc.HookEventArgs{
		Adapter: adapter, Event: event, SessionID: sessionID,
	})
	if err != nil || !reply.Allow {
		reason := reply.Reason
		if err != nil || reason == "" {
			reason = "supervisor_unavailable"
		}
		return block(adapter, stdout, stderr, reason)
	}
	return nil
}

func validPayload(input io.Reader, event string) bool {
	raw, err := io.ReadAll(io.LimitReader(input, maxHookInputBytes+1))
	if err != nil || len(raw) == 0 || len(raw) > maxHookInputBytes {
		return false
	}
	var envelope struct {
		HookEventName string `json:"hook_event_name"`
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	if err := decoder.Decode(&envelope); err != nil || envelope.HookEventName != event {
		return false
	}
	if decoder.Decode(&struct{}{}) != io.EOF {
		return false
	}
	return true
}

func validAdapter(adapter string) bool {
	return adapter == "claude" || adapter == "codex" || adapter == "opencode"
}

func block(adapter string, stdout, stderr io.Writer, reasonCode string) error {
	const reason = "Radioactive Ralph verification is not complete."
	switch adapter {
	case "claude":
		// Ralph exits 2 for ErrBlocked. Claude treats stderr as the blocking
		// reason on that path; structured stdout is for the exit-0 protocol.
		_, _ = fmt.Fprintln(stderr, reason)
	case "codex":
		// Codex parses structured decisions on the exit-0 path. Returning
		// ErrBlocked here would turn this into exit 2, where Codex instead
		// expects a plain stderr reason.
		encoded, _ := json.Marshal(struct {
			Decision string `json:"decision"`
			Reason   string `json:"reason"`
		}{Decision: "block", Reason: reason})
		_, _ = fmt.Fprintln(stdout, string(encoded))
		return nil
	case "opencode":
		// The generated plugin consumes this finite code to decide whether to
		// poll an asynchronous verification. It never includes provider input.
		encoded, _ := json.Marshal(struct {
			Status string `json:"status"`
		}{Status: reasonCode})
		_, _ = fmt.Fprintln(stdout, string(encoded))
	default:
		_, _ = fmt.Fprintln(stderr, reason)
	}
	return ErrBlocked
}
