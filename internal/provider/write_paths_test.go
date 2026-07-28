package provider

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"
)

// TestCodexDeclaresItsRequiredWritePath closes the second half of #296.
//
// Codex cannot initialize its app-server without writing to $HOME/.codex, and
// opencode cannot open its own log without $HOME/.local/share/opencode. Both
// were measured by bisecting the sandbox profile one grant at a time, then
// verified with real turns through wrapCommand.
//
// Declaring the path is what turns "cannot be contained" into "contained with
// one narrow allowance". Without it these providers stay permanently spared,
// which is a working outcome but a strictly weaker one: their turns run with
// full write access on a project that asked for a boundary.
func TestCodexDeclaresItsRequiredWritePath(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("no home dir: %v", err)
	}
	cfg := defaultCodexProvider()
	paths := BindingWritePaths(Binding{Config: cfg})
	want := filepath.Join(home, ".codex")
	if !hasPath(paths, want) {
		t.Fatalf("codex declares write paths %v, want it to include %q; without the "+
			"declaration it cannot start under containment and stays permanently "+
			"spared instead of contained", paths, want)
	}
	// And it must now be CAPABLE: the allowance is what makes that true.
	if !supportsContainment(cfg) {
		t.Fatal("codex still declares SupportsContainment=false; with its required " +
			"path declared it runs confined, verified with a real turn")
	}
}

func TestOpencodeDeclaresItsRequiredWritePath(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("no home dir: %v", err)
	}
	cfg := defaultOpencodeProvider()
	paths := BindingWritePaths(Binding{Config: cfg})
	want := filepath.Join(home, ".local", "share", "opencode")
	if !hasPath(paths, want) {
		t.Fatalf("opencode declares write paths %v, want it to include %q", paths, want)
	}
	if !supportsContainment(cfg) {
		t.Fatal("opencode still declares SupportsContainment=false")
	}
}

// TestClaudeDeclaresNoExtraWritePath keeps the allowance from becoming a
// reflex. Claude already completes a real turn confined with no grant at all,
// so declaring one would widen the boundary for nothing.
func TestClaudeDeclaresNoExtraWritePath(t *testing.T) {
	if paths := BindingWritePaths(Binding{Config: defaultClaudeProvider()}); len(paths) != 0 {
		t.Fatalf("claude declares write paths %v, want none: it runs confined "+
			"without any allowance, so granting one widens the boundary for nothing", paths)
	}
}

// TestWritePathsExpandHomeRelativeDeclarations pins that a binding declares its
// path portably. A config cannot hardcode /Users/someone, and an absolute path
// baked into a shipped default would be wrong on every other machine.
func TestWritePathsExpandHomeRelativeDeclarations(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("no home dir: %v", err)
	}
	cfg := BindingConfig{Type: "x", WritePaths: []string{"~/.example"}}
	paths := BindingWritePaths(Binding{Config: cfg})
	want := filepath.Join(home, ".example")
	if !hasPath(paths, want) {
		t.Fatalf("BindingWritePaths(~/.example) = %v, want %q expanded", paths, want)
	}
	for _, p := range paths {
		if strings.HasPrefix(p, "~") {
			t.Fatalf("path %q was not expanded; a literal ~ is not a real directory, "+
				"so the grant would silently apply to nothing", p)
		}
	}
}

func hasPath(haystack []string, needle string) bool {
	for _, h := range haystack {
		if h == needle {
			return true
		}
	}
	return false
}

// TestOperatorConfigWritePathsAreHonored pins that write_paths works from
// CONFIG, not only from a shipped default.
//
// The shipped defaults declare their own paths in Go, so every other test here
// would pass even if the toml tag were wrong or the field never decoded -- and
// an operator adding a provider Ralph does not ship would silently get no
// grant, with their contained turns failing for a reason the config appears to
// address.
//
// That is the inert-config shape this codebase has closed four times (the
// contain key, WithProviderWriteContainment, the declarative stream-json shape,
// and ralph-task `binding`). Verified through an actual toml decode rather than
// by constructing the struct in Go, which would prove nothing about the tag.
func TestOperatorConfigWritePathsAreHonored(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("no home dir: %v", err)
	}
	var file struct {
		Providers map[string]BindingConfig `toml:"providers"`
	}
	const src = `
[providers.custom]
type = "codex"
binary = "codex"
write_paths = ["~/.custom-provider-state"]
`
	if _, err := toml.Decode(src, &file); err != nil {
		t.Fatalf("decode provider config: %v", err)
	}
	cfg, ok := file.Providers["custom"]
	if !ok {
		t.Fatal("provider block did not decode at all")
	}
	if len(cfg.WritePaths) != 1 {
		t.Fatalf("WritePaths = %v, want one entry; the toml tag does not match the "+
			"key an operator writes, so a configured grant silently does nothing",
			cfg.WritePaths)
	}
	want := filepath.Join(home, ".custom-provider-state")
	if got := BindingWritePaths(Binding{Config: cfg}); !hasPath(got, want) {
		t.Fatalf("BindingWritePaths = %v, want it to include %q", got, want)
	}
}
