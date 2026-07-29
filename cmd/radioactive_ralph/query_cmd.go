package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/jbcom/radioactive-ralph/internal/ipc"
	"github.com/jbcom/radioactive-ralph/internal/observe"
	"github.com/jbcom/radioactive-ralph/internal/supervisor"
	"github.com/jbcom/radioactive-ralph/internal/xdg"
	"github.com/spf13/cobra"
)

var errActiveWorkers = errors.New(
	"radioactive_ralph: active workers remain",
)

type observeClient interface {
	ObserveSnapshot(
		context.Context,
		ipc.ObserveSnapshotArgs,
	) (*ipc.ObserveSnapshotReply, error)
	ObserveMessages(
		context.Context,
		ipc.ObserveMessagesArgs,
	) (*ipc.ObserveMessagesReply, error)
}

func newStatusCmd() *cobra.Command {
	var (
		asJSON             bool
		requireZeroWorkers bool
		query              ipc.ObserveSnapshotArgs
		taskAfter          observe.TaskCursor
	)
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Read the current project's safe supervisor snapshot",
		Long: "Read one versioned, project-scoped snapshot from the supervisor. " +
			"No raw SQLite fallback is used. --require-zero-workers returns " +
			"nonzero when the trustworthy active-worker count is not zero.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			query.TaskAfter = taskAfter
			return runStatusQuery(
				cmd.Context(),
				cmd,
				query,
				asJSON,
				requireZeroWorkers,
			)
		},
	}
	cmd.Flags().BoolVar(
		&asJSON,
		"json",
		false,
		"emit the complete versioned snapshot as JSON",
	)
	cmd.Flags().BoolVar(
		&requireZeroWorkers,
		"require-zero-workers",
		false,
		"exit nonzero if any active worker remains",
	)
	cmd.Flags().IntVar(&query.PlanLimit, "plan-limit", 0, "plan page size")
	cmd.Flags().StringVar(
		&query.PlanAfterID,
		"plan-after",
		"",
		"resume plans after this plan id",
	)
	cmd.Flags().IntVar(&query.TaskLimit, "task-limit", 0, "task page size")
	cmd.Flags().StringVar(
		&taskAfter.PlanID,
		"task-after-plan",
		"",
		"task cursor plan id (requires --task-after)",
	)
	cmd.Flags().StringVar(
		&taskAfter.TaskID,
		"task-after",
		"",
		"task cursor task id (requires --task-after-plan)",
	)
	cmd.Flags().IntVar(&query.EventLimit, "event-limit", 0, "recent event page size")
	cmd.Flags().Int64Var(
		&query.EventBeforeID,
		"event-before",
		0,
		"continue with events older than this id",
	)
	return cmd
}

func runStatusQuery(
	ctx context.Context,
	cmd *cobra.Command,
	query ipc.ObserveSnapshotArgs,
	asJSON, requireZeroWorkers bool,
) error {
	stateRoot, projectID, err := queryProject(ctx, cmd)
	if err != nil {
		return err
	}
	query.ProjectID = projectID
	client, err := supervisor.Find(stateRoot)
	if err != nil {
		return errNoSupervisorListening
	}
	defer func() { _ = client.Close() }()
	return runStatusQueryWith(
		ctx,
		cmd.OutOrStdout(),
		client,
		query,
		asJSON,
		requireZeroWorkers,
	)
}

