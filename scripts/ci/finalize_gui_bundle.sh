#!/usr/bin/env bash
# Produce and smoke the actual GUI delivery shape from build_gui_package.sh.
# Used before tags in CI and again by the release workflow.
set -euo pipefail

VERSION="${1:?usage: finalize_gui_bundle.sh <version> <target> <arch>}"
TARGET="${2:?usage: finalize_gui_bundle.sh <version> <target> <arch>}"
ARCH="${3:?usage: finalize_gui_bundle.sh <version> <target> <arch>}"
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"

case "$TARGET" in
  darwin)
    codesign --force --deep --sign - "$ROOT/radioactive-ralph.app"
    DMG="$ROOT/radioactive-ralph_${VERSION}_darwin_${ARCH}.dmg"
    hdiutil create \
      -volname radioactive-ralph \
      -srcfolder "$ROOT/radioactive-ralph.app" \
      -ov \
      -format UDZO \
      "$DMG"
    [[ -s "$DMG" ]]
    mountpoint="$(mktemp -d)"
    cleanup() {
      hdiutil detach "$mountpoint" >/dev/null 2>&1 || true
      rmdir "$mountpoint" >/dev/null 2>&1 || true
    }
    trap cleanup EXIT
    hdiutil attach -nobrowse -readonly -mountpoint "$mountpoint" "$DMG" >/dev/null
    binary="$(find "$mountpoint/radioactive-ralph.app/Contents/MacOS" \
      -type f -perm -u+x | head -n 1)"
    "$binary" --version | grep -F "$VERSION"
    go version -m "$binary" | grep -F $'dep\tfyne.io/fyne/v2'
    cleanup
    trap - EXIT
    ;;
  linux)
    bash "$ROOT/packaging/linux/build-appimage.sh" "$VERSION"
    appimage="$(find "$ROOT" -maxdepth 1 -name '*.AppImage' -perm -u+x | head -n 1)"
    [[ -n "$appimage" ]]
    APPIMAGE_EXTRACT_AND_RUN=1 "$appimage" --version | grep -F "$VERSION"
    ;;
  windows)
    exe="$ROOT/radioactive-ralph.exe"
    [[ -x "$exe" ]]
    "$exe" --version | grep -F "$VERSION"
    go version -m "$exe" | grep -F $'dep\tfyne.io/fyne/v2'
    ;;
  *)
    echo "finalize GUI bundle: unsupported target $TARGET" >&2
    exit 1
    ;;
esac
