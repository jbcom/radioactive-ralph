package fixit

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/jbcom/radioactive-ralph/internal/inventory"
)

// Explore runs Stage 2 — deterministic repo exploration. Every field
// in the returned RepoContext comes from a shell-out (git, gh, file
// walk) — zero LLM calls.
func Explore(ctx context.Context, repoRoot string) (RepoContext, error) {
	rc := RepoContext{GitRoot: repoRoot, LangCounts: map[string]int{}}

	// Git basics.
	rc.CurrentBranch = strings.TrimSpace(runOrEmpty(ctx, repoRoot,
		"git", "rev-parse", "--abbrev-ref", "HEAD"))
	rc.DefaultBranch = inferDefaultBranch(ctx, repoRoot)
	rc.OnDefaultBranch = rc.CurrentBranch != "" && rc.CurrentBranch == rc.DefaultBranch
	rc.Commits = parseGitLog(runOrEmpty(ctx, repoRoot,
		"git", "log", "--pretty=format:%h|%s|%an|%aI", "-50"))

	// Docs scan.
	docs, stale, missing := scanDocs(repoRoot)
	rc.DocsPresent = docs
	rc.DocsStale = stale
	rc.DocsMissing = missing

	// Plans tree.
	rc.PlansDir = filepath.Join(repoRoot, ".radioactive-ralph", "plans")
	rc.PlansIndexExists, rc.PlansIndexFM, rc.PlansFiles = readPlansIndex(rc.PlansDir)

	// gh PR / issue scrape (skipped silently when gh is missing or
	// not authenticated).
	rc.GHAuthenticated = ghAvailable(ctx)
	if rc.GHAuthenticated {
		rc.OpenPRs = ghIssues(ctx, repoRoot, "pr", "")
		rc.OpenIssues = ghIssues(ctx, repoRoot, "issue", "")
		rc.AIWelcomeIssues = ghIssues(ctx, repoRoot, "issue", "ai-welcome")
	}

	// Inventory snapshot.
	rc.Inventory = takeInventorySnapshot()

	// Language mix.
	rc.LangCounts = countLangs(repoRoot)

	// Governance gaps — known canonical files we expect every repo to
	// have. Missing entries become a signal for the scorer.
	rc.GovernanceMissing = listGovernanceGaps(repoRoot)

	return rc, nil
}

// runOrEmpty execs name+args and returns trimmed stdout, or empty
// string on any failure (deterministic exploration must never error
// out — missing tools just mean fewer signals).
func runOrEmpty(ctx context.Context, cwd string, name string, args ...string) string {
	cmd := exec.CommandContext(ctx, name, args...) //nolint:gosec // args are hardcoded shell-outs
	cmd.Dir = cwd
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return string(out)
}

// inferDefaultBranch parses `git symbolic-ref refs/remotes/origin/HEAD`
// then falls back to a hardcoded list when origin's HEAD isn't set.
func inferDefaultBranch(ctx context.Context, repoRoot string) string {
	if out := runOrEmpty(ctx, repoRoot, "git", "symbolic-ref",
		"refs/remotes/origin/HEAD"); out != "" {
		// Output looks like "refs/remotes/origin/main" — strip prefix.
		if idx := strings.LastIndex(out, "/"); idx >= 0 {
			return strings.TrimSpace(out[idx+1:])
		}
	}
	for _, candidate := range []string{"main", "master", "trunk"} {
		if runOrEmpty(ctx, repoRoot, "git", "rev-parse", "--verify",
			"refs/heads/"+candidate) != "" {
			return candidate
		}
	}
	return ""
}

// parseGitLog turns the pipe-delimited oneline output into a typed slice.
func parseGitLog(raw string) []GitCommit {
	var out []GitCommit
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "|", 4)
		if len(parts) != 4 {
			continue
		}
		out = append(out, GitCommit{
			SHA: parts[0], Subject: parts[1],
			Author: parts[2], DateISO: parts[3],
		})
	}
	return out
}

// scanDocs walks docs/ and the repo root for .md files, parses their
// frontmatter, and flags stale/missing canonical docs.
func scanDocs(repoRoot string) (present []DocFile, stale []string, missing []string) {
	canonical := []string{
		"README.md", "CHANGELOG.md", "STANDARDS.md",
		"docs/ARCHITECTURE.md", "docs/STATE.md", "docs/TESTING.md",
	}
	seen := map[string]bool{}

	// Walk root + docs/ for .md files.
	walkRoots := []string{repoRoot, filepath.Join(repoRoot, "docs")}
	for _, root := range walkRoots {
		_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return nil //nolint:nilerr // best-effort
			}
			if !strings.HasSuffix(path, ".md") {
				return nil
			}
			rel, _ := filepath.Rel(repoRoot, path)
			fm := readFrontmatter(path)
			updated := fm["updated"]
			if updated == "" {
				if info, err := os.Stat(path); err == nil {
					updated = info.ModTime().UTC().Format("2006-01-02")
				}
			}
			doc := DocFile{Path: rel, Frontmatter: fm, UpdatedISO: updated}
			present = append(present, doc)
			seen[rel] = true
			if isStale(fm, updated) {
				stale = append(stale, rel)
			}
			return nil
		})
	}

	for _, c := range canonical {
		if !seen[c] {
			missing = append(missing, c)
		}
	}
	return
}

