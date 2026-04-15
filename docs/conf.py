"""Sphinx configuration for radioactive-ralph docs."""

from __future__ import annotations

import os
import subprocess
from pathlib import Path

project = "radioactive-ralph"
author = "Jon Bogaty"
copyright = f"2026, {author}"
html_title = project
html_baseurl = "https://jonbogaty.com/radioactive-ralph/"


def _release() -> str:
    if env := os.environ.get("DOCS_VERSION"):
        return env
    repo_root = Path(__file__).resolve().parent.parent
    try:
        result = subprocess.run(
            ["git", "describe", "--tags", "--always", "--dirty"],
            cwd=repo_root,
            check=True,
            capture_output=True,
            text=True,
        )
    except (FileNotFoundError, subprocess.CalledProcessError):
        return "dev"
    return result.stdout.strip() or "dev"


release = version = _release()

extensions = [
    "myst_parser",
    "sphinx.ext.githubpages",
]

source_suffix = {
    ".md": "markdown",
}

exclude_patterns = [
    "_build",
    "Thumbs.db",
    ".DS_Store",
]

html_theme = "shibuya"
html_logo = "_static/ralph-mascot.png"
html_favicon = "_static/ralph-mascot.png"
html_static_path = ["_static"]
html_css_files = ["custom.css"]

html_theme_options = {
    "accent_color": "green",
    "announcement": "☢️ Ralph dressed himself, and now he dressed the docs too. ☢️",
    "nav_links": [
        {"name": "Get started", "url": "https://jonbogaty.com/radioactive-ralph/getting-started/"},
        {"name": "Guides", "url": "https://jonbogaty.com/radioactive-ralph/guides/"},
        {"name": "Variants", "url": "https://jonbogaty.com/radioactive-ralph/variants/"},
        {"name": "Reference", "url": "https://jonbogaty.com/radioactive-ralph/reference/"},
        {"name": "API", "url": "https://jonbogaty.com/radioactive-ralph/api/"},
        {"name": "GitHub", "url": "https://github.com/jbcom/radioactive-ralph"},
    ],
}

myst_enable_extensions = [
    "colon_fence",
    "deflist",
    "html_image",
    "linkify",
]

myst_heading_anchors = 3

# The restored docs use page-title frontmatter heavily, and the API pages
# are generated from gomarkdoc markdown that emits unresolved symbol links.
suppress_warnings = [
    "myst.header",
    "myst.xref_missing",
    "misc.highlighting_failure",
]
