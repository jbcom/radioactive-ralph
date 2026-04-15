package variant

// oldManProfile — Old-Man Ralph (The Maestro). Totalitarian branch imposition.
// Spec: docs/variants/old-man-ralph.md.
//
// Single pass, force-resets branches to operator's target state,
// resolves all merge conflicts with `-X ours`, deletes blocking
// branches, closes opposing PRs. Protected branches (main, master,
// production, release*) are exempt even for old-man. Uses sonnet for
// execution (precision, not escalation); one opus planning call
// allowed when the target state itself requires judgment.
//
// Destructive variant: pins ObjectStoreFull (independent objects so
// forceful history rewrites can't corrupt the operator's shared
// objects), fresh confirm per invocation, refuses service context,
// refuses default branches.
func oldManProfile() Profile {
	return Profile{
		Name:                 OldMan,
		Description:          "Maestro mode. Force-resets branches to your vision, resolves conflicts -X ours, removes obstacles. Gated.",
		Isolation:            IsolationMirrorSingle, // one worktree, one surgical strike
		MaxParallelWorktrees: 1,
		Models: map[Stage]Model{
			StagePlan:    ModelOpus, // when target state needs judgment
			StageExecute: ModelSonnet,
		},
		ToolAllowlist: []string{
			ToolAgent, ToolBash, ToolEdit, ToolGlob,
			ToolGrep, ToolRead, ToolWrite,
		},
		Termination:      TerminationSinglePass,
		ConfirmationGate: "--confirm-no-mercy",
		SafetyFloors: SafetyFloors{
			// Destructive variant — independent object store so force
			// pushes and history rewrites can't blow up shared objects
			// referenced by the operator's working repo.
			ObjectStore:               ObjectStoreFull,
			RefuseDefaultBranch:       true, // main/master/production/release* exempt
			FreshConfirmPerInvocation: true,
			RefuseServiceContext:      true,
		},
		ObjectStoreDefault: ObjectStoreFull,
		SyncSourceDefault:  SyncSourceBoth,
		LFSModeDefault:     LFSOnDemand,
		SkillBiases: map[BiasCategory]BiasSnippet{
			BiasBrainstorm: "Before imposing, confirm the target state via /{skill} — the Maestro is ruthless, not suicidal.",
		},
	}
}
