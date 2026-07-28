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
func applyContainment(containmentRoot, bin string, args []string) (string, []string, error) {
	if containmentRoot == "" {
		return bin, args, nil
	}
	policy, err := contain.NewPolicy(containmentRoot)
	if err != nil {
		return "", nil, err
	}
	return policy.Wrap(bin, args)
}
