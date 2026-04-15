package variant

// greyProfile — Original dumb grey Ralph. Single-pass mechanical hygiene.
// Spec: skills/grey-ralph/SKILL.md.
//
// One repo (cwd), haiku for every task, one sweep one PR, no loop.
// Uses mirror-single isolation to avoid contaminating the operator's
// working tree even though the scope is narrow.
func greyProfile() Profile {
	return Profile{
		Name:                 Grey,
		Description:          "Mechanical file hygiene on one repo. Haiku exclusive. Single sweep, one PR, exit.",
		Isolation:            IsolationMirrorSingle,
		MaxParallelWorktrees: 1,
		Models: map[Stage]Model{
			// 100% haiku per spec. No escalation.
			StageExecute: ModelHaiku,
		},
		ToolAllowlist: []string{
			ToolAgent, ToolBash, ToolRead, ToolWrite, ToolEdit,
			ToolGlob, ToolGrep,
		},
		Termination:        TerminationSinglePass,
		ObjectStoreDefault: ObjectStoreReference,
		SyncSourceDefault:  SyncSourceBoth,
		LFSModeDefault:     LFSPointersOnly, // grey never touches binaries
		SkillBiases:        map[BiasCategory]BiasSnippet{
			// Grey is dumb by design — no skill biases. Kept empty
			// deliberately. Any bias declared here would contradict
			// "dumb = safe".
		},
	}
}
