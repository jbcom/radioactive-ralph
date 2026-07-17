package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

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

// planFrontmatterSlug and planFrontmatterTitle are pulled from a plan
// markdown's leading content: the first level-1 heading is the title, and a
// slug is derived from it (or the filename). No YAML frontmatter parser is
// pulled in — this stays consistent with the heuristic-markdown plan
// philosophy (goldmark structure, not a bespoke metadata grammar).
var planHeadingRe = regexp.MustCompile(`(?m)^#\s+(.+?)\s*$`)

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

	title := derivePlanTitle(markdown, planPath)
	if slug == "" {
		slug = slugify(title)
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

// derivePlanTitle returns the plan's first level-1 heading, or the file's
// base name (sans extension) when the markdown has no heading.
func derivePlanTitle(markdown, planPath string) string {
	if m := planHeadingRe.FindStringSubmatch(markdown); m != nil {
		if t := strings.TrimSpace(m[1]); t != "" {
			return t
		}
	}
	base := filepath.Base(planPath)
	return strings.TrimSuffix(base, filepath.Ext(base))
}

// slugify lower-cases a title and replaces every run of non-alphanumeric
// characters with a single hyphen, trimming leading/trailing hyphens — a
// stable, filesystem-and-URL-safe plan slug.
func slugify(title string) string {
	var b strings.Builder
	prevHyphen := false
	for _, r := range strings.ToLower(title) {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'):
			b.WriteRune(r)
			prevHyphen = false
		default:
			if !prevHyphen {
				b.WriteByte('-')
				prevHyphen = true
			}
		}
	}
	slug := strings.Trim(b.String(), "-")
	if slug == "" {
		return "plan"
	}
	return slug
}
