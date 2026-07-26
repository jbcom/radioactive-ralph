package orch

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/jbcom/radioactive-ralph/internal/plan"
	"github.com/jbcom/radioactive-ralph/internal/provider"
	"github.com/jbcom/radioactive-ralph/internal/store"
)

func TestV2DispatchHonorsDAGProviderSeparationAndPersistsEvidence(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)
	projectDir := t.TempDir()
	projectID, err := st.CreateProject(ctx, "v2-runtime", []store.Fingerprint{{
		Kind: store.FingerprintKindAbsPath, Value: projectDir,
	}})
	if err != nil {
		t.Fatal(err)
	}
	input := []byte("story contract\n")
	if err := os.WriteFile(filepath.Join(projectDir, "contract.md"), input, 0o600); err != nil {
		t.Fatal(err)
	}
	hash := fmt.Sprintf("%x", sha256.Sum256(input))
	md := "# Design\n\n" +
		v2RuntimeStep("task.write", nil, []string{"claude"}, nil, hash) + "\n" +
		v2RuntimeStep("task.review", []string{"task.write"}, []string{"claude", "codex"}, []string{"task.write"}, hash)

	runner := &bindingRecordingRunner{}
	resolver := constrainedTestPool("claude", "codex")
	o := New(st,
		WithRunnerFactory(func(provider.Binding) (provider.Runner, error) { return runner, nil }),
		WithConstrainedBindingResolver(resolver),
	)
	planID, err := o.ImportPlan(ctx, ImportPlanOpts{
		ProjectID: projectID, Slug: "v2-runtime", Title: "V2 Runtime", Markdown: md,
	})
	if err != nil {
		t.Fatalf("ImportPlan: %v", err)
	}

	if count, err := o.DispatchNext(ctx, projectID, planID); err != nil || count != 1 {
		t.Fatalf("first DispatchNext = %d, %v; want 1", count, err)
	}
	o.Wait()
	if count, err := o.DispatchNext(ctx, projectID, planID); err != nil || count != 1 {
		t.Fatalf("second DispatchNext = %d, %v; want 1", count, err)
	}
	o.Wait()

	if got := runner.names(); strings.Join(got, ",") != "claude,codex" {
		t.Fatalf("providers = %v, want claude,codex", got)
	}
	for taskID, wantProvider := range map[string]string{
		"task.write": "claude", "task.review": "codex",
	} {
		task, err := st.GetTask(ctx, planID, taskID)
		if err != nil || task.Status != store.TaskStatusDone {
			t.Fatalf("task %s = %+v, %v; want done", taskID, task, err)
		}
		metadata, err := st.GetTaskExecutionMetadata(ctx, planID, taskID)
		if err != nil {
			t.Fatal(err)
		}
		if metadata.AssignedProvider != wantProvider || metadata.CompletionEvidenceJSON == "" {
			t.Fatalf("metadata %s = %+v", taskID, metadata)
		}
	}
	progress, err := o.PlanProgress(ctx, planID)
	if err != nil || progress != (Progress{Done: 2, Total: 2}) {
		t.Fatalf("progress = %+v, %v; want 2/2", progress, err)
	}
}

