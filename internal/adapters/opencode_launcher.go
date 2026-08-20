package adapters

import (
	"bytes"
	"context"
	"crypto/rand"
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
	openCodeProcessScopeEnv         = "RADIOACTIVE_RALPH_PROCESS_SCOPE"
	managedOpenCodeLauncherFailure  = "Radioactive Ralph managed OpenCode launch failed."
	managedOpenCodeVerificationWait = "Radioactive Ralph verification is still pending."
	openCodeStopPollInterval        = 2 * time.Second
	openCodeStopPollAttempts        = 360
	openCodeStopPollTimeout         = 12 * time.Minute
	openCodeProviderReapTimeout     = 5 * time.Second
	maxOpenCodeAuthBytes            = 1 << 20
)

type openCodeStopPolling struct {
	interval         time.Duration
	progressInterval time.Duration
	attempts         int
	timeout          time.Duration
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
	// VerificationProgressInterval must remain inside the parent provider's
	// resolved stall lease so a healthy asynchronous verification is not reaped.
	VerificationProgressInterval time.Duration
}

// RunOpenCodeLauncher runs the real provider in a separately reclaimable
// process group plus a per-launch descendant scope. A genuine provider failure
// is returned unchanged after every descendant is gone. Only a successful,
// fully reaped managed run submits the
// synchronous Stop event and polls its finite status while verification runs;
// unavailable, timed-out, or failed verification exits through OpenCode's
// finite fail-closed protocol.
func RunOpenCodeLauncher(opts OpenCodeLaunchOptions) int {
	return runOpenCodeLauncher(opts, openCodeStopPolling{
		interval:         openCodeStopPollInterval,
		progressInterval: opts.VerificationProgressInterval,
		attempts:         openCodeStopPollAttempts,
		timeout:          openCodeStopPollTimeout,
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
	if managed && !managedOpenCodeProviderSupported() {
		_, _ = fmt.Fprintln(opts.Stderr, managedOpenCodeLauncherFailure)
		return 1
	}
	if polling.progressInterval == 0 {
		polling.progressInterval = openCodeStopPollInterval
	}
	if managed && polling.progressInterval < 0 {
		_, _ = fmt.Fprintln(opts.Stderr, managedOpenCodeLauncherFailure)
		return 1
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
	var runErr error
	if managed {
		var controlErr error
		runErr, controlErr = runManagedOpenCodeProvider(
			cmd, opts.Stdout, opts.Stderr, openCodeProviderReapTimeout,
		)
		if controlErr != nil {
			_, _ = fmt.Fprintln(opts.Stderr, managedOpenCodeLauncherFailure)
			return 1
		}
	} else {
		runErr = cmd.Run()
	}
	if runErr != nil {
		if exitErr, ok := runErr.(*exec.ExitError); ok {
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

func runManagedOpenCodeProvider(
	cmd *exec.Cmd, stdout, stderr io.Writer, outputDrainTimeout time.Duration,
) (runErr, controlErr error) {
	processScope := make([]byte, 32)
	if _, err := rand.Read(processScope); err != nil {
		return nil, fmt.Errorf("create managed provider process scope: %w", err)
	}
	cmd.Env = replaceEnvironment(cmd.Env, map[string]string{
		openCodeProcessScopeEnv: fmt.Sprintf("%x", processScope),
	}, nil)
	// The random scope is a process-lifetime capability, not provider input.
	// Keep only the encoded form needed by the child-environment tracker and do
	// not include it in errors, status payloads, or generated artifacts.
	processScopeValue := environmentLookup(cmd.Env)(openCodeProcessScopeEnv)
	clear(processScope)

	stdoutReader, stdoutWriter, err := os.Pipe()
	if err != nil {
		return nil, fmt.Errorf("create managed provider stdout pipe: %w", err)
	}
	stderrReader, stderrWriter, err := os.Pipe()
	if err != nil {
		_ = stdoutReader.Close()
		_ = stdoutWriter.Close()
		return nil, fmt.Errorf("create managed provider stderr pipe: %w", err)
	}
	copyResults := make(chan error, 2)
	go func() {
		_, copyErr := io.Copy(stdout, stdoutReader)
		copyResults <- copyErr
	}()
	go func() {
		_, copyErr := io.Copy(stderr, stderrReader)
		copyResults <- copyErr
	}()
	closePipes := func() {
		_ = stdoutReader.Close()
		_ = stderrReader.Close()
		_ = stdoutWriter.Close()
		_ = stderrWriter.Close()
	}

	cmd.Stdout, cmd.Stderr = stdoutWriter, stderrWriter
	configureOpenCodeProviderProcess(cmd)
	if err := cmd.Start(); err != nil {
		closePipes()
		return err, nil
	}
	// The child owns the write handles after Start. Closing Ralph's duplicates
	// lets the copy goroutines observe EOF after the direct child and every
	// reclaimed scoped descendant have closed their inherited descriptors.
	_ = stdoutWriter.Close()
	_ = stderrWriter.Close()
	// Observe provider exit without reaping its process-group leader. The
	// waitable leader anchors the numeric group identity until reclamation is
	// complete, so cleanup cannot signal an unrelated group after PID reuse.
	if err := waitOpenCodeProviderExit(cmd.Process); err != nil {
		cleanupErr := reclaimOpenCodeProviderProcess(cmd.Process, processScopeValue)
		runErr = cmd.Wait()
		closePipes()
		drainOpenCodeCopyResults(copyResults)
		if cleanupErr != nil {
			return runErr, fmt.Errorf("observe managed provider exit: %w; cleanup also failed: %v", err, cleanupErr)
		}
		return runErr, fmt.Errorf("observe managed provider exit: %w", err)
	}
	// A successful direct-child exit is not a completion boundary: tools may
	// have left background descendants in the provider process group. Reap and
	// prove that group absent before reaping the leader or running acceptance.
	if err := reclaimOpenCodeProviderProcess(cmd.Process, processScopeValue); err != nil {
		runErr = cmd.Wait()
		closePipes()
		drainOpenCodeCopyResults(copyResults)
		return runErr, err
	}
	runErr = cmd.Wait()

	timer := time.NewTimer(outputDrainTimeout)
	defer timer.Stop()
	for completed := 0; completed < 2; completed++ {
		select {
		case copyErr := <-copyResults:
			if copyErr != nil {
				closePipes()
				drainOpenCodeCopyResults(copyResults)
				return runErr, fmt.Errorf("copy managed provider output: %w", copyErr)
			}
		case <-timer.C:
			closePipes()
			drainOpenCodeCopyResults(copyResults)
			return runErr, fmt.Errorf("managed provider output remained open after cleanup")
		}
	}
	_ = stdoutReader.Close()
	_ = stderrReader.Close()
	return runErr, nil
}

// drainOpenCodeCopyResults consumes remaining copy goroutine results without
// blocking, so the caller can safely write to the shared stdout/stderr writers
// after runManagedOpenCodeProvider returns an error.
func drainOpenCodeCopyResults(copyResults chan error) {
	for {
		select {
		case <-copyResults:
		default:
			return
		}
	}
}

func pollOpenCodeStop(opts OpenCodeLaunchOptions, env []string, polling openCodeStopPolling) int {
	if polling.attempts <= 0 || polling.interval < 0 || polling.progressInterval <= 0 || polling.timeout < 0 {
		writeOpenCodeStatus(opts.Stdout, "supervisor_unavailable")
		return 2
	}
	pollCtx := opts.Context
	cancel := func() {}
	if polling.timeout > 0 {
		pollCtx, cancel = context.WithTimeout(opts.Context, polling.timeout)
	}
	defer cancel()
	// Start before the first Stop RPC. A local supervisor fault may consume the
	// hook client's own timeout, which must not leave a shorter provider stall
	// lease silent even before the first pending verdict arrives.
	progress := startOpenCodeVerificationProgress(opts.Stderr, polling.progressInterval)
	defer progress.stop()
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
	if err := copyOpenCodeAuth(opts.Env, dataHome); err != nil {
		return nil, fmt.Errorf("copy managed OpenCode authentication: %w", err)
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

// copyOpenCodeAuth preserves only OpenCode's documented credential file in
// the launch-private data root. Its contents never enter generated artifacts,
// process arguments, logs, or Ralph's finite failure protocol.
func copyOpenCodeAuth(env []string, privateDataHome string) error {
	lookup := environmentLookup(env)
	callerDataHome := lookup("XDG_DATA_HOME")
	if callerDataHome == "" {
		home := lookup("HOME")
		if home == "" {
			return nil
		}
		callerDataHome = filepath.Join(home, ".local", "share")
	}
	if !filepath.IsAbs(callerDataHome) {
		return fmt.Errorf("OpenCode authentication root is invalid")
	}
	sourcePath := filepath.Join(callerDataHome, "opencode", "auth.json")
	entry, err := os.Lstat(sourcePath) //nolint:gosec // operator-owned documented OpenCode credential path
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil || !entry.Mode().IsRegular() || entry.Size() < 0 || entry.Size() > maxOpenCodeAuthBytes {
		return fmt.Errorf("OpenCode authentication file is invalid")
	}
	source, err := os.Open(sourcePath) //nolint:gosec // validated documented OpenCode credential path
	if err != nil {
		return fmt.Errorf("open OpenCode authentication file")
	}
	defer func() { _ = source.Close() }()
	opened, err := source.Stat()
	if err != nil || !opened.Mode().IsRegular() || !os.SameFile(entry, opened) {
		return fmt.Errorf("OpenCode authentication file changed during validation")
	}

	targetDir := filepath.Join(privateDataHome, "opencode")
	if err := os.MkdirAll(targetDir, 0o700); err != nil {
		return fmt.Errorf("create private OpenCode data directory")
	}
	targetPath := filepath.Join(targetDir, "auth.json")
	target, err := os.OpenFile(targetPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600) //nolint:gosec // fixed child of fresh launch-private data root
	if err != nil {
		return fmt.Errorf("create private OpenCode authentication file")
	}
	copied, copyErr := io.Copy(target, io.LimitReader(source, maxOpenCodeAuthBytes+1))
	if copyErr == nil && copied != entry.Size() {
		copyErr = fmt.Errorf("OpenCode authentication file size changed")
	}
	if copyErr == nil {
		copyErr = target.Sync()
	}
	if closeErr := target.Close(); copyErr == nil {
		copyErr = closeErr
	}
	if copyErr != nil {
		return fmt.Errorf("copy private OpenCode authentication file")
	}
	return nil
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
