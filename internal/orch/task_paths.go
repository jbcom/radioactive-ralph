package orch

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/jbcom/radioactive-ralph/internal/plan"
)

// ErrTaskPathEscapesProject reports a declared input or output that does not
// resolve to a location inside the project root. Callers distinguish it from an
// I/O fault: containment is a refusal, not a transient failure.
var ErrTaskPathEscapesProject = errors.New("orch: declared path escapes the project root")

// ErrTaskInputPinMismatch reports a declared input whose bytes do not match the
// sha256 the plan pinned it to. Distinct from a containment refusal: the path
// is in scope, its CONTENT is not what the task was written against.
var ErrTaskInputPinMismatch = errors.New("orch: declared input does not match its pinned hash")

// ErrTaskPathUnresolvable reports a path that could not be resolved for a
// reason that is NOT an escape — a symlink loop, a permission error, transient
// I/O. Kept separate because the caller's response differs: a containment
// refusal blocks the task permanently, while a fault may clear on its own and
// must not strand the task.
var ErrTaskPathUnresolvable = errors.New("orch: declared path could not be resolved")

// taskFilesystemDecl is a task's declared filesystem surface, as project-
// relative paths.
type taskFilesystemDecl struct {
	Inputs  []taskInputDecl
	Outputs []string
}

// taskInputDecl is one declared input. An empty SHA256 means the plan declared
// the path's SCOPE but did not pin its content — a meaningful distinction,
// since an unpinned input may legitimately not exist yet.
type taskInputDecl struct {
	Path   string
	SHA256 string
}

// secureProjectPath resolves a project-relative declared path and returns the
// RESOLVED path, or ErrTaskPathEscapesProject if it lands outside root.
//
// Returning the resolved path rather than the candidate is the CWE-22 fix, and
// it is not a detail. Handing back the candidate gives the caller a string that
// was never the thing containment approved: the caller then opens the
// candidate, which re-resolves through whatever the symlink points at at open
// time. Every caller must operate on what was actually checked.
//
// A declared path is always project-relative. An absolute path is refused
// outright rather than honored, so a plan cannot name /etc/passwd as an
// "output" and have it treated as in scope.
//
// # What this does and does not guarantee
//
// This constrains RALPH's own reads and completion checks. It does NOT
// constrain the provider, which is a separate process performing its own
// pathname-based open minutes later. A peer task can replace a directory
// component with a symlink after this returns and before the provider writes;
// no string this function returns travels into that process's syscalls.
//
// So: declared-output containment is best-effort VALIDATION, not a security
// boundary. Ralph's guarantee is that an escaping output is DETECTED at
// completion, not that a provider cannot write outside the project root. A real
// write-side guarantee needs a containment primitive around the provider
// process — sandbox profile, mount namespace, or a brokered file API — which is
// a separate design with its own macOS/Linux/Windows matrix.
func secureProjectPath(root, declared string) (string, error) {
	if declared == "" {
		return "", fmt.Errorf("%w: empty path", ErrTaskPathEscapesProject)
	}
	if isRooted(declared) {
		return "", fmt.Errorf("%w: %q is rooted; declared paths are project-relative",
			ErrTaskPathEscapesProject, declared)
	}

	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		// A root that will not resolve is a FAULT, not an escape: there is
		// nothing to contain against, so no comparison below would mean
		// anything, but nothing escaped either.
		return "", fmt.Errorf("%w: resolve project root: %v", ErrTaskPathUnresolvable, err)
	}

	candidate := filepath.Join(resolvedRoot, filepath.Clean(declared))
	resolved, err := resolveThroughExistingAncestor(candidate)
	if err != nil {
		// Symlink loop, permission error, transient I/O — none of these is an
		// escape. Reporting them as containment would make the caller block the
		// task permanently for a condition that may clear on its own.
		return "", fmt.Errorf("%w: resolve %q: %v", ErrTaskPathUnresolvable, declared, err)
	}
	if !containedIn(resolvedRoot, resolved) {
		return "", fmt.Errorf("%w: %q resolves to %q", ErrTaskPathEscapesProject, declared, resolved)
	}
	return resolved, nil
}

// isRooted reports whether declared names a location from a filesystem root
// rather than from the project directory.
//
// filepath.IsAbs alone is NOT enough, because it is PLATFORM-SPECIFIC: on
// Windows "\etc\passwd" is not absolute (no drive), and on Unix
// "C:\Windows" is not absolute either. A single IsAbs check therefore refuses
// a path on one platform and admits the very same string on the other — and a
// declared path is operator-supplied text that may well have been written on a
// different OS than the one running it. Windows CI caught exactly this.
//
// So every rooted SHAPE is refused on every platform: leading slash or
// backslash (POSIX absolute and Windows root-relative), a drive letter prefix,
// and UNC.
func isRooted(declared string) bool {
	if declared == "" {
		return false
	}
	if filepath.IsAbs(declared) {
		return true
	}
	if declared[0] == '/' || declared[0] == '\\' {
		// Covers POSIX absolute, Windows root-relative, and both UNC spellings.
		return true
	}
	// Drive-relative or drive-absolute: "C:", "C:\x", "C:/x", and even "C:x",
	// which resolves against that drive's current directory rather than ours.
	if len(declared) >= 2 && declared[1] == ':' && isDriveLetter(declared[0]) {
		return true
	}
	return false
}

