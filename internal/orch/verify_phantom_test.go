package orch

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestAcceptanceFlagsFindingsForNonexistentPaths guards the failure mode that
// cost a full diagnosis cycle and produced a four-file phantom "fix".
//
// golangci-lint resurrected 11 findings from its own cache, every one naming a
// file under a sibling directory that no longer exists on disk. The step failed,
// a provider turn was handed those findings, and it did exactly what a diligent
// agent should: it tried to fix them. It could not -- the files are gone -- so
// it asked for interactive guidance and the task died as interactive_prompt.
//
// The agent was not wrong. The problem is that an IMPOSSIBLE task is
// indistinguishable from a merely hard one, so the turn was spent before anyone
// could notice the paths pointed outside the project.
//
// A tool reporting a finding for a path that does not resolve inside the
// project is not reporting a finding; it is reporting a stale index. Naming
// that is strictly better than passing it to an agent to "fix".
func TestAcceptanceFlagsFindingsForNonexistentPaths(t *testing.T) {
	dir := t.TempDir()

	// A command that FAILS while naming a file outside the project which does
	// not exist -- the exact shape of the stale-cache output.
	phantom := filepath.Join(dir, "..", "gone-worktree", "internal", "x.go")
	cmd := "echo '" + phantom + ":90:12: G304: Potential file inclusion via variable (gosec)'; exit 1"

	ok, reason, err := checkCommandExitsZero(context.Background(), dir, cmd)
	if err != nil {
		t.Fatalf("checkCommandExitsZero: %v", err)
	}
	if ok {
		t.Fatal("a failing command reported ok; fixture is wrong")
	}
	if !strings.Contains(reason, "stale") && !strings.Contains(reason, "phantom") &&
		!strings.Contains(reason, "does not exist") {
		t.Errorf("reason = %q\n\nwant it to name the finding as referring to a "+
			"path that does not exist, rather than passing the raw tool output "+
			"through as an ordinary failure -- an agent handed this will try to "+
			"fix a file that is not there", reason)
	}
}

// TestAcceptanceLeavesRealFailuresAlone is the other half: the detector must not
// rewrite a genuine failure into a phantom one.
//
// A real finding names a file that EXISTS. If the check flagged those too it
// would suppress every true positive -- the failure mode that is worse than the
// one being fixed, since it turns a red step green.
func TestAcceptanceLeavesRealFailuresAlone(t *testing.T) {
	dir := t.TempDir()
	existing := filepath.Join(dir, "real.go")
	if err := writeFileForTest(existing, "package x\n"); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	cmd := "echo '" + existing + ":1:1: some real finding (govet)'; exit 1"
	ok, reason, err := checkCommandExitsZero(context.Background(), dir, cmd)
	if err != nil {
		t.Fatalf("checkCommandExitsZero: %v", err)
	}
	if ok {
		t.Fatal("a failing command reported ok")
	}
	if strings.Contains(reason, "stale") || strings.Contains(reason, "phantom") {
		t.Errorf("reason = %q, but every path named EXISTS -- flagging a real "+
			"failure as stale would suppress true positives", reason)
	}
}

func writeFileForTest(path, content string) error {
	return os.WriteFile(path, []byte(content), 0o600)
}

// TestPhantomFindingPathsHandlesRelativePaths pins the case the first
// implementation missed.
//
// It skipped relative paths on the theory that they resolve against the TOOL's
// working directory, which might not be the acceptance dir. But
// checkCommandExitsZero sets cmd.Dir itself, so that directory is known -- and
// golangci-lint reports the real stale-cache paths RELATIVELY
// ("../.worktrees/..."). The detector therefore missed the exact output it was
// written for while passing against a synthetic absolute-path fixture: a check
// that works only on its own test data.
func TestPhantomFindingPathsHandlesRelativePaths(t *testing.T) {
	dir := t.TempDir()
	existing := filepath.Join(dir, "real.go")
	if err := writeFileForTest(existing, "package x\n"); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	out := "../gone-worktree/internal/cassette.go:90:12: G304: Potential file inclusion (gosec)\n" +
		filepath.Join(dir, "gone", "recorder.go") + ":68:9: G204: Subprocess launched (gosec)\n" +
		existing + ":1:1: a genuine finding (govet)"

	got := phantomFindingPaths(dir, out)
	if len(got) != 2 {
		t.Fatalf("phantomFindingPaths = %v (%d), want 2: the relative phantom AND "+
			"the absolute phantom, but not the file that exists", got, len(got))
	}
	for _, g := range got {
		if strings.Contains(g, "real.go") {
			t.Errorf("flagged an existing file as phantom: %s", g)
		}
	}
}

// TestFindingPathReMatchesWindowsDriveLetters pins a bug that only the Windows
// runner could see, using a fixture that runs everywhere.
//
// The path body cannot contain ':' (that is what terminates it before the line
// number), so without an explicit drive-letter alternative `C:\src\x.go:9:1:`
// matched NOTHING and every Windows finding was invisible to the detector. The
// tests passed on macOS and Linux and failed only on the one platform where a
// colon is part of an ordinary absolute path.
//
// Asserted against the REGEX rather than the filesystem so it fails on any host:
// a guard that needs a Windows runner to notice a Windows bug is the same
// too-late signal that let this reach CI.
func TestFindingPathReMatchesWindowsDriveLetters(t *testing.T) {
	for _, tc := range []struct {
		name string
		line string
		want string
	}{
		{
			name: "windows absolute",
			line: `C:\Users\RUNNER~1\AppData\Local\Temp\t\gone\x.go:90:12: G304: bad (gosec)`,
			want: `C:\Users\RUNNER~1\AppData\Local\Temp\t\gone\x.go`,
		},
		{
			name: "unix absolute",
			line: "/tmp/t/gone/x.go:90:12: G304: bad (gosec)",
			want: "/tmp/t/gone/x.go",
		},
		{
			name: "relative",
			line: "../.worktrees/rr-sandbox/internal/cassette.go:90:12: G304: bad (gosec)",
			want: "../.worktrees/rr-sandbox/internal/cassette.go",
		},
		{
			name: "line only, no column",
			line: "internal/x.go:12: some finding (govet)",
			want: "internal/x.go",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := findingPathRe.FindStringSubmatch(tc.line)
			if m == nil {
				t.Fatalf("no match for %q; findings on this platform are invisible "+
					"to the phantom detector", tc.line)
			}
			if m[1] != tc.want {
				t.Errorf("captured %q, want %q", m[1], tc.want)
			}
		})
	}
}
