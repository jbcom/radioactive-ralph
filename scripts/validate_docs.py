from __future__ import annotations

import json
import re
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
README = ROOT / "README.md"
PLUGIN = ROOT / ".claude-plugin" / "plugin.json"
PYPROJECT = ROOT / "pyproject.toml"
DISALLOWED_PATTERNS = {
    r"docs/content": "remove docs/content shadow-tree references",
    r"content/skills": "use docs/variants instead of copied content/skills paths",
    r"jbcom\.github\.io/radioactive-ralph": "use the canonical jonbogaty.com docs domain",
    r"jonbogaty\.com/radioactive-ralph/_images": (
        "README/docs assets should not depend on generated _images paths"
    ),
}
SCAN_SUFFIXES = {".md", ".py", ".toml", ".json", ".yml", ".yaml"}


def iter_files() -> list[Path]:
    paths: list[Path] = []
    for path in ROOT.rglob("*"):
        if not path.is_file() or path.suffix not in SCAN_SUFFIXES:
            continue
        ignored_parts = {".git", ".mypy_cache", ".pytest_cache", "__pycache__", "_build"}
        if any(part in ignored_parts for part in path.parts):
            continue
        if path == Path(__file__).resolve():
            continue
        paths.append(path)
    return paths


def project_version() -> str:
    for line in PYPROJECT.read_text().splitlines():
        if line.startswith("version = "):
            return line.split("=", 1)[1].strip().strip('"')
    raise RuntimeError("Could not find project version in pyproject.toml")


def plugin_version() -> str:
    return json.loads(PLUGIN.read_text())["version"]


def main() -> int:
    errors: list[str] = []

    required_paths = [
        README,
        ROOT / "assets" / "brand" / "ralph-mascot.png",
        ROOT / "docs" / "index.md",
        ROOT / "docs" / "getting-started" / "index.md",
        ROOT / "docs" / "variants" / "index.md",
        ROOT / "skills" / "README.md",
    ]
    for path in required_paths:
        if not path.exists():
            errors.append(f"Missing required docs asset: {path.relative_to(ROOT)}")

    if (ROOT / "docs" / "content").exists():
        errors.append("docs/content should not exist")

    if project_version() != plugin_version():
        errors.append(".claude-plugin/plugin.json version does not match pyproject.toml")

    for path in iter_files():
        text = path.read_text()
        for pattern, message in DISALLOWED_PATTERNS.items():
            if re.search(pattern, text):
                errors.append(f"{path.relative_to(ROOT)}: {message}")

    if errors:
        print("Documentation validation failed:", file=sys.stderr)
        for error in errors:
            print(f"- {error}", file=sys.stderr)
        return 1

    print("Documentation validation passed.")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
