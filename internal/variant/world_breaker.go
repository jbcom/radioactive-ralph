package variant

// worldBreakerProfile — World-Breaker Ralph. All-opus, all-repos, no sleep.
// Spec: docs/variants/world-breaker-ralph.md.
//
// 10 parallel agents, EVERY agent opus (including PR review and merge
// decisions), zero sleep, discovers repos beyond config.toml. Hard gate
// via --confirm-burn-everything. Refuses service context — sending a
// launchd unit into world-breaker mode would be operator malpractice.
//
// Pinned ObjectStoreFull because opus-driven changes across all repos
// simultaneously warrant isolated object stores per worktree.
func worldBreakerProfile() Profile {
	return Profile{
		Name:                 WorldBreaker,
		Description:          "Everything at opus. 10 parallel, no sleep, all repos in scope. Burns the budget. Hard gate.",
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
			RefuseServiceContext:      true,
			RequireSpendCap:           true,
		},
		ObjectStoreDefault: ObjectStoreFull,
		SyncSourceDefault:  SyncSourceBoth,
		LFSModeDefault:     LFSOnDemand,
		SkillBiases: map[BiasCategory]BiasSnippet{
			BiasReview:         "Every merge pass through /{skill} — opus reviewers still benefit from a second set of eyes.",
			BiasSecurityReview: "Cross-repo opus edits touching auth/secrets/IAM MUST route through /{skill} before merge.",
			BiasBrainstorm:     "Before committing to a cycle plan, run /{skill} on the scope — world-breaker runs are expensive.",
			BiasDebugging:      "Failures in world-breaker are systemic — use /{skill} before respawning.",
			BiasDocsQuery:      "Unfamiliar frameworks in any repo: resolve via /{skill} before editing.",
		},
	}
}
