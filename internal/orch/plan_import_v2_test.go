package orch

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jbcom/radioactive-ralph/internal/store"
)

func TestImportV2RejectsFilesystemAndAcceptanceBeforeAnyRows(t *testing.T) {
	tests := map[string]func(t *testing.T, root string, hash string) string{
		"input hash": func(_ *testing.T, _ string, _ string) string {
			return v2RuntimeStep("task.bad", nil, []string{"claude"}, nil, fmt.Sprintf("%064d", 0))
		},
		"output symlink": func(t *testing.T, root, hash string) string {
			outside := t.TempDir()
			if err := os.Symlink(outside, filepath.Join(root, "out")); err != nil {
				t.Fatal(err)
			}
			return v2RuntimeStep("task.bad", nil, []string{"claude"}, nil, hash)
		},
		"missing acceptance": func(_ *testing.T, _ string, hash string) string {
			return strings.Replace(
				v2RuntimeStep("task.bad", nil, []string{"claude"}, nil, hash),
				"  `accept-file: contract.md`\n\n", "", 1,
			)
		},
		"duplicate acceptance": func(_ *testing.T, _ string, hash string) string {
			return strings.Replace(
				v2RuntimeStep("task.bad", nil, []string{"claude"}, nil, hash),
				"`accept-file: contract.md`", "`accept-file: contract.md` `accept-file: contract.md`", 1,
			)
		},
		"unsafe acceptance file": func(_ *testing.T, _ string, hash string) string {
			return strings.Replace(
				v2RuntimeStep("task.bad", nil, []string{"claude"}, nil, hash),
				"`accept-file: contract.md`", "`accept-file: ../escape`", 1,
			)
		},
		"empty acceptance": func(_ *testing.T, _ string, hash string) string {
			return strings.Replace(
				v2RuntimeStep("task.bad", nil, []string{"claude"}, nil, hash),
				"`accept-file: contract.md`", "`accept-file:`", 1,
			)
		},
		"uncalibrated measured capability": func(_ *testing.T, _ string, hash string) string {
			return strings.Replace(
				v2RuntimeStep("task.bad", nil, []string{"claude"}, nil, hash),
				`"requires":["local-agent"]`,
				`"requires":["local-agent","quality.graph-reasoning"]`, 1,
			)
		},
	}
	for name, build := range tests {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			st := newTestStore(t)
			root := t.TempDir()
			projectID, err := st.CreateProject(ctx, "reject-"+strings.ReplaceAll(name, " ", "-"), []store.Fingerprint{{
				Kind: store.FingerprintKindAbsPath, Value: root,
			}})
			if err != nil {
				t.Fatal(err)
			}
			input := []byte("contract")
			if err := os.WriteFile(filepath.Join(root, "contract.md"), input, 0o600); err != nil {
				t.Fatal(err)
			}
			md := "# Reject\n\n" + build(t, root, fmt.Sprintf("%x", sha256.Sum256(input)))
			o := New(st, WithConstrainedBindingResolver(constrainedTestPool("claude")))
			if _, err := o.ImportPlan(ctx, ImportPlanOpts{
				ProjectID: projectID, Slug: "reject", Title: "Reject", Markdown: md,
			}); err == nil {
				t.Fatal("ImportPlan accepted invalid v2 contract")
			}
			var plans, tasks int
			if err := st.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM plans`).Scan(&plans); err != nil {
				t.Fatal(err)
			}
			if err := st.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM tasks`).Scan(&tasks); err != nil {
				t.Fatal(err)
			}
			if plans != 0 || tasks != 0 {
				t.Fatalf("rejected import left plans=%d tasks=%d", plans, tasks)
			}
		})
	}
}

func TestImportLegacyStillAllowsJudgmentOnlyAcceptance(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)
	projectID, err := st.CreateProject(ctx, "legacy-acceptance", nil)
	if err != nil {
		t.Fatal(err)
	}
	o := New(st)
	if _, err := o.ImportPlan(ctx, ImportPlanOpts{
		ProjectID: projectID, Slug: "legacy", Title: "Legacy",
		Markdown: "# Legacy\n\n- human judgment remains unchanged\n",
	}); err != nil {
		t.Fatalf("legacy import: %v", err)
	}
}
