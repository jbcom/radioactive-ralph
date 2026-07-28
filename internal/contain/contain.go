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
	"strings"
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

	// ExtraWritable are additional absolute, symlink-resolved subpaths the
	// provider may write under, declared by its binding because the CLI cannot
	// start without them.
	//
	// Measured, never guessed: codex fails to initialize its app-server without
	// $HOME/.codex, and opencode cannot open its own log without
	// $HOME/.local/share/opencode. Each entry belongs to ONE provider's state
	// directory.
	//
	// Kept narrow on purpose. A blanket $HOME grant also satisfies both and
	// would make containment vacuous -- the same failure that got TMPDIR
	// removed from the darwin allow-set, since on macOS it resolves under
	// /private/tmp and re-opened the boundary wholesale.
	ExtraWritable []string
}

// ErrExtraPathNotAbsolute rejects a relative allowance: it would resolve
// against whatever cwd the provider happens to have, so the granted boundary
// would depend on where the turn runs.
var ErrExtraPathNotAbsolute = errors.New("contain: extra writable path is not absolute")

// ErrExtraPathTooBroad rejects an allowance that swallows the boundary.
//
// Granting "/" or the whole home directory satisfies every provider and
// destroys the point of containment. The allowance exists for a CLI's own state
// directory; anything wide enough to contain the home directory is an opt-out
// wearing the shape of a grant.
var ErrExtraPathTooBroad = errors.New("contain: extra writable path is too broad")

// NewPolicy resolves root and any declared extra writable paths into a policy.
func NewPolicy(root string, extra ...string) (Policy, error) {
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
	policy := Policy{Root: resolved}
	for _, raw := range extra {
		grant, err := resolveExtraWritable(raw)
		if err != nil {
			return Policy{}, err
		}
		policy.ExtraWritable = append(policy.ExtraWritable, grant)
	}
	return policy, nil
}

// resolveExtraWritable validates and resolves one declared allowance.
//
// A path that does not exist yet is NOT an error: a provider's state directory
// may be created on first run, and refusing would make containment depend on
// whether the CLI had ever been used. It is resolved when possible and taken
// as-is otherwise, having already been checked for absoluteness and breadth.
func resolveExtraWritable(raw string) (string, error) {
	if !filepath.IsAbs(raw) {
		return "", fmt.Errorf("%w: %q", ErrExtraPathNotAbsolute, raw)
	}
	cleaned := filepath.Clean(raw)
	if cleaned == string(filepath.Separator) {
		return "", fmt.Errorf("%w: %q grants the entire filesystem", ErrExtraPathTooBroad, raw)
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		resolvedHome, herr := filepath.EvalSymlinks(home)
		if herr != nil {
			resolvedHome = filepath.Clean(home)
		}
		// The home directory ITSELF (or any ancestor of it) is too broad. A
		// SUBPATH of home is exactly what these allowances are for.
		if cleaned == filepath.Clean(home) || cleaned == resolvedHome ||
			isAncestor(cleaned, resolvedHome) {
			return "", fmt.Errorf("%w: %q contains the home directory", ErrExtraPathTooBroad, raw)
		}
	}
	if resolved, err := filepath.EvalSymlinks(cleaned); err == nil {
		return resolved, nil
	}
	return cleaned, nil
}

// isAncestor reports whether dir contains target.
func isAncestor(dir, target string) bool {
	rel, err := filepath.Rel(dir, target)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && rel != "."
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
