package variant

// immortalProfile — Immortal Ralph. Crash-resistant marathon loop.
// Spec: docs/variants/immortal-ralph.md.
//
// Loops forever, survives transient failures, resumes from state on
// startup. Sonnet for EVERYTHING. Max 3 parallel agents (resilience >
// speed). Refuses risky tasks — those are logged for later human or
// professor-ralph handling.
func immortalProfile() Profile {
	return Profile{
		Name:                 Immortal,
		Description:          "Crash-resistant marathon loop. Sonnet only. Retries everything. Resumes on restart.",
		Isolation:            IsolationMirrorPool,
		MaxParallelWorktrees: 3, // spec caps at 3 for resilience over speed
		Models: map[Stage]Model{
			StageExecute: ModelSonnet,
			StageReview:  ModelSonnet,
		},
		ToolAllowlist: []string{
			ToolAgent, ToolBash, ToolEdit, ToolGlob,
			ToolGrep, ToolRead, ToolWrite,
		},
		Termination:        TerminationInfinite,
		ObjectStoreDefault: ObjectStoreReference,
		SyncSourceDefault:  SyncSourceBoth,
		LFSModeDefault:     LFSOnDemand,
		SkillBiases: map[BiasCategory]BiasSnippet{
			BiasDebugging: "On transient failures, check /{skill} for known patterns before the retry-in-60s fallback.",
			BiasReview:    "Every PR merges only after /{skill} signs off — conservative is the identity.",
		},
	}
}
