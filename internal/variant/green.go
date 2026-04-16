package variant

// greenProfile — Classic green Ralph. The default workhorse.
// Spec: docs/variants/green-ralph.md.
//
// Infinite loop for one repo service at a time, up to 6 parallel
// worktrees, with model tiering (haiku → sonnet → opus) delegated
// through the provider binding.
func greenProfile() Profile {
	return Profile{
		Name:                 Green,
		Description:          "RALPH SMASH. Unlimited loop, full power, model tiering, 6 parallel subagents.",
		AttachedAllowed:      false,
		DurableAllowed:       true,
		Isolation:            IsolationMirrorPool,
		MaxParallelWorktrees: 6,
		Models: map[Stage]Model{
			StageExecute: ModelSonnet, // default workhorse
			StageReview:  ModelSonnet, // PR review via sonnet per spec
			// haiku/opus spawned via Agent tool by the session itself,
			// not by the runtime directly — they aren't stages.
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
			"Prefer steady forward progress over dramatic rewrites.",
			"Run the relevant verification before marking a task done.",
			"When context is thin, inspect the codebase and local docs before broad edits.",
		},
	}
}
