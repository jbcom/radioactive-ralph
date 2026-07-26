// Package tui implements the read-only Bubble Tea client described in
// docs/superpowers/specs/2026-07-16-supervisor-architecture-design.md §7.
package tui

import (
	"context"

	"github.com/jbcom/radioactive-ralph/internal/ipc"
	"github.com/jbcom/radioactive-ralph/internal/observe"
)

// DataSource is the TUI's complete supervisor-owned read boundary. Snapshot
// returns one bounded, internally consistent safe DTO; Attach streams only the
// safe event DTO. No raw store type or direct-store fallback crosses this seam.
type DataSource interface {
	Snapshot(
		ctx context.Context,
		query observe.SnapshotQuery,
	) (*observe.Snapshot, error)

	// Attach resumes strictly after the model-owned cursor. The first cursor is
	// seeded from Snapshot.EventCursor, which was captured in the same read
	// transaction as the initial visible state.
	Attach(
		ctx context.Context,
		afterID int64,
		fn func(ipc.AttachEvent) error,
	) error
}
