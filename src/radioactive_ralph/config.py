"""Load and parse radioactive-ralph configuration."""

from __future__ import annotations

import tomllib
from pathlib import Path

from .models import AutoloopConfig

_DEFAULT_CONFIG = AutoloopConfig(
    orgs={
        "arcade-cabinet": "~/src/arcade-cabinet",
        "jbcom": "~/src/jbcom",
        "jbdevprimary": "~/src/jbdevprimary",
    }
)


def load_config(path: Path) -> AutoloopConfig:
    """Load config from TOML file, returning defaults if file not found."""
    if not path.exists():
        return _DEFAULT_CONFIG
    with open(path, "rb") as f:
        data = tomllib.load(f)
    return AutoloopConfig.model_validate(data)
