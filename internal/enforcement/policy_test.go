package enforcement

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestLifecycleRequiresExplicitVerification(t *testing.T) {
	policy := testPolicy()
	snapshot := activeSnapshot()

	requested := Evaluate(Request{
		Policy: policy, Snapshot: snapshot, Event: Event{Action: ActionRequestVerify},
	})
	assertDecision(t, requested, StateVerify, ReasonVerificationRequested, 0, 0)

	completed := Evaluate(Request{
		Policy: policy,
		Snapshot: Snapshot{
			State: StateVerify,
		},
		Event: Event{Action: ActionVerify, Results: passingResults()},
	})
	assertDecision(t, completed, StateComplete, ReasonAcceptancePassed, 0, 0)

	direct := Evaluate(Request{
		Policy: policy, Snapshot: snapshot,
		Event: Event{Action: ActionVerify, Results: passingResults()},
	})
	assertDecision(t, direct, StateBlocked, ReasonInvalidTransition, 0, 0)
}

func TestAcceptancePredicatesAreAllRequired(t *testing.T) {
	tests := []struct {
		name    string
		results map[string]PredicateResult
		state   State
		reason  ReasonCode
	}{
		{
			name:    "all observed and satisfied",
			results: passingResults(),
			state:   StateComplete,
			reason:  ReasonAcceptancePassed,
		},
		{
			name: "missing result",
			results: map[string]PredicateResult{
				"tests": {Observed: true, Satisfied: true},
			},
			state:  StateActive,
			reason: ReasonAcceptanceFailed,
		},
		{
			name: "observed failure",
			results: map[string]PredicateResult{
				"tests":  {Observed: true, Satisfied: false},
				"review": {Observed: true, Satisfied: true},
			},
			state:  StateActive,
			reason: ReasonAcceptanceFailed,
		},
		{
			name: "unobserved",
			results: map[string]PredicateResult{
				"tests":  {Observed: true, Satisfied: true},
				"review": {},
			},
			state:  StateActive,
			reason: ReasonAcceptanceFailed,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			decision := Evaluate(Request{
				Policy:   testPolicy(),
				Snapshot: Snapshot{State: StateVerify},
				Event:    Event{Action: ActionVerify, Results: tt.results},
			})
			assertDecision(t, decision, tt.state, tt.reason, boolToInt(tt.state != StateComplete), 0)
		})
	}
}

func TestRetryAndNoProgressBudgets(t *testing.T) {
	policy := testPolicy()

	retry := activeSnapshot()
	for attempt := 1; attempt <= policy.RetryBudget; attempt++ {
		decision := Evaluate(Request{
			Policy: policy, Snapshot: retry, Event: Event{Action: ActionRetryableFailure},
		})
		assertDecision(t, decision, StateActive, ReasonRetryScheduled, attempt, 0)
		retry = snapshotFromDecision(decision)
	}
	exhaustedRetry := Evaluate(Request{
		Policy: policy, Snapshot: retry, Event: Event{Action: ActionRetryableFailure},
	})
	assertDecision(t, exhaustedRetry, StateBlocked, ReasonRetryExhausted, policy.RetryBudget+1, 0)

	noProgress := activeSnapshot()
	for count := 1; count <= policy.NoProgressBudget; count++ {
		decision := Evaluate(Request{
			Policy: policy, Snapshot: noProgress, Event: Event{Action: ActionNoProgress},
		})
		assertDecision(t, decision, StateActive, ReasonNoProgressRecorded, 0, count)
		noProgress = snapshotFromDecision(decision)
	}
	exhaustedProgress := Evaluate(Request{
		Policy: policy, Snapshot: noProgress, Event: Event{Action: ActionNoProgress},
	})
	assertDecision(t, exhaustedProgress, StateBlocked, ReasonNoProgressExhausted, 0, policy.NoProgressBudget+1)

	reset := Evaluate(Request{
		Policy:   policy,
		Snapshot: Snapshot{State: StateActive, NoProgressCount: policy.NoProgressBudget},
		Event:    Event{Action: ActionProgress},
	})
	assertDecision(t, reset, StateActive, ReasonProgressRecorded, 0, 0)
}

