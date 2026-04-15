package rlog

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestJSONModeShapesStreamJSON(t *testing.T) {
	var buf bytes.Buffer
	log := New(ModeJSON, &buf)

	log.Info("init.start", "repo", "/tmp/foo")

	line := buf.String()
	var rec map[string]any
	if err := json.Unmarshal([]byte(line), &rec); err != nil {
		t.Fatalf("parse: %v  line=%q", err, line)
	}
	if rec["type"] != "ralph" {
		t.Errorf("type = %v, want ralph", rec["type"])
	}
	if rec["event"] != "init.start" {
		t.Errorf("event = %v, want init.start", rec["event"])
	}
	if rec["repo"] != "/tmp/foo" {
		t.Errorf("repo = %v, want /tmp/foo", rec["repo"])
	}
	if _, ok := rec["ts"]; !ok {
		t.Error("missing ts key")
	}
	if _, ok := rec["level"]; ok {
		t.Error("level should be dropped in stream-json shape")
	}
}

func TestTextModeIsHumanReadable(t *testing.T) {
	var buf bytes.Buffer
	log := New(ModeText, &buf)

	log.Info("hello", "k", "v")

	out := buf.String()
	if !strings.Contains(out, "msg=hello") || !strings.Contains(out, "k=v") {
		t.Errorf("unexpected text output: %q", out)
	}
}

func TestFromContextFallsBackToDefault(t *testing.T) {
	got := FromContext(context.Background())
	if got == nil {
		t.Error("FromContext returned nil for empty ctx")
	}

	custom := New(ModeText, nil)
	ctx := WithLogger(context.Background(), custom)
	if FromContext(ctx) != custom {
		t.Error("FromContext did not return the attached logger")
	}
}
