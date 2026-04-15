package initcmd

import (
	"fmt"
	"sort"
	"strings"

	"github.com/jbcom/radioactive-ralph/internal/config"
	"github.com/jbcom/radioactive-ralph/internal/inventory"
	"github.com/jbcom/radioactive-ralph/internal/variant"
)

// resolveChoices walks each BiasCategory and picks the operator's
// preferred skill.
func resolveChoices(inv inventory.Inventory, resolver Resolver, prior config.File) (
	map[variant.BiasCategory]string, []string, error,
) {
	categories := []variant.BiasCategory{
		variant.BiasReview,
		variant.BiasSecurityReview,
		variant.BiasDocsQuery,
		variant.BiasBrainstorm,
		variant.BiasDebugging,
	}
	choices := make(map[variant.BiasCategory]string, len(categories))
	disabled := append([]string(nil), prior.Capabilities.DisabledBiases...)

	for _, cat := range categories {
		// Respect prior choice on Refresh.
		if previous := priorChoice(cat, prior); previous != "" {
			choices[cat] = previous
			continue
		}

		candidates := candidatesFor(cat, inv)
		switch len(candidates) {
		case 0:
			// Nothing installed for this category; skip.
			continue
		case 1:
			// Auto-select.
			choices[cat] = candidates[0]
		default:
			if resolver == nil {
				return nil, nil, fmt.Errorf(
					"category %q has %d candidates and no Resolver was provided: %v",
					cat, len(candidates), candidates,
				)
			}
			pick, err := resolver(cat, candidates)
			if err != nil {
				return nil, nil, err
			}
			switch {
			case pick == "":
				// No preference — leave empty, runtime will silently skip.
			case contains(candidates, pick):
				choices[cat] = pick
			default:
				// Pick wasn't in candidates — treat as explicit disable.
				disabled = append(disabled, pick)
			}
		}
	}
	sort.Strings(disabled)
	disabled = dedupe(disabled)
	return choices, disabled, nil
}

// candidatesFor finds skills whose FullName mentions the category
// name. Matches variant/prompt.go's inferSkill heuristic so init and
// runtime agree.
func candidatesFor(cat variant.BiasCategory, inv inventory.Inventory) []string {
	needle := strings.ReplaceAll(strings.ToLower(string(cat)), "_", "")
	var out []string
	for _, s := range inv.Skills {
		full := s.FullName()
		normalized := strings.ReplaceAll(strings.ToLower(full), "_", "")
		if strings.Contains(normalized, needle) {
			out = append(out, full)
		}
	}
	sort.Strings(out)
	return dedupe(out)
}

// priorChoice returns the operator's existing preference for cat from
// a prior config.File, or "" if not set.
func priorChoice(cat variant.BiasCategory, prior config.File) string {
	switch cat {
	case variant.BiasReview:
		return prior.Capabilities.Review
	case variant.BiasSecurityReview:
		return prior.Capabilities.SecurityReview
	case variant.BiasDocsQuery:
		return prior.Capabilities.DocsQuery
	case variant.BiasBrainstorm:
		return prior.Capabilities.Brainstorm
	case variant.BiasDebugging:
		return prior.Capabilities.Debugging
	default:
		return ""
	}
}

// contains reports whether s contains v.
func contains(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

// dedupe returns a sorted deduplicated copy of s.
func dedupe(s []string) []string {
	sort.Strings(s)
	out := s[:0]
	var prev string
	for _, v := range s {
		if v == prev {
			continue
		}
		out = append(out, v)
		prev = v
	}
	return out
}
