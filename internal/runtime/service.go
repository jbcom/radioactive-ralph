package runtime

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/jbcom/radioactive-ralph/internal/config"
	"github.com/jbcom/radioactive-ralph/internal/db"
	"github.com/jbcom/radioactive-ralph/internal/ipc"
	"github.com/jbcom/radioactive-ralph/internal/plandag"
	"github.com/jbcom/radioactive-ralph/internal/provider"
	"github.com/jbcom/radioactive-ralph/internal/variant"
	"github.com/jbcom/radioactive-ralph/internal/workspace"
	"github.com/jbcom/radioactive-ralph/internal/xdg"
)

const (
	servicePIDName = "service.pid"
)

// RunnerFactory resolves a configured provider binding into a runnable backend.
type RunnerFactory func(binding provider.Binding) (provider.Runner, error)

// Options configures one repo-scoped runtime instance.
type Options struct {
	RepoPath          string
	TickInterval      time.Duration
	HeartbeatInterval time.Duration
	ShutdownTimeout   time.Duration
	RunnerFactory     RunnerFactory
	SessionMode       plandag.SessionMode
	SessionTransport  plandag.SessionTransport
	VariantFilter     variant.Name
	ExitWhenIdle      bool
}

type workerState struct {
	PlanID            string
	TaskID            string
	Variant           variant.Name
	Provider          string
	ProviderSessionID string
	Worktree          *workspace.Worktree
	SessionVar        string
}

// Service is the durable repo-scoped runtime authority.
type Service struct {
	opts       Options
	paths      xdg.Paths
	started    time.Time
	eventDB    *db.DB
	planStore  *plandag.Store
	server     *ipc.Server
	cfg        config.File
	local      config.Local
	sessionID  string
	pidFD      *os.File
	pidFile    string
	shutdownCh chan struct{}

	mu       sync.Mutex
	workers  map[string]workerState
	managers map[variant.Name]*workspace.Manager
	wg       sync.WaitGroup
}

// NewService constructs a repo-scoped runtime with validated defaults.
func NewService(opts Options) (*Service, error) {
	if opts.RepoPath == "" {
		return nil, errors.New("runtime: RepoPath required")
	}
	if opts.TickInterval <= 0 {
		opts.TickInterval = time.Second
	}
	if opts.HeartbeatInterval <= 0 {
		opts.HeartbeatInterval = 10 * time.Second
	}
	if opts.ShutdownTimeout <= 0 {
		opts.ShutdownTimeout = 30 * time.Second
	}
	if opts.RunnerFactory == nil {
		opts.RunnerFactory = provider.NewRunner
	}
	if opts.SessionMode == "" {
		opts.SessionMode = plandag.SessionModeDurable
	}
	if opts.SessionTransport == "" {
		opts.SessionTransport = plandag.SessionTransportSocket
	}
	paths, err := xdg.Resolve(opts.RepoPath)
	if err != nil {
		return nil, fmt.Errorf("runtime: resolve paths: %w", err)
	}
	return &Service{
		opts:       opts,
		paths:      paths,
		shutdownCh: make(chan struct{}),
		workers:    map[string]workerState{},
		managers:   map[variant.Name]*workspace.Manager{},
	}, nil
}

// Run starts the repo runtime and blocks until shutdown or context cancel.
func (s *Service) Run(ctx context.Context) error {
	s.started = time.Now()
	if err := s.paths.Ensure(); err != nil {
		return fmt.Errorf("ensure state dirs: %w", err)
	}
	pidPath := filepath.Join(s.paths.Sessions, servicePIDName)
	fd, err := acquirePIDLock(pidPath)
	if err != nil {
		return err
	}
	s.pidFD = fd
	s.pidFile = pidPath

	if err := s.loadRepoState(); err != nil {
		s.cleanupPID()
		return err
	}
	defer s.cleanup()

	socketPath, heartbeatPath := ipc.ServiceEndpoint(s.paths.Sessions)
	srv, err := ipc.NewServer(ipc.ServerOptions{
		SocketPath:    socketPath,
		HeartbeatPath: heartbeatPath,
		Handler:       &handler{svc: s},
	})
	if err != nil {
		return fmt.Errorf("ipc server: %w", err)
	}
	s.server = srv
	if err := srv.Start(s.opts.HeartbeatInterval); err != nil {
		return fmt.Errorf("ipc start: %w", err)
	}

	if err := s.logEvent(ctx, "service.start", map[string]any{
		"repo": s.opts.RepoPath,
		"pid":  os.Getpid(),
	}); err != nil {
		_ = err
	}

	workerCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		s.schedulerLoop(workerCtx)
	}()

	select {
	case <-ctx.Done():
	case <-s.shutdownCh:
	}
	cancel()
	s.wg.Wait()
	_ = s.logEvent(context.Background(), "service.stop", map[string]any{
		"repo":   s.opts.RepoPath,
		"uptime": time.Since(s.started).String(),
	})
	return nil
}