func runStatusQueryWith(
	ctx context.Context,
	out io.Writer,
	client observeClient,
	query ipc.ObserveSnapshotArgs,
	asJSON, requireZeroWorkers bool,
) error {
	snapshot, err := client.ObserveSnapshot(ctx, query)
	if err != nil {
		return queryCommandError(err)
	}
	if err := observe.ValidateSnapshotResponse(snapshot, query); err != nil {
		return fmt.Errorf("status: %w", err)
	}
	if asJSON {
		if err := writeJSON(out, snapshot); err != nil {
			return fmt.Errorf("status: encode JSON: %w", err)
		}
	} else {
		_, err := fmt.Fprintf(
			out,
			"project=%s plans=%d tasks=%d active_workers=%d captured_at=%s\n",
			snapshot.Project.ID,
			snapshot.Summary.PlanTotal,
			snapshot.Summary.TaskTotal,
			snapshot.Summary.ActiveWorkerCount,
			snapshot.CapturedAt.Format("2006-01-02T15:04:05Z07:00"),
		)
		if err != nil {
			return fmt.Errorf("status: write output: %w", err)
		}
		if err := writePlanWarnings(out, snapshot.Plans); err != nil {
			return err
		}
		if err := writeTaskLines(out, snapshot.Tasks, snapshot.RecentEvents); err != nil {
			return err
		}
	}
	if requireZeroWorkers && snapshot.Summary.ActiveWorkerCount != 0 {
		return fmt.Errorf(
			"%w: %d",
			errActiveWorkers,
			snapshot.Summary.ActiveWorkerCount,
		)
	}
	return nil
}

func newMessagesCmd() *cobra.Command {
	var query ipc.ObserveMessagesArgs
	cmd := &cobra.Command{
		Use:   "messages",
		Short: "Read content-free A2A message metadata as JSON",
		Long: "Read one bounded chronological metadata page from the supervisor. " +
			"Message parts/provider output are never returned.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runMessagesQuery(cmd.Context(), cmd, query)
		},
	}
	cmd.Flags().StringVar(&query.PlanID, "plan", "", "limit metadata to this plan id")
	cmd.Flags().StringVar(&query.TaskID, "task", "", "limit metadata to this task id (requires --plan)")
	cmd.Flags().Int64Var(&query.AfterID, "after", 0, "resume after this message id")
	cmd.Flags().IntVar(&query.Limit, "limit", 0, "message page size")
	return cmd
}

func runMessagesQuery(
	ctx context.Context,
	cmd *cobra.Command,
	query ipc.ObserveMessagesArgs,
) error {
	stateRoot, projectID, err := queryProject(ctx, cmd)
	if err != nil {
		return err
	}
	query.ProjectID = projectID
	client, err := supervisor.Find(stateRoot)
	if err != nil {
		return errNoSupervisorListening
	}
	defer func() { _ = client.Close() }()
	return runMessagesQueryWith(ctx, cmd.OutOrStdout(), client, query)
}

func runMessagesQueryWith(
	ctx context.Context,
	out io.Writer,
	client observeClient,
	query ipc.ObserveMessagesArgs,
) error {
	page, err := client.ObserveMessages(ctx, query)
	if err != nil {
		return queryCommandError(err)
	}
	if err := observe.ValidateMessageResponse(page); err != nil {
		return fmt.Errorf("messages: %w", err)
	}
	if err := writeJSON(out, page); err != nil {
		return fmt.Errorf("messages: encode JSON: %w", err)
	}
	return nil
}

func queryProject(
	ctx context.Context,
	cmd *cobra.Command,
) (stateRoot, projectID string, err error) {
	stateRoot, err = xdg.StateRoot()
	if err != nil {
		return "", "", fmt.Errorf("resolve state root: %w", err)
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "", "", fmt.Errorf("resolve cwd: %w", err)
	}
	projectID, err = ensureProjectKnown(ctx, cmd, stateRoot, cwd)
	if err != nil {
		return "", "", err
	}
	return stateRoot, projectID, nil
}

func queryCommandError(err error) error {
	if ipc.IsCode(err, ipc.CodeUnsupportedCommand) {
		return fmt.Errorf(
			"supervisor protocol v%d query support required; upgrade and restart the supervisor: %w",
			ipc.QueryProtoVersion,
			err,
		)
	}
	return err
}

func writeJSON(out io.Writer, value any) error {
	encoder := json.NewEncoder(out)
	encoder.SetEscapeHTML(false)
	return encoder.Encode(value)
}

