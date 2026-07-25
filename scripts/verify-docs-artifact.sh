#!/usr/bin/env bash

set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

if [[ $# -ne 1 ]]; then
  echo "usage: verify-docs-artifact.sh <built-docs-directory>" >&2
  exit 2
fi

artifact_dir="$1"
if [[ "$artifact_dir" != /* ]]; then
  artifact_dir="$ROOT/$artifact_dir"
fi

source_installer="$ROOT/docs/install.sh"
built_installer="$artifact_dir/install.sh"

if [[ ! -f "$built_installer" ]]; then
  echo "docs artifact validation: missing $built_installer" >&2
  exit 1
fi

if ! cmp -s "$source_installer" "$built_installer"; then
  echo "docs artifact validation: install.sh differs from its canonical source" >&2
  exit 1
fi

grep -Fq 'REPO="jbcom/radioactive-ralph"' "$built_installer"
grep -Fq 'https://jonbogaty.com/radioactive-ralph/install.sh' "$built_installer"

echo "docs artifact validation: install.sh present and byte-identical"
