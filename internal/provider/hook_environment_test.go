package provider

import (
	"strings"
	"testing"

	"github.com/jbcom/radioactive-ralph/internal/adapters"
)

func TestManagedHookEnvironmentReplacesCoordinatesOnce(t *testing.T) {
	t.Setenv(adapters.ManagedSessionEnv, "stale-session")
	t.Setenv(adapters.HookEndpointEnv, "stale-endpoint")
	env := managedHookEnvironment(Request{
		ManagedSessionID: "current-session",
		HookEndpoint:     "/tmp/current.sock",
	})
	assertOneEnvironmentValue(t, env, adapters.ManagedSessionEnv, "current-session")
	assertOneEnvironmentValue(t, env, adapters.HookEndpointEnv, "/tmp/current.sock")
}

func TestManagedHookEnvironmentPreservesLegacyNil(t *testing.T) {
	if got := managedHookEnvironmentFrom(Request{}, []string{"PATH=/usr/bin:/bin"}); got != nil {
		t.Fatalf("unmanaged environment = %#v, want nil", got)
	}
}

func TestManagedHookEnvironmentStripsInheritedCoordinatesFromUnmanagedTurn(t *testing.T) {
	env := managedHookEnvironmentFrom(Request{}, []string{
		"PATH=/usr/bin:/bin",
		adapters.ManagedSessionEnv + "=stale-session",
		adapters.HookEndpointEnv + "=/tmp/stale.sock",
	})
	if env == nil {
		t.Fatal("filtered environment is nil; agent would inherit stale coordinates")
	}
	if len(env) != 1 || env[0] != "PATH=/usr/bin:/bin" {
		t.Fatalf("filtered environment = %#v", env)
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
