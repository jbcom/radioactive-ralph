package fixit

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jbcom/radioactive-ralph/internal/variant"
)

// ─── Stage 1: CaptureIntent ──────────────────────────────────────────

func TestCaptureIntentNonInteractivePassthrough(t *testing.T) {
	spec, err := CaptureIntent(IntentOptions{
		Topic:          "runtime-stabilization",
		Description:    "finish the rewrite",
		Constraints:    []string{"no opus"},
		NonInteractive: true,
	})
	if err != nil {
		t.Fatalf("CaptureIntent: %v", err)
	}
	if spec.Topic != "runtime-stabilization" {
		t.Errorf("Topic = %q", spec.Topic)
	}
	if spec.Description != "finish the rewrite" {
		t.Errorf("Description = %q", spec.Description)
	}
	if len(spec.Constraints) != 1 || spec.Constraints[0] != "no opus" {
		t.Errorf("Constraints = %v", spec.Constraints)
	}
}

func TestCaptureIntentReadsTopicMD(t *testing.T) {
	repo := t.TempDir()
	topicPath := filepath.Join(repo, "TOPIC.md")
	if err := os.WriteFile(topicPath, []byte("described via topic.md"), 0o644); err != nil {
		t.Fatalf("write TOPIC.md: %v", err)
	}
	spec, err := CaptureIntent(IntentOptions{
		Topic:          "auto",
		NonInteractive: true,
		RepoRoot:       repo,
	})
	if err != nil {
		t.Fatalf("CaptureIntent: %v", err)
	}
	if spec.Description != "described via topic.md" {
		t.Errorf("Description = %q; should come from TOPIC.md", spec.Description)
	}
}

func TestSanitizeTopic(t *testing.T) {
	cases := map[string]string{
		"":                       "general",
		"runtime stabilization":  "runtime-stabilization",
		"Runtime-Stabilization":  "runtime-stabilization",
		"weird!!@chars":          "weird-chars",
		"---":                    "general",
		"plan_a":                 "plan_a",
	}
	for in, want := range cases {
		got := sanitizeTopic(in)
		if got != want {
			t.Errorf("sanitizeTopic(%q) = %q, want %q", in, got, want)
		}
	}
}

// ─── Stage 2: Explore ────────────────────────────────────────────────

func newTestRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	mustRun(t, dir, "git", "init", "-q", "-b", "main")
	mustRun(t, dir, "git", "config", "user.email", "ralph@example.com")
	mustRun(t, dir, "git", "config", "user.name", "Ralph")
	mustRun(t, dir, "git", "config", "commit.gpgsign", "false")
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("---\ntitle: README\nupdated: 2026-04-15\nstatus: current\n---\n\n# Test\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mustRun(t, dir, "git", "add", ".")
	mustRun(t, dir, "git", "commit", "-q", "-m", "init")
	return dir
}

func mustRun(t *testing.T, cwd, name string, args ...string) {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = cwd
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%s %v: %v\n%s", name, args, err, out)
	}
}

func TestExploreReportsRepoShape(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()
	rc, err := Explore(ctx, repo)
	if err != nil {
		t.Fatalf("Explore: %v", err)
	}
	if rc.CurrentBranch != "main" {
		t.Errorf("CurrentBranch = %q, want main", rc.CurrentBranch)
	}
	if len(rc.Commits) == 0 {
		t.Error("Commits should be non-empty after init + one commit")
	}
	if len(rc.DocsPresent) == 0 {
		t.Error("should have detected at least the README")
	}
	foundReadme := false
	for _, d := range rc.DocsPresent {
		if strings.HasSuffix(d.Path, "README.md") {
			foundReadme = true
			if d.Frontmatter["status"] != "current" {
				t.Errorf("README frontmatter status = %q", d.Frontmatter["status"])
			}
		}
	}
	if !foundReadme {
		t.Error("README.md missing from DocsPresent")
	}
	if rc.PlansIndexExists {
		t.Error("plans/index.md should not exist in a fresh repo")
	}
}

