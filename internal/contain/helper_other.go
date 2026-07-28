//go:build !linux

package contain

// Only the Linux path re-execs a helper: macOS wraps the command with
// sandbox-exec, which needs no cooperation from the wrapped binary, and other
// platforms have no primitive at all. So a helper argv can never legitimately
// appear here.
func isHelperInvocation([]string) (string, []string, []string, bool) { return "", nil, nil, false }

func runHelper(string, []string, []string) error { return ErrContainmentUnavailable }
