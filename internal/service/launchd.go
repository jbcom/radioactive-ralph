package service

import (
	"fmt"
	"path"
	"sort"
	"strings"
)

// renderLaunchd returns a fully-formed plist XML for the per-user launchd
// agent that runs `<ralphBin> --supervisor`, auto-restarts on crash
// (KeepAlive=true), and logs to
// <home>/Library/Logs/radioactive-ralph/supervisor.log. It always sets
// LAUNCHED_BY=launchd so the runtime can detect durable service context.
//
// home is the resolved home directory the log path is written under.
// launchd's StandardOutPath/StandardErrorPath keys take a literal
// filesystem path — unlike a shell command line, launchd does NOT expand
// "${HOME}" or "~" in plist string values, so the log path must be
// resolved to an absolute path before being written into the plist (a
// literal "${HOME}/..." string previously here silently pointed at a
// nonexistent directory, which made launchd itself refuse to spawn the
// job at all — an EX_CONFIG/78 exit with no log ever written, since the
// job never got far enough to open its own log file). Install creates
// the log directory before writing this plist so launchd never has to.
//
// Intentionally plain-string templated rather than via encoding/xml —
// plists are finicky about attribute ordering and empty-element shape,
// and the target format is small and stable.
func renderLaunchd(opts InstallOptions, home string) string {
	var sb strings.Builder
	sb.WriteString(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
`)
	fmt.Fprintf(&sb, "  <key>Label</key>\n  <string>%s</string>\n", UnitName(BackendLaunchd))
	sb.WriteString("  <key>ProgramArguments</key>\n  <array>\n")
	fmt.Fprintf(&sb, "    <string>%s</string>\n", xmlEscape(opts.RalphBin))
	sb.WriteString("    <string>--supervisor</string>\n")
	sb.WriteString("  </array>\n")

	sb.WriteString("  <key>KeepAlive</key>\n  <true/>\n")
	sb.WriteString("  <key>RunAtLoad</key>\n  <true/>\n")
	sb.WriteString("  <key>ProcessType</key>\n  <string>Background</string>\n")

	// Log paths — <home>/Library/Logs/radioactive-ralph/supervisor.log.
	logPath := path.Join(home, "Library", "Logs", "radioactive-ralph", "supervisor.log")
	fmt.Fprintf(&sb, "  <key>StandardOutPath</key>\n  <string>%s</string>\n", xmlEscape(logPath))
	fmt.Fprintf(&sb, "  <key>StandardErrorPath</key>\n  <string>%s</string>\n", xmlEscape(logPath))

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
