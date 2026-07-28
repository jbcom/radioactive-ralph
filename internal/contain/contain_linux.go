//go:build linux

package contain

import (
	"fmt"
	"math"
	"os"
	"strconv"
	"syscall"
	"unsafe"

	"golang.org/x/sys/unix"
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

// fsBitsThroughABI maps a Landlock ABI version to the filesystem rights it
// defines, so the handled set is capped by what the RUNNING kernel knows.
//
// This matters below ABI 5: REFER arrived in ABI 2 and TRUNCATE in ABI 3, so a
// kernel at ABI 1 rejects a ruleset containing them with EINVAL — and the
// failure lands on create_ruleset or on the /dev rule, neither of which
// mentions the offending bit. Capping is the kernel's own documented
// best-effort pattern rather than a workaround.
func fsBitsThroughABI(abi uintptr) uint64 {
	switch {
	case abi >= 5:
		return fsABI6Bits // IOCTL_DEV (bit 15) and everything below
	case abi == 4:
		return 0x7FFF // through TRUNCATE (bit 14)
	case abi == 3:
		return 0x7FFF // TRUNCATE added
	case abi == 2:
		return 0x3FFF // REFER added (bit 13)
	default:
		return 0x1FFF // ABI 1: through MAKE_SYM (bit 12)
	}
}

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
	wrapped := make([]string, 0, 4+len(p.ExtraWritable)+len(args))
	wrapped = append(wrapped, helperFlag, p.Root, strconv.Itoa(len(p.ExtraWritable)))
	wrapped = append(wrapped, p.ExtraWritable...)
	wrapped = append(wrapped, name)
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
	r, _, cmd, k := parseHelperInvocation(argv)
	return r, cmd, k
}

// parseHelperInvocation splits a helper argv into root, extra writable paths,
// and the command to exec.
//
// The extras are length-PREFIXED rather than delimited, because a path is
// arbitrary text: any sentinel token could legitimately appear as a directory
// name and would silently truncate the grant list, turning a contained turn
// into one with the wrong boundary. A count cannot collide with its own data.
func parseHelperInvocation(argv []string) (root string, extra []string, command []string, ok bool) {
	if len(argv) < 5 || argv[1] != helperFlag {
		return "", nil, nil, false
	}
	root = argv[2]
	n, err := strconv.Atoi(argv[3])
	if err != nil || n < 0 || len(argv) < 4+n+1 {
		return "", nil, nil, false
	}
	extra = argv[4 : 4+n]
	command = argv[4+n:]
	if len(command) == 0 {
		return "", nil, nil, false
	}
	return root, extra, command, true
}

// RunHelper applies the containment policy to the current process and execs
// command. It never returns on success.
func RunHelper(root string, command []string) error {
	return RunHelperWithExtras(root, nil, command)
}

// RunHelperWithExtras is RunHelper with additional writable subtrees, declared
// by the provider's binding because its CLI cannot start without them.
func RunHelperWithExtras(root string, extra []string, command []string) error {
	if len(command) == 0 {
		return fmt.Errorf("contain: helper invoked with no command")
	}
	if err := restrictSelfToRoot(root, extra...); err != nil {
		return err
	}
	// syscall.Exec replaces this image; the Landlock domain is inherited.
	// G204: launching a caller-supplied command is the ENTIRE PURPOSE of this
	// helper — it exists to exec the provider CLI under containment. The argv
	// comes from Ralph's own resolved binding, not from provider output, and it
	// is passed as a slice (no shell), so there is nothing to inject into.
	if err := syscall.Exec(command[0], command, os.Environ()); err != nil { //nolint:gosec // see above
		return fmt.Errorf("contain: exec %s under containment: %w", command[0], err)
	}
	return nil
}

