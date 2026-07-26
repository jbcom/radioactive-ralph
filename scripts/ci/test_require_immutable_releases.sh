#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
SCRIPT="$ROOT/scripts/ci/require_immutable_releases.sh"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

FAKE_GH="$TMP/gh"
cat >"$FAKE_GH" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail

printf '%s\n' "${GH_TOKEN:-}" >"$FAKE_RECORD/token"
printf '%s\n' "$@" >"$FAKE_RECORD/args"

case "${FAKE_MODE:-}" in
  inaccessible)
    exit 1
    ;;
  invalid)
    printf '%s\n' '{"unexpected":true}'
    ;;
  disabled)
    printf '%s\n' '{"enabled":false,"enforced_by_owner":false}'
    ;;
  enabled)
    printf '%s\n' '{"enabled":true,"enforced_by_owner":false}'
    ;;
  *)
    exit 2
    ;;
esac
EOF
chmod +x "$FAKE_GH"

run_gate() {
  local mode="$1"
  shift
  FAKE_MODE="$mode" \
    FAKE_RECORD="$TMP" \
    GH_BIN="$FAKE_GH" \
    GITHUB_REPOSITORY=jbcom/radioactive-ralph \
    "$@" bash "$SCRIPT"
}

if FAKE_MODE=enabled FAKE_RECORD="$TMP" GH_BIN="$FAKE_GH" \
  GITHUB_REPOSITORY=jbcom/radioactive-ralph GH_TOKEN=legacy-broad \
  bash "$SCRIPT" 2>"$TMP/missing.err"; then
  echo "expected a missing CI_GITHUB_TOKEN to fail" >&2
  exit 1
fi
grep -Fq "CI_GITHUB_TOKEN is not configured" "$TMP/missing.err"

if run_gate inaccessible env CI_GITHUB_TOKEN=test-admin-read \
  2>"$TMP/inaccessible.err"; then
  echo "expected an inaccessible immutable-release setting to fail" >&2
  exit 1
fi
grep -Fq "Administration read" "$TMP/inaccessible.err"

if run_gate invalid env CI_GITHUB_TOKEN=test-admin-read \
  2>"$TMP/invalid.err"; then
  echo "expected an invalid immutable-release response to fail" >&2
  exit 1
fi
grep -Fq "invalid response" "$TMP/invalid.err"

if run_gate disabled env CI_GITHUB_TOKEN=test-admin-read \
  2>"$TMP/disabled.err"; then
  echo "expected disabled immutable releases to fail" >&2
  exit 1
fi
grep -Fq "immutable releases are disabled" "$TMP/disabled.err"

run_gate enabled env CI_GITHUB_TOKEN=test-admin-read \
  >"$TMP/enabled.out"
grep -Fq "Repository immutable releases are enabled." "$TMP/enabled.out"
grep -Fxq "test-admin-read" "$TMP/token"
grep -Fxq "api" "$TMP/args"
grep -Fxq "X-GitHub-Api-Version: 2026-03-10" "$TMP/args"
grep -Fxq "repos/jbcom/radioactive-ralph/immutable-releases" "$TMP/args"

if grep -F "test-admin-read" \
  "$TMP/missing.err" "$TMP/inaccessible.err" "$TMP/invalid.err" \
  "$TMP/disabled.err" "$TMP/enabled.out"; then
  echo "immutable-release gate exposed credential material" >&2
  exit 1
fi

echo "immutable-release authority tests passed"
