// Package enforcement defines the portable, deterministic policy contract used
// by tool-specific supervision adapters. It decides state transitions only; it
// never executes acceptance checks or handles provider credentials.
package enforcement

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"errors"
	"io"
	"regexp"
	"time"
)

const (
	// SchemaVersion is the only policy schema version accepted by this package.
	SchemaVersion = 1
	maxBudget     = 1000
	maxPredicates = 64
)

// State is a persisted enforcement lifecycle state.
type State string

// State values are the complete persisted lifecycle vocabulary.
const (
	StateActive       State = "ACTIVE"
	StateWaitExternal State = "WAIT_EXTERNAL"
	StateVerify       State = "VERIFY"
	StateComplete     State = "COMPLETE"
	StateBlocked      State = "BLOCKED"
)

// Action is an observation submitted to the evaluator by an adapter.
type Action string

// Action values are the complete adapter event vocabulary.
const (
	ActionProgress         Action = "PROGRESS"
	ActionNoProgress       Action = "NO_PROGRESS"
	ActionWaitExternal     Action = "WAIT_EXTERNAL"
	ActionRecheck          Action = "RECHECK"
	ActionRequestVerify    Action = "REQUEST_VERIFY"
	ActionVerify           Action = "VERIFY"
	ActionRetryableFailure Action = "RETRYABLE_FAILURE"
	ActionTerminalFailure  Action = "TERMINAL_FAILURE"
)

// PredicateKind says what an adapter must observe. The evaluator consumes only
// the normalized observed/satisfied result and therefore remains deterministic.
type PredicateKind string

// PredicateKind values are the supported normalized acceptance observations.
const (
	PredicateCommandExitZero PredicateKind = "command_exit_zero"
	PredicateFileExists      PredicateKind = "file_exists"
	PredicateChecksGreen     PredicateKind = "checks_green"
	PredicateReviewApproved  PredicateKind = "review_approved"
	PredicateExactArtifact   PredicateKind = "exact_artifact_match"
)

// WaitOwner is the accountable system or actor for an external wait. It is a
// finite code so a decision never reflects an arbitrary owner string.
type WaitOwner string

// WaitOwner values are the supported accountable external owners.
const (
	WaitOwnerOperator         WaitOwner = "operator"
	WaitOwnerGitHub           WaitOwner = "github"
	WaitOwnerGitea            WaitOwner = "gitea"
	WaitOwnerClaude           WaitOwner = "claude"
	WaitOwnerCodex            WaitOwner = "codex"
	WaitOwnerOpenCode         WaitOwner = "opencode"
	WaitOwnerRadioactiveRalph WaitOwner = "radioactive-ralph"
	WaitOwnerCodeRabbit       WaitOwner = "coderabbit"
	WaitOwnerAIGitBot         WaitOwner = "ai-git-bot"
	WaitOwnerExternal         WaitOwner = "external"
)

// ReasonCode is a finite, non-secret explanation for a decision.
type ReasonCode string

// ReasonCode values are the complete non-secret decision explanation vocabulary.
const (
	ReasonMalformedInput        ReasonCode = "MALFORMED_INPUT"
	ReasonInvalidTransition     ReasonCode = "INVALID_TRANSITION"
	ReasonProgressRecorded      ReasonCode = "PROGRESS_RECORDED"
	ReasonNoProgressRecorded    ReasonCode = "NO_PROGRESS_RECORDED"
	ReasonNoProgressExhausted   ReasonCode = "NO_PROGRESS_BUDGET_EXHAUSTED"
	ReasonRetryScheduled        ReasonCode = "RETRY_SCHEDULED"
	ReasonRetryExhausted        ReasonCode = "RETRY_BUDGET_EXHAUSTED"
	ReasonWaitingExternal       ReasonCode = "WAITING_EXTERNAL"
	ReasonWaitNotDue            ReasonCode = "WAIT_NOT_DUE"
	ReasonWaitRecheckDue        ReasonCode = "WAIT_RECHECK_DUE"
	ReasonWaitDeadlineExceeded  ReasonCode = "WAIT_DEADLINE_EXCEEDED"
	ReasonVerificationRequested ReasonCode = "VERIFICATION_REQUESTED"
	ReasonAcceptancePassed      ReasonCode = "ACCEPTANCE_PASSED"
	ReasonAcceptanceFailed      ReasonCode = "ACCEPTANCE_FAILED"
	ReasonTerminalFailure       ReasonCode = "TERMINAL_FAILURE"
	ReasonTerminalState         ReasonCode = "TERMINAL_STATE"
)

// Predicate is one required acceptance observation. Every predicate in a
// policy must be observed and satisfied before the evaluator returns COMPLETE.
type Predicate struct {
	ID   string        `json:"id"`
	Kind PredicateKind `json:"kind"`
}

