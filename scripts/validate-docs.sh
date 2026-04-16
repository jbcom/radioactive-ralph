#!/usr/bin/env bash

set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

LIVE_DOCS=(
  README.md
  CLAUDE.md
  AGENTS.md
  SECURITY.md
  STANDARDS.md
  .github/copilot-instructions.md
  docs
  assets/ASSETS.md
  site/README.md
)

LIVE_RELEASE_FILES=(
  .goreleaser.yaml
  .goreleaser.chocolatey.yaml
  site/public/install.sh
)

fail() {
  echo "docs validation: $*" >&2
  exit 1
}

search() {
  local pattern="$1"
  shift
  if command -v rg >/dev/null 2>&1; then
    rg -n -- "$pattern" "$@"
  else
    grep -R -n -E --binary-files=without-match --exclude-dir=.git --exclude-dir=_build -- "$pattern" "$@"
  fi
}

search_o() {
  local pattern="$1"
  shift
  if command -v rg >/dev/null 2>&1; then
    rg -n -o -- "$pattern" "$@"
  else
    grep -R -n -o -E --binary-files=without-match --exclude-dir=.git --exclude-dir=_build -- "$pattern" "$@"
  fi
}

if search 'site/src/content/docs' "${LIVE_DOCS[@]}" .github "${LIVE_RELEASE_FILES[@]}"; then
  fail "found stale references to site/src/content/docs"
fi

if search 'autoapi/' "${LIVE_DOCS[@]}" .github "${LIVE_RELEASE_FILES[@]}"; then
  fail "found stale references to autoapi output"
fi

if search 'install-skill' "${LIVE_DOCS[@]}" "${LIVE_RELEASE_FILES[@]}"; then
  fail "found stale install-skill references"
fi

for pattern in \
  'uvx radioactive-ralph' \
  'pip install radioactive-ralph' \
  'radioactive_ralph --variant ' \
  'claude plugin install' \
  'claude plugin marketplace' \
  '/green-ralph([[:space:]`]|$)' \
  '/grey-ralph([[:space:]`]|$)' \
  '/red-ralph([[:space:]`]|$)' \
  '/blue-ralph([[:space:]`]|$)' \
  '/professor-ralph([[:space:]`]|$)' \
  '/fixit-ralph([[:space:]`]|$)' \
  '/immortal-ralph([[:space:]`]|$)' \
  '/savage-ralph([[:space:]`]|$)' \
  '/old-man-ralph([[:space:]`]|$)' \
  '/world-breaker-ralph([[:space:]`]|$)' \
  'ralph dashboard' \
  'ralph discover' \
  'ralph pr list' \
  'hatch '
do
  if search "$pattern" \
    docs/getting-started docs/guides docs/reference docs/design docs/variants \
    README.md CLAUDE.md AGENTS.md SECURITY.md STANDARDS.md \
    assets/ASSETS.md scripts/demo.tape scripts/record-demo.sh site/README.md \
    "${LIVE_RELEASE_FILES[@]}"; then
    fail "found stale docs pattern: $pattern"
  fi
done

for pattern in \
  'ralph run --detach' \
  'cmd/ralph/' \
  'ralph enqueue' \
  '--transport http' \
  'serve --mcp' \
  'mcp register' \
  '--skip-mcp' \
  'stdio MCP' \
  'Claude MCP' \
  'status --variant' \
  'attach --variant' \
  'stop --variant' \
  'service install --variant' \
  'run --variant .+ --foreground' \
  'internal/mcp' \
  'internal/variantpool' \
  'internal/supervisor' \
  '_supervisor'
do
  if search "$pattern" \
    docs/getting-started docs/guides docs/reference docs/design docs/variants \
    README.md AGENTS.md SECURITY.md assets/ASSETS.md site/README.md \
    "${LIVE_RELEASE_FILES[@]}"; then
    fail "found stale live-docs pattern: $pattern"
  fi
done

refs="$(mktemp)"
trap 'rm -f "$refs"' EXIT

search_o 'docs/plans/[A-Za-z0-9._/-]+\.md' README.md CLAUDE.md CHANGELOG.md reference docs \
  | cut -d: -f3- | sort -u > "$refs"

while IFS= read -r rel; do
  [[ -z "$rel" ]] && continue
  [[ -f "$ROOT/$rel" ]] || fail "missing referenced plan file: $rel"
done < "$refs"

echo "docs validation: ok"
