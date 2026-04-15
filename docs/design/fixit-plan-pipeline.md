---
title: Fixit Plan-Creation Pipeline
lastUpdated: 2026-04-15
---

# Fixit Plan-Creation Pipeline

Fixit-Ralph is the only variant in the Ralph family permitted to create
plans for the rest. This document specifies exactly how Fixit produces
those plans — deliberately, in stages, with constrained prompting and
hard validation gates — so the output is trustworthy enough that the
supervisor and the other nine variants can safely act on it.

The single most important property: **Fixit never freestyles.** Every
LLM call has a narrow scope, a structured input, a constrained output
schema, and a validation step. If Claude returns something off-shape,
Fixit retries with corrective context. If it still fails, Fixit writes
an explicit "I could not produce a coherent plan; here's what I tried"
report rather than guessing.

## Why deliberate pipeline matters

Letting Claude wander a repo and "produce a plan" is what happens
when nobody specifies the contract. The result is verbose, optimistic,
non-actionable, and impossible to validate. Concretely, an unbounded
Fixit advisor would:

- Produce a plan referring to files that don't exist.
- Recommend a variant whose safety floors forbid the workload.
- Skip the plans-frontmatter contract that the rest of the system
  enforces, making the output worthless to the gate it's meant to
  satisfy.
- Drift toward "implement everything yourself, claude" — exactly the
  freestyle the operator was trying to escape by asking for advice
  in the first place.

The pipeline below has six stages, each with a defined contract.
Failure at any stage halts and produces a diagnostic, not a guess.

## Stages

```
┌─────────────────────────────────────────────────────────────────┐
│ Stage 1: Operator intent capture                                │
│   • interactive --topic <slug> + optional --description text    │
│   • interactive operator questions on first run (init prompts)  │
│   • produces: IntentSpec{topic, description, constraints}       │
└─────────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────────┐
│ Stage 2: Deterministic repo exploration                         │
│   • git log --oneline -50 + git status                          │
│   • walk docs/, enumerate frontmatter status fields             │
│   • count files by extension (signal of language mix)           │
│   • read existing .radioactive-ralph/plans/ tree                │
│   • gh pr list --json (if gh authenticated)                     │
│   • gh issue list --label "ai-welcome" --json                   │
│   • capability inventory snapshot (what skills are installed)   │
│   • produces: RepoContext{commits, docs, plans, prs, issues,    │
│                            inventory}                            │
│   • NO LLM CALLS YET                                            │
└─────────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────────┐
│ Stage 3: Variant-fitness deterministic scorer                   │
│   • rule-based ranking against known signals:                   │
│       - missing governance files     → grey weight ↑            │
│       - failing CI on >1 PR          → red weight ↑             │
│       - stale STATE.md (>14d)         → professor weight ↑       │
│       - many small unprioritized      → fixit-roi weight ↑      │
│       - heavy refactor in flight      → green weight ↑          │
│       - CRITICAL incident            → world-breaker weight ↑    │
│   • EVERY variant gets a 0..100 score with explanation          │
│   • produces: VariantScores[]                                   │
│   • NO LLM CALLS YET                                            │
└─────────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────────┐
│ Stage 4: Constrained Claude analysis                            │
│   • spawn sonnet subprocess in --advisor scope                  │
│   • inputs: IntentSpec + RepoContext + VariantScores            │
│   • prompt template: structured-only, JSON output schema        │
│   • output schema (strict):                                     │
│       {                                                          │
│         "primary": "<variant-name>",                             │
│         "primary_rationale": "<one sentence>",                   │
│         "alternate": "<variant-name | null>",                   │
│         "alternate_when": "<one sentence | null>",              │
│         "tasks": [                                               │
│           {"title": "...", "effort": "S|M|L", "impact": "..."}  │
│         ],                                                       │
│         "acceptance_criteria": ["...", "..."],                   │
│         "confidence": 0..100                                     │
│       }                                                          │
│   • parse + JSON-schema validate                                │
│   • on parse failure: ONE retry with the parse error appended   │
│   • on second failure: bail to fallback (Stage 6)               │
│   • produces: PlanProposal                                      │
└─────────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────────┐
│ Stage 5: Plan validation                                        │
│   • primary + alternate both in known variant registry          │
│   • primary's safety floors compatible with this repo state     │
│     (e.g. don't recommend old-man if the operator is on main)   │
│   • acceptance criteria are concrete (no "improve quality")     │
│   • tasks reference real files when paths appear                │
│   • confidence ≥ MIN_CONFIDENCE (default 50)                    │
│   • on failure: emit warning + still write the plan but mark    │
│     status: provisional in frontmatter so other variants refuse │
│   • produces: ValidatedPlan                                     │
└─────────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────────┐
│ Stage 6: Plan emission                                          │
│   • write .radioactive-ralph/plans/<topic>-advisor.md           │
│   • frontmatter (always valid plan-format):                     │
│       title, updated, status (current|provisional|fallback),    │
│       domain, variant_recommendation, variant_alternate,        │
│       confidence                                                 │
│   • body sections: Context, Plan, Tasks, Acceptance Criteria,   │
│     Tradeoffs, Methodology (what Fixit did to produce this)     │
│   • on --auto-handoff AND status=current AND no alternate:      │
│     spawn the recommended variant immediately                    │
└─────────────────────────────────────────────────────────────────┘
```