// Policy is the versioned, adapter-independent enforcement configuration.
type Policy struct {
	SchemaVersion    int         `json:"schema_version"`
	PolicyID         string      `json:"policy_id"`
	RetryBudget      int         `json:"retry_budget"`
	NoProgressBudget int         `json:"no_progress_budget"`
	Acceptance       []Predicate `json:"acceptance"`
}

// WaitWindow records who owns an external wait and exactly when it must be
// reconsidered. Times are RFC 3339 strings so JSON adapters preserve them.
type WaitWindow struct {
	Owner     WaitOwner `json:"owner"`
	RecheckAt string    `json:"recheck_at"`
	Deadline  string    `json:"deadline"`
}

// Snapshot is the complete evaluator state an adapter must persist.
type Snapshot struct {
	State           State       `json:"state"`
	RetryCount      int         `json:"retry_count"`
	NoProgressCount int         `json:"no_progress_count"`
	Wait            *WaitWindow `json:"wait,omitempty"`
}

// PredicateResult is deliberately free of diagnostic strings. Adapters retain
// provider output separately and submit only the normalized result here.
type PredicateResult struct {
	Observed  bool `json:"observed"`
	Satisfied bool `json:"satisfied"`
}

// Event is one normalized observation. Now is required only for wait/recheck
// actions; Results is required only for verification.
type Event struct {
	Action  Action                     `json:"action"`
	Now     string                     `json:"now,omitempty"`
	Wait    *WaitWindow                `json:"wait,omitempty"`
	Results map[string]PredicateResult `json:"results,omitempty"`
}

// Request is the strict JSON boundary shared by tool-specific adapters.
type Request struct {
	Policy   Policy   `json:"policy"`
	Snapshot Snapshot `json:"snapshot"`
	Event    Event    `json:"event"`
}

// Decision is the only evaluator output. Reason is a finite code and never
// contains source JSON, provider output, environment values, or credentials.
type Decision struct {
	SchemaVersion   int         `json:"schema_version"`
	State           State       `json:"state"`
	RetryCount      int         `json:"retry_count"`
	NoProgressCount int         `json:"no_progress_count"`
	Wait            *WaitWindow `json:"wait,omitempty"`
	Reason          ReasonCode  `json:"reason"`
}

var identifierPattern = regexp.MustCompile(`^[a-z][a-z0-9._-]{0,63}$`)

var errMalformed = errors.New("enforcement input is malformed")

//go:embed schema/policy-v1.schema.json
var policySchemaJSON []byte

// PolicySchema returns an independent copy of the canonical JSON Schema for
// the current policy version.
func PolicySchema() []byte {
	return bytes.Clone(policySchemaJSON)
}

// EvaluateJSON strictly decodes and evaluates a request. Malformed JSON,
// unknown fields, trailing values, and invalid semantics all fail closed.
func EvaluateJSON(data []byte) Decision {
	var request Request
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		return blockedMalformed()
	}
	if err := rejectTrailingJSON(decoder); err != nil {
		return blockedMalformed()
	}
	return Evaluate(request)
}

// Evaluate applies exactly one deterministic transition. It does not read the
// clock, filesystem, network, process environment, or provider output.
func Evaluate(request Request) Decision {
	if validateRequest(request) != nil {
		return blockedMalformed()
	}

	current := decisionFromSnapshot(request.Snapshot, ReasonInvalidTransition)
	if request.Snapshot.State == StateComplete || request.Snapshot.State == StateBlocked {
		current.Reason = ReasonTerminalState
		return current
	}
	if request.Event.Action == ActionTerminalFailure {
		return blockedFrom(request.Snapshot, ReasonTerminalFailure)
	}

	switch request.Snapshot.State {
	case StateActive:
		return evaluateActive(request)
	case StateWaitExternal:
		return evaluateWait(request)
	case StateVerify:
		return evaluateVerify(request)
	default:
		return blockedFrom(request.Snapshot, ReasonInvalidTransition)
	}
}

func evaluateActive(request Request) Decision {
	snapshot := request.Snapshot
	switch request.Event.Action {
	case ActionProgress:
		snapshot.NoProgressCount = 0
		return decisionFromSnapshot(snapshot, ReasonProgressRecorded)
	case ActionNoProgress:
		snapshot.NoProgressCount++
		if snapshot.NoProgressCount > request.Policy.NoProgressBudget {
			return blockedFrom(snapshot, ReasonNoProgressExhausted)
		}
		return decisionFromSnapshot(snapshot, ReasonNoProgressRecorded)
	case ActionRetryableFailure:
		return retryOrBlock(request.Policy, snapshot, ReasonRetryScheduled)
	case ActionWaitExternal:
		snapshot.State = StateWaitExternal
		snapshot.Wait = normalizedWait(request.Event.Wait)
		return decisionFromSnapshot(snapshot, ReasonWaitingExternal)
	case ActionRequestVerify:
		snapshot.State = StateVerify
		return decisionFromSnapshot(snapshot, ReasonVerificationRequested)
	default:
		return blockedFrom(snapshot, ReasonInvalidTransition)
	}
}

