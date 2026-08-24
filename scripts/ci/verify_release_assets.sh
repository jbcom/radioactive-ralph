#!/usr/bin/env bash
# Verify the exact immutable-release asset set, including the signed package
# rollback provenance captured while the release is still a draft.
set -euo pipefail

VERSION="${1:?usage: verify_release_assets.sh <version>}"
RELEASE_REPO="${RELEASE_REPO:-jbcom/radioactive-ralph}"
RELEASE_GH_TOKEN="${RELEASE_GH_TOKEN:-}"
GH_BIN="${GH_BIN:-gh}"
COSIGN_BIN="${COSIGN_BIN:-cosign}"

[[ "$VERSION" =~ ^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$ ]] || {
  echo "release assets: invalid stable version: $VERSION" >&2
  exit 1
}
[[ -n "$RELEASE_GH_TOKEN" ]] || {
  echo "release assets: RELEASE_GH_TOKEN is required" >&2
  exit 1
}

release_gh() {
  GH_TOKEN="$RELEASE_GH_TOKEN" "$GH_BIN" "$@"
}

work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

cli_assets=(
  "radioactive_ralph_${VERSION}_darwin_amd64.tar.gz"
  "radioactive_ralph_${VERSION}_darwin_arm64.tar.gz"
  "radioactive_ralph_${VERSION}_linux_amd64.tar.gz"
  "radioactive_ralph_${VERSION}_linux_arm64.tar.gz"
  "radioactive_ralph_${VERSION}_windows_amd64.zip"
  "radioactive-ralph_${VERSION}_linux_amd64.deb"
  "radioactive-ralph_${VERSION}_linux_arm64.deb"
  "radioactive-ralph_${VERSION}_linux_amd64.rpm"
  "radioactive-ralph_${VERSION}_linux_arm64.rpm"
)
gui_assets=(
  "radioactive-ralph_${VERSION}_darwin_amd64.dmg"
  "radioactive-ralph_${VERSION}_darwin_arm64.dmg"
  "radioactive-ralph_${VERSION}_linux_x86_64.AppImage"
  "radioactive-ralph.exe"
)
metadata_assets=(
  checksums.txt
  checksums.txt.sigstore.json
  gui-checksums.txt
  gui-checksums.txt.sigstore.json
  package-rollback.tar.gz
  package-rollback.tar.gz.sigstore.json
  package-manifests.tar.gz
  package-manifests.tar.gz.sigstore.json
  release-seal.json
  release-seal.json.sigstore.json
)
expected=("${cli_assets[@]}" "${gui_assets[@]}" "${metadata_assets[@]}")

release="$(release_gh release view "v${VERSION}" \
  --repo "$RELEASE_REPO" \
  --json assets,targetCommitish)"
actual_names="$(jq -r '.assets[].name' <<<"$release" | LC_ALL=C sort)"
expected_names="$(printf '%s\n' "${expected[@]}" | LC_ALL=C sort)"
[[ "$actual_names" == "$expected_names" ]] || {
  echo "release assets: uploaded asset set is not exact" >&2
  diff -u <(printf '%s\n' "$expected_names") <(printf '%s\n' "$actual_names") >&2 ||
    true
  exit 1
}

download_asset() {
  local asset="$1" output="$2" asset_api_url
  asset_api_url="$(jq -er --arg name "$asset" \
    '.assets[] | select(.name == $name) | .apiUrl' <<<"$release")"
  release_gh api -H 'Accept: application/octet-stream' "$asset_api_url" > "$output"
}

for asset in "${expected[@]}"; do
  download_asset "$asset" "$work/$asset"
  [[ -f "$work/$asset" ]] || exit 1
done

identity="https://github.com/${RELEASE_REPO}/.github/workflows/release.yml@refs/tags/v${VERSION}"
issuer="https://token.actions.githubusercontent.com"
for manifest in checksums.txt gui-checksums.txt; do
  "$COSIGN_BIN" verify-blob "$work/$manifest" \
    --bundle "$work/${manifest}.sigstore.json" \
    --certificate-identity "$identity" \
    --certificate-oidc-issuer "$issuer" >/dev/null
done
"$COSIGN_BIN" verify-blob "$work/package-rollback.tar.gz" \
  --bundle "$work/package-rollback.tar.gz.sigstore.json" \
  --certificate-identity "$identity" \
  --certificate-oidc-issuer "$issuer" >/dev/null
"$COSIGN_BIN" verify-blob "$work/package-manifests.tar.gz" \
  --bundle "$work/package-manifests.tar.gz.sigstore.json" \
  --certificate-identity "$identity" \
  --certificate-oidc-issuer "$issuer" >/dev/null
