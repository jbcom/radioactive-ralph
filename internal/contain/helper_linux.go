//go:build linux

package contain

func isHelperInvocation(argv []string) (string, []string, []string, bool) {
	return parseHelperInvocation(argv)
}

func runHelper(root string, extra []string, command []string) error {
	return RunHelperWithExtras(root, extra, command)
}
