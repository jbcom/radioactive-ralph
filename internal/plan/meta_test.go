package plan

import "testing"

func TestTitleUsesFirstHeading(t *testing.T) {
	if got := Title("# Rebuild the runtime\n\nbody\n", "fallback"); got != "Rebuild the runtime" {
		t.Errorf("Title = %q, want %q", got, "Rebuild the runtime")
	}
}

func TestTitleFallsBackWhenNoHeading(t *testing.T) {
	if got := Title("no heading here\n", "fallback"); got != "fallback" {
		t.Errorf("Title = %q, want the fallback", got)
	}
}

func TestSlug(t *testing.T) {
	cases := map[string]string{
		"Rebuild the Runtime":  "rebuild-the-runtime",
		"  Trailing/Leading  ": "trailing-leading",
		"UPPER_and_snake":      "upper-and-snake",
		"a---b":                "a-b",
		"!!!":                  "plan",
		"Ship v2.0 (final)":    "ship-v2-0-final",
	}
	for in, want := range cases {
		if got := Slug(in); got != want {
			t.Errorf("Slug(%q) = %q, want %q", in, got, want)
		}
	}
}
