#!/usr/bin/env bash
# Archived wrapper for the historical Python snapshot.
#
# The live demo tooling now lives at the repo root. Forward there so anyone
# following old paths still reaches the current tape/recording flow.

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
exec "${REPO_ROOT}/scripts/record-demo.sh" "$@"
