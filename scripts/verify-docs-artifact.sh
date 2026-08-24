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

for required_file in index.html sitemap.xml search-index.json llms.txt llms-full.txt; do
  if [[ ! -s "$artifact_dir/$required_file" ]]; then
    echo "docs artifact validation: missing or empty $artifact_dir/$required_file" >&2
    exit 1
  fi
done

if ! find "$artifact_dir" -type f -name '*.html' -print -quit | grep -q .; then
  echo "docs artifact validation: no rendered HTML pages" >&2
  exit 1
fi

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

echo "docs artifact validation: Sourcey output present and install.sh byte-identical"
