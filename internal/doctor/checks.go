package doctor

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"time"
)

// checkGitVersion verifies git is installed at a high-enough version
// for worktree support (≥ 2.5.0 by default).
func checkGitVersion(ctx context.Context, cfg RunOptions) Check {
	out, err := withTimeout(ctx, 5*time.Second, func(ctx context.Context) (string, error) {
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
	_, err := withTimeout(ctx, 5*time.Second, func(ctx context.Context) (string, error) {
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
	_, err := withTimeout(ctx, 5*time.Second, func(ctx context.Context) (string, error) {
		return cfg.runCommand(ctx, "codex", "login", "status")
	})
	if err == nil {
		return Check{Name: "codex auth", Severity: OK, Detail: "authenticated"}
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

func checkGeminiVersion(ctx context.Context, cfg RunOptions) Check {
	return checkProviderVersion(ctx, cfg, providerVersionCheck{
		Name:          "gemini",
		Binary:        "gemini",
		VersionArgs:   []string{"--version"},
		MissingLevel:  WARN,
		MissingDetail: "gemini CLI not found on PATH",
		MissingFix:    "install Gemini CLI so the `gemini` provider binding is usable",
	})
}

func checkGeminiAuth(_ context.Context, _ RunOptions) Check {
	if os.Getenv("GEMINI_API_KEY") != "" || os.Getenv("GOOGLE_API_KEY") != "" {
		return Check{Name: "gemini auth", Severity: OK, Detail: "API key present in environment"}
	}
	return Check{
		Name:      "gemini auth",
		Severity:  WARN,
		Detail:    "Gemini auth could not be verified automatically",
		Remediate: "set GEMINI_API_KEY / GOOGLE_API_KEY or complete the Gemini CLI login flow before using the `gemini` provider",
	}
}

// checkGhVersion warns when the GitHub CLI is absent — present
// because most variants use `gh pr ...` internally, but non-fatal so
// Ralph can still run on machines without it.
func checkGhVersion(ctx context.Context, cfg RunOptions) Check {
	out, err := withTimeout(ctx, 5*time.Second, func(ctx context.Context) (string, error) {
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
	_, err := withTimeout(ctx, 5*time.Second, func(ctx context.Context) (string, error) {
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
	out, err := withTimeout(ctx, 5*time.Second, func(ctx context.Context) (string, error) {
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
