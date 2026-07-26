package orch

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/jbcom/radioactive-ralph/internal/plan"
	"github.com/jbcom/radioactive-ralph/internal/provider"
	"github.com/jbcom/radioactive-ralph/internal/store"
)

func TestV2NoWriteRunnerCannotComplete(t *testing.T) {
	task, _, _ := runV2BoundaryTask(
		t,
		func(md string) string { return md },
		&fakeRunner{results: []provider.Result{{AssistantOutput: "claimed done"}}},
	)
	if task.Status != store.TaskStatusPending {
		t.Fatalf("no-write task status = %s, want pending after rejected completion", task.Status)
	}
}

func TestV2MissingOneOfMultipleOutputsCannotComplete(t *testing.T) {
	task, _, _ := runV2BoundaryTask(
		t,
		func(md string) string {
			return strings.Replace(
				md,
				`"outputs":[{"path":"out/task.boundary.json","mode":"exclusive"}]`,
				`"outputs":[{"path":"out/task.boundary.json","mode":"exclusive"},{"path":"out/second.json","mode":"exclusive"}]`,
				1,
			)
		},
		&firstV2OutputRunner{},
	)
	if task.Status != store.TaskStatusPending {
		t.Fatalf("partial-output task status = %s, want pending", task.Status)
	}
}

func TestV2InputMutationAfterTurnCannotComplete(t *testing.T) {
	task, root, _ := runV2BoundaryTask(
		t,
		func(md string) string { return md },
		&mutatingV2InputRunner{},
	)
	if task.Status != store.TaskStatusPending {
		t.Fatalf("input-mutating task status = %s, want pending", task.Status)
	}
	raw, err := os.ReadFile(filepath.Join(root, "contract.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != "mutated after admission" {
		t.Fatalf("runner did not exercise input mutation: %q", raw)
	}
}

func TestV2EscapingOutputSymlinkAfterTurnCannotComplete(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation is not available to an unprivileged Windows test")
	}
	outside := t.TempDir()
	outsideFile := filepath.Join(outside, "escaped.json")
	if err := os.WriteFile(outsideFile, []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}
	task, _, _ := runV2BoundaryTask(
		t,
		func(md string) string { return md },
		&escapingV2OutputRunner{target: outsideFile},
	)
	if task.Status != store.TaskStatusPending {
		t.Fatalf("escaping-output task status = %s, want pending", task.Status)
	}
}

func TestV2AcceptanceCommandCannotRemoveVerifiedOutput(t *testing.T) {
	task, root, _ := runV2BoundaryTask(
		t,
		func(md string) string {
			return strings.Replace(
				md,
				"`accept-file: out/task.boundary.json`",
				"`accept-file: out/task.boundary.json` `accept: rm out/task.boundary.json`",
				1,
			)
		},
		&bindingRecordingRunner{},
	)
	if task.Status != store.TaskStatusPending {
		t.Fatalf("acceptance-mutated task status = %s, want pending", task.Status)
	}
	if _, err := os.Stat(filepath.Join(root, "out", "task.boundary.json")); !os.IsNotExist(err) {
		t.Fatalf("acceptance command did not remove output as intended: %v", err)
	}
}

func TestStrictV2AcceptanceCarriesEveryDeclaredOutput(t *testing.T) {
	root := t.TempDir()
	metadata := &plan.TaskMetadata{
		Outputs: []plan.TaskOutput{
			{Path: "out/one.json", Mode: "exclusive"},
			{Path: "out/two.json", Mode: "exclusive"},
		},
	}
	raw, err := strictV2AcceptanceJSON(
		plan.Step{Text: "produce both `accept-file: out/one.json`"},
		metadata,
		root,
	)
	if err != nil {
		t.Fatalf("strictV2AcceptanceJSON: %v", err)
	}
	var acceptance Acceptance
	if err := json.Unmarshal([]byte(raw), &acceptance); err != nil {
		t.Fatalf("decode acceptance: %v", err)
	}
	if strings.Join(acceptance.RequiredOutputs, ",") != "out/one.json,out/two.json" {
		t.Fatalf("required outputs = %v", acceptance.RequiredOutputs)
	}
}

type firstV2OutputRunner struct{}

