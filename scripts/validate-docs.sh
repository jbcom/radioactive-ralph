#!/usr/bin/env bash

set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"
# shellcheck source=scripts/ci/package_guidance_contract.sh
# shellcheck source=scripts/ci/package_guidance_contract.sh
source "$ROOT/scripts/ci/package_guidance_contract.sh"

LIVE_DOCS=(
  README.md
  CLAUDE.md
  AGENTS.md
  SECURITY.md
  STANDARDS.md
  .github/copilot-instructions.md
  docs
  assets/ASSETS.md
)

LIVE_RELEASE_FILES=(
  .goreleaser.yaml
  .goreleaser.chocolatey.yaml
  docs/install.sh
)

RETIRED_INSTALL_SURFACES=(
  site/package.json
  site/pnpm-lock.yaml
  site/public/install.sh
  docs/api
  docs/conf.py
  docs/requirements.lock
  reference/pyproject.toml
  reference/tox.ini
  reference/uv.lock
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

[[ -x docs/install.sh ]] || fail "docs/install.sh must exist and be executable"
[[ -f docs/sourcey.config.ts ]] || fail "docs/sourcey.config.ts must exist"
[[ -f docs/package.json && -f docs/pnpm-lock.yaml ]] ||
  fail "Sourcey package metadata and lockfile must exist"
ralph_validate_goreleaser_release_footer "$ROOT/.goreleaser.yaml" ||
  fail "GoReleaser footer violates the platform-specific first-run contract"
ralph_validate_winget_config_contract "$ROOT/.goreleaser.yaml" ||
  fail "Winget metadata violates the native Windows package contract"
ralph_validate_chocolatey_config_contract "$ROOT/.goreleaser.chocolatey.yaml" ||
  fail "Chocolatey metadata violates the native Windows package contract"

for path in "${RETIRED_INSTALL_SURFACES[@]}"; do
  [[ ! -e "$path" && ! -L "$path" ]] || fail "retired install surface returned: $path"
done

if search 'Sphinx|sphinx-build|gomarkdoc|docs/_build/html|docs/api/' \
  docs/getting-started docs/guides docs/reference/architecture.md \
  docs/reference/state.md docs/reference/testing.md README.md CLAUDE.md \
  AGENTS.md SECURITY.md STANDARDS.md .github Makefile "${LIVE_RELEASE_FILES[@]}"; then
  fail "found stale legacy documentation renderer reference"
fi

if search 'install-skill' "${LIVE_DOCS[@]}" "${LIVE_RELEASE_FILES[@]}"; then
  fail "found stale install-skill references"
fi

for pattern in \
  'uvx radioactive-ralph' \
  'pip install radioactive-ralph' \
  'radioactive_ralph run --variant' \
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
  '\.radioactive-ralph/config\.toml' \
  'internal/plandag' \
  'internal/variant\b' \
  'hatch '
do
  if search "$pattern" \
    docs/getting-started docs/guides docs/reference docs/design \
    README.md CLAUDE.md AGENTS.md SECURITY.md STANDARDS.md \
    assets/ASSETS.md scripts/demo.tape scripts/record-demo.sh \
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
  'internal/variantpool'
do
  if search "$pattern" \
    docs/getting-started docs/guides docs/reference docs/design \
    README.md AGENTS.md SECURITY.md assets/ASSETS.md \
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
