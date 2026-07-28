//go:build darwin

package agent

import (
	"errors"
	"testing"
)

// TestAuditTokenReportsGoneForEveryDeadProcessMachCode pins that a member which
// exits mid-lookup is reported as errDarwinProcessGone from BOTH Mach calls in
// auditTokenForPID, for every status the kernel uses to describe a dead target.
//
// Regression: only task_name_for_pid had gone-handling, and only for
// KERN_INVALID_ARGUMENT(4)/KERN_FAILURE(5). Two gaps followed. First, a member
// could survive task_name_for_pid and then die before the TASK_AUDIT_TOKEN
// query, which failed with an unhandled code. Second, once the target exits the
// task-name port becomes a dead name and the query fails at the IPC layer with
// MACH_SEND_INVALID_DEST(0x10000003), which was not in the gone set at all.
// Either gap turned an ordinary teardown race into a hard cleanup failure,
// wrapping ErrProcessSessionCleanup around the caller's real error.
func TestAuditTokenReportsGoneForEveryDeadProcessMachCode(t *testing.T) {
	deadCodes := map[string]int32{
		"KERN_INVALID_ARGUMENT":  machKernInvalidArgument,
		"KERN_FAILURE":           machKernFailure,
		"MACH_SEND_INVALID_DEST": machSendInvalidDest,
	}

	for name, code := range deadCodes {
		t.Run("task_name_for_pid/"+name, func(t *testing.T) {
			api := darwinProcessAPI{
				machTaskSelf:       func() uint32 { return 1 },
				taskNameForPID:     func(uint32, int32, *uint32) int32 { return code },
				machPortDeallocate: func(uint32, uint32) int32 { return machSuccess },
			}
			_, err := api.auditTokenForPID(4242)
			if !errors.Is(err, errDarwinProcessGone) {
				t.Fatalf("auditTokenForPID = %v, want errDarwinProcessGone", err)
			}
		})

		t.Run("TASK_AUDIT_TOKEN/"+name, func(t *testing.T) {
			// The target survives task_name_for_pid, then dies before the token
			// query — the exact race the original code did not handle.
			api := darwinProcessAPI{
				machTaskSelf:       func() uint32 { return 1 },
				taskNameForPID:     func(uint32, int32, *uint32) int32 { return machSuccess },
				taskInfo:           func(uint32, int32, *uint32, *uint32) int32 { return code },
				machPortDeallocate: func(uint32, uint32) int32 { return machSuccess },
			}
			_, err := api.auditTokenForPID(4242)
			if !errors.Is(err, errDarwinProcessGone) {
				t.Fatalf("auditTokenForPID = %v, want errDarwinProcessGone", err)
			}
		})
	}
}

// TestAuditTokenSurfacesRealFailuresAsErrors keeps the gone-sentinel narrow: a
// permission or kernel error that does NOT mean "the process exited" must still
// surface as a cleanup failure rather than being silently skipped.
func TestAuditTokenSurfacesRealFailuresAsErrors(t *testing.T) {
	const kernProtectionFailure = 2 // KERN_PROTECTION_FAILURE: a real refusal.

	api := darwinProcessAPI{
		machTaskSelf:       func() uint32 { return 1 },
		taskNameForPID:     func(uint32, int32, *uint32) int32 { return machSuccess },
		taskInfo:           func(uint32, int32, *uint32, *uint32) int32 { return kernProtectionFailure },
		machPortDeallocate: func(uint32, uint32) int32 { return machSuccess },
	}
	_, err := api.auditTokenForPID(4242)
	if err == nil {
		t.Fatal("auditTokenForPID = nil, want a real error for KERN_PROTECTION_FAILURE")
	}
	if errors.Is(err, errDarwinProcessGone) {
		t.Fatalf("auditTokenForPID = %v, want a real error, not the gone sentinel", err)
	}
}
