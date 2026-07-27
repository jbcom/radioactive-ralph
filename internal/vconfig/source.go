package vconfig

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jbcom/radioactive-ralph/internal/store"
)

// ConfigSource is where vconfig reads and writes project config.
//
// It exists so resolving config layers does not REQUIRE a *store.Store. Taking
// the store directly meant every caller that wanted its effective config had to
// be a direct database reader — and the CLI is supposed to be a dumb client
// that talks to the supervisor and refuses to run without one. With this seam
// the supervisor passes a store-backed source and the CLI passes an IPC-backed
// one, and BOTH run the same resolution code: one path, not a client path and a
// server path that drift.
type ConfigSource interface {
	// UserScopeProject returns the id of the synthetic project holding
	// user-level (non-project) config, creating it if needed.
	UserScopeProject(ctx context.Context) (string, error)
	// ProjectConfigValues returns one project's stored config as the RAW
	// JSON-encoded strings the store holds. Decoding stays in vconfig, which
	// owns the format.
	ProjectConfigValues(ctx context.Context, projectID string) (map[string]string, error)
	// ApplyProjectConfigValues upserts and deletes keys in one operation.
	ApplyProjectConfigValues(ctx context.Context, projectID string, upserts map[string]string, deleteKeys []string) error
}

// StoreConfigSource adapts a *store.Store to ConfigSource. Used by the
// supervisor, which legitimately owns the database.
type StoreConfigSource struct {
	Store *store.Store
}

// NewStoreConfigSource returns a ConfigSource backed by st, or nil when st is
// nil so callers can keep the "no store, file layers only" behavior.
func NewStoreConfigSource(st *store.Store) *StoreConfigSource {
	if st == nil {
		return nil
	}
	return &StoreConfigSource{Store: st}
}

// UserScopeProject resolves (creating if needed) the synthetic project that
// holds user-level config.
func (s *StoreConfigSource) UserScopeProject(ctx context.Context) (string, error) {
	return UserScopeProjectID(ctx, s.Store)
}

// ProjectConfigValues returns one project's raw stored config values.
func (s *StoreConfigSource) ProjectConfigValues(ctx context.Context, projectID string) (map[string]string, error) {
	return s.Store.GetProjectConfig(ctx, projectID)
}

// ApplyProjectConfigValues upserts and deletes config keys in one operation.
func (s *StoreConfigSource) ApplyProjectConfigValues(
	ctx context.Context, projectID string, upserts map[string]string, deleteKeys []string,
) error {
	return s.Store.ApplyProjectConfig(ctx, projectID, upserts, deleteKeys)
}

// loadSourceConfig reads one project's config through src and decodes each
// value. The store persists JSON-encoded strings (see store.SetProjectConfig),
// and vconfig owns that decoding.
func loadSourceConfig(ctx context.Context, src ConfigSource, projectID string) (map[string]any, error) {
	raw, err := src.ProjectConfigValues(ctx, projectID)
	if err != nil {
		return nil, err
	}
	return decodeConfigValues(raw)
}

// decodeConfigValues turns stored JSON strings into Go values.
//
// A non-JSON value is a store-layer bug rather than a caller error, so it
// surfaces instead of being silently dropped — a dropped key reads as "never
// configured", which is indistinguishable from a fresh project.
func decodeConfigValues(raw map[string]string) (map[string]any, error) {
	out := make(map[string]any, len(raw))
	for k, v := range raw {
		decoded, err := decodeConfigValue(k, v)
		if err != nil {
			return nil, err
		}
		out[k] = decoded
	}
	return out, nil
}

func decodeConfigValue(key, encoded string) (any, error) {
	var decoded any
	if err := json.Unmarshal([]byte(encoded), &decoded); err != nil {
		return nil, fmt.Errorf("vconfig: decode stored value for %q: %w", key, err)
	}
	return decoded, nil
}

// isNilConfigSource reports whether src carries no source at all.
//
// A typed-nil pointer in a non-nil interface is the classic Go trap here:
// NewStoreConfigSource(nil) returns a nil *StoreConfigSource, which as a
// ConfigSource is NOT == nil, so a plain `src != nil` check would call methods
// on it and panic.
func isNilConfigSource(src ConfigSource) bool {
	if src == nil {
		return true
	}
	if s, ok := src.(*StoreConfigSource); ok {
		return s == nil || s.Store == nil
	}
	return false
}
