package provider

import (
	"strings"
	"testing"

	"github.com/jbcom/radioactive-ralph/internal/adapters"
)

func TestManagedHookEnvironmentReplacesCoordinatesOnce(t *testing.T) {
	t.Setenv(adapters.ManagedSessionEnv, "stale-session")
	t.Setenv(adapters.HookEndpointEnv, "stale-endpoint")
	env, err := managedHookEnvironment(Request{
		ManagedSessionID: "current-session",
		HookEndpoint:     "/tmp/current.sock",
	})
	if err != nil {
		t.Fatalf("managedHookEnvironment: %v", err)
	}
	assertOneEnvironmentValue(t, env, adapters.ManagedSessionEnv, "current-session")
	assertOneEnvironmentValue(t, env, adapters.HookEndpointEnv, "/tmp/current.sock")
}

func TestManagedHookEnvironmentPreservesLegacyNil(t *testing.T) {
	got, err := managedHookEnvironmentFrom(Request{}, []string{"PATH=/usr/bin:/bin"})
	if err != nil || got != nil {
		t.Fatalf("unmanaged environment = %#v, want nil", got)
	}
}

func TestManagedHookEnvironmentStripsInheritedCoordinatesFromUnmanagedTurn(t *testing.T) {
	env, err := managedHookEnvironmentFrom(Request{}, []string{
		"PATH=/usr/bin:/bin",
		adapters.ManagedSessionEnv + "=stale-session",
		adapters.HookEndpointEnv + "=/tmp/stale.sock",
	})
	if err != nil {
		t.Fatalf("managedHookEnvironmentFrom: %v", err)
	}
	if env == nil {
		t.Fatal("filtered environment is nil; agent would inherit stale coordinates")
	}
	if len(env) != 1 || env[0] != "PATH=/usr/bin:/bin" {
		t.Fatalf("filtered environment = %#v", env)
	}
}

func TestManagedHookEnvironmentStripsMixedCaseCoordinatesOnWindows(t *testing.T) {
	env, err := managedHookEnvironmentFromGOOS(Request{}, []string{
		"PATH=C:\\Windows\\System32",
		"ralph_managed_session_id=stale-session",
		"Ralph_Hook_Endpoint=stale-endpoint",
	}, "windows")
	if err != nil {
		t.Fatalf("managedHookEnvironmentFromGOOS: %v", err)
	}
	if env == nil {
		t.Fatal("filtered Windows environment is nil; agent would inherit stale coordinates")
	}
	if len(env) != 1 || env[0] != "PATH=C:\\Windows\\System32" {
		t.Fatalf("filtered Windows environment = %#v", env)
	}
}

func TestManagedHookEnvironmentRejectsPartialCoordinates(t *testing.T) {
	for _, req := range []Request{
		{ManagedSessionID: "session-canary"},
		{HookEndpoint: "endpoint-canary"},
	} {
		env, err := managedHookEnvironmentFrom(req, []string{"PATH=/usr/bin:/bin"})
		if err == nil || env != nil {
			t.Fatalf("partial coordinates: env=%#v err=%v, want static rejection", env, err)
		}
		if strings.Contains(err.Error(), "canary") {
			t.Fatalf("partial-coordinate error echoed value: %v", err)
		}
	}
}

func assertOneEnvironmentValue(t *testing.T, env []string, key, want string) {
	t.Helper()
	prefix := key + "="
	var values []string
	for _, entry := range env {
		if strings.HasPrefix(entry, prefix) {
			values = append(values, strings.TrimPrefix(entry, prefix))
		}
	}
	if len(values) != 1 || values[0] != want {
		t.Fatalf("%s values = %#v, want [%q]", key, values, want)
	}
}
