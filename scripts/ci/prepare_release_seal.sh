#!/usr/bin/env bash
# Create-last signed inventory for every immutable release input. Reruns verify
# and reuse the existing seal; they never regenerate it from changed bytes.
set -euo pipefail

VERSION="${1:?usage: prepare_release_seal.sh <version> <source-commit> <release-id> <workflow-commit>}"
SOURCE_COMMIT="${2:?source commit is required}"
RELEASE_ID="${3:?release id is required}"
WORKFLOW_COMMIT="${4:?workflow commit is required}"
RELEASE_REPO="${RELEASE_REPO:-jbcom/radioactive-ralph}"
RELEASE_GH_TOKEN="${RELEASE_GH_TOKEN:-}"
GH_BIN="${GH_BIN:-gh}"
COSIGN_BIN="${COSIGN_BIN:-cosign}"
SEAL="release-seal.json"
BUNDLE="release-seal.json.sigstore.json"

[[ "$VERSION" =~ ^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$ ]]
[[ "$SOURCE_COMMIT" =~ ^[0-9a-f]{40,64}$ ]]
[[ "$RELEASE_ID" =~ ^[1-9][0-9]*$ ]]
[[ "$WORKFLOW_COMMIT" =~ ^[0-9a-f]{40}$ ]]
[[ -n "$RELEASE_GH_TOKEN" ]]

release_gh() {
  GH_TOKEN="$RELEASE_GH_TOKEN" "$GH_BIN" "$@"
}
script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=scripts/ci/release_by_id.sh
source "$script_dir/release_by_id.sh"
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
# shellcheck source=scripts/ci/release_workflow_identity.sh
source "$script_dir/release_workflow_identity.sh"
identity="$(ralph_release_workflow_identity "$RELEASE_REPO" "$VERSION")"
issuer="https://token.actions.githubusercontent.com"
workflow_sha_args=()
if [[ "$VERSION" == "0.22.0" ]]; then
  workflow_sha_args=(
    --certificate-github-workflow-sha "$WORKFLOW_COMMIT"
  )
fi

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

build_seal_candidate() {
  local destination="${1:?seal destination is required}"
  local inventory="$work/inventory"
  local assets_json asset digest size
  mkdir "$inventory"
  assets_json='[]'
  for asset in "${expected_assets[@]}"; do
    ralph_release_download_asset "v${VERSION}" "$SOURCE_COMMIT" draft \
      "$asset" "$inventory/$asset"
    digest="$(sha256sum "$inventory/$asset" | awk '{print $1}')"
    size="$(wc -c < "$inventory/$asset" | tr -d ' ')"
    assets_json="$(jq -c --arg name "$asset" --arg sha "$digest" \
      --argjson size "$size" \
      '. + [{name: $name, size: $size, sha256: $sha}]' <<<"$assets_json")"
  done
  jq -S -n --arg version "$VERSION" --arg source "$SOURCE_COMMIT" \
    --argjson release_id "$RELEASE_ID" --arg workflow "$WORKFLOW_COMMIT" \
    --argjson assets "$assets_json" \
    '{
      schema: 2,
      release_version: $version,
      tag: ("v" + $version),
      source_commit: $source,
      release_id: $release_id,
      workflow_commit: $workflow,
      workflow: "release.yml",
      tools: {
        "cosign-installer": "v4.1.2",
        "fyne": "v1.7.2",
        "goreleaser": "v2.17.0"
      },
      assets: ($assets | sort_by(.name))
    }' > "$destination"
}

ralph_release_by_id "v${VERSION}" "$SOURCE_COMMIT" draft >/dev/null
actual="$(ralph_release_assets_by_id | jq -r '.[].name' | LC_ALL=C sort)"
unsealed="$(printf '%s\n' "${expected_assets[@]}" | LC_ALL=C sort)"
seal_only="$(
  printf '%s\n' "${expected_assets[@]}" "$SEAL" | LC_ALL=C sort
)"
sealed="$(printf '%s\n' "${expected_assets[@]}" "$SEAL" "$BUNDLE" | LC_ALL=C sort)"

if [[ "$actual" == "$sealed" ]]; then
  ralph_release_download_asset "v${VERSION}" "$SOURCE_COMMIT" draft \
    "$SEAL" "$work/$SEAL"
  ralph_release_download_asset "v${VERSION}" "$SOURCE_COMMIT" draft \
    "$BUNDLE" "$work/$BUNDLE"
  "$COSIGN_BIN" verify-blob "$work/$SEAL" --bundle "$work/$BUNDLE" \
    --certificate-identity "$identity" \
    "${workflow_sha_args[@]}" \
    --certificate-oidc-issuer "$issuer" >/dev/null
  jq -e --arg version "$VERSION" --arg source "$SOURCE_COMMIT" \
    --argjson release_id "$RELEASE_ID" --arg workflow "$WORKFLOW_COMMIT" \
    '.schema == 2 and .release_version == $version and
     .tag == ("v" + $version) and .source_commit == $source and
     .release_id == $release_id and .workflow_commit == $workflow and
     .workflow == "release.yml" and
     .tools == {
       "cosign-installer": "v4.1.2",
       "fyne": "v1.7.2",
       "goreleaser": "v2.17.0"
     } and (.assets | length) == 21' "$work/$SEAL" >/dev/null
  for asset in "${expected_assets[@]}"; do
    ralph_release_download_asset "v${VERSION}" "$SOURCE_COMMIT" draft \
      "$asset" "$work/$asset"
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

if [[ "$actual" == "$seal_only" ]]; then
  ralph_release_download_asset "v${VERSION}" "$SOURCE_COMMIT" draft \
    "$SEAL" "$work/existing-$SEAL"
  build_seal_candidate "$work/$SEAL"
  cmp -s "$work/existing-$SEAL" "$work/$SEAL" || {
    echo "release seal: partial seal differs from deterministic inventory" >&2
    exit 1
  }
  "$COSIGN_BIN" sign-blob "$work/$SEAL" --bundle "$work/$BUNDLE" --yes
  "$COSIGN_BIN" verify-blob "$work/$SEAL" --bundle "$work/$BUNDLE" \
    --certificate-identity "$identity" \
    "${workflow_sha_args[@]}" \
    --certificate-oidc-issuer "$issuer" >/dev/null
  ralph_release_upload_asset "v${VERSION}" "$SOURCE_COMMIT" \
    "$work/$BUNDLE" >/dev/null
  echo "release seal: resumed missing signature bundle"
  exit 0
fi

[[ "$actual" == "$unsealed" ]] || {
  echo "release seal: ambiguous or partial asset set; quarantine required" >&2
  diff -u <(printf '%s\n' "$unsealed") <(printf '%s\n' "$actual") >&2 || true
  exit 1
}

build_seal_candidate "$work/$SEAL"
"$COSIGN_BIN" sign-blob "$work/$SEAL" --bundle "$work/$BUNDLE" --yes
ralph_release_upload_asset "v${VERSION}" "$SOURCE_COMMIT" \
  "$work/$SEAL" >/dev/null
ralph_release_upload_asset "v${VERSION}" "$SOURCE_COMMIT" \
  "$work/$BUNDLE" >/dev/null
echo "release seal: signed create-last inventory uploaded"
