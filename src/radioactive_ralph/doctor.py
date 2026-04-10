"""Health check runner for radioactive-ralph.

Runs diagnostic checks verifying the daemon's environment: Python version,
required CLIs, API credentials, config file, configured repos, state file,
and the optional Claude Code plugin. Each check returns a DoctorResult.

Called from ``ralph doctor``. Nothing here prints — the CLI owns output.
"""

from __future__ import annotations

import json
import os
import re
import shutil
import subprocess
import sys
import tomllib
from dataclasses import dataclass, field
from pathlib import Path

from radioactive_ralph.config import RadioactiveRalphConfig, load_config

OK = "OK"
WARN = "WARN"
FAIL = "FAIL"


@dataclass
class DoctorResult:
    """Result of a single diagnostic check."""

    name: str
    status: str
    detail: str
    fix: str = ""

    def to_dict(self) -> dict[str, str]:
        """Convert the result to a dictionary.

        Returns:
            A dictionary representation of the result.
        """
        return {"name": self.name, "status": self.status, "detail": self.detail, "fix": self.fix}


@dataclass
class DoctorReport:
    """Aggregated results of all doctor checks."""

    results: list[DoctorResult] = field(default_factory=list)

    @property
    def failed(self) -> int:
        """Get the number of failed checks.

        Returns:
            The count of checks with FAIL status.
        """
        return sum(1 for r in self.results if r.status == FAIL)

    @property
    def warnings(self) -> int:
        """Get the number of warning checks.

        Returns:
            The count of checks with WARN status.
        """
        return sum(1 for r in self.results if r.status == WARN)

    @property
    def ok(self) -> bool:
        """Check if all checks passed without failures.

        Returns:
            True if there are no failures, False otherwise.
        """
        return self.failed == 0

    def to_dict(self) -> dict[str, object]:
        """Convert the report to a dictionary.

        Returns:
            A dictionary containing all check results and a summary.
        """
        return {
            "checks": [r.to_dict() for r in self.results],
            "summary": {
                "total": len(self.results),
                "failed": self.failed,
                "warnings": self.warnings,
                "ok": self.ok,
            },
        }


def _run(cmd: list[str], timeout: int = 10) -> subprocess.CompletedProcess[str]:
    """Run a command without raising. Returns CompletedProcess even on failure.

    Args:
        cmd: The command to run as a list of strings.
        timeout: The timeout in seconds.

    Returns:
        A CompletedProcess instance.
    """
    try:
        return subprocess.run(cmd, capture_output=True, text=True, check=False, timeout=timeout)
    except (FileNotFoundError, subprocess.TimeoutExpired) as exc:
        return subprocess.CompletedProcess(cmd, returncode=127, stdout="", stderr=str(exc))


def _which(binary: str) -> str | None:
    """Find the path to an executable binary.

    Args:
        binary: The name of the binary to find.

    Returns:
        The absolute path to the binary, or None if not found.
    """
    return shutil.which(binary)


def check_python() -> DoctorResult:
    """Check if the Python version meets requirements.

    Returns:
        A DoctorResult with the check status.
    """
    major, minor, patch = sys.version_info[:3]
    ver = f"{major}.{minor}.{patch}"
    if (major, minor) >= (3, 12):
        return DoctorResult("Python", OK, f"Python {ver}")
    return DoctorResult(
        "Python", FAIL, f"Python {ver} (need >= 3.12)", "Install Python 3.12+ via pyenv/uv."
    )


def check_uv() -> DoctorResult:
    """Check if uv is installed and working.

    Returns:
        A DoctorResult with the check status.
    """
    if _which("uv") is None:
        return DoctorResult(
            "uv", WARN, "not found on PATH",
            "Install: curl -LsSf https://astral.sh/uv/install.sh | sh",
        )
    proc = _run(["uv", "--version"])
    if proc.returncode != 0:
        return DoctorResult("uv", WARN, "installed but --version failed")
    return DoctorResult("uv", OK, proc.stdout.strip() or "installed")


def check_git() -> DoctorResult:
    """Check if git is installed and meets version requirements.

    Returns:
        A DoctorResult with the check status.
    """
    if _which("git") is None:
        return DoctorResult("git", FAIL, "not found on PATH", "Install git (brew install git).")
    proc = _run(["git", "--version"])
    out = proc.stdout.strip() or "installed"
    m = re.search(r"(\d+)\.(\d+)", out)
    if m and (int(m.group(1)), int(m.group(2))) < (2, 30):
        return DoctorResult("git", WARN, f"{out} (upgrade to >= 2.30)", "brew upgrade git")
    return DoctorResult("git", OK, out)


