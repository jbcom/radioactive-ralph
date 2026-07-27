package plan

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"

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
	return &metadata, nil
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
