package plan

import (
	"strings"
	"testing"
)

func TestValidateCleanPlanHasNoFindings(t *testing.T) {
	md := []byte(`# Do first

- alpha
- beta

# Do next

## Sub one

1. x

## Sub two

- y
`)
	errs := Validate(md)
	if len(errs) != 0 {
		t.Fatalf("Validate = %+v, want no findings", errs)
	}
}

func TestValidateEmptyGroupFlagged(t *testing.T) {
	md := []byte(`# Notes

Just narrative, no list at all.
`)
	errs := Validate(md)
	if len(errs) != 1 {
		t.Fatalf("Validate = %+v, want exactly 1 finding", errs)
	}
	if !strings.Contains(errs[0].Msg, "no steps") {
		t.Errorf("Msg = %q, want mention of 'no steps'", errs[0].Msg)
	}
	if errs[0].Line != 1 {
		t.Errorf("Line = %d, want 1", errs[0].Line)
	}
}

func TestValidateLeadingParagraphBeforeListFlagged(t *testing.T) {
	md := []byte(`# Group

This looks like it might be step zero but it's actually narrative.

- real step
`)
	errs := Validate(md)
	if len(errs) != 1 {
		t.Fatalf("Validate = %+v, want exactly 1 finding", errs)
	}
	if !strings.Contains(errs[0].Msg, "paragraph before its list") {
		t.Errorf("Msg = %q", errs[0].Msg)
	}
}

func TestValidateTrailingParagraphNotFlagged(t *testing.T) {
	// A paragraph AFTER the list is legitimate step detail, not an
	// ambiguity -- must not be flagged.
	md := []byte(`# Group

- configure

Detail about configuring goes here.
`)
	errs := Validate(md)
	if len(errs) != 0 {
		t.Fatalf("Validate = %+v, want no findings (trailing paragraph is detail)", errs)
	}
}

func TestValidateMixedOrderedUnorderedListsFlagged(t *testing.T) {
	md := []byte(`# Group

- unordered one

1. ordered one
`)
	errs := Validate(md)
	found := false
	for _, e := range errs {
		if strings.Contains(e.Msg, "mixes ordered and unordered") {
			found = true
		}
	}
	if !found {
		t.Fatalf("Validate = %+v, want a mixed-list finding", errs)
	}
}

func TestValidateSubheadingsNotTreatedAsLeaf(t *testing.T) {
	// A heading with child subheadings must not be flagged as an empty
	// leaf group even though it has no list of its own -- the
	// subheadings carry the ordering.
	md := []byte(`# Deploy

## Build

- compile

## Push

- push
`)
	errs := Validate(md)
	if len(errs) != 0 {
		t.Fatalf("Validate = %+v, want no findings", errs)
	}
}

func TestValidateEmptyDocument(t *testing.T) {
	errs := Validate([]byte(""))
	if len(errs) != 0 {
		t.Fatalf("Validate = %+v, want no findings for empty document", errs)
	}
}

func TestPlanErrorString(t *testing.T) {
	e := PlanError{Line: 5, Msg: "example"}
	if got := e.String(); got != "line 5: example" {
		t.Errorf("String() = %q", got)
	}
	e2 := PlanError{Msg: "no line"}
	if got := e2.String(); got != "no line" {
		t.Errorf("String() = %q", got)
	}
}
