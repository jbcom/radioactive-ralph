//go:build darwin

package proclife

import (
	"os/exec"
	"syscall"
)

// attach puts the child in its own process group and arranges for
// the parent-side kqueue watchdog to deliver SIGTERM to the child
// when the parent exits.
//
// macOS doesn't expose PR_SET_PDEATHSIG. The portable answer is a
// parent-side kqueue registered on the child's pid and a child-side
// kqueue watching the parent — the SupervisorStartWatchdog function
// (called from cmd/radioactive_ralph/supervisor.go inside the
// child after exec) handles the child side. Attach only needs to
// set Setpgid so our signal delivery is predictable.
func attach(cmd *exec.Cmd) error {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
	return nil
}

// postStart is a no-op on darwin — the child's kqueue watchdog is
// set up inside the child by SupervisorStartWatchdog, not here.
func postStart(_ *exec.Cmd) error {
	return nil
}

// SupervisorStartWatchdog is called by the spawned supervisor
// process (child side) to register a kqueue that fires when the
// parent (identified by parentPID) exits. On fire, the callback
// runs and the process should exit cleanly.
//
// Returns an error only on kqueue setup failure. On success, spawns
// a goroutine that holds the kqueue handle for the process lifetime.
func SupervisorStartWatchdog(parentPID int, onParentDeath func()) error {
	kq, err := syscall.Kqueue()
	if err != nil {
		return err
	}

	// EVFILT_PROC + NOTE_EXIT: fires once when parentPID exits.
	changes := []syscall.Kevent_t{{
		Ident:  uint64(parentPID), //nolint:gosec // PID is non-negative
		Filter: syscall.EVFILT_PROC,
		Flags:  syscall.EV_ADD | syscall.EV_ENABLE | syscall.EV_ONESHOT,
		Fflags: noteExit,
	}}

	// Register the change list. The immediate call with 0
	// max-events is a pure registration (no events pulled).
	_, err = syscall.Kevent(kq, changes, nil, nil)
	if err != nil {
		_ = syscall.Close(kq)
		return err
	}

	// Goroutine blocks in Kevent() until the parent exits.
	go func() {
		events := make([]syscall.Kevent_t, 1)
		for {
			n, err := syscall.Kevent(kq, nil, events, nil)
			if err != nil {
				if err == syscall.EINTR {
					continue
				}
				// Unrecoverable kqueue error; release the handle.
				_ = syscall.Close(kq)
				return
			}
			if n > 0 && events[0].Filter == syscall.EVFILT_PROC {
				if onParentDeath != nil {
					onParentDeath()
				}
				_ = syscall.Close(kq)
				return
			}
		}
	}()

	return nil
}

// noteExit mirrors the C NOTE_EXIT constant from <sys/event.h>.
// The stdlib syscall package doesn't re-export it as of Go 1.26,
// so we pin the value here. Defined stable since macOS 10.4.
const noteExit uint32 = 0x80000000
