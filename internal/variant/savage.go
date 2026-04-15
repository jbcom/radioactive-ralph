package variant

// savageProfile — Savage Ralph. Maximum throttle.
// Spec: docs/variants/savage-ralph.md.
//
// 10 parallel agents, every model escalated one tier (haiku→sonnet,
// sonnet→opus, opus→opus), zero sleep between cycles, all priority
// tiers worked simultaneously. Gated by --confirm-burn-budget. Refuses
// service context because burning $25-80/hr from a supervised
// launchd/systemd unit would be genuinely dangerous.
func savageProfile() Profile {
	return Profile{
		Name:                 Savage,
		Description:          "Maximum power. 10 parallel opus-heavy agents, no sleep, all priority tiers. Burns budget. Gated.",
		Isolation:            IsolationMirrorPool,
		MaxParallelWorktrees: 10,
		Models: map[Stage]Model{
			// Savage escalates tier-by-task at dispatch time. Supervisor
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
			// spend, not repo destruction. Service refusal is the real
			// safety floor: you do not hand a savage variant a cron.
			RefuseServiceContext:      true,
			FreshConfirmPerInvocation: true,
			RequireSpendCap:           true,
		},
		ObjectStoreDefault: ObjectStoreReference,
		SyncSourceDefault:  SyncSourceBoth,
		LFSModeDefault:     LFSOnDemand,
		SkillBiases: map[BiasCategory]BiasSnippet{
			BiasReview:         "Savage merges aggressively; let /{skill} flag anything that smells before a squash-merge.",
			BiasSecurityReview: "Even in savage mode, security-sensitive diffs pass through /{skill} before merge.",
			BiasDebugging:      "On test failure spikes, /{skill} is the first triage step before respawning agents.",
		},
	}
}
