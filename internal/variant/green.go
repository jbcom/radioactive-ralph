package variant

// greenProfile — Classic green Ralph. The default workhorse.
// Spec: docs/variants/green-ralph.md.
//
// Infinite loop, all repos in config.toml, up to 6 parallel subagents,
// model tiering (haiku → sonnet → opus) per task class, 60s sleep
// between cycles.
func greenProfile() Profile {
	return Profile{
		Name:                 Green,
		Description:          "RALPH SMASH. Unlimited loop, full power, model tiering, 6 parallel subagents.",
		Isolation:            IsolationMirrorPool,
		MaxParallelWorktrees: 6,
		Models: map[Stage]Model{
			StageExecute: ModelSonnet, // default workhorse
			StageReview:  ModelSonnet, // PR review via sonnet per spec
			// haiku/opus spawned via Agent tool by the session itself,
			// not by the supervisor directly — they aren't stages.
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
			BiasReview:    "After landing a fix, invoke /{skill} on the diff before marking the task done.",
			BiasDocsQuery: "For unfamiliar third-party APIs, query /{skill} before editing.",
			BiasDebugging: "When a test fails in a way you don't recognize, use /{skill} before patching.",
		},
	}
}
