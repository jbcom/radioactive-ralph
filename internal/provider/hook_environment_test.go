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
	if got := managedHookEnvironment(Request{}); got != nil {
		t.Fatalf("unmanaged environment = %#v, want nil", got)
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
