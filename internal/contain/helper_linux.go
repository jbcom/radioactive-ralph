//go:build linux

package contain

func isHelperInvocation(argv []string) (string, []string, bool) {
	return IsHelperInvocation(argv)
}

func runHelper(root string, command []string) error { return RunHelper(root, command) }
