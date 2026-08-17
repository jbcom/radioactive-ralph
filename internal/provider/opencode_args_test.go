package provider

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/jbcom/radioactive-ralph/internal/adapters"
)

func opencodeTestArgs() []string {
	binding := Binding{Name: "opencode", Config: BindingConfig{Type: "opencode", Binary: "opencode"}}
	req := Request{UserPrompt: "do the thing"}
	inv, err := ResolveInvocation(binding, req)
	if err != nil {
		panic(err)
	}
	return opencodeArgs(binding, req, inv, false)
}

// TestOpencodeRunsPure pins --pure, verified present on the installed opencode:
// `--pure  run without external plugins`.
//
// A supervised turn must be reproducible from the plan alone. A plugin
// installed in the operator's environment would silently change what the agent
// can do, so the same plan would behave differently on two machines for reasons
// nothing records. This is a DETERMINISM choice, which is why it lands while
// --auto does not.
func TestOpencodeRunsPure(t *testing.T) {
	if args := opencodeTestArgs(); !slices.Contains(args, "--pure") {
		t.Fatalf("args = %v, want --pure", args)
	}
}

// TestOpencodeDoesNotAutoApprove is a REFUSAL, recorded so it cannot be
// re-added as an oversight.
//
// opencode's own help calls it what it is: `--auto  auto-approve permissions
// that are not explicitly denied (dangerous!)`. It is the same class of change
// as claude's bypassPermissions, and the same reasoning applies — the watchdog
// already kills a prompting turn and reports FailureInteractivePrompt, so the
// never-block invariant does not need it. What it WOULD change is the blast
// radius of an agent running unattended against a real checkout.
//
// AGENTS.md's control invariant sanctions "auto-resolves, DENIES, or
// kills-and-reclaims". Deny is listed; auto-approve is not.
func TestOpencodeDoesNotAutoApprove(t *testing.T) {
	if args := opencodeTestArgs(); slices.Contains(args, "--auto") {
		t.Fatalf("args carry --auto (opencode labels it dangerous); permission "+
			"prompts must stay a killable, reported condition: %v", args)
	}
}

// TestOpencodeBindingArgsStillAppendLast keeps the operator escape hatch: a
// binding that genuinely wants --auto can carry it in its own config Args,
// where it is a visible per-binding choice rather than an invisible default.
func TestOpencodeBindingArgsStillAppendLast(t *testing.T) {
	binding := Binding{Name: "opencode", Config: BindingConfig{
		Type: "opencode", Binary: "opencode", Args: []string{"--auto"},
	}}
	req := Request{UserPrompt: "x"}
	inv, err := ResolveInvocation(binding, req)
	if err != nil {
		t.Fatalf("ResolveInvocation: %v", err)
	}
	args := opencodeArgs(binding, req, inv, false)
	if len(args) == 0 || args[len(args)-1] != "--auto" {
		t.Fatalf("binding args must append LAST so an operator can opt in: %v", args)
	}
}

func TestManagedOpencodeUsesIsolatedPluginInsteadOfPure(t *testing.T) {
	binding := Binding{Name: "opencode", Config: BindingConfig{Type: "opencode", Binary: "opencode"}}
	inv, err := ResolveInvocation(binding, Request{})
	if err != nil {
		t.Fatalf("ResolveInvocation: %v", err)
	}
	if args := opencodeArgs(binding, Request{}, inv, true); slices.Contains(args, "--pure") {
		t.Fatalf("managed args disable Ralph's reviewed plugin: %v", args)
	}
}

func TestResolveManagedOpencodeLaunchUsesVerifiedAbsoluteWrapper(t *testing.T) {
	target := installOpencodeLaunchTestBundle(t)
	realBinary := filepath.Join(t.TempDir(), "opencode")
	if err := os.WriteFile(realBinary, []byte("#!/bin/sh\n"), 0o700); err != nil {
		t.Fatalf("write real OpenCode fixture: %v", err)
	}
	legacyState := filepath.Join(t.TempDir(), "legacy-opencode-state")
	binding := Binding{Name: "opencode", Config: BindingConfig{
		Type: "opencode", Binary: "opencode", WritePaths: []string{legacyState},
	}}
	req := Request{UserPrompt: "managed prompt", ManagedSessionID: "session", HookEndpoint: "/socket"}
	inv, err := ResolveInvocation(binding, req)
	if err != nil {
		t.Fatalf("ResolveInvocation: %v", err)
	}
	launch, err := resolveOpencodeLaunch(
		binding, req, inv, 300*time.Millisecond,
		func(key string) string {
			if key == adapters.AdapterRootEnv {
				return target
			}
			return ""
		},
		func(name string) (string, error) {
			if name != "opencode" {
				t.Fatalf("lookPath(%q), want opencode", name)
			}
			return realBinary, nil
		},
	)
	if err != nil {
		t.Fatalf("resolveOpencodeLaunch: %v", err)
	}
	defer launch.cleanup()
	bundle, err := adapters.ResolveCurrentBundle(target)
	if err != nil {
		t.Fatalf("ResolveCurrentBundle: %v", err)
	}
	if launch.command != bundle.Executable || !filepath.IsAbs(launch.command) {
		t.Fatalf("managed command = %q, want absolute %q", launch.command, bundle.Executable)
	}
	wantPrefix := []string{
		"hook", "launch-opencode", "--binary", realBinary,
		"--adapter-root", bundle.Target,
		"--runtime-root", launch.runtimeRoot,
		"--verification-progress-interval", "100ms", "--",
	}
	if len(launch.args) < len(wantPrefix) || !reflect.DeepEqual(launch.args[:len(wantPrefix)], wantPrefix) {
		t.Fatalf("managed args prefix = %v, want %v", launch.args, wantPrefix)
	}
	if slices.Contains(launch.args, "--pure") {
		t.Fatalf("managed args disable the reviewed plugin: %v", launch.args)
	}
	wantContainmentWritePaths := []string{
		filepath.Join(launch.runtimeRoot, "home"), filepath.Join(launch.runtimeRoot, "config"),
	}
	if !reflect.DeepEqual(launch.containmentWritePaths, wantContainmentWritePaths) {
		t.Fatalf("managed containment paths = %v, want exact ordered paths %v",
			launch.containmentWritePaths, wantContainmentWritePaths)
	}
	for _, forbidden := range []string{
		bundle.Target, bundle.Root, bundle.OpenCodeRuntimeDir, filepath.Dir(bundle.Root),
	} {
		if slices.Contains(launch.containmentWritePaths, forbidden) {
			t.Fatalf("managed containment paths include broad release path %q: %v",
				forbidden, launch.containmentWritePaths)
		}
	}
	if slices.Contains(launch.containmentWritePaths, legacyState) {
		t.Fatalf("managed containment retained legacy real-state grant %q: %v",
			legacyState, launch.containmentWritePaths)
	}
}

