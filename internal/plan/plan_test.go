package plan

import (
	"testing"
)

func TestParseTwoTopLevelGroupsSequential(t *testing.T) {
	md := []byte(`# Do first

- alpha
- beta

# Do next

- gamma
`)
	p, err := Parse(md)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(p.Groups) != 2 {
		t.Fatalf("want 2 top-level groups, got %d", len(p.Groups))
	}
	if p.Groups[0].Heading != "Do first" {
		t.Errorf("Groups[0].Heading = %q, want %q", p.Groups[0].Heading, "Do first")
	}
	if p.Groups[1].Heading != "Do next" {
		t.Errorf("Groups[1].Heading = %q, want %q", p.Groups[1].Heading, "Do next")
	}
	if !p.Groups[0].Parallel {
		t.Errorf("Groups[0].Parallel = false, want true (unordered list)")
	}
	if len(p.Groups[0].Steps) != 2 {
		t.Fatalf("Groups[0].Steps len = %d, want 2", len(p.Groups[0].Steps))
	}
	if len(p.Groups[1].Steps) != 1 {
		t.Fatalf("Groups[1].Steps len = %d, want 1", len(p.Groups[1].Steps))
	}
}

func TestParseSubheadingsCarryOrderingDontDescend(t *testing.T) {
	md := []byte(`# Deploy

## Build image

1. compile
2. package

## Push image

- tag
- push
`)
	p, err := Parse(md)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(p.Groups) != 1 {
		t.Fatalf("want 1 top-level group, got %d", len(p.Groups))
	}
	top := p.Groups[0]
	if top.Heading != "Deploy" {
		t.Fatalf("top.Heading = %q", top.Heading)
	}
	// The top-level group has subheadings: it must NOT collect steps
	// itself -- the subheadings carry the ordering.
	if len(top.Steps) != 0 {
		t.Errorf("top.Steps = %v, want empty (has subheadings, must not descend)", top.Steps)
	}
	if len(top.SubGroups) != 2 {
		t.Fatalf("top.SubGroups len = %d, want 2", len(top.SubGroups))
	}

	build := top.SubGroups[0]
	if build.Heading != "Build image" {
		t.Errorf("SubGroups[0].Heading = %q", build.Heading)
	}
	if build.Parallel {
		t.Errorf("build.Parallel = true, want false (ordered list)")
	}
	if len(build.Steps) != 2 {
		t.Fatalf("build.Steps len = %d, want 2", len(build.Steps))
	}
	if build.Steps[0].Text != "compile" || build.Steps[1].Text != "package" {
		t.Errorf("build.Steps = %+v", build.Steps)
	}

	push := top.SubGroups[1]
	if push.Heading != "Push image" {
		t.Errorf("SubGroups[1].Heading = %q", push.Heading)
	}
	if !push.Parallel {
		t.Errorf("push.Parallel = false, want true (unordered list)")
	}
}

func TestParseUnorderedListIsParallel(t *testing.T) {
	md := []byte(`# Group

- one
- two
- three
`)
	p, err := Parse(md)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	g := p.Groups[0]
	if !g.Parallel {
		t.Fatalf("Parallel = false, want true")
	}
	if len(g.Steps) != 3 {
		t.Fatalf("Steps len = %d, want 3", len(g.Steps))
	}
}

func TestParseOrderedListIsSequential(t *testing.T) {
	md := []byte(`# Group

1. first
2. second
`)
	p, err := Parse(md)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	g := p.Groups[0]
	if g.Parallel {
		t.Fatalf("Parallel = true, want false")
	}
	if len(g.Steps) != 2 {
		t.Fatalf("Steps len = %d, want 2", len(g.Steps))
	}
}

func TestParseBulletsPlusParagraphIsOneStepWithDetail(t *testing.T) {
	md := []byte(`# Group

- configure the server

Make sure TLS is enabled and the cert is from the internal CA, not a
public one -- this trips people up constantly.
`)
	p, err := Parse(md)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	g := p.Groups[0]
	if len(g.Steps) != 1 {
		t.Fatalf("Steps len = %d, want 1", len(g.Steps))
	}
	step := g.Steps[0]
	if step.Text != "configure the server" {
		t.Errorf("step.Text = %q", step.Text)
	}
	if step.Detail == "" {
		t.Errorf("step.Detail is empty, want the trailing paragraph")
	}
}

func TestParseLeafHeadingNoList(t *testing.T) {
	md := []byte(`# Notes

Just some narrative about context, no actionable steps here.
`)
	p, err := Parse(md)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	g := p.Groups[0]
	if len(g.Steps) != 0 {
		t.Errorf("Steps = %+v, want empty (bare paragraph is narrative, not a step)", g.Steps)
	}
	if len(g.SubGroups) != 0 {
		t.Errorf("SubGroups = %+v, want empty", g.SubGroups)
	}
}

func TestParseThreeLevelNesting(t *testing.T) {
	md := []byte(`# Release

## Backend

### Database

1. run migrations

### API

1. deploy service

## Frontend

- build assets
- deploy assets
`)
	p, err := Parse(md)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(p.Groups) != 1 {
		t.Fatalf("want 1 top group, got %d", len(p.Groups))
	}
	release := p.Groups[0]
	if len(release.SubGroups) != 2 {
		t.Fatalf("release.SubGroups len = %d, want 2", len(release.SubGroups))
	}
	backend := release.SubGroups[0]
	if backend.Heading != "Backend" {
		t.Fatalf("backend.Heading = %q", backend.Heading)
	}
	if len(backend.SubGroups) != 2 {
		t.Fatalf("backend.SubGroups len = %d, want 2", len(backend.SubGroups))
	}
	if backend.SubGroups[0].Heading != "Database" || backend.SubGroups[1].Heading != "API" {
		t.Errorf("backend.SubGroups headings = %q, %q", backend.SubGroups[0].Heading, backend.SubGroups[1].Heading)
	}
	frontend := release.SubGroups[1]
	if !frontend.Parallel {
		t.Errorf("frontend.Parallel = false, want true")
	}
}

func TestParseEmptyDocument(t *testing.T) {
	p, err := Parse([]byte(""))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(p.Groups) != 0 {
		t.Errorf("Groups = %+v, want empty", p.Groups)
	}
}

func TestParseHeadingLevelSkip(t *testing.T) {
	// A level-3 heading directly under a level-1 heading (skipping level
	// 2) is still a child subheading -- it's the next heading found in
	// the section regardless of the exact level delta.
	md := []byte(`# Top

### Deep child

- step one
`)
	p, err := Parse(md)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	top := p.Groups[0]
	if len(top.SubGroups) != 1 {
		t.Fatalf("top.SubGroups len = %d, want 1", len(top.SubGroups))
	}
	if top.SubGroups[0].Level != 3 {
		t.Errorf("SubGroups[0].Level = %d, want 3", top.SubGroups[0].Level)
	}
}
