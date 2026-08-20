package provider

import "testing"

// Real JSONL lines captured directly from `@github/copilot` 1.0.80
// (`copilot -p "reply with exactly the word: hello" --output-format json -s
// --no-color`), 2026-08-20 -- not fabricated, so copilotResultCollector is
// tested against the actual schema, including the fields this runner
// deliberately ignores (ephemeral progress frames).
const copilotEphemeralFrame = `{"type":"assistant.message_start","data":{"messageId":"5672b33b-fadd-4840-96f6-0c4ad5a570e4"},"ephemeral":true,"id":"7f903dc3-e4a7-4589-b11c-917a29a1e9b0","timestamp":"2026-08-20T06:27:38.574Z","parentId":"0c0b335f-c2a4-4b43-a8b0-c270049b460f"}`

const copilotAssistantMessageFrame = `{"type":"assistant.message","data":{"messageId":"5672b33b-fadd-4840-96f6-0c4ad5a570e4","model":"claude-sonnet-4.6","content":"hello","toolRequests":[],"interactionId":"1a62fe75-0e28-45d8-ada9-b42c917be56d","turnId":"0"},"id":"f7ab6a9e-4b96-4f63-b51d-f7cedf508ece","timestamp":"2026-08-20T06:27:38.601Z","parentId":"0c0b335f-c2a4-4b43-a8b0-c270049b460f"}`

const copilotResultFrame = `{"type":"result","timestamp":"2026-08-20T06:27:38.643Z","sessionId":"14636444-dac3-4d1c-b262-4ffafb13c12d","exitCode":0,"usage":{"premiumRequests":1,"totalApiDurationMs":1810,"sessionDurationMs":5614,"codeChanges":{"linesAdded":0,"linesRemoved":0,"filesModified":[]}}}`

func TestCopilotResultCollectorParsesRealSuccessFrames(t *testing.T) {
	var c copilotResultCollector
	c.consume([]byte(copilotEphemeralFrame))
	c.consume([]byte(copilotAssistantMessageFrame))
	c.consume([]byte(copilotResultFrame))

	if !c.succeeded() {
		t.Fatalf("expected succeeded() true, collector = %+v", c)
	}
	if c.failed() {
		t.Fatalf("expected failed() false, collector = %+v", c)
	}
	if c.assistantText != "hello" {
		t.Fatalf("assistantText = %q, want %q", c.assistantText, "hello")
	}
	if c.sessionID != "14636444-dac3-4d1c-b262-4ffafb13c12d" {
		t.Fatalf("sessionID = %q, want the real captured session id", c.sessionID)
	}
}

// Real JSONL captured from a failing turn (`copilot -p ... --model
// not-a-real-model-xyz`), 2026-08-20: the CLI prints a plain-text error to
// the merged stdout+stderr stream (confirmed: NO JSON at all for this
// failure mode) and exits 1. Only the startup frames are JSON; there is no
// "result" frame.
func TestCopilotResultCollectorTreatsMissingResultFrameAsUnresolved(t *testing.T) {
	var c copilotResultCollector
	c.consume([]byte(`{"type":"session.mcp_servers_loaded","data":{"servers":[]},"ephemeral":true,"id":"x","timestamp":"2026-08-20T06:28:30.899Z","parentId":"y"}`))
	c.consume([]byte(`Error: Model "not-a-real-model-xyz" from --model flag is not available.`))

	if c.succeeded() {
		t.Fatalf("expected succeeded() false with no result frame, collector = %+v", c)
	}
	if c.failed() {
		// failed() is specifically "saw a result frame with a nonzero exit
		// code" -- this failure mode never gets one, so the runner's own
		// fallback (exitErr != nil && !succeeded()) is what catches it, not
		// failed(). Both false is the correct, verified shape here.
		t.Fatalf("expected failed() false too (no result frame arrived to set it), collector = %+v", c)
	}
}

func TestCopilotResultCollectorRecognizesAResultFrameWithNonzeroExit(t *testing.T) {
	var c copilotResultCollector
	c.consume([]byte(`{"type":"result","timestamp":"2026-08-20T06:29:00.000Z","sessionId":"abc","exitCode":1}`))

	if c.succeeded() {
		t.Fatal("expected succeeded() false for a nonzero exitCode result frame")
	}
	if !c.failed() {
		t.Fatal("expected failed() true for a nonzero exitCode result frame")
	}
}

func TestCopilotArgsUsePromptAsArgumentNotStdin(t *testing.T) {
	binding := Binding{Config: BindingConfig{Binary: "copilot"}}
	req := Request{UserPrompt: "do the thing", WorkingDir: "/work"}
	invocation := Invocation{}

	args := copilotArgs(binding, req, invocation)

	joined := make(map[string]bool, len(args))
	for i, a := range args {
		joined[a] = true
		if a == "-p" && i+1 < len(args) && args[i+1] == combinePrompt(req) {
			joined["__prompt_arg_present__"] = true
		}
	}
	if !joined["__prompt_arg_present__"] {
		t.Fatalf("expected -p <prompt> in args, got %v", args)
	}
	if !joined["--allow-all-tools"] {
		t.Fatalf("expected --allow-all-tools (required for non-interactive mode per `copilot --help`), got %v", args)
	}
	if !joined["-C"] || !joined["/work"] {
		t.Fatalf("expected -C /work (working directory), got %v", args)
	}
}

func TestCopilotArgsOmitDefaultEffortOverride(t *testing.T) {
	binding := Binding{Config: BindingConfig{Binary: "copilot"}}
	req := Request{UserPrompt: "x"}

	args := copilotArgs(binding, req, Invocation{Effort: "default"})
	for _, a := range args {
		if a == "--effort" {
			t.Fatalf("expected no --effort override for the \"default\" sentinel (that names copilot's own configured lane, not a Ralph-translated value), got %v", args)
		}
	}

	args = copilotArgs(binding, req, Invocation{Effort: "high"})
	found := false
	for i, a := range args {
		if a == "--effort" && i+1 < len(args) && args[i+1] == "high" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected --effort high for a real effort value, got %v", args)
	}
}
