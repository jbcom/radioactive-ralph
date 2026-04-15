// Package schema embeds the plandag SQL migrations.
//
// Migrations are versioned files named NNNN_description.{up,down}.sql.
// The Runner in the parent plandag package applies them in order.
package schema

import "embed"

// FS is the embedded filesystem containing migration files.
//
//go:embed *.sql
var FS embed.FS