func TestWaitExternalHasOwnerRecheckAndDeadline(t *testing.T) {
	policy := testPolicy()
	wait := &WaitWindow{
		Owner:     "github",
		RecheckAt: "2026-08-16T18:05:00Z",
		Deadline:  "2026-08-16T19:00:00Z",
	}

	waiting := Evaluate(Request{
		Policy:   policy,
		Snapshot: activeSnapshot(),
		Event:    Event{Action: ActionWaitExternal, Now: "2026-08-16T18:00:00Z", Wait: wait},
	})
	assertDecision(t, waiting, StateWaitExternal, ReasonWaitingExternal, 0, 0)
	if waiting.Wait == wait {
		t.Fatal("decision aliases caller wait metadata")
	}

	notDue := Evaluate(Request{
		Policy:   policy,
		Snapshot: snapshotFromDecision(waiting),
		Event:    Event{Action: ActionRecheck, Now: "2026-08-16T18:04:59Z"},
	})
	assertDecision(t, notDue, StateWaitExternal, ReasonWaitNotDue, 0, 0)

	due := Evaluate(Request{
		Policy:   policy,
		Snapshot: snapshotFromDecision(waiting),
		Event:    Event{Action: ActionRecheck, Now: "2026-08-16T18:05:00Z"},
	})
	assertDecision(t, due, StateActive, ReasonWaitRecheckDue, 0, 0)
	if due.Wait != nil {
		t.Fatalf("wait metadata retained after recheck: %+v", due.Wait)
	}

	deadline := Evaluate(Request{
		Policy:   policy,
		Snapshot: snapshotFromDecision(waiting),
		Event:    Event{Action: ActionRecheck, Now: "2026-08-16T19:00:00Z"},
	})
	assertDecision(t, deadline, StateBlocked, ReasonWaitDeadlineExceeded, 0, 0)
}

func TestTerminalStatesAreAbsorbing(t *testing.T) {
	for _, state := range []State{StateComplete, StateBlocked} {
		decision := Evaluate(Request{
			Policy:   testPolicy(),
			Snapshot: Snapshot{State: state, RetryCount: 1, NoProgressCount: 2},
			Event:    Event{Action: ActionProgress},
		})
		assertDecision(t, decision, state, ReasonTerminalState, 1, 2)
	}
}

