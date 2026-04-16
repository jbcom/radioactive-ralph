package runtime

import (
	"encoding/json"
	"fmt"
	"strings"
)

type workerResult struct {
	Outcome          string   `json:"outcome"`
	Summary          string   `json:"summary"`
	Evidence         []string `json:"evidence"`
	Reason           string   `json:"reason,omitempty"`
	HandoffTo        string   `json:"handoff_to,omitempty"`
	ApprovalRequired bool     `json:"approval_required,omitempty"`
	Retryable        bool     `json:"retryable,omitempty"`
	NeedsContext     []string `json:"needs_context,omitempty"`
}

func parseWorkerResult(raw string) (workerResult, error) {
	openIdx := strings.Index(raw, "{")
	closeIdx := strings.LastIndex(raw, "}")
	if openIdx < 0 || closeIdx < 0 || closeIdx < openIdx {
		return workerResult{}, fmt.Errorf("no JSON object found in provider output")
	}
	body := raw[openIdx : closeIdx+1]
	var out workerResult
	dec := json.NewDecoder(strings.NewReader(body))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&out); err != nil {
		return workerResult{}, err
	}
	switch out.Outcome {
	case "done", "failed", "need_operator", "handoff", "blocked", "need_context":
	default:
		return workerResult{}, fmt.Errorf("unknown outcome %q", out.Outcome)
	}
	switch out.Outcome {
	case "handoff":
		if strings.TrimSpace(out.HandoffTo) == "" {
			return workerResult{}, fmt.Errorf("handoff outcome requires handoff_to")
		}
	case "need_context":
		if len(out.NeedsContext) == 0 {
			return workerResult{}, fmt.Errorf("need_context outcome requires needs_context")
		}
	}
	return out, nil
}

func workerResultSchema() string {
	return `{
  "type": "object",
  "additionalProperties": false,
  "properties": {
    "outcome": {
      "type": "string",
      "enum": ["done", "failed", "need_operator", "handoff", "blocked", "need_context"]
    },
    "summary": { "type": "string" },
    "evidence": {
      "type": "array",
      "items": { "type": "string" }
    },
    "reason": { "type": "string" },
    "handoff_to": { "type": "string" },
    "approval_required": { "type": "boolean" },
    "retryable": { "type": "boolean" },
    "needs_context": {
      "type": "array",
      "items": { "type": "string" }
    }
  },
  "required": ["outcome", "summary", "evidence"]
}`
}
