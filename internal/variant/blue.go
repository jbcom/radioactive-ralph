package variant

// blueProfile — Blue Ralph (Captain Universe fusion). Read-only observer.
// Spec: docs/variants/blue-ralph.md.
//
// Structurally incapable of modifying repos: Edit and Write are
// excluded from the tool allowlist. Shared isolation is therefore
// safe — no worktree needed. Sonnet for every review (no haiku
// because reviews need reasoning; no opus because budget discipline).
func blueProfile() Profile {
	return Profile{
		Name:        Blue,
		Description: "Calm observer. Read-only review across all configured repos. Never writes.",
		Isolation:   IsolationShared,
		// Shared isolation — operator's repo is the workspace, but tool
		// allowlist forbids writes, so this is safe. No worktree pool.
		MaxParallelWorktrees: 0,
		Models: map[Stage]Model{
			StageReview:  ModelSonnet,
			StageExecute: ModelSonnet, // no execution, but fallback is defined
		},
		// Deliberately omits Edit and Write. Validate enforces this
		// against IsolationShared.
		ToolAllowlist: []string{
			ToolAgent, ToolBash, ToolRead, ToolGlob, ToolGrep,
		},
		// Blue needs Bash for `gh pr review --comment` and other
		// read-focused gh commands. The variant docs constrain Bash to that
		// scope; we opt in explicitly rather than permit shared+Bash
		// silently.
		ShellExplicitlyTrusted: true,
		Termination:            TerminationSinglePass, // default single-pass; --loop optional at CLI layer
		ObjectStoreDefault:     "",                    // N/A for shared isolation
		SyncSourceDefault:      "",                    // N/A for shared isolation
		LFSModeDefault:         LFSOnDemand,           // reads may need LFS content
		SkillBiases: map[BiasCategory]BiasSnippet{
			BiasReview:         "Invoke /{skill} as the review engine for every PR diff and governance scan.",
			BiasSecurityReview: "When the diff touches auth, secrets, or IAM, add a second pass with /{skill}.",
		},
	}
}