"$COSIGN_BIN" verify-blob "$work/release-seal.json" \
  --bundle "$work/release-seal.json.sigstore.json" \
  --certificate-identity "$identity" \
  --certificate-oidc-issuer "$issuer" >/dev/null

cli_names="$(awk '{name=$2; sub(/^\*/, "", name); print name}' \
  "$work/checksums.txt" | LC_ALL=C sort)"
gui_names="$(awk '{name=$2; sub(/^\*/, "", name); print name}' \
  "$work/gui-checksums.txt" | LC_ALL=C sort)"
[[ "$cli_names" == "$(printf '%s\n' "${cli_assets[@]}" | LC_ALL=C sort)" ]]
[[ "$gui_names" == "$(printf '%s\n' "${gui_assets[@]}" | LC_ALL=C sort)" ]]

(cd "$work" && sha256sum -c checksums.txt && sha256sum -c gui-checksums.txt) \
  >/dev/null

rollback_names="$(tar -tzf "$work/package-rollback.tar.gz" | LC_ALL=C sort)"
[[ "$(printf '%s\n' "$rollback_names" | uniq)" == "$rollback_names" ]]
while IFS= read -r member; do
  case "$member" in
    provenance.json|Casks/radioactive-ralph.rb|\
      Casks/radioactive-ralph-gui.rb|bucket/radioactive-ralph.json)
      ;;
    *)
      echo "release assets: rollback member is not trusted: $member" >&2
      exit 1
      ;;
  esac
done <<<"$rollback_names"
[[ "$(grep -Fxc provenance.json <<<"$rollback_names")" == 1 ]]
mkdir "$work/rollback"
tar -xzf "$work/package-rollback.tar.gz" -C "$work/rollback"
[[ -f "$work/rollback/provenance.json" &&
   ! -L "$work/rollback/provenance.json" ]]
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
   )' "$work/rollback/provenance.json" >/dev/null
expected_rollback_names="provenance.json"
for path in \
  Casks/radioactive-ralph.rb \
  Casks/radioactive-ralph-gui.rb \
  bucket/radioactive-ralph.json; do
  state="$(jq -er --arg path "$path" '.files[$path].state' \
    "$work/rollback/provenance.json")"
  case "$state" in
    present)
      expected_rollback_names+=$'\n'"$path"
      [[ -f "$work/rollback/$path" && ! -L "$work/rollback/$path" ]]
      expected_sha="$(jq -er --arg path "$path" '.files[$path].sha256' \
        "$work/rollback/provenance.json")"
      [[ "$(sha256sum "$work/rollback/$path" | awk '{print $1}')" == "$expected_sha" ]]
      ;;
    missing)
      [[ ! -e "$work/rollback/$path" && ! -L "$work/rollback/$path" ]]
      ;;
    *)
      exit 1
      ;;
  esac
done
expected_rollback_names="$(
  printf '%s\n' "$expected_rollback_names" | LC_ALL=C sort
)"
[[ "$rollback_names" == "$expected_rollback_names" ]]

package_names="$(tar -tzf "$work/package-manifests.tar.gz" | LC_ALL=C sort)"
expected_package_names="$(printf '%s\n' \
  Casks/radioactive-ralph.rb \
  Casks/radioactive-ralph-gui.rb \
  bucket/radioactive-ralph.json | LC_ALL=C sort)"
[[ "$package_names" == "$expected_package_names" ]]

release_target="$(jq -er '.targetCommitish' <<<"$release")"
jq -e \
  --arg version "$VERSION" \
  --arg source "$release_target" \
  '.schema == 1 and .release_version == $version and
   .tag == ("v" + $version) and .source_commit == $source and
   .workflow == "release.yml" and
   (.assets | length) == 21' "$work/release-seal.json" >/dev/null
sealed_names="$(jq -r '.assets[].name' "$work/release-seal.json" | LC_ALL=C sort)"
expected_sealed_names="$(printf '%s\n' "${expected[@]}" |
  grep -v '^release-seal\.json' | LC_ALL=C sort)"
[[ "$sealed_names" == "$expected_sealed_names" ]]
for asset in "${expected[@]}"; do
  [[ "$asset" == release-seal.json* ]] && continue
  seal_sha="$(jq -er --arg name "$asset" \
    '.assets[] | select(.name == $name) | .sha256' "$work/release-seal.json")"
  seal_size="$(jq -er --arg name "$asset" \
    '.assets[] | select(.name == $name) | .size' "$work/release-seal.json")"
  [[ "$(sha256sum "$work/$asset" | awk '{print $1}')" == "$seal_sha" ]]
  [[ "$(wc -c < "$work/$asset" | tr -d ' ')" == "$seal_size" ]]
done

echo "release assets: exact 23-asset immutable set and 13 deliverables verified"
