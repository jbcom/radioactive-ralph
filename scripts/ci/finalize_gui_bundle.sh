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
    # hdiutil create fails with "Resource busy" when a previous DMG for this
    # path is still attached. On a REUSED runner that attachment can outlive
    # the job that made it -- a real failure observed on macos-15-intel, whose
    # post-job cleanup then reported terminating an orphan diskimages-help
    # process. The job before it had leaked the device.
    #
    # Detaching by path is not enough: the stale device belongs to a file that
    # may already be gone, so ask hdiutil which devices are backed by THIS dmg
    # path and detach each one before creating.
    detach_stale_devices() {
      # No [[ -e "$DMG" ]] guard: the whole point is that the DEVICE can
      # outlive the FILE, so an early return on a missing file would skip
      # exactly the case this function exists for. hdiutil info reports
      # attached devices regardless of whether the backing file still exists.
      # Block layout, verified against real `hdiutil info` output: each image
      # starts at a ==== separator, `image-path` comes BEFORE its /dev/diskN
      # entries, and the device field is /dev/disk5 (not a bare "/dev/disk"),
      # so match on the prefix and buffer per block rather than per line.
      hdiutil info 2>/dev/null \
        | awk -v img="$DMG" '
            /^=====/                        { mine = 0; next }
            /^image-path/                   { mine = (index($0, img) > 0); next }
            mine && $1 ~ /^\/dev\/disk[0-9]+$/ { print $1 }
          ' \
        | while read -r dev; do
            hdiutil detach "$dev" -force >/dev/null 2>&1 || true
          done
    }

    # Retry rather than fail the build outright: the busy device can also be
    # released asynchronously a moment after the previous step finishes, so a
    # single attempt turns a transient condition into a red build.
    for attempt in 1 2 3; do
      detach_stale_devices
      if hdiutil create \
        -volname radioactive-ralph \
        -srcfolder "$ROOT/radioactive-ralph.app" \
        -ov \
        -format UDZO \
        "$DMG"; then
        break
      fi
      if [[ "$attempt" == 3 ]]; then
        echo "hdiutil create failed after 3 attempts; attached images:" >&2
        hdiutil info >&2 || true
        exit 1
      fi
      echo "hdiutil create failed (attempt $attempt); retrying" >&2
      sleep 5
    done
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
