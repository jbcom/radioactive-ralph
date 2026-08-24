#!/usr/bin/env bash
# Capture the exact pre-release package manifest bytes once, sign them, and
# attach them to the draft. A same-tag rerun must reuse this provenance rather
# than treating an already-updated package main as the rollback point.
set -euo pipefail

VERSION="${1:?usage: prepare_package_rollback_provenance.sh <version>}"
PKGS_REPO="${PKGS_REPO:-jbcom/pkgs}"
RELEASE_REPO="${RELEASE_REPO:-jbcom/radioactive-ralph}"
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
sha256_file() {
  sha256sum "$1" | awk '{print $1}'
}

work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
identity="https://github.com/${RELEASE_REPO}/.github/workflows/release.yml@refs/tags/v${VERSION}"
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

release="$(release_gh release view "v${VERSION}" --repo "$RELEASE_REPO" \
  --json isDraft,isPrerelease,tagName,assets)"
jq -e \
  --arg tag "v${VERSION}" \
  '.isDraft == true and .isPrerelease == false and .tagName == $tag' \
  <<<"$release" >/dev/null
mapfile -t existing < <(jq -r '.assets[].name' <<<"$release")
(( ${#existing[@]} > 0 )) || {
  echo "package provenance: draft release has no assets after GoReleaser" >&2
  exit 1
}
has_archive=false
has_bundle=false
printf '%s\n' "${existing[@]}" | grep -Fxq "$ARCHIVE" && has_archive=true
printf '%s\n' "${existing[@]}" | grep -Fxq "$BUNDLE" && has_bundle=true

if [[ "$has_archive" == true || "$has_bundle" == true ]]; then
  [[ "$has_archive" == true && "$has_bundle" == true ]] || {
    echo "package provenance: partial prior provenance asset set" >&2
    exit 1
  }
  release_gh release download "v${VERSION}" --repo "$RELEASE_REPO" \
    --pattern "$ARCHIVE" --output "$work/$ARCHIVE"
  release_gh release download "v${VERSION}" --repo "$RELEASE_REPO" \
    --pattern "$BUNDLE" --output "$work/$BUNDLE"
  "$COSIGN_BIN" verify-blob "$work/$ARCHIVE" \
    --bundle "$work/$BUNDLE" \
    --certificate-identity "$identity" \
    --certificate-oidc-issuer "$issuer" >/dev/null
  validate_archive "$work/$ARCHIVE"
  echo "package provenance: reused signed original rollback bytes"
  exit 0
fi

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
  -czf "$work/$ARCHIVE" -C "$work/payload" \
  "${archive_members[@]}"
"$COSIGN_BIN" sign-blob "$work/$ARCHIVE" \
  --bundle "$work/$BUNDLE" --yes
validate_archive "$work/$ARCHIVE"
release_gh release upload "v${VERSION}" --repo "$RELEASE_REPO" \
  "$work/$ARCHIVE" "$work/$BUNDLE"
echo "package provenance: captured and signed original package main $prior_main_oid"
