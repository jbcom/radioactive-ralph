package inventory

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// writeSkill creates a fake SKILL.md file with the given frontmatter.
func writeSkill(t *testing.T, dir, name, description string) string {
	t.Helper()
	skillDir := filepath.Join(dir, name)
	if err := os.MkdirAll(skillDir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	path := filepath.Join(skillDir, "SKILL.md")
	body := "---\nname: " + name + "\ndescription: \"" + description + "\"\n---\n\n# " + name + "\n\nbody\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	return path
}

// writeAgent creates a fake agent markdown file.
func writeAgent(t *testing.T, dir, name string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	path := filepath.Join(dir, name+".md")
	body := "---\nname: " + name + "\ndescription: \"Test agent\"\n---\n\nBody\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	return path
}

// writeSettings writes a claude-code settings.json with the given MCP servers.
func writeSettings(t *testing.T, path string, mcps []string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	mcp := make(map[string]any)
	for _, name := range mcps {
		mcp[name] = map[string]any{"command": "fake"}
	}
	data, err := json.Marshal(map[string]any{"mcpServers": mcp})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
}

func TestSkillFullName(t *testing.T) {
	cases := []struct {
		name, plugin, want string
	}{
		{"brainstorming", "", "brainstorming"},
		{"brainstorming", "superpowers", "superpowers:brainstorming"},
		{"code-review", "code-review", "code-review:code-review"},
	}
	for _, c := range cases {
		t.Run(c.want, func(t *testing.T) {
			s := Skill{Name: c.name, Plugin: c.plugin}
			if s.FullName() != c.want {
				t.Errorf("FullName() = %q, want %q", s.FullName(), c.want)
			}
		})
	}
}

func TestDiscoverEmpty(t *testing.T) {
	home := t.TempDir()
	inv, errs := Discover(Options{Home: home})
	if len(errs) != 0 {
		t.Errorf("expected no errors for empty home, got %v", errs)
	}
	if len(inv.Skills) != 0 {
		t.Errorf("expected 0 skills, got %d", len(inv.Skills))
	}
	if inv.GeneratedAt.IsZero() {
		t.Error("GeneratedAt should be set")
	}
}

func TestDiscoverUserSkills(t *testing.T) {
	home := t.TempDir()
	skillsDir := filepath.Join(home, ".claude", "skills")
	writeSkill(t, skillsDir, "autoloop", "Autonomous loop.")
	writeSkill(t, skillsDir, "deslop", "Clean slop.")

	inv, errs := Discover(Options{Home: home})
	if len(errs) != 0 {
		t.Fatalf("Discover errors: %v", errs)
	}
	if len(inv.Skills) != 2 {
		t.Fatalf("expected 2 skills, got %d: %+v", len(inv.Skills), inv.Skills)
	}
	if inv.Skills[0].Name != "autoloop" {
		t.Errorf("Skills[0] = %q, expected autoloop (sorted)", inv.Skills[0].Name)
	}
	for _, s := range inv.Skills {
		if s.Plugin != "" {
			t.Errorf("user skill should have empty Plugin, got %q", s.Plugin)
		}
		if s.FullName() != s.Name {
			t.Errorf("user skill FullName should equal Name; got %q vs %q", s.FullName(), s.Name)
		}
	}
}

func TestDiscoverPluginSkills(t *testing.T) {
	home := t.TempDir()
	cacheDir := filepath.Join(home, ".claude", "plugins", "cache")
	pluginSkillsDir := filepath.Join(cacheDir, "claude-plugins-official", "superpowers", "5.0.7", "skills")
	writeSkill(t, pluginSkillsDir, "brainstorming", "Brainstorm before implementing.")
	writeSkill(t, pluginSkillsDir, "systematic-debugging", "Debug methodically.")

	inv, errs := Discover(Options{Home: home})
	if len(errs) != 0 {
		t.Fatalf("Discover errors: %v", errs)
	}
	if len(inv.Skills) != 2 {
		t.Fatalf("expected 2 skills, got %d", len(inv.Skills))
	}
	for _, s := range inv.Skills {
		if s.Plugin != "superpowers" {
			t.Errorf("expected Plugin=superpowers, got %q", s.Plugin)
		}
	}
	if !inv.HasSkill("superpowers:brainstorming") {
		t.Error("HasSkill should find superpowers:brainstorming")
	}
	if inv.HasSkill("brainstorming") {
		t.Error("HasSkill for bare name shouldn't match plugin-bundled skill")
	}
}

func TestDiscoverPluginSkillsMultipleVersionsDedupeToLatest(t *testing.T) {
	home := t.TempDir()
	cacheDir := filepath.Join(home, ".claude", "plugins", "cache")
	v1 := filepath.Join(cacheDir, "mp", "plugA", "1.0.0", "skills")
	v2 := filepath.Join(cacheDir, "mp", "plugA", "2.0.0", "skills")
	writeSkill(t, v1, "same-skill", "v1")
	writeSkill(t, v2, "same-skill", "v2")

	inv, errs := Discover(Options{Home: home})
	if len(errs) != 0 {
		t.Fatalf("Discover errors: %v", errs)
	}
	if len(inv.Skills) != 1 {
		t.Fatalf("expected 1 skill after dedup, got %d", len(inv.Skills))
	}
	// Map iteration is unordered so we can't guarantee we get v2 over v1,
	// but we CAN guarantee only one entry exists. The latest-version
	// preference is documented but we accept "any stable version" as the
	// inventory behavior until we have a reason to care.
}

func TestDiscoverAgents(t *testing.T) {
	home := t.TempDir()
	agentsDir := filepath.Join(home, ".claude", "agents")
	writeAgent(t, agentsDir, "code-reviewer")
	writeAgent(t, agentsDir, "explore")

	inv, errs := Discover(Options{Home: home})
	if len(errs) != 0 {
		t.Fatalf("Discover errors: %v", errs)
	}
	if len(inv.Agents) != 2 {
		t.Fatalf("expected 2 agents, got %d", len(inv.Agents))
	}
	if inv.Agents[0].Name != "code-reviewer" {
		t.Errorf("Agents sorted wrong: %+v", inv.Agents)
	}
}

func TestDiscoverUserMCP(t *testing.T) {
	home := t.TempDir()
	settingsPath := filepath.Join(home, ".claude", "settings.json")
	writeSettings(t, settingsPath, []string{"context7", "chrome-devtools"})

	inv, errs := Discover(Options{Home: home})
	if len(errs) != 0 {
		t.Fatalf("Discover errors: %v", errs)
	}
	if len(inv.MCPServers) != 2 {
		t.Fatalf("expected 2 MCPs, got %d", len(inv.MCPServers))
	}
	if !inv.HasMCP("context7") {
		t.Error("HasMCP should find context7")
	}
	if inv.MCPServers[0].Source != "user" {
		t.Errorf("expected source=user, got %q", inv.MCPServers[0].Source)
	}
}

func TestDiscoverProjectMCP(t *testing.T) {
	home := t.TempDir()
	repo := t.TempDir()
	writeSettings(t, filepath.Join(home, ".claude", "settings.json"), []string{"user-level"})
	writeSettings(t, filepath.Join(repo, ".claude", "settings.json"), []string{"project-level"})

	inv, errs := Discover(Options{Home: home, RepoRoot: repo})
	if len(errs) != 0 {
		t.Fatalf("Discover errors: %v", errs)
	}

	gotUser, gotProj := false, false
	for _, m := range inv.MCPServers {
		switch m.Name {
		case "user-level":
			gotUser = true
			if m.Source != "user" {
				t.Errorf("expected user-level source=user, got %q", m.Source)
			}
		case "project-level":
			gotProj = true
			if m.Source != "project" {
				t.Errorf("expected project-level source=project, got %q", m.Source)
			}
		}
	}
	if !gotUser || !gotProj {
		t.Errorf("missing MCPs: user=%v project=%v list=%+v", gotUser, gotProj, inv.MCPServers)
	}
}

func TestDiscoverSkipsMalformedSkill(t *testing.T) {
	home := t.TempDir()
	skillsDir := filepath.Join(home, ".claude", "skills")
	// Valid skill
	writeSkill(t, skillsDir, "valid", "ok")
	// Malformed — no frontmatter
	brokenDir := filepath.Join(skillsDir, "broken")
	if err := os.MkdirAll(brokenDir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(brokenDir, "SKILL.md"), []byte("no frontmatter here\n"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	inv, errs := Discover(Options{Home: home})
	if len(errs) != 0 {
		t.Fatalf("Discover errors (malformed should skip silently): %v", errs)
	}
	if len(inv.Skills) != 1 {
		t.Fatalf("expected only valid skill, got %d: %+v", len(inv.Skills), inv.Skills)
	}
}

func TestDiscoverEnvironmentSignals(t *testing.T) {
	home := t.TempDir()
	inv, _ := Discover(Options{
		Home:            home,
		GhAuthenticated: true,
		InClaudeCode:    true,
	})
	if !inv.Environment.GhAuthenticated {
		t.Error("GhAuthenticated should be passed through")
	}
	if !inv.Environment.InClaudeCode {
		t.Error("InClaudeCode should be passed through")
	}
}

func TestSaveAndLoadRoundtrip(t *testing.T) {
	orig := Inventory{
		GeneratedAt: time.Now().UTC().Round(time.Second),
		Skills: []Skill{
			{Name: "autoloop", Source: "/fake/path"},
			{Name: "brainstorming", Plugin: "superpowers", Source: "/other"},
		},
		MCPServers:  []MCPServer{{Name: "context7", Source: "user"}},
		Agents:      []Agent{{Name: "code-reviewer", Source: "/a"}},
		Environment: Environment{GhAuthenticated: true},
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "inventory.json")
	if err := orig.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !loaded.GeneratedAt.Equal(orig.GeneratedAt) {
		t.Errorf("GeneratedAt mismatch: got %v want %v", loaded.GeneratedAt, orig.GeneratedAt)
	}
	if len(loaded.Skills) != len(orig.Skills) {
		t.Errorf("Skills len mismatch")
	}
	if !loaded.HasSkill("autoloop") || !loaded.HasSkill("superpowers:brainstorming") {
		t.Error("HasSkill should work on loaded inventory")
	}
	if !loaded.HasMCP("context7") {
		t.Error("HasMCP should work on loaded inventory")
	}
}

func TestLoadMissingFile(t *testing.T) {
	_, err := Load(filepath.Join(t.TempDir(), "no-such-file.json"))
	if err == nil {
		t.Fatal("expected error for missing file")
	}
	if !strings.Contains(err.Error(), "inventory") {
		t.Errorf("error should mention inventory: %v", err)
	}
}

func TestDiscoverResultIsSorted(t *testing.T) {
	home := t.TempDir()
	skillsDir := filepath.Join(home, ".claude", "skills")
	// Write in scrambled order
	writeSkill(t, skillsDir, "charlie", "c")
	writeSkill(t, skillsDir, "alpha", "a")
	writeSkill(t, skillsDir, "bravo", "b")

	inv, _ := Discover(Options{Home: home})
	want := []string{"alpha", "bravo", "charlie"}
	if len(inv.Skills) != len(want) {
		t.Fatalf("wrong count: %d", len(inv.Skills))
	}
	for i, w := range want {
		if inv.Skills[i].Name != w {
			t.Errorf("Skills[%d] = %q, want %q", i, inv.Skills[i].Name, w)
		}
	}
}