func TestV2DispatchBlocksWhenProviderSeparationExhaustsPool(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)
	projectDir := t.TempDir()
	projectID, _ := st.CreateProject(ctx, "v2-block", []store.Fingerprint{{
		Kind: store.FingerprintKindAbsPath, Value: projectDir,
	}})
	input := []byte("fixed\n")
	_ = os.WriteFile(filepath.Join(projectDir, "contract.md"), input, 0o600)
	hash := fmt.Sprintf("%x", sha256.Sum256(input))
	md := "# Team\n\n" +
		v2RuntimeStep("task.one", nil, []string{"claude"}, nil, hash) + "\n" +
		v2RuntimeStep("task.two", []string{"task.one"}, []string{"claude"}, []string{"task.one"}, hash)
	runner := &bindingRecordingRunner{}
	o := New(st,
		WithRunnerFactory(func(provider.Binding) (provider.Runner, error) { return runner, nil }),
		WithConstrainedBindingResolver(constrainedTestPool("claude")),
	)
	planID, err := o.ImportPlan(ctx, ImportPlanOpts{
		ProjectID: projectID, Slug: "block", Title: "Block", Markdown: md,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, _ = o.DispatchNext(ctx, projectID, planID)
	o.Wait()
	if count, err := o.DispatchNext(ctx, projectID, planID); err != nil || count != 0 {
		t.Fatalf("blocked dispatch = %d, %v", count, err)
	}
	task, _ := st.GetTask(ctx, planID, "task.two")
	if task.Status != store.TaskStatusBlockedCapability {
		t.Fatalf("status = %s, want blocked_capability", task.Status)
	}
}

func TestV2DispatchHashMismatchNeverInvokesProvider(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)
	projectDir := t.TempDir()
	projectID, _ := st.CreateProject(ctx, "v2-hash", []store.Fingerprint{{
		Kind: store.FingerprintKindAbsPath, Value: projectDir,
	}})
	input := []byte("expected")
	_ = os.WriteFile(filepath.Join(projectDir, "contract.md"), input, 0o600)
	md := "# Team\n\n" + v2RuntimeStep(
		"task.hash", nil, []string{"claude"}, nil, fmt.Sprintf("%x", sha256.Sum256(input)),
	)
	runner := &bindingRecordingRunner{}
	o := New(st,
		WithRunnerFactory(func(provider.Binding) (provider.Runner, error) { return runner, nil }),
		WithConstrainedBindingResolver(constrainedTestPool("claude")),
	)
	planID, err := o.ImportPlan(ctx, ImportPlanOpts{
		ProjectID: projectID, Slug: "hash", Title: "Hash", Markdown: md,
	})
	if err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(projectDir, "contract.md"), []byte("changed"), 0o600)
	if count, err := o.DispatchNext(ctx, projectID, planID); err != nil || count != 0 {
		t.Fatalf("DispatchNext = %d, %v", count, err)
	}
	if got := runner.names(); len(got) != 0 {
		t.Fatalf("provider invoked on stale input: %v", got)
	}
	task, _ := st.GetTask(ctx, planID, "task.hash")
	if task.Status != store.TaskStatusBlockedInput {
		t.Fatalf("status = %s, want blocked_input", task.Status)
	}
	if err := os.WriteFile(filepath.Join(projectDir, "contract.md"), input, 0o600); err != nil {
		t.Fatal(err)
	}
	if found, changed, err := st.RetryBlockedTask(ctx, planID, "task.hash"); err != nil || !found || !changed {
		t.Fatalf("RetryBlockedTask = %v,%v,%v", found, changed, err)
	}
	if count, err := o.DispatchNext(ctx, projectID, planID); err != nil || count != 1 {
		t.Fatalf("retry DispatchNext = %d,%v", count, err)
	}
	o.Wait()
	task, _ = st.GetTask(ctx, planID, "task.hash")
	if task.Status != store.TaskStatusDone || len(runner.names()) != 1 {
		t.Fatalf("retried task=%+v runner=%v", task, runner.names())
	}
}

