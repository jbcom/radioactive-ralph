#!/usr/bin/env bash
# Capture the exact pre-release package manifest bytes once, sign them, and
# attach them to the draft. A same-tag rerun must reuse this provenance rather
# than treating an already-updated package main as the rollback point.
set -euo pipefail

VERSION="${1:?usage: prepare_package_rollback_provenance.sh <version>}"
PKGS_REPO="${PKGS_REPO:-jbcom/pkgs}"
RELEASE_REPO="${RELEASE_REPO:-jbcom/radioactive-ralph}"
RELEASE_ID="${RELEASE_ID:-}"
RELEASE_SOURCE_COMMIT="${RELEASE_SOURCE_COMMIT:-}"
RELEASE_WORKFLOW_COMMIT="${RELEASE_WORKFLOW_COMMIT:-}"
PKGS_GH_TOKEN="${PKGS_GH_TOKEN:-}"
RELEASE_GH_TOKEN="${RELEASE_GH_TOKEN:-}"
GH_BIN="${GH_BIN:-gh}"
COSIGN_BIN="${COSIGN_BIN:-cosign}"
TAR_BIN="${TAR_BIN:-tar}"
ARCHIVE="package-rollback.tar.gz"
BUNDLE="${ARCHIVE}.sigstore.json"
FILES=(
  Casks/radioactive-ralph.rb
  Casks/radioactive-ralph-gui.rb
  bucket/radioactive-ralph.json
)

[[ "$VERSION" =~ ^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$ ]]
[[ "$RELEASE_SOURCE_COMMIT" =~ ^[0-9a-f]{40,64}$ ]]
[[ "$RELEASE_WORKFLOW_COMMIT" =~ ^[0-9a-f]{40}$ ]]
[[ -n "$PKGS_GH_TOKEN" ]] || {
  echo "package rollback provenance: PKGS_GH_TOKEN is required" >&2
  exit 1
}
[[ -n "$RELEASE_GH_TOKEN" ]] || {
  echo "package rollback provenance: RELEASE_GH_TOKEN is required" >&2
  exit 1
}

pkgs_gh() {
  GH_TOKEN="$PKGS_GH_TOKEN" "$GH_BIN" "$@"
}
release_gh() {
  GH_TOKEN="$RELEASE_GH_TOKEN" "$GH_BIN" "$@"
}
# shellcheck source=scripts/ci/release_by_id.sh
source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/release_by_id.sh"
ralph_release_by_id "v${VERSION}" "$RELEASE_SOURCE_COMMIT" draft >/dev/null
sha256_file() {
  sha256sum "$1" | awk '{print $1}'
}

work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
# shellcheck source=scripts/ci/release_workflow_identity.sh
source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/release_workflow_identity.sh"
identity="$(ralph_release_workflow_identity "$RELEASE_REPO" "$VERSION")"
issuer="https://token.actions.githubusercontent.com"

validate_archive() {
  local archive="$1"
  local listing expected path state recorded actual member
  listing="$("$TAR_BIN" -tzf "$archive" | LC_ALL=C sort)"
  [[ "$(printf '%s\n' "$listing" | uniq)" == "$listing" ]]
  while IFS= read -r member; do
    case "$member" in
      provenance.json|Casks/radioactive-ralph.rb|\
        Casks/radioactive-ralph-gui.rb|bucket/radioactive-ralph.json)
        ;;
      *)
        echo "package provenance: archive member is not trusted: $member" >&2
        return 1
        ;;
    esac
  done <<<"$listing"
  [[ "$(grep -Fxc provenance.json <<<"$listing")" == 1 ]]
  mkdir "$work/extracted"
  "$TAR_BIN" -xzf "$archive" -C "$work/extracted"
  [[ -f "$work/extracted/provenance.json" &&
     ! -L "$work/extracted/provenance.json" ]]
  jq -e \
    --arg version "$VERSION" \
    '.schema == 2 and .release_version == $version and
     (.prior_main_oid | test("^[0-9a-f]{40,64}$")) and
     (keys | sort) == [
       "files", "prior_main_oid", "release_version", "schema"
     ] and
     (.files | keys | sort) == [
       "Casks/radioactive-ralph-gui.rb",
       "Casks/radioactive-ralph.rb",
       "bucket/radioactive-ralph.json"
     ] and
     all(.files[];
       ((keys | sort) == ["sha256", "state"] and
        .state == "present" and
        (.sha256 | test("^[0-9a-f]{64}$"))) or
       ((keys | sort) == ["state"] and .state == "missing")
     )' "$work/extracted/provenance.json" >/dev/null
  expected="provenance.json"
  for path in "${FILES[@]}"; do
    state="$(jq -er --arg path "$path" '.files[$path].state' \
      "$work/extracted/provenance.json")"
    case "$state" in
      present)
        expected+=$'\n'"$path"
        [[ -f "$work/extracted/$path" && ! -L "$work/extracted/$path" ]]
        recorded="$(jq -er --arg path "$path" '.files[$path].sha256' \
          "$work/extracted/provenance.json")"
        actual="$(sha256_file "$work/extracted/$path")"
        [[ "$recorded" == "$actual" ]]
        ;;
      missing)
        [[ ! -e "$work/extracted/$path" && ! -L "$work/extracted/$path" ]]
        ;;
      *)
        return 1
        ;;
    esac
  done
  expected="$(printf '%s\n' "$expected" | LC_ALL=C sort)"
  [[ "$listing" == "$expected" ]] || {
    echo "package provenance: archive member set is not exact" >&2
    return 1
  }
}

