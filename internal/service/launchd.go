package service

import (
	"fmt"
	"sort"
	"strings"
)

// renderLaunchd returns a fully-formed plist XML for a per-user
// launchd agent.
//
// The agent runs `<ralphBin> service start --foreground --repo-root <repo>`
// under RepoPath, auto-restarts on crash (KeepAlive=true), logs to
// ~/Library/Logs/radioactive-ralph/<repo-token>.log, and always sets
// LAUNCHED_BY=launchd so the runtime can detect durable service context.
//
// Intentionally plain-string templated rather than via encoding/xml —
// plists are finicky about attribute ordering and empty-element shape,
// and the target format is small and stable.
func renderLaunchd(opts InstallOptions) string {
	slug, _ := repoToken(opts.RepoPath)
	var sb strings.Builder
	sb.WriteString(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
`)
	fmt.Fprintf(&sb, "  <key>Label</key>\n  <string>%s</string>\n",
		UnitName(BackendLaunchd, opts.RepoPath))
	sb.WriteString("  <key>ProgramArguments</key>\n  <array>\n")
	fmt.Fprintf(&sb, "    <string>%s</string>\n", xmlEscape(opts.RalphBin))
	sb.WriteString("    <string>service</string>\n")
	sb.WriteString("    <string>start</string>\n")
	sb.WriteString("    <string>--foreground</string>\n")
	sb.WriteString("    <string>--repo-root</string>\n")
	fmt.Fprintf(&sb, "    <string>%s</string>\n", xmlEscape(opts.RepoPath))
	sb.WriteString("  </array>\n")

	if opts.RepoPath != "" {
		fmt.Fprintf(&sb, "  <key>WorkingDirectory</key>\n  <string>%s</string>\n",
			xmlEscape(opts.RepoPath))
	}

	sb.WriteString("  <key>KeepAlive</key>\n  <true/>\n")
	sb.WriteString("  <key>RunAtLoad</key>\n  <true/>\n")
	sb.WriteString("  <key>ProcessType</key>\n  <string>Background</string>\n")

	// Log paths — ~/Library/Logs/radioactive-ralph/<variant>.log
	logPath := fmt.Sprintf("${HOME}/Library/Logs/radioactive-ralph/%s.log", slug)
	fmt.Fprintf(&sb, "  <key>StandardOutPath</key>\n  <string>%s</string>\n", logPath)
	fmt.Fprintf(&sb, "  <key>StandardErrorPath</key>\n  <string>%s</string>\n", logPath)

	// Environment — LAUNCHED_BY=launchd plus any operator-supplied extras.
	env := make(map[string]string, len(opts.ExtraEnv)+1)
	for k, v := range opts.ExtraEnv {
		env[k] = v
	}
	env["LAUNCHED_BY"] = "launchd"

	sb.WriteString("  <key>EnvironmentVariables</key>\n  <dict>\n")
	for _, k := range sortedKeys(env) {
		fmt.Fprintf(&sb, "    <key>%s</key>\n    <string>%s</string>\n",
			xmlEscape(k), xmlEscape(env[k]))
	}
	sb.WriteString("  </dict>\n")

	sb.WriteString("</dict>\n</plist>\n")
	return sb.String()
}

// xmlEscape escapes the five XML metacharacters.
func xmlEscape(s string) string {
	r := strings.NewReplacer(
		"&", "&amp;",
		"<", "&lt;",
		">", "&gt;",
		`"`, "&quot;",
		"'", "&apos;",
	)
	return r.Replace(s)
}

// sortedKeys returns m's keys sorted for stable output.
func sortedKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