func TestMalformedInputFailsClosedWithoutEcho(t *testing.T) {
	const secretCanary = "ghp_super-secret-canary"
	tests := []struct {
		name string
		data string
	}{
		{name: "invalid JSON", data: `{` + secretCanary},
		{name: "unknown field", data: readFixture(t, "canary-unknown-field-v1.json")},
		{name: "unsupported version", data: readFixture(t, "canary-policy-v2.json")},
		{name: "trailing JSON", data: validRequestJSON(t) + ` {"provider_token":"` + secretCanary + `"}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			decision := EvaluateJSON([]byte(tt.data))
			assertDecision(t, decision, StateBlocked, ReasonMalformedInput, 0, 0)
			encoded, err := json.Marshal(decision)
			if err != nil {
				t.Fatalf("marshal decision: %v", err)
			}
			if strings.Contains(string(encoded), secretCanary) {
				t.Fatalf("decision echoed input canary: %s", encoded)
			}
		})
	}
}

func TestSemanticValidationFailsClosed(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Request)
	}{
		{name: "empty acceptance", mutate: func(r *Request) { r.Policy.Acceptance = nil }},
		{name: "too many predicates", mutate: func(r *Request) {
			r.Policy.Acceptance = make([]Predicate, maxPredicates+1)
			for i := range r.Policy.Acceptance {
				r.Policy.Acceptance[i] = Predicate{ID: "predicate-" + strings.Repeat("a", i%10) + string(rune('a'+i%26)), Kind: PredicateFileExists}
			}
		}},
		{name: "duplicate predicate", mutate: func(r *Request) { r.Policy.Acceptance[1].ID = "tests" }},
		{name: "unknown predicate kind", mutate: func(r *Request) { r.Policy.Acceptance[0].Kind = "shell" }},
		{name: "unknown state", mutate: func(r *Request) { r.Snapshot.State = "DONE" }},
		{name: "unknown action", mutate: func(r *Request) { r.Event.Action = "STOP" }},
		{name: "wait lacks metadata", mutate: func(r *Request) { r.Event.Action = ActionWaitExternal }},
		{name: "wait owner is not finite", mutate: func(r *Request) {
			r.Event = Event{Action: ActionWaitExternal, Now: "2026-08-16T18:00:00Z", Wait: &WaitWindow{
				Owner: "ghp_super-secret-canary", RecheckAt: "2026-08-16T18:05:00Z", Deadline: "2026-08-16T19:00:00Z",
			}}
		}},
		{name: "wait now is malformed", mutate: func(r *Request) {
			r.Event = Event{Action: ActionWaitExternal, Now: "not-a-time", Wait: &WaitWindow{
				Owner: WaitOwnerGitHub, RecheckAt: "2026-08-16T18:05:00Z", Deadline: "2026-08-16T19:00:00Z",
			}}
		}},
		{name: "wait recheck is malformed", mutate: func(r *Request) {
			r.Event = Event{Action: ActionWaitExternal, Now: "2026-08-16T18:00:00Z", Wait: &WaitWindow{
				Owner: WaitOwnerGitHub, RecheckAt: "not-a-time", Deadline: "2026-08-16T19:00:00Z",
			}}
		}},
		{name: "wait deadline already passed", mutate: func(r *Request) {
			r.Event = Event{Action: ActionWaitExternal, Now: "2026-08-16T20:00:00Z", Wait: &WaitWindow{
				Owner: "github", RecheckAt: "2026-08-16T18:05:00Z", Deadline: "2026-08-16T19:00:00Z",
			}}
		}},
		{name: "wait recheck not in future", mutate: func(r *Request) {
			r.Event = Event{Action: ActionWaitExternal, Now: "2026-08-16T18:05:00Z", Wait: &WaitWindow{
				Owner: "github", RecheckAt: "2026-08-16T18:05:00Z", Deadline: "2026-08-16T19:00:00Z",
			}}
		}},
		{name: "wait has no usable recheck window", mutate: func(r *Request) {
			r.Event = Event{Action: ActionWaitExternal, Now: "2026-08-16T18:00:00Z", Wait: &WaitWindow{
				Owner: "github", RecheckAt: "2026-08-16T19:00:00Z", Deadline: "2026-08-16T19:00:00Z",
			}}
		}},
		{name: "active retry counter already exhausted", mutate: func(r *Request) {
			r.Snapshot.RetryCount = r.Policy.RetryBudget + 1
		}},
		{name: "satisfied but unobserved", mutate: func(r *Request) {
			r.Snapshot.State = StateVerify
			r.Event = Event{Action: ActionVerify, Results: map[string]PredicateResult{
				"tests": {Satisfied: true},
			}}
		}},
		{name: "unknown result id", mutate: func(r *Request) {
			r.Snapshot.State = StateVerify
			r.Event = Event{Action: ActionVerify, Results: map[string]PredicateResult{
				"different-policy": {Observed: true, Satisfied: true},
			}}
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			request := Request{Policy: testPolicy(), Snapshot: activeSnapshot(), Event: Event{Action: ActionProgress}}
			tt.mutate(&request)
			assertDecision(t, Evaluate(request), StateBlocked, ReasonMalformedInput, 0, 0)
		})
	}
}

func TestEmbeddedPolicySchemaIsVersionedAndCopied(t *testing.T) {
	first := PolicySchema()
	second := PolicySchema()
	if len(first) == 0 || !json.Valid(first) {
		t.Fatal("embedded policy schema is empty or invalid JSON")
	}
	if !strings.Contains(string(first), `"schema_version"`) || !strings.Contains(string(first), `"const": 1`) {
		t.Fatal("embedded schema does not pin schema version 1")
	}
	first[0] = 'x'
	if !json.Valid(second) {
		t.Fatal("PolicySchema returned aliased mutable storage")
	}
}

func TestInvalidStateActionPairsFailClosed(t *testing.T) {
	tests := []struct {
		state State
		event Event
	}{
		{state: StateActive, event: Event{Action: ActionRecheck, Now: "2026-08-16T18:05:00Z"}},
		{state: StateActive, event: Event{Action: ActionVerify, Results: passingResults()}},
		{state: StateVerify, event: Event{Action: ActionProgress}},
		{state: StateWaitExternal, event: Event{Action: ActionProgress}},
	}

	for _, tt := range tests {
		snapshot := Snapshot{State: tt.state}
		if tt.state == StateWaitExternal {
			snapshot.Wait = &WaitWindow{Owner: "github", RecheckAt: "2026-08-16T18:05:00Z", Deadline: "2026-08-16T19:00:00Z"}
		}
		decision := Evaluate(Request{Policy: testPolicy(), Snapshot: snapshot, Event: tt.event})
		assertDecision(t, decision, StateBlocked, ReasonInvalidTransition, 0, 0)
	}
}

func TestAdapterParityFixture(t *testing.T) {
	var fixture struct {
		SchemaVersion int      `json:"schema_version"`
		Adapters      []string `json:"adapters"`
		Cases         []struct {
			Name     string   `json:"name"`
			Request  Request  `json:"request"`
			Expected Decision `json:"expected"`
		} `json:"cases"`
	}
	decodeStrictFixture(t, "parity-v1.json", &fixture)
	if fixture.SchemaVersion != SchemaVersion {
		t.Fatalf("fixture schema = %d, want %d", fixture.SchemaVersion, SchemaVersion)
	}
	if want := []string{"claude", "codex", "opencode"}; !reflect.DeepEqual(fixture.Adapters, want) {
		t.Fatalf("adapter canaries = %v, want %v", fixture.Adapters, want)
	}
	if len(fixture.Cases) == 0 {
		t.Fatal("parity fixture has no cases")
	}
	for _, adapter := range fixture.Adapters {
		for _, testCase := range fixture.Cases {
			t.Run(adapter+"/"+testCase.Name, func(t *testing.T) {
				actual := Evaluate(testCase.Request)
				if !reflect.DeepEqual(actual, testCase.Expected) {
					t.Fatalf("decision = %+v, want %+v", actual, testCase.Expected)
				}
			})
		}
	}
}

func assertDecision(t *testing.T, decision Decision, state State, reason ReasonCode, retryCount, noProgressCount int) {
	t.Helper()
	if decision.SchemaVersion != SchemaVersion || decision.State != state || decision.Reason != reason ||
		decision.RetryCount != retryCount || decision.NoProgressCount != noProgressCount {
		t.Fatalf("decision = %+v; want version=%d state=%s reason=%s retries=%d no-progress=%d",
			decision, SchemaVersion, state, reason, retryCount, noProgressCount)
	}
}

func testPolicy() Policy {
	return Policy{
		SchemaVersion:    SchemaVersion,
		PolicyID:         "adapter-parity",
		RetryBudget:      2,
		NoProgressBudget: 2,
		Acceptance: []Predicate{
			{ID: "tests", Kind: PredicateCommandExitZero},
			{ID: "review", Kind: PredicateReviewApproved},
		},
	}
}

func activeSnapshot() Snapshot {
	return Snapshot{State: StateActive}
}

func passingResults() map[string]PredicateResult {
	return map[string]PredicateResult{
		"tests":  {Observed: true, Satisfied: true},
		"review": {Observed: true, Satisfied: true},
	}
}

func snapshotFromDecision(decision Decision) Snapshot {
	return Snapshot{
		State:           decision.State,
		RetryCount:      decision.RetryCount,
		NoProgressCount: decision.NoProgressCount,
		Wait:            normalizedWait(decision.Wait),
	}
}

func boolToInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func readFixture(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return string(data)
}

func validRequestJSON(t *testing.T) string {
	t.Helper()
	data, err := json.Marshal(Request{
		Policy: testPolicy(), Snapshot: activeSnapshot(), Event: Event{Action: ActionProgress},
	})
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	return string(data)
}

func decodeStrictFixture(t *testing.T, name string, target any) {
	t.Helper()
	decoder := json.NewDecoder(strings.NewReader(readFixture(t, name)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		t.Fatalf("decode fixture %s: %v", name, err)
	}
}
