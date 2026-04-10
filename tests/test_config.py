"""Tests for the layered pydantic-settings config loader.

Covers the full priority stack the `load_config` helper promises:

    init (CLI overrides)  >  env vars (RALPH_*)  >  TOML file  >  defaults

Each test isolates ``$RALPH_CONFIG_PATH`` via ``monkeypatch`` and writes
any needed TOML fixture into ``tmp_path`` so no real user files are
touched.
"""

from __future__ import annotations

from pathlib import Path

import pytest

from radioactive_ralph.config import RadioactiveRalphConfig, load_config


@pytest.fixture(autouse=True)
def _clear_ralph_env(monkeypatch: pytest.MonkeyPatch) -> None:
    """Wipe any RALPH_* env vars so each test starts from a clean slate.

    Args:
        monkeypatch: Description of monkeypatch.

    Returns:
        Description of return value.

    """
    import os

    for key in list(os.environ):
        if key.startswith("RALPH_"):
            monkeypatch.delenv(key, raising=False)


@pytest.fixture
def toml_config(tmp_path: Path) -> Path:
    """Write a minimal TOML fixture and return its path.

    Args:
        tmp_path: Description of tmp_path.

    Returns:
        Description of return value.

    """
    cfg = tmp_path / "config.toml"
    cfg.write_text(
        'default_model = "claude-from-toml"\n'
        'bulk_model = "haiku-from-toml"\n'
        "max_parallel_agents = 7\n"
        "cycle_sleep_seconds = 99\n"
        "attribution_enabled = false\n"
        "[orgs]\n"
        'alpha = "~/src/alpha"\n'
        'beta = "~/src/beta"\n',
        encoding="utf-8",
    )
    return cfg


def test_defaults_when_no_toml_no_env(monkeypatch: pytest.MonkeyPatch, tmp_path: Path) -> None:
    """With no TOML and no env vars, defaults must win.

    Args:
        monkeypatch: Description of monkeypatch.
        tmp_path: Description of tmp_path.

    Returns:
        Description of return value.

    """
    monkeypatch.setenv("RALPH_CONFIG_PATH", str(tmp_path / "does-not-exist.toml"))
    cfg = RadioactiveRalphConfig()
    assert cfg.default_model == "claude-sonnet-4-6"
    assert cfg.bulk_model == "claude-haiku-4-5-20251001"
    assert cfg.deep_model == "claude-opus-4-6"
    assert cfg.max_parallel_agents == 5
    assert cfg.cycle_sleep_seconds == 30
    assert cfg.attribution_enabled is True


def test_toml_overrides_defaults(toml_config: Path, monkeypatch: pytest.MonkeyPatch) -> None:
    """A present TOML file should override the built-in defaults.

    Args:
        toml_config: Description of toml_config.
        monkeypatch: Description of monkeypatch.

    Returns:
        Description of return value.

    """
    monkeypatch.setenv("RALPH_CONFIG_PATH", str(toml_config))
    cfg = RadioactiveRalphConfig()
    assert cfg.default_model == "claude-from-toml"
    assert cfg.bulk_model == "haiku-from-toml"
    assert cfg.max_parallel_agents == 7
    assert cfg.cycle_sleep_seconds == 99
    assert cfg.attribution_enabled is False
    assert cfg.orgs == {"alpha": "~/src/alpha", "beta": "~/src/beta"}


def test_env_overrides_toml(toml_config: Path, monkeypatch: pytest.MonkeyPatch) -> None:
    """RALPH_DEFAULT_MODEL env var must beat the TOML file.

    Args:
        toml_config: Description of toml_config.
        monkeypatch: Description of monkeypatch.

    Returns:
        Description of return value.

    """
    monkeypatch.setenv("RALPH_CONFIG_PATH", str(toml_config))
    monkeypatch.setenv("RALPH_DEFAULT_MODEL", "claude-from-env")
    monkeypatch.setenv("RALPH_MAX_PARALLEL_AGENTS", "42")
    cfg = RadioactiveRalphConfig()
    assert cfg.default_model == "claude-from-env"
    assert cfg.max_parallel_agents == 42
    # TOML-only values still win over defaults:
    assert cfg.bulk_model == "haiku-from-toml"
    assert cfg.cycle_sleep_seconds == 99


def test_init_overrides_env_and_toml(
    toml_config: Path, monkeypatch: pytest.MonkeyPatch
) -> None:
    """Explicit constructor kwargs are the highest-priority source.

    Args:
        toml_config: Description of toml_config.
        monkeypatch: Description of monkeypatch.

    Returns:
        Description of return value.

    """
    monkeypatch.setenv("RALPH_CONFIG_PATH", str(toml_config))
    monkeypatch.setenv("RALPH_DEFAULT_MODEL", "claude-from-env")
    cfg = RadioactiveRalphConfig(default_model="claude-from-init", max_parallel_agents=1)
    assert cfg.default_model == "claude-from-init"
    assert cfg.max_parallel_agents == 1


