package runtime

import "testing"

func TestParseWorkerResultExpandedOutcomes(t *testing.T) {
	cases := []struct {
		name string
		raw  string
	}{
		{
			name: "blocked",
			raw:  `{"outcome":"blocked","summary":"wait","evidence":[],"reason":"missing dependency","retryable":true}`,
		},
		{
			name: "need_context",
			raw:  `{"outcome":"need_context","summary":"need docs","evidence":[],"needs_context":["api docs"]}`,
		},
		{
			name: "handoff",
			raw:  `{"outcome":"handoff","summary":"give this to professor","evidence":[],"handoff_to":"professor"}`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			parsed, err := parseWorkerResult(tc.raw)
			if err != nil {
				t.Fatalf("parseWorkerResult: %v", err)
			}
			if parsed.Outcome == "" {
				t.Fatal("expected outcome")
			}
		})
	}
}

func TestParseWorkerResultRejectsMissingFields(t *testing.T) {
	cases := []string{
		`{"outcome":"handoff","summary":"bad","evidence":[]}`,
		`{"outcome":"need_context","summary":"bad","evidence":[]}`,
	}
	for _, raw := range cases {
		if _, err := parseWorkerResult(raw); err == nil {
			t.Fatalf("expected parse failure for %s", raw)
		}
	}
}
