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

This tree is source reference only. Its `package.json` and lockfile were
removed because a frozen package-manager surface falsely implied that the
prototype was supported and produced recurring dependency alerts. The last
installable snapshot remains recoverable from Git history, including commit
`394acab`.

The canonical curl installer now lives at [`../docs/install.sh`](../docs/install.sh).
Sphinx copies that file into the root of the Pages artifact, where it is served
as `https://jonbogaty.com/radioactive-ralph/install.sh`.

When the live docs reach 1.0, the remaining prototype source can be deleted in
one commit without losing its history.