// Shutdown asks the service loop to stop gracefully.
func (s *Service) Shutdown() {
	select {
	case <-s.shutdownCh:
	default:
		close(s.shutdownCh)
	}
}

func (s *Service) loadRepoState() error {
	eventDB, err := db.Open(context.Background(), s.paths.StateDB)
	if err != nil {
		return fmt.Errorf("open event db: %w", err)
	}
	s.eventDB = eventDB

	planStore, err := openPlanStore(context.Background())
	if err != nil {
		return fmt.Errorf("open plan store: %w", err)
	}
	s.planStore = planStore

	if cfg, err := config.Load(s.opts.RepoPath); err == nil {
		s.cfg = cfg
	} else if !config.IsMissingConfig(err) {
		return fmt.Errorf("config.Load: %w", err)
	}
	if local, err := config.LoadLocal(s.opts.RepoPath); err == nil {
		s.local = local
	} else if !config.IsMissingLocal(err) {
		return fmt.Errorf("config.LoadLocal: %w", err)
	}
	if s.cfg.Providers == nil {
		s.cfg.Providers = map[string]config.ProviderFile{
			"claude": config.DefaultClaudeProvider(),
			"codex":  config.DefaultCodexProvider(),
			"gemini": config.DefaultGeminiProvider(),
		}
	}
	if s.cfg.DefaultProvider == "" {
		s.cfg.DefaultProvider = "claude"
	}
	if err := s.validateProviderBindings(); err != nil {
		return err
	}

	sessionID, err := s.planStore.CreateSession(context.Background(), plandag.SessionOpts{
		Mode:         s.opts.SessionMode,
		Transport:    s.opts.SessionTransport,
		PID:          os.Getpid(),
		PIDStartTime: fmt.Sprintf("service-%d", os.Getpid()),
		Host:         hostnameOrLocal(),
	})
	if err != nil {
		return fmt.Errorf("create durable session: %w", err)
	}
	s.sessionID = sessionID
	return nil
}

