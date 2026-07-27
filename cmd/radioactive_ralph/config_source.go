package main

import (
	"context"
	"fmt"

	"github.com/jbcom/radioactive-ralph/internal/ipc"
	"github.com/jbcom/radioactive-ralph/internal/vconfig"
)

// newSupervisorConfigSource returns a vconfig.ConfigSource backed by the
// supervisor socket.
func newSupervisorConfigSource(client *ipc.Client) vconfig.ConfigSource {
	return &supervisorConfigSource{client: client}
}

// supervisorConfigSource resolves vconfig layers over the supervisor socket.
//
// This is what lets `init` stop opening the store. The supervisor is the single
// writer of record; a client that reads and writes the database directly is a
// second writer to a database it does not own, which is the ownership split the
// one-binary architecture exists to prevent.
type supervisorConfigSource struct {
	client *ipc.Client
	// userProjectID caches the user-scope project id for this command's
	// lifetime. Resolving it is a round trip AND may create the project, so
	// repeating it per layer would be both slower and needlessly write-y.
	userProjectID string
	// userValues is the user-scope config the UserScopeProject round trip
	// already returned, so resolving the user layer does not immediately ask
	// for the same values again.
	userValues map[string]string
}

func (s *supervisorConfigSource) UserScopeProject(ctx context.Context) (string, error) {
	if s.userProjectID != "" {
		return s.userProjectID, nil
	}
	// Ask the SUPERVISOR to resolve it: doing so may create the project, and
	// that is a store write the client must not perform.
	reply, err := s.client.ProjectConfigGet(ctx, ipc.ProjectConfigGetArgs{UserScope: true})
	if err != nil {
		return "", fmt.Errorf("resolve user-scope project: %w", err)
	}
	s.userProjectID = reply.ProjectID
	s.userValues = reply.Values
	return reply.ProjectID, nil
}

func (s *supervisorConfigSource) ProjectConfigValues(
	ctx context.Context, projectID string,
) (map[string]string, error) {
	if projectID != "" && projectID == s.userProjectID && s.userValues != nil {
		return s.userValues, nil
	}
	reply, err := s.client.ProjectConfigGet(ctx, ipc.ProjectConfigGetArgs{Project: projectID})
	if err != nil {
		return nil, err
	}
	return reply.Values, nil
}

func (s *supervisorConfigSource) ApplyProjectConfigValues(
	ctx context.Context, projectID string, upserts map[string]string, deleteKeys []string,
) error {
	if len(upserts) == 0 && len(deleteKeys) == 0 {
		return nil
	}
	// Express user scope as USER SCOPE rather than as the project id it happens
	// to resolve to. Sending the resolved id with UserScope=false reaches the
	// same row today only because this client resolved it first — it asks the
	// supervisor to trust a client-computed identity for a scope the supervisor
	// owns, and it bypasses the mutual-exclusivity check that exists to catch
	// exactly that confusion.
	args := ipc.ProjectConfigApplyArgs{
		Project: projectID, Upserts: upserts, DeleteKeys: deleteKeys,
	}
	if projectID != "" && projectID == s.userProjectID {
		args.Project = ""
		args.UserScope = true
	}
	return s.client.ProjectConfigApply(ctx, args)
}
