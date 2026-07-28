// Package contain puts a KERNEL-ENFORCED write boundary around a provider
// process.
//
// This is deliberately not what internal/orch's secureProjectPath does. That
// validates path strings Ralph itself handles — which files Ralph will read,
// and which declared outputs Ralph will admit — and #228 added detection at
// completion. None of it constrains the provider: it is a separate process
// opening files by pathname, minutes later, and no string Ralph returns ever
// travels into its syscalls. Validation can say "this task declared an
// escaping path"; only the kernel can say "this process may not write there".
//
// The two layers are complementary. Validation catches the honest mistake
// early, with a good error. Containment is what makes the guarantee true when
// the provider does something the declaration did not describe.
package contain

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// ErrRootNotAbsolute reports a containment root that is not an absolute path.
//
// A relative root resolves against whatever working directory the provider
// inherits, so the boundary would guard a different directory than the caller
// meant — and would report success while doing it.
var ErrRootNotAbsolute = errors.New("contain: containment root must be an absolute path")

// ErrContainmentUnavailable reports a platform with no write-containment
// primitive.
//
// Returned rather than silently passing the command through: a caller that
// believes it is contained when it is not is making exactly the false
// guarantee this package exists to replace.
var ErrContainmentUnavailable = errors.New("contain: no write containment primitive on this platform")

// ErrRootNotDirectory reports a containment root that resolves to something
// other than a directory.
//
// EvalSymlinks resolves a regular file without complaint, and a single file
// cannot represent a writable subtree: macOS would build a subpath rule for a
// non-directory, and Landlock would grant directory-creation rights beneath a
// plain file. Neither is a boundary anyone reasoned about, so this fails closed.
var ErrRootNotDirectory = errors.New("contain: containment root is not a directory")

// Policy is a resolved write boundary: the provider may write beneath Root and
// nowhere else.
type Policy struct {
	// Root is the ABSOLUTE, SYMLINK-RESOLVED directory the provider may write
	// under. Resolved because a policy written against a symlink names the link
	// rather than its target, so a provider writing through the resolved path
	// would land outside a boundary that appears to contain it.
	Root string
}

// NewPolicy resolves root into a containment policy.
func NewPolicy(root string) (Policy, error) {
	if !filepath.IsAbs(root) {
		return Policy{}, fmt.Errorf("%w: %q", ErrRootNotAbsolute, root)
	}
	resolved, err := filepath.EvalSymlinks(root)
	if err != nil {
		return Policy{}, fmt.Errorf("contain: resolve containment root %q: %w", root, err)
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return Policy{}, fmt.Errorf("contain: stat containment root %q: %w", resolved, err)
	}
	if !info.IsDir() {
		return Policy{}, fmt.Errorf("%w: %q", ErrRootNotDirectory, resolved)
	}
	return Policy{Root: resolved}, nil
}

// Wrap rewrites a command so it runs under the policy, returning the name and
// args to execute.
//
// It returns ErrContainmentUnavailable where no primitive exists. Callers
// decide whether that is fatal; this package will not pretend.
func (p Policy) Wrap(name string, args []string) (string, []string, error) {
	if p.Root == "" {
		return "", nil, fmt.Errorf("%w: empty root", ErrRootNotAbsolute)
	}
	return wrapCommand(p, name, args)
}

// Available reports whether this platform can enforce a write boundary.
//
// Callers check it to decide policy — refuse to dispatch, or proceed with the
// weaker validation-only guarantee — rather than discovering at Wrap time.
func Available() bool { return available() }

// MaybeRunHelper handles a containment-helper re-invocation, if this argv is
// one. It returns handled=false for a normal invocation.
//
// main() must call this FIRST, before flags, config, or logging: the helper's
// entire job is to restrict and exec, and any work done before that either
// escapes the restriction or is discarded by the exec.
//
// On success it never returns — the process is replaced by the provider.
func MaybeRunHelper(argv []string) (handled bool, err error) {
	root, command, ok := isHelperInvocation(argv)
	if !ok {
		return false, nil
	}
	return true, runHelper(root, command)
}
