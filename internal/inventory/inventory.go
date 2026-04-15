// Package inventory discovers the operator's installed Claude Code skills,
// MCP servers, subagents, and plugins by walking the filesystem and
// parsing Claude Code's settings files.
//
// Discovery is pure shell / filesystem work. No Claude is involved — we
// don't prompt any session to self-describe. This package runs during
// `radioactive_ralph init` so the operator can pick preferences for ambiguous
// categories (multiple review skills, for instance) and is re-run at
// `radioactive_ralph run` start so the supervisor can filter variant biases against
// what's actually installed at runtime.
//
// Discovery sources, in order:
//
//  1. User-level skills at ~/.claude/skills/<name>/SKILL.md
//  2. Plugin-bundled skills at ~/.claude/plugins/cache/<marketplace>/
//     <plugin>/<version>/skills/<name>/SKILL.md
//  3. User-level subagents at ~/.claude/agents/
//  4. MCP servers declared in ~/.claude/settings.json "mcpServers"
//  5. Project-local MCP servers from ./.claude/settings.json if the
//     caller provides a repo root
//  6. Plugin-declared MCP servers from each plugin's plugin.json
//
// Output is a stable JSON document written to the workspace inventory
// path (see xdg.Paths.Inventory) and re-readable by the supervisor.
package inventory

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Inventory is the JSON-serialisable discovery snapshot. It's the single
// source of truth for "what capabilities does this operator have?" and is
// consumed by the init wizard, the supervisor's prompt renderer, and
// `radioactive_ralph doctor`.
type Inventory struct {
	GeneratedAt   time.Time   `json:"generated_at"`
	ClaudeVersion string      `json:"claude_version,omitempty"`
	Skills        []Skill     `json:"skills"`
	MCPServers    []MCPServer `json:"mcp_servers"`
	Agents        []Agent     `json:"agents"`
	Environment   Environment `json:"environment"`
}

// Skill represents one discovered skill. The Name field matches the
// `name:` value in SKILL.md frontmatter; for plugin-bundled skills the
// Plugin field holds the plugin ID that provides it (e.g. "superpowers")
// so operators can disambiguate same-named skills from different plugins.
type Skill struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Plugin      string `json:"plugin,omitempty"` // empty for user-level skills
	Source      string `json:"source"`           // absolute path to SKILL.md
}

// FullName returns "plugin:name" when the skill is plugin-bundled, or
// just "name" when it's a user-level skill. This matches how variant
// profiles reference skills in their BiasSnippet tables.
func (s Skill) FullName() string {
	if s.Plugin == "" {
		return s.Name
	}
	return s.Plugin + ":" + s.Name
}

// MCPServer represents one entry from settings.json mcpServers. We do
// not attempt to reach the server during inventory; Reachable is
// populated later by the runtime connectivity check.
type MCPServer struct {
	Name      string `json:"name"`
	Reachable bool   `json:"reachable"`
	Source    string `json:"source"` // "user" | "project" | "plugin"
}

// Agent represents a Claude Code subagent declared under ~/.claude/agents/.
type Agent struct {
	Name   string `json:"name"`
	Source string `json:"source"`
}

// Environment captures free-floating signals the supervisor might want
// when deciding how to spawn sessions. Kept small and boolean-heavy so
// the inventory JSON stays readable.
type Environment struct {
	GhAuthenticated bool `json:"gh_authenticated"`
	InClaudeCode    bool `json:"in_claude_code"`
}

// Options controls discovery. Every field is optional; zero value does
// "normal" discovery against the current user's home dir.
type Options struct {
	// Home overrides the home directory probe. Useful for tests.
	Home string

	// RepoRoot, if non-empty, triggers project-local discovery (reads
	// .claude/settings.json inside the repo).
	RepoRoot string

	// ClaudeVersion is a caller-provided version string. We don't shell
	// out to `claude --version` from this package to avoid a subprocess
	// dependency; the CLI supplies it.
	ClaudeVersion string

	// GhAuthenticated is a caller-provided signal. Same reasoning.
	GhAuthenticated bool

	// InClaudeCode is a caller-provided signal for the CLAUDECODE env.
	InClaudeCode bool
}