func TestV2AdmissionCapSkipsBlockedCandidateWithoutStarvingReadyWork(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)
	projectDir := t.TempDir()
	projectID, _ := st.CreateProject(ctx, "v2-cap", []store.Fingerprint{{
		Kind: store.FingerprintKindAbsPath, Value: projectDir,
	}})
	staleInput := []byte("stale expected")
	readyInput := []byte("ready expected")
	_ = os.WriteFile(filepath.Join(projectDir, "stale.md"), staleInput, 0o600)
	_ = os.WriteFile(filepath.Join(projectDir, "ready.md"), readyInput, 0o600)
	md := "# Team\n\n" +
		v2RuntimeStepWithInput("task.stale", nil, []string{"claude"}, nil, "stale.md", fmt.Sprintf("%x", sha256.Sum256(staleInput))) + "\n" +
		v2RuntimeStepWithInput("task.ready", nil, []string{"claude"}, nil, "ready.md", fmt.Sprintf("%x", sha256.Sum256(readyInput)))
	runner := &bindingRecordingRunner{}
	o := New(st,
		WithMaxParallel(1),
		WithRunnerFactory(func(provider.Binding) (provider.Runner, error) { return runner, nil }),
		WithConstrainedBindingResolver(constrainedTestPool("claude")),
	)
	planID, err := o.ImportPlan(ctx, ImportPlanOpts{
		ProjectID: projectID, Slug: "cap", Title: "Cap", Markdown: md,
	})
	if err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(projectDir, "stale.md"), []byte("changed"), 0o600)
	if count, err := o.DispatchNext(ctx, projectID, planID); err != nil || count != 1 {
		t.Fatalf("DispatchNext = %d, %v; want one admitted worker", count, err)
	}
	o.Wait()
	stale, _ := st.GetTask(ctx, planID, "task.stale")
	ready, _ := st.GetTask(ctx, planID, "task.ready")
	if stale.Status != store.TaskStatusBlockedInput || ready.Status != store.TaskStatusDone {
		t.Fatalf("stale=%s ready=%s", stale.Status, ready.Status)
	}
}

type calibratedRecordingRunner struct {
	request provider.Request
	binding provider.Binding
}

func (r *calibratedRecordingRunner) Run(
	_ context.Context,
	binding provider.Binding,
	req provider.Request,
) (provider.Result, error) {
	r.binding, r.request = binding, req
	if err := writeDeclaredV2Outputs(req); err != nil {
		return provider.Result{}, err
	}
	invocation, err := provider.ResolveInvocation(binding, req)
	if err != nil {
		return provider.Result{}, err
	}
	return provider.Result{
		SessionID: "provider-session-exact", AssistantOutput: "done",
		Invocation: invocation,
	}, nil
}

