package service

import (
	"fmt"
	"strings"
)

// renderSystemdUser returns a systemd user .service unit for the per-user
// radioactive_ralph supervisor.
//
// The unit runs `<ralphBin> --supervisor` with Restart=on-failure and
// systemd's own journal for stdout/stderr. INVOCATION_ID is automatically
// set by systemd — no need to inject.
func renderSystemdUser(opts InstallOptions) string {
	var sb strings.Builder
	sb.WriteString("[Unit]\n")
	fmt.Fprintf(&sb, "Description=radioactive-ralph durable supervisor (%s)\n",
		UnitName(BackendSystemdUser))
	sb.WriteString("After=network-online.target\n\n")

	sb.WriteString("[Service]\n")
	sb.WriteString("Type=simple\n")
	fmt.Fprintf(&sb, "ExecStart=%s --supervisor\n", opts.RalphBin)
	sb.WriteString("Restart=on-failure\n")
	sb.WriteString("RestartSec=10\n")

	// Environment — sorted for stable output across runs.
	for _, k := range sortedKeys(opts.ExtraEnv) {
		fmt.Fprintf(&sb, "Environment=%s=%s\n", k, opts.ExtraEnv[k])
	}

	sb.WriteString("\n[Install]\n")
	sb.WriteString("WantedBy=default.target\n")
	return sb.String()
}
