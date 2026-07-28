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
		if err := writeTaskLines(out, snapshot.Tasks); err != nil {
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
func writeTaskLines(out io.Writer, page observe.TaskPage) error {
	if len(page.Items) == 0 {
		return nil
	}
	labels := observe.PartitionLabels(page.Items)
	for _, task := range page.Items {
		line := fmt.Sprintf("  %-16s %-24s", task.ID, task.Status)
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
		if task.Blocked != nil && task.Blocked.Summary != "" {
			line += " — " + task.Blocked.Summary
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
