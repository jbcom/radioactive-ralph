//go:build darwin

package agent

import (
	"errors"
	"fmt"
	"math"
	"runtime"
	"sync"
	"syscall"

	"github.com/ebitengine/purego"
)

const (
	taskAuditTokenFlavor = 15
	machSuccess          = 0
	// task_name_for_pid(2) reports a dead or reaped PID as KERN_FAILURE and a
	// PID outside the valid range as KERN_INVALID_ARGUMENT. Neither sets ESRCH.
	machKernInvalidArgument = 4
	machKernFailure         = 5
)

// errDarwinProcessGone reports that a PID enumerated moments earlier no longer
// names a live process. task_name_for_pid(2) fails with a Mach code rather than
// setting ESRCH, so callers cannot detect an ordinary exit by inspecting errno.
// Cleanup must treat a vanished member as already-reclaimed instead of as a
// cleanup failure: a provider fan-out routinely exits children while the
// session is being torn down.
var errDarwinProcessGone = errors.New("agent: Darwin process is gone")

// darwinAuditToken is audit_token_t from <bsm/audit.h>. The kernel embeds the
// numeric PID and a monotonically changing pidversion in this token, allowing
// proc_signal_with_audittoken to target an exact process execution rather than
// whichever process happens to own a recycled PID.
type darwinAuditToken [8]uint32

func (t darwinAuditToken) euid() uint32 { return t[1] }
func (t darwinAuditToken) pid() int     { return int(t[5]) }

type darwinProcessAPI struct {
	machTaskSelf             func() uint32
	taskNameForPID           func(uint32, int32, *uint32) int32
	taskInfo                 func(uint32, int32, *uint32, *uint32) int32
	machPortDeallocate       func(uint32, uint32) int32
	procSignalWithAuditToken func(*darwinAuditToken, int32) int32
	errnoLocation            func() *int32
}

var (
	loadDarwinProcessAPIOnce sync.Once
	loadedDarwinProcessAPI   darwinProcessAPI
	loadDarwinProcessAPIErr  error
)

func systemDarwinProcessAPI() (darwinProcessAPI, error) {
	loadDarwinProcessAPIOnce.Do(func() {
		loadedDarwinProcessAPI, loadDarwinProcessAPIErr = openDarwinProcessAPI()
	})
	return loadedDarwinProcessAPI, loadDarwinProcessAPIErr
}

func openDarwinProcessAPI() (api darwinProcessAPI, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("agent: bind Darwin process API: %v", recovered)
		}
	}()
	libSystem, err := purego.Dlopen("/usr/lib/libSystem.B.dylib", purego.RTLD_LOCAL|purego.RTLD_NOW)
	if err != nil {
		return darwinProcessAPI{}, fmt.Errorf("agent: load libSystem: %w", err)
	}
	bind := func(target any, symbol string) error {
		address, symbolErr := purego.Dlsym(libSystem, symbol)
		if symbolErr != nil {
			return fmt.Errorf("agent: resolve %s: %w", symbol, symbolErr)
		}
		purego.RegisterFunc(target, address)
		return nil
	}
	for _, symbol := range []struct {
		target any
		name   string
	}{
		{&api.machTaskSelf, "mach_task_self"},
		{&api.taskNameForPID, "task_name_for_pid"},
		{&api.taskInfo, "task_info"},
		{&api.machPortDeallocate, "mach_port_deallocate"},
		{&api.procSignalWithAuditToken, "proc_signal_with_audittoken"},
		{&api.errnoLocation, "__error"},
	} {
		if err := bind(symbol.target, symbol.name); err != nil {
			return darwinProcessAPI{}, err
		}
	}
	return api, nil
}

func (api darwinProcessAPI) auditTokenForPID(pid int) (darwinAuditToken, error) {
	if pid <= 1 {
		return darwinAuditToken{}, fmt.Errorf("agent: refuse audit token for unsafe PID %d", pid)
	}
	if pid > math.MaxInt32 {
		return darwinAuditToken{}, fmt.Errorf("agent: PID %d exceeds Darwin pid_t", pid)
	}
	self := api.machTaskSelf()
	var taskName uint32
	if code := api.taskNameForPID(self, int32(pid), &taskName); code != machSuccess {
		// A member enumerated moments ago can exit before we look it up. That is
		// an ordinary race during teardown, not a cleanup failure, so report it
		// as errDarwinProcessGone and let the caller skip the member. EPERM-like
		// refusals keep surfacing as real errors.
		if code == machKernFailure || code == machKernInvalidArgument {
			return darwinAuditToken{}, fmt.Errorf(
				"%w: task_name_for_pid(%d) failed with Mach code %d", errDarwinProcessGone, pid, code)
		}
		return darwinAuditToken{}, fmt.Errorf("agent: task_name_for_pid(%d) failed with Mach code %d", pid, code)
	}
	defer func() { _ = api.machPortDeallocate(self, taskName) }()

	var token darwinAuditToken
	count := uint32(len(token))
	if code := api.taskInfo(taskName, taskAuditTokenFlavor, &token[0], &count); code != machSuccess {
		// The same exit race as task_name_for_pid above, one call later: the
		// member can die between acquiring its task-name port and reading the
		// token, which leaves a dead port that fails the query. Report it as a
		// vanished member rather than a cleanup failure.
		if code == machKernFailure || code == machKernInvalidArgument {
			return darwinAuditToken{}, fmt.Errorf(
				"%w: TASK_AUDIT_TOKEN for PID %d failed with Mach code %d", errDarwinProcessGone, pid, code)
		}
		return darwinAuditToken{}, fmt.Errorf("agent: TASK_AUDIT_TOKEN for PID %d failed with Mach code %d", pid, code)
	}
	if count != uint32(len(token)) || token.pid() != pid {
		return darwinAuditToken{}, fmt.Errorf("agent: invalid audit token for PID %d", pid)
	}
	return token, nil
}

func (api darwinProcessAPI) signalAuditToken(token darwinAuditToken, signal syscall.Signal) error {
	err := api.signalAuditTokenRaw(token, signal)
	if errors.Is(err, syscall.ESRCH) {
		return nil
	}
	return err
}

func (api darwinProcessAPI) signalAuditTokenRaw(token darwinAuditToken, signal syscall.Signal) error {
	if signal <= 0 || uint64(signal) > math.MaxInt32 {
		return syscall.EINVAL
	}
	// errno is thread-local. Pin these adjacent libSystem calls so a failed
	// proc_signal call and __error refer to the same Darwin thread.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if result := api.procSignalWithAuditToken(&token, int32(signal)); result == 0 {
		return nil
	}
	// Darwin errno values are positive 32-bit integers; syscall.Errno's
	// uintptr representation is wider on every supported Darwin target.
	errno := syscall.Errno(*api.errnoLocation()) //nolint:gosec // checked platform ABI widening
	return errno
}
