package session

import (
	"fmt"
	"sort"
	"strings"

	"github.com/jbcom/radioactive-ralph/internal/inventory"
	"github.com/jbcom/radioactive-ralph/internal/variant"
)

// BiasChoice is the operator's resolved preference for a single bias
// category. Typically sourced from config.toml's [capabilities]
// section plus per-variant overrides.
//
// Skill is the full skill name (e.g. "coderabbit:review"). Empty means
// "no preference, use variant default if inventory has it".
//
// Disabled means "skip this bias entirely regardless of inventory".
type BiasChoice struct {
	Skill    string
	Disabled bool
}

// PromptOptions feeds RenderSystemPrompt.
type PromptOptions struct {
	// Variant is the active profile. Required.
	Variant variant.Profile

	// Inventory is the capability inventory. Required; if empty, bias
	// injection silently skips slots.
	Inventory inventory.Inventory

	// OperatorChoices is the per-category operator preference map.
	// Keys are BiasCategory values; missing keys fall back to the
	// variant's declared snippet target.
	OperatorChoices map[variant.BiasCategory]BiasChoice
}

// RenderSystemPrompt combines the variant's bias snippets with
// operator preferences and installed inventory to produce the
// --append-system-prompt text.
//
// Rules:
//
//  1. For each bias category the variant declares a snippet for:
//     a. If OperatorChoices has Disabled=true, skip.
//     b. Else if OperatorChoices has Skill!="" AND inventory has it,
//     render the snippet with {skill} expanded.
//     c. Else if variant snippet contains {skill} and inventory has
//     ANY skill matching the category name, pick the first
//     alphabetically for determinism.
//     d. Else skip.
//
//  2. Output is newline-joined with a fixed preamble announcing the
//     variant and a safety note about the tool allowlist.
//
//  3. Output is deterministic given the same inputs — categories are
//     iterated in alphabetical order.
func RenderSystemPrompt(opts PromptOptions) string {
	var b strings.Builder
	fmt.Fprintf(&b, "You are running under radioactive-ralph as variant %q.\n",
		opts.Variant.Name)
	fmt.Fprintf(&b, "Description: %s\n\n", opts.Variant.Description)

	categories := sortedBiasCategories(opts.Variant.SkillBiases)
	for _, cat := range categories {
		snippet := opts.Variant.SkillBiases[cat]
		choice := opts.OperatorChoices[cat]

		if choice.Disabled {
			continue
		}

		skill := choice.Skill
		if skill == "" {
			skill = inferSkill(cat, opts.Inventory)
		}
		if skill == "" {
			continue
		}
		if !opts.Inventory.HasSkill(skill) {
			continue
		}
		rendered := strings.ReplaceAll(string(snippet), "{skill}", skill)
		b.WriteString(rendered)
		b.WriteString("\n")
	}

	return strings.TrimRight(b.String(), "\n")
}

// sortedBiasCategories returns the map keys in alphabetical order so
// renderings are stable across runs.
func sortedBiasCategories(m map[variant.BiasCategory]variant.BiasSnippet) []variant.BiasCategory {
	out := make([]variant.BiasCategory, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// inferSkill tries to pick a sensible skill for a bias category when
// the operator hasn't declared one. Heuristic:
//
//   - Look for a skill whose FullName contains the category name
//     (e.g. BiasReview → any skill with "review" in the name).
//   - Returns the alphabetically-first match for determinism.
//   - Returns "" if nothing qualifies — the bias silently drops.
func inferSkill(cat variant.BiasCategory, inv inventory.Inventory) string {
	needle := strings.ReplaceAll(string(cat), "_", "")
	var candidates []string
	for _, s := range inv.Skills {
		full := s.FullName()
		if strings.Contains(strings.ReplaceAll(strings.ToLower(full), "_", ""),
			strings.ToLower(needle)) {
			candidates = append(candidates, full)
		}
	}
	sort.Strings(candidates)
	if len(candidates) == 0 {
		return ""
	}
	return candidates[0]
}