func (s *Service) validateProviderBindings() error {
	seen := map[string]bool{}
	for _, profile := range variant.All() {
		variantCfg := s.cfg.Variants[string(profile.Name)]
		binding, err := provider.ResolveBinding(s.cfg, s.local, profile, variantCfg)
		if err != nil {
			return fmt.Errorf("provider binding for variant %s: %w", profile.Name, err)
		}
		if seen[binding.Name] {
			continue
		}
		seen[binding.Name] = true
		if err := provider.ValidateBinding(binding); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) cleanup() {
	if s.server != nil {
		_ = s.server.Stop()
	}
	for _, mgr := range s.managers {
		for _, wt := range mgr.Pool() {
			_ = mgr.ReleaseWorktree(context.Background(), wt)
		}
	}
	if s.planStore != nil {
		if s.sessionID != "" {
			_ = s.planStore.CloseSession(context.Background(), s.sessionID)
		}
		_ = s.planStore.Close()
	}
	if s.eventDB != nil {
		_ = s.eventDB.Close()
	}
	s.cleanupPID()
}

func (s *Service) cleanupPID() {
	if s.pidFD != nil {
		_ = s.pidFD.Close()
		s.pidFD = nil
	}
	if s.pidFile != "" {
		_ = os.Remove(s.pidFile)
		s.pidFile = ""
	}
}

func (s *Service) schedulerLoop(ctx context.Context) {
	ticker := time.NewTicker(s.opts.TickInterval)
	defer ticker.Stop()
	for {
		if s.opts.ExitWhenIdle && s.idle() {
			s.Shutdown()
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			_ = s.planStore.HeartbeatSession(context.Background(), s.sessionID)
			s.heartbeatWorkers(context.Background())
			s.dispatchOnce(ctx)
		}
	}
}

func (s *Service) dispatchOnce(ctx context.Context) {
	dispatched := false
	plans, err := s.planStore.ListPlans(ctx, []plandag.PlanStatus{plandag.PlanStatusActive})
	if err != nil {
		return
	}
	for _, plan := range plans {
		if plan.RepoPath != s.opts.RepoPath {
			continue
		}
		_ = s.planStore.AttachPlan(ctx, s.sessionID, plan.ID)
		ready, err := s.planStore.Ready(ctx, plan.ID)
		if err != nil || len(ready) == 0 {
			continue
		}
		for _, candidate := range ready {
			profile, err := chooseProfile(candidate, plan)
			if err != nil {
				continue
			}
			if s.opts.VariantFilter != "" && profile.Name != s.opts.VariantFilter {
				continue
			}
			if !s.variantAllowed(profile) || !s.hasCapacity(profile) {
				continue
			}
			sessionVarID, err := s.planStore.CreateSessionVariant(ctx, plandag.SessionVariantOpts{
				SessionID:           s.sessionID,
				VariantName:         string(profile.Name),
				SubprocessPID:       os.Getpid(),
				SubprocessStartTime: fmt.Sprintf("worker-%d-%d", os.Getpid(), time.Now().UnixNano()),
			})
			if err != nil {
				continue
			}
			task, err := s.planStore.ClaimNextReady(ctx, plan.ID, string(profile.Name), s.sessionID, sessionVarID)
			if err != nil {
				continue
			}
			s.startWorker(ctx, plan, *task, profile, sessionVarID)
			dispatched = true
		}
	}
	if s.opts.ExitWhenIdle && !dispatched && s.idle() {
		s.Shutdown()
	}
}

func (s *Service) startWorker(ctx context.Context, plan plandag.Plan, task plandag.Task, profile variant.Profile, sessionVariantID string) {
	worktree, mgr, err := s.acquireWorkspace(ctx, profile)
	if err != nil {
		_, _ = s.planStore.MarkFailed(ctx, plan.ID, task.ID, s.sessionID, err.Error(), 0)
		return
	}
	key := workerKey(plan.ID, task.ID)
	s.mu.Lock()
	s.workers[key] = workerState{
		PlanID:     plan.ID,
		TaskID:     task.ID,
		Variant:    profile.Name,
		Worktree:   worktree,
		SessionVar: sessionVariantID,
	}
	s.mu.Unlock()
	_ = s.planStore.SetSessionVariantTask(ctx, sessionVariantID, plan.ID, task.ID)

	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		defer s.finishWorker(ctx, key, mgr, worktree)
		s.executeWorker(ctx, plan, task, profile, worktree)
	}()
}

func (s *Service) finishWorker(ctx context.Context, key string, mgr *workspace.Manager, wt *workspace.Worktree) {
	sessionVariantID := ""
	s.mu.Lock()
	if worker, ok := s.workers[key]; ok {
		sessionVariantID = worker.SessionVar
	}
	delete(s.workers, key)
	s.mu.Unlock()
	if sessionVariantID != "" && s.planStore != nil {
		_ = s.planStore.ClearSessionVariantTask(ctx, sessionVariantID, "idle")
	}
	if mgr != nil && wt != nil {
		_ = mgr.ReleaseWorktree(ctx, wt)
	}
}