// writeTaskLines renders the task page under the summary line, so the
// human-readable output is not strictly less informative than the JSON it
// wraps. The summary reports COUNTS; without this an operator had to pipe
// through jq or open a UI to learn which task was running and what ran it.
//
// It reuses observe.PartitionLabels and Task.ProvenanceLabel rather than
// re-deriving either. Those encode display POLICY (label only multi-task
// partitions, never print the raw ordinal, prefer alias over provider type),
// and a CLI that re-implemented them would become a third dialect that drifts
// from the TUI and GUI.
func writeTaskLines(out io.Writer, page observe.TaskPage, events observe.EventPage) error {
	if len(page.Items) == 0 {
		return nil
	}
	labels := observe.PartitionLabels(page.Items)
	reasons := failureReasonsByTask(events)
	for _, task := range page.Items {
		line := fmt.Sprintf("  %-16s %-24s", task.ID, task.Status)
		// Which worker currently HOLDS the task, which is a different question
		// from which partition it shares: two tasks can sit in one ready
		// partition and still be claimed by different workers. Abbreviated to the
		// distinguishing tail exactly as the meso view does -- the guide promises
		// the same markers, and shipping a subset made that promise false.
		if id := task.ClaimedByWorkerID; id != "" {
			line += " w:" + observe.WorkerSuffix(id, 8)
		}
		if name := task.ProvenanceLabel(); name != "" {
			line += " via=" + name
		}
		if label := labels[task.PartitionOrdinal]; label != "" {
			line += " " + label
		}
		// Why it is stalled, for the one status where the operator cannot infer
		// it. A blocked task looks exactly like one waiting on a dependency --
		// both sit at zero progress -- but one clears itself as upstream work
		// finishes and the other needs the operator to change something. Without
		// this the human-readable list still sent them to --json for precisely
		// the case it is most useful for.
		//
		// Safe to print because Blocked is a CLASSIFICATION carrying static
		// remediation text, deliberately not the stored reason string, which is
		// error-derived and would leak free text across this boundary.
		// Another task's failure makes this one unreachable -- distinct from
		// Blocked, which is a configuration problem on this task itself.
		if task.BlockedByTaskID != "" {
			line += " — cannot run: " + task.BlockedByTaskID + " failed"
		}
		if task.Blocked != nil && task.Blocked.Summary != "" {
			line += " — " + task.Blocked.Summary
		}
		// Why a failed task failed. Not derivable from the row: a task that
		// exhausted its retries and one that never reached a provider both read
		// "failed". The snapshot already carries the closed FailureSummary and
		// the TUI has rendered it all along; without this the CLI human path
		// stayed strictly less informative than its own --json.
		// Gated on STATUS, which is the authority on what the task IS; an event
		// only explains a failure status already reports. Newest-wins alone was
		// not enough: it clears a stale reason only when a NEWER EVENT exists,
		// and a task requeued and re-claimed may have none in the page yet, so
		// its last attempt's failure would render on a row reading "running".
		if task.Status == "failed" {
			if reason := reasons[taskKey(task.PlanID, task.ID)]; reason != "" {
				line += " — " + reason
			} else if task.FailureCategory != "" {
				// Fallback for the evicted case: the event page is bounded, so a
				// long-terminal task loses its event while the durable category
				// on the task row survives. Terser than the event summary, but
				// it still answers "why" rather than leaving a bare "failed".
				line += " — " + task.FailureCategory
			}
		}
		// A reclaimed task carries a count with no cause, which reads as alarming
		// rather than informative: 2 looks identical whether the worker crashed
		// twice or the turns were killed for producing no output. Diagnosing that
		// difference on a live run meant reading watchdog source by hand.
		//
		// NOT gated on status, unlike the failure reason above: a reclaim moves
		// the task back to pending and it may be re-claimed and running again by
		// the time anyone looks. The count is on the row regardless, so the
		// explanation belongs beside it in every state it can appear.
		if task.ReclaimCount > 0 && task.ReclaimReason != "" {
			line += fmt.Sprintf(" — reclaimed %dx: %s", task.ReclaimCount, task.ReclaimReason)
			// Only when work was genuinely in flight. A reclaim on an otherwise
			// idle machine has nothing to blame the load for, and a marker on
			// every row is noise that trains the reader to skip the column.
			if task.ReclaimConcurrentClaims > 1 {
				line += fmt.Sprintf(" (%d claims in flight)", task.ReclaimConcurrentClaims)
			}
		}
		// The status column is padded so the marker columns align, which leaves
		// trailing spaces on any row whose markers are all absent -- an unrun
		// task, the common case. Trailing whitespace is invisible until it shows
		// up as a diff or trips a linter, so trim it here rather than shipping
		// rows that differ only in blanks.
		if _, err := fmt.Fprintln(out, strings.TrimRight(line, " ")); err != nil {
			return fmt.Errorf("status: write task line: %w", err)
		}
	}
	// A truncated list that looks complete is the failure mode worth guarding:
	// an operator reading "3 tasks" from a bounded page could conclude the rest
	// finished.
	if page.HasMore {
		if _, err := fmt.Fprintln(out,
			"  … more tasks available; showing the first bounded page"); err != nil {
			return fmt.Errorf("status: write task pagination notice: %w", err)
		}
	}
	return nil
}