func TestResolveUnmanagedOpencodeLaunchIsDirectAndPure(t *testing.T) {
	legacyState := filepath.Join(t.TempDir(), "legacy-opencode-state")
	binding := Binding{Name: "opencode", Config: BindingConfig{
		Type: "opencode", Binary: "opencode", WritePaths: []string{legacyState},
	}}
	req := Request{UserPrompt: "ordinary prompt"}
	inv, err := ResolveInvocation(binding, req)
	if err != nil {
		t.Fatalf("ResolveInvocation: %v", err)
	}
	launch, err := resolveOpencodeLaunch(
		binding, req, inv, time.Second,
		func(string) string { panic("unmanaged launch read adapter environment") },
		func(string) (string, error) { panic("unmanaged launch resolved wrapper path") },
	)
	if err != nil {
		t.Fatalf("resolveOpencodeLaunch: %v", err)
	}
	if launch.command != "opencode" || !slices.Contains(launch.args, "--pure") {
		t.Fatalf("unmanaged launch = %q %v", launch.command, launch.args)
	}
	if !reflect.DeepEqual(launch.containmentWritePaths, []string{legacyState}) {
		t.Fatalf("unmanaged launch paths = %v, want declared %q",
			launch.containmentWritePaths, legacyState)
	}
}

func TestOpencodeVerificationProgressIntervalFailsClosedForImpossibleLease(t *testing.T) {
	for _, stall := range []time.Duration{
		-time.Second, 0, time.Nanosecond, 2 * time.Nanosecond, 299 * time.Millisecond,
	} {
		if interval, err := opencodeVerificationProgressInterval(stall); err == nil || interval != 0 {
			t.Fatalf("stall %s resolved to (%s, %v), want fail-closed error", stall, interval, err)
		}
	}
	if interval, err := opencodeVerificationProgressInterval(300 * time.Millisecond); err != nil ||
		interval != 100*time.Millisecond {
		t.Fatalf("300ms stall resolved to (%s, %v), want 100ms", interval, err)
	}
}

func TestResolveManagedOpencodeLaunchRejectsTamperedBundleBeforeBinaryLookup(t *testing.T) {
	target := installOpencodeLaunchTestBundle(t)
	bundle, err := adapters.ResolveCurrentBundle(target)
	if err != nil {
		t.Fatalf("ResolveCurrentBundle: %v", err)
	}
	if err := os.WriteFile(bundle.OpenCodePlugin, []byte("tampered\n"), 0o600); err != nil {
		t.Fatalf("tamper plugin: %v", err)
	}
	binding := Binding{Name: "opencode", Config: BindingConfig{Type: "opencode", Binary: "opencode"}}
	req := Request{ManagedSessionID: "session", HookEndpoint: "/socket"}
	inv, err := ResolveInvocation(binding, req)
	if err != nil {
		t.Fatalf("ResolveInvocation: %v", err)
	}
	_, err = resolveOpencodeLaunch(
		binding, req, inv, time.Second,
		func(key string) string {
			if key == adapters.AdapterRootEnv {
				return target
			}
			return ""
		},
		func(string) (string, error) {
			t.Fatal("tampered bundle reached real binary lookup")
			return "", errors.New("unreachable")
		},
	)
	if err == nil || !strings.Contains(err.Error(), "managed OpenCode adapter unavailable") {
		t.Fatalf("tampered bundle error = %v", err)
	}
}

func installOpencodeLaunchTestBundle(t *testing.T) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("adapter installation is unsupported on native Windows")
	}
	source := filepath.Join(t.TempDir(), "radioactive_ralph")
	if err := os.WriteFile(source, []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
		t.Fatalf("write Ralph fixture: %v", err)
	}
	target := t.TempDir()
	if _, err := adapters.Install(source, target); err != nil {
		t.Fatalf("Install: %v", err)
	}
	return target
}

// TestOpencodeKeepsItsStructuredOutputContract guards the flags the runner's
// parsing depends on.
func TestOpencodeKeepsItsStructuredOutputContract(t *testing.T) {
	args := opencodeTestArgs()
	for _, required := range []string{"run", "--format", "json"} {
		if !slices.Contains(args, required) {
			t.Errorf("args lost %s: %v", required, args)
		}
	}
}
