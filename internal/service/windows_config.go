package service

import (
	"encoding/json"
	"fmt"
)

// WindowsServiceConfig is the persisted config payload used by the native
// Windows service host.
type WindowsServiceConfig struct {
	RepoPath string            `json:"repo_path"`
	ExtraEnv map[string]string `json:"extra_env,omitempty"`
}

// BuildWindowsServiceConfig produces the persisted config payload for a repo
// service instance.
func BuildWindowsServiceConfig(opts InstallOptions) WindowsServiceConfig {
	cfg := WindowsServiceConfig{
		RepoPath: opts.RepoPath,
	}
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

// WindowsServiceArgs returns the radioactive_ralph argv used by the native
// Windows SCM service entry.
func WindowsServiceArgs(repoPath, serviceName, configPath string) []string {
	if serviceName == "" {
		serviceName = UnitName(BackendWindowsSCM, repoPath)
	}
	args := []string{
		"service",
		"run-windows",
		"--repo-root", repoPath,
		"--service-name", serviceName,
	}
	if configPath != "" {
		args = append(args, "--config-path", configPath)
	}
	return args
}
