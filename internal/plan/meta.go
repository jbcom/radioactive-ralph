package plan

import (
	"regexp"
	"strings"
)

// headingRe matches a markdown level-1 heading line.
var headingRe = regexp.MustCompile(`(?m)^#\s+(.+?)\s*$`)

// Title returns the plan markdown's first level-1 heading, or fallback (e.g. a
// filename sans extension) when there is none or it is blank. Shared by the
// `plan import` CLI and the supervisor's plan-import IPC handler so both derive
// identical titles.
func Title(markdown, fallback string) string {
	if m := headingRe.FindStringSubmatch(markdown); m != nil {
		if t := strings.TrimSpace(m[1]); t != "" {
			return t
		}
	}
	return fallback
}

// Slug lower-cases a title and collapses every run of non-alphanumeric
// characters into a single hyphen, trimming leading/trailing hyphens — a
// stable, filesystem-and-URL-safe plan slug. Returns "plan" for an
// all-punctuation/empty title.
func Slug(title string) string {
	var b strings.Builder
	prevHyphen := false
	for _, r := range strings.ToLower(title) {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'):
			b.WriteRune(r)
			prevHyphen = false
		default:
			if !prevHyphen {
				b.WriteByte('-')
				prevHyphen = true
			}
		}
	}
	slug := strings.Trim(b.String(), "-")
	if slug == "" {
		return "plan"
	}
	return slug
}
