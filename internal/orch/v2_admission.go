package orch

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/jbcom/radioactive-ralph/internal/plan"
)

func validateV2Filesystem(projectDir string, metadata *plan.TaskMetadata) error {
	if err := validateV2Inputs(projectDir, metadata); err != nil {
		return err
	}
	for _, output := range metadata.Outputs {
		if _, err := secureProjectPath(projectDir, output.Path, false); err != nil {
			return fmt.Errorf("output %s: %w", output.Path, err)
		}
	}
	return nil
}

func validateV2Inputs(projectDir string, metadata *plan.TaskMetadata) error {
	for _, input := range metadata.Inputs {
		path, err := secureProjectPath(projectDir, input.Path, true)
		if err != nil {
			return fmt.Errorf("input %s: %w", input.Path, err)
		}
		raw, err := os.ReadFile(path) //nolint:gosec // path is contained below project root
		if err != nil {
			return fmt.Errorf("input %s: %w", input.Path, err)
		}
		actual := fmt.Sprintf("%x", sha256.Sum256(raw))
		if actual != input.SHA256 {
			return fmt.Errorf(
				"input %s sha256 mismatch: expected %s, got %s",
				input.Path, input.SHA256, actual,
			)
		}
	}
	return nil
}

// verifyV2CompletionFilesystem is the strict v2 post-turn boundary. It
// rechecks immutable input bytes and proves every declared output currently
// exists beneath the project root before mechanical acceptance runs. This is
// intentionally not isolation, attempt attribution, undeclared-write
// exclusion, a content manifest, CAS publication, or determinism.
func verifyV2CompletionFilesystem(
	projectDir string,
	metadata *plan.TaskMetadata,
	acceptanceJSON string,
) error {
	if err := validateV2Inputs(projectDir, metadata); err != nil {
		return err
	}
	for _, output := range metadata.Outputs {
		if _, err := secureProjectPath(projectDir, output.Path, true); err != nil {
			return fmt.Errorf("output %s: %w", output.Path, err)
		}
	}
	var acceptance Acceptance
	if err := json.Unmarshal([]byte(acceptanceJSON), &acceptance); err != nil {
		return fmt.Errorf("decode acceptance: %w", err)
	}
	if acceptance.FileExists != "" {
		if _, err := secureProjectPath(projectDir, acceptance.FileExists, true); err != nil {
			return fmt.Errorf("accept-file %s: %w", acceptance.FileExists, err)
		}
	}
	return nil
}

func secureProjectPath(projectDir, relative string, mustExist bool) (string, error) {
	root, err := filepath.Abs(projectDir)
	if err != nil {
		return "", fmt.Errorf("resolve project root: %w", err)
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return "", fmt.Errorf("resolve project root symlinks: %w", err)
	}
	candidate := filepath.Join(root, filepath.Clean(relative))
	if !pathContained(root, candidate) {
		return "", fmt.Errorf("path escapes project root")
	}

	var resolved string
	if mustExist {
		resolved, err = filepath.EvalSymlinks(candidate)
	} else {
		resolved, err = resolveThroughExistingAncestor(candidate)
	}
	if err != nil {
		return "", fmt.Errorf("resolve symlinks: %w", err)
	}
	if !pathContained(root, resolved) {
		return "", fmt.Errorf("symlink escapes project root")
	}
	if mustExist {
		return resolved, nil
	}
	return candidate, nil
}

func resolveThroughExistingAncestor(path string) (string, error) {
	ancestor := path
	var suffix []string
	for {
		if _, err := os.Lstat(ancestor); err == nil {
			resolved, err := filepath.EvalSymlinks(ancestor)
			if err != nil {
				return "", err
			}
			for i := len(suffix) - 1; i >= 0; i-- {
				resolved = filepath.Join(resolved, suffix[i])
			}
			return resolved, nil
		} else if !os.IsNotExist(err) {
			return "", err
		}
		parent := filepath.Dir(ancestor)
		if parent == ancestor {
			return "", fmt.Errorf("no existing ancestor")
		}
		suffix = append(suffix, filepath.Base(ancestor))
		ancestor = parent
	}
}

func pathContained(root, candidate string) bool {
	relative, err := filepath.Rel(root, candidate)
	return err == nil && relative != ".." &&
		!strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

func pathWithinDeclaredOutput(candidate string, outputs []plan.TaskOutput) bool {
	candidate = filepath.Clean(candidate)
	for _, output := range outputs {
		base := filepath.Clean(output.Path)
		relative, err := filepath.Rel(base, candidate)
		if err == nil && relative != ".." &&
			!strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return true
		}
	}
	return false
}
