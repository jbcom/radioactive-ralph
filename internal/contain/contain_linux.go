//go:build linux

package contain

import (
	"fmt"
	"os"
	"syscall"
	"unsafe"
)

// Landlock syscall numbers. Arch-generic, so no per-architecture table.
const (
	sysLandlockCreateRuleset = 444
	sysLandlockAddRule       = 445
	sysLandlockRestrictSelf  = 446

	landlockRuleTypePathBeneath  = 1
	landlockCreateRulesetVersion = 1

	// prctlSetNoNewPrivs is the prctl OPTION (not a syscall number), required
	// before restrict_self for an unprivileged task.
	prctlSetNoNewPrivs = 38
)

// Landlock filesystem access rights, by bit position.
//
// The numbering matters more than it looks: bit 2 is READ_FILE, not a write
// right. An earlier draft used a "0x7fe = all write bits" mask that silently
// included READ_FILE and READ_DIR, and because Landlock denies a HANDLED right
// everywhere a rule does not grant it, that made every file outside the root
// unreadable — including the provider binary and the dynamic loader. execve
// then failed with EACCES, which looks nothing like a containment bug.
const (
	fsExecute    uint64 = 1 << 0
	fsWriteFile  uint64 = 1 << 1
	fsReadFile   uint64 = 1 << 2
	fsReadDir    uint64 = 1 << 3
	fsRemoveDir  uint64 = 1 << 4
	fsRemoveFile uint64 = 1 << 5
	fsMakeChar   uint64 = 1 << 6
	fsMakeDir    uint64 = 1 << 7
	fsMakeReg    uint64 = 1 << 8
	fsMakeSock   uint64 = 1 << 9
	fsMakeFifo   uint64 = 1 << 10
	fsMakeBlock  uint64 = 1 << 11
	fsMakeSym    uint64 = 1 << 12
	fsRefer      uint64 = 1 << 13
	fsTruncate   uint64 = 1 << 14
	fsIoctlDev   uint64 = 1 << 15
)

// fsWriteBits is the set of rights this package HANDLES: exactly those that
// mutate the filesystem.
//
// READ_FILE, READ_DIR, EXECUTE and IOCTL_DEV are deliberately absent. An
// unhandled right is never enforced, so binaries and the loader stay readable
// and execve keeps working across the re-exec — which is the whole mechanism
// here. Handling a read right instead denies it everywhere outside the root and
// breaks exec, for reasons that surface as a confusing EACCES.
const fsWriteBits = fsWriteFile | fsRemoveDir | fsRemoveFile |
	fsMakeChar | fsMakeDir | fsMakeReg | fsMakeSock | fsMakeFifo |
	fsMakeBlock | fsMakeSym | fsRefer | fsTruncate

// fsABI6Bits bounds the rights defined through ABI 6 (bits 0-15). Passing a bit
// the running kernel does not define makes create_ruleset return EINVAL, so the
// handled set is clamped rather than assumed.
const fsABI6Bits uint64 = 0xFFFF

type landlockRulesetAttr struct {
	HandledAccessFS  uint64
	HandledAccessNet uint64
}

type landlockPathBeneathAttr struct {
	AllowedAccess uint64
	ParentFd      int32
	_             [4]byte
}

// wrapCommand re-invokes THIS binary as a containment helper, which applies the
// Landlock ruleset to itself and then execs the real command.
//
// The indirection is not incidental. Landlock is applied by the process to
// ITSELF, and below ABI 8 there is no LANDLOCK_RESTRICT_SELF_TSYNC, so
// restrict_self binds only the CALLING THREAD. Go's runtime already has threads
// running before any user code, and a thread created before the call is NOT in
// the restricted domain — measured: such a thread writes outside the root
// successfully. Restricting in-process from Go would therefore ship a boundary
// with a hole while reporting containment.
//
// execve is what closes it: it replaces the process image with a single thread,
// and the Landlock domain IS inherited across it. So the helper restricts, then
// immediately execs — nothing survives but the restriction.
func wrapCommand(p Policy, name string, args []string) (string, []string, error) {
	if !available() {
		return "", nil, ErrContainmentUnavailable
	}
	self, err := os.Executable()
	if err != nil {
		return "", nil, fmt.Errorf("contain: locate self for containment helper: %w", err)
	}
	wrapped := make([]string, 0, 3+len(args))
	wrapped = append(wrapped, helperFlag, p.Root, name)
	return self, append(wrapped, args...), nil
}

