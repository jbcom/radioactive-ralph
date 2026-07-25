package orch

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/jbcom/radioactive-ralph/internal/plan"
)

func TestValidateV2FilesystemChecksExactHash(t *testing.T) {
	root := t.TempDir()
	raw := []byte("contract\n")
	if err := os.WriteFile(filepath.Join(root, "contract.md"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	metadata := &plan.TaskMetadata{Inputs: []plan.TaskInput{{
		Path: "contract.md", SHA256: fmt.Sprintf("%x", sha256.Sum256(raw)),
	}}}
	if err := validateV2Filesystem(root, metadata); err != nil {
		t.Fatalf("exact input rejected: %v", err)
	}
	metadata.Inputs[0].SHA256 = fmt.Sprintf("%064d", 0)
	if err := validateV2Filesystem(root, metadata); err == nil {
		t.Fatal("hash mismatch accepted")
	}
}

func TestValidateV2FilesystemRejectsSymlinkEscape(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret"), []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "escape")); err != nil {
		t.Fatal(err)
	}
	metadata := &plan.TaskMetadata{Inputs: []plan.TaskInput{{
		Path: "escape/secret", SHA256: fmt.Sprintf("%x", sha256.Sum256([]byte("secret"))),
	}}}
	if err := validateV2Filesystem(root, metadata); err == nil {
		t.Fatal("input symlink escape accepted")
	}
	metadata.Inputs = nil
	metadata.Outputs = []plan.TaskOutput{{Path: "escape/new", Mode: "exclusive"}}
	if err := validateV2Filesystem(root, metadata); err == nil {
		t.Fatal("output symlink escape accepted")
	}
}
