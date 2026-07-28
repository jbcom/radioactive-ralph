//go:build darwin

package contain

import (
	"fmt"
	"os/exec"
)

// sandboxExec is macOS's Seatbelt entry point. It applies a policy to the
// process AND everything it spawns, which is what makes it usable here: a
// fan-out provider runs its own sub-agents, so a boundary that held only for
// the top-level process would be escaped by exactly the providers Ralph runs.
const sandboxExec = "/usr/bin/sandbox-exec"

// denyWritesOutsideRoot is a Seatbelt profile: allow everything by default,
// then deny all writes, then re-allow them beneath the root.
//
// Default-allow is deliberate and NOT laziness. A default-deny profile would
// have to enumerate every read, network call, and mach lookup a provider CLI
// needs — an open-ended list that differs per provider and per version, and
// whose first omission looks like a provider bug rather than a policy one.
// This package's guarantee is scoped to WRITES, so the profile denies exactly
// that and nothing else. Reads are already covered by the validation layer;
// network egress is out of scope and named as such in the design doc.
//
// There is deliberately NO temp-directory exception. An earlier draft granted
// the resolved TMPDIR so scratch files would work, and that single line
// silently re-opened the boundary: on macOS TMPDIR resolves under /private/tmp,
// so "allow writes to the temp dir" allowed writes to a subtree containing
// other users' and other tools' files. The behavioral test caught it — an
// escape target in that subtree wrote successfully while the policy claimed to
// contain it. A convenience grant that widens the boundary is worse than no
// containment, because it reports success.
//
// A provider needing scratch space writes it under the project root, which is
// where a task's work belongs anyway. Verified: a compiled Go binary runs and
// writes inside the root under this profile. (`go version` itself fails,
// because the Go TOOLCHAIN wants its build cache — that is the toolchain, not
// a provider CLI, and Ralph does not run it inside containment.)
//
// The /dev exceptions are not a hole: they keep stdio and the pty working and
// cannot modify the checkout or the user's files.
const denyWritesOutsideRoot = `(version 1)
(allow default)
(deny file-write*)
(allow file-write* (subpath (param "ROOT")))
(allow file-write-data (literal "/dev/null") (literal "/dev/dtracehelper"))
(allow file-write* (regex #"^/dev/tty"))
`

func wrapCommand(p Policy, name string, args []string) (string, []string, error) {
	if !available() {
		return "", nil, ErrContainmentUnavailable
	}
	// -D binds a parameter the profile references. Passing the root as a
	// PARAMETER rather than interpolating it into the profile text keeps a path
	// containing quotes or parens from changing the policy's meaning.
	// Each declared allowance becomes its own numbered parameter, and the
	// profile gains one (allow file-write* (subpath (param "EXTRA_N"))) line per
	// grant. Parameters rather than interpolation for the same reason ROOT uses
	// one: a path containing quotes or parens would otherwise rewrite the
	// profile's meaning rather than widen it by one subpath.
	profile := denyWritesOutsideRoot
	params := make([]string, 0, 2*len(p.ExtraWritable))
	for i, extra := range p.ExtraWritable {
		key := fmt.Sprintf("EXTRA_%d", i)
		profile += fmt.Sprintf("(allow file-write* (subpath (param %q)))\n", key)
		params = append(params, "-D", key+"="+extra)
	}

	wrapped := make([]string, 0, 6+len(params)+len(args))
	wrapped = append(wrapped,
		"-p", profile,
		"-D", "ROOT="+p.Root,
	)
	wrapped = append(wrapped, params...)
	wrapped = append(wrapped, "--", name)
	return sandboxExec, append(wrapped, args...), nil
}

func available() bool {
	if _, err := exec.LookPath(sandboxExec); err != nil {
		return false
	}
	return true
}
