"""Sphinx configuration for radioactive-ralph docs."""

project = "radioactive-ralph"
copyright = "2026, jbcom"
author = "jbcom"
release = "0.1.0"

extensions = [
    "sphinx.ext.autodoc",
    "sphinx.ext.napoleon",
    "autoapi.extension",
]

autoapi_dirs = ["../src"]
autoapi_options = ["members", "undoc-members", "show-inheritance"]

html_theme = "sphinx_rtd_theme"
html_static_path = ["_static"]
html_logo = "../assets/ralph-mascot.png"

html_theme_options = {
    "logo_only": False,
    "display_version": True,
}
