package orch

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

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
	_ = os.WriteFile(filepath.Join(projectDir, "contract.md"), []byte("changed"), 0o600)
	md := "# Team\n\n" + v2RuntimeStep(
		"task.hash", nil, []string{"claude"}, nil, fmt.Sprintf("%064d", 0),
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
}

func TestV2AdmissionCapSkipsBlockedCandidateWithoutStarvingReadyWork(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)
	projectDir := t.TempDir()
	projectID, _ := st.CreateProject(ctx, "v2-cap", []store.Fingerprint{{
		Kind: store.FingerprintKindAbsPath, Value: projectDir,
	}})
	input := []byte("current")
	_ = os.WriteFile(filepath.Join(projectDir, "contract.md"), input, 0o600)
	md := "# Team\n\n" +
		v2RuntimeStep("task.stale", nil, []string{"claude"}, nil, fmt.Sprintf("%064d", 0)) + "\n" +
		v2RuntimeStep("task.ready", nil, []string{"claude"}, nil, fmt.Sprintf("%x", sha256.Sum256(input)))
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

  `+"```ralph-task"+`
  {"id":%q,"after":%s,"team":"studio/design","requires":["local-agent"],"providers":%s,"differentFrom":%s,"inputs":[{"path":"contract.md","sha256":%q}],"outputs":[{"path":"out/%s.json","mode":"exclusive"}]}
  `+"```"+`
`, id, id, encode(after), encode(providers), encode(differentFrom), hash, id)
}
