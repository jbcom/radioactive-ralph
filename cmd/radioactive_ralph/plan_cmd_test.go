package main

import "testing"

func TestDerivePlanTitleUsesFirstHeading(t *testing.T) {
	got := derivePlanTitle("# Rebuild the runtime\n\nsome body\n", "/plans/x.md")
	if got != "Rebuild the runtime" {
		t.Errorf("title = %q, want %q", got, "Rebuild the runtime")
	}
}

func TestDerivePlanTitleFallsBackToFilename(t *testing.T) {
	got := derivePlanTitle("no heading here\n", "/plans/my-plan.md")
	if got != "my-plan" {
		t.Errorf("title = %q, want %q (filename sans extension)", got, "my-plan")
	}
}

func TestSlugify(t *testing.T) {
	cases := map[string]string{
		"Rebuild the Runtime":  "rebuild-the-runtime",
		"  Trailing/Leading  ": "trailing-leading",
		"UPPER_and_snake":      "upper-and-snake",
		"a---b":                "a-b",
		"!!!":                  "plan",
		"Ship v2.0 (final)":    "ship-v2-0-final",
	}
	for in, want := range cases {
		if got := slugify(in); got != want {
			t.Errorf("slugify(%q) = %q, want %q", in, got, want)
		}
	}
}
