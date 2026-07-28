package plan

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/yuin/goldmark/ast"
)

// taskMetadataLanguage is the fenced-code-block language that marks a step's
// execution metadata. The `[approval]` marker is the precedent: plan markdown
// already carries structured meaning inside an ordinary list item, and this is
// the same idea with more than a boolean to say.
const taskMetadataLanguage = "ralph-task"

// parseTaskMetadataBlock decodes the single ```ralph-task block inside one list
// item, or returns nil when the step has none.
//
// Decoding is strict in the ways that prevent silent misinterpretation —
// unknown fields are rejected, so a typo'd key fails the import instead of being
// ignored, and trailing JSON is rejected so a second object cannot hide behind
// the first. It is deliberately NOT strict about which keys are present: only
// `id` is required. Requiring all of them would force every annotated step to
// spell out eight fields it does not care about, and would make an omitted
// `after` impossible to express — but omitted-vs-empty `after` is exactly the
// distinction that decides whether a step keeps document order or becomes a
// root (see TaskMetadata.After).
func parseTaskMetadataBlock(item ast.Node, source []byte) (*TaskMetadata, error) {
	var blocks []*ast.FencedCodeBlock
	if err := ast.Walk(item, func(node ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		block, ok := node.(*ast.FencedCodeBlock)
		if ok && string(block.Language(source)) == taskMetadataLanguage {
			blocks = append(blocks, block)
		}
		return ast.WalkContinue, nil
	}); err != nil {
		return nil, fmt.Errorf("walk task metadata: %w", err)
	}
	if len(blocks) == 0 {
		return nil, nil
	}
	if len(blocks) != 1 {
		return nil, fmt.Errorf(
			"step must contain at most one %s block, found %d", taskMetadataLanguage, len(blocks))
	}

	var raw bytes.Buffer
	for i := range blocks[0].Lines().Len() {
		line := blocks[0].Lines().At(i)
		raw.Write(line.Value(source))
	}
	rawJSON := raw.Bytes()

	// Reject duplicate keys BEFORE decoding. encoding/json silently keeps the
	// last value for a repeated key even with DisallowUnknownFields, which
	// defeats the ordering guarantee this whole grammar exists to provide:
	// {"id":"x","after":["prepare"],"after":[]} would decode as an
	// unconditioned root and run before `prepare`. A duplicate can also hide a
	// preceding null from rejectNullMetadataFields.
	if err := rejectDuplicateKeys(rawJSON); err != nil {
		return nil, err
	}

	decoder := json.NewDecoder(bytes.NewReader(rawJSON))
	// A misspelled key is a plan bug, not a field to ignore: without this, a
	// step annotated `"dependsOn"` instead of `"after"` would import with no
	// edges and run in the wrong order.
	decoder.DisallowUnknownFields()
	var metadata TaskMetadata
	if err := decoder.Decode(&metadata); err != nil {
		return nil, fmt.Errorf("decode %s JSON: %w", taskMetadataLanguage, err)
	}
	if err := ensureJSONEOF(decoder); err != nil {
		return nil, err
	}
	if err := rejectNullMetadataFields(rawJSON); err != nil {
		return nil, err
	}
	if metadata.ID == "" {
		return nil, fmt.Errorf("%s block requires a non-empty %q", taskMetadataLanguage, "id")
	}
	if err := rejectPaddedTaskIDs(&metadata); err != nil {
		return nil, err
	}
	return &metadata, nil
}

// rejectPaddedTaskIDs refuses a task ID carrying leading or trailing whitespace,
// in `id` or in any reference to one.
//
// These strings are used verbatim as lookup keys — `differentFrom` entries reach
// GetTaskExecutionMetadata as task IDs, and `after` entries resolve to graph
// edges. Whitespace makes the key un-matchable against the real task, and the
// resulting failure is the quiet kind: the step is not rejected, not marked
// blocked, and not reported. It simply never becomes dispatchable, forever,
// because no amount of running `produce` creates a task named " produce ".
//
// Rejected rather than silently trimmed, for the same reason a null or a
// duplicate key is rejected above: the author is present at import to fix it,
// and quietly repairing input means a plan that reads one way runs another. It
// also keeps validation honest — validateDifferentFrom compares TrimSpace'd
// values, so without this an entry could PASS validation and then fail every
// lookup it was validated for.
func rejectPaddedTaskIDs(m *TaskMetadata) error {
	check := func(field, value string) error {
		if value != strings.TrimSpace(value) {
			return fmt.Errorf(
				"%s field %q contains the task ID %q with surrounding whitespace; "+
					"task IDs are matched exactly, so this can never resolve",
				taskMetadataLanguage, field, value)
		}
		return nil
	}
	if err := check("id", m.ID); err != nil {
		return err
	}
	for _, peer := range m.DifferentFrom {
		if err := check("differentFrom", peer); err != nil {
			return err
		}
	}
	if m.After != nil {
		for _, dep := range *m.After {
			if err := check("after", dep); err != nil {
				return err
			}
		}
	}
	return nil
}

