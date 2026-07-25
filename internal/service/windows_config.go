package service

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

const windowsServiceConfigFlag = "--windows-service-config"

// WindowsServiceConfig is the persisted config payload used by the native
// Windows service host for the per-user supervisor service.
type WindowsServiceConfig struct {
	ExtraEnv map[string]string `json:"extra_env,omitempty"`
}

// BuildWindowsServiceConfig produces the persisted config payload for the
// supervisor service instance.
func BuildWindowsServiceConfig(opts InstallOptions) WindowsServiceConfig {
	cfg := WindowsServiceConfig{}
	if len(opts.ExtraEnv) != 0 {
		cfg.ExtraEnv = make(map[string]string, len(opts.ExtraEnv))
		for k, v := range opts.ExtraEnv {
			cfg.ExtraEnv[k] = v
		}
	}
	return cfg
}

// MarshalWindowsServiceConfig renders the Windows service config in the exact
// JSON form written to disk for the native service host.
func MarshalWindowsServiceConfig(opts InstallOptions) ([]byte, error) {
	raw, err := json.MarshalIndent(BuildWindowsServiceConfig(opts), "", "  ")
	if err != nil {
		return nil, fmt.Errorf("service: marshal windows config: %w", err)
	}
	return raw, nil
}

// ParseWindowsServiceConfig parses the persisted Windows service config JSON.
func ParseWindowsServiceConfig(raw []byte) (WindowsServiceConfig, error) {
	var cfg WindowsServiceConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return WindowsServiceConfig{}, fmt.Errorf("service: parse windows config: %w", err)
	}
	return cfg, nil
}

// LoadWindowsServiceConfig reads the private config file named on the SCM
// command line. The explicit path matters because an SCM service commonly
// runs under a different account than the operator who installed it, so
// recomputing the operator's LocalAppData directory inside the service would
// load the wrong file.
func LoadWindowsServiceConfig(path string) (WindowsServiceConfig, error) {
	if strings.TrimSpace(path) == "" {
		return WindowsServiceConfig{}, fmt.Errorf("service: windows config path required")
	}
	raw, err := os.ReadFile(path) //nolint:gosec // the trusted SCM definition supplies this path
	if err != nil {
		return WindowsServiceConfig{}, fmt.Errorf("service: read windows config %s: %w", path, err)
	}
	return ParseWindowsServiceConfig(raw)
}

// WindowsServiceArgs returns the radioactive_ralph argv used by the native
// Windows SCM service entry when no persisted environment is required. It is
// retained as the minimal supervisor invocation contract used by callers and
// tests outside the SCM installer.
func WindowsServiceArgs() []string {
	return []string{"--supervisor"}
}

// WindowsServiceArgsForConfig returns the complete SCM argv. The config path
// is carried explicitly instead of inferred from the service account's home.
func WindowsServiceArgsForConfig(configPath string) []string {
	return []string{"--supervisor", windowsServiceConfigFlag, configPath}
}

// WindowsServiceConfigPath extracts the private config path from an SCM
// invocation. It accepts both the two-argument and --flag=value spellings so
// diagnostics remain predictable if an administrator inspects or edits the
// service definition.
func WindowsServiceConfigPath(args []string) (string, error) {
	for i, arg := range args {
		switch {
		case arg == windowsServiceConfigFlag:
			if i+1 >= len(args) || strings.TrimSpace(args[i+1]) == "" {
				return "", fmt.Errorf("service: %s requires a path", windowsServiceConfigFlag)
			}
			return args[i+1], nil
		case strings.HasPrefix(arg, windowsServiceConfigFlag+"="):
			path := strings.TrimSpace(strings.TrimPrefix(arg, windowsServiceConfigFlag+"="))
			if path == "" {
				return "", fmt.Errorf("service: %s requires a path", windowsServiceConfigFlag)
			}
			return path, nil
		}
	}
	return "", fmt.Errorf("service: SCM invocation is missing %s", windowsServiceConfigFlag)
}