func evaluateWait(request Request) Decision {
	snapshot := request.Snapshot
	if request.Event.Action != ActionRecheck {
		return blockedFrom(snapshot, ReasonInvalidTransition)
	}
	now, err := time.Parse(time.RFC3339, request.Event.Now)
	if err != nil {
		return blockedMalformed()
	}
	recheckAt, deadline, err := parseWait(snapshot.Wait)
	if err != nil {
		return blockedMalformed()
	}
	if !now.Before(deadline) {
		return blockedFrom(snapshot, ReasonWaitDeadlineExceeded)
	}
	if now.Before(recheckAt) {
		return decisionFromSnapshot(snapshot, ReasonWaitNotDue)
	}
	snapshot.State = StateActive
	snapshot.Wait = nil
	return decisionFromSnapshot(snapshot, ReasonWaitRecheckDue)
}

func evaluateVerify(request Request) Decision {
	snapshot := request.Snapshot
	switch request.Event.Action {
	case ActionVerify:
		if acceptanceSatisfied(request.Policy, request.Event.Results) {
			snapshot.State = StateComplete
			snapshot.Wait = nil
			return decisionFromSnapshot(snapshot, ReasonAcceptancePassed)
		}
		snapshot.State = StateActive
		return retryOrBlock(request.Policy, snapshot, ReasonAcceptanceFailed)
	case ActionWaitExternal:
		snapshot.State = StateWaitExternal
		snapshot.Wait = normalizedWait(request.Event.Wait)
		return decisionFromSnapshot(snapshot, ReasonWaitingExternal)
	case ActionRetryableFailure:
		snapshot.State = StateActive
		return retryOrBlock(request.Policy, snapshot, ReasonRetryScheduled)
	default:
		return blockedFrom(snapshot, ReasonInvalidTransition)
	}
}

func retryOrBlock(policy Policy, snapshot Snapshot, retryReason ReasonCode) Decision {
	snapshot.RetryCount++
	if snapshot.RetryCount > policy.RetryBudget {
		return blockedFrom(snapshot, ReasonRetryExhausted)
	}
	return decisionFromSnapshot(snapshot, retryReason)
}

func acceptanceSatisfied(policy Policy, results map[string]PredicateResult) bool {
	for _, predicate := range policy.Acceptance {
		result, ok := results[predicate.ID]
		if !ok || !result.Observed || !result.Satisfied {
			return false
		}
	}
	return true
}

func validateRequest(request Request) error {
	if request.Policy.SchemaVersion != SchemaVersion ||
		!identifierPattern.MatchString(request.Policy.PolicyID) ||
		request.Policy.RetryBudget < 0 || request.Policy.RetryBudget > maxBudget ||
		request.Policy.NoProgressBudget < 0 || request.Policy.NoProgressBudget > maxBudget ||
		len(request.Policy.Acceptance) == 0 || len(request.Policy.Acceptance) > maxPredicates {
		return errMalformed
	}
	predicateIDs := make(map[string]struct{}, len(request.Policy.Acceptance))
	for _, predicate := range request.Policy.Acceptance {
		if !identifierPattern.MatchString(predicate.ID) || !validPredicateKind(predicate.Kind) {
			return errMalformed
		}
		if _, duplicate := predicateIDs[predicate.ID]; duplicate {
			return errMalformed
		}
		predicateIDs[predicate.ID] = struct{}{}
	}
	if !validState(request.Snapshot.State) || request.Snapshot.RetryCount < 0 ||
		request.Snapshot.NoProgressCount < 0 || request.Snapshot.RetryCount > maxBudget+1 ||
		request.Snapshot.NoProgressCount > maxBudget+1 {
		return errMalformed
	}
	if request.Snapshot.State != StateBlocked &&
		(request.Snapshot.RetryCount > request.Policy.RetryBudget ||
			request.Snapshot.NoProgressCount > request.Policy.NoProgressBudget) {
		return errMalformed
	}
	if request.Snapshot.State == StateWaitExternal {
		if validateWait(request.Snapshot.Wait) != nil {
			return errMalformed
		}
	} else if request.Snapshot.Wait != nil {
		return errMalformed
	}
	if !validAction(request.Event.Action) {
		return errMalformed
	}
	return validateEvent(request.Event, predicateIDs)
}