func TestV2CalibratedBindingPinsExactInvocationAndProvenance(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("calibrated lanes fail closed until Windows Job Object cleanup is implemented")
	}
	ctx := context.Background()
	st := newTestStore(t)
	root := t.TempDir()
	projectID, err := st.CreateProject(ctx, "calibrated", []store.Fingerprint{{
		Kind: store.FingerprintKindAbsPath, Value: root,
	}})
	if err != nil {
		t.Fatal(err)
	}
	input := []byte("contract")
	if err := os.WriteFile(filepath.Join(root, "contract.md"), input, 0o600); err != nil {
		t.Fatal(err)
	}
	binPath, binBytes := writeCalibrationTestBinary(t, "claude", "test")
	t.Setenv("PATH", filepath.Dir(binPath))
	alias := "claude-exact-xhigh"
	binding, err := provider.ResolveShippedBinding("claude")
	if err != nil {
		t.Fatal(err)
	}
	binding.Name = alias
	binding.Config.Binary = binPath
	invocationHash, err := provider.InvocationConfigHash(
		binding, provider.Model("claude-exact-5"), "xhigh",
	)
	if err != nil {
		t.Fatal(err)
	}
	calibration := store.ProviderCalibration{
		Alias: alias, Provider: "claude", Model: "claude-exact-5", Effort: "xhigh",
		BinaryPath: binPath, BinaryVersion: "test",
		BinarySHA256:   fmt.Sprintf("%x", sha256.Sum256(binBytes)),
		InvocationHash: invocationHash, InferenceDomain: "anthropic",
		ControlDomain: "local-cli", IndependenceDomain: "anthropic",
		Capabilities: []string{"quality.causal-narrative"},
		EvidenceJSON: `{"fixture":"causal","passes":3}`,
	}
	calibrationID, err := st.PutProviderCalibration(ctx, calibration)
	if err != nil {
		t.Fatal(err)
	}
	hash := fmt.Sprintf("%x", sha256.Sum256(input))
	md := v2RuntimeStep("task.exact", nil, []string{"claude"}, nil, hash)
	md = strings.Replace(
		md,
		`"binding":{"mode":"pool","alias":"","provider":"","model":"","effort":"","calibration":"","repetitions":0,"fixture":""}`,
		fmt.Sprintf(
			`"binding":{"mode":"calibrated","alias":%q,"provider":"claude","model":"claude-exact-5","effort":"xhigh","calibration":%q,"repetitions":0,"fixture":""}`,
			alias, calibrationID,
		),
		1,
	)
	md = strings.Replace(
		md, `"requires":["local-agent"]`,
		`"requires":["local-agent","quality.causal-narrative"]`, 1,
	)
	runner := &calibratedRecordingRunner{}
	o := New(st, WithRunnerFactory(func(provider.Binding) (provider.Runner, error) {
		return runner, nil
	}))
	planID, err := o.ImportPlan(ctx, ImportPlanOpts{
		ProjectID: projectID, Slug: "exact", Title: "Exact", Markdown: "# Exact\n\n" + md,
	})
	if err != nil {
		t.Fatal(err)
	}
	if count, err := o.DispatchNext(ctx, projectID, planID); err != nil || count != 1 {
		t.Fatalf("DispatchNext = %d, %v", count, err)
	}
	o.Wait()
	if runner.binding.Name != alias || runner.request.Model != "claude-exact-5" ||
		runner.request.Effort != "xhigh" || !runner.request.StrictBinding ||
		runner.binding.Config.Binary != binPath {
		t.Fatalf("runner binding=%+v request=%+v", runner.binding, runner.request)
	}
	promptContract, ok := v2ContractFromPrompt(runner.request.UserPrompt)
	if !ok {
		t.Fatal("worker prompt omitted the canonical v2 task contract")
	}
	var prompted plan.TaskMetadata
	if err := json.Unmarshal([]byte(promptContract), &prompted); err != nil {
		t.Fatalf("decode prompted contract: %v", err)
	}
	if prompted.ID != "task.exact" || prompted.Team != "studio/design" ||
		len(prompted.Inputs) != 1 || prompted.Inputs[0].Path != "contract.md" ||
		prompted.Inputs[0].SHA256 != hash ||
		len(prompted.Outputs) != 1 || prompted.Outputs[0].Path != "out/task.exact.json" ||
		!strings.Contains(runner.request.UserPrompt, v2AcceptanceHead) ||
		!strings.Contains(
			runner.request.UserPrompt,
			`"required_outputs":["out/task.exact.json"]`,
		) {
		t.Fatalf("incomplete v2 prompt contract: metadata=%+v prompt=%q", prompted, runner.request.UserPrompt)
	}
	metadata, err := st.GetTaskExecutionMetadata(ctx, planID, "task.exact")
	if err != nil {
		t.Fatal(err)
	}
	if metadata.AssignedAlias != alias || metadata.AssignedProvider != "claude" ||
		metadata.AssignedModel != "claude-exact-5" ||
		metadata.AssignedEffort != "xhigh" ||
		metadata.AssignedIndependenceDomain != "anthropic" ||
		metadata.ProviderSessionID != "provider-session-exact" {
		t.Fatalf("execution provenance = %+v", metadata)
	}
}

type exactSequenceRunner struct {
	mu       sync.Mutex
	requests []provider.Request
}

func (r *exactSequenceRunner) Run(
	_ context.Context,
	binding provider.Binding,
	req provider.Request,
) (provider.Result, error) {
	if err := writeDeclaredV2Outputs(req); err != nil {
		return provider.Result{}, err
	}
	invocation, err := provider.ResolveInvocation(binding, req)
	if err != nil {
		return provider.Result{}, err
	}
	r.mu.Lock()
	r.requests = append(r.requests, req)
	index := len(r.requests)
	r.mu.Unlock()
	return provider.Result{
		SessionID:       fmt.Sprintf("exact-session-%d", index),
		AssistantOutput: fmt.Sprintf("independent-fixture-output-%d", index),
		Invocation:      invocation,
	}, nil
}

func (r *exactSequenceRunner) calls() []provider.Request {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]provider.Request{}, r.requests...)
}

