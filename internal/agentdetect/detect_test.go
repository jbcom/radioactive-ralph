package agentdetect

import (
	"errors"
	"strings"
	"testing"
)

// withFakePath swaps lookPath/runVersion for the duration of a test so
// Detect never touches the real PATH or spawns real binaries. found is the
// set of candidate names to report as present (mapped to a fake path);
// versions maps a name to the fake --version output to return.
func withFakePath(t *testing.T, found map[string]string, versions map[string]string) {
	t.Helper()
	origLookPath, origRunVersion := lookPath, runVersion
	t.Cleanup(func() {
		lookPath = origLookPath
		runVersion = origRunVersion
	})
	lookPath = func(name string) (string, error) {
		if p, ok := found[name]; ok {
			return p, nil
		}
		return "", errors.New("not found")
	}
	runVersion = func(path string, _ []string) (string, error) {
		for name, p := range found {
			if p == path {
				if v, ok := versions[name]; ok {
					return v, nil
				}
			}
		}
		return "", errors.New("no version")
	}
}

func TestDetectClassifiesEveryCandidate(t *testing.T) {
	withFakePath(t, map[string]string{
		"claude":       "/usr/local/bin/claude",
		"codex":        "/usr/local/bin/codex",
		"opencode":     "/usr/local/bin/opencode",
		"gemini":       "/usr/local/bin/gemini",
		"cursor-agent": "/usr/local/bin/cursor-agent",
		"cursor":       "/usr/local/bin/cursor",
		"agy":          "/usr/local/bin/agy",
	}, map[string]string{
		"claude":       "2.1.211 (Claude Code)",
		"codex":        "codex-cli 0.142.0",
		"opencode":     "1.18.3",
		"gemini":       "0.49.0",
		"cursor-agent": "2025.11.25-d5b3271",
		"cursor":       "3.7.27",
		"agy":          "1.1.3",
	})

	detected := Detect()
	byName := indexByName(detected)

	wantStatus := map[string]Status{
		"claude":       Supported,
		"codex":        Supported,
		"opencode":     Supported,
		"gemini":       Deprecated,
		"cursor-agent": RemoteDelegating,
		"cursor":       Unknown,
		"agy":          Unknown,
	}
	for name, want := range wantStatus {
		got, ok := byName[name]
		if !ok {
			t.Fatalf("Detect() missing candidate %q", name)
		}
		if got.Status != want {
			t.Errorf("%s status = %v, want %v", name, got.Status, want)
		}
		if got.Path == "" {
			t.Errorf("%s Path is empty, want populated (found in fake PATH)", name)
		}
		if got.Version == "" {
			t.Errorf("%s Version is empty, want populated", name)
		}
		if got.Reason == "" {
			t.Errorf("%s Reason is empty, want a classification rationale", name)
		}
	}
}

func TestDetectCursorVsCursorAgentDistinctClassification(t *testing.T) {
	withFakePath(t, map[string]string{
		"cursor":       "/usr/local/bin/cursor",
		"cursor-agent": "/Users/x/.local/bin/cursor-agent",
	}, nil)

	detected := Detect()
	byName := indexByName(detected)

	cursor := byName["cursor"]
	if cursor.Status != Unknown {
		t.Errorf("cursor status = %v, want Unknown", cursor.Status)
	}
	if cursor.Reason == "" || !strings.Contains(cursor.Reason, "editor") {
		t.Errorf("cursor reason = %q, want it to explain it is an editor, not an agent CLI", cursor.Reason)
	}

	cursorAgent := byName["cursor-agent"]
	if cursorAgent.Status != RemoteDelegating {
		t.Errorf("cursor-agent status = %v, want RemoteDelegating", cursorAgent.Status)
	}
	if cursorAgent.Reason == "" || !strings.Contains(cursorAgent.Reason, "cloud") {
		t.Errorf("cursor-agent reason = %q, want it to mention cloud delegation", cursorAgent.Reason)
	}
}

func TestDetectMissingBinaryLeavesPathAndVersionEmpty(t *testing.T) {
	withFakePath(t, map[string]string{}, nil)

	detected := Detect()
	for _, d := range detected {
		if d.Path != "" {
			t.Errorf("%s: Path = %q, want empty when not found on PATH", d.Name, d.Path)
		}
		if d.Version != "" {
			t.Errorf("%s: Version = %q, want empty when not found on PATH", d.Name, d.Version)
		}
		// Status/Reason are standing classifications independent of
		// whether the binary is actually installed on this machine.
		if d.Reason == "" {
			t.Errorf("%s: Reason should still be populated even when not found", d.Name)
		}
	}
}

func TestDetectGeminiReasonCitesDeprecation(t *testing.T) {
	withFakePath(t, nil, nil)
	detected := Detect()
	gemini := indexByName(detected)["gemini"]
	if gemini.Status != Deprecated {
		t.Fatalf("gemini status = %v, want Deprecated", gemini.Status)
	}
	if !strings.Contains(gemini.Reason, "2026-06-18") || !strings.Contains(gemini.Reason, "410") {
		t.Errorf("gemini reason = %q, want it to cite the 2026-06-18 deprecation and 410 Gone", gemini.Reason)
	}
}

func TestDetectAgyReasonCitesUnconfirmedLocalSurface(t *testing.T) {
	withFakePath(t, nil, nil)
	detected := Detect()
	agy := indexByName(detected)["agy"]
	if agy.Status != Unknown {
		t.Fatalf("agy status = %v, want Unknown", agy.Status)
	}
	if !strings.Contains(agy.Reason, "unconfirmed") {
		t.Errorf("agy reason = %q, want it to say the local surface is unconfirmed", agy.Reason)
	}
}

func TestSuggestReturnsOnlySupportedAndPresent(t *testing.T) {
	withFakePath(t, map[string]string{
		"claude":   "/usr/local/bin/claude",
		"opencode": "/usr/local/bin/opencode",
		// codex intentionally absent — Supported status but not on PATH.
		"gemini": "/usr/local/bin/gemini",
	}, nil)

	suggested := Suggest(Detect())
	want := map[string]bool{"claude": true, "opencode": true}
	if len(suggested) != len(want) {
		t.Fatalf("Suggest() = %v, want exactly %v", suggested, want)
	}
	for _, name := range suggested {
		if !want[name] {
			t.Errorf("Suggest() unexpectedly included %q", name)
		}
	}
}

func TestStatusStringCoversAllValues(t *testing.T) {
	cases := map[Status]string{
		Supported:        "supported",
		Deprecated:       "deprecated",
		RemoteDelegating: "remote-delegating",
		Unknown:          "unknown",
	}
	for status, want := range cases {
		if got := status.String(); got != want {
			t.Errorf("Status(%d).String() = %q, want %q", status, got, want)
		}
	}
}

func indexByName(detected []DetectedCLI) map[string]DetectedCLI {
	out := make(map[string]DetectedCLI, len(detected))
	for _, d := range detected {
		out[d.Name] = d
	}
	return out
}