func (s *Service) executeWorker(ctx context.Context, plan plandag.Plan, task plandag.Task, profile variant.Profile, wt *workspace.Worktree) {
	variantCfg := s.cfg.Variants[string(profile.Name)]
	binding, err := provider.ResolveBinding(s.cfg, s.local, profile, variantCfg)
	if err != nil {
		_, _ = s.planStore.MarkFailed(ctx, plan.ID, task.ID, s.sessionID, err.Error(), 0)
		return
	}
	runner, err := s.opts.RunnerFactory(binding)
	if err != nil {
		_, _ = s.planStore.MarkFailed(ctx, plan.ID, task.ID, s.sessionID, err.Error(), 0)
		return
	}
	workDir := s.opts.RepoPath
	if wt != nil && wt.Path != "" {
		workDir = wt.Path
	}
	request := provider.Request{
		WorkingDir:   workDir,
		SystemPrompt: buildWorkerSystemPrompt(promptContext{Variant: profile, Plan: plan, Task: task, Repo: s.opts.RepoPath}),
		UserPrompt:   buildWorkerUserPrompt(promptContext{Variant: profile, Plan: plan, Task: task, Repo: s.opts.RepoPath}),
		OutputSchema: workerResultSchema(),
		Model:        profile.ModelForStage(variant.StageExecute),
		Effort:       defaultEffort(profile.ModelForStage(variant.StageExecute)),
		AllowedTools: append([]string(nil), profile.ToolAllowlist...),
	}
	result, err := runner.Run(ctx, binding, request)
	if err != nil {
		_, _ = s.planStore.MarkFailed(ctx, plan.ID, task.ID, s.sessionID, err.Error(), 2)
		_ = s.logEvent(ctx, "worker.error", map[string]any{
			"plan_id":  plan.ID,
			"task_id":  task.ID,
			"variant":  profile.Name,
			"provider": binding.Name,
			"error":    err.Error(),
		})
		return
	}
	s.recordWorkerProvider(plan.ID, task.ID, binding.Name, result.SessionID)
	parsed, err := parseWorkerResult(result.AssistantOutput)
	if err != nil {
		_, _ = s.planStore.MarkFailed(ctx, plan.ID, task.ID, s.sessionID, err.Error(), 0)
		return
	}

	payload, _ := json.Marshal(parsed)
	taskPayload := plandag.TaskEventPayload{
		Summary:           parsed.Summary,
		Evidence:          parsed.Evidence,
		Reason:            parsed.Reason,
		HandoffTo:         parsed.HandoffTo,
		ApprovalRequired:  parsed.ApprovalRequired,
		Retryable:         parsed.Retryable,
		NeedsContext:      parsed.NeedsContext,
		Provider:          binding.Name,
		ProviderSessionID: result.SessionID,
	}
	switch parsed.Outcome {
	case "done":
		_, _ = s.planStore.MarkDone(ctx, plan.ID, task.ID, s.sessionID, string(payload))
	case "handoff":
		_ = s.planStore.RequeueTaskWithPayload(ctx, plan.ID, task.ID, s.sessionID, taskPayload, parsed.HandoffTo, parsed.ApprovalRequired)
	case "need_operator":
		taskPayload.ApprovalRequired = true
		_ = s.planStore.RequeueTaskWithPayload(ctx, plan.ID, task.ID, s.sessionID, taskPayload, parsed.HandoffTo, true)
	case "blocked", "need_context":
		_ = s.planStore.MarkBlocked(ctx, plan.ID, task.ID, s.sessionID, taskPayload)
	default:
		maxRetries := 0
		if parsed.Retryable {
			maxRetries = task.RetryCount + 1
		}
		_, _ = s.planStore.MarkFailedWithPayload(ctx, plan.ID, task.ID, s.sessionID, taskPayload, maxRetries)
	}
	_ = s.logEvent(ctx, "worker.result", map[string]any{
		"plan_id":             plan.ID,
		"task_id":             task.ID,
		"variant":             profile.Name,
		"provider":            binding.Name,
		"provider_session_id": result.SessionID,
		"raw_summary":         parsed.Summary,
		"outcome":             parsed.Outcome,
		"retryable":           parsed.Retryable,
		"handoff_to":          parsed.HandoffTo,
		"needs_context":       parsed.NeedsContext,
		"approval_required":   parsed.ApprovalRequired,
	})
}

func (s *Service) recordWorkerProvider(planID, taskID, providerName, providerSessionID string) {
	key := workerKey(planID, taskID)
	s.mu.Lock()
	defer s.mu.Unlock()
	worker, ok := s.workers[key]
	if !ok {
		return
	}
	worker.Provider = providerName
	worker.ProviderSessionID = providerSessionID
	s.workers[key] = worker
}