func (*firstV2OutputRunner) Run(
	_ context.Context,
	_ provider.Binding,
	req provider.Request,
) (provider.Result, error) {
	metadata, err := promptedV2Metadata(req)
	if err != nil {
		return provider.Result{}, err
	}
	if len(metadata.Outputs) == 0 {
		return provider.Result{}, fmt.Errorf("prompt has no outputs")
	}
	if err := writeV2TestFile(req.WorkingDir, metadata.Outputs[0].Path, "first only"); err != nil {
		return provider.Result{}, err
	}
	return provider.Result{AssistantOutput: "wrote first output"}, nil
}

type mutatingV2InputRunner struct{}

func (*mutatingV2InputRunner) Run(
	_ context.Context,
	_ provider.Binding,
	req provider.Request,
) (provider.Result, error) {
	metadata, err := promptedV2Metadata(req)
	if err != nil {
		return provider.Result{}, err
	}
	if err := writeDeclaredV2Outputs(req); err != nil {
		return provider.Result{}, err
	}
	if len(metadata.Inputs) == 0 {
		return provider.Result{}, fmt.Errorf("prompt has no inputs")
	}
	if err := os.WriteFile(
		filepath.Join(req.WorkingDir, filepath.FromSlash(metadata.Inputs[0].Path)),
		[]byte("mutated after admission"),
		0o600,
	); err != nil {
		return provider.Result{}, err
	}
	return provider.Result{AssistantOutput: "mutated input and wrote output"}, nil
}

type escapingV2OutputRunner struct {
	target string
}

func (r *escapingV2OutputRunner) Run(
	_ context.Context,
	_ provider.Binding,
	req provider.Request,
) (provider.Result, error) {
	metadata, err := promptedV2Metadata(req)
	if err != nil {
		return provider.Result{}, err
	}
	if len(metadata.Outputs) == 0 {
		return provider.Result{}, fmt.Errorf("prompt has no outputs")
	}
	output := filepath.Join(
		req.WorkingDir,
		filepath.FromSlash(metadata.Outputs[0].Path),
	)
	if err := os.MkdirAll(filepath.Dir(output), 0o755); err != nil {
		return provider.Result{}, err
	}
	if err := os.Symlink(r.target, output); err != nil {
		return provider.Result{}, err
	}
	return provider.Result{AssistantOutput: "created escaping symlink"}, nil
}

func promptedV2Metadata(req provider.Request) (plan.TaskMetadata, error) {
	raw, ok := v2ContractFromPrompt(req.UserPrompt)
	if !ok {
		return plan.TaskMetadata{}, fmt.Errorf("v2 prompt contract missing")
	}
	var metadata plan.TaskMetadata
	if err := json.Unmarshal([]byte(raw), &metadata); err != nil {
		return plan.TaskMetadata{}, err
	}
	return metadata, nil
}

func writeV2TestFile(root, relative, contents string) error {
	full := filepath.Join(root, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return err
	}
	return os.WriteFile(full, []byte(contents), 0o600)
}

func runV2BoundaryTask(
	t *testing.T,
	mutatePlan func(string) string,
	runner provider.Runner,
) (*store.Task, string, string) {
	t.Helper()
	ctx := context.Background()
	st := newTestStore(t)
	root := t.TempDir()
	projectID, err := st.CreateProject(ctx, "v2-boundary", []store.Fingerprint{{
		Kind: store.FingerprintKindAbsPath, Value: root,
	}})
	if err != nil {
		t.Fatal(err)
	}
	input := []byte("boundary contract")
	if err := os.WriteFile(filepath.Join(root, "contract.md"), input, 0o600); err != nil {
		t.Fatal(err)
	}
	hash := fmt.Sprintf("%x", sha256.Sum256(input))
	md := mutatePlan("# Boundary\n\n" + v2RuntimeStep(
		"task.boundary", nil, []string{"claude"}, nil, hash,
	))
	o := New(
		st,
		WithRunnerFactory(func(provider.Binding) (provider.Runner, error) {
			return runner, nil
		}),
		WithConstrainedBindingResolver(constrainedTestPool("claude")),
	)
	planID, err := o.ImportPlan(ctx, ImportPlanOpts{
		ProjectID: projectID,
		Slug:      "v2-boundary",
		Title:     "V2 Boundary",
		Markdown:  md,
	})
	if err != nil {
		t.Fatalf("ImportPlan: %v", err)
	}
	if count, err := o.DispatchNext(ctx, projectID, planID); err != nil || count != 1 {
		t.Fatalf("DispatchNext = %d, %v; want one", count, err)
	}
	o.Wait()
	task, err := st.GetTask(ctx, planID, "task.boundary")
	if err != nil {
		t.Fatal(err)
	}
	return task, root, planID
}