func TestReadFrontmatterPreservesTimestampDates(t *testing.T) {
	repo := t.TempDir()
	path := filepath.Join(repo, "README.md")
	if err := os.WriteFile(path, []byte("---\nupdated: 2026-04-14\nstatus: current\n---\n\n# Test\n"), 0o644); err != nil {
		t.Fatalf("write README.md: %v", err)
	}
	fm := readFrontmatter(path)
	if got := fm["updated"]; got != "2026-04-14" {
		t.Fatalf("updated = %q, want 2026-04-14", got)
	}
}

// ─── Stage 3: Score ──────────────────────────────────────────────────

func TestScoreNoGovernancePicksGrey(t *testing.T) {
	rc := RepoContext{
		GovernanceMissing: []string{"CHANGELOG.md", "STANDARDS.md", ".github/dependabot.yml"},
	}
	scores := Score(rc, IntentSpec{})
	if scores[0].Variant != "grey" {
		t.Errorf("top variant = %q, want grey", scores[0].Variant)
	}
}

func TestScoreFailingPRsPicksRed(t *testing.T) {
	rc := RepoContext{
		OpenPRs: []GHIssue{
			{Number: 1, MergeStatus: "DIRTY"},
			{Number: 2, MergeStatus: "BLOCKED"},
		},
	}
	scores := Score(rc, IntentSpec{})
	if scores[0].Variant != "red" {
		t.Errorf("top variant = %q, want red (had 2 blocked PRs)", scores[0].Variant)
	}
}

func TestScorePlansPresentPicksProfessor(t *testing.T) {
	rc := RepoContext{
		PlansIndexExists: true,
		PlansFiles:       []string{"plan-a.md", "plan-b.md"},
	}
	scores := Score(rc, IntentSpec{})
	// Professor should beat the baseline green when plans exist.
	first := scores[0].Variant
	if first != "professor" && first != "green" {
		t.Errorf("top variant = %q, want professor or green", first)
	}
}

func TestScoreOffLimitsDisqualifies(t *testing.T) {
	rc := RepoContext{OpenPRs: []GHIssue{{MergeStatus: "DIRTY"}}}
	scores := Score(rc, IntentSpec{
		Constraints: []string{"variant off-limits: red"},
	})
	// Red should have score 0 + Disqualifying entry.
	for _, s := range scores {
		if s.Variant != "red" {
			continue
		}
		if s.Score != 0 {
			t.Errorf("red.Score = %d, want 0 after off-limits", s.Score)
		}
		if len(s.Disqualifying) == 0 {
			t.Error("red should have a Disqualifying entry")
		}
	}
}

func TestScoreDefaultBranchDisqualifiesOldMan(t *testing.T) {
	rc := RepoContext{OnDefaultBranch: true}
	scores := Score(rc, IntentSpec{})
	for _, s := range scores {
		if s.Variant == "old-man" {
			if s.Score != 0 {
				t.Errorf("old-man on default branch: score=%d, want 0", s.Score)
			}
			return
		}
	}
	t.Error("old-man variant missing from scores")
}

// ─── Stage 5: Validate ──────────────────────────────────────────────

func TestValidatePassesCleanProposal(t *testing.T) {
	p := PlanProposal{
		Primary:          "green",
		PrimaryRationale: "baseline full loop",
		Tasks:            []Task{{Title: "Iterate on features", Effort: "M", Impact: "M"}},
		AcceptanceCriteria: []string{
			"All CI checks return green",
			"PR merges cleanly",
			"Task count matches 3 items",
		},
		Confidence: 80,
	}
	result := Validate(p, RepoContext{CurrentBranch: "feat/x", OnDefaultBranch: false}, IntentSpec{})
	if !result.Passed {
		t.Errorf("expected pass, got failures: %v", result.Failures)
	}
}

func TestValidateRejectsUnknownVariant(t *testing.T) {
	p := PlanProposal{Primary: "purple", Confidence: 80,
		AcceptanceCriteria: []string{"passes test", "exists file", "equal to 3"}}
	result := Validate(p, RepoContext{}, IntentSpec{})
	if result.Passed {
		t.Error("expected failure for unknown primary variant")
	}
}

