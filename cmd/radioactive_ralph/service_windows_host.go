package main

import (
	"fmt"
	"strings"
)

type windowsServiceHostArgs struct {
	RepoRoot    string
	ServiceName string
	ConfigPath  string
}

func parseWindowsServiceHostArgs(args []string) (windowsServiceHostArgs, bool, error) {
	var parsed windowsServiceHostArgs
	if len(args) < 3 || args[1] != "service" || args[2] != "run-windows" {
		return parsed, false, nil
	}
	rest := append([]string(nil), args[3:]...)
	for len(rest) > 0 {
		arg := rest[0]
		rest = rest[1:]
		key, value, hasValue := strings.Cut(arg, "=")
		switch key {
		case "--repo-root":
			if !hasValue {
				if len(rest) == 0 {
					return parsed, true, fmt.Errorf("--repo-root requires a value")
				}
				value = rest[0]
				rest = rest[1:]
			}
			parsed.RepoRoot = value
		case "--service-name":
			if !hasValue {
				if len(rest) == 0 {
					return parsed, true, fmt.Errorf("--service-name requires a value")
				}
				value = rest[0]
				rest = rest[1:]
			}
			parsed.ServiceName = value
		case "--config-path":
			if !hasValue {
				if len(rest) == 0 {
					return parsed, true, fmt.Errorf("--config-path requires a value")
				}
				value = rest[0]
				rest = rest[1:]
			}
			parsed.ConfigPath = value
		default:
			return parsed, true, fmt.Errorf("unknown run-windows argument %q", arg)
		}
	}
	if parsed.RepoRoot == "" && parsed.ConfigPath == "" {
		return parsed, true, fmt.Errorf("run-windows requires --repo-root or --config-path")
	}
	return parsed, true, nil
}
