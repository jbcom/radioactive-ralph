package orch

import (
	"encoding/json"
	"fmt"

	"github.com/jbcom/radioactive-ralph/internal/a2a"
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
