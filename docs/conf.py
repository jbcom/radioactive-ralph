"""Sphinx configuration for radioactive-ralph docs.

Uses sphinx-autodoc2 for automatic API reference generation from
Google-style docstrings across the entire ``radioactive_ralph`` package.
"""

project = "radioactive-ralph"
copyright = "2026, jbcom"
author = "jbcom"
release = "0.1.0"

extensions = [
    "sphinx.ext.napoleon",
    "autodoc2",
]

# autodoc2: generate API docs from the installed package
autodoc2_packages = ["../src/radioactive_ralph"]
autodoc2_render_plugin = "myst"
autodoc2_docstring_parser_regexes = [
    (r".*", "myst"),
]

# Napoleon (Google docstring) settings
napoleon_google_docstring = True
napoleon_numpy_docstring = False
napoleon_include_init_with_doc = True
napoleon_attr_annotations = True

html_theme = "sphinx_rtd_theme"
html_static_path = ["_static"]
html_logo = "../assets/ralph-mascot.png"

html_theme_options = {
    "logo_only": False,
    "display_version": True,
}
