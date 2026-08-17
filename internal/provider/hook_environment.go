package provider

import (
	"os"
	"strings"

	"github.com/jbcom/radioactive-ralph/internal/adapters"
)

// managedHookEnvironment preserves the ordinary inherited process environment
// while replacing only Ralph's two non-secret hook coordinates. It does not
// invoke a login shell or serialize an environment snapshot.
func managedHookEnvironment(req Request) []string {
	return managedHookEnvironmentFrom(req, os.Environ())
}

func managedHookEnvironmentFrom(req Request, inherited []string) []string {
	prefixes := []string{adapters.ManagedSessionEnv + "=", adapters.HookEndpointEnv + "="}
	env := make([]string, 0, len(inherited)+2)
	removed := false
	for _, entry := range inherited {
		if strings.HasPrefix(entry, prefixes[0]) || strings.HasPrefix(entry, prefixes[1]) {
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
