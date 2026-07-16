package genesis

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jbcom/radioactive-ralph/internal/plan"
)

const validPlanMD = `# Do first

- alpha
- beta

# Do next

- gamma
`

func TestRefine_ConvergesWithFakeRefiner(t *testing.T) {
	calls := 0
	fake := Refiner(func(_ context.Context, draft string) (string, bool, error) {
		calls++
		if calls == 1 {
			return draft + "\nstill drafting", false, nil
		}
		return validPlanMD, true, nil
	})

	md, err := Refine(context.Background(), "vague idea", fake, RefineOptions{})
	if err != nil {
		t.Fatalf("Refine: %v", err)
	}
	if calls != 2 {
		t.Fatalf("expected 2 refiner calls, got %d", calls)
	}
	if _, err := plan.Parse(md); err != nil {
		t.Fatalf("expected Refine output to parse as a valid plan: %v", err)
	}
}

func TestRefine_RejectsEmptyPlanEvenIfRefinerClaimsDone(t *testing.T) {
	// The refiner claims done=true immediately but the "plan" has no
	// headings/steps at all -- Refine must not treat that as converged.
	fake := Refiner(func(_ context.Context, _ string) (string, bool, error) {
		return "just some narrative text, no plan structure at all", true, nil
	})

	_, err := Refine(context.Background(), "vague idea", fake, RefineOptions{MaxRounds: 2})
	if !errors.Is(err, ErrRefinementDidNotConverge) {
		t.Fatalf("expected ErrRefinementDidNotConverge, got %v", err)
	}
}

func TestRefine_GivesUpAfterMaxRounds(t *testing.T) {
	calls := 0
	fake := Refiner(func(_ context.Context, draft string) (string, bool, error) {
		calls++
		return draft, false, nil // never converges
	})

	_, err := Refine(context.Background(), "vague idea", fake, RefineOptions{MaxRounds: 3})
	if !errors.Is(err, ErrRefinementDidNotConverge) {
		t.Fatalf("expected ErrRefinementDidNotConverge, got %v", err)
	}
	if calls != 3 {
		t.Fatalf("expected exactly MaxRounds=3 calls, got %d", calls)
	}
}

func TestRefine_PropagatesRefinerError(t *testing.T) {
	wantErr := errors.New("boom")
	fake := Refiner(func(_ context.Context, _ string) (string, bool, error) {
		return "", false, wantErr
	})

	_, err := Refine(context.Background(), "vague idea", fake, RefineOptions{})
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected wrapped wantErr, got %v", err)
	}
}

func TestRefine_RequiresRefiner(t *testing.T) {
	_, err := Refine(context.Background(), "x", nil, RefineOptions{})
	if err == nil {
		t.Fatalf("expected error for nil refiner")
	}
}

func TestRefine_RespectsContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	fake := Refiner(func(_ context.Context, draft string) (string, bool, error) {
		t.Fatalf("refiner should not be called with an already-cancelled context")
		return draft, false, nil
	})

	_, err := Refine(ctx, "x", fake, RefineOptions{})
	if err == nil {
		t.Fatalf("expected error from cancelled context")
	}
}

func TestSkip_PassesInputThroughUnchanged(t *testing.T) {
	input := "whatever the operator typed, valid plan or not\n\n\n"
	got := Skip(input)
	want := strings.TrimRight(input, "\n")
	if string(got) != want {
		t.Fatalf("Skip() = %q, want %q", got, want)
	}
}

func TestValidate_RejectsMalformedPlan(t *testing.T) {
	// A section that mixes ordered and unordered lists is exactly the
	// ambiguity plan.Validate is documented to flag.
	md := []byte(`# Group

1. first
- second
`)
	findings := plan.Validate(md)
	if len(findings) == 0 {
		t.Fatalf("expected plan.Validate to flag the mixed list types as a finding")
	}
}
