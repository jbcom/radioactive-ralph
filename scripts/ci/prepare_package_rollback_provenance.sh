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
  local listing expected path recorded actual
  listing="$(tar -tzf "$archive" | LC_ALL=C sort)"
  expected="$(printf '%s\n' \
    provenance.json \
    Casks/radioactive-ralph.rb \
    Casks/radioactive-ralph-gui.rb \
    bucket/radioactive-ralph.json | LC_ALL=C sort)"
  [[ "$listing" == "$expected" ]] || {
    echo "package provenance: archive member set is not exact" >&2
    return 1
  }
  mkdir "$work/extracted"
  tar -xzf "$archive" -C "$work/extracted"
  jq -e \
    --arg version "$VERSION" \
    '.schema == 1 and .release_version == $version and
     (.prior_main_oid | test("^[0-9a-f]{40,64}$")) and
     (.files | keys | sort) == [
       "Casks/radioactive-ralph-gui.rb",
       "Casks/radioactive-ralph.rb",
       "bucket/radioactive-ralph.json"
     ]' "$work/extracted/provenance.json" >/dev/null
  for path in "${FILES[@]}"; do
    recorded="$(jq -er --arg path "$path" '.files[$path].sha256' \
      "$work/extracted/provenance.json")"
    actual="$(sha256_file "$work/extracted/$path")"
    [[ "$recorded" == "$actual" ]]
  done
}

mapfile -t existing < <(release_gh release view "v${VERSION}" \
  --repo "$RELEASE_REPO" --json assets --jq '.assets[].name')
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

release="$(release_gh api \
  "repos/${RELEASE_REPO}/releases/tags/v${VERSION}")"
jq -e \
  --arg tag "v${VERSION}" \
  '.draft == true and .prerelease == false and .tag_name == $tag' \
  <<<"$release" >/dev/null
prior_main_oid="$(pkgs_gh api --jq '.object.sha' \
  "repos/${PKGS_REPO}/git/ref/heads/main")"
[[ "$prior_main_oid" =~ ^[0-9a-f]{40,64}$ ]]

mkdir -p "$work/payload/Casks" "$work/payload/bucket"
files_json='{}'
for path in "${FILES[@]}"; do
  pkgs_gh api -H "Accept: application/vnd.github.raw+json" \
    "repos/${PKGS_REPO}/contents/${path}?ref=${prior_main_oid}" \
    > "$work/payload/$path"
  digest="$(sha256_file "$work/payload/$path")"
  files_json="$(jq -c --arg path "$path" --arg sha "$digest" \
    '. + {($path): {sha256: $sha}}' <<<"$files_json")"
done
jq -n \
  --arg version "$VERSION" \
  --arg prior "$prior_main_oid" \
  --argjson files "$files_json" \
  '{schema: 1, release_version: $version, prior_main_oid: $prior, files: $files}' \
  > "$work/payload/provenance.json"

tar --sort=name --mtime='@0' --owner=0 --group=0 --numeric-owner \
  -czf "$work/$ARCHIVE" -C "$work/payload" \
  provenance.json "${FILES[@]}"
"$COSIGN_BIN" sign-blob "$work/$ARCHIVE" \
  --bundle "$work/$BUNDLE" --yes
validate_archive "$work/$ARCHIVE"
release_gh release upload "v${VERSION}" --repo "$RELEASE_REPO" \
  "$work/$ARCHIVE" "$work/$BUNDLE"
echo "package provenance: captured and signed original package main $prior_main_oid"
