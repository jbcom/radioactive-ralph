#!/usr/bin/env python3
"""Archived docs validator stub for the Python reference snapshot.

The live documentation pipeline for radioactive-ralph lives at the repo root:

- bash scripts/validate-docs.sh
- python3 -m tox -e docs

This reference/ tree is preserved for historical context only. The old Python
docs validator no longer reflects the product contract and should not be used
as an authority for the shipped docs.
"""

from __future__ import annotations

import sys


def main() -> int:
    print(
        "reference/scripts/validate_docs.py is archival only.\n"
        "Use the repo-root docs pipeline instead:\n"
        "  - bash scripts/validate-docs.sh\n"
        "  - python3 -m tox -e docs",
        file=sys.stderr,
    )
    return 1


if __name__ == "__main__":
    raise SystemExit(main())
