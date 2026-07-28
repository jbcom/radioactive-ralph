package supervisor

import (
	"context"
	"fmt"

	"github.com/jbcom/radioactive-ralph/internal/ipc"
	"github.com/jbcom/radioactive-ralph/internal/store"
)

// HandleProjectEnsure resolves the caller's directory to a project, creating it
// when no accumulated fingerprint matches.
//
// This exists so a client never opens the store to identify itself. The client
// computes its own fingerprints (absolute path, git root commit, git remote)
// from its working directory — local facts, not store reads — and the
// supervisor, the single writer of record, does the resolve/create/touch.
//
// Resolve-create-touch is ONE command rather than three: there is a race
// between "not found" and "create", and three round trips would let two
// concurrent clients in the same directory each observe not-found and both
// create a project for it.
func (s *Supervisor) HandleProjectEnsure(
	ctx context.Context,
	args ipc.ProjectEnsureArgs,
) (*ipc.ProjectEnsureReply, error) {
	if len(args.Fingerprints) == 0 {
		return nil, &codedError{ipc.CodeInvalidArgs, "project-ensure: at least one fingerprint required"}
	}
	fingerprints := make([]store.Fingerprint, 0, len(args.Fingerprints))
	for _, fp := range args.Fingerprints {
		if fp.Kind == "" || fp.Value == "" {
			return nil, &codedError{ipc.CodeInvalidArgs, "project-ensure: fingerprint kind and value required"}
		}
		fingerprints = append(fingerprints, store.Fingerprint{Kind: fp.Kind, Value: fp.Value})
	}

	projectID, found, err := s.store.ResolveProject(ctx, fingerprints)
	if err != nil {
		return nil, fmt.Errorf("supervisor: resolve project: %w", err)
	}
	if found && args.ResolveOnly {
		// Read-only means read-only: skip the accumulate/touch writes below
		// too, not just the create. A "resolve" that still mutates rows is not
		// one, and the desktop path may run against a directory the operator
		// never intended to register.
		return &ipc.ProjectEnsureReply{ProjectID: projectID, Created: false}, nil
	}
	if !found && args.ResolveOnly {
		// Unknown directory: report the miss rather than creating. The caller
		// scopes its view to nothing and shows an actionable banner.
		return &ipc.ProjectEnsureReply{}, nil
	}
	if found {
		// Accumulate any fingerprint this directory has grown since it was
		// first seen (a git remote added later, say), so a subsequent resolve
		// from a different signal still finds the same project.
		if err := s.store.AddProjectIdentifiers(ctx, projectID, fingerprints); err != nil {
			return nil, fmt.Errorf("supervisor: accumulate project identifiers: %w", err)
		}
		if err := s.store.TouchProjectLastSeen(ctx, projectID); err != nil {
			return nil, fmt.Errorf("supervisor: touch project: %w", err)
		}
		return &ipc.ProjectEnsureReply{ProjectID: projectID, Created: false}, nil
	}

	displayName := args.DisplayName
	if displayName == "" {
		return nil, &codedError{ipc.CodeInvalidArgs, "project-ensure: display_name required to create a project"}
	}
	projectID, err = s.store.CreateProject(ctx, displayName, fingerprints)
	if err != nil {
		return nil, fmt.Errorf("supervisor: create project: %w", err)
	}
	return &ipc.ProjectEnsureReply{ProjectID: projectID, Created: true}, nil
}