func TestV2CalibrationBootstrapRunsEveryRepetitionThenReadmitsAwaiter(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("calibrated lanes fail closed until Windows Job Object cleanup is implemented")
	}
	ctx := context.Background()
	st := newTestStore(t)
	root := t.TempDir()
	projectID, err := st.CreateProject(ctx, "calibration-bootstrap", []store.Fingerprint{{
		Kind: store.FingerprintKindAbsPath, Value: root,
	}})
	if err != nil {
		t.Fatal(err)
	}
	input := []byte("calibration contract")
	if err := os.WriteFile(filepath.Join(root, "contract.md"), input, 0o600); err != nil {
		t.Fatal(err)
	}
	hash := fmt.Sprintf("%x", sha256.Sum256(input))
	alias := "codex-sol-xhigh"
	const exactModel = "gpt-5.6-sol"

	fixtureStep := v2RuntimeStep("calibration.graph", nil, []string{"codex"}, nil, hash)
	fixtureStep = strings.Replace(
		fixtureStep,
		`"binding":{"mode":"pool","alias":"","provider":"","model":"","effort":"","calibration":"","repetitions":0,"fixture":""}`,
		fmt.Sprintf(
			`"binding":{"mode":"calibration","alias":%q,"provider":"codex","model":%q,"effort":"xhigh","calibration":"","repetitions":3,"fixture":"graph-reasoning"}`,
			alias, exactModel,
		),
		1,
	)
	fixtureStep = strings.Replace(fixtureStep, `"requires":["local-agent"]`, `"requires":[]`, 1)

	awaitStep := v2RuntimeStep("production.graph", nil, []string{"codex"}, nil, hash)
	awaitStep = strings.Replace(
		awaitStep,
		`"binding":{"mode":"pool","alias":"","provider":"","model":"","effort":"","calibration":"","repetitions":0,"fixture":""}`,
		fmt.Sprintf(
			`"binding":{"mode":"await-calibration","alias":%q,"provider":"codex","model":%q,"effort":"xhigh","calibration":"","repetitions":0,"fixture":""}`,
			alias, exactModel,
		),
		1,
	)
	awaitStep = strings.Replace(
		awaitStep, `"requires":["local-agent"]`,
		`"requires":["local-agent","quality.graph-reasoning"]`, 1,
	)

	runner := &exactSequenceRunner{}
	o := New(st, WithRunnerFactory(func(provider.Binding) (provider.Runner, error) {
		return runner, nil
	}))
	awaitPlanID, err := o.ImportPlan(ctx, ImportPlanOpts{
		ProjectID: projectID, Slug: "await", Title: "Await",
		Markdown: "# Await\n\n" + awaitStep,
	})
	if err != nil {
		t.Fatalf("import await plan before calibration: %v", err)
	}
	if count, err := o.DispatchNext(ctx, projectID, awaitPlanID); err != nil || count != 0 {
		t.Fatalf("pre-calibration dispatch = %d, %v; want blocked", count, err)
	}
	awaitTask, err := st.GetTask(ctx, awaitPlanID, "production.graph")
	if err != nil || awaitTask.Status != store.TaskStatusBlockedCapability {
		t.Fatalf("pre-calibration task = %+v, %v", awaitTask, err)
	}

	fixturePlanID, err := o.ImportPlan(ctx, ImportPlanOpts{
		ProjectID: projectID, Slug: "fixture", Title: "Fixture",
		Markdown: "# Fixture\n\n" + fixtureStep,
	})
	if err != nil {
		t.Fatalf("import fixture: %v", err)
	}
	if count, err := o.DispatchNext(ctx, projectID, fixturePlanID); err != nil || count != 1 {
		t.Fatalf("fixture dispatch = %d, %v", count, err)
	}
	o.Wait()

	calls := runner.calls()
	if len(calls) != 3 {
		t.Fatalf("calibration invocations = %d, want 3", len(calls))
	}
	for i, call := range calls {
		wantRepetition := fmt.Sprintf("Independent repetition: %d of 3", i+1)
		if !call.StrictBinding || call.Model != exactModel || call.Effort != "xhigh" ||
			!strings.Contains(call.UserPrompt, "Calibration fixture: graph-reasoning") ||
			!strings.Contains(call.UserPrompt, wantRepetition) {
			t.Fatalf("calibration call %d = %+v", i+1, call)
		}
	}
	attempts, err := st.ListCalibrationAttempts(ctx, fixturePlanID, "calibration.graph")
	if err != nil || len(attempts) != 3 {
		t.Fatalf("calibration attempts = %+v, %v", attempts, err)
	}
	for i, attempt := range attempts {
		wantHash := fmt.Sprintf(
			"%x", sha256.Sum256([]byte(fmt.Sprintf("independent-fixture-output-%d", i+1))),
		)
		if attempt.AttemptSequence != 1 || attempt.Repetition != i+1 ||
			attempt.Alias != alias || attempt.Provider != "codex" ||
			attempt.Model != exactModel || attempt.Effort != "xhigh" ||
			attempt.AssistantOutputSHA256 != wantHash {
			t.Fatalf("attempt %d = %+v", i+1, attempt)
		}
	}
	fixtureMetadata, err := st.GetTaskExecutionMetadata(ctx, fixturePlanID, "calibration.graph")
	if err != nil || !strings.Contains(
		fixtureMetadata.CompletionEvidenceJSON,
		"Calibration fixture graph-reasoning completed 3 independent repetitions",
	) || !strings.Contains(fixtureMetadata.CompletionEvidenceJSON, "Repetition 3") {
		t.Fatalf("aggregate fixture evidence = %q, %v", fixtureMetadata.CompletionEvidenceJSON, err)
	}

	binPath, binBytes := writeCalibrationTestBinary(t, "codex", "test")
	t.Setenv("PATH", filepath.Dir(binPath))
	binding, err := provider.ResolveShippedBinding("codex")
	if err != nil {
		t.Fatal(err)
	}
	binding.Name = alias
	binding.Config.Binary = binPath
	invocationHash, err := provider.InvocationConfigHash(
		binding, provider.Model(exactModel), "xhigh",
	)
	if err != nil {
		t.Fatal(err)
	}
	evidenceJSON, err := json.Marshal(map[string]any{
		"fixture_plan_id": fixturePlanID,
		"fixture_task_id": "calibration.graph",
		"attempts":        attempts,
		"adjudication":    "pass",
	})
	if err != nil {
		t.Fatal(err)
	}
	calibrationID, err := st.PutProviderCalibration(ctx, store.ProviderCalibration{
		Alias: alias, Provider: "codex", Model: exactModel, Effort: "xhigh",
		BinaryPath: binPath, BinaryVersion: "test",
		BinarySHA256:    fmt.Sprintf("%x", sha256.Sum256(binBytes)),
		InvocationHash:  invocationHash,
		InferenceDomain: "openai", ControlDomain: "local-cli",
		IndependenceDomain: "openai",
		Capabilities:       []string{"quality.graph-reasoning"},
		EvidenceJSON:       string(evidenceJSON),
	})
	if err != nil {
		t.Fatalf("adjudicate and mint calibration: %v", err)
	}
	minted, err := st.GetProviderCalibration(ctx, calibrationID)
	if err != nil || minted.Alias != alias ||
		!slices.Contains(minted.Capabilities, "quality.graph-reasoning") {
		t.Fatalf("minted calibration = %+v, %v", minted, err)
	}
	awaitTask, err = st.GetTask(ctx, awaitPlanID, "production.graph")
	if err != nil || awaitTask.Status != store.TaskStatusPending {
		t.Fatalf("auto-readmitted task = %+v, %v", awaitTask, err)
	}
	if count, err := o.DispatchNext(ctx, projectID, awaitPlanID); err != nil || count != 1 {
		t.Fatalf("post-calibration dispatch = %d, %v", count, err)
	}
	o.Wait()
	awaitMetadata, err := st.GetTaskExecutionMetadata(ctx, awaitPlanID, "production.graph")
	if err != nil || awaitMetadata.CalibrationID != calibrationID ||
		awaitMetadata.AssignedAlias != alias ||
		awaitMetadata.AssignedModel != exactModel ||
		awaitMetadata.AssignedEffort != "xhigh" {
		t.Fatalf("await execution metadata = %+v, %v", awaitMetadata, err)
	}
	if got := len(runner.calls()); got != 4 {
		t.Fatalf("total exact invocations = %d, want 4", got)
	}
}

