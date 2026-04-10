"""Sphinx configuration for radioactive-ralph docs."""

from __future__ import annotations

import os
import sys
import tomllib
from datetime import datetime
from importlib.metadata import PackageNotFoundError
from importlib.metadata import version as package_version
from pathlib import Path

sys.path.insert(0, os.path.abspath("../src"))

project = "radioactive-ralph"
author = "Jon Bogaty"
copyright = f"{datetime.now().year}, {author}"
html_title = "radioactive-ralph"
html_baseurl = "https://jonbogaty.com/radioactive-ralph/"

try:
    release = package_version("radioactive-ralph")
except PackageNotFoundError:
    pyproject = Path(__file__).resolve().parent.parent / "pyproject.toml"
    try:
        release = tomllib.loads(pyproject.read_text())["project"]["version"]
    except (KeyError, tomllib.TOMLDecodeError) as exc:
        msg = f"Failed to read version from {pyproject.name}: {exc}"
        raise RuntimeError(msg) from exc
version = release

extensions = [
    "sphinx.ext.autodoc",
    "sphinx.ext.napoleon",
    "sphinx.ext.viewcode",
    "autoapi.extension",
    "myst_parser",
]

source_suffix = {
    ".rst": "restructuredtext",
    ".md": "markdown",
}

templates_path = ["_templates"]
exclude_patterns = [
    "_build",
    "Thumbs.db",
    ".DS_Store",
]

html_theme = "shibuya"
html_logo = "../assets/brand/ralph-mascot.png"
html_favicon = "../assets/brand/ralph-mascot.png"
html_static_path = ["_static"]
html_css_files = ["custom.css"]

html_theme_options = {
    "accent_color": "green",
    "announcement": "☢️ Ralph dressed himself, and now he dressed the docs too. ☢️",
    "nav_links": [
        {"name": "Get started", "url": "https://jonbogaty.com/radioactive-ralph/getting-started/"},
        {"name": "Variants", "url": "https://jonbogaty.com/radioactive-ralph/variants/"},
        {"name": "API", "url": "https://jonbogaty.com/radioactive-ralph/autoapi/"},
        {"name": "GitHub", "url": "https://github.com/jbcom/radioactive-ralph"},
        {"name": "LinkedIn", "url": "https://linkedin.com/in/jonbogaty"},
    ],
}

autoapi_dirs = ["../src/radioactive_ralph"]
autoapi_type = "python"
autoapi_template_dir = "_templates"
autoapi_options = [
    "members",
    "undoc-members",
    "show-inheritance",
    "show-module-summary",
    "special-members",
    "imported-members",
]
autoapi_member_order = "bysource"
autoapi_python_class_content = "both"
autoapi_keep_files = False
autoapi_add_toctree_entry = False

myst_enable_extensions = [
    "colon_fence",
    "deflist",
    "html_image",
]

napoleon_google_docstring = True
napoleon_numpy_docstring = False
