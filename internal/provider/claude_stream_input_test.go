package provider

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestStreamJSONInputUsesTheWireFieldNames pins the stdin frame Ralph writes to
// `claude --input-format stream-json`.
//
// The struct's Message field carried NO json tag, so encoding/json emitted
// "Message". The CLI looks for `message.role`, found nothing, and every real
// turn died in under a second with:
//
//	Error: Expected message role 'user', got 'undefined'
//
// Nothing caught it. The unit tests around this function asserted on the Go
// struct or on round-tripping through the same encoder, both of which are
// blind to a field NAME being wrong -- and the live tests that would have hit
// it are opt-in behind RALPH_E2E_LIVE, so CI never ran a real claude turn.
//
// This asserts on the DECODED WIRE BYTES, using the key the CLI actually reads.
// A test that unmarshals into the producing struct would pass on the broken
// version, because the same wrong tag governs both directions.
func TestStreamJSONInputUsesTheWireFieldNames(t *testing.T) {
	raw, err := streamJSONInput("hello")
	if err != nil {
		t.Fatalf("streamJSONInput: %v", err)
	}
	if !strings.HasSuffix(string(raw), "\n") {
		t.Fatal("frame must end in a newline; the CLI reads one JSON value per line")
	}

	var wire map[string]any
	if err := json.Unmarshal(raw, &wire); err != nil {
		t.Fatalf("emitted frame is not valid JSON: %v (%s)", err, raw)
	}
	if wire["type"] != "user" {
		t.Fatalf("type = %v, want \"user\"", wire["type"])
	}
	if _, ok := wire["Message"]; ok {
		t.Fatalf("frame carries a capitalized \"Message\" key: %s\n"+
			"the CLI reads `message`, so it sees no role and rejects the turn with "+
			"\"Expected message role 'user', got 'undefined'\"", raw)
	}

	message, ok := wire["message"].(map[string]any)
	if !ok {
		t.Fatalf("frame has no lowercase \"message\" object: %s", raw)
	}
	if message["role"] != "user" {
		t.Fatalf("message.role = %v, want \"user\" -- this is the exact field the "+
			"CLI reported as 'undefined'", message["role"])
	}

	content, ok := message["content"].([]any)
	if !ok || len(content) != 1 {
		t.Fatalf("message.content = %v, want one block", message["content"])
	}
	block, ok := content[0].(map[string]any)
	if !ok {
		t.Fatalf("content[0] is not an object: %v", content[0])
	}
	if block["type"] != "text" || block["text"] != "hello" {
		t.Fatalf("content[0] = %v, want {type:text, text:hello}", block)
	}
}