// rejectDuplicateKeys refuses any object in the metadata that repeats a key, at
// any nesting depth.
//
// encoding/json resolves a duplicate by silently keeping the last occurrence.
// Here that is an ordering-safety hole rather than a style nit: a step declaring
// {"after":["prepare"],"after":[]} decodes as an unconditioned root and would
// dispatch before the task it depends on. A duplicate can also mask a null that
// rejectNullMetadataFields would otherwise catch.
//
// The check walks the token stream rather than decoding into a map, because a
// map decode has already collapsed the duplicate by the time it is observable.
func rejectDuplicateKeys(raw []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	if err := checkJSONValue(decoder); err != nil {
		return err
	}
	return nil
}

// checkJSONValue consumes exactly one JSON value, descending through containers
// and enforcing key uniqueness within each object.
func checkJSONValue(decoder *json.Decoder) error {
	tok, err := decoder.Token()
	if err != nil {
		if errors.Is(err, io.EOF) {
			return nil
		}
		return fmt.Errorf("scan %s JSON: %w", taskMetadataLanguage, err)
	}
	delim, isDelim := tok.(json.Delim)
	if !isDelim {
		return nil // scalar
	}

	switch delim {
	case '{':
		seen := map[string]struct{}{}
		for {
			keyTok, err := decoder.Token()
			if err != nil {
				return fmt.Errorf("scan %s object: %w", taskMetadataLanguage, err)
			}
			if d, ok := keyTok.(json.Delim); ok && d == '}' {
				return nil
			}
			key, ok := keyTok.(string)
			if !ok {
				return fmt.Errorf("%s object key must be a string", taskMetadataLanguage)
			}
			if _, dup := seen[key]; dup {
				return fmt.Errorf(
					"%s block repeats key %q; a duplicate silently overrides the earlier value",
					taskMetadataLanguage, key)
			}
			seen[key] = struct{}{}
			if err := checkJSONValue(decoder); err != nil {
				return err
			}
		}
	case '[':
		for decoder.More() {
			if err := checkJSONValue(decoder); err != nil {
				return err
			}
		}
		if _, err := decoder.Token(); err != nil { // consume ']'
			return fmt.Errorf("scan %s array: %w", taskMetadataLanguage, err)
		}
		return nil
	}
	return nil
}

// rejectNullMetadataFields refuses an explicit JSON null.
//
// `"after": null` decodes to a nil *[]string, which is indistinguishable from an
// omitted key — and those two mean different things here. Rather than silently
// treating a null as "omitted", reject it: the author wrote something
// deliberate, and guessing which of the two they meant is how a plan ends up
// with a graph its author did not intend.
func rejectNullMetadataFields(raw []byte) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return fmt.Errorf("decode %s object: %w", taskMetadataLanguage, err)
	}
	for field, value := range fields {
		if string(value) == "null" {
			return fmt.Errorf(
				"%s field %q must not be null; omit the key instead", taskMetadataLanguage, field)
		}
	}
	if binding, ok := fields["binding"]; ok {
		var bindingFields map[string]json.RawMessage
		if err := json.Unmarshal(binding, &bindingFields); err != nil {
			return fmt.Errorf("%s field %q must be an object", taskMetadataLanguage, "binding")
		}
		for field, value := range bindingFields {
			if string(value) == "null" {
				return fmt.Errorf(
					"%s binding field %q must not be null; omit the key instead",
					taskMetadataLanguage, field)
			}
		}
	}
	return nil
}

// ensureJSONEOF rejects a second JSON value after the metadata object, so a
// trailing object cannot sit unread inside the block.
func ensureJSONEOF(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return fmt.Errorf("%s block contains more than one JSON value", taskMetadataLanguage)
		}
		return fmt.Errorf("decode trailing %s JSON: %w", taskMetadataLanguage, err)
	}
	return nil
}
