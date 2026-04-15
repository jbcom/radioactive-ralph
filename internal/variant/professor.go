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
		SkillBiases: map[BiasCategory]BiasSnippet{
			BiasBrainstorm: "Planning phase: run /{skill} to stress-test the task ranking before execution.",
			BiasDocsQuery:  "During planning, query /{skill} for canonical references on any unfamiliar framework.",
			BiasReview:     "After each execution task ships a PR, invoke /{skill} on the diff as part of reflection.",
		},
	}
}
