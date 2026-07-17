// Package schema embeds the user-level store SQL migrations.
//
// Migrations are versioned files named NNNN_description.{up,down}.sql.
// The Migrate function in the parent store package applies them in
// lexical order.
package schema

import "embed"

// FS is the embedded filesystem containing migration files.
//
//go:embed *.sql
var FS embed.FS
