package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/jbcom/radioactive-ralph/internal/ipc"
	"github.com/jbcom/radioactive-ralph/internal/store"
	"github.com/jbcom/radioactive-ralph/internal/supervisor"
	"github.com/jbcom/radioactive-ralph/internal/xdg"
	"github.com/spf13/cobra"
)

// eventSource is the seam the events command reads through: a backlog snapshot
// (+ its max id for the live cursor) and the live tail. The real implementation
// wraps the store (backlog) and an *ipc.Client (live); tests supply a fake so
// runEventsWith is exercisable with no supervisor and no store.
type eventSource interface {
	// Backlog returns up to n recent events OLDEST-FIRST plus the highest event
	// id in the project (the live cursor seed). n==0 returns no rows but still
	// reports the max id, so a --backlog 0 run tails strictly from "now".
	Backlog(ctx context.Context, projectID string, n int) (events []store.Event, maxID int64, err error)
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

// runEvents wires the real store+client seam, then delegates to runEventsWith.
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

	client, err := supervisor.Find(stateRoot)
	if err != nil {
		return errNoSupervisorListening
	}
	defer func() { _ = client.Close() }()

	src := &liveEventSource{stateRoot: stateRoot, client: client}
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
		writeEvent(out, storeEventToAttach(ev), asJSON)
	}

	attachErr := src.AttachEvents(ctx, ipc.AttachArgs{ProjectID: projectID, AfterID: cursor}, func(ev ipc.AttachEvent) error {
		writeEvent(out, ev, asJSON)
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

func writeEvent(out io.Writer, ev ipc.AttachEvent, asJSON bool) {
	if asJSON {
		if raw, err := json.Marshal(ev); err == nil {
			_, _ = fmt.Fprintln(out, string(raw))
		}
		return
	}
	line := ev.OccurredAt.Format("15:04:05") + " " + ev.Kind
	if ev.TaskID != "" {
		line += " task=" + ev.TaskID
	}
	if ev.Actor != "" {
		line += " actor=" + ev.Actor
	}
	_, _ = fmt.Fprintln(out, line)
}

// storeEventToAttach maps a stored backlog row to the same wire shape the live
// stream delivers, so backlog and live lines render identically.
func storeEventToAttach(ev store.Event) ipc.AttachEvent {
	var payload json.RawMessage
	if ev.PayloadJSON != "" {
		payload = json.RawMessage(ev.PayloadJSON)
	}
	return ipc.AttachEvent{
		ID:         ev.ID,
		Kind:       ev.Kind,
		Stream:     ev.Stream,
		PlanID:     ev.PlanID,
		TaskID:     ev.TaskID,
		Actor:      ev.Actor,
		Payload:    payload,
		OccurredAt: ev.OccurredAt,
	}
}

// liveEventSource is the production eventSource: the backlog + cursor come from
// the store, the live tail from the supervisor client.
type liveEventSource struct {
	stateRoot string
	client    *ipc.Client
}

func (s *liveEventSource) Backlog(ctx context.Context, projectID string, n int) ([]store.Event, int64, error) {
	st, err := store.Open(ctx, store.Options{DSN: store.DSN(storeDBPath(s.stateRoot))})
	if err != nil {
		return nil, 0, fmt.Errorf("open store: %w", err)
	}
	defer func() { _ = st.Close() }()

	maxID, err := st.MaxEventID(ctx, projectID)
	if err != nil {
		return nil, 0, fmt.Errorf("max event id: %w", err)
	}
	if n <= 0 {
		return nil, maxID, nil
	}
	// ListProjectEvents is newest-first; reverse to oldest-first for a readable
	// scrollback that flows into the live tail.
	recent, err := st.ListProjectEvents(ctx, projectID, n)
	if err != nil {
		return nil, 0, fmt.Errorf("list events: %w", err)
	}
	for i, j := 0, len(recent)-1; i < j; i, j = i+1, j-1 {
		recent[i], recent[j] = recent[j], recent[i]
	}
	return recent, maxID, nil
}

func (s *liveEventSource) AttachEvents(ctx context.Context, args ipc.AttachArgs, fn func(ipc.AttachEvent) error) error {
	return s.client.AttachEvents(ctx, args, fn)
}
