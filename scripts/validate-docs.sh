#!/usr/bin/env bash

set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

fail() {
  echo "docs validation: $*" >&2
  exit 1
}

if rg -n 'site/src/content/docs' README.md CLAUDE.md AGENTS.md docs .github site/README.md; then
  fail "found stale references to site/src/content/docs"
fi

if rg -n 'autoapi/' docs README.md CLAUDE.md AGENTS.md .github; then
  fail "found stale references to autoapi output"
fi

if rg -n 'install-skill' README.md CLAUDE.md AGENTS.md docs/variants skills/README.md skills/*/README.md .claude-plugin site/README.md; then
  fail "found stale install-skill references"
fi

for pattern in \
  'uvx radioactive-ralph' \
  'pip install radioactive-ralph' \
  'claude plugin install ralph@jbcom-plugins' \
  'ralph dashboard' \
  'ralph discover' \
  'ralph pr list' \
  'hatch '
do
  if rg -n "$pattern" docs/getting-started docs/guides docs/reference docs/design docs/variants skills/README.md skills/*/README.md README.md CLAUDE.md AGENTS.md STANDARDS.md assets/ASSETS.md .claude-plugin site/README.md; then
    fail "found stale docs pattern: $pattern"
  fi
done

for pattern in \
  'ralph run --detach' \
  'cmd/ralph/' \
  'ralph enqueue'
do
  if rg -n "$pattern" docs/getting-started docs/guides docs/reference docs/design docs/variants skills/README.md skills/*/README.md README.md AGENTS.md assets/ASSETS.md site/README.md; then
    fail "found stale live-docs pattern: $pattern"
  fi
done

refs="$(mktemp)"
trap 'rm -f "$refs"' EXIT

rg -n -o 'docs/plans/[A-Za-z0-9._/-]+\.md' README.md CLAUDE.md CHANGELOG.md reference docs \
  | cut -d: -f3- | sort -u > "$refs"

while IFS= read -r rel; do
  [[ -z "$rel" ]] && continue
  [[ -f "$ROOT/$rel" ]] || fail "missing referenced plan file: $rel"
done < "$refs"

echo "docs validation: ok"
