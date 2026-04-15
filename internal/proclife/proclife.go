// Package proclife exposes the cross-platform parent-death
// primitives that back up variantpool's lifeline pipe.
//
// The lifeline pipe alone (see internal/variantpool) is portable and
// sufficient — children that honor EOF-on-FD-3 self-terminate when
// the parent dies, regardless of OS. This package adds OS-level
// defenses for children that don't cooperate:
//
//   - Linux: PR_SET_PDEATHSIG via syscall.SysProcAttr.Pdeathsig.
//     Kernel signals the child when the parent exits.
//   - macOS: kqueue NOTE_EXIT on the parent pid. Child registers a
//     watch and exits when the kevent fires.
//   - Windows: Job Objects with
//     JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE. Kernel kills every
//     process in the job when the job handle is released.
//
// Each implementation is a single file behind a go:build tag.
// Callers import this package and call Attach(cmd) before Start.
//
// Attach is best-effort: if the OS primitive is unavailable or
// fails, the function returns nil (the lifeline pipe remains the
// primary safety net, not this). Errors surface only in platform
// tests.
package proclife

import "os/exec"

// Attach configures the (unstarted) command with the strongest
// parent-death primitive available on this OS. Callers must call
// Attach BEFORE cmd.Start().
//
// On POSIX this is a one-liner that extends cmd.SysProcAttr. On
// Windows it's a no-op at Attach-time; the Job Object setup happens
// in a separate step after cmd.Start() on that platform.
func Attach(cmd *exec.Cmd) error {
	return attach(cmd)
}

// PostStart is called after cmd.Start() for platforms that need to
// bind the child to the parent's lifetime at process-handle level
// (Windows). POSIX implementations make it a no-op.
func PostStart(cmd *exec.Cmd) error {
	return postStart(cmd)
}
