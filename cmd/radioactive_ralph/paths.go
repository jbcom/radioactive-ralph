package main

import "path/filepath"

// dbFileName is the single user-level SQLite database file (spec §6)
// every mode of this binary opens under the XDG state root.
const dbFileName = "ralph.db"

// storeDBPath returns the store DB path under stateRoot. Shared by
// supervisor, --init, and plain-client modes so all three always agree on
// where the one user-level database lives.
func storeDBPath(stateRoot string) string {
	return filepath.Join(stateRoot, dbFileName)
}
