package supervisor

import (
	"context"
	"slices"
	"time"

	"github.com/jbcom/radioactive-ralph/internal/ipc"
	"github.com/jbcom/radioactive-ralph/internal/store"
)

var hookAdapters = []string{"claude", "codex", "opencode"}

type hookRunKey struct {
	SessionID string
	PlanID    string
	TaskID    string
}

// HandleHookEvent accepts only the finite identifiers emitted by generated
// adapters. Raw provider JSON never crosses this boundary. The supervisor maps
// Ralph's opaque managed session id back to the live store claim and remains
// the only authority that can decide whether a Stop is safe.
func (s *Supervisor) HandleHookEvent(
	ctx context.Context,
	args ipc.HookEventArgs,
) (ipc.HookEventReply, error) {
	if !slices.Contains(hookAdapters, args.Adapter) ||
		(args.Event != ipc.HookEventPostToolUse && args.Event != ipc.HookEventStop) ||
		args.SessionID == "" {
		return ipc.HookEventReply{Allow: false, Reason: "invalid_event"}, nil
	}

	tasks, err := s.store.RunningHookTasks(ctx, args.SessionID)
	if err != nil {
		s.log("hook task lookup failed")
		return ipc.HookEventReply{Allow: false, Reason: "internal_error"}, nil
	}
	if len(tasks) == 0 {
		return ipc.HookEventReply{Allow: false, Reason: "not_managed"}, nil
	}
	for _, task := range tasks {
		if task.Provider != args.Adapter {
			return ipc.HookEventReply{Allow: false, Reason: "adapter_mismatch"}, nil
		}
	}

	if args.Event == ipc.HookEventPostToolUse {
		if err := s.store.InvalidateHookVerifications(ctx, args.SessionID); err != nil {
			s.log("hook verification invalidation failed")
			return ipc.HookEventReply{Allow: false, Reason: "internal_error"}, nil
		}
		seen := make(map[string]struct{}, len(tasks))
		for _, task := range tasks {
			if task.WorkerID == "" {
				return ipc.HookEventReply{Allow: false, Reason: "worker_missing"}, nil
			}
			if _, ok := seen[task.WorkerID]; ok {
				continue
			}
			seen[task.WorkerID] = struct{}{}
			if err := s.store.HeartbeatWorkerAndSession(ctx, task.WorkerID); err != nil {
				s.log("hook heartbeat failed")
				return ipc.HookEventReply{Allow: false, Reason: "internal_error"}, nil
			}
		}
		return ipc.HookEventReply{Allow: true, Reason: "progress_recorded"}, nil
	}

	states, err := s.store.HookVerificationStates(ctx, args.SessionID)
	if err != nil {
		s.log("hook verification state lookup failed")
		return ipc.HookEventReply{Allow: false, Reason: "internal_error"}, nil
	}
	allPassed := true
	started := false
	pending := false
	failed := false
	for _, task := range tasks {
		state := states[store.HookVerificationKey{PlanID: task.PlanID, TaskID: task.TaskID}]
		if state == store.HookVerificationPassed {
			continue
		}
		allPassed = false
		if state == store.HookVerificationFailed {
			failed = true
			continue
		}
		key := hookRunKey{SessionID: args.SessionID, PlanID: task.PlanID, TaskID: task.TaskID}
		if !s.claimHookRun(key) {
			pending = true
			continue
		}
		if err := s.store.SetHookVerificationPending(ctx, args.SessionID, task.PlanID, task.TaskID); err != nil {
			s.releaseHookRun(key)
			s.log("hook verification start failed")
			return ipc.HookEventReply{Allow: false, Reason: "internal_error"}, nil
		}
		started = true
		s.orch.VerifyStopAsync(task.PlanID, task.TaskID, args.SessionID, func(passed bool, verifyErr error) {
			defer s.releaseHookRun(key)
			persistCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			var err error
			if verifyErr != nil {
				err = s.store.ClearHookVerification(
					persistCtx, key.SessionID, key.PlanID, key.TaskID)
			} else {
				err = s.store.SetHookVerificationResult(
					persistCtx, key.SessionID, key.PlanID, key.TaskID, passed)
			}
			if err != nil {
				s.log("hook verification result write failed")
			}
		})
	}
	if allPassed {
		return ipc.HookEventReply{Allow: true, Reason: "acceptance_passed"}, nil
	}
	if started {
		return ipc.HookEventReply{Allow: false, Reason: "verification_started"}, nil
	}
	if pending {
		return ipc.HookEventReply{Allow: false, Reason: "verification_pending"}, nil
	}
	if failed {
		return ipc.HookEventReply{Allow: false, Reason: "verification_failed"}, nil
	}
	return ipc.HookEventReply{Allow: false, Reason: "verification_required"}, nil
}

func (s *Supervisor) claimHookRun(key hookRunKey) bool {
	s.hookRunsMu.Lock()
	defer s.hookRunsMu.Unlock()
	if s.hookRuns == nil {
		s.hookRuns = make(map[hookRunKey]struct{})
	}
	if _, exists := s.hookRuns[key]; exists {
		return false
	}
	s.hookRuns[key] = struct{}{}
	return true
}

func (s *Supervisor) releaseHookRun(key hookRunKey) {
	s.hookRunsMu.Lock()
	delete(s.hookRuns, key)
	s.hookRunsMu.Unlock()
}
