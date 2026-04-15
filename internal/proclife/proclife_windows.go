//go:build windows

package proclife

import (
	"fmt"
	"os/exec"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// attach sets CREATE_SUSPENDED so PostStart can bind the child to a
// Job Object before the child runs its first instruction. Without
// this race-free setup, a fast-exiting child could miss the Job
// Object entirely.
func attach(cmd *exec.Cmd) error {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.CreationFlags |= windows.CREATE_SUSPENDED
	return nil
}

// postStart creates a Job Object with JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE,
// assigns the suspended child process to it, then resumes the main
// thread. When this Go process exits (any way — SIGKILL, crash,
// clean), the kernel releases the Job Object handle and terminates
// every process in the job.
func postStart(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return fmt.Errorf("proclife: cmd not started")
	}

	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return fmt.Errorf("proclife: CreateJobObject: %w", err)
	}

	info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	info.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE

	if _, err := windows.SetInformationJobObject(
		job,
		windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)), //nolint:gosec // official Win32 pattern
		uint32(unsafe.Sizeof(info)),    //nolint:gosec // fixed-size struct
	); err != nil {
		_ = windows.CloseHandle(job)
		return fmt.Errorf("proclife: SetInformationJobObject: %w", err)
	}

	// Open the child's process handle with full access so we can
	// both assign-to-job and resume its main thread. cmd.Process.Pid
	// is the only handle Go exposes; we re-open via OpenProcess.
	processHandle, err := windows.OpenProcess(
		windows.PROCESS_ALL_ACCESS,
		false,
		uint32(cmd.Process.Pid),
	)
	if err != nil {
		_ = windows.CloseHandle(job)
		return fmt.Errorf("proclife: OpenProcess: %w", err)
	}
	defer windows.CloseHandle(processHandle)

	if err := windows.AssignProcessToJobObject(job, processHandle); err != nil {
		_ = windows.CloseHandle(job)
		return fmt.Errorf("proclife: AssignProcessToJobObject: %w", err)
	}

	// Resume the suspended main thread so the child actually runs.
	// cmd.Start with CREATE_SUSPENDED leaves the first thread
	// suspended; Go's os/exec doesn't expose a ResumeThread helper,
	// so we reach into the Thread list via NtResumeProcess's
	// userland equivalent — for simplicity, use a fresh thread
	// enumeration via CreateToolhelp32Snapshot.
	if err := resumeAllThreads(cmd.Process.Pid); err != nil {
		_ = windows.CloseHandle(job)
		return fmt.Errorf("proclife: resumeAllThreads: %w", err)
	}

	// Intentionally hold `job` open for process lifetime. When this
	// Go process exits, the OS reaps the handle and KILL_ON_JOB_CLOSE
	// fires — the child dies with us.
	return nil
}

// resumeAllThreads walks the child's thread list and resumes every
// one. Required because we started the child with CREATE_SUSPENDED.
func resumeAllThreads(pid int) error {
	snapshot, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPTHREAD, 0)
	if err != nil {
		return fmt.Errorf("CreateToolhelp32Snapshot: %w", err)
	}
	defer windows.CloseHandle(snapshot)

	var te windows.ThreadEntry32
	te.Size = uint32(unsafe.Sizeof(te)) //nolint:gosec // fixed-size struct
	if err := windows.Thread32First(snapshot, &te); err != nil {
		return fmt.Errorf("Thread32First: %w", err)
	}

	for {
		if te.OwnerProcessID == uint32(pid) {
			th, err := windows.OpenThread(windows.THREAD_SUSPEND_RESUME, false, te.ThreadID)
			if err == nil {
				_, _ = windows.ResumeThread(th)
				_ = windows.CloseHandle(th)
			}
		}
		if err := windows.Thread32Next(snapshot, &te); err != nil {
			// ERROR_NO_MORE_FILES is the clean end-of-iteration marker.
			if err == syscall.ERROR_NO_MORE_FILES {
				return nil
			}
			return fmt.Errorf("Thread32Next: %w", err)
		}
	}
}
