package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/jbcom/radioactive-ralph/internal/ipc"
	"github.com/jbcom/radioactive-ralph/internal/observe"
	"github.com/jbcom/radioactive-ralph/internal/supervisor"
	"github.com/jbcom/radioactive-ralph/internal/xdg"
	"github.com/spf13/cobra"
)

// eventSource is the seam the events command reads through: a safe backlog
// snapshot (+ its max id for the live cursor) and the safe live tail.
type eventSource interface {
	// Backlog returns up to n recent events OLDEST-FIRST plus the highest event
	// id in the project (the live cursor seed). n==0 returns no rows but still
	// reports the max id, so a --backlog 0 run tails strictly from "now".
	Backlog(ctx context.Context, projectID string, n int) (events []ipc.AttachEvent, maxID int64, err error)
	// AttachEvents streams live events with id > args.AfterID until ctx is
	// cancelled or the stream ends. fn is called once per event.
	AttachEvents(ctx context.Context, args ipc.AttachArgs, fn func(ipc.AttachEvent) error) error
}

func newEventsCmd() *cobra.Command {
	var backlog int
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "events",
		Short: "Tail the current project's live supervisor events",
		Long: "Stream the current project's events to stdout, one line per event, " +
			"until interrupted. The headless peer of the TUI/GUI live view.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runEvents(cmd.Context(), cmd, backlog, asJSON)
		},
	}
	cmd.Flags().IntVar(&backlog, "backlog", 0, "print the N most recent existing events before tailing live")
	cmd.Flags().BoolVar(&asJSON, "json", false, "emit each event as one JSON object per line (JSONL)")
	return cmd
}

// runEvents resolves the current project, wires the safe supervisor client,
// then delegates to runEventsWith.
func runEvents(ctx context.Context, cmd *cobra.Command, backlog int, asJSON bool) error {
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

	src := &liveEventSource{stateRoot: stateRoot}
	return runEventsWith(ctx, cmd.OutOrStdout(), cmd.ErrOrStderr(), src, projectID, backlog, asJSON)
}

// runEventsWith is the testable core: print the backlog, then tail live from the
// backlog's max id (a client-owned cursor, so no event is missed or duplicated
// across the backlog→live boundary).
func runEventsWith(ctx context.Context, out, errOut io.Writer, src eventSource, projectID string, backlog int, asJSON bool) error {
	events, cursor, err := src.Backlog(ctx, projectID, backlog)
	if err != nil {
		return fmt.Errorf("read event backlog: %w", err)
	}
	for _, ev := range events {
		writeEvent(out, errOut, ev, asJSON)
	}

	attachErr := src.AttachEvents(ctx, ipc.AttachArgs{ProjectID: projectID, AfterID: cursor}, func(ev ipc.AttachEvent) error {
		writeEvent(out, errOut, ev, asJSON)
		return nil
	})
	// A clean end-of-stream (nil) or a user interrupt (ctx cancelled) is a
	// success. A mid-stream error (supervisor closed/gone) is surfaced non-zero
	// so a CI wrapper sees the drop rather than a silent exit.
	if attachErr != nil && ctx.Err() == nil {
		_, _ = fmt.Fprintf(errOut, "radioactive_ralph: event stream ended: %v\n", attachErr)
		return attachErr
	}
	return nil
}

func writeEvent(out, errOut io.Writer, ev ipc.AttachEvent, asJSON bool) {
	if asJSON {
		raw, err := json.Marshal(ev)
		if err != nil {
			// Don't silently drop the event — a gap in a --json stream would
			// mislead a CI consumer into thinking it never occurred. Keep stdout
			// pure JSONL (a machine parses it) and report the drop on stderr with
			// the event id so it can be reconciled against the store.
			_, _ = fmt.Fprintf(errOut, "radioactive_ralph: skipped event %d (marshal failed: %v)\n", ev.ID, err)
			return
		}
		_, _ = fmt.Fprintln(out, string(raw))
		return
	}
	line := ev.OccurredAt.Format("15:04:05") + " " + ev.Kind
	if ev.TaskID != "" {
		line += " task=" + ev.TaskID
	}
	if ev.Failure != nil {
		line += " failure=" + string(ev.Failure.Category)
	}
	_, _ = fmt.Fprintln(out, line)
}

func observeEventToAttach(ev observe.Event) ipc.AttachEvent {
	return ipc.AttachEvent{
		ID:         ev.ID,
		Kind:       ev.Kind,
		Stream:     ev.Stream,
		PlanID:     ev.PlanID,
		TaskID:     ev.TaskID,
		OccurredAt: ev.OccurredAt,
		Failure:    ev.Failure,
	}
}

// liveEventSource reads both backlog and live tail through fresh one-shot
// supervisor connections. It never opens SQLite.
type liveEventSource struct {
	stateRoot string
	dial      func() (eventClient, error)
}

type eventClient interface {
	observeClient
	AttachEvents(
		context.Context,
		ipc.AttachArgs,
		func(ipc.AttachEvent) error,
	) error
	Close() error
}

func (s *liveEventSource) open() (eventClient, error) {
	if s.dial != nil {
		return s.dial()
	}
	return supervisor.Find(s.stateRoot)
}

func (s *liveEventSource) Backlog(
	ctx context.Context,
	projectID string,
	n int,
) ([]ipc.AttachEvent, int64, error) {
	if n < 0 {
		return nil, 0, fmt.Errorf("backlog must be non-negative")
	}
	client, err := s.open()
	if err != nil {
		return nil, 0, errNoSupervisorListening
	}
	defer func() { _ = client.Close() }()

	eventLimit := n
	if eventLimit == 0 {
		// One safe event is enough to seed the live cursor while printing no
		// backlog. Zero would select the store default rather than "none".
		eventLimit = 1
	}
	query := ipc.ObserveSnapshotArgs{
		ProjectID:  projectID,
		PlanLimit:  1,
		TaskLimit:  1,
		EventLimit: eventLimit,
	}
	snapshot, err := client.ObserveSnapshot(ctx, query)
	if err != nil {
		return nil, 0, queryCommandError(err)
	}
	if err := observe.ValidateSnapshotResponse(snapshot, query); err != nil {
		return nil, 0, fmt.Errorf("event backlog: %w", err)
	}
	recent := snapshot.RecentEvents.Items
	if len(recent) == 0 {
		return []ipc.AttachEvent{}, snapshot.EventCursor, nil
	}
	cursor := snapshot.EventCursor
	if n == 0 {
		return []ipc.AttachEvent{}, cursor, nil
	}
	events := make([]ipc.AttachEvent, 0, len(recent))
	for i := len(recent) - 1; i >= 0; i-- {
		events = append(events, observeEventToAttach(recent[i]))
	}
	return events, cursor, nil
}

func (s *liveEventSource) AttachEvents(ctx context.Context, args ipc.AttachArgs, fn func(ipc.AttachEvent) error) error {
	client, err := s.open()
	if err != nil {
		return errNoSupervisorListening
	}
	defer func() { _ = client.Close() }()
	return client.AttachEvents(ctx, args, fn)
}
