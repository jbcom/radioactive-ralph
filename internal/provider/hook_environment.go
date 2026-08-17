package provider

import (
	"os"
	"runtime"
	"strings"

	"github.com/jbcom/radioactive-ralph/internal/adapters"
)

// managedHookEnvironment preserves the ordinary inherited process environment
// while replacing only Ralph's two non-secret hook coordinates. It does not
// invoke a login shell or serialize an environment snapshot.
func managedHookEnvironment(req Request) []string {
	return managedHookEnvironmentFromGOOS(req, os.Environ(), runtime.GOOS)
}

func managedHookEnvironmentFrom(req Request, inherited []string) []string {
	return managedHookEnvironmentFromGOOS(req, inherited, runtime.GOOS)
}

func managedHookEnvironmentFromGOOS(req Request, inherited []string, goos string) []string {
	env := make([]string, 0, len(inherited)+2)
	removed := false
	for _, entry := range inherited {
		key, _, _ := strings.Cut(entry, "=")
		managedKey := key == adapters.ManagedSessionEnv || key == adapters.HookEndpointEnv
		if goos == "windows" {
			managedKey = strings.EqualFold(key, adapters.ManagedSessionEnv) ||
				strings.EqualFold(key, adapters.HookEndpointEnv)
		}
		if managedKey {
			removed = true
			continue
		}
		env = append(env, entry)
	}
	managed := req.ManagedSessionID != "" && req.HookEndpoint != ""
	if !managed {
		// nil is exec.Cmd's established "inherit exactly" contract. Preserve it
		// only when there are no stale Ralph coordinates to remove.
		if !removed {
			return nil
		}
		return env
	}
	env = append(env,
		adapters.ManagedSessionEnv+"="+req.ManagedSessionID,
		adapters.HookEndpointEnv+"="+req.HookEndpoint,
	)
	return env
}