mapfile -t existing < <(ralph_release_assets_by_id | jq -r '.[].name')
has_archive=false
has_bundle=false
printf '%s\n' "${existing[@]}" | grep -Fxq "$ARCHIVE" && has_archive=true
printf '%s\n' "${existing[@]}" | grep -Fxq "$BUNDLE" && has_bundle=true

if [[ "$has_bundle" == true && "$has_archive" == false ]]; then
  echo "package provenance: orphaned signature bundle; quarantine required" >&2
  exit 1
fi
resume_archive=false
if [[ "$has_archive" == true ]]; then
  ralph_release_download_asset "v${VERSION}" "$RELEASE_SOURCE_COMMIT" draft \
    "$ARCHIVE" "$work/$ARCHIVE"
  if [[ "$has_bundle" == true ]]; then
    ralph_release_download_asset "v${VERSION}" "$RELEASE_SOURCE_COMMIT" draft \
      "$BUNDLE" "$work/$BUNDLE"
    "$COSIGN_BIN" verify-blob "$work/$ARCHIVE" \
      --bundle "$work/$BUNDLE" \
      --certificate-identity "$identity" \
      --certificate-oidc-issuer "$issuer" \
      --certificate-github-workflow-sha "$RELEASE_WORKFLOW_COMMIT" >/dev/null
    validate_archive "$work/$ARCHIVE"
    echo "package provenance: reused signed original rollback bytes"
    exit 0
  fi
  resume_archive=true
fi

ralph_release_by_id "v${VERSION}" "$RELEASE_SOURCE_COMMIT" draft >/dev/null
prior_main_oid="$(pkgs_gh api --jq '.object.sha' \
  "repos/${PKGS_REPO}/git/ref/heads/main")"
[[ "$prior_main_oid" =~ ^[0-9a-f]{40,64}$ ]]

mkdir -p "$work/payload/Casks" "$work/payload/bucket"
files_json='{}'
archive_members=(provenance.json)
for path in "${FILES[@]}"; do
  fetch_status="$work/fetch-status"
  fetch_error="$work/fetch-error"
  endpoint="repos/${PKGS_REPO}/contents/${path}?ref=${prior_main_oid}"
  if pkgs_gh api --silent --include "$endpoint" \
    >"$fetch_status" 2>"$fetch_error"; then
    pkgs_gh api -H "Accept: application/vnd.github.raw+json" "$endpoint" \
      > "$work/payload/$path"
    digest="$(sha256_file "$work/payload/$path")"
    files_json="$(jq -c --arg path "$path" --arg sha "$digest" \
      '. + {($path): {state: "present", sha256: $sha}}' <<<"$files_json")"
    archive_members+=("$path")
  elif grep -Eq '^HTTP/[0-9.]+ 404([[:space:]]|$)' "$fetch_status"; then
    rm -f "$work/payload/$path"
    files_json="$(jq -c --arg path "$path" \
      '. + {($path): {state: "missing"}}' <<<"$files_json")"
  else
    cat "$fetch_status" >&2
    cat "$fetch_error" >&2
    exit 1
  fi
done
jq -n \
  --arg version "$VERSION" \
  --arg prior "$prior_main_oid" \
  --argjson files "$files_json" \
  '{schema: 2, release_version: $version, prior_main_oid: $prior, files: $files}' \
  > "$work/payload/provenance.json"

"$TAR_BIN" --sort=name --mtime='@0' --owner=0 --group=0 --numeric-owner \
  -czf "$work/$ARCHIVE.candidate" -C "$work/payload" \
  "${archive_members[@]}"
validate_archive "$work/$ARCHIVE.candidate"
if [[ "$resume_archive" == true ]]; then
  cmp -s "$work/$ARCHIVE.candidate" "$work/$ARCHIVE" || {
    echo "package provenance: partial archive differs from current original package bytes" >&2
    exit 1
  }
  "$COSIGN_BIN" sign-blob "$work/$ARCHIVE" \
    --bundle "$work/$BUNDLE" --yes
  ralph_release_upload_asset "v${VERSION}" "$RELEASE_SOURCE_COMMIT" \
    "$work/$BUNDLE" >/dev/null
  echo "package provenance: resumed missing signature bundle"
  exit 0
fi
mv "$work/$ARCHIVE.candidate" "$work/$ARCHIVE"
"$COSIGN_BIN" sign-blob "$work/$ARCHIVE" \
  --bundle "$work/$BUNDLE" --yes
ralph_release_upload_asset "v${VERSION}" "$RELEASE_SOURCE_COMMIT" \
  "$work/$ARCHIVE" >/dev/null
ralph_release_upload_asset "v${VERSION}" "$RELEASE_SOURCE_COMMIT" \
  "$work/$BUNDLE" >/dev/null
echo "package provenance: captured and signed original package main $prior_main_oid"
