package variant

// savageProfile — Savage Ralph. Maximum throttle.
// Spec: docs/variants/savage-ralph.md.
//
// 10 parallel agents, every model escalated one tier (haiku→sonnet,
// sonnet→opus, opus→opus), zero sleep between cycles, all priority
// tiers worked simultaneously. Gated by --confirm-burn-budget.
// Durable-service-only because burning $25-80/hr belongs behind
// explicit operator gates, spend caps, and repo-service supervision.
func savageProfile() Profile {
	return Profile{
		Name:                 Savage,
		Description:          "Maximum power. 10 parallel opus-heavy agents, no sleep, all priority tiers. Burns budget. Gated.",
		AttachedAllowed:      false,
		DurableAllowed:       true,
		Isolation:            IsolationMirrorPool,
		MaxParallelWorktrees: 10,
		Models: map[Stage]Model{
			// Savage escalates tier-by-task at dispatch time. Runtime
			// default for standard stages is opus because savage's
			// identity is "everything hotter than it needs to be".
			StageExecute: ModelOpus,
			StageReview:  ModelOpus,
			StagePlan:    ModelOpus,
			StageReflect: ModelOpus,
		},
		ToolAllowlist: []string{
			ToolAgent, ToolBash, ToolEdit, ToolGlob,
			ToolGrep, ToolRead, ToolWrite,
		},
		Termination:      TerminationInfinite,
		ConfirmationGate: "--confirm-burn-budget",
		SafetyFloors: SafetyFloors{
			// Savage still respects core git safety (no force-push, no
			// --admin) but does not pin object_store — its hazard is
			// spend, not repo destruction.
			FreshConfirmPerInvocation: true,
			RequireSpendCap:           true,
		},
		ObjectStoreDefault: ObjectStoreReference,
		SyncSourceDefault:  SyncSourceBoth,
		LFSModeDefault:     LFSOnDemand,
		PromptDirectives: []string{
			"Move fast and parallelize aggressively, but respect approval gates and spend caps.",
			"Before finalizing risky changes, do one deliberate sanity pass on blast radius.",
		},
	}
}
