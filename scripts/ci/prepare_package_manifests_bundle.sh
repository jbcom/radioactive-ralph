#!/usr/bin/env bash
# Persist the exact atomic package PR payload as a signed draft asset before the
# create-last release seal. This is the durable source for initial and rerun PRs.
set -euo pipefail

VERSION="${1:?usage: prepare_package_manifests_bundle.sh <version>}"
SOURCE_ROOT="${SOURCE_ROOT:-dist}"
RELEASE_REPO="${RELEASE_REPO:-jbcom/radioactive-ralph}"
RELEASE_GH_TOKEN="${RELEASE_GH_TOKEN:-}"
GH_BIN="${GH_BIN:-gh}"
COSIGN_BIN="${COSIGN_BIN:-cosign}"
TAR_BIN="${TAR_BIN:-tar}"
ARCHIVE=package-manifests.tar.gz
BUNDLE=package-manifests.tar.gz.sigstore.json

[[ "$VERSION" =~ ^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$ ]]
[[ -n "$RELEASE_GH_TOKEN" ]]
[[ -f "$SOURCE_ROOT/homebrew/Casks/radioactive-ralph.rb" ]]
[[ -f "$SOURCE_ROOT/scoop/bucket/radioactive-ralph.json" ]]

release_gh() {
  GH_TOKEN="$RELEASE_GH_TOKEN" "$GH_BIN" "$@"
}
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
mkdir -p "$work/payload/Casks" "$work/payload/bucket"
cp "$SOURCE_ROOT/homebrew/Casks/radioactive-ralph.rb" \
  "$work/payload/Casks/radioactive-ralph.rb"
cp "$SOURCE_ROOT/scoop/bucket/radioactive-ralph.json" \
  "$work/payload/bucket/radioactive-ralph.json"
release_gh release download "v${VERSION}" --repo "$RELEASE_REPO" \
  --pattern gui-checksums.txt --output "$work/gui-checksums.txt"
gui_sha() {
  local arch="$1" asset sha
  asset="radioactive-ralph_${VERSION}_darwin_${arch}.dmg"
  sha="$(awk -v asset="$asset" \
    '$2 == asset || $2 == "*" asset {print $1}' "$work/gui-checksums.txt")"
  [[ "$sha" =~ ^[0-9a-f]{64}$ ]]
  printf '%s\n' "$sha"
}
arm64_sha="$(gui_sha arm64)"
amd64_sha="$(gui_sha amd64)"
cat > "$work/payload/Casks/radioactive-ralph-gui.rb" <<CASK
cask "radioactive-ralph-gui" do
  version "${VERSION}"

  on_arm do
    sha256 "${arm64_sha}"
    url "https://github.com/${RELEASE_REPO}/releases/download/v#{version}/radioactive-ralph_#{version}_darwin_arm64.dmg"
  end
  on_intel do
    sha256 "${amd64_sha}"
    url "https://github.com/${RELEASE_REPO}/releases/download/v#{version}/radioactive-ralph_#{version}_darwin_amd64.dmg"
  end

  name "radioactive-ralph"
  desc "Supervised-execution runtime for local AI-agent CLIs"
  homepage "https://github.com/${RELEASE_REPO}"

  app "radioactive-ralph.app"

  # The app is ad-hoc signed (free, no Apple Developer cert), so Gatekeeper
  # would quarantine it on first launch. Strip the quarantine attribute after
  # install so it opens cleanly — the standard OSS-cask approach for an
  # un-notarized app. (Homebrew does NOT remove quarantine by default.)
  postflight do
    system_command "/usr/bin/xattr",
                   args: ["-dr", "com.apple.quarantine", "#{appdir}/radioactive-ralph.app"],
                   sudo: false
  end

  caveats <<~EOS
    Install the CLI cask, start the supervisor, and register a project:

      brew install --cask radioactive-ralph
      radioactive_ralph service install
      cd /path/to/repo && radioactive_ralph --init

    The desktop app and the terminal UI are peers on the same local supervisor.
  EOS
end
CASK

mapfile -t existing < <(release_gh release view "v${VERSION}" \
  --repo "$RELEASE_REPO" --json assets --jq '.assets[].name')
has_archive=false
has_bundle=false
printf '%s\n' "${existing[@]}" | grep -Fxq "$ARCHIVE" && has_archive=true
printf '%s\n' "${existing[@]}" | grep -Fxq "$BUNDLE" && has_bundle=true
if [[ "$has_archive" == true || "$has_bundle" == true ]]; then
  [[ "$has_archive" == true && "$has_bundle" == true ]] || {
    echo "package manifests: partial signed archive; quarantine required" >&2
    exit 1
  }
  release_gh release download "v${VERSION}" --repo "$RELEASE_REPO" \
    --pattern "$ARCHIVE" --output "$work/$ARCHIVE"
  release_gh release download "v${VERSION}" --repo "$RELEASE_REPO" \
    --pattern "$BUNDLE" --output "$work/$BUNDLE"
  "$COSIGN_BIN" verify-blob "$work/$ARCHIVE" --bundle "$work/$BUNDLE" \
    --certificate-identity \
    "https://github.com/${RELEASE_REPO}/.github/workflows/release.yml@refs/tags/v${VERSION}" \
    --certificate-oidc-issuer "https://token.actions.githubusercontent.com" \
    >/dev/null
  listing="$("$TAR_BIN" -tzf "$work/$ARCHIVE" | LC_ALL=C sort)"
  expected="$(printf '%s\n' \
    Casks/radioactive-ralph.rb Casks/radioactive-ralph-gui.rb \
    bucket/radioactive-ralph.json | LC_ALL=C sort)"
  [[ "$listing" == "$expected" ]]
  mkdir "$work/existing"
  "$TAR_BIN" -xzf "$work/$ARCHIVE" -C "$work/existing"
  for path in \
    Casks/radioactive-ralph.rb \
    Casks/radioactive-ralph-gui.rb \
    bucket/radioactive-ralph.json; do
    cmp -s "$work/payload/$path" "$work/existing/$path"
  done
  echo "package manifests: reused signed exact atomic PR payload"
  exit 0
fi

"$TAR_BIN" --sort=name --mtime='@0' --owner=0 --group=0 --numeric-owner \
  -czf "$work/$ARCHIVE" -C "$work/payload" \
  Casks/radioactive-ralph.rb Casks/radioactive-ralph-gui.rb \
  bucket/radioactive-ralph.json
"$COSIGN_BIN" sign-blob "$work/$ARCHIVE" --bundle "$work/$BUNDLE" --yes
release_gh release upload "v${VERSION}" --repo "$RELEASE_REPO" \
  "$work/$ARCHIVE" "$work/$BUNDLE"
echo "package manifests: signed exact atomic PR payload uploaded"