func validateEvent(event Event, predicateIDs map[string]struct{}) error {
	switch event.Action {
	case ActionWaitExternal:
		if event.Now == "" || validateTimestamp(event.Now) != nil || validateWait(event.Wait) != nil ||
			event.Results != nil {
			return errMalformed
		}
		now, err := time.Parse(time.RFC3339, event.Now)
		if err != nil {
			return errMalformed
		}
		recheckAt, deadline, err := parseWait(event.Wait)
		if err != nil {
			return errMalformed
		}
		if !now.Before(recheckAt) || !now.Before(deadline) {
			return errMalformed
		}
	case ActionRecheck:
		if event.Now == "" || validateTimestamp(event.Now) != nil || event.Wait != nil || event.Results != nil {
			return errMalformed
		}
	case ActionVerify:
		if event.Now != "" || event.Wait != nil || event.Results == nil {
			return errMalformed
		}
		for id, result := range event.Results {
			if _, known := predicateIDs[id]; !known || (result.Satisfied && !result.Observed) {
				return errMalformed
			}
		}
	default:
		if event.Now != "" || event.Wait != nil || event.Results != nil {
			return errMalformed
		}
	}
	return nil
}

func validateWait(wait *WaitWindow) error {
	if wait == nil || !validWaitOwner(wait.Owner) {
		return errMalformed
	}
	recheckAt, deadline, err := parseWait(wait)
	if err != nil || !recheckAt.Before(deadline) {
		return errMalformed
	}
	return nil
}

func parseWait(wait *WaitWindow) (time.Time, time.Time, error) {
	if wait == nil {
		return time.Time{}, time.Time{}, errMalformed
	}
	recheckAt, err := time.Parse(time.RFC3339, wait.RecheckAt)
	if err != nil {
		return time.Time{}, time.Time{}, errMalformed
	}
	deadline, err := time.Parse(time.RFC3339, wait.Deadline)
	if err != nil {
		return time.Time{}, time.Time{}, errMalformed
	}
	return recheckAt, deadline, nil
}

func validateTimestamp(value string) error {
	if _, err := time.Parse(time.RFC3339, value); err != nil {
		return errMalformed
	}
	return nil
}

func validState(state State) bool {
	switch state {
	case StateActive, StateWaitExternal, StateVerify, StateComplete, StateBlocked:
		return true
	default:
		return false
	}
}

func validAction(action Action) bool {
	switch action {
	case ActionProgress, ActionNoProgress, ActionWaitExternal, ActionRecheck,
		ActionRequestVerify, ActionVerify, ActionRetryableFailure, ActionTerminalFailure:
		return true
	default:
		return false
	}
}

func validPredicateKind(kind PredicateKind) bool {
	switch kind {
	case PredicateCommandExitZero, PredicateFileExists, PredicateChecksGreen,
		PredicateReviewApproved, PredicateExactArtifact:
		return true
	default:
		return false
	}
}

func validWaitOwner(owner WaitOwner) bool {
	switch owner {
	case WaitOwnerOperator, WaitOwnerGitHub, WaitOwnerGitea, WaitOwnerClaude,
		WaitOwnerCodex, WaitOwnerOpenCode, WaitOwnerRadioactiveRalph,
		WaitOwnerCodeRabbit, WaitOwnerAIGitBot, WaitOwnerExternal:
		return true
	default:
		return false
	}
}

func decisionFromSnapshot(snapshot Snapshot, reason ReasonCode) Decision {
	return Decision{
		SchemaVersion:   SchemaVersion,
		State:           snapshot.State,
		RetryCount:      snapshot.RetryCount,
		NoProgressCount: snapshot.NoProgressCount,
		Wait:            normalizedWait(snapshot.Wait),
		Reason:          reason,
	}
}

func blockedFrom(snapshot Snapshot, reason ReasonCode) Decision {
	snapshot.State = StateBlocked
	snapshot.Wait = nil
	return decisionFromSnapshot(snapshot, reason)
}

func blockedMalformed() Decision {
	return Decision{SchemaVersion: SchemaVersion, State: StateBlocked, Reason: ReasonMalformedInput}
}

func normalizedWait(wait *WaitWindow) *WaitWindow {
	if wait == nil {
		return nil
	}
	recheckAt, deadline, err := parseWait(wait)
	if err != nil {
		return nil
	}
	return &WaitWindow{
		Owner:     wait.Owner,
		RecheckAt: recheckAt.UTC().Format(time.RFC3339Nano),
		Deadline:  deadline.UTC().Format(time.RFC3339Nano),
	}
}

func rejectTrailingJSON(decoder *json.Decoder) error {
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return errMalformed
	}
	return nil
}