def check_gh() -> DoctorResult:
    """Check if the GitHub CLI (gh) is installed and authenticated.

    Returns:
        A DoctorResult with the check status.
    """
    if _which("gh") is None:
        return DoctorResult("gh CLI", FAIL, "not found on PATH", "brew install gh")
    proc = _run(["gh", "auth", "status"])
    if proc.returncode != 0:
        msg = (proc.stderr or proc.stdout or "not authenticated").strip().splitlines()[0]
        return DoctorResult(
            "gh CLI", FAIL, f"not authenticated ({msg[:80]})", "Run: gh auth login"
        )
    return DoctorResult("gh CLI", OK, "installed and authenticated")


def check_claude() -> DoctorResult:
    """Check if the Claude CLI is installed.

    Returns:
        A DoctorResult with the check status.
    """
    if _which("claude") is None:
        return DoctorResult(
            "claude CLI", FAIL, "not found on PATH",
            "Install Claude Code: https://docs.claude.com/claude-code",
        )
    proc = _run(["claude", "--version"])
    if proc.returncode != 0:
        return DoctorResult("claude CLI", WARN, "installed but --version failed")
    return DoctorResult("claude CLI", OK, proc.stdout.strip() or "installed")


def check_api_key() -> DoctorResult:
    """Check if the ANTHROPIC_API_KEY environment variable is set.

    Returns:
        A DoctorResult with the check status.
    """
    if os.environ.get("ANTHROPIC_API_KEY"):
        return DoctorResult("ANTHROPIC_API_KEY", OK, "set in environment")
    return DoctorResult(
        "ANTHROPIC_API_KEY", FAIL, "not set",
        "Export ANTHROPIC_API_KEY=sk-ant-... in your shell profile.",
    )


def check_config(config_path: Path) -> tuple[DoctorResult, RadioactiveRalphConfig | None]:
    """Check if the configuration file is valid and readable.

    Args:
        config_path: The path to the configuration file.

    Returns:
        A tuple containing the DoctorResult and the parsed config (or None).
    """
    if not config_path.exists():
        try:
            cfg = load_config(config_path)
        except Exception:
            cfg = None
        return (
            DoctorResult(
                "Config file", FAIL, f"{config_path} not found",
                f"Create {config_path} with at least one [orgs] entry.",
            ),
            cfg,
        )
    # Parse directly so TOML errors surface (load_config silently swallows them).
    try:
        with open(config_path, "rb") as f:
            tomllib.load(f)
    except tomllib.TOMLDecodeError as exc:
        return (
            DoctorResult("Config file", FAIL, f"TOML parse error: {exc}", "Fix TOML syntax."),
            None,
        )
    except OSError as exc:
        return (DoctorResult("Config file", FAIL, f"cannot read {config_path}: {exc}"), None)
    try:
        cfg = load_config(config_path)
    except Exception as exc:
        return (
            DoctorResult(
                "Config file", FAIL, f"validation error: {type(exc).__name__}: {exc}",
                "Fix schema errors in config.toml.",
            ),
            None,
        )
    if not cfg.orgs:
        return (
            DoctorResult(
                "Config file", WARN, "no orgs configured", "Add at least one [orgs] entry."
            ),
            cfg,
        )
    return (DoctorResult("Config file", OK, f"{len(cfg.orgs)} org(s) at {config_path}"), cfg)


def _check_single_repo(repo_path: Path) -> DoctorResult:
    """Check the health of a single local repository.

    Args:
        repo_path: The path to the repository.

    Returns:
        A DoctorResult with the check status.
    """
    name = f"repo: {repo_path.name}"
    if not repo_path.exists():
        return DoctorResult(name, FAIL, f"{repo_path} missing", "Clone or fix config path.")
    if not (repo_path / ".git").exists():
        return DoctorResult(name, FAIL, f"{repo_path} is not a git repo")
    remote = _run(["git", "-C", str(repo_path), "remote", "get-url", "origin"])
    if remote.returncode != 0 or not remote.stdout.strip():
        return DoctorResult(name, WARN, "no 'origin' remote", "git remote add origin <url>")
    url = remote.stdout.strip()
    if "github.com" in url and _which("gh") is not None:
        m = re.search(r"[:/]([^/]+/[^/]+?)(?:\.git)?$", url)
        if m:
            slug = m.group(1)
            reach = _run(["gh", "repo", "view", slug, "--json", "name"], timeout=15)
            if reach.returncode != 0:
                return DoctorResult(
                    name, WARN, f"remote {slug} unreachable via gh",
                    "Check gh auth and network.",
                )
    return DoctorResult(name, OK, url)


