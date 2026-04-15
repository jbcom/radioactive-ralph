// Package main is a test double for `claude -p --input-format stream-json`.
//
// It implements enough of the stream-json protocol for session tests:
//
//   - Emits an initial system frame with the assigned --session-id.
//   - Reads user messages from stdin, one JSON line at a time.
//   - For each user message, emits an assistant frame echoing the
//     user's text, then a result frame with subtype=success.
//   - Exits 0 on stdin EOF.
//
// Behavior knobs via env vars:
//
//   - FAKE_CLAUDE_DELAY_MS: sleep this many ms before emitting the
//     result frame, to exercise WaitForIdle timing.
//   - FAKE_CLAUDE_RESUME_ECHO: if set, the first emitted frame after a
//     --resume flag is an assistant message with subtype=resumed_ack
//     carrying the session ID, so tests can verify resume routing.
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type inbound struct {
	Type    string `json:"type"`
	Message struct {
		Role    string `json:"role"`
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	} `json:"message"`
}

func main() {
	var sessionID string
	var resumed bool
	for i, a := range os.Args {
		switch a {
		case "--session-id":
			if i+1 < len(os.Args) {
				sessionID = os.Args[i+1]
			}
		case "--resume":
			if i+1 < len(os.Args) {
				sessionID = os.Args[i+1]
				resumed = true
			}
		}
	}
	if sessionID == "" {
		sessionID = "fake-session"
	}

	// Initial system frame.
	emit(map[string]any{
		"type":       "system",
		"subtype":    "init",
		"session_id": sessionID,
	})

	if resumed && os.Getenv("FAKE_CLAUDE_RESUME_ECHO") != "" {
		emit(map[string]any{
			"type":       "assistant",
			"session_id": sessionID,
			"subtype":    "resumed_ack",
			"message": map[string]any{
				"role": "assistant",
				"content": []map[string]any{
					{"type": "text", "text": "resumed"},
				},
			},
		})
	}

	delay := 0
	if v := os.Getenv("FAKE_CLAUDE_DELAY_MS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			delay = n
		}
	}

	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for scanner.Scan() {
		var in inbound
		if err := json.Unmarshal(scanner.Bytes(), &in); err != nil {
			fmt.Fprintf(os.Stderr, "fake-claude: decode: %v\n", err)
			continue
		}
		echo := ""
		if len(in.Message.Content) > 0 {
			echo = in.Message.Content[0].Text
		}
		emit(map[string]any{
			"type":       "assistant",
			"session_id": sessionID,
			"message": map[string]any{
				"role": "assistant",
				"content": []map[string]any{
					{"type": "text", "text": "echo: " + echo},
				},
			},
		})
		if delay > 0 {
			time.Sleep(time.Duration(delay) * time.Millisecond)
		}
		emit(map[string]any{
			"type":       "result",
			"subtype":    "success",
			"session_id": sessionID,
			"result":     json.RawMessage(fmt.Sprintf(`%q`, strings.TrimSpace(echo))),
		})
	}
}

// emit writes one JSON object plus a newline to stdout and flushes.
func emit(v map[string]any) {
	b, err := json.Marshal(v)
	if err != nil {
		fmt.Fprintf(os.Stderr, "fake-claude: marshal: %v\n", err)
		return
	}
	_, _ = os.Stdout.Write(b)
	_, _ = os.Stdout.WriteString("\n")
}