// available reports whether the running kernel supports Landlock.
func available() bool {
	abi, _, errno := syscall.Syscall(
		sysLandlockCreateRuleset, 0, 0, landlockCreateRulesetVersion)
	return errno == 0 && abi >= 1
}

// helperFlag marks an argv as a containment-helper re-invocation.
//
// Deliberately obscure: a provider argument that happened to match would make
// Ralph exec the wrong thing, so the sentinel is one no CLI would produce.
const helperFlag = "--radioactive-ralph-contain-exec"

// IsHelperInvocation reports whether argv is a containment-helper re-invocation
// and returns the root and the command to run.
//
// main() must call this FIRST, before any other setup: the helper's whole job
// is to restrict and exec, and any work done before that either escapes the
// restriction or is thrown away by the exec.
func IsHelperInvocation(argv []string) (root string, command []string, ok bool) {
	if len(argv) < 4 || argv[1] != helperFlag {
		return "", nil, false
	}
	return argv[2], argv[3:], true
}

// RunHelper applies the containment policy to the current process and execs
// command. It never returns on success.
func RunHelper(root string, command []string) error {
	if len(command) == 0 {
		return fmt.Errorf("contain: helper invoked with no command")
	}
	if err := restrictSelfToRoot(root); err != nil {
		return err
	}
	// syscall.Exec replaces this image; the Landlock domain is inherited.
	if err := syscall.Exec(command[0], command, os.Environ()); err != nil {
		return fmt.Errorf("contain: exec %s under containment: %w", command[0], err)
	}
	return nil
}

// restrictSelfToRoot builds and enforces the ruleset.
func restrictSelfToRoot(root string) error {
	abi, _, errno := syscall.Syscall(
		sysLandlockCreateRuleset, 0, 0, landlockCreateRulesetVersion)
	if errno != 0 || abi < 1 {
		return ErrContainmentUnavailable
	}

	// Clamp to rights the running kernel defines. Per the kernel's documented
	// best-effort pattern, an unsupported bit must be dropped rather than
	// passed — create_ruleset returns EINVAL on an unknown bit.
	handled := fsWriteBits & fsABI6Bits
	attr := landlockRulesetAttr{HandledAccessFS: handled}
	fd, _, errno := syscall.Syscall(sysLandlockCreateRuleset,
		uintptr(unsafe.Pointer(&attr)), unsafe.Sizeof(attr), 0)
	if errno != 0 {
		return fmt.Errorf("contain: create landlock ruleset: %w", errno)
	}
	defer func() { _ = syscall.Close(int(fd)) }()

	// The root is the only writable subtree. /dev is granted the same rights
	// because stdio and the pty live there and a provider that cannot write to
	// its own terminal cannot run; it holds no project or user files.
	for _, dir := range []string{root, "/dev"} {
		if err := allowWritesBeneath(fd, dir, handled); err != nil {
			return err
		}
	}

	if _, _, errno := syscall.Syscall6(
		syscall.SYS_PRCTL, prctlSetNoNewPrivs, 1, 0, 0, 0, 0); errno != 0 {
		return fmt.Errorf("contain: set no_new_privs: %w", errno)
	}
	if _, _, errno := syscall.Syscall(sysLandlockRestrictSelf, fd, 0, 0); errno != 0 {
		return fmt.Errorf("contain: enforce landlock ruleset: %w", errno)
	}
	return nil
}

// allowWritesBeneath grants the handled write rights under dir.
func allowWritesBeneath(rulesetFd uintptr, dir string, handled uint64) error {
	dirFd, err := syscall.Open(dir, syscall.O_PATH|syscall.O_CLOEXEC, 0)
	if err != nil {
		return fmt.Errorf("contain: open %s for containment rule: %w", dir, err)
	}
	defer func() { _ = syscall.Close(dirFd) }()

	// allowed_access MUST be a subset of handled_access_fs or add_rule returns
	// EINVAL — the kernel's documented masking requirement.
	rule := landlockPathBeneathAttr{
		AllowedAccess: handled,
		ParentFd:      int32(dirFd),
	}
	if _, _, errno := syscall.Syscall6(sysLandlockAddRule, rulesetFd,
		landlockRuleTypePathBeneath, uintptr(unsafe.Pointer(&rule)), 0, 0, 0); errno != 0 {
		return fmt.Errorf("contain: add landlock rule for %s: %w", dir, errno)
	}
	return nil
}