## Contract: IntentSpec (Stage 1 output)

```go
type IntentSpec struct {
    Topic        string   // sanitized slug for the output filename
    Description  string   // operator's --description text, or scraped
                          // from a TOPIC.md file at the repo root if
                          // present, or empty
    Constraints  []string // operator-declared limits like "no opus",
                          // "stay under $5", "main only"
    AnswersToQs  map[string]string // collected from operator prompts
                                   // when interactive
}
```

When invoked non-interactively (CI, --yes, --auto-handoff cascade), Stage 1 fills
IntentSpec from the CLI flags only — no prompts.

When invoked interactively in a terminal, Stage 1 asks the operator three short questions:

1. **What's the goal?** — free text. Recorded as IntentSpec.Description.
2. **What variants are off-limits?** — multi-select from the ten variants.
   Recorded as IntentSpec.Constraints.
3. **Time/budget cap?** — one of: "single session", "~1 hour", "1-2 days",
   "1+ week", "no cap". Recorded as IntentSpec.Constraints.

Operator can pass `--non-interactive` to skip; in that case all three are blank
and Stage 4 prompts compensate by asking Claude to be more conservative.

## Contract: RepoContext (Stage 2 output)

Every field is populated from deterministic shell-outs. Zero LLM calls.

```go
type RepoContext struct {
    GitRoot           string
    CurrentBranch     string
    Commits           []GitCommit       // last 50 oneline + author + date
    DefaultBranch     string            // from origin/HEAD
    OnDefaultBranch   bool

    DocsPresent       []DocFile         // path + frontmatter map
    DocsStale         []string          // status=stale or updated > 30d ago
    DocsMissing       []string          // expected canonical docs not found

    PlansDir          string
    PlansIndexExists  bool
    PlansIndexFM      map[string]string
    PlansFiles        []string          // referenced by index.md

    GHAuthenticated   bool
    OpenPRs           []GHIssue         // num, title, draft, mergeStatus
    OpenIssues        []GHIssue
    AIWelcomeIssues   []GHIssue         // labeled ai-welcome

    Inventory         InventorySnapshot // skills/MCPs/agents installed

    LangCounts        map[string]int    // .go/.py/.ts/.tf/etc
    GovernanceMissing []string          // CHANGELOG, dependabot, etc
}
```

## Contract: VariantScores (Stage 3 output)

```go
type VariantScore struct {
    Variant     string
    Score       int      // 0..100
    Reasons     []string // bullet justifications referencing RepoContext
    Disqualifying []string // hard exclusions (e.g. "world-breaker
                            // gated and operator did not pass
                            // --confirm-burn-everything")
}
```

The scorer is rule-based and deterministic. Same input always
produces same output. Rules live in `internal/fixit/scorer.go` so
they can be unit-tested independently of the LLM pipeline.

The scorer does NOT pick a winner — it provides ranked context to
Stage 4. Picking is Claude's job because picking requires weighing
the IntentSpec + tradeoffs.

## Contract: PlanProposal (Stage 4 output)

JSON schema enforced by `json.Decoder.DisallowUnknownFields` + a
field-by-field check. Output that doesn't match → retry with the
specific schema-error message appended. Second failure → fallback
plan.