// Discover walks the filesystem and returns an Inventory. Errors are
// collected per-source rather than returned as a single terminal error:
// a missing ~/.claude/skills/ directory should not prevent plugin
// discovery, and a malformed plugin.json should not prevent MCP
// discovery.
func Discover(opts Options) (Inventory, []error) {
	inv := Inventory{
		GeneratedAt:   time.Now().UTC(),
		ClaudeVersion: opts.ClaudeVersion,
		Environment: Environment{
			GhAuthenticated: opts.GhAuthenticated,
			InClaudeCode:    opts.InClaudeCode,
		},
	}

	home := opts.Home
	if home == "" {
		h, err := os.UserHomeDir()
		if err != nil {
			return inv, []error{fmt.Errorf("inventory: resolve home: %w", err)}
		}
		home = h
	}

	var errs []error

	// 1. user-level skills
	userSkills, err := discoverUserSkills(filepath.Join(home, ".claude", "skills"))
	if err != nil {
		errs = append(errs, fmt.Errorf("user skills: %w", err))
	}
	inv.Skills = append(inv.Skills, userSkills...)

	// 2. plugin-bundled skills
	pluginSkills, err := discoverPluginSkills(filepath.Join(home, ".claude", "plugins", "cache"))
	if err != nil {
		errs = append(errs, fmt.Errorf("plugin skills: %w", err))
	}
	inv.Skills = append(inv.Skills, pluginSkills...)

	// 3. subagents
	agents, err := discoverAgents(filepath.Join(home, ".claude", "agents"))
	if err != nil {
		errs = append(errs, fmt.Errorf("agents: %w", err))
	}
	inv.Agents = agents

	// 4. user MCP servers
	userMCP, err := discoverMCPServers(filepath.Join(home, ".claude", "settings.json"), "user")
	if err != nil {
		errs = append(errs, fmt.Errorf("user MCP: %w", err))
	}
	inv.MCPServers = append(inv.MCPServers, userMCP...)

	// 5. project-local MCP servers
	if opts.RepoRoot != "" {
		projectMCP, err := discoverMCPServers(filepath.Join(opts.RepoRoot, ".claude", "settings.json"), "project")
		if err != nil {
			errs = append(errs, fmt.Errorf("project MCP: %w", err))
		}
		inv.MCPServers = append(inv.MCPServers, projectMCP...)
	}

	// Sort for stable output.
	sort.Slice(inv.Skills, func(i, j int) bool {
		a, b := inv.Skills[i].FullName(), inv.Skills[j].FullName()
		return a < b
	})
	sort.Slice(inv.MCPServers, func(i, j int) bool {
		return inv.MCPServers[i].Name < inv.MCPServers[j].Name
	})
	sort.Slice(inv.Agents, func(i, j int) bool {
		return inv.Agents[i].Name < inv.Agents[j].Name
	})

	return inv, errs
}

// Save writes the inventory JSON to path with 0o600 mode.
func (inv Inventory) Save(path string) error {
	data, err := json.MarshalIndent(inv, "", "  ")
	if err != nil {
		return fmt.Errorf("inventory: marshal: %w", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("inventory: write %s: %w", path, err)
	}
	return nil
}

// Load reads an inventory JSON file previously written by Save.
func Load(path string) (Inventory, error) {
	var inv Inventory
	data, err := os.ReadFile(path) //nolint:gosec // path comes from xdg.Paths which is caller-controlled
	if err != nil {
		return inv, fmt.Errorf("inventory: read %s: %w", path, err)
	}
	if err := json.Unmarshal(data, &inv); err != nil {
		return inv, fmt.Errorf("inventory: parse %s: %w", path, err)
	}
	return inv, nil
}

// HasSkill reports whether the inventory contains a skill with the given
// full name (either plain "name" or "plugin:name").
func (inv Inventory) HasSkill(fullName string) bool {
	for _, s := range inv.Skills {
		if s.FullName() == fullName {
			return true
		}
	}
	return false
}

// HasMCP reports whether the inventory contains an MCP server with the
// given name. Reachability is not considered — use the MCPServer field
// Reachable directly if that matters.
func (inv Inventory) HasMCP(name string) bool {
	for _, m := range inv.MCPServers {
		if m.Name == name {
			return true
		}
	}
	return false
}

// --- filesystem helpers --------------------------------------------------

// skillFrontmatter is the shape of the YAML frontmatter we care about
// inside a SKILL.md file. Only the name + description are extracted.
// Richer frontmatter (argument-hint, triggers, user-invocable, etc.)
// is ignored by the inventory and read directly by the relevant
// consumer if needed.
type skillFrontmatter struct {
	Name        string
	Description string
}

// discoverUserSkills walks ~/.claude/skills/*/SKILL.md.
func discoverUserSkills(dir string) ([]Skill, error) {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []Skill
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		skillPath := filepath.Join(dir, entry.Name(), "SKILL.md")
		skill, ok := parseSkillFile(skillPath, "")
		if ok {
			out = append(out, skill)
		}
	}
	return out, nil
}

