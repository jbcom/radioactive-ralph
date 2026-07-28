//go:build linux

package contain

import (
	"os"
	"strings"
	"testing"
)

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
//
// The argv now carries a COUNT of extra writable paths between the root and the
// command, because a policy may grant subpaths a provider needs to start. The
// count is length-prefixed rather than delimited: a path is arbitrary text, so
// any sentinel could appear as a directory name and would silently truncate the
// grant list into a WRONG boundary rather than an error.
//
// A zero count is the pre-existing shape, so this test still describes the
// common case -- one root, no extras -- and would fail loudly if the format
// changed again without the callers being updated.
func TestHelperInvocationRoundTrips(t *testing.T) {
	root, cmd, ok := IsHelperInvocation([]string{
		"/usr/bin/radioactive_ralph", helperFlag, "/work", "0", "/bin/sh", "-c", "true",
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

// TestNoArchSpecificSyscallConstants records a portability trap this package
// already hit, and names the rule that avoids it.
//
// syscall.O_PATH is defined on SOME linux architectures and not others — arm64
// has it, amd64 does not. Developing on an arm64 machine, `go build` and the
// whole Linux test suite passed; CI on amd64 failed to compile. The fix is to
// take such constants from golang.org/x/sys/unix, which defines them
// uniformly across arches.
//
// Honest scope: on the arches where the constant is missing, the COMPILER
// already catches this, so the test is not the primary defense — a
// cross-arch `go vet` is. Its value is that the failure it prevents is
// bewildering (a Landlock package failing to build for a file-open flag), and
// this states the rule where the next person editing these syscalls will read
// it.
func TestNoArchSpecificSyscallConstants(t *testing.T) {
	src, err := os.ReadFile("contain_linux.go")
	if err != nil {
		t.Fatalf("read source: %v", err)
	}
	// Only flag real uses, not the explanatory comment naming the trap.
	for _, line := range strings.Split(string(src), "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "//") {
			continue
		}
		for _, bad := range []string{"syscall.O_PATH", "syscall.O_CLOEXEC"} {
			if strings.Contains(trimmed, bad) {
				t.Errorf("%s is not defined on every linux arch; use the "+
					"golang.org/x/sys/unix equivalent: %s", bad, trimmed)
			}
		}
	}
}