// failureReasonsByTask maps (plan, task) -> the newest failure summary for that
// task, from the event page the snapshot already returned.
//
// The event summary is the PREFERRED reason because it is prose an operator can
// act on. It is not the only one: the event page is bounded (20 by default), so
// a long-terminal task loses its failure event to newer activity while staying
// failed forever. That gap is now closed by the durable failure_category on the
// task row (migration 0004), which the caller falls back to -- terser, but it
// still answers "why" instead of leaving a bare "failed".
//
// Newest wins because a task can fail, be requeued, and fail again: the
// operator asks about the CURRENT state, and an older attempt's reason would
// describe a run that has since been superseded. The event page is newest-first
// (EventPage documents that ordering), so the first entry seen for a task is
// the one to keep.
//
// It reuses the FailureSummary the snapshot carries rather than deriving a
// second taxonomy from event kinds -- observe owns that classification, and a
// CLI-local copy would drift from the TUI the first time either changed.
func failureReasonsByTask(page observe.EventPage) map[string]string {
	reasons := make(map[string]string)
	seen := make(map[string]bool)
	for _, ev := range page.Items {
		if ev.TaskID == "" {
			continue
		}
		key := taskKey(ev.PlanID, ev.TaskID)
		// FIRST event wins, failure or not. The page is newest-first, so the
		// first entry for a task describes its CURRENT attempt. Skipping
		// non-failure events instead would let an older failure outlive the
		// retry that succeeded, rendering "done — task attempt failed and was
		// requeued" -- a reason that describes a run already superseded.
		if seen[key] {
			continue
		}
		seen[key] = true
		if ev.Failure != nil && ev.Failure.Summary != "" {
			reasons[key] = ev.Failure.Summary
		}
	}
	return reasons
}

// taskKey composes the (plan, task) identity a task actually has. Task ids are
// unique only WITHIN a plan -- tasks is PRIMARY KEY (plan_id, id), and
// positional ids like "0.0" recur in every plan -- while `status` spans all of
// a project's plans by default. Keying on the task id alone let one plan's
// failure explain a same-named task in another, which is a confidently wrong
// answer rather than a missing one.
func taskKey(planID, taskID string) string {
	return planID + "\x00" + taskID
}

// writePlanWarnings names any plan that cannot advance.
//
// Only plans that are STUCK are listed -- a healthy plan needs no line, and a
// warning on every plan is one an operator stops reading. The summary counts
// above say how much work exists; this says which of it is already dead.
func writePlanWarnings(out io.Writer, page observe.PlanPage) error {
	for _, plan := range page.Items {
		if !plan.NoRunnableWork {
			continue
		}
		if _, err := fmt.Fprintf(out,
			"  plan %s: no runnable work — every task is done, failed, or blocked by a failure\n",
			plan.Slug); err != nil {
			return fmt.Errorf("status: write plan warning: %w", err)
		}
	}
	return nil
}
