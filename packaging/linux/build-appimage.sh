#!/usr/bin/env bash
# Build a radioactive-ralph AppImage from the tarball `fyne package --target
# linux` produces. Run from the repo root after the package step:
#
#   packaging/linux/build-appimage.sh <version>
#
# Produces radioactive-ralph_<version>_linux_<arch>.AppImage in the cwd.
# AppImages are unsigned by convention; the release checksum is the integrity
# anchor. See docs/superpowers/specs/2026-07-17-native-packaging-design.md.
set -euo pipefail

VERSION="${1:?usage: build-appimage.sh <version>}"
ARCH="$(uname -m)"          # x86_64 / aarch64
ROOT="$(cd "$(dirname "$0")/../.." && pwd)"

# `fyne package --target linux` writes a .tar.xz rootfs (usr/local/bin/<exe> +
# usr/local/share/{applications,icons}). Discover it by glob rather than hard-
# coding the name, which has varied across fyne CLI versions.
TARBALL="$(find . -maxdepth 1 -name '*.tar.xz' | head -n1)"
if [ -z "$TARBALL" ]; then
  echo "build-appimage: no .tar.xz found (run 'fyne package --target linux' first)" >&2
  exit 1
fi

APPDIR="$(mktemp -d)/AppDir"
mkdir -p "$APPDIR"
tar -xJf "$TARBALL" -C "$APPDIR"

# The executable fyne installed (name may be radioactive_ralph or
# radioactive-ralph depending on the tool version); find it.
EXE="$(find "$APPDIR" -type f -path '*/bin/*' -perm -u+x | head -n1)"
if [ -z "$EXE" ]; then
  echo "build-appimage: no executable found in the fyne tarball" >&2
  exit 1
fi

# AppImage requires AppRun + a top-level .desktop + icon. Use the committed
# .desktop; the icon name in it (radioactive-ralph) must match the icon file.
install -m 0644 "$ROOT/packaging/linux/radioactive-ralph.desktop" "$APPDIR/radioactive-ralph.desktop"
install -m 0644 "$ROOT/packaging/icons/radioactive-ralph.png" "$APPDIR/radioactive-ralph.png"

cat > "$APPDIR/AppRun" <<APPRUN
#!/bin/sh
HERE="\$(dirname "\$(readlink -f "\$0")")"
exec "\$HERE/$(basename "$(dirname "$EXE")")/$(basename "$EXE")" "\$@"
APPRUN
chmod +x "$APPDIR/AppRun"

# Fetch appimagetool if not already present.
TOOL="$(command -v appimagetool || true)"
if [ -z "$TOOL" ]; then
  TOOL="$(mktemp -d)/appimagetool"
  curl -sSL -o "$TOOL" \
    "https://github.com/AppImage/appimagetool/releases/download/continuous/appimagetool-${ARCH}.AppImage"
  chmod +x "$TOOL"
fi

OUT="radioactive-ralph_${VERSION}_linux_${ARCH}.AppImage"
# ARCH env is what appimagetool stamps into the runtime.
ARCH="$ARCH" "$TOOL" --no-appstream "$APPDIR" "$OUT"
echo "build-appimage: wrote $OUT"
