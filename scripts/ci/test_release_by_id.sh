#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT
RELEASE_REPO=jbcom/radioactive-ralph
RELEASE_ID=360124508
TAG=v0.22.0
TARGET=2f8089830875757f3d82a960fabac0be1dad34c8
GH_CALLS="$TMP/calls"
export RELEASE_REPO RELEASE_ID GH_CALLS

cat >"$TMP/gh" <<'GH'
#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$*" >>"${GH_CALLS:?}"
case "$*" in
  "api repos/jbcom/radioactive-ralph/releases/360124508")
    jq -cn --arg target "${TARGET:?}" '{
      id: 360124508,
      tag_name: "v0.22.0",
      target_commitish: $target,
      draft: true,
      prerelease: false,
      immutable: false
    }'
    ;;
  "api --paginate --slurp repos/jbcom/radioactive-ralph/releases/360124508/assets?per_page=100")
    assets='[]'
    if [[ -e "${CHECKSUMS_MARKER:?}" ]]; then
      checksum_id="$(<"${CHECKSUMS_ID:?}")"
      assets="$(jq -c --argjson id "$checksum_id" \
        '. + [{id: $id, name: "checksums.txt", state: "uploaded"}]' \
        <<<"$assets")"
    fi
    if [[ "${DUPLICATE_ASSET:-0}" == 1 ]]; then
      assets="$(jq -c \
        '. + [{id: 7999, name: "checksums.txt", state: "uploaded"}]' \
        <<<"$assets")"
    fi
    if [[ -e "${UPLOADED_MARKER:?}" ]]; then
      assets="$(jq -c \
        '. + [{id: 7002, name: "new.txt", state: "uploaded"}]' \
        <<<"$assets")"
    fi
    jq -cn --argjson assets "$assets" '[$assets]'
    ;;
  "api -H Accept: application/octet-stream repos/jbcom/radioactive-ralph/releases/assets/7001")
    printf '%s\n' 'exact bytes'
    ;;
  "api --method POST --hostname uploads.github.com -H Content-Type: application/octet-stream repos/jbcom/radioactive-ralph/releases/360124508/assets?name=new.txt --input "*)
    touch "${UPLOADED_MARKER:?}"
    printf '%s\n' '{"id":7002,"name":"new.txt","state":"uploaded"}'
    ;;
  "api --method DELETE repos/jbcom/radioactive-ralph/releases/assets/7001")
    rm "${CHECKSUMS_MARKER:?}"
    ;;
  "api --method POST --hostname uploads.github.com -H Content-Type: application/octet-stream repos/jbcom/radioactive-ralph/releases/360124508/assets?name=checksums.txt --input "*)
    printf '%s\n' 7003 >"${CHECKSUMS_ID:?}"
    touch "${CHECKSUMS_MARKER:?}"
    printf '%s\n' '{"id":7003,"name":"checksums.txt","state":"uploaded"}'
    ;;
  *)
    echo "unexpected gh invocation: $*" >&2
    exit 1
    ;;
esac
GH
chmod +x "$TMP/gh"
GH_BIN="$TMP/gh"
UPLOADED_MARKER="$TMP/uploaded"
CHECKSUMS_MARKER="$TMP/checksums-present"
CHECKSUMS_ID="$TMP/checksums-id"
touch "$CHECKSUMS_MARKER"
printf '%s\n' 7001 >"$CHECKSUMS_ID"
export GH_BIN TARGET UPLOADED_MARKER CHECKSUMS_MARKER CHECKSUMS_ID
release_gh() {
  "$GH_BIN" "$@"
}
# shellcheck source=scripts/ci/release_by_id.sh
source "$ROOT/scripts/ci/release_by_id.sh"

ralph_release_by_id "$TAG" "$TARGET" draft >/dev/null
if ralph_release_by_id "$TAG" \
  aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa draft >/dev/null 2>&1; then
  echo "expected wrong target to fail" >&2
  exit 1
fi
if RELEASE_ID=not-numeric \
  ralph_release_by_id "$TAG" "$TARGET" draft >/dev/null 2>&1; then
  echo "expected non-numeric release ID to fail" >&2
  exit 1
fi

ralph_release_download_asset "$TAG" "$TARGET" draft checksums.txt \
  "$TMP/checksums.txt"
[[ "$(cat "$TMP/checksums.txt")" == "exact bytes" ]]
if ralph_release_download_asset "$TAG" "$TARGET" draft missing.txt \
  "$TMP/missing.txt" >/dev/null 2>&1; then
  echo "expected absent asset to fail" >&2
  exit 1
fi
if DUPLICATE_ASSET=1 ralph_release_asset_by_name checksums.txt \
  >/dev/null 2>&1; then
  echo "expected duplicate asset name to fail closed" >&2
  exit 1
fi

printf '%s\n' 'new bytes' >"$TMP/new.txt"
ralph_release_upload_asset "$TAG" "$TARGET" "$TMP/new.txt" >/dev/null
if ralph_release_upload_asset "$TAG" "$TARGET" "$TMP/new.txt" \
  >/dev/null 2>&1; then
  echo "expected replacement upload to fail" >&2
  exit 1
fi
mkdir "$TMP/replacement"
printf '%s\n' 'replacement bytes' >"$TMP/replacement/checksums.txt"
ralph_release_upload_asset "$TAG" "$TARGET" \
  "$TMP/replacement/checksums.txt" replace-before-seal >/dev/null
[[ "$(<"$CHECKSUMS_ID")" == 7003 ]]

if grep -Fq "releases/tags/" "$GH_CALLS"; then
  echo "numeric release helper fell back to draft-invisible tag endpoint" >&2
  exit 1
fi
grep -Fq "releases/360124508" "$GH_CALLS"
grep -Fq "releases/assets/7001" "$GH_CALLS"

echo "release by id tests: ok"
