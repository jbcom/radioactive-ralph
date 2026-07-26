//go:build gui

package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"
)

const (
	desktopDispatchHelperEnv    = "RALPH_GUI_DISPATCH_HELPER"
	desktopDispatchHelperArg    = "--ralph-gui-dispatch-helper"
	desktopDispatchHelperMarker = "desktop dispatch handled"
)

// TestMain owns the helper-process entry so desktop dispatch starts on the
// process's main goroutine, before testing.M runs tests on worker goroutines.
// The helper injects a deterministic runner rather than opening a native
// window: it proves root dispatch without requiring a display, supervisor, or
// network and without confusing that proof with a native Fyne lifecycle E2E.
func TestMain(m *testing.M) {
	if desktopDispatchHelperRequested(os.Args, os.Getenv) {
		os.Exit(runDesktopDispatchHelper())
	}
	os.Exit(m.Run())
}

func TestShouldLaunchDesktopGUI(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		stdinTTY  bool
		stdoutTTY bool
		want      bool
	}{
		{name: "no controlling terminal", want: true},
		{name: "stdin terminal", stdinTTY: true},
		{name: "stdout terminal", stdoutTTY: true},
		{name: "both terminals", stdinTTY: true, stdoutTTY: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := shouldLaunchDesktopGUI(tt.stdinTTY, tt.stdoutTTY); got != tt.want {
				t.Errorf(
					"shouldLaunchDesktopGUI(stdinTTY=%t, stdoutTTY=%t) = %t, want %t",
					tt.stdinTTY,
					tt.stdoutTTY,
					got,
					tt.want,
				)
			}
		})
	}
}

func TestDesktopDispatchHelperRequested(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		args  []string
		value string
		want  bool
	}{
		{
			name:  "explicit environment and exact argument",
			args:  []string{"radioactive_ralph.test", desktopDispatchHelperArg},
			value: "1",
			want:  true,
		},
		{
			name: "environment absent",
			args: []string{"radioactive_ralph.test", desktopDispatchHelperArg},
		},
		{
			name:  "environment value wrong",
			args:  []string{"radioactive_ralph.test", desktopDispatchHelperArg},
			value: "0",
		},
		{
			name:  "helper argument absent",
			args:  []string{"radioactive_ralph.test"},
			value: "1",
		},
		{
			name:  "helper argument wrong",
			args:  []string{"radioactive_ralph.test", "-test.run=TestSomething"},
			value: "1",
		},
		{
			name:  "extra argument rejected",
			args:  []string{"radioactive_ralph.test", desktopDispatchHelperArg, "extra"},
			value: "1",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			getenv := func(key string) string {
				if key == desktopDispatchHelperEnv {
					return tt.value
				}
				return ""
			}
			if got := desktopDispatchHelperRequested(tt.args, getenv); got != tt.want {
				t.Errorf("desktopDispatchHelperRequested(%q, env=%q) = %t, want %t", tt.args, tt.value, got, tt.want)
			}
		})
	}
}

func TestRootDispatch_DesktopLauncherHelperProcess(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	t.Setenv(desktopDispatchHelperEnv, "1")
	cmd := exec.CommandContext(ctx, os.Args[0], desktopDispatchHelperArg)
	cmd.Env = os.Environ()
	out, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		t.Fatalf("desktop dispatch helper did not terminate: %v\n%s", ctx.Err(), out)
	}
	if err != nil {
		t.Fatalf("desktop dispatch helper: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), desktopDispatchHelperMarker) {
		t.Fatalf("desktop dispatch helper output = %q, want marker %q", out, desktopDispatchHelperMarker)
	}
}

func TestRootDispatch_DesktopLauncherErrorIsReturned(t *testing.T) {
	want := errors.New("desktop launch failed")
	root := newRootCmd(context.Background(), func(context.Context, *cobra.Command) (bool, error) {
		return true, want
	})
	root.SetArgs([]string{})

	if err := root.Execute(); !errors.Is(err, want) {
		t.Fatalf("root.Execute() error = %v, want %v", err, want)
	}
}

func desktopDispatchHelperRequested(args []string, getenv func(string) string) bool {
	return getenv(desktopDispatchHelperEnv) == "1" &&
		len(args) == 2 &&
		args[1] == desktopDispatchHelperArg
}

func runDesktopDispatchHelper() int {
	calls := 0
	root := newRootCmd(context.Background(), func(context.Context, *cobra.Command) (bool, error) {
		calls++
		return true, nil
	})
	// An explicit empty slice prevents Cobra from parsing the helper selector in
	// os.Args; TestMain already consumed that test-only process contract.
	root.SetArgs([]string{})

	if err := root.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "desktop dispatch helper execute: %v\n", err)
		return 1
	}
	if calls != 1 {
		fmt.Fprintf(os.Stderr, "desktop launcher calls = %d, want 1\n", calls)
		return 1
	}
	if _, err := fmt.Fprintln(os.Stdout, desktopDispatchHelperMarker); err != nil {
		fmt.Fprintf(os.Stderr, "desktop dispatch helper marker: %v\n", err)
		return 1
	}
	return 0
}
