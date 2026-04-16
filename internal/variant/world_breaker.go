package variant

// worldBreakerProfile — World-Breaker Ralph. All-opus, all-repos, no sleep.
// Spec: docs/variants/world-breaker-ralph.md.
//
// 10 parallel agents, EVERY agent opus (including PR review and merge
// decisions), zero sleep, discovers repos beyond config.toml. Hard gate
// via --confirm-burn-everything. Durable-service-only because this is
// the highest-blast-radius mode the runtime ships.
//
// Pinned ObjectStoreFull because opus-driven changes across all repos
// simultaneously warrant isolated object stores per worktree.
func worldBreakerProfile() Profile {
	return Profile{
		Name:                 WorldBreaker,
		Description:          "Everything at opus. 10 parallel, no sleep, all repos in scope. Burns the budget. Hard gate.",
		AttachedAllowed:      false,
		DurableAllowed:       true,
		Isolation:            IsolationMirrorPool,
		MaxParallelWorktrees: 10,
		Models: map[Stage]Model{
			StagePlan:    ModelOpus,
			StageExecute: ModelOpus,
			StageReview:  ModelOpus,
			StageReflect: ModelOpus,
		},
		ToolAllowlist: []string{
			ToolAgent, ToolBash, ToolEdit, ToolGlob,
			ToolGrep, ToolRead, ToolWrite,
			// World-breaker explicitly gets the Task* tools because the
			// variant docs allow TodoWrite-style fan-out inside the run.
			ToolTaskCreate, ToolTaskUpdate, ToolTaskList,
		},
		Termination:      TerminationInfinite,
		ConfirmationGate: "--confirm-burn-everything",
		SafetyFloors: SafetyFloors{
			// Destructive-spend variant — full independent object stores
			// across N parallel worktrees so cross-repo opus edits
			// cannot pollute shared objects.
			ObjectStore:               ObjectStoreFull,
			FreshConfirmPerInvocation: true,
			RequireSpendCap:           true,
		},
		ObjectStoreDefault: ObjectStoreFull,
		SyncSourceDefault:  SyncSourceBoth,
		LFSModeDefault:     LFSOnDemand,
		PromptDirectives: []string{
			"Treat every action as high-blast-radius and high-cost; verify before compounding mistakes.",
			"Across repos, favor explicit reasoning, visible evidence, and clean handoffs over improvisation.",
		},
	}
}
