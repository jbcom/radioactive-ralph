package adapters

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	openCodeConfigContentEnv        = "OPENCODE_CONFIG_CONTENT"
	openCodeConfigDirEnv            = "OPENCODE_CONFIG_DIR"
	openCodeDisableProjectEnv       = "OPENCODE_DISABLE_PROJECT_CONFIG"
	openCodePureEnv                 = "OPENCODE_PURE"
	managedOpenCodeLauncherFailure  = "Radioactive Ralph managed OpenCode launch failed."
	managedOpenCodeVerificationWait = "Radioactive Ralph verification is still pending."
	openCodeStopPollInterval        = 2 * time.Second
	openCodeStopPollAttempts        = 360
	openCodeStopPollTimeout         = 12 * time.Minute
)

type openCodeStopPolling struct {
	interval time.Duration
	attempts int
	timeout  time.Duration
}

// OpenCodeLaunchOptions are the finite inputs to Ralph's managed OpenCode
// process wrapper. Provider arguments and streams pass through unchanged.
type OpenCodeLaunchOptions struct {
	Context   context.Context
	Binary    string
	Plugin    string
	Home      string
	ConfigDir string
	Args      []string
	Env       []string
	Stdin     io.Reader
	Stdout    io.Writer
	Stderr    io.Writer
}

// RunOpenCodeLauncher runs the real provider first. A genuine provider
// failure is returned unchanged. Only a successful managed run submits the
// synchronous Stop event and polls its finite status while verification runs;
// unavailable, timed-out, or failed verification exits through OpenCode's
// finite fail-closed protocol.
func RunOpenCodeLauncher(opts OpenCodeLaunchOptions) int {
	return runOpenCodeLauncher(opts, openCodeStopPolling{
		interval: openCodeStopPollInterval,
		attempts: openCodeStopPollAttempts,
		timeout:  openCodeStopPollTimeout,
	})
}

func runOpenCodeLauncher(opts OpenCodeLaunchOptions, polling openCodeStopPolling) int {
	if opts.Context == nil {
		opts.Context = context.Background()
	}
	if opts.Stdin == nil {
		opts.Stdin = os.Stdin
	}
	if opts.Stdout == nil {
		opts.Stdout = os.Stdout
	}
	if opts.Stderr == nil {
		opts.Stderr = os.Stderr
	}
	lookup := environmentLookup(opts.Env)
	sessionID, endpoint := lookup(ManagedSessionEnv), lookup(HookEndpointEnv)
	managed := sessionID != "" && endpoint != ""
	if (sessionID == "") != (endpoint == "") {
		_ = block("opencode", opts.Stdout, opts.Stderr, "invalid_event")
		return 2
	}
	if !filepath.IsAbs(opts.Binary) || !regularExecutable(opts.Binary) {
		_, _ = fmt.Fprintln(opts.Stderr, managedOpenCodeLauncherFailure)
		return 1
	}
	env := opts.Env
	if managed {
		var err error
		env, err = managedOpenCodeEnvironment(opts)
		if err != nil {
			_, _ = fmt.Fprintln(opts.Stderr, managedOpenCodeLauncherFailure)
			return 1
		}
	}
	cmd := exec.CommandContext(opts.Context, opts.Binary, opts.Args...) //nolint:gosec // binary is an absolute, operator-resolved provider binding
	cmd.Env = env
	cmd.Stdin = opts.Stdin
	cmd.Stdout = opts.Stdout
	cmd.Stderr = opts.Stderr
	if err := cmd.Run(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return providerExitCode(exitErr)
		}
		_, _ = fmt.Fprintln(opts.Stderr, managedOpenCodeLauncherFailure)
		return 1
	}
	if !managed {
		return 0
	}
	return pollOpenCodeStop(opts, env, polling)
}

func pollOpenCodeStop(opts OpenCodeLaunchOptions, env []string, polling openCodeStopPolling) int {
	if polling.attempts <= 0 || polling.interval < 0 || polling.timeout < 0 {
		writeOpenCodeStatus(opts.Stdout, "supervisor_unavailable")
		return 2
	}
	pollCtx := opts.Context
	cancel := func() {}
	if polling.timeout > 0 {
		pollCtx, cancel = context.WithTimeout(opts.Context, polling.timeout)
	}
	defer cancel()
	for attempt := 0; attempt < polling.attempts; attempt++ {
		var stdout, stderr bytes.Buffer
		err := RunHook(
			pollCtx, "opencode", "Stop",
			strings.NewReader(`{"hook_event_name":"Stop"}`),
			&stdout, &stderr, environmentLookup(env),
		)
		if err == nil {
			return 0
		}
		status := parseOpenCodeStatus(stdout.Bytes())
		if status != "verification_started" && status != "verification_pending" {
			writeOpenCodeStatus(opts.Stdout, status)
			return 2
		}
		if attempt == polling.attempts-1 {
			writeOpenCodeStatus(opts.Stdout, "verification_pending")
			return 2
		}
		_, _ = fmt.Fprintln(opts.Stderr, managedOpenCodeVerificationWait)
		timer := time.NewTimer(polling.interval)
		select {
		case <-pollCtx.Done():
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			writeOpenCodeStatus(opts.Stdout, "supervisor_unavailable")
			return 2
		case <-timer.C:
		}
	}
	writeOpenCodeStatus(opts.Stdout, "supervisor_unavailable")
	return 2
}

