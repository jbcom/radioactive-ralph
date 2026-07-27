package main

import (
	"context"
	"fmt"

	"github.com/jbcom/radioactive-ralph/internal/ipc"
	"github.com/jbcom/radioactive-ralph/internal/supervisor"
	"github.com/jbcom/radioactive-ralph/internal/vconfig"
)

// newSupervisorConfigSource returns a vconfig.ConfigSource backed by the
// supervisor socket.
func newSupervisorConfigSource(stateRoot string) vconfig.ConfigSource {
	return &supervisorConfigSource{stateRoot: stateRoot}
}

// dial opens a connection for ONE request. The caller closes it.
func (s *supervisorConfigSource) dial() (*ipc.Client, error) {
	client, err := supervisor.Find(s.stateRoot)
	if err != nil {
		return nil, fmt.Errorf(
			"%w: resolving project config needs a running supervisor; start one with: %s",
			errNoSupervisorListening, supervisorStartHint())
	}
	return client, nil
}

// supervisorConfigSource resolves vconfig layers over the supervisor socket.
//
// This is what lets `init` stop opening the store. The supervisor is the single
// writer of record; a client that reads and writes the database directly is a
// second writer to a database it does not own, which is the ownership split the
// one-binary architecture exists to prevent.
type supervisorConfigSource struct {
	// stateRoot rather than a held *ipc.Client: the protocol is deliberately
	// one request per connection (dial -> request -> response -> close, see
	// internal/ipc/server.go), so reusing a client across calls gets a broken
	// pipe on the second one. Config resolution makes several calls, so each
	// dials its own connection.
	stateRoot string
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
	client, err := s.dial()
	if err != nil {
		return "", err
	}
	defer func() { _ = client.Close() }()
	reply, err := client.ProjectConfigGet(ctx, ipc.ProjectConfigGetArgs{UserScope: true})
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
	client, err := s.dial()
	if err != nil {
		return nil, err
	}
	defer func() { _ = client.Close() }()
	reply, err := client.ProjectConfigGet(ctx, ipc.ProjectConfigGetArgs{Project: projectID})
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
	client, err := s.dial()
	if err != nil {
		return err
	}
	defer func() { _ = client.Close() }()
	return client.ProjectConfigApply(ctx, ipc.ProjectConfigApplyArgs{
		Project: projectID, Upserts: upserts, DeleteKeys: deleteKeys,
	})
}
