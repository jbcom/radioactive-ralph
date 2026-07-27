package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/jbcom/radioactive-ralph/internal/ipc"
	"github.com/jbcom/radioactive-ralph/internal/observe"
	"github.com/jbcom/radioactive-ralph/internal/plan"
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
			"project from it, and marks the plan active. The supervisor's " +
			"dispatch loop then drives its ready steps. A RUNNING SUPERVISOR IS " +
			"REQUIRED: the supervisor is the single writer of record for the " +
			"store, so import refuses rather than writing a plan nothing is " +
			"driving. The plan title is the file's first level-1 heading " +
			"(falling back to the filename); pass --slug to override the " +
			"derived slug.",
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
	if err := plan.ValidateForImport(raw); err != nil {
		return fmt.Errorf("plan file %s is invalid: %w", planPath, err)
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

	// The supervisor is the SINGLE writer of record for the store. There is no
	// offline direct-write fallback: AGENTS.md specifies that the client
	// "connects to the supervisor via its socket, initializes project config,
	// and renders a read-only view. It refuses to run without a supervisor."
	// A fallback that opened the DB directly would make the client a second
	// writer to a supervisor-owned database — the exact ownership split the
	// one-binary/supervisor-owned-state architecture exists to prevent — and it
	// silently produced a plan nothing was driving.
	client, err := supervisor.Find(stateRoot)
	if err != nil {
		return fmt.Errorf(
			"%w: plan import needs a running supervisor; start one with: %s",
			errNoSupervisorListening, supervisorStartHint())
	}
	defer func() { _ = client.Close() }()

	reply, err := client.PlanImport(ctx, ipc.PlanImportArgs{
		Markdown: markdown, Slug: slug, Title: title, Project: projectID,
	})
	if err != nil {
		return fmt.Errorf("import plan via supervisor: %w", err)
	}
	fmt.Printf("radioactive_ralph: imported plan %q (%s) — active\n", reply.Title, reply.Slug)
	return nil
}

func newPlanLsCmd() *cobra.Command {
	var all bool
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "ls",
		Short: "List plans for the current project",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runPlanLs(cmd.Context(), cmd, all, asJSON)
		},
	}
	cmd.Flags().BoolVar(&all, "all", false, "list plans in every status, not just active/paused")
	cmd.Flags().BoolVar(&asJSON, "json", false, "emit JSONL, one plan per line, for machine consumption")
	return cmd
}

// planSource is the plan-listing read boundary. It exists so the command is
// testable without a live supervisor, and so plan_cmd.go cannot reach SQLite —
// the architecture gate in internal/observe/client_boundary_test.go enforces
// that this file never imports the store package.
type planSource interface {
	ListPlans(ctx context.Context, projectID string, all bool) ([]observe.Plan, error)
}

type livePlanSource struct{ stateRoot string }

// ListPlans reads the project's plans through the supervisor's versioned query
// surface.
//
// It PAGINATES to exhaustion before filtering. The snapshot caps a page at
// MaxPageLimit, and filtering a truncated page is worse than truncating a
// filtered one: with more than a page of plans, `plan ls` could print "no
// plans" while an active plan sat on page two.
//
// It then sorts by UpdatedAt descending. The snapshot orders by plan id, but
// this command has always shown most-recently-touched first — an operator who
// just paused a plan expects to see it at the top, not wherever its id sorts.
func (s *livePlanSource) ListPlans(
	ctx context.Context,
	projectID string,
	all bool,
) ([]observe.Plan, error) {
	client, err := supervisor.Find(s.stateRoot)
	if err != nil {
		return nil, errNoSupervisorListening
	}
	defer func() { _ = client.Close() }()

	return collectPlanPages(ctx, all, func(afterID string) (observe.PlanPage, error) {
		reply, err := client.ObserveSnapshot(ctx, ipc.ObserveSnapshotArgs{
			ProjectID:   projectID,
			PlanLimit:   observe.MaxPageLimit,
			PlanAfterID: afterID,
		})
		if err != nil {
			return observe.PlanPage{}, err
		}
		return reply.Plans, nil
	})
}

// collectPlanPages walks every snapshot page, applies the default status
// filter, and orders the result the way `plan ls` has always ordered it.
//
// Split out from the transport so the three properties that actually broke are
// testable without standing up a supervisor: exhausting pagination, keeping
// drafts behind --all, and most-recently-updated-first.
func collectPlanPages(
	ctx context.Context,
	all bool,
	fetch func(afterID string) (observe.PlanPage, error),
) ([]observe.Plan, error) {
	var items []observe.Plan
	afterID := ""
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		page, err := fetch(afterID)
		if err != nil {
			return nil, err
		}
		items = append(items, page.Items...)
		if !page.HasMore || page.NextAfterID == "" {
			break
		}
		afterID = page.NextAfterID
	}

	if !all {
		// Default view: active and paused, matching what this command has
		// always shown. Draft plans are NOT included — a draft is not something
		// an operator is acting on, and --all is what widens the view.
		kept := make([]observe.Plan, 0, len(items))
		for _, plan := range items {
			switch plan.Status {
			case "active", "paused":
				kept = append(kept, plan)
			}
		}
		items = kept
	}

	sort.SliceStable(items, func(i, j int) bool {
		return items[i].UpdatedAt.After(items[j].UpdatedAt)
	})
	return items, nil
}

func runPlanLs(ctx context.Context, cmd *cobra.Command, all, asJSON bool) error {
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
	return runPlanLsWith(ctx, os.Stdout, &livePlanSource{stateRoot: stateRoot}, projectID, all, asJSON)
}

// runPlanLsWith is the testable core: it takes the source and writer so a test
// can exercise both output shapes without a supervisor.
func runPlanLsWith(
	ctx context.Context,
	out io.Writer,
	src planSource,
	projectID string,
	all, asJSON bool,
) error {
	plans, err := src.ListPlans(ctx, projectID, all)
	if err != nil {
		return fmt.Errorf("list plans: %w", err)
	}
	if asJSON {
		// JSONL rather than a wrapping array: a caller can stream it line by
		// line, and an empty result is legitimately zero lines rather than a
		// bare "[]" that has to be special-cased.
		encoder := json.NewEncoder(out)
		for _, plan := range plans {
			if err := encoder.Encode(plan); err != nil {
				return fmt.Errorf("encode plan: %w", err)
			}
		}
		return nil
	}
	if len(plans) == 0 {
		_, err := fmt.Fprintln(out, "radioactive_ralph: no plans for this project")
		return err
	}
	for _, plan := range plans {
		// A write failure here is real (a closed pipe, a full disk): reporting
		// it beats printing a partial list and exiting 0.
		if _, err := fmt.Fprintf(
			out, "%-10s  %-24s  %s\n", plan.Status, plan.Slug, plan.Title,
		); err != nil {
			return fmt.Errorf("write plan list: %w", err)
		}
	}
	return nil
}
