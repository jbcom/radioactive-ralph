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
- installer: [`../site/public/install.sh`](../site/public/install.sh)
- demo helpers: [`../scripts/demo.tape`](../scripts/demo.tape) and [`../scripts/record-demo.sh`](../scripts/record-demo.sh)

When the Go rewrite reaches M4 (release of 1.0.0), this entire directory
will be deleted in one commit. The `radioactive-ralph` package on PyPI
at 0.5.1 remains available for anyone who pinned to it; the 1.0.0 release
will be the Go binary only.

## Running the Python code (if you really need to)

```bash
cd reference
uv sync --all-extras
uv run --all-extras pytest
```

Not recommended. Most of the daemon logic here is stubbed with
`NotImplementedError` pointing at the PRD — the M1 PR that landed right
before the Go pivot removed the broken implementations.

Do not copy install, demo, or packaging instructions from this tree into the
live docs. Treat them as historical artifacts unless they explicitly redirect
you back to the repo root.