def check_repos(cfg: RadioactiveRalphConfig | None) -> list[DoctorResult]:
    """Check all configured repositories.

    Args:
        cfg: The parsed configuration object.

    Returns:
        A list of DoctorResult instances for each repository.
    """
    if cfg is None:
        return []
    paths = cfg.all_repo_paths()
    if not paths:
        return [
            DoctorResult(
                "Configured repos", WARN, "no repos under configured org paths",
                "Ensure org directories contain git repos.",
            )
        ]
    return [_check_single_repo(p) for p in paths]


def _resolved_state_path(cfg: RadioactiveRalphConfig | None) -> Path:
    """Check all configured repositories.

    Args:
        cfg: The parsed configuration object.

    Returns:
        A list of DoctorResult instances for each repository.
    """
    if cfg is None:
        return Path.home() / ".radioactive-ralph" / "state.json"
    return cfg.resolve_state_path()


def check_state_file(cfg: RadioactiveRalphConfig | None) -> DoctorResult:
    """Check all configured repositories.

    Args:
        cfg: The parsed configuration object.

    Returns:
        A list of DoctorResult instances for each repository.
    """
    path = _resolved_state_path(cfg)
    if not path.exists():
        return DoctorResult("State file", WARN, f"{path} not yet created")
    try:
        raw = path.read_text(encoding="utf-8")
        if raw.strip():
            json.loads(raw)
    except OSError as exc:
        return DoctorResult("State file", FAIL, f"cannot read {path}: {exc}", "Check permissions.")
    except json.JSONDecodeError as exc:
        return DoctorResult(
            "State file", FAIL, f"{path} not valid JSON: {exc}", "Remove or repair state file."
        )
    return DoctorResult("State file", OK, f"{path} valid")


def check_write_permissions(cfg: RadioactiveRalphConfig | None) -> DoctorResult:
    """Check all configured repositories.

    Args:
        cfg: The parsed configuration object.

    Returns:
        A list of DoctorResult instances for each repository.
    """
    parent = _resolved_state_path(cfg).parent
    try:
        parent.mkdir(parents=True, exist_ok=True)
    except OSError as exc:
        return DoctorResult(
            "Write permissions", FAIL, f"cannot create {parent}: {exc}",
            f"Create {parent} manually.",
        )
    if not os.access(parent, os.W_OK):
        return DoctorResult(
            "Write permissions", FAIL, f"{parent} not writable", f"chmod u+w {parent}"
        )
    return DoctorResult("Write permissions", OK, f"{parent} writable")


def check_plugin() -> DoctorResult:
    """Informational: whether radioactive-ralph is installed as a Claude plugin.

    Returns:
        A DoctorResult with the check status.
    """
    if _which("claude") is None:
        return DoctorResult("Claude plugin", WARN, "claude CLI not found; skipped")
    proc = _run(["claude", "plugin", "list"], timeout=15)
    if proc.returncode != 0:
        return DoctorResult("Claude plugin", WARN, "could not run `claude plugin list`")
    combined = (proc.stdout or "") + (proc.stderr or "")
    if "radioactive-ralph" in combined:
        return DoctorResult("Claude plugin", OK, "radioactive-ralph plugin installed")
    return DoctorResult(
        "Claude plugin", WARN, "plugin not installed (informational)",
        "Optional: claude plugin install radioactive-ralph",
    )


def run_all_checks(config_path: Path) -> DoctorReport:
    """Run every diagnostic check and return an aggregated report.

    Args:
        config_path: The path to the configuration file.

    Returns:
        A DoctorReport containing the results of all checks.
    """
    report = DoctorReport()
    for fn in (check_python, check_uv, check_git, check_gh, check_claude, check_api_key):
        report.results.append(fn())
    cfg_result, cfg = check_config(config_path)
    report.results.append(cfg_result)
    report.results.extend(check_repos(cfg))
    report.results.append(check_state_file(cfg))
    report.results.append(check_write_permissions(cfg))
    report.results.append(check_plugin())
    return report
