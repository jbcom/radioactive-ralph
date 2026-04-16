package variant

// professorProfile — Professor Ralph. Plan → Execute → Reflect.
// Spec: docs/variants/professor-ralph.md.
//
// Three-phase cycle: opus planning, sonnet execution (up to 4
// parallel), sonnet reflection. 5min sleep between cycles. No haiku
// (brainpower is the identity).
func professorProfile() Profile {
	return Profile{
		Name:                 Professor,
		Description:          "Brains and brawn. Opus plans, sonnet executes, sonnet reflects. Think first, smash second.",
		AttachedAllowed:      false,
		DurableAllowed:       true,
		Isolation:            IsolationMirrorPool,
		MaxParallelWorktrees: 4, // spec caps at 4 execution agents
		Models: map[Stage]Model{
			StagePlan:    ModelOpus,   // strategic reasoning per spec
			StageExecute: ModelSonnet, // standard implementation
			StageReflect: ModelSonnet, // STATE.md update
			StageReview:  ModelSonnet, // opportunistic PR review
		},
		ToolAllowlist: []string{
			ToolAgent, ToolBash, ToolEdit, ToolGlob,
			ToolGrep, ToolRead, ToolWrite,
		},
		Termination:        TerminationInfinite,
		ObjectStoreDefault: ObjectStoreReference,
		SyncSourceDefault:  SyncSourceBoth,
		LFSModeDefault:     LFSOnDemand,
		PromptDirectives: []string{
			"Plan before editing, then execute deliberately, then reflect on what changed.",
			"Prefer explicit rationale and ranked next steps over improvisation.",
		},
	}
}