// discoverPluginSkills walks ~/.claude/plugins/cache/*/*/*/skills/*/SKILL.md.
// The four "*" are marketplace, plugin, version, skill. We keep the
// highest-version entry per (plugin, skill) pair so the same skill from
// two versions of one plugin doesn't double-count.
func discoverPluginSkills(cacheDir string) ([]Skill, error) {
	marketplaces, err := os.ReadDir(cacheDir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	type key struct{ plugin, skill string }
	latest := make(map[key]Skill)

	for _, mpEntry := range marketplaces {
		if !mpEntry.IsDir() {
			continue
		}
		mpDir := filepath.Join(cacheDir, mpEntry.Name())
		plugins, err := os.ReadDir(mpDir)
		if err != nil {
			continue
		}
		for _, pluginEntry := range plugins {
			if !pluginEntry.IsDir() {
				continue
			}
			pluginDir := filepath.Join(mpDir, pluginEntry.Name())
			versions, err := os.ReadDir(pluginDir)
			if err != nil {
				continue
			}
			for _, verEntry := range versions {
				if !verEntry.IsDir() {
					continue
				}
				skillsDir := filepath.Join(pluginDir, verEntry.Name(), "skills")
				skills, err := os.ReadDir(skillsDir)
				if errors.Is(err, fs.ErrNotExist) {
					continue
				}
				if err != nil {
					continue
				}
				for _, skillEntry := range skills {
					if !skillEntry.IsDir() {
						continue
					}
					skillPath := filepath.Join(skillsDir, skillEntry.Name(), "SKILL.md")
					s, ok := parseSkillFile(skillPath, pluginEntry.Name())
					if !ok {
						continue
					}
					latest[key{s.Plugin, s.Name}] = s
				}
			}
		}
	}

	out := make([]Skill, 0, len(latest))
	for _, s := range latest {
		out = append(out, s)
	}
	return out, nil
}

// discoverAgents walks ~/.claude/agents/ looking for *.md files whose
// frontmatter declares a `name:` key.
func discoverAgents(dir string) ([]Agent, error) {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []Agent
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		if fm, ok := readFrontmatter(path); ok && fm.Name != "" {
			out = append(out, Agent{Name: fm.Name, Source: path})
		}
	}
	return out, nil
}

// discoverMCPServers parses a Claude Code settings.json file and extracts
// the mcpServers keys. The values' structure varies (command vs URL
// configs) and we don't need it for inventory — just the names. source is
// a human-friendly label ("user" or "project") attached to each returned
// MCPServer entry.
func discoverMCPServers(settingsPath, source string) ([]MCPServer, error) {
	data, err := os.ReadFile(settingsPath) //nolint:gosec // settingsPath is caller-controlled
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var raw struct {
		MCPServers map[string]any `json:"mcpServers"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parse %s: %w", settingsPath, err)
	}
	out := make([]MCPServer, 0, len(raw.MCPServers))
	for name := range raw.MCPServers {
		out = append(out, MCPServer{Name: name, Source: source})
	}
	return out, nil
}

// --- frontmatter parsing -------------------------------------------------

// parseSkillFile reads SKILL.md, extracts frontmatter, returns a Skill
// with plugin set. Returns (zero, false) if the file is missing or
// malformed — inventory discovery skips silently rather than failing loudly.
func parseSkillFile(path, plugin string) (Skill, bool) {
	fm, ok := readFrontmatter(path)
	if !ok || fm.Name == "" {
		return Skill{}, false
	}
	return Skill{
		Name:        fm.Name,
		Description: fm.Description,
		Plugin:      plugin,
		Source:      path,
	}, true
}

// readFrontmatter reads a markdown file, extracts the content between the
// first pair of --- markers, and parses it as a narrow subset of YAML
// (top-level `key: value` pairs). Returns (zero, false) if any step
// fails.
//
// The parser is hand-rolled because SKILL.md frontmatter uses YAML
// syntax (unlike TOML's `key = value`). Pulling a full YAML library in
// for two fields is overkill. If the ecosystem ever expands SKILL.md
// frontmatter to nested YAML, swap in goccy/go-yaml.
func readFrontmatter(path string) (skillFrontmatter, bool) {
	var zero skillFrontmatter
	data, err := os.ReadFile(path) //nolint:gosec // path from filesystem walk, caller-controlled
	if err != nil {
		return zero, false
	}
	content := string(data)
	if !strings.HasPrefix(content, "---\n") && !strings.HasPrefix(content, "---\r\n") {
		return zero, false
	}
	// Skip the opening fence.
	rest := content[4:]
	block, _, ok := strings.Cut(rest, "\n---")
	if !ok {
		return zero, false
	}
	return parseSimpleYAML(block), true
}

// parseSimpleYAML extracts top-level `key: value` pairs from a narrow
// subset of YAML. Only `name` and `description` are surfaced; all
// other keys are ignored. Quotes (single or double) around the value
// are stripped.
func parseSimpleYAML(block string) skillFrontmatter {
	var fm skillFrontmatter
	for line := range strings.SplitSeq(block, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		key, rawValue, ok := strings.Cut(trimmed, ":")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		value := strings.TrimSpace(rawValue)
		value = strings.TrimPrefix(value, "\"")
		value = strings.TrimSuffix(value, "\"")
		value = strings.TrimPrefix(value, "'")
		value = strings.TrimSuffix(value, "'")
		switch key {
		case "name":
			fm.Name = value
		case "description":
			fm.Description = value
		}
	}
	return fm
}
