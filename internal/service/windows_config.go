package service

import (
	"encoding/json"
	"fmt"
)

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

// WindowsServiceArgs returns the radioactive_ralph argv used by the native
// Windows SCM service entry: just --supervisor, since the per-user
// supervisor takes no repo-scoped arguments.
func WindowsServiceArgs() []string {
	return []string{"--supervisor"}
}
