#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT
# shellcheck source=scripts/ci/release_workflow_identity.sh
source "$ROOT/scripts/ci/release_workflow_identity.sh"

[[ "$(ralph_release_workflow_identity jbcom/radioactive-ralph 0.22.0)" == \
  "https://github.com/jbcom/radioactive-ralph/.github/workflows/release.yml@refs/heads/main" ]]
[[ "$(ralph_release_workflow_identity jbcom/radioactive-ralph 0.22.1)" == \
  "https://github.com/jbcom/radioactive-ralph/.github/workflows/release.yml@refs/tags/v0.22.1" ]]

now="$(date -u +%s)"
source_commit=2f8089830875757f3d82a960fabac0be1dad34c8
evidence="$(jq -cn \
  --arg source "$source_commit" \
  --argjson checked_at "$now" \
  '{
    schema: 1,
    repository: "jbcom/radioactive-ralph",
    tag: "v0.22.0",
    source_commit: $source,
    release_id: 360124508,
    phase: "prepare",
    immutable_releases_enabled: true,
    checked_at: $checked_at,
    etag_sha256: ("a" * 64),
    request_id: "ABCD:1234:CAFE"
  }')"
GITHUB_REPOSITORY=jbcom/radioactive-ralph \
  bash "$ROOT/scripts/ci/verify_immutable_release_preflight.sh" \
  "$evidence" v0.22.0 "$source_commit" 360124508 prepare 600

if GITHUB_REPOSITORY=jbcom/radioactive-ralph \
  bash "$ROOT/scripts/ci/verify_immutable_release_preflight.sh" \
  "$evidence" v0.22.1 "$source_commit" 360124508 prepare 600 \
  >/dev/null 2>&1; then
  echo "expected non-recovery tag to fail" >&2
  exit 1
fi
stale="$(jq -c '.checked_at = 1' <<<"$evidence")"
if GITHUB_REPOSITORY=jbcom/radioactive-ralph \
  bash "$ROOT/scripts/ci/verify_immutable_release_preflight.sh" \
  "$stale" v0.22.0 "$source_commit" 360124508 prepare 600 \
  >/dev/null 2>&1; then
  echo "expected stale evidence to fail" >&2
  exit 1
fi
wrong_release="$(jq -c '.release_id = 360124509' <<<"$evidence")"
if GITHUB_REPOSITORY=jbcom/radioactive-ralph \
  bash "$ROOT/scripts/ci/verify_immutable_release_preflight.sh" \
  "$wrong_release" v0.22.0 "$source_commit" 360124508 prepare 600 \
  >/dev/null 2>&1; then
  echo "expected mismatched numeric release ID to fail" >&2
  exit 1
fi
if GITHUB_REPOSITORY=jbcom/radioactive-ralph \
  bash "$ROOT/scripts/ci/verify_immutable_release_preflight.sh" \
  "$evidence" v0.22.0 "$source_commit" 360124508 publish 600 \
  >/dev/null 2>&1; then
  echo "expected mismatched release phase to fail" >&2
  exit 1
fi
disabled="$(jq -c '.immutable_releases_enabled = false' <<<"$evidence")"
if GITHUB_REPOSITORY=jbcom/radioactive-ralph \
  bash "$ROOT/scripts/ci/verify_immutable_release_preflight.sh" \
  "$disabled" v0.22.0 "$source_commit" 360124508 prepare 600 \
  >/dev/null 2>&1; then
  echo "expected disabled immutable releases evidence to fail" >&2
  exit 1
fi

cat >"$TMP/gh" <<'GH'
#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$*" >>"${GH_CALLS:?}"
case "$*" in
  "api user --jq .id")
    printf '%s\n' "${FAKE_ACTOR_ID:-2650679}"
    ;;
  *"immutable-releases")
    cat <<'RESPONSE'
HTTP/2.0 200 OK
Date: Sun, 26 Jul 2026 22:10:38 GMT
Etag: W/"8acf5bc61f50953ddae2e47e4aabd0a2733899b60ba5bf6e4d8ed0466d8bc00a"
X-Github-Request-Id: F234:2B05E9:1DD5ECD:67ED303:6A6685DE

{"enabled":true,"enforced_by_owner":false}
RESPONSE
    ;;
  *"git/ref/tags/v0.22.0")
    printf '%s\n' 2f8089830875757f3d82a960fabac0be1dad34c8
    ;;
  *"release view v0.22.0"*)
    printf '%s\n' '{"databaseId":360124508,"isDraft":true,"isPrerelease":false,"tagName":"v0.22.0","targetCommitish":"2f8089830875757f3d82a960fabac0be1dad34c8"}'
    ;;
  *"releases/360124508")
    printf '%s\n' '{"id":360124508,"draft":true,"prerelease":false,"tag_name":"v0.22.0","target_commitish":"2f8089830875757f3d82a960fabac0be1dad34c8"}'
    ;;
  *"git/ref/heads/main")
    printf '%s\n' aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
    ;;
  *"workflow run release.yml"*)
    ;;
  *)
    echo "unexpected gh invocation: $*" >&2
    exit 1
    ;;
esac
GH
chmod +x "$TMP/gh"
if GH_CALLS="$TMP/calls" GH_BIN="$TMP/gh" FAKE_ACTOR_ID=999 \
  bash "$ROOT/scripts/ci/dispatch_release_recovery.sh" prepare v0.22.0 \
  >/dev/null 2>&1; then
  echo "expected non-administrator dispatch to fail" >&2
  exit 1
fi
: >"$TMP/calls"
GH_CALLS="$TMP/calls" GH_BIN="$TMP/gh" \
  bash "$ROOT/scripts/ci/dispatch_release_recovery.sh" prepare v0.22.0
grep -Fq "workflow run release.yml --repo jbcom/radioactive-ralph --ref main" \
  "$TMP/calls"
grep -Fq -- "--field tag=v0.22.0" "$TMP/calls"
grep -Fq -- "--field phase=prepare" "$TMP/calls"
grep -Fq -- "--field source_commit=$source_commit" "$TMP/calls"
grep -Fq -- "--field release_id=360124508" "$TMP/calls"
grep -Fq -- "--field prepared_run_id=0" "$TMP/calls"
grep -Fq -- "--field workflow_commit=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" \
  "$TMP/calls"

: >"$TMP/calls"
GH_CALLS="$TMP/calls" GH_BIN="$TMP/gh" \
  bash "$ROOT/scripts/ci/dispatch_release_recovery.sh" \
  publish v0.22.0 30229999999
grep -Fq -- "--field phase=publish" "$TMP/calls"
grep -Fq -- "--field prepared_run_id=30229999999" "$TMP/calls"

echo "immutable release recovery tests: ok"
