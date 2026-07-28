package provider

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestEveryDeclarativeShapeConfinesItsTurn is a BEHAVIORAL guarantee, not a
// signature check: each declarative shape runs a real command that tries to
// write outside the containment root, and the write must fail.
//
// It exists because a signature check would not have caught the bug it covers.
// declarativePlainStdout and declarativeLastMessageFile threaded
// req.ContainmentRoot through runCommandWithStallContained while
// declarativeStreamJSON called runStreamJSONCommand, which had no containment
// parameter at all — so a stream-json binding ran UNCONFINED while the project
// config said otherwise, and every existing test still passed. A shape added
// later gets caught here the moment it forgets, because the escape is measured
// rather than the argument list.
func TestEveryDeclarativeShapeConfinesItsTurn(t *testing.T) {
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		t.Skipf("kernel-enforced containment is implemented for darwin and linux, not %s", runtime.GOOS)
	}

	// The escape target lives OUTSIDE the containment root on purpose: writing
	// inside it must keep working, so the root itself cannot be the assertion.
	outside := t.TempDir()
	escape := filepath.Join(outside, "escaped")

	for _, shape := range []string{
		declarativePlainStdout,
		declarativeLastMessageFile,
		declarativeStreamJSON,
	} {
		t.Run(shape, func(t *testing.T) {
			root := t.TempDir()
			_ = os.Remove(escape)

			binding := Binding{
				Name: "escaper",
				// The committed-config allow-list rejects arbitrary binaries; this
				// is the local.toml override path a real operator uses for a
				// custom CLI, and it is what lets the test drive /bin/sh.
				BinaryFromLocal: true,
				Config: BindingConfig{
					Type:   shape,
					Binary: "/bin/sh",
					Args:   []string{"-c", "printf x > " + escape},
				},
			}
			if shape == declarativeLastMessageFile {
				// This shape reads its answer from a file the CLI writes, so give
				// it one inside the root. The turn still attempts the escape.
				out := filepath.Join(root, "last-message")
				binding.Config.OutputFile = out
				binding.Config.Args = []string{
					"-c", "printf x > " + escape + "; printf hi > " + out,
				}
			}

			_, err := DeclarativeRunner{}.Run(context.Background(), binding, Request{
				WorkingDir:      root,
				ContainmentRoot: root,
				UserPrompt:      "ignored",
			})

			// The turn may fail (the shell cannot write) or succeed while the write
			// itself is refused. Either is confinement; the file appearing is not.
			if _, statErr := os.Stat(escape); statErr == nil {
				t.Fatalf("%s wrote outside the containment root (err from Run: %v) — "+
					"the shape is running unconfined even though ContainmentRoot was set",
					shape, err)
			}
		})
	}
}

// TestStreamJSONFailsClosedOnAnUnusableContainmentRoot pins the fail-CLOSED half
// of the contract. A root that cannot back a policy must abort the turn, never
// launch it unwrapped: silently degrading to an unconfined run is the failure
// mode that makes a security boundary worse than none, because the config claims
// protection that is not there.
func TestStreamJSONFailsClosedOnAnUnusableContainmentRoot(t *testing.T) {
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		t.Skipf("containment is implemented for darwin and linux, not %s", runtime.GOOS)
	}
	// A regular FILE is not a usable root; contain.NewPolicy must reject it.
	file := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatalf("seed file: %v", err)
	}

	_, _, err := runStreamJSONCommand(
		context.Background(), 0, t.TempDir(), file, nil, "/bin/sh", []string{"-c", "true"})
	if err == nil {
		t.Fatal("runStreamJSONCommand accepted an unusable containment root — " +
			"it must fail closed rather than run the turn unconfined")
	}
	if strings.Contains(err.Error(), "start ") {
		t.Fatalf("turn was LAUNCHED before containment was applied: %v", err)
	}
}
