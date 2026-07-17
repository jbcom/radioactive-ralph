package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/jbcom/radioactive-ralph/internal/ipc"
	"github.com/jbcom/radioactive-ralph/internal/plan"
	"github.com/jbcom/radioactive-ralph/internal/store"
	"github.com/jbcom/radioactive-ralph/internal/supervisor"
	"github.com/jbcom/radioactive-ralph/internal/xdg"
	"github.com/spf13/cobra"
)

// newPlanCmd builds the `plan` subcommand group: the production path that
// creates and lists plans against the current project. Before this existed
// the runtime had no user-facing way to seed a plan at all — a project could
// be initialized and its read-only TUI opened, but nothing ever called
// store.CreatePlan, so the supervisor's dispatch loop had nothing to drive.
// `plan import` closes that gap; `plan ls` lets an operator confirm the
// result.
func newPlanCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "plan",
		Short: "Create and inspect plans for the current project",
	}
	cmd.AddCommand(newPlanImportCmd())
	cmd.AddCommand(newPlanLsCmd())
	return cmd
}

// planTitleFallback is the title used when a plan markdown has no level-1
// heading: the file's base name sans extension. Title/slug derivation itself
// lives in internal/plan (plan.Title/plan.Slug) so the CLI and the supervisor's
// plan-import handler produce identical results.
func planTitleFallback(planPath string) string {
	base := filepath.Base(planPath)
	return strings.TrimSuffix(base, filepath.Ext(base))
}

func newPlanImportCmd() *cobra.Command {
	var slug string
	cmd := &cobra.Command{
		Use:   "import <plan.md>",
		Short: "Import a markdown plan file and activate it for the current project",
		Long: "Reads a markdown plan file, creates a plan row for the current " +
			"project from it, and marks the plan active. A running supervisor's " +
			"dispatch loop then drives its ready steps; if no supervisor is " +
			"running the plan is queued until one starts. The plan title is " +
			"the file's first level-1 heading (falling back to the filename); " +
			"pass --slug to override the derived slug.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runPlanImport(cmd.Context(), cmd, args[0], slug)
		},
	}
	cmd.Flags().StringVar(&slug, "slug", "", "explicit plan slug (default: derived from title/filename)")
	return cmd
}

func runPlanImport(ctx context.Context, cmd *cobra.Command, planPath, slug string) error {
	raw, err := os.ReadFile(planPath) //nolint:gosec // operator-supplied plan path is the command's entire purpose
	if err != nil {
		return fmt.Errorf("read plan file: %w", err)
	}
	markdown := string(raw)
	if strings.TrimSpace(markdown) == "" {
		return fmt.Errorf("plan file %s is empty", planPath)
	}

	title := plan.Title(markdown, planTitleFallback(planPath))
	if slug == "" {
		slug = plan.Slug(title)
	}

	stateRoot, err := xdg.StateRoot()
	if err != nil {
		return fmt.Errorf("resolve state root: %w", err)
	}
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("resolve cwd: %w", err)
	}
	projectID, err := ensureProjectKnown(ctx, cmd, stateRoot, cwd)
	if err != nil {
		return err
	}

	// Prefer the supervisor's plan-import IPC command when one is reachable so
	// the supervisor is the single writer of record (same code path the GUI
	// uses). Fall back to a direct store write only when offline.
	if client, ferr := supervisor.Find(stateRoot); ferr == nil {
		defer func() { _ = client.Close() }()
		reply, cerr := client.PlanImport(ctx, ipc.PlanImportArgs{
			Markdown: markdown, Slug: slug, Title: title, Project: projectID,
		})
		if cerr != nil {
			return fmt.Errorf("import plan via supervisor: %w", cerr)
		}
		fmt.Printf("radioactive_ralph: imported plan %q (%s) — active\n", reply.Title, reply.Slug)
		return nil
	}

	st, err := store.Open(ctx, store.Options{DSN: store.DSN(storeDBPath(stateRoot))})
	if err != nil {
		return fmt.Errorf("open store: %w", err)
	}
	defer func() { _ = st.Close() }()

	planID, err := st.CreatePlan(ctx, store.CreatePlanOpts{
		ProjectID:      projectID,
		Slug:           slug,
		Title:          title,
		SourceMarkdown: markdown,
	})
	if err != nil {
		return fmt.Errorf("create plan: %w", err)
	}
	// A freshly imported plan is meant to run: activate it so the
	// supervisor's periodic dispatch loop picks it up (ListPlans with an
	// empty filter returns active+paused plans).
	if err := st.SetPlanStatus(ctx, planID, store.PlanStatusActive); err != nil {
		return fmt.Errorf("activate plan: %w", err)
	}

	fmt.Printf("radioactive_ralph: imported plan %q (%s) — active\n", title, slug)
	// "Active" only means "eligible for dispatch"; nothing actually drives it
	// until a supervisor is running. Don't imply work has started when it
	// hasn't — tell the operator how to start the supervisor if none is up.
	if client, err := supervisor.Find(stateRoot); err != nil {
		fmt.Fprintln(os.Stderr, "note: no supervisor is running, so the plan is queued but not yet being driven.")
		fmt.Fprintln(os.Stderr, "      start one with:  radioactive_ralph service install   (or: radioactive_ralph --supervisor)")
	} else {
		_ = client.Close()
	}
	return nil
}

func newPlanLsCmd() *cobra.Command {
	var all bool
	cmd := &cobra.Command{
		Use:   "ls",
		Short: "List plans for the current project",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runPlanLs(cmd.Context(), cmd, all)
		},
	}
	cmd.Flags().BoolVar(&all, "all", false, "list plans in every status, not just active/paused")
	return cmd
}

func runPlanLs(ctx context.Context, cmd *cobra.Command, all bool) error {
	stateRoot, err := xdg.StateRoot()
	if err != nil {
		return fmt.Errorf("resolve state root: %w", err)
	}
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("resolve cwd: %w", err)
	}
	projectID, err := ensureProjectKnown(ctx, cmd, stateRoot, cwd)
	if err != nil {
		return err
	}

	st, err := store.Open(ctx, store.Options{DSN: store.DSN(storeDBPath(stateRoot))})
	if err != nil {
		return fmt.Errorf("open store: %w", err)
	}
	defer func() { _ = st.Close() }()

	var statuses []store.PlanStatus
	if all {
		statuses = []store.PlanStatus{
			store.PlanStatusDraft, store.PlanStatusActive, store.PlanStatusPaused,
			store.PlanStatusDone, store.PlanStatusFailedPartial,
			store.PlanStatusArchived, store.PlanStatusAbandoned,
		}
	}
	plans, err := st.ListPlans(ctx, projectID, statuses)
	if err != nil {
		return fmt.Errorf("list plans: %w", err)
	}
	if len(plans) == 0 {
		fmt.Println("radioactive_ralph: no plans for this project")
		return nil
	}
	for _, p := range plans {
		fmt.Printf("%-10s  %-24s  %s\n", p.Status, p.Slug, p.Title)
	}
	return nil
}
