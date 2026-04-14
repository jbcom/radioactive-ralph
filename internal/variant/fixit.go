package variant

// fixitProfile — Fixit Ralph. Advisor + ROI-scored N-cycle worker.
// Spec: skills/fixit-ralph/SKILL.md (renamed from joe-fixit-ralph 2026-04-14).
//
// Fixit is the ONLY variant permitted to recommend peer variants.
// Modes are auto-detected at runtime by the supervisor based on plans
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
// Single repo per invocation. Sonnet default; haiku allowed in ROI
// mode for purely mechanical work. No opus — "too expensive labor"
// even in advisor mode.
func fixitProfile() Profile {
	return Profile{
		Name:                 Fixit,
		Description:          "Hammer and clipboard. Recommends a variant if plans are missing; otherwise bangs out ROI-ranked PRs with a bill.",
		Isolation:            IsolationMirrorSingle,
		MaxParallelWorktrees: 1,
		Models: map[Stage]Model{
			// StagePlan = advisor mode analysis (read-heavy reasoning).
			StagePlan:    ModelSonnet,
			StageExecute: ModelSonnet,
			StageReview:  ModelSonnet,
			// haiku spawned inside the session via Agent when ROI favors
			// it. Supervisor default is sonnet for both modes.
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
			// supervisor enforces this distinction at mode-entry time.
			RequireSpendCap: true,
		},
		ObjectStoreDefault: ObjectStoreReference,
		SyncSourceDefault:  SyncSourceBoth,
		LFSModeDefault:     LFSPointersOnly,
		SkillBiases: map[BiasCategory]BiasSnippet{
			BiasBrainstorm: "Advisor mode: use /{skill} to stress-test variant recommendations before writing the plan file.",
			BiasReview:     "ROI mode: validate every PR through /{skill} to enforce the ≤5-file / ≤200-LOC discipline.",
			BiasDocsQuery:  "Query /{skill} when the plan mentions a library you don't have cached context for.",
		},
	}
}
