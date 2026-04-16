#!/usr/bin/env bash
# Archived installer stub for the Python reference snapshot.
#
# This file is intentionally not the live installer. The shipped product is the
# Go binary in the repo root. Use:
#   - site/public/install.sh
#   - docs/getting-started/index.md
#
# Keeping a short redirect here is safer than preserving the old plugin/MCP-era
# install flow, which no longer matches the product contract.

set -euo pipefail

cat >&2 <<'EOF'
reference/scripts/install.sh is archival only.

The live installer for radioactive-ralph is:
  site/public/install.sh

The live setup instructions are:
  docs/getting-started/index.md

This reference/ tree preserves the old Python implementation for history. It
is not the current install path and should not be used for the shipped product.
EOF

exit 1
