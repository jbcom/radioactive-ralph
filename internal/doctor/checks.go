package doctor

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"time"
)

const defaultCommandTimeout = 15 * time.Second

// checkGitVersion verifies git is installed at a high-enough version
// for worktree support (≥ 2.5.0 by default).
func checkGitVersion(ctx context.Context, cfg RunOptions) Check {
	out, err := withTimeout(ctx, cfg.CommandTimeout, func(ctx context.Context) (string, error) {
		return cfg.runCommand(ctx, "git", "--version")
	})
	if err != nil {
		return Check{
			Name:      "git",
			Severity:  FAIL,
			Detail:    "git not found on PATH",
			Remediate: "install git ≥ " + cfg.MinGitVersion + " (e.g. `brew install git` or your distro's package manager)",
		}
	}
	ver := parseVersion(out, "git version")
	if cfg.MinGitVersion != "" && !versionAtLeast(ver, cfg.MinGitVersion) {
		return Check{
			Name:      "git",
			Severity:  FAIL,
			Detail:    fmt.Sprintf("git %s (require ≥ %s for worktree support)", ver, cfg.MinGitVersion),
			Remediate: "upgrade git; worktree support requires ≥ 2.5.0",
		}
	}
	return Check{Name: "git", Severity: OK, Detail: "git " + ver}
}

// checkClaudeVersion verifies the Claude Code CLI is installed at a
// compatible version.
func checkClaudeVersion(ctx context.Context, cfg RunOptions) Check {
	return checkProviderVersion(ctx, cfg, providerVersionCheck{
		Name:          "claude",
		Binary:        "claude",
		VersionArgs:   []string{"--version"},
		MinVersion:    cfg.MinClaudeVersion,
		MissingLevel:  FAIL,
		MissingDetail: "claude CLI not found on PATH",
		MissingFix:    "install Claude Code: `npm install -g @anthropic-ai/claude-code`",
		UpgradeFix:    "upgrade Claude Code: `npm update -g @anthropic-ai/claude-code`",
	})
}

func checkClaudeAuth(ctx context.Context, cfg RunOptions) Check {
	if os.Getenv("ANTHROPIC_API_KEY") != "" {
		return Check{Name: "claude auth", Severity: OK, Detail: "ANTHROPIC_API_KEY present in environment"}
	}
	_, err := withTimeout(ctx, cfg.CommandTimeout, func(ctx context.Context) (string, error) {
		return cfg.runCommand(ctx, "claude", "auth", "status")
	})
	if err != nil {
		return Check{
			Name:      "claude auth",
			Severity:  WARN,
			Detail:    "claude CLI is not authenticated",
			Remediate: "run `claude auth login`",
		}
	}
	return Check{Name: "claude auth", Severity: OK, Detail: "authenticated"}
}

func checkCodexVersion(ctx context.Context, cfg RunOptions) Check {
	return checkProviderVersion(ctx, cfg, providerVersionCheck{
		Name:          "codex",
		Binary:        "codex",
		VersionArgs:   []string{"--version"},
		MissingLevel:  WARN,
		MissingDetail: "codex CLI not found on PATH",
		MissingFix:    "install Codex CLI so the `codex` provider binding is usable",
	})
}

func checkCodexAuth(ctx context.Context, cfg RunOptions) Check {
	_, err := withTimeout(ctx, cfg.CommandTimeout, func(ctx context.Context) (string, error) {
		return cfg.runCommand(ctx, "codex", "login", "status")
	})
	if err == nil {
		return Check{Name: "codex auth", Severity: OK, Detail: "authenticated"}
	}
	if errors.Is(err, exec.ErrNotFound) {
		return Check{
			Name:      "codex auth",
			Severity:  WARN,
			Detail:    "codex CLI not found on PATH; auth check skipped",
			Remediate: "install Codex CLI so the `codex` provider binding is usable",
		}
	}
	if os.Getenv("OPENAI_API_KEY") != "" {
		return Check{
			Name:      "codex auth",
			Severity:  WARN,
			Detail:    "OPENAI_API_KEY is present, but the codex CLI is not logged in",
			Remediate: "run `printenv OPENAI_API_KEY | codex login --with-api-key`",
		}
	}
	return Check{
		Name:      "codex auth",
		Severity:  WARN,
		Detail:    "codex CLI is not authenticated",
		Remediate: "run `codex login`",
	}
}

