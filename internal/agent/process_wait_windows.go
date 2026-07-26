//go:build windows

package agent

// Windows reports TerminateProcess through an exit code rather than a Unix
// signal status. The stable process handle still proves whether termination
// was requested.
func processWaitWasForced(_ error, terminationRequested bool) bool {
	return terminationRequested
}
