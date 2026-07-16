package orch

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/jbcom/radioactive-ralph/internal/a2a"
	"github.com/jbcom/radioactive-ralph/internal/plan"
	"github.com/jbcom/radioactive-ralph/internal/store"
)

// jsonMarshal is a small wrapper so call sites read as intent (see
// mustPayloadJSON) without repeating the encoding/json import everywhere.
func jsonMarshal(v store.EventPayload) (string, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

// jsonMarshalMessage serializes an a2a.Message for storage in
// a2a_messages.content_json.
func jsonMarshalMessage(msg *a2a.Message) (string, error) {
	raw, err := json.Marshal(msg)
	if err != nil {
		return "", fmt.Errorf("orch: marshal a2a message: %w", err)
	}
	return string(raw), nil
}

// parseStepRefID parses a StepRef.ID()-shaped string (e.g. "0.1.2", dot
// -joined non-negative integers) back into a plan.StepRef. This is the
// inverse of StepRef.ID(): the last component is the leaf Index, and every
// component before it is the GroupPath.
func parseStepRefID(id string) (plan.StepRef, bool) {
	if id == "" {
		return plan.StepRef{}, false
	}
	parts := strings.Split(id, ".")
	nums := make([]int, 0, len(parts))
	for _, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return plan.StepRef{}, false
		}
		nums = append(nums, n)
	}
	if len(nums) == 0 {
		return plan.StepRef{}, false
	}
	return plan.StepRef{GroupPath: nums[:len(nums)-1], Index: nums[len(nums)-1]}, true
}
