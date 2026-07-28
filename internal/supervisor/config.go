package supervisor

import (
	"context"
	"fmt"

	"github.com/jbcom/radioactive-ralph/internal/ipc"
	"github.com/jbcom/radioactive-ralph/internal/vconfig"
)

// HandleProjectConfigGet returns a project's stored config values.
//
// This exists so `init` can resolve vconfig layers without opening the store.
// The supervisor is the single writer of record; a client that reads and writes
// the database directly is a second writer to a database it does not own, which
// is the ownership split the one-binary architecture exists to prevent.
//
// Values cross the wire as the RAW JSON-encoded strings the store holds.
// vconfig owns that format, and decoding here would make the supervisor a
// second interpreter of a schema it has no stake in.
func (s *Supervisor) HandleProjectConfigGet(
	ctx context.Context, args ipc.ProjectConfigGetArgs,
) (ipc.ProjectConfigGetReply, error) {
	var zero ipc.ProjectConfigGetReply
	projectID, err := s.resolveConfigProject(ctx, args.Project, args.UserScope)
	if err != nil {
		return zero, err
	}
	values, err := s.store.GetProjectConfig(ctx, projectID)
	if err != nil {
		return zero, fmt.Errorf("supervisor: get project config: %w", err)
	}
	return ipc.ProjectConfigGetReply{Values: values, ProjectID: projectID}, nil
}

// HandleProjectConfigApply upserts and deletes project config keys in one call.
//
// Both sets travel together because vconfig computes them together: an overlay
// to write, plus (when a provider selection replaces the legacy singular key) a
// key to remove. Applying them in separate calls would let a crash in between
// leave the project with the new selection AND the stale key it was meant to
// replace — a state no code path expects.
func (s *Supervisor) HandleProjectConfigApply(
	ctx context.Context, args ipc.ProjectConfigApplyArgs,
) error {
	if len(args.Upserts) == 0 && len(args.DeleteKeys) == 0 {
		return &codedError{ipc.CodeInvalidArgs, "project-config-apply: nothing to apply"}
	}
	projectID, err := s.resolveConfigProject(ctx, args.Project, args.UserScope)
	if err != nil {
		return err
	}
	if err := s.store.ApplyProjectConfig(ctx, projectID, args.Upserts, args.DeleteKeys); err != nil {
		return fmt.Errorf("supervisor: apply project config: %w", err)
	}
	return nil
}

// resolveConfigProject turns a request's project selector into a concrete id.
//
// The user-scope project is resolved HERE rather than by the client, because
// resolving it may CREATE it — a store write, and the supervisor is the single
// writer of record. A client that created it would be writing to a database it
// does not own.
func (s *Supervisor) resolveConfigProject(ctx context.Context, projectID string, userScope bool) (string, error) {
	if userScope {
		if projectID != "" {
			return "", &codedError{
				ipc.CodeInvalidArgs,
				"project config: user_scope and an explicit project id are mutually exclusive",
			}
		}
		id, err := vconfig.UserScopeProjectID(ctx, s.store)
		if err != nil {
			return "", fmt.Errorf("supervisor: resolve user-scope project: %w", err)
		}
		return id, nil
	}
	if projectID == "" {
		return "", &codedError{ipc.CodeInvalidArgs, "project config: project id required"}
	}
	return projectID, nil
}