// restrictSelfToRoot builds and enforces the ruleset.
func restrictSelfToRoot(root string, extra ...string) error {
	abi, _, errno := syscall.Syscall(
		sysLandlockCreateRuleset, 0, 0, landlockCreateRulesetVersion)
	if errno != 0 || abi < 1 {
		return ErrContainmentUnavailable
	}

	// Clamp to rights the running kernel defines. Per the kernel's documented
	// best-effort pattern, an unsupported bit must be dropped rather than
	// passed — create_ruleset returns EINVAL on an unknown bit.
	handled := fsWriteBits & fsBitsThroughABI(abi)
	attr := landlockRulesetAttr{HandledAccessFS: handled}
	// G103: Landlock has no libc wrapper in Go's syscall package, so the
	// ruleset attr must be passed as a raw pointer. attr is a local struct of
	// fixed-size integers that outlives the call.
	fd, _, errno := syscall.Syscall(sysLandlockCreateRuleset,
		uintptr(unsafe.Pointer(&attr)), unsafe.Sizeof(attr), 0) //nolint:gosec // raw syscall is the only Landlock interface
	if errno != 0 {
		return fmt.Errorf("contain: create landlock ruleset: %w", errno)
	}
	defer func() { _ = syscall.Close(int(fd)) }()

	// The project root is granted full write rights.
	if err := allowWritesBeneath(fd, root, handled); err != nil {
		return err
	}

	// Plus any subtree the bound CLI declared it must write to start -- codex's
	// app-server directory, for instance. Measured per provider and validated by
	// NewPolicy, which refuses "/" and anything containing the home directory so
	// a grant cannot swallow the boundary it is widening.
	for _, path := range extra {
		if err := allowWritesBeneath(fd, path, handled); err != nil {
			return err
		}
	}

	// /dev gets WRITE_FILE ONLY — enough for stdio and the pty, which a provider
	// cannot run without.
	//
	// NOT the full write set: that would include MAKE_CHAR, MAKE_BLOCK,
	// REMOVE_FILE and friends across every /dev descendant, letting a contained
	// provider create device nodes or delete existing ones. The macOS profile
	// already scoped this correctly (write-data on specific nodes plus the tty);
	// granting everything here was an inconsistency in this package, not a
	// requirement.
	if err := allowWritesBeneath(fd, "/dev", fsWriteFile&handled); err != nil {
		return err
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
	// unix.O_PATH, not syscall.O_PATH: the latter is only defined on SOME
	// linux architectures (present on arm64, absent on amd64), so building
	// against it compiles on a dev machine and fails in CI on another arch.
	dirFd, err := unix.Open(dir, unix.O_PATH|unix.O_CLOEXEC, 0)
	if err != nil {
		return fmt.Errorf("contain: open %s for containment rule: %w", dir, err)
	}
	defer func() { _ = syscall.Close(dirFd) }()

	// allowed_access MUST be a subset of handled_access_fs or add_rule returns
	// EINVAL — the kernel's documented masking requirement.
	// G115: the kernel's landlock_path_beneath_attr declares parent_fd as
	// __s32, so the narrowing is required by the ABI rather than incidental. A
	// negative or out-of-range fd cannot be a real descriptor, so it is refused
	// instead of silently wrapping to some other fd number.
	if dirFd < 0 || dirFd > math.MaxInt32 {
		return fmt.Errorf("contain: descriptor for %s (%d) is outside the range "+
			"landlock_path_beneath_attr.parent_fd can represent", dir, dirFd)
	}
	rule := landlockPathBeneathAttr{
		AllowedAccess: handled,
		ParentFd:      int32(dirFd),
	}
	if _, _, errno := syscall.Syscall6(sysLandlockAddRule, rulesetFd,
		landlockRuleTypePathBeneath, uintptr(unsafe.Pointer(&rule)), 0, 0, 0); errno != 0 { //nolint:gosec // raw syscall is the only Landlock interface
		return fmt.Errorf("contain: add landlock rule for %s: %w", dir, errno)
	}
	return nil
}
