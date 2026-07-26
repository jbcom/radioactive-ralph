//go:build windows

package agent

// Native Windows pty execution is unsupported today. Keep the shared reader
// contract buildable without treating an arbitrary future ConPTY error as EOF.
func isPTYEOF(error) bool {
	return false
}
