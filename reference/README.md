# reference/ — archival Python snapshot (not part of the live runtime)

> **Historical prototype.** This directory is frozen at v0.5.1 of the
> pre-Go Python implementation. It is **not** the live product, is
> **not** maintained, and is **not** shipped.

This directory contains the original Python implementation of radioactive-ralph
as it existed at v0.5.1. The project was rewritten in Go — see
[`../docs/plans/2026-04-14-radioactive-ralph-rewrite.prq.md`](../docs/plans/2026-04-14-radioactive-ralph-rewrite.prq.md)
for the rationale and the four-milestone plan, and
[`../docs/plans/m2-audit.md`](../docs/plans/m2-audit.md) for a
commit-by-commit map of what was ported, renamed, or intentionally
dropped.

Nothing here is maintained, supported, or part of the shipped product
contract. It is preserved to:

1. Keep git history navigable via `git log --follow reference/src/...`.
2. Give the Go rewrite a side-by-side reference for the preserved-idea
   pieces (Ralph's personality voice, variant vocabulary, work-priority
   thinking).

If you want the live product, use the repo root:

- docs: [`../docs/`](../docs/)
- installer: [`../docs/install.sh`](../docs/install.sh)
- demo helpers: [`../scripts/demo.tape`](../scripts/demo.tape) and [`../scripts/record-demo.sh`](../scripts/record-demo.sh)

The Python package manifest, lockfile, and tox configuration were removed
because they made this frozen snapshot look installable and supported. The
source, tests, type stubs, and historical scripts remain available for design
and implementation archaeology, but they are intentionally not a runnable
project.

The complete runnable snapshot remains recoverable from Git history. For
example, commit `394acab` contains:

```bash
git show 394acab:reference/pyproject.toml
git show 394acab:reference/uv.lock
git show 394acab:reference/tox.ini
```

When the Go rewrite reaches M4 (release of 1.0.0), this remaining source tree
will be deleted in one commit. The `radioactive-ralph` package on PyPI at 0.5.1
remains available for anyone who pinned to it; the 1.0.0 release will be the Go
binary only.

Do not copy install, demo, or packaging instructions from this tree into the
live docs. Treat them as historical artifacts unless they explicitly redirect
you back to the repo root.
