package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"

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
	if err := validateSnapshotReply(snapshot, query); err != nil {
		return err
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
	if err := validateMessageReply(page); err != nil {
		return err
	}
	if err := writeJSON(out, page); err != nil {
		return fmt.Errorf("messages: encode JSON: %w", err)
	}
	return nil
}

func validateSnapshotReply(
	snapshot *ipc.ObserveSnapshotReply,
	query ipc.ObserveSnapshotArgs,
) error {
	if snapshot == nil {
		return fmt.Errorf("status: supervisor returned no snapshot")
	}
	if snapshot.SchemaVersion != observe.SchemaVersion {
		return fmt.Errorf(
			"status: unsupported observation schema %d (client supports %d)",
			snapshot.SchemaVersion,
			observe.SchemaVersion,
		)
	}
	if snapshot.Project.ID != query.ProjectID {
		return fmt.Errorf(
			"status: supervisor returned project %q for requested project %q",
			snapshot.Project.ID,
			query.ProjectID,
		)
	}
	if snapshot.Summary.ActiveWorkerCount != len(snapshot.Workers) {
		return fmt.Errorf(
			"status: inconsistent active-worker count %d for %d worker records",
			snapshot.Summary.ActiveWorkerCount,
			len(snapshot.Workers),
		)
	}
	if snapshot.Summary.ZeroActiveWorkers !=
		(snapshot.Summary.ActiveWorkerCount == 0) {
		return fmt.Errorf(
			"status: inconsistent zero-worker assertion for count %d",
			snapshot.Summary.ActiveWorkerCount,
		)
	}
	return nil
}

func validateMessageReply(page *ipc.ObserveMessagesReply) error {
	if page == nil {
		return fmt.Errorf("messages: supervisor returned no page")
	}
	if page.SchemaVersion != observe.SchemaVersion {
		return fmt.Errorf(
			"messages: unsupported observation schema %d (client supports %d)",
			page.SchemaVersion,
			observe.SchemaVersion,
		)
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
