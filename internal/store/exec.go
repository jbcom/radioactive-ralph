package store

import (
	"context"
	"database/sql"
)

// execer is satisfied by both *sql.DB and *sql.Tx.
//
// It exists so a write has exactly ONE implementation that works standalone and
// inside a transaction. The alternative — a public autocommit method plus a
// hand-copied transactional twin — is how the two drift: the source branch's
// CreatePlanGraph duplicated CreatePlan/CreateTask/AddDep in raw SQL and, in
// doing so, silently dropped AddDep's cycle check.
type execer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

var (
	_ execer = (*sql.DB)(nil)
	_ execer = (*sql.Tx)(nil)
)