// checkCodexMetering surfaces a known observability limitation so an operator
// who configures a codex spend cap understands it will not be enforced. Unlike
// claude/opencode (which emit stream-json usage frames the runtime parses into
// token/cost), codex has no stable machine-readable usage stream, so its
// per-turn cost is not metered and cannot count against a spend cap. This is
// informational (OK), not a fault — it exists so the gap is discoverable rather
// than silent.
func checkCodexMetering(_ context.Context, _ RunOptions) Check {
	// The guidance lives in Detail, not Remediate: WriteText only prints Remediate
	// for non-OK checks, and this check is deliberately OK, so an OK-check's
	// Remediate would never reach the operator.
	return Check{
		Name:     "codex metering",
		Severity: OK,
		Detail: "codex usage/cost is not metered (its CLI has no machine-readable usage stream), " +
			"so a codex spend cap is not enforced — cap codex spend at the OpenAI account level if you " +
			"need a hard limit; claude/opencode usage IS metered",
	}
}

// checkOpencodeVersion warns when the opencode CLI is absent. opencode is a
// first-class supported provider (a NativeFanout-capable local agent CLI),
// so doctor reports on it alongside claude and codex.
func checkOpencodeVersion(ctx context.Context, cfg RunOptions) Check {
	return checkProviderVersion(ctx, cfg, providerVersionCheck{
		Name:          "opencode",
		Binary:        "opencode",
		VersionArgs:   []string{"--version"},
		MissingLevel:  WARN,
		MissingDetail: "opencode CLI not found on PATH",
		MissingFix:    "install opencode so the `opencode` provider binding is usable",
	})
}

// checkGhVersion warns when the GitHub CLI is absent — present
// because most variants use `gh pr ...` internally, but non-fatal so
// Ralph can still run on machines without it.
func checkGhVersion(ctx context.Context, cfg RunOptions) Check {
	out, err := withTimeout(ctx, cfg.CommandTimeout, func(ctx context.Context) (string, error) {
		return cfg.runCommand(ctx, "gh", "--version")
	})
	if err != nil {
		return Check{
			Name:      "gh",
			Severity:  WARN,
			Detail:    "gh CLI not found on PATH",
			Remediate: "install GitHub CLI for PR/forge operations: `brew install gh` or https://cli.github.com",
		}
	}
	return Check{Name: "gh", Severity: OK, Detail: "gh " + parseVersion(out, "gh version")}
}

// checkGhAuth verifies `gh auth status` reports authenticated.
func checkGhAuth(ctx context.Context, cfg RunOptions) Check {
	_, err := withTimeout(ctx, cfg.CommandTimeout, func(ctx context.Context) (string, error) {
		return cfg.runCommand(ctx, "gh", "auth", "status")
	})
	if err != nil {
		return Check{
			Name:      "gh auth",
			Severity:  WARN,
			Detail:    "gh CLI not authenticated",
			Remediate: "run `gh auth login`",
		}
	}
	return Check{Name: "gh auth", Severity: OK, Detail: "authenticated"}
}

// checkServicePlatform reports whether the current platform supports the
// durable repo service contract.
func checkServicePlatform(_ context.Context, _ RunOptions) Check {
	switch runtime.GOOS {
	case "darwin":
		return Check{Name: "service platform", Severity: OK, Detail: "macOS launchd + Unix sockets supported"}
	case "linux":
		return Check{Name: "service platform", Severity: OK, Detail: "Linux systemd-user + Unix sockets supported"}
	case "windows":
		return Check{Name: "service platform", Severity: OK, Detail: "native Windows SCM + named pipes supported"}
	default:
		return Check{
			Name:      "service platform",
			Severity:  FAIL,
			Detail:    fmt.Sprintf("%s is not a supported durable-service platform", runtime.GOOS),
			Remediate: "use macOS, Linux, or native Windows for the full repo-service runtime",
		}
	}
}

type providerVersionCheck struct {
	Name          string
	Binary        string
	VersionArgs   []string
	MinVersion    string
	MissingLevel  Severity
	MissingDetail string
	MissingFix    string
	UpgradeFix    string
}

func checkProviderVersion(ctx context.Context, cfg RunOptions, check providerVersionCheck) Check {
	out, err := withTimeout(ctx, cfg.CommandTimeout, func(ctx context.Context) (string, error) {
		return cfg.runCommand(ctx, check.Binary, check.VersionArgs...)
	})
	if err != nil {
		return Check{
			Name:      check.Name,
			Severity:  check.MissingLevel,
			Detail:    check.MissingDetail,
			Remediate: check.MissingFix,
		}
	}
	ver := parseVersion(out, "")
	if check.MinVersion != "" && !versionAtLeast(ver, check.MinVersion) {
		return Check{
			Name:      check.Name,
			Severity:  WARN,
			Detail:    fmt.Sprintf("%s %s (newer recommended: ≥ %s)", check.Name, ver, check.MinVersion),
			Remediate: check.UpgradeFix,
		}
	}
	return Check{Name: check.Name, Severity: OK, Detail: check.Name + " " + ver}
}
