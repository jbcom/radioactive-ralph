package fixit

import (
	"fmt"
	"sort"
	"strings"
)

// Score runs Stage 3 — deterministic variant ranking. Every variant
// gets a 0..100 score with bullet-justified Reasons. Disqualifying
// notes set the score to 0 (e.g. world-breaker without
// --confirm-burn-everything from the CLI, since gated variants
// can't be auto-handed-off).
//
// Same input always produces same output. Rules live here so they
// can be unit-tested independently of any provider call.
func Score(rc RepoContext, intent IntentSpec) []VariantScore {
	scores := map[string]*VariantScore{
		"blue":          {Variant: "blue", Score: 30},
		"grey":          {Variant: "grey", Score: 20},
		"green":         {Variant: "green", Score: 50}, // sensible default
		"red":           {Variant: "red", Score: 20},
		"professor":     {Variant: "professor", Score: 40},
		"fixit":         {Variant: "fixit", Score: 30},
		"immortal":      {Variant: "immortal", Score: 30},
		"savage":        {Variant: "savage", Score: 10},
		"old-man":       {Variant: "old-man", Score: 5},
		"world-breaker": {Variant: "world-breaker", Score: 5},
	}

	// Collect off-limits names up-front; apply at the end so late
	// signals can't bump a banned variant back above 0.
	bannedByOperator := map[string]bool{}
	for _, c := range intent.Constraints {
		if name, ok := strings.CutPrefix(c, "variant off-limits: "); ok {
			bannedByOperator[strings.TrimSpace(name)] = true
		}
	}

	// Signal: governance files missing → grey wins big, professor
	// also benefits (it'll plan around the cleanup).
	if n := len(rc.GovernanceMissing); n > 0 {
		scores["grey"].Score += 70
		scores["grey"].Reasons = append(scores["grey"].Reasons,
			fmt.Sprintf("%d canonical governance file(s) missing — grey's mechanical sweep is purpose-built for this", n))
		scores["professor"].Score += 10
		scores["professor"].Reasons = append(scores["professor"].Reasons,
			"governance gaps could be sequenced ahead of feature work")
	}

	// Signal: stale docs → grey + professor.
	if n := len(rc.DocsStale); n > 0 {
		scores["grey"].Score += 15
		scores["grey"].Reasons = append(scores["grey"].Reasons,
			fmt.Sprintf("%d doc(s) flagged stale", n))
	}

	// Signal: failing CI on PRs → red wins.
	failingPRs := 0
	for _, pr := range rc.OpenPRs {
		if pr.MergeStatus == "DIRTY" || pr.MergeStatus == "BLOCKED" || pr.MergeStatus == "BEHIND" {
			failingPRs++
		}
	}
	if failingPRs > 0 {
		scores["red"].Score += 50
		scores["red"].Reasons = append(scores["red"].Reasons,
			fmt.Sprintf("%d PR(s) blocked or behind — red's triage is the right tool", failingPRs))
	}

	// Signal: many open AI-welcome issues → green / fixit.
	if n := len(rc.AIWelcomeIssues); n >= 3 {
		scores["green"].Score += 20
		scores["green"].Reasons = append(scores["green"].Reasons,
			fmt.Sprintf("%d ai-welcome issues — green's full-loop is built for backlogs", n))
		scores["fixit"].Score += 15
		scores["fixit"].Reasons = append(scores["fixit"].Reasons,
			"could ROI-rank the ai-welcome backlog and ship the highest-impact small ones")
	}

	// Signal: plans/index.md exists already with multiple referenced
	// files → professor (planned execution makes sense). Without
	// plans → fixit (advisor itself).
	if rc.PlansIndexExists && len(rc.PlansFiles) >= 2 {
		scores["professor"].Score += 25
		scores["professor"].Reasons = append(scores["professor"].Reasons,
			"plans/index.md already references multiple plans — professor's plan→execute→reflect cycle fits")
	}
	if !rc.PlansIndexExists {
		scores["fixit"].Score += 30
		scores["fixit"].Reasons = append(scores["fixit"].Reasons,
			"no plans yet — fixit advisor is the only variant that can scaffold them")
	}

	// Signal: on default branch + no destructive intent → safer
	// variants only. old-man hard-disqualifies.
	if rc.OnDefaultBranch {
		scores["old-man"].Score = 0
		scores["old-man"].Disqualifying = append(scores["old-man"].Disqualifying,
			"old-man refuses to operate on default branch (main/master/etc)")
		scores["world-breaker"].Score = max(0, scores["world-breaker"].Score-30)
		scores["world-breaker"].Reasons = append(scores["world-breaker"].Reasons,
			"on default branch — world-breaker downweighted (less safe context)")
	}

	// Signal: gated variants (savage, world-breaker) need explicit
	// confirmation. Without it from the IntentSpec, they're disqualified.
	confirmed := map[string]bool{}
	for _, c := range intent.Constraints {
		switch {
		case strings.Contains(c, "confirm-burn-budget"):
			confirmed["savage"] = true
		case strings.Contains(c, "confirm-no-mercy"):
			confirmed["old-man"] = true
		case strings.Contains(c, "confirm-burn-everything"):
			confirmed["world-breaker"] = true
		}
	}
	for _, gated := range []string{"savage", "old-man", "world-breaker"} {
		if !confirmed[gated] {
			scores[gated].Score = 0
			scores[gated].Disqualifying = append(scores[gated].Disqualifying,
				"gated variant; operator did not declare the confirmation flag")
		}
	}

	// Apply operator off-limits LAST so no signal can resurrect a banned
	// variant. This is deliberate — operator intent is absolute.
	for name := range bannedByOperator {
		if s, ok := scores[name]; ok {
			s.Score = 0
			s.Disqualifying = append(s.Disqualifying,
				"operator declared this variant off-limits")
		}
	}

	// Cap scores at 100.
	for _, s := range scores {
		if s.Score > 100 {
			s.Score = 100
		}
	}

	// Sort by score descending, ties broken alphabetically.
	out := make([]VariantScore, 0, len(scores))
	for _, s := range scores {
		out = append(out, *s)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Score != out[j].Score {
			return out[i].Score > out[j].Score
		}
		return out[i].Variant < out[j].Variant
	})
	return out
}