func isDriveLetter(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

// resolveThroughExistingAncestor resolves symlinks for a path that may not exist
// yet — the normal case for a declared OUTPUT, which by definition has not been
// written at admission time.
//
// It resolves the deepest EXISTING ancestor and re-joins the remaining suffix.
// The suffix is a gap: a symlink planted inside it after this returns is
// invisible here, which is one of the reasons the doc comment on
// secureProjectPath calls this validation rather than a boundary. What it does
// close is the case that actually matters at admission — an ancestor that is
// ALREADY a symlink pointing out of the project, which is how a declared
// "build/out.txt" ends up writing to /etc.
func resolveThroughExistingAncestor(path string) (string, error) {
	remaining := []string{}
	current := path
	for {
		resolved, err := filepath.EvalSymlinks(current)
		if err == nil {
			if len(remaining) == 0 {
				return resolved, nil
			}
			// Re-join in the order the components were peeled off.
			parts := append([]string{resolved}, reversed(remaining)...)
			return filepath.Join(parts...), nil
		}
		if !os.IsNotExist(err) {
			return "", err
		}
		parent := filepath.Dir(current)
		if parent == current {
			// Walked to the filesystem root without finding anything that
			// exists. Nothing to resolve against; fail closed.
			return "", fmt.Errorf("no existing ancestor for %q", path)
		}
		remaining = append(remaining, filepath.Base(current))
		current = parent
	}
}

func reversed(in []string) []string {
	out := make([]string, len(in))
	for i, s := range in {
		out[len(in)-1-i] = s
	}
	return out
}

// containedIn reports whether path is root itself or lies beneath it. The
// separator check is what stops "/project-evil" from matching root "/project".
func containedIn(root, path string) bool {
	if path == root {
		return true
	}
	return strings.HasPrefix(path, root+string(filepath.Separator))
}

// hashContainedFile returns the SHA-256 of path, read through the no-follow
// open and hashed FROM THE FILE HANDLE.
//
// Hashing from the handle rather than re-reading the pathname is the point: the
// bytes hashed are the bytes of the inode that was opened, so a path swapped
// for a symlink after the open cannot change what was pinned.
func hashContainedFile(path string) (string, error) {
	f, err := openContainedFile(path)
	if err != nil {
		return "", fmt.Errorf("orch: open %q: %w", path, err)
	}
	defer func() { _ = f.Close() }()

	digest := sha256.New()
	if _, err := io.Copy(digest, f); err != nil {
		return "", fmt.Errorf("orch: hash %q: %w", path, err)
	}
	return hex.EncodeToString(digest.Sum(nil)), nil
}

// validateTaskFilesystem checks every declared path in one task at admission.
//
// This is a FAST FAIL, not the security boundary — see secureProjectPath. It
// refuses a plan whose declarations are already escaping so the failure lands
// at import with a clear message, rather than mid-run.
func validateTaskFilesystem(root string, decl taskFilesystemDecl) error {
	for _, in := range decl.Inputs {
		resolved, err := secureProjectPath(root, in.Path)
		if err != nil {
			return fmt.Errorf("declared input %q: %w", in.Path, err)
		}
		if in.SHA256 == "" {
			// Declared scope, not a pin. An unpinned input may legitimately not
			// exist yet — a predecessor may still be writing it — so requiring
			// it to be readable here would refuse valid plans.
			continue
		}
		// A pin is a claim about CONTENT, so it has to be checked against the
		// bytes. Reading through the no-follow handle and hashing from the
		// *os.File is what makes the check mean anything: a pathname re-read
		// could hash a different inode than the one containment approved.
		got, err := hashContainedFile(resolved)
		if err != nil {
			return fmt.Errorf("declared input %q: %w: %v", in.Path, ErrTaskInputPinMismatch, err)
		}
		if got != in.SHA256 {
			return fmt.Errorf(
				"declared input %q: %w", in.Path, ErrTaskInputPinMismatch)
		}
	}
	for _, out := range decl.Outputs {
		if _, err := secureProjectPath(root, out); err != nil {
			return fmt.Errorf("declared output %q: %w", out, err)
		}
	}
	return nil
}

// declFromMetadata reads a step's declared filesystem surface out of its
// ralph-task metadata. A step with no metadata declares nothing, which is the
// common case and correctly yields an empty decl.
func declFromMetadata(step plan.Step) taskFilesystemDecl {
	if step.Metadata == nil {
		return taskFilesystemDecl{}
	}
	decl := taskFilesystemDecl{}
	for _, in := range step.Metadata.Inputs {
		// Carry the digest, not just the path: dropping it silently turned every
		// pin into a no-op.
		decl.Inputs = append(decl.Inputs, taskInputDecl{Path: in.Path, SHA256: in.SHA256})
	}
	for _, out := range step.Metadata.Outputs {
		decl.Outputs = append(decl.Outputs, out.Path)
	}
	return decl
}