```json
{
  "primary": "grey",
  "primary_rationale": "Repo has 4 docs flagged stale and 3 missing CHANGELOG entries; mechanical hygiene before feature work has highest near-term value.",
  "alternate": "professor",
  "alternate_when": "if the operator wants a strategic plan rather than a janitor pass first",
  "tasks": [
    {"title": "Backfill CHANGELOG entries for the last 5 releases", "effort": "M", "impact": "M"},
    {"title": "Refresh docs/STATE.md frontmatter", "effort": "S", "impact": "L"},
    {"title": "Scaffold .github/dependabot.yml", "effort": "S", "impact": "M"}
  ],
  "acceptance_criteria": [
    "All docs have status: current frontmatter",
    "CHANGELOG.md has entries for v1.2 through v1.6",
    "dependabot opens at least one PR within 24h"
  ],
  "confidence": 78
}
```

## Stage 4 prompt template (verbatim)

The exact text is in `internal/fixit/prompts/advisor.tmpl`. The
operative properties:

1. **System prompt is short and uncompromising.** "You are an analytical
   subprocess. Output is consumed by code that requires JSON matching
   the schema below. Do not include explanation, markdown, or any
   text outside the JSON object."
2. **Schema is in the prompt as TypeScript-flavored type declarations.**
   Claude follows TypeScript constraints reliably.
3. **RepoContext is rendered as compact YAML** (token-efficient and
   familiar to LLMs).
4. **VariantScores are rendered as a table** so Claude can see
   ranking without re-deriving it.
5. **IntentSpec.Constraints become hard constraints in the prompt:**
   "The operator forbids: opus, force-push." Claude must respect
   them or the validator rejects.
6. **No examples.** Examples bias toward repeating example patterns;
   the schema is enough.

## Validation gate details (Stage 5)

Each validation rule is a pure function over (PlanProposal, RepoContext, VariantRegistry).
Failures are collected — partial passes still emit a plan, but with
`status: provisional` so other variants' plans-first gate refuses to
run on it. The operator must explicitly accept a provisional plan by
editing `status: current`.

Rules (initial set; extend in scorer.go siblings):

- `RuleVariantExists` — `primary` and `alternate` are registered.
- `RuleVariantSafe` — `primary`'s SafetyFloors don't conflict with
  RepoContext.OnDefaultBranch.
- `RuleAcceptanceConcrete` — every acceptance criterion contains a
  measurable verb ("passes", "exists", "≥", "≤", "matches"). Bans
  "improves", "considers", "addresses".
- `RuleTaskFilesExist` — for every task that mentions a file path
  (regex `\.[a-z]+\b`), the file exists in the repo.
- `RuleConfidenceFloor` — `confidence ≥ MIN_CONFIDENCE` (default 50).

## Fallback plan (Stage 6 escape hatch)

When Stage 4 fails twice OR Stage 5 produces a hard rejection,
Fixit emits a plan whose `status` is `fallback` and whose body
explains:

- What stage failed
- The raw Claude output (when Stage 4 failure)
- The validation errors (when Stage 5 failure)
- A safe default recommendation (`grey` for unknown repos, `blue`
  for repos with config but unclear scope)

Fallback plans never satisfy the plans-first gate. Operators see them
as a diagnostic — if they get one, they know Fixit could not safely
produce advice and they should inspect the report manually.

## Self-validation

The first real validation of this pipeline is having Fixit answer:
"How should radioactive-ralph finish M3?" The output plan should:

- Recognize that the supervisor's session pool is the unblocking
  dependency.
- Recommend `professor-ralph` (plan→execute→reflect) as primary,
  not `grey` (mechanical) and not `green` (free-for-all).
- List concrete acceptance criteria (e.g. "tests/integration/ session-
  pool-spawn test passes", "radioactive_ralph run --variant green
  actually backgrounds
  via tmux", "advisor stage 4 calls real claude subprocess").
- Reference real files (cmd/radioactive_ralph/run.go, internal/supervisor/).

If the pipeline produces that plan, M3 unblocks itself: every
remaining task gets executed by a Ralph variant against this repo.

## Implementation files

- `internal/fixit/intent.go` — Stage 1 IntentSpec + interactive prompts
- `internal/fixit/explore.go` — Stage 2 RepoContext gatherer
- `internal/fixit/scorer.go` — Stage 3 deterministic ranker
- `internal/fixit/analyze.go` — Stage 4 Claude subprocess + JSON parsing
- `internal/fixit/validate.go` — Stage 5 validation rules
- `internal/fixit/emit.go` — Stage 6 plan-file writer
- `internal/fixit/prompts/advisor.tmpl` — Stage 4 prompt template
- `internal/fixit/prompts/schema.ts` — output schema (referenced by template)
- `cmd/radioactive_ralph/advisor.go` — wiring, replaces the current state-driven stub