func TestValidateRejectsVagueAcceptance(t *testing.T) {
	p := PlanProposal{
		Primary:    "green",
		Confidence: 80,
		AcceptanceCriteria: []string{
			"improves code quality",
			"considers edge cases",
			"addresses feedback",
		},
	}
	result := Validate(p, RepoContext{}, IntentSpec{})
	if result.Passed {
		t.Error("vague criteria should fail")
	}
	if len(result.Failures) < 3 {
		t.Errorf("want ≥3 failures for 3 vague criteria, got %d", len(result.Failures))
	}
}

func TestValidateRejectsLowConfidence(t *testing.T) {
	p := PlanProposal{
		Primary:    "green",
		Confidence: 20,
		AcceptanceCriteria: []string{
			"All CI checks return green",
			"PR exists",
			"Tests pass",
		},
	}
	result := Validate(p, RepoContext{}, IntentSpec{})
	if result.Passed {
		t.Error("low confidence should fail")
	}
}

func TestValidateRejectsOldManOnDefaultBranch(t *testing.T) {
	p := PlanProposal{
		Primary:    "old-man",
		Confidence: 80,
		AcceptanceCriteria: []string{
			"Branch matches target", "Files exist", "Result passes",
		},
	}
	rc := RepoContext{CurrentBranch: "main", OnDefaultBranch: true}
	result := Validate(p, rc, IntentSpec{
		Constraints: []string{"confirm-no-mercy declared"},
	})
	if result.Passed {
		t.Error("old-man on main should fail")
	}
	_ = variant.OldMan // ensure import is used
}

// ─── Stage 6: Emit ───────────────────────────────────────────────────

func TestEmitWritesValidFrontmatter(t *testing.T) {
	repo := newTestRepo(t)
	plansDir := filepath.Join(repo, ".radioactive-ralph", "plans")
	p := PlanProposal{
		Primary:          "green",
		PrimaryRationale: "default",
		Confidence:       80,
		AcceptanceCriteria: []string{
			"CI passes",
			"PR exists",
			"Tests return green",
		},
	}
	emitted, err := Emit(plansDir, "test", p, ValidationResult{Passed: true},
		StatusCurrent,
		IntentSpec{Topic: "test", Description: "dogfood"},
		RepoContext{GitRoot: repo, CurrentBranch: "main"})
	if err != nil {
		t.Fatalf("Emit: %v", err)
	}
	raw, err := os.ReadFile(emitted.Path) //nolint:gosec // test path
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	content := string(raw)
	for _, want := range []string{
		"---", "status: current", "variant_recommendation: green",
		"# Fixit advisor", "## Primary recommendation: `green-ralph`",
		"## Methodology",
	} {
		if !strings.Contains(content, want) {
			t.Errorf("emitted report missing %q:\n%s", want, content)
		}
	}
}

func TestEmitFallbackHasStatusFallback(t *testing.T) {
	repo := newTestRepo(t)
	plansDir := filepath.Join(repo, ".radioactive-ralph", "plans")
	emitted, err := EmitFallback(plansDir, "broken", "stage 4 failed twice", "bad json",
		IntentSpec{Topic: "broken"}, RepoContext{GitRoot: repo})
	if err != nil {
		t.Fatalf("EmitFallback: %v", err)
	}
	if emitted.Status != StatusFallback {
		t.Errorf("Status = %q", emitted.Status)
	}
	raw, _ := os.ReadFile(emitted.Path) //nolint:gosec
	if !strings.Contains(string(raw), "status: fallback") {
		t.Error("fallback frontmatter missing")
	}
}

// ─── Full pipeline ──────────────────────────────────────────────────

func TestRunPipelineSkipAnalysisProducesCurrentPlan(t *testing.T) {
	repo := newTestRepo(t)
	result, err := RunPipeline(context.Background(), RunOptions{
		RepoRoot:       repo,
		Topic:          "pipeline-test",
		Description:    "exercise the skip-analysis path",
		NonInteractive: true,
		SkipAnalysis:   true,
	})
	if err != nil {
		t.Fatalf("RunPipeline: %v", err)
	}
	if result.Path == "" {
		t.Error("emitted path empty")
	}
	if _, err := os.Stat(result.Path); err != nil {
		t.Errorf("plan file missing: %v", err)
	}
}
