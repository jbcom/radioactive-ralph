package vconfig

import (
	"testing"

	"github.com/spf13/cobra"
)

// TestAddFlagsRegistersAllThree verifies AddFlags wires up all three
// persistent flags and FlagsFrom reads back parsed values correctly,
// including the -C shorthand for --config-file.
func TestAddFlagsRegistersAllThree(t *testing.T) {
	cmd := &cobra.Command{Use: "test"}
	AddFlags(cmd)

	args := []string{
		"-C", "/tmp/joint.toml",
		"--user-config-file", "/tmp/user.toml",
		"--project-config-file", "/tmp/project.toml",
	}
	cmd.SetArgs(args)
	if err := cmd.ParseFlags(args); err != nil {
		t.Fatalf("ParseFlags: %v", err)
	}

	configFile, userConfigFile, projectConfigFile := FlagsFrom(cmd)
	if configFile != "/tmp/joint.toml" {
		t.Errorf("configFile = %q, want /tmp/joint.toml", configFile)
	}
	if userConfigFile != "/tmp/user.toml" {
		t.Errorf("userConfigFile = %q, want /tmp/user.toml", userConfigFile)
	}
	if projectConfigFile != "/tmp/project.toml" {
		t.Errorf("projectConfigFile = %q, want /tmp/project.toml", projectConfigFile)
	}
}

// TestFlagsFromDefaultsEmpty verifies FlagsFrom returns empty strings when
// no flags were passed.
func TestFlagsFromDefaultsEmpty(t *testing.T) {
	cmd := &cobra.Command{Use: "test"}
	AddFlags(cmd)

	configFile, userConfigFile, projectConfigFile := FlagsFrom(cmd)
	if configFile != "" || userConfigFile != "" || projectConfigFile != "" {
		t.Errorf("expected all empty, got (%q, %q, %q)", configFile, userConfigFile, projectConfigFile)
	}
}
