"""Configuration loader for radioactive-ralph.

Uses pydantic-settings to reconcile four input sources, in priority order
(highest wins):

    1. Explicit constructor overrides (CLI flags via `RadioactiveRalphConfig(**cli_overrides)`)
    2. Environment variables prefixed with ``RALPH_`` (e.g. ``RALPH_DEFAULT_MODEL``)
    3. TOML file at ``~/.radioactive-ralph/config.toml`` (or ``$RALPH_CONFIG_PATH``)
    4. Built-in defaults (encoded on the model)

Every tunable in the orchestrator lives here. Any new tunable should be added as
a field on :class:`RadioactiveRalphConfig` — that automatically makes it configurable
through all four sources with zero extra glue code.
"""

from __future__ import annotations

import os
import tomllib
from pathlib import Path
from typing import Any

from pydantic import Field
from pydantic_settings import (
    BaseSettings,
    PydanticBaseSettingsSource,
    SettingsConfigDict,
)

__all__ = ["RadioactiveRalphConfig", "load_config"]

_DEFAULT_CONFIG_PATH = Path.home() / ".radioactive-ralph" / "config.toml"
_ENV_CONFIG_PATH_VAR = "RALPH_CONFIG_PATH"


def _resolve_toml_path() -> Path:
    """Return the TOML config path, honoring ``$RALPH_CONFIG_PATH``."""
    override = os.environ.get(_ENV_CONFIG_PATH_VAR)
    if override:
        return Path(override).expanduser()
    return _DEFAULT_CONFIG_PATH


class _TomlConfigSource(PydanticBaseSettingsSource):
    """Pydantic-settings source that reads from the resolved TOML path.

    We use a custom source rather than the built-in ``TomlConfigSettingsSource``
    so we can dynamically resolve the path from ``$RALPH_CONFIG_PATH`` at load
    time instead of locking it to a class attribute.
    """

    def __init__(self, settings_cls: type[BaseSettings]) -> None:
        super().__init__(settings_cls)
        self._data: dict[str, Any] | None = None

    def _load(self) -> dict[str, Any]:
        if self._data is not None:
            return self._data
        path = _resolve_toml_path()
        if not path.is_file():
            self._data = {}
            return self._data
        try:
            with open(path, "rb") as f:
                self._data = tomllib.load(f)
        except (OSError, tomllib.TOMLDecodeError):
            self._data = {}
        return self._data

    def get_field_value(
        self, field: Any, field_name: str
    ) -> tuple[Any, str, bool]:
        value = self._load().get(field_name)
        return value, field_name, value is not None

    def __call__(self) -> dict[str, Any]:
        return {k: v for k, v in self._load().items() if v is not None}


class RadioactiveRalphConfig(BaseSettings):
    """Parsed configuration for radioactive-ralph.

    Every field is a tunable. Every field can be set through:
      - the TOML file,
      - an environment variable (e.g. ``RALPH_DEFAULT_MODEL``),
      - or a constructor override (CLI flag).

    Add new fields here; they'll automatically flow through all input sources.
    """

    model_config = SettingsConfigDict(
        env_prefix="RALPH_",
        env_nested_delimiter="__",
        extra="ignore",
        case_sensitive=False,
    )

    # ── Portfolio ──────────────────────────────────────────────────────────
    orgs: dict[str, str] = Field(
        default_factory=lambda: {
            "arcade-cabinet": "~/src/arcade-cabinet",
            "jbcom": "~/src/jbcom",
            "jbdevprimary": "~/src/jbdevprimary",
        }
    )

    # ── Model tiering ──────────────────────────────────────────────────────
    bulk_model: str = "claude-haiku-4-5-20251001"
    default_model: str = "claude-sonnet-4-6"
    deep_model: str = "claude-opus-4-6"

    # ── Parallelism / pacing ───────────────────────────────────────────────
    max_parallel_agents: int = 5
    max_parallel_doc_sweep: int = 10
    agent_timeout_minutes: int = 30
    cycle_sleep_seconds: int = 30

    # ── Paths ──────────────────────────────────────────────────────────────
    state_path: str = ""

    # ── Attribution (credit Ralph on every commit / PR the daemon produces) ─
    attribution_enabled: bool = True
    attribution_text: str = (
        "🤖 Orchestrated by [radioactive-ralph]"
        "(https://github.com/jbcom/radioactive-ralph)"
    )
    attribution_footer_trailer: str = "Ralph-Orchestrated-By: radioactive-ralph"

    @classmethod
    def settings_customise_sources(
        cls,
        settings_cls: type[BaseSettings],
        init_settings: PydanticBaseSettingsSource,
        env_settings: PydanticBaseSettingsSource,
        dotenv_settings: PydanticBaseSettingsSource,
        file_secret_settings: PydanticBaseSettingsSource,
    ) -> tuple[PydanticBaseSettingsSource, ...]:
        """Priority order: init (CLI) > env vars > TOML file > defaults."""
        return (
            init_settings,
            env_settings,
            _TomlConfigSource(settings_cls),
            file_secret_settings,
        )

    # ── Helpers ────────────────────────────────────────────────────────────
    def resolve_state_path(self) -> Path:
        if self.state_path:
            return Path(self.state_path).expanduser()
        return Path.home() / ".radioactive-ralph" / "state.json"

    def pr_body_attribution(self) -> str:
        """Return the attribution block to append to PR descriptions."""
        if not self.attribution_enabled:
            return ""
        return f"\n\n---\n{self.attribution_text}\n"

    def commit_trailer(self) -> str:
        """Return a Git trailer line for commit messages (empty if disabled)."""
        return self.attribution_footer_trailer if self.attribution_enabled else ""

    def all_repo_paths(self) -> list[Path]:
        paths: list[Path] = []
        for org_path in self.orgs.values():
            expanded = Path(org_path).expanduser()
            if expanded.is_dir():
                for child in sorted(expanded.iterdir()):
                    if child.is_dir() and (child / ".git").exists():
                        paths.append(child)
        return paths


def load_config(path: Path | None = None, **overrides: Any) -> RadioactiveRalphConfig:
    """Load config with the full source stack.

    Args:
        path: Optional explicit TOML path. If given, sets ``$RALPH_CONFIG_PATH``
            for the duration of this call so the custom TOML source picks it up.
        **overrides: Keyword arguments that become the highest-priority init
            source (CLI flags, test fixtures, etc.).
    """
    if path is not None:
        # Route the explicit path through the env var that the TOML source reads,
        # so the same source stack is used for every load call.
        os.environ[_ENV_CONFIG_PATH_VAR] = str(path.expanduser())
    return RadioactiveRalphConfig(**overrides)