func (s *Service) heartbeatWorkers(ctx context.Context) {
	if s.planStore == nil {
		return
	}
	s.mu.Lock()
	sessionVariants := make([]string, 0, len(s.workers))
	for _, worker := range s.workers {
		if worker.SessionVar != "" {
			sessionVariants = append(sessionVariants, worker.SessionVar)
		}
	}
	s.mu.Unlock()
	for _, sessionVariantID := range sessionVariants {
		_ = s.planStore.HeartbeatSessionVariant(ctx, sessionVariantID)
	}
}

func (s *Service) acquireWorkspace(ctx context.Context, profile variant.Profile) (*workspace.Worktree, *workspace.Manager, error) {
	manager, err := s.managerForVariant(profile)
	if err != nil {
		return nil, nil, err
	}
	if err := manager.Init(ctx); err != nil {
		return nil, nil, err
	}
	if err := manager.Reconcile(ctx); err != nil {
		return nil, nil, err
	}
	wt, err := manager.AcquireWorktree(ctx)
	if err != nil {
		return nil, manager, err
	}
	return wt, manager, nil
}

func (s *Service) managerForVariant(profile variant.Profile) (*workspace.Manager, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if mgr, ok := s.managers[profile.Name]; ok {
		return mgr, nil
	}
	override := s.cfg.Variants[string(profile.Name)]
	objectStore := resolveObjectStore(firstNonEmptyString(override.ObjectStore, s.cfg.Service.DefaultObjectStore, string(profile.ObjectStoreDefault)))
	syncSource := resolveSyncSource(firstNonEmptyString(override.SyncSource, string(profile.SyncSourceDefault)))
	lfsMode := resolveLFSMode(firstNonEmptyString(override.LfsMode, s.cfg.Service.DefaultLfsMode, string(profile.LFSModeDefault)))
	mgr, err := workspace.New(s.opts.RepoPath, profile, profile.Isolation, objectStore, syncSource, lfsMode)
	if err != nil {
		return nil, err
	}
	if s.cfg.Service.CopyHooks != nil {
		mgr.CopyHooksEnabled = *s.cfg.Service.CopyHooks
	}
	s.managers[profile.Name] = mgr
	return mgr, nil
}

func (s *Service) hasCapacity(profile variant.Profile) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	limit := profile.MaxParallelWorktrees
	if limit <= 0 {
		limit = 1
	}
	active := 0
	for _, worker := range s.workers {
		if worker.Variant == profile.Name {
			active++
		}
	}
	if s.cfg.Service.AllowConcurrentVariants != nil && !*s.cfg.Service.AllowConcurrentVariants && len(s.workers) > 0 {
		return false
	}
	return active < limit
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func (s *Service) variantAllowed(profile variant.Profile) bool {
	switch s.opts.SessionMode {
	case plandag.SessionModeAttached:
		return profile.AttachedAllowed
	default:
		return profile.DurableAllowed
	}
}

func (s *Service) idle() bool {
	s.mu.Lock()
	activeWorkers := len(s.workers)
	s.mu.Unlock()
	if activeWorkers > 0 || s.planStore == nil {
		return false
	}
	plans, err := s.planStore.ListPlans(context.Background(), []plandag.PlanStatus{plandag.PlanStatusActive})
	if err != nil {
		return false
	}
	for _, plan := range plans {
		if plan.RepoPath != s.opts.RepoPath {
			continue
		}
		ready, err := s.planStore.Ready(context.Background(), plan.ID)
		if err != nil {
			continue
		}
		for _, task := range ready {
			profile, err := chooseProfile(task, plan)
			if err != nil {
				continue
			}
			if s.opts.VariantFilter != "" && profile.Name != s.opts.VariantFilter {
				continue
			}
			if s.variantAllowed(profile) {
				return false
			}
		}
	}
	return true
}