func constrainedTestPool(names ...string) ConstrainedBindingResolver {
	var mu sync.Mutex
	cursor := 0
	return func(
		_ context.Context,
		_ string,
		_ bool,
		purpose BindingResolutionPurpose,
		constraints BindingConstraints,
	) (provider.Binding, error) {
		allowed, denied := stringBoolSet(constraints.AllowedProviders), stringBoolSet(constraints.DeniedProviders)
		var eligible []provider.Binding
		for _, name := range names {
			if len(allowed) > 0 && !allowed[name] || denied[name] {
				continue
			}
			binding, err := provider.ResolveShippedBinding(name)
			if err != nil {
				return provider.Binding{}, err
			}
			if binding.SupportsRequirements(constraints.Requirements) {
				eligible = append(eligible, binding)
			}
		}
		if len(eligible) == 0 {
			return provider.Binding{}, fmt.Errorf("%w: test pool exhausted", ErrNoCapableProvider)
		}
		mu.Lock()
		binding := eligible[cursor%len(eligible)]
		if purpose == BindingDispatch {
			cursor++
		}
		mu.Unlock()
		return binding, nil
	}
}

func stringBoolSet(values []string) map[string]bool {
	out := make(map[string]bool, len(values))
	for _, value := range values {
		out[value] = true
	}
	return out
}

