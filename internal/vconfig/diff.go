package vconfig

import "reflect"

// Conflict records one config key where a previously stored value and an
// incoming value (typically from a projects: stanza key in --config-file or
// --user-config-file) disagree. Used by the supervisor to warn: "these keys
// would be overridden: ...; keep passing --project-config-file OR remove
// the conflicts (auto-remove? y/N)".
type Conflict struct {
	Key      string
	Stored   any
	Incoming any
}

// DiffConflicts compares stored (the project's current effective config,
// e.g. from ResolveProjects) against incoming (candidate values headed for
// that project, e.g. a projects: stanza entry) and reports every key present
// in both where the values differ. Keys only in one side are not
// conflicts — there's nothing to override.
func DiffConflicts(stored ProjectConfig, incoming map[string]any) []Conflict {
	var conflicts []Conflict
	for key, incomingVal := range incoming {
		storedVal, ok := stored.Values[key]
		if !ok {
			continue
		}
		if !reflect.DeepEqual(storedVal, incomingVal) {
			conflicts = append(conflicts, Conflict{
				Key:      key,
				Stored:   storedVal,
				Incoming: incomingVal,
			})
		}
	}
	return conflicts
}

// AutoRemove returns a copy of incoming with every conflicting key deleted,
// leaving only the keys that didn't collide with a stored value. incoming
// itself is left untouched.
func AutoRemove(incoming map[string]any, conflicts []Conflict) map[string]any {
	out := cloneMap(incoming)
	for _, c := range conflicts {
		delete(out, c.Key)
	}
	return out
}