// readFrontmatter parses a YAML frontmatter block (--- … ---) into a
// flat map. Only top-level scalar key:value pairs are extracted; nested
// structures are ignored. Good enough for the fields we care about
// (title, updated, status, domain).
func readFrontmatter(path string) map[string]string {
	out := map[string]string{}
	raw, err := os.ReadFile(path) //nolint:gosec // explored-path
	if err != nil {
		return out
	}
	text := string(raw)
	if !strings.HasPrefix(text, "---") {
		return out
	}
	end := strings.Index(text[3:], "\n---")
	if end < 0 {
		return out
	}
	block := text[3 : 3+end]
	for _, line := range strings.Split(block, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		idx := strings.Index(line, ":")
		if idx < 0 {
			continue
		}
		key := strings.TrimSpace(line[:idx])
		val := strings.TrimSpace(line[idx+1:])
		val = strings.Trim(val, `"'`)
		out[key] = val
	}
	return out
}

// isStale returns true when frontmatter says status: stale OR the
// updated date is older than 30 days.
func isStale(fm map[string]string, updated string) bool {
	if fm["status"] == "stale" || fm["status"] == "archived" {
		return true
	}
	if updated == "" {
		return false
	}
	t, err := time.Parse("2006-01-02", updated)
	if err != nil {
		return false
	}
	return time.Since(t) > 30*24*time.Hour
}

// readPlansIndex returns whether plans/index.md exists, its
// frontmatter, and the list of plan files referenced in its body.
func readPlansIndex(plansDir string) (exists bool, fm map[string]string, files []string) {
	indexPath := filepath.Join(plansDir, "index.md")
	raw, err := os.ReadFile(indexPath) //nolint:gosec // workspace path
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil, nil
	}
	if err != nil {
		return false, nil, nil
	}
	exists = true
	fm = readFrontmatter(indexPath)
	// Find markdown links to other .md files in this directory.
	for _, line := range strings.Split(string(raw), "\n") {
		// Crude: looks for .md filenames after "(" or whitespace.
		for _, tok := range strings.Fields(line) {
			tok = strings.Trim(tok, "(),.;\"")
			if !strings.HasSuffix(tok, ".md") || tok == "index.md" {
				continue
			}
			files = append(files, tok)
		}
	}
	return
}

// ghAvailable reports whether `gh auth status` succeeds. Soft check;
// failures yield empty PR/issue lists rather than exploration errors.
func ghAvailable(ctx context.Context) bool {
	cmd := exec.CommandContext(ctx, "gh", "auth", "status") //nolint:gosec // fixed args
	if err := cmd.Run(); err != nil {
		return false
	}
	return true
}

// ghIssues runs `gh pr list` or `gh issue list` (kind = "pr" or
// "issue") with optional label filter. Returns empty on any failure.
func ghIssues(ctx context.Context, cwd, kind, label string) []GHIssue {
	args := []string{kind, "list", "--state", "open", "--json",
		"number,title,isDraft,state,author,labels"}
	if kind == "pr" {
		args = append(args, "--json", "mergeStateStatus")
	}
	if label != "" {
		args = append(args, "--label", label)
	}
	cmd := exec.CommandContext(ctx, "gh", args...) //nolint:gosec // fixed prefix
	cmd.Dir = cwd
	out, err := cmd.Output()
	if err != nil {
		return nil
	}
	var raw []map[string]any
	if err := json.Unmarshal(out, &raw); err != nil {
		return nil
	}
	var result []GHIssue
	for _, m := range raw {
		issue := GHIssue{
			Number: intFrom(m["number"]),
			Title:  strFrom(m["title"]),
			Draft:  boolFrom(m["isDraft"]),
			State:  strFrom(m["state"]),
		}
		if a, ok := m["author"].(map[string]any); ok {
			issue.Author = strFrom(a["login"])
		}
		if labels, ok := m["labels"].([]any); ok {
			for _, l := range labels {
				if lm, ok := l.(map[string]any); ok {
					issue.Labels = append(issue.Labels, strFrom(lm["name"]))
				}
			}
		}
		issue.MergeStatus = strFrom(m["mergeStateStatus"])
		result = append(result, issue)
	}
	return result
}

func strFrom(v any) string { s, _ := v.(string); return s }
func boolFrom(v any) bool  { b, _ := v.(bool); return b }
func intFrom(v any) int    { f, _ := v.(float64); return int(f) }

// takeInventorySnapshot calls inventory.Discover and flattens the
// result into the simpler InventorySnapshot shape Stage 4 needs.
func takeInventorySnapshot() InventorySnapshot {
	inv, _ := inventory.Discover(inventory.Options{})
	snap := InventorySnapshot{}
	for _, s := range inv.Skills {
		snap.Skills = append(snap.Skills, s.FullName())
	}
	for _, m := range inv.MCPServers {
		snap.MCPs = append(snap.MCPs, m.Name)
	}
	for _, a := range inv.Agents {
		snap.Agents = append(snap.Agents, a.Name)
	}
	return snap
}

// countLangs walks the repo and counts files by extension. Skips
// node_modules, vendor, .git, and the operator's reference/ tree.
func countLangs(repoRoot string) map[string]int {
	skip := map[string]bool{
		".git": true, "node_modules": true, "vendor": true,
		"reference": true, "dist": true, "build": true,
	}
	out := map[string]int{}
	_ = filepath.WalkDir(repoRoot, func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil //nolint:nilerr // best-effort
		}
		if d.IsDir() {
			if skip[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		ext := strings.ToLower(filepath.Ext(d.Name()))
		if ext == "" {
			return nil
		}
		out[ext]++
		return nil
	})
	return out
}

// listGovernanceGaps returns the canonical files we expect every repo
// to have, missing.
func listGovernanceGaps(repoRoot string) []string {
	expected := []string{
		"CHANGELOG.md", "STANDARDS.md", ".github/dependabot.yml",
		".github/workflows/ci.yml",
	}
	var missing []string
	for _, p := range expected {
		full := filepath.Join(repoRoot, p)
		if _, err := os.Stat(full); errors.Is(err, fs.ErrNotExist) {
			missing = append(missing, p)
		}
	}
	return missing
}
