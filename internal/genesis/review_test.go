package genesis

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestRenderForReview_HeadlessEmitsToWriter(t *testing.T) {
	md := []byte(validPlanMD)
	var buf bytes.Buffer

	final, findings, err := RenderForReview(md, ReviewOptions{Mode: ReviewHeadless, Writer: &buf})
	if err != nil {
		t.Fatalf("RenderForReview: %v", err)
	}
	if buf.String() != string(md) {
		t.Fatalf("expected markdown written to buf verbatim, got %q", buf.String())
	}
	if string(final) != string(md) {
		t.Fatalf("expected final == input in headless mode")
	}
	if len(findings) != 0 {
		t.Fatalf("expected no findings for a clean plan, got %v", findings)
	}
}

func TestRenderForReview_HeadlessSurfacesValidateFindings(t *testing.T) {
	md := []byte("# Group\n\n1. first\n- second\n")
	var buf bytes.Buffer

	_, findings, err := RenderForReview(md, ReviewOptions{Mode: ReviewHeadless, Writer: &buf})
	if err != nil {
		t.Fatalf("RenderForReview: %v", err)
	}
	if len(findings) == 0 {
		t.Fatalf("expected findings for the mixed-list-type plan")
	}
}

func TestRenderForReview_EditorRoundTrip(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses a POSIX shell script as the fake editor")
	}

	dir := t.TempDir()
	planPath := filepath.Join(dir, "plan.md")

	// A fake "editor" that just appends a line to whatever file it's
	// pointed at, so the test can assert the round trip without invoking
	// a real interactive editor.
	fakeEditor := filepath.Join(dir, "fake-editor.sh")
	script := "#!/bin/sh\nprintf '\\n# Appended\\n\\n- extra\\n' >> \"$1\"\n"
	if err := os.WriteFile(fakeEditor, []byte(script), 0o700); err != nil { //nolint:gosec // test fixture, deliberately executable
		t.Fatalf("write fake editor: %v", err)
	}

	final, _, err := RenderForReview([]byte(validPlanMD), ReviewOptions{
		Mode:         ReviewEditor,
		Editor:       fakeEditor,
		TempFilePath: planPath,
	})
	if err != nil {
		t.Fatalf("RenderForReview: %v", err)
	}
	if !bytes.Contains(final, []byte("Appended")) {
		t.Fatalf("expected editor round-trip to include the appended section, got %q", final)
	}
}
