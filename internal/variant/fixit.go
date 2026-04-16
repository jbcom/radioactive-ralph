package variant

// fixitProfile — Fixit Ralph. Advisor + ROI-scored N-cycle worker.
// Spec: docs/variants/fixit-ralph.md.
//
// Fixit is the ONLY variant permitted to recommend peer variants.
// Modes are auto-detected at runtime by the repo service based on plans
// setup state:
//
//   - Advisor mode (no valid plans dir, malformed index, or --advise):
//     scans the codebase + any provided description, writes a
//     recommendation to .radioactive-ralph/plans/<topic>-advisor.md,
//     optionally auto-hands off to the recommended variant when
//     --auto-handoff is set AND confidence is high.
//
//   - ROI mode (valid plans setup, default when one exists): bangs the
//     hammer on the top-ranked task per cycle, ≤5 files / ≤200 LOC
//     PRs, N cycles then bill.
//
// Single repo per invocation. Sonnet is the default execution tier;
// planning may scale up to opus when the operator or repo config asks
// for it.
func fixitProfile() Profile {
	return Profile{
		Name:                 Fixit,
		Description:          "Hammer and clipboard. Recommends a variant if plans are missing; otherwise bangs out ROI-ranked PRs with a bill.",
		AttachedAllowed:      true,
		DurableAllowed:       true,
		Isolation:            IsolationMirrorSingle,
		MaxParallelWorktrees: 1,
		Models: map[Stage]Model{
			// StagePlan = advisor mode analysis (read-heavy reasoning).
			StagePlan:    ModelSonnet,
			StageExecute: ModelSonnet,
			StageReview:  ModelSonnet,
			// Haiku may still be selected by provider/task policy for
			// narrowly mechanical work, but the runtime defaults fixit
			// execution to sonnet and lets the advisor path choose its
			// own planning tier.
		},
		ToolAllowlist: []string{
			ToolAgent, ToolBash, ToolEdit, ToolGlob,
			ToolGrep, ToolRead, ToolWrite,
		},
		Termination: TerminationNCycles,
		CycleLimit:  3, // ROI mode default; CLI --cycles overrides. Advisor mode exits after one pass regardless.
		SafetyFloors: SafetyFloors{
			// RequireSpendCap applies only in ROI mode. Advisor mode is
			// a single bounded pass and shouldn't need a cap — the
			// runtime enforces this distinction at mode-entry time.
			RequireSpendCap: true,
		},
		ObjectStoreDefault: ObjectStoreReference,
		SyncSourceDefault:  SyncSourceBoth,
		LFSModeDefault:     LFSPointersOnly,
		PromptDirectives: []string{
			"Prefer the smallest high-ROI step that materially improves plan state or task throughput.",
			"Advisor mode should clarify direction; execution mode should stay narrowly scoped and measurable.",
		},
	}
}