func v2RuntimeStep(
	id string,
	after, providers, differentFrom []string,
	hash string,
) string {
	return v2RuntimeStepWithInput(id, after, providers, differentFrom, "contract.md", hash)
}

func v2RuntimeStepWithInput(
	id string,
	after, providers, differentFrom []string,
	inputPath, hash string,
) string {
	encode := func(values []string) string {
		if values == nil {
			return "[]"
		}
		parts := make([]string, len(values))
		for i, value := range values {
			parts[i] = fmt.Sprintf("%q", value)
		}
		return "[" + strings.Join(parts, ",") + "]"
	}
	return fmt.Sprintf(`- execute %s

  `+"`accept-file: out/%s.json`"+`

  `+"```ralph-task"+`
  {"id":%q,"after":%s,"team":"studio/design","binding":{"mode":"pool","alias":"","provider":"","model":"","effort":"","calibration":"","repetitions":0,"fixture":""},"requires":["local-agent"],"providers":%s,"differentFrom":%s,"inputs":[{"path":%q,"sha256":%q}],"outputs":[{"path":"out/%s.json","mode":"exclusive"}]}
  `+"```"+`
`, id, id, id, encode(after), encode(providers), encode(differentFrom), inputPath, hash, id)
}

func writeDeclaredV2Outputs(req provider.Request) error {
	contractJSON, ok := v2ContractFromPrompt(req.UserPrompt)
	if !ok {
		return nil
	}
	var metadata plan.TaskMetadata
	if err := json.Unmarshal([]byte(contractJSON), &metadata); err != nil {
		return fmt.Errorf("decode v2 prompt contract: %w", err)
	}
	for _, output := range metadata.Outputs {
		full := filepath.Join(req.WorkingDir, filepath.FromSlash(output.Path))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(full, []byte("produced by "+metadata.ID+"\n"), 0o600); err != nil {
			return err
		}
	}
	return nil
}

func v2ContractFromPrompt(prompt string) (string, bool) {
	start := strings.Index(prompt, v2ContractBegin)
	if start < 0 {
		return "", false
	}
	start += len(v2ContractBegin)
	end := strings.Index(prompt[start:], v2ContractEnd)
	if end < 0 {
		return "", false
	}
	raw := strings.TrimSpace(prompt[start : start+end])
	if newline := strings.IndexByte(raw, '\n'); newline >= 0 &&
		strings.HasPrefix(raw[:newline], "Task ID: ") {
		raw = strings.TrimSpace(raw[newline+1:])
	}
	return raw, true
}
