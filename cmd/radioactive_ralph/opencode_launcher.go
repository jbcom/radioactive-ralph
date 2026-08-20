package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/jbcom/radioactive-ralph/internal/adapters"
)

func maybeRunOpenCodeLauncher(args []string) (bool, int) {
	if len(args) < 3 || args[1] != "hook" || args[2] != "launch-opencode" {
		return false, 0
	}
	flags := flag.NewFlagSet("hook launch-opencode", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	var binary, adapterRoot, runtimeRoot string
	var verificationProgressInterval time.Duration
	flags.StringVar(&binary, "binary", "", "")
	flags.StringVar(&adapterRoot, "adapter-root", "", "")
	flags.StringVar(&runtimeRoot, "runtime-root", "", "")
	flags.DurationVar(
		&verificationProgressInterval,
		"verification-progress-interval",
		0,
		"",
	)
	if err := flags.Parse(args[3:]); err != nil || binary == "" {
		fmt.Fprintln(os.Stderr, "Radioactive Ralph managed OpenCode launch failed.")
		return true, 1
	}

	opts := adapters.OpenCodeLaunchOptions{
		Context:                      context.Background(),
		Binary:                       binary,
		Args:                         flags.Args(),
		Env:                          os.Environ(),
		Stdin:                        os.Stdin,
		Stdout:                       os.Stdout,
		Stderr:                       os.Stderr,
		VerificationProgressInterval: verificationProgressInterval,
	}
	managed := os.Getenv(adapters.ManagedSessionEnv) != "" &&
		os.Getenv(adapters.HookEndpointEnv) != ""
	if managed {
		bundle, err := adapters.ResolveCurrentBundle(adapterRoot)
		if err != nil || !launcherIsCurrentBundleExecutable(bundle.Executable) {
			fmt.Fprintln(os.Stderr, "Radioactive Ralph managed OpenCode launch failed.")
			return true, 1
		}
		runtimePaths, err := adapters.ResolveOpenCodeRuntime(bundle, runtimeRoot)
		if err != nil {
			fmt.Fprintln(os.Stderr, "Radioactive Ralph managed OpenCode launch failed.")
			return true, 1
		}
		opts.Plugin = bundle.OpenCodePlugin
		opts.Home = runtimePaths.Home
		opts.ConfigDir = runtimePaths.ConfigDir
	}
	return true, adapters.RunOpenCodeLauncher(opts)
}

func launcherIsCurrentBundleExecutable(want string) bool {
	running, err := os.Executable()
	if err != nil {
		return false
	}
	running, err = filepath.EvalSymlinks(running)
	if err != nil {
		return false
	}
	want, err = filepath.EvalSymlinks(want)
	return err == nil && running == want
}
