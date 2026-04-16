package variant

// redProfile — Red Ralph (Principal Skinner). Single-cycle triage.
// Spec: docs/variants/red-ralph.md.
//
// One cycle only, exits when done. Up to 8 parallel agents (speed
// matters in triage). Sonnet default; STUCK agents respawn once at
// opus. No haiku — triage needs reasoning.
func redProfile() Profile {
	return Profile{
		Name:                 Red,
		Description:          "Principal precision. One cycle, triage CI/PR fires, report, exit.",
		AttachedAllowed:      true,
		DurableAllowed:       true,
		Isolation:            IsolationMirrorPool,
		MaxParallelWorktrees: 8,
		Models: map[Stage]Model{
			StageExecute: ModelSonnet,
			StageReview:  ModelSonnet,
			// StageReflect = final battlefield-report generation.
			StageReflect: ModelSonnet,
			// Escalation to opus happens dynamically on STUCK — not a
			// stage, it's a retry decision made by the runtime.
		},
		ToolAllowlist: []string{
			ToolAgent, ToolBash, ToolEdit, ToolGlob,
			ToolGrep, ToolRead, ToolWrite,
		},
		Termination:        TerminationSinglePass,
		ObjectStoreDefault: ObjectStoreReference,
		SyncSourceDefault:  SyncSourceBoth,
		LFSModeDefault:     LFSOnDemand,
		PromptDirectives: []string{
			"Start from the failing symptom and isolate the root cause before broad edits.",
			"Keep triage focused on the active incident, failing check, or review-blocking regression.",
		},
	}
}
