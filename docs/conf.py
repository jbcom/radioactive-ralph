"""Sphinx configuration for radioactive-ralph docs.

Uses shibuya theme and sphinx-autoapi for automatic API reference generation.
"""

import os
import sys
from datetime import datetime

# Add src to path for autoapi
sys.path.insert(0, os.path.abspath("../src"))

project = "radioactive-ralph"
copyright = f"{datetime.now().year}, Jon Bogaty"
author = "Jon Bogaty"
release = "0.1.0"

extensions = [
    "sphinx.ext.autodoc",
    "sphinx.ext.napoleon",
    "sphinx.ext.viewcode",
    "autoapi.extension",
    "myst_parser",
]

# Shibuya theme options: lean into the "Radioactive Ralph" vibe
html_theme = "shibuya"
html_static_path = ["_static"]
html_logo = "../assets/ralph-mascot.png"
html_favicon = "../assets/ralph-mascot.png"

html_theme_options = {
    "accent_color": "magenta",
    "nav_links": [
        {"name": "GitHub", "url": "https://github.com/jbcom/radioactive-ralph"},
        {"name": "PyPI", "url": "https://pypi.org/project/radioactive-ralph/"},
    ],
    "announcement": "☢️ Ralph is awake and radioactive! ☢️",
}

# AutoAPI settings
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

# MyST settings
myst_enable_extensions = [
    "colon_fence",
    "deflist",
    "html_image",
]

# Napoleon settings
napoleon_google_docstring = True
napoleon_numpy_docstring = False

# Custom styling and fonts (vendored)
html_css_files = [
    "custom.css",
]

# Tell Sphinx where the source files are
source_suffix = {
    ".rst": "restructuredtext",
    ".md": "markdown",
}
