package provider

import (
	"github.com/jbcom/radioactive-ralph/internal/contain"
)

// applyContainment rewrites a command so it runs under a kernel-enforced write
// boundary rooted at containmentRoot.
//
// An empty root returns the command unchanged: containment is opt-in per
// request, so every existing caller behaves exactly as before.
//
// A non-empty root that CANNOT be honored is an error, not a silent
// pass-through. A caller that asked for containment and got an unconfined
// process would hold precisely the false guarantee this exists to replace, and
// the provider would look contained while writing anywhere.
// extraWritable are directories outside the root the bound CLI must be able to
// write for a contained turn to START -- codex's app-server directory, for
// example. They come from the binding's own capability record and are measured,
// never guessed. An empty slice leaves the boundary exactly as it was.
func applyContainment(containmentRoot string, extraWritable []string, bin string, args []string) (string, []string, error) {
	if containmentRoot == "" {
		return bin, args, nil
	}
	policy, err := contain.NewPolicy(containmentRoot, extraWritable...)
	if err != nil {
		return "", nil, err
	}
	return policy.Wrap(bin, args)
}
