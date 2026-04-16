# site/ — archival Astro prototype (not the live docs)

> **Not part of the live runtime.** This directory is a frozen
> prototype of an Astro/Starlight docs layout that was explored and
> then dropped.

The production documentation site builds from the repo-root
[`../docs/`](../docs/) tree with Sphinx + Shibuya, and GitHub Pages
publishes `docs/_build/html` via `tox -e docs`. See `../tox.ini` and
`.github/workflows/ci.yml` for the wiring.

Keep this directory only as an archival Astro/Starlight prototype
while there is still value in the old components or styling
experiments. Do **NOT**:

- Add new authored docs content here
- Point workflows or edit links back at this tree
- Copy installer or demo instructions from here into live docs

The one exception: `site/public/install.sh` is the live curl-pipe
installer that GitHub Pages serves. The rest of the directory is
not published.

When the live docs reach 1.0, `site/` will be deleted entirely
except for `public/install.sh`, which will move to the repo root.
