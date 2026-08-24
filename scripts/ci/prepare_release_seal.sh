#!/usr/bin/env bash
# Create-last signed inventory for every immutable release input. Reruns verify
# and reuse the existing seal; they never regenerate it from changed bytes.
set -euo pipefail

VERSION="${1:?usage: prepare_release_seal.sh <version>}"
SOURCE_COMMIT="${2:?usage: prepare_release_seal.sh <version> <source-commit>}"
RELEASE_REPO="${RELEASE_REPO:-jbcom/radioactive-ralph}"
RELEASE_GH_TOKEN="${RELEASE_GH_TOKEN:-}"
GH_BIN="${GH_BIN:-gh}"
COSIGN_BIN="${COSIGN_BIN:-cosign}"
SEAL="release-seal.json"
BUNDLE="release-seal.json.sigstore.json"

[[ "$VERSION" =~ ^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$ ]]
[[ "$SOURCE_COMMIT" =~ ^[0-9a-f]{40,64}$ ]]
[[ -n "$RELEASE_GH_TOKEN" ]]

release_gh() {
  GH_TOKEN="$RELEASE_GH_TOKEN" "$GH_BIN" "$@"
}
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
identity="https://github.com/${RELEASE_REPO}/.github/workflows/release.yml@refs/tags/v${VERSION}"
issuer="https://token.actions.githubusercontent.com"

expected_assets=(
  "radioactive_ralph_${VERSION}_darwin_amd64.tar.gz"
  "radioactive_ralph_${VERSION}_darwin_arm64.tar.gz"
  "radioactive_ralph_${VERSION}_linux_amd64.tar.gz"
  "radioactive_ralph_${VERSION}_linux_arm64.tar.gz"
  "radioactive_ralph_${VERSION}_windows_amd64.zip"
  "radioactive-ralph_${VERSION}_linux_amd64.deb"
  "radioactive-ralph_${VERSION}_linux_arm64.deb"
  "radioactive-ralph_${VERSION}_linux_amd64.rpm"
  "radioactive-ralph_${VERSION}_linux_arm64.rpm"
  "radioactive-ralph_${VERSION}_darwin_amd64.dmg"
  "radioactive-ralph_${VERSION}_darwin_arm64.dmg"
  "radioactive-ralph_${VERSION}_linux_x86_64.AppImage"
  radioactive-ralph.exe
  checksums.txt
  checksums.txt.sigstore.json
  gui-checksums.txt
  gui-checksums.txt.sigstore.json
  package-rollback.tar.gz
  package-rollback.tar.gz.sigstore.json
  package-manifests.tar.gz
  package-manifests.tar.gz.sigstore.json
)

release="$(release_gh release view "v${VERSION}" --repo "$RELEASE_REPO" \
  --json isDraft,isPrerelease,tagName,targetCommitish,assets)"
jq -e --arg tag "v${VERSION}" --arg target "$SOURCE_COMMIT" \
  '.isDraft == true and .isPrerelease == false and
   .tagName == $tag and .targetCommitish == $target' <<<"$release" >/dev/null
actual="$(jq -r '.assets[].name' <<<"$release" | LC_ALL=C sort)"
unsealed="$(printf '%s\n' "${expected_assets[@]}" | LC_ALL=C sort)"
sealed="$(printf '%s\n' "${expected_assets[@]}" "$SEAL" "$BUNDLE" | LC_ALL=C sort)"

if [[ "$actual" == "$sealed" ]]; then
  release_gh release download "v${VERSION}" --repo "$RELEASE_REPO" \
    --pattern "$SEAL" --output "$work/$SEAL"
  release_gh release download "v${VERSION}" --repo "$RELEASE_REPO" \
    --pattern "$BUNDLE" --output "$work/$BUNDLE"
  "$COSIGN_BIN" verify-blob "$work/$SEAL" --bundle "$work/$BUNDLE" \
    --certificate-identity "$identity" \
    --certificate-oidc-issuer "$issuer" >/dev/null
  jq -e --arg version "$VERSION" --arg source "$SOURCE_COMMIT" \
    '.schema == 1 and .release_version == $version and
     .tag == ("v" + $version) and .source_commit == $source and
     .workflow == "release.yml" and
     .tools == {
       "cosign-installer": "v4.1.2",
       "fyne": "v1.7.2",
       "goreleaser": "v2.17.0"
     } and (.assets | length) == 21' "$work/$SEAL" >/dev/null
  for asset in "${expected_assets[@]}"; do
    release_gh release download "v${VERSION}" --repo "$RELEASE_REPO" \
      --pattern "$asset" --output "$work/$asset"
    expected_sha="$(jq -er --arg name "$asset" \
      '.assets[] | select(.name == $name) | .sha256' "$work/$SEAL")"
    expected_size="$(jq -er --arg name "$asset" \
      '.assets[] | select(.name == $name) | .size' "$work/$SEAL")"
    [[ "$(sha256sum "$work/$asset" | awk '{print $1}')" == "$expected_sha" ]]
    [[ "$(wc -c < "$work/$asset" | tr -d ' ')" == "$expected_size" ]]
  done
  echo "release seal: existing signed inventory verified"
  exit 0
fi

[[ "$actual" == "$unsealed" ]] || {
  echo "release seal: ambiguous or partial asset set; quarantine required" >&2
  diff -u <(printf '%s\n' "$unsealed") <(printf '%s\n' "$actual") >&2 || true
  exit 1
}

assets_json='[]'
for asset in "${expected_assets[@]}"; do
  release_gh release download "v${VERSION}" --repo "$RELEASE_REPO" \
    --pattern "$asset" --output "$work/$asset"
  digest="$(sha256sum "$work/$asset" | awk '{print $1}')"
  size="$(wc -c < "$work/$asset" | tr -d ' ')"
  assets_json="$(jq -c --arg name "$asset" --arg sha "$digest" \
    --argjson size "$size" '. + [{name: $name, size: $size, sha256: $sha}]' \
    <<<"$assets_json")"
done
jq -S -n --arg version "$VERSION" --arg source "$SOURCE_COMMIT" \
  --argjson assets "$assets_json" \
  '{
    schema: 1,
    release_version: $version,
    tag: ("v" + $version),
    source_commit: $source,
    workflow: "release.yml",
    tools: {
      "cosign-installer": "v4.1.2",
      "fyne": "v1.7.2",
      "goreleaser": "v2.17.0"
    },
    assets: ($assets | sort_by(.name))
  }' > "$work/$SEAL"
"$COSIGN_BIN" sign-blob "$work/$SEAL" --bundle "$work/$BUNDLE" --yes
release_gh release upload "v${VERSION}" --repo "$RELEASE_REPO" \
  "$work/$SEAL" "$work/$BUNDLE"
echo "release seal: signed create-last inventory uploaded"
