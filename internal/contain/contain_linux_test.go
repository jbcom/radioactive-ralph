//go:build linux

package contain

import "testing"

// TestHandledMaskExcludesReadAndExec is the regression for the bug that cost
// the most time building this: a mask written as "all write bits" (0x7fe)
// silently included READ_FILE (bit 2) and READ_DIR (bit 3).
//
// Landlock denies a HANDLED right everywhere no rule grants it. Handling
// READ_FILE while granting it only under the root therefore made every file
// outside the root unreadable — including the provider binary and the dynamic
// loader — so execve failed with EACCES. The symptom looks like an exec or
// permissions problem and says nothing about reads, which is exactly why an
// assertion on the MASK is worth more than another end-to-end test.
func TestHandledMaskExcludesReadAndExec(t *testing.T) {
	const forbidden = fsExecute | fsReadFile | fsReadDir | fsIoctlDev
	if got := fsWriteBits & forbidden; got != 0 {
		t.Fatalf("handled mask %#x handles read/exec rights %#x; an unhandled "+
			"right is never enforced, and handling these denies reading the "+
			"provider binary and the loader, breaking execve with EACCES",
			fsWriteBits, got)
	}
}

// TestHandledMaskStaysWithinTheSupportedABI keeps create_ruleset from failing
// with EINVAL on a bit the running kernel does not define.
func TestHandledMaskStaysWithinTheSupportedABI(t *testing.T) {
	if fsWriteBits&^fsABI6Bits != 0 {
		t.Fatalf("handled mask %#x exceeds the ABI-6 right bits %#x; "+
			"create_ruleset returns EINVAL on an undefined bit",
			fsWriteBits, fsABI6Bits)
	}
}

// TestHandledMaskCoversEveryMutatingRight is the other direction: a write right
// left OUT of the mask is a hole, because Landlock only enforces what it
// handles. This fails if a new mutating right is added to the constants above
// without being handled.
func TestHandledMaskCoversEveryMutatingRight(t *testing.T) {
	mutating := map[string]uint64{
		"WRITE_FILE": fsWriteFile, "REMOVE_DIR": fsRemoveDir,
		"REMOVE_FILE": fsRemoveFile, "MAKE_CHAR": fsMakeChar,
		"MAKE_DIR": fsMakeDir, "MAKE_REG": fsMakeReg,
		"MAKE_SOCK": fsMakeSock, "MAKE_FIFO": fsMakeFifo,
		"MAKE_BLOCK": fsMakeBlock, "MAKE_SYM": fsMakeSym,
		"REFER": fsRefer, "TRUNCATE": fsTruncate,
	}
	for name, bit := range mutating {
		if fsWriteBits&bit == 0 {
			t.Errorf("%s is a filesystem-mutating right but is not handled; "+
				"Landlock enforces only what it handles, so this is a hole", name)
		}
	}
}

// TestHelperInvocationRoundTrips pins the sentinel contract main() depends on.
func TestHelperInvocationRoundTrips(t *testing.T) {
	root, cmd, ok := IsHelperInvocation([]string{
		"/usr/bin/radioactive_ralph", helperFlag, "/work", "/bin/sh", "-c", "true",
	})
	if !ok {
		t.Fatal("a helper argv was not recognized; the provider would run UNCONTAINED")
	}
	if root != "/work" {
		t.Errorf("root = %q, want /work", root)
	}
	if len(cmd) != 3 || cmd[0] != "/bin/sh" {
		t.Errorf("command = %v, want the full provider argv", cmd)
	}

	// A normal invocation must NOT be mistaken for the helper, or Ralph would
	// exec an argument instead of starting.
	if _, _, ok := IsHelperInvocation([]string{"/usr/bin/radioactive_ralph", "--supervisor"}); ok {
		t.Error("a normal invocation was treated as a containment helper")
	}
}
