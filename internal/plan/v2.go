package plan

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/yuin/goldmark/ast"
)

const taskMetadataLanguage = "ralph-task"

var (
	stableTaskIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,127}$`)
	teamPathPattern     = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]*(?:/[a-z0-9][a-z0-9._-]*)*$`)
	sha256Pattern       = regexp.MustCompile(`^[0-9a-f]{64}$`)
	namePattern         = regexp.MustCompile(`^[a-z][a-z0-9._-]*$`)
	modelPattern        = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/-]*$`)
)

// TaskMetadata is the additive ralph.plan/v2 contract embedded in a list item
// as one strict JSON fenced block labelled ralph-task.
type TaskMetadata struct {
	ID            string       `json:"id"`
	After         []string     `json:"after"`
	Team          string       `json:"team"`
	Binding       TaskBinding  `json:"binding"`
	Requires      []string     `json:"requires"`
	Providers     []string     `json:"providers"`
	DifferentFrom []string     `json:"differentFrom"`
	Inputs        []TaskInput  `json:"inputs"`
	Outputs       []TaskOutput `json:"outputs"`
}

// TaskBinding either leaves every field empty for configured-pool selection,
// or pins an immutable calibrated execution lane.
type TaskBinding struct {
	Mode        string `json:"mode"`
	Alias       string `json:"alias"`
	Provider    string `json:"provider"`
	Model       string `json:"model"`
	Effort      string `json:"effort"`
	Calibration string `json:"calibration"`
	Repetitions int    `json:"repetitions"`
	Fixture     string `json:"fixture"`
}

// TaskInput pins one project-relative file to its exact content hash.
type TaskInput struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

// TaskOutput declares one project-relative exclusive output reservation.
type TaskOutput struct {
	Path string `json:"path"`
	Mode string `json:"mode"`
}

// V2Task pairs a task with its hierarchy and document order.
type V2Task struct {
	Step         Step
	GroupHeading string
	Order        int
}

// V2Tasks flattens v2 tasks in deterministic document order.
func (p *Plan) V2Tasks() []V2Task {
	var out []V2Task
	var walk func([]Group)
	walk = func(groups []Group) {
		for _, group := range groups {
			if len(group.SubGroups) > 0 {
				walk(group.SubGroups)
				continue
			}
			for _, step := range group.Steps {
				out = append(out, V2Task{Step: step, GroupHeading: group.Heading, Order: len(out)})
			}
		}
	}
	walk(p.Groups)
	return out
}

func countMetadata(groups []Group) (metadata, steps int) {
	for _, group := range groups {
		for _, step := range group.Steps {
			steps++
			if step.Metadata != nil {
				metadata++
			}
		}
		childMetadata, childSteps := countMetadata(group.SubGroups)
		metadata += childMetadata
		steps += childSteps
	}
	return metadata, steps
}

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
		return nil, fmt.Errorf("plan v2 step must contain exactly one %s block", taskMetadataLanguage)
	}
	var raw bytes.Buffer
	for i := 0; i < blocks[0].Lines().Len(); i++ {
		line := blocks[0].Lines().At(i)
		raw.Write(line.Value(source))
	}
	rawJSON := append([]byte{}, raw.Bytes()...)
	decoder := json.NewDecoder(bytes.NewReader(rawJSON))
	decoder.DisallowUnknownFields()
	var metadata TaskMetadata
	if err := decoder.Decode(&metadata); err != nil {
		return nil, fmt.Errorf("decode %s JSON: %w", taskMetadataLanguage, err)
	}
	if err := ensureJSONEOF(decoder); err != nil {
		return nil, err
	}
	if err := requireMetadataFields(rawJSON); err != nil {
		return nil, err
	}
	return &metadata, nil
}

func requireMetadataFields(raw []byte) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return fmt.Errorf("decode %s object: %w", taskMetadataLanguage, err)
	}
	for _, field := range []string{
		"id", "after", "team", "binding", "requires", "providers", "differentFrom", "inputs", "outputs",
	} {
		value, ok := fields[field]
		if !ok {
			return fmt.Errorf("%s block missing required field %q", taskMetadataLanguage, field)
		}
		if string(value) == "null" {
			return fmt.Errorf("%s field %q must not be null", taskMetadataLanguage, field)
		}
	}
	var bindingFields map[string]json.RawMessage
	if err := json.Unmarshal(fields["binding"], &bindingFields); err != nil {
		return fmt.Errorf("%s field %q must be an object", taskMetadataLanguage, "binding")
	}
	for _, field := range []string{
		"mode", "alias", "provider", "model", "effort",
		"calibration", "repetitions", "fixture",
	} {
		value, ok := bindingFields[field]
		if !ok {
			return fmt.Errorf("%s binding missing required field %q", taskMetadataLanguage, field)
		}
		if string(value) == "null" {
			return fmt.Errorf("%s binding field %q must not be null", taskMetadataLanguage, field)
		}
	}
	return nil
}

func ensureJSONEOF(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return fmt.Errorf("%s block contains more than one JSON value", taskMetadataLanguage)
		}
		return fmt.Errorf("decode trailing %s JSON: %w", taskMetadataLanguage, err)
	}
	return nil
}

func validateTaskMetadata(metadata *TaskMetadata) error {
	if !stableTaskIDPattern.MatchString(metadata.ID) {
		return fmt.Errorf("invalid stable id %q", metadata.ID)
	}
	if !teamPathPattern.MatchString(metadata.Team) {
		return fmt.Errorf("task %s has invalid team path %q", metadata.ID, metadata.Team)
	}
	if err := validateTaskBinding(metadata); err != nil {
		return err
	}
	if err := validateTaskLists(metadata); err != nil {
		return err
	}
	return validateTaskPaths(metadata)
}

func validateTaskBinding(metadata *TaskMetadata) error {
	bindingValues := []string{
		metadata.Binding.Alias, metadata.Binding.Provider,
		metadata.Binding.Model, metadata.Binding.Effort,
	}
	nonemptyBindingValues := 0
	for _, value := range bindingValues {
		if value != "" {
			nonemptyBindingValues++
		}
	}
	switch metadata.Binding.Mode {
	case "pool":
		if nonemptyBindingValues != 0 || metadata.Binding.Calibration != "" ||
			metadata.Binding.Repetitions != 0 || metadata.Binding.Fixture != "" {
			return fmt.Errorf("task %s pool binding must not pin calibration fields", metadata.ID)
		}
	case "calibrated":
		if nonemptyBindingValues != len(bindingValues) || metadata.Binding.Calibration == "" ||
			metadata.Binding.Repetitions != 0 || metadata.Binding.Fixture != "" {
			return fmt.Errorf("task %s calibrated binding requires an exact tuple and content address", metadata.ID)
		}
	case "await-calibration":
		if nonemptyBindingValues != len(bindingValues) || metadata.Binding.Calibration != "" ||
			metadata.Binding.Repetitions != 0 || metadata.Binding.Fixture != "" {
			return fmt.Errorf("task %s awaiting binding requires an exact tuple only", metadata.ID)
		}
	case "calibration":
		if nonemptyBindingValues != len(bindingValues) || metadata.Binding.Calibration != "" ||
			metadata.Binding.Repetitions < 3 || !namePattern.MatchString(metadata.Binding.Fixture) {
			return fmt.Errorf("task %s calibration run requires an exact tuple, fixture, and at least 3 repetitions", metadata.ID)
		}
		if len(metadata.Requires) != 0 {
			return fmt.Errorf("task %s calibration run cannot require capabilities before they are measured", metadata.ID)
		}
	default:
		return fmt.Errorf("task %s binding mode %q is invalid", metadata.ID, metadata.Binding.Mode)
	}
	return validateTaskBindingSyntax(metadata)
}

func validateTaskBindingSyntax(metadata *TaskMetadata) error {
	if metadata.Binding.Alias != "" && !namePattern.MatchString(metadata.Binding.Alias) {
		return fmt.Errorf("task %s binding alias %q is invalid", metadata.ID, metadata.Binding.Alias)
	}
	if metadata.Binding.Provider != "" && !namePattern.MatchString(metadata.Binding.Provider) {
		return fmt.Errorf("task %s binding provider %q is invalid", metadata.ID, metadata.Binding.Provider)
	}
	if metadata.Binding.Model != "" && !modelPattern.MatchString(metadata.Binding.Model) {
		return fmt.Errorf("task %s binding model %q is invalid", metadata.ID, metadata.Binding.Model)
	}
	if metadata.Binding.Effort != "" && !namePattern.MatchString(metadata.Binding.Effort) {
		return fmt.Errorf("task %s binding effort %q is invalid", metadata.ID, metadata.Binding.Effort)
	}
	if metadata.Binding.Calibration != "" &&
		!strings.HasPrefix(metadata.Binding.Calibration, "sha256:") {
		return fmt.Errorf("task %s binding calibration must be a sha256 content address", metadata.ID)
	}
	if metadata.Binding.Calibration != "" &&
		!sha256Pattern.MatchString(strings.TrimPrefix(metadata.Binding.Calibration, "sha256:")) {
		return fmt.Errorf("task %s binding calibration has invalid sha256", metadata.ID)
	}
	return nil
}

func validateTaskLists(metadata *TaskMetadata) error {
	for field, values := range map[string][]string{
		"after": metadata.After, "requires": metadata.Requires,
		"providers": metadata.Providers, "differentFrom": metadata.DifferentFrom,
	} {
		if contains(values, "") || len(values) != len(unique(values)) {
			return fmt.Errorf("task %s field %s must contain unique non-empty values", metadata.ID, field)
		}
	}
	for _, value := range append(append([]string{}, metadata.Requires...), metadata.Providers...) {
		if !namePattern.MatchString(value) {
			return fmt.Errorf("task %s has invalid capability/provider %q", metadata.ID, value)
		}
	}
	return nil
}

func validateTaskPaths(metadata *TaskMetadata) error {
	for _, input := range metadata.Inputs {
		if err := validateRelativePath(input.Path); err != nil {
			return fmt.Errorf("task %s input: %w", metadata.ID, err)
		}
		if !sha256Pattern.MatchString(input.SHA256) {
			return fmt.Errorf("task %s input %s has invalid sha256", metadata.ID, input.Path)
		}
	}
	if err := uniqueTaskPaths(metadata.ID, "inputs", inputPaths(metadata.Inputs)); err != nil {
		return err
	}
	for _, output := range metadata.Outputs {
		if err := validateRelativePath(output.Path); err != nil {
			return fmt.Errorf("task %s output: %w", metadata.ID, err)
		}
		if output.Mode != "exclusive" {
			return fmt.Errorf("task %s output %s mode must be exclusive", metadata.ID, output.Path)
		}
	}
	if err := uniqueTaskPaths(metadata.ID, "outputs", outputPaths(metadata.Outputs)); err != nil {
		return err
	}
	return nil
}

func validateRelativePath(path string) error {
	if path == "" || filepath.IsAbs(path) || strings.Contains(path, `\`) {
		return fmt.Errorf("path %q must be non-empty and relative", path)
	}
	clean := filepath.Clean(path)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return fmt.Errorf("path %q traverses outside the project", path)
	}
	return nil
}

func inputPaths(inputs []TaskInput) []string {
	paths := make([]string, len(inputs))
	for i := range inputs {
		paths[i] = inputs[i].Path
	}
	return paths
}

func outputPaths(outputs []TaskOutput) []string {
	paths := make([]string, len(outputs))
	for i := range outputs {
		paths[i] = outputs[i].Path
	}
	return paths
}

func uniqueTaskPaths(taskID, field string, paths []string) error {
	seen := map[string]bool{}
	for _, path := range paths {
		canonical := strings.ToLower(filepath.ToSlash(filepath.Clean(path)))
		if seen[canonical] {
			return fmt.Errorf("task %s field %s contains duplicate path %q", taskID, field, path)
		}
		seen[canonical] = true
	}
	return nil
}

func unique(values []string) []string {
	clone := append([]string{}, values...)
	sort.Strings(clone)
	if len(clone) == 0 {
		return clone
	}
	result := clone[:1]
	for _, value := range clone[1:] {
		if value != result[len(result)-1] {
			result = append(result, value)
		}
	}
	return result
}

func contains(values []string, needle string) bool {
	for _, value := range values {
		if value == needle {
			return true
		}
	}
	return false
}
