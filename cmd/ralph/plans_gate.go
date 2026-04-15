package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// requirePlansIndex enforces the plans-first discipline: every variant
// except fixit refuses to run unless .radioactive-ralph/plans/index.md
// exists and has minimum YAML frontmatter.
//
// This is the M2 shape — a strict file-exists + frontmatter-present
// check. Full frontmatter validation (status, updated, domain,
// variant_recommendation) lives in a dedicated plans package added
// alongside fixit's advisor-mode write path.
func requirePlansIndex(repoRoot string) error {
	indexPath := filepath.Join(repoRoot, ".radioactive-ralph", "plans", "index.md")
	raw, err := os.ReadFile(indexPath) //nolint:gosec // repo-owned path
	if errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf(
			"plans-first discipline: %s not found — run `ralph run --variant fixit --advise` to scaffold it",
			indexPath,
		)
	}
	if err != nil {
		return fmt.Errorf("read %s: %w", indexPath, err)
	}
	content := string(raw)
	if !strings.HasPrefix(strings.TrimSpace(content), "---") {
		return fmt.Errorf(
			"%s is missing YAML frontmatter — run `ralph init --refresh` to regenerate",
			indexPath,
		)
	}
	return nil
}
