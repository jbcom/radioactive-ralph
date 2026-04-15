package doctor

import (
	"context"
	"fmt"
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
	out, err := withTimeout(ctx, 5*time.Second, func(ctx context.Context) (string, error) {
		return cfg.runCommand(ctx, "claude", "--version")
	})
	if err != nil {
		return Check{
			Name:      "claude",
			Severity:  FAIL,
			Detail:    "claude CLI not found on PATH",
			Remediate: "install Claude Code: `npm install -g @anthropic-ai/claude-code`",
		}
	}
	ver := parseVersion(out, "")
	if cfg.MinClaudeVersion != "" && !versionAtLeast(ver, cfg.MinClaudeVersion) {
		return Check{
			Name:      "claude",
			Severity:  WARN,
			Detail:    fmt.Sprintf("claude %s (newer recommended: ≥ %s)", ver, cfg.MinClaudeVersion),
			Remediate: "upgrade Claude Code: `npm update -g @anthropic-ai/claude-code`",
		}
	}
	return Check{Name: "claude", Severity: OK, Detail: "claude " + ver}
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

// checkMultiplexers reports on the multiplexer fallback chain (tmux →
// screen → setsid).
func checkMultiplexers(ctx context.Context, cfg RunOptions) Check {
	haveTmux := hasOnPath(ctx, cfg, "tmux")
	haveScreen := hasOnPath(ctx, cfg, "screen")

	switch {
	case haveTmux:
		return Check{Name: "multiplexer", Severity: OK, Detail: "tmux available (recommended)"}
	case haveScreen:
		return Check{
			Name:      "multiplexer",
			Severity:  WARN,
			Detail:    "tmux not found; screen will be used as fallback",
			Remediate: "install tmux for a better re-attach UX: `brew install tmux`",
		}
	default:
		// setsid is a syscall, not a binary — always available on POSIX.
		if runtime.GOOS == "windows" {
			return Check{
				Name:      "multiplexer",
				Severity:  FAIL,
				Detail:    "no multiplexer; Windows requires WSL2 for supervisor",
				Remediate: "run Ralph via WSL2 where tmux and POSIX setsid are available",
			}
		}
		return Check{
			Name:      "multiplexer",
			Severity:  WARN,
			Detail:    "neither tmux nor screen found; setsid fallback will be used (no re-attach UI)",
			Remediate: "install tmux for re-attach support: `brew install tmux`",
		}
	}
}