func (s *Service) status(ctx context.Context) (ipc.StatusReply, error) {
	var ready, approvals, blocked, running, failed, activePlans int
	if s.planStore != nil {
		if err := s.planStore.DB().QueryRowContext(ctx, `
			SELECT
			  COALESCE(SUM(CASE WHEN tasks.status IN ('pending','ready') THEN 1 ELSE 0 END), 0),
			  COALESCE(SUM(CASE WHEN tasks.status = 'ready_pending_approval' THEN 1 ELSE 0 END), 0),
			  COALESCE(SUM(CASE WHEN tasks.status = 'blocked' THEN 1 ELSE 0 END), 0),
			  COALESCE(SUM(CASE WHEN tasks.status = 'running' THEN 1 ELSE 0 END), 0),
			  COALESCE(SUM(CASE WHEN tasks.status = 'failed' THEN 1 ELSE 0 END), 0),
			  COUNT(DISTINCT plans.id)
			FROM tasks
			JOIN plans ON plans.id = tasks.plan_id
			WHERE plans.repo_path = ? AND plans.status IN ('active', 'paused')
		`, s.opts.RepoPath).Scan(&ready, &approvals, &blocked, &running, &failed, &activePlans); err != nil && !errors.Is(err, sql.ErrNoRows) {
			return ipc.StatusReply{}, err
		}
	}
	s.mu.Lock()
	activeWorkers := len(s.workers)
	workers := make([]ipc.WorkerSummary, 0, len(s.workers))
	for _, worker := range s.workers {
		summary := ipc.WorkerSummary{
			PlanID:            worker.PlanID,
			TaskID:            worker.TaskID,
			Variant:           string(worker.Variant),
			Provider:          worker.Provider,
			ProviderSessionID: worker.ProviderSessionID,
		}
		if worker.Worktree != nil {
			summary.WorktreePath = worker.Worktree.Path
		}
		workers = append(workers, summary)
	}
	s.mu.Unlock()
	return ipc.StatusReply{
		RepoPath:      s.opts.RepoPath,
		PID:           os.Getpid(),
		Uptime:        time.Since(s.started),
		ActiveWorkers: activeWorkers,
		ReadyTasks:    ready,
		ApprovalTasks: approvals,
		BlockedTasks:  blocked,
		RunningTasks:  running,
		FailedTasks:   failed,
		ActivePlans:   activePlans,
		Workers:       workers,
		HeartbeatAge:  0,
	}, nil
}

func (s *Service) logEvent(ctx context.Context, kind string, payload any) error {
	if s.eventDB == nil {
		return nil
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	_, err = s.eventDB.Append(ctx, db.Event{
		Kind:       kind,
		Stream:     "service",
		Actor:      "radioactive_ralph",
		PayloadRaw: raw,
	})
	return err
}

func chooseProfile(task plandag.Task, plan plandag.Plan) (variant.Profile, error) {
	name := task.VariantHint
	if name == "" {
		name = plan.PrimaryVariant
	}
	if name == "" {
		name = string(variant.Fixit)
	}
	return variant.Lookup(name)
}

func workerKey(planID, taskID string) string {
	return planID + ":" + taskID
}

func defaultEffort(model variant.Model) string {
	switch model {
	case variant.ModelHaiku:
		return "low"
	case variant.ModelOpus:
		return "high"
	default:
		return "medium"
	}
}

func openPlanStore(ctx context.Context) (*plandag.Store, error) {
	root, err := xdg.StateRoot()
	if err != nil {
		return nil, err
	}
	dbPath := filepath.Join(root, "plans.db")
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o750); err != nil {
		return nil, err
	}
	dsn := "file:" + dbPath + "?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)"
	return plandag.Open(ctx, plandag.Options{DSN: dsn})
}

func hostnameOrLocal() string {
	host, err := os.Hostname()
	if err != nil || host == "" {
		return "local"
	}
	return host
}

func resolveObjectStore(raw string) variant.ObjectStoreMode {
	switch raw {
	case string(variant.ObjectStoreFull):
		return variant.ObjectStoreFull
	default:
		return variant.ObjectStoreReference
	}
}

func resolveSyncSource(raw string) variant.SyncSource {
	switch raw {
	case string(variant.SyncSourceLocal):
		return variant.SyncSourceLocal
	case string(variant.SyncSourceOrigin):
		return variant.SyncSourceOrigin
	default:
		return variant.SyncSourceBoth
	}
}

func resolveLFSMode(raw string) variant.LFSMode {
	switch raw {
	case string(variant.LFSFull):
		return variant.LFSFull
	case string(variant.LFSPointersOnly):
		return variant.LFSPointersOnly
	case string(variant.LFSExcluded):
		return variant.LFSExcluded
	default:
		return variant.LFSOnDemand
	}
}