func parseOpenCodeStatus(raw []byte) string {
	var message struct {
		Status string `json:"status"`
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&message); err != nil || decoder.Decode(&struct{}{}) != io.EOF ||
		message.Status == "" || finiteOpenCodeStatus(message.Status) != message.Status {
		return "supervisor_unavailable"
	}
	return message.Status
}

func writeOpenCodeStatus(output io.Writer, status string) {
	encoded, _ := json.Marshal(struct {
		Status string `json:"status"`
	}{Status: finiteOpenCodeStatus(status)})
	_, _ = fmt.Fprintln(output, string(encoded))
}

func managedOpenCodeEnvironment(opts OpenCodeLaunchOptions) ([]string, error) {
	for _, path := range []string{opts.Plugin, opts.Home, opts.ConfigDir} {
		if !filepath.IsAbs(path) {
			return nil, fmt.Errorf("managed OpenCode path is not absolute")
		}
	}
	pluginInfo, err := os.Stat(opts.Plugin)
	if err != nil || !pluginInfo.Mode().IsRegular() {
		return nil, fmt.Errorf("managed OpenCode plugin is invalid")
	}
	for _, path := range []string{opts.Home, opts.ConfigDir} {
		info, statErr := os.Stat(path)
		if statErr != nil || !info.IsDir() {
			return nil, fmt.Errorf("managed OpenCode directory is invalid")
		}
	}
	if !safeManagedOpenCodeDirectories(opts.Home, opts.ConfigDir) {
		return nil, fmt.Errorf("managed OpenCode configuration is not isolated")
	}

	dataHome := filepath.Join(opts.Home, ".local", "share")
	cacheHome := filepath.Join(opts.Home, ".cache")
	stateHome := filepath.Join(opts.Home, ".local", "state")
	pluginURL := (&url.URL{Scheme: "file", Path: opts.Plugin}).String()
	config, err := json.Marshal(struct {
		Plugin []string `json:"plugin"`
	}{Plugin: []string{pluginURL}})
	if err != nil {
		return nil, fmt.Errorf("encode managed OpenCode config: %w", err)
	}

	replacements := map[string]string{
		"HOME":                    opts.Home,
		"XDG_CONFIG_HOME":         opts.ConfigDir,
		"XDG_DATA_HOME":           dataHome,
		"XDG_CACHE_HOME":          cacheHome,
		"XDG_STATE_HOME":          stateHome,
		openCodeConfigContentEnv:  string(config),
		openCodeConfigDirEnv:      opts.ConfigDir,
		openCodeDisableProjectEnv: "1",
	}
	remove := map[string]bool{
		"OPENCODE_CONFIG": true,
		openCodePureEnv:   true,
	}
	return replaceEnvironment(opts.Env, replacements, remove), nil
}

// OpenCode bootstraps package metadata and node_modules under its private
// config root. Those runtime files are allowed, but every documented config,
// extension, and compatible global-skill entry point is rejected. The only
// enabled plugin must therefore come from OPENCODE_CONFIG_CONTENT.
func safeManagedOpenCodeDirectories(home, config string) bool {
	for _, name := range []string{
		"config.json", "opencode.json", "opencode.jsonc",
		"plugin", "plugins", "command", "commands", "agent", "agents",
		"mode", "modes", "skill", "skills", "tool", "tools",
	} {
		if pathExistsOrUnknown(filepath.Join(config, name)) {
			return false
		}
	}
	for _, name := range []string{".opencode", ".claude", ".agents"} {
		if pathExistsOrUnknown(filepath.Join(home, name)) {
			return false
		}
	}
	return true
}

func pathExistsOrUnknown(path string) bool {
	_, err := os.Lstat(path) //nolint:gosec // read-only check of a fixed child under operator-selected managed roots
	return err == nil || !os.IsNotExist(err)
}

func regularExecutable(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular() && info.Mode().Perm()&0o111 != 0
}

func environmentLookup(env []string) Environment {
	values := make(map[string]string, len(env))
	for _, entry := range env {
		key, value, ok := strings.Cut(entry, "=")
		if ok {
			values[key] = value
		}
	}
	return func(key string) string { return values[key] }
}

func replaceEnvironment(env []string, replacements map[string]string, remove map[string]bool) []string {
	result := make([]string, 0, len(env)+len(replacements))
	for _, entry := range env {
		key, _, _ := strings.Cut(entry, "=")
		if _, replace := replacements[key]; replace || remove[key] {
			continue
		}
		result = append(result, entry)
	}
	keys := make([]string, 0, len(replacements))
	for key := range replacements {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		value := replacements[key]
		result = append(result, key+"="+value)
	}
	return result
}
