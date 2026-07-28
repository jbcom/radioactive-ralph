package main

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/jbcom/radioactive-ralph/internal/orch"
	"github.com/jbcom/radioactive-ralph/internal/rlog"
	"github.com/jbcom/radioactive-ralph/internal/store"
	"github.com/jbcom/radioactive-ralph/internal/supervisor"
	"github.com/jbcom/radioactive-ralph/internal/xdg"
)

const (
	maxParallelEnv   = "RALPH_MAX_PARALLEL"
	maxParallelLimit = 256
)

// runSupervisorMode opens the single user-level store (spec §6) and runs
// the supervisor until ctx is cancelled or a client asks it to stop. The
// working directory is irrelevant here — everything is keyed off the XDG
// state root, never the caller's cwd (spec §4).
//
// logFormat selects internal/rlog's output shape ("text" or "json"):
// structured JSON logging matters here because the supervisor is the one
// long-lived process an operator or the E2E harness needs to observe from
// outside (tailing stderr, grepping for a lifecycle event) — a stream-json
// line per lifecycle/reaper event is far easier to assert on than an
// ad-hoc fmt.Fprintf line shape.
func runSupervisorMode(ctx context.Context, logFormat string) error {
	stateRoot, err := xdg.StateRoot()
	if err != nil {
		return fmt.Errorf("resolve state root: %w", err)
	}
	if err := os.MkdirAll(stateRoot, 0o700); err != nil {
		return fmt.Errorf("create state root: %w", err)
	}

	st, err := store.Open(ctx, store.Options{DSN: store.DSN(storeDBPath(stateRoot))})
	if err != nil {
		return fmt.Errorf("open store: %w", err)
	}
	defer func() { _ = st.Close() }()

	mode := rlog.ModeText
	if logFormat == "json" {
		mode = rlog.ModeJSON
	}
	logger := rlog.New(mode, os.Stderr)

	maxParallel, err := supervisorMaxParallel(os.LookupEnv)
	if err != nil {
		return err
	}

	// Build the orchestrator with a config-backed binding resolver: stored
	// virtual config (a project's or the user's default_provider/provider
	// key) selects the provider, instead of every dispatch deterministically
	// falling back to the built-in claude binding. Without this, the
	// supervisor's default orch.New(store) would ignore stored config
	// entirely and always run claude.
	// Containment is resolved the same way, and for the same reason: the
	// contain_provider_writes key is stored PER PROJECT, so a process-wide flag
	// could only apply one project's answer to every project on the host.
	// Without this the key is inert — vconfig parses it and nothing consults it,
	// so an operator who enables containment gets none and no indication that
	// the setting did nothing. A config that lies is worse than a missing one.
	orchestratorOptions := []orch.Option{
		orch.WithBindingResolver(storeBindingResolver(st)),
		orch.WithContainmentResolver(storeContainmentResolver(st)),
	}
	if maxParallel > 0 {
		orchestratorOptions = append(orchestratorOptions, orch.WithMaxParallel(maxParallel))
	}
	orchestrator := orch.New(st, orchestratorOptions...)

	logger.Info("supervisor.starting", "state_root", stateRoot, "max_parallel", maxParallel)
	err = supervisor.Run(ctx, supervisor.Options{
		RuntimeDir:   stateRoot,
		Store:        st,
		Orchestrator: orchestrator,
		Logger: func(msg string, args ...any) {
			logger.Info(msg, args...)
		},
	})
	// supervisor.Run's errors already carry the "supervisor:" prefix; don't
	// double it.
	return err
}

// supervisorMaxParallel converts the launchd/systemd-friendly environment
// setting into the orchestrator's process-wide emergency worker ceiling.
// Unset preserves the legacy unbounded default; neither mode is adaptive or a
// recommended optimum. A present value must be positive so a typo cannot
// silently disable the bound.
func supervisorMaxParallel(lookupEnv func(string) (string, bool)) (int, error) {
	value, configured := lookupEnv(maxParallelEnv)
	if !configured {
		return 0, nil
	}
	raw := strings.TrimSpace(value)
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 || n > maxParallelLimit {
		return 0, fmt.Errorf("%s must be an integer from 1 through %d, got %q", maxParallelEnv, maxParallelLimit, raw)
	}
	return n, nil
}