def test_ralph_config_path_env_var_is_honored(
    tmp_path: Path, monkeypatch: pytest.MonkeyPatch
) -> None:
    """RALPH_CONFIG_PATH should redirect the TOML source to a custom file.

    Args:
        tmp_path: Description of tmp_path.
        monkeypatch: Description of monkeypatch.

    Returns:
        Description of return value.

    """
    custom = tmp_path / "custom.toml"
    custom.write_text('default_model = "from-custom"\n', encoding="utf-8")
    monkeypatch.setenv("RALPH_CONFIG_PATH", str(custom))
    cfg = RadioactiveRalphConfig()
    assert cfg.default_model == "from-custom"


def test_load_config_helper_sets_env_var(
    tmp_path: Path, monkeypatch: pytest.MonkeyPatch
) -> None:
    """load_config(path=...) routes through the RALPH_CONFIG_PATH env var.

    Args:
        tmp_path: Description of tmp_path.
        monkeypatch: Description of monkeypatch.

    Returns:
        Description of return value.

    """
    custom = tmp_path / "explicit.toml"
    custom.write_text('default_model = "from-explicit"\n', encoding="utf-8")
    cfg = load_config(custom)
    assert cfg.default_model == "from-explicit"


def test_load_config_overrides_win(tmp_path: Path, monkeypatch: pytest.MonkeyPatch) -> None:
    """load_config(**overrides) must bypass env + TOML for the named keys.

    Args:
        tmp_path: Description of tmp_path.
        monkeypatch: Description of monkeypatch.

    Returns:
        Description of return value.

    """
    custom = tmp_path / "explicit.toml"
    custom.write_text(
        'default_model = "toml-value"\nmax_parallel_agents = 3\n',
        encoding="utf-8",
    )
    monkeypatch.setenv("RALPH_DEFAULT_MODEL", "env-value")
    cfg = load_config(custom, default_model="cli-value")
    assert cfg.default_model == "cli-value"
    # max_parallel_agents not overridden → comes from TOML:
    assert cfg.max_parallel_agents == 3


def test_attribution_disabled_produces_empty_strings(
    toml_config: Path, monkeypatch: pytest.MonkeyPatch
) -> None:
    """When attribution_enabled=False, both helper strings collapse.

    Args:
        toml_config: Description of toml_config.
        monkeypatch: Description of monkeypatch.

    Returns:
        Description of return value.

    """
    monkeypatch.setenv("RALPH_CONFIG_PATH", str(toml_config))
    cfg = RadioactiveRalphConfig()
    assert cfg.attribution_enabled is False
    assert cfg.pr_body_attribution() == ""
    assert cfg.commit_trailer() == ""


def test_attribution_enabled_by_default(
    monkeypatch: pytest.MonkeyPatch, tmp_path: Path
) -> None:
    """When attribution is on, both helpers produce non-empty strings.

    Args:
        monkeypatch: Description of monkeypatch.
        tmp_path: Description of tmp_path.

    Returns:
        Description of return value.

    """
    monkeypatch.setenv("RALPH_CONFIG_PATH", str(tmp_path / "missing.toml"))
    cfg = RadioactiveRalphConfig()
    assert cfg.attribution_enabled is True
    assert "radioactive-ralph" in cfg.pr_body_attribution()
    assert cfg.commit_trailer() == "Ralph-Orchestrated-By: radioactive-ralph"


def test_resolve_state_path_defaults_to_home(
    monkeypatch: pytest.MonkeyPatch, tmp_path: Path
) -> None:
    """With no explicit state_path, resolve_state_path uses ~/.radioactive-ralph.

    Args:
        monkeypatch: Description of monkeypatch.
        tmp_path: Description of tmp_path.

    Returns:
        Description of return value.

    """
    monkeypatch.setenv("RALPH_CONFIG_PATH", str(tmp_path / "missing.toml"))
    cfg = RadioactiveRalphConfig()
    resolved = cfg.resolve_state_path()
    assert resolved.name == "state.json"
    assert ".radioactive-ralph" in str(resolved)


def test_resolve_state_path_expands_user(
    monkeypatch: pytest.MonkeyPatch, tmp_path: Path
) -> None:
    """An explicit state_path honoring ~ must be expanded.

    Args:
        monkeypatch: Description of monkeypatch.
        tmp_path: Description of tmp_path.

    Returns:
        Description of return value.

    """
    monkeypatch.setenv("RALPH_CONFIG_PATH", str(tmp_path / "missing.toml"))
    cfg = RadioactiveRalphConfig(state_path="~/.custom/state.json")
    resolved = cfg.resolve_state_path()
    assert "~" not in str(resolved)
    assert resolved.name == "state.json"


def test_malformed_toml_falls_back_to_defaults(
    tmp_path: Path, monkeypatch: pytest.MonkeyPatch
) -> None:
    """Parse errors in the TOML file are swallowed — defaults still apply.

    Args:
        tmp_path: Description of tmp_path.
        monkeypatch: Description of monkeypatch.

    Returns:
        Description of return value.

    """
    bad = tmp_path / "broken.toml"
    bad.write_text("this is not valid TOML = = =\n[[[", encoding="utf-8")
    monkeypatch.setenv("RALPH_CONFIG_PATH", str(bad))
    cfg = RadioactiveRalphConfig()
    assert cfg.default_model == "claude-sonnet-4-6"
