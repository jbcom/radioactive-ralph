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
	if req.ManagedSessionID == "" && req.HookEndpoint == "" {
		return nil
	}
	prefixes := []string{adapters.ManagedSessionEnv + "=", adapters.HookEndpointEnv + "="}
	env := make([]string, 0, len(os.Environ())+2)
	for _, entry := range os.Environ() {
		if strings.HasPrefix(entry, prefixes[0]) || strings.HasPrefix(entry, prefixes[1]) {
			continue
		}
		env = append(env, entry)
	}
	env = append(env,
		adapters.ManagedSessionEnv+"="+req.ManagedSessionID,
		adapters.HookEndpointEnv+"="+req.HookEndpoint,
	)
	return env
}
