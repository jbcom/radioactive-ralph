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

# AppRun must exec the binary at its real location inside the AppDir. fyne
# extracts it to <pkgdir>/usr/local/bin/<exe>, so use EXE's path RELATIVE to the
# AppDir root — not basename(dirname) (which would wrongly be $HERE/bin/<exe>).
REL="${EXE#"$APPDIR"/}"
cat > "$APPDIR/AppRun" <<APPRUN
#!/bin/sh
HERE="\$(dirname "\$(readlink -f "\$0")")"
exec "\$HERE/$REL" "\$@"
APPRUN
chmod +x "$APPDIR/AppRun"

# Fetch appimagetool if not already present.
# Pin appimagetool to a specific release + verify its SHA-256. The `continuous`
# tag is mutable (a compromised or updated build would run unverified), so we
# fetch a fixed version and refuse to run it if the hash doesn't match.
APPIMAGETOOL_VERSION="1.9.1"
case "$ARCH" in
  x86_64)  TOOL_SHA="ed4ce84f0d9caff66f50bcca6ff6f35aae54ce8135408b3fa33abfc3cb384eb0" ;;
  aarch64) TOOL_SHA="f0837e7448a0c1e4e650a93bb3e85802546e60654ef287576f46c71c126a9158" ;;
  *) echo "build-appimage: no pinned appimagetool for arch $ARCH" >&2; exit 1 ;;
esac
TOOL="$(mktemp -d)/appimagetool"
TOOL_URL="https://github.com/AppImage/appimagetool/releases/download/${APPIMAGETOOL_VERSION}/appimagetool-${ARCH}.AppImage"

# Retry the DOWNLOAD, never the verification. A truncated transfer and a
# tampered artifact both surface as a hash mismatch, and only one of them is
# worth retrying — so each attempt re-downloads from scratch and is checked
# independently, and the script still refuses to run anything unverified.
#
# This exists because a single corrupted fetch failed CI while upstream was
# serving exactly the pinned bytes: verified by re-downloading the same URL and
# getting the pinned SHA-256. Without a retry, one bad transfer of a 15MB
# artifact fails the whole packaging job.
fetched=no
for attempt in 1 2 3; do
  # --fail so an HTTP error is not written to disk as if it were the artifact;
  # --retry covers transport-level blips within a single attempt.
  if curl -sSL --fail --retry 2 --retry-delay 2 -o "$TOOL" "$TOOL_URL" &&
     echo "${TOOL_SHA}  ${TOOL}" | sha256sum -c - >/dev/null 2>&1; then
    fetched=yes
    break
  fi
  echo "build-appimage: appimagetool fetch/verify attempt ${attempt} failed" >&2
  rm -f "$TOOL"
  sleep $((attempt * 3))
done
if [ "$fetched" != "yes" ]; then
  echo "build-appimage: appimagetool SHA-256 mismatch after 3 attempts — refusing to run" >&2
  echo "build-appimage: expected ${TOOL_SHA} from ${TOOL_URL}" >&2
  exit 1
fi
chmod +x "$TOOL"

OUT="radioactive-ralph_${VERSION}_linux_${ARCH}.AppImage"
# appimagetool is itself an AppImage, so running it normally needs libfuse2 —
# which GitHub's ubuntu-latest runners do NOT ship. APPIMAGE_EXTRACT_AND_RUN=1
# tells it to self-extract and run without FUSE, so the build works on a stock
# runner with no extra apt packages. ARCH is what appimagetool stamps into the
# produced runtime.
ARCH="$ARCH" APPIMAGE_EXTRACT_AND_RUN=1 "$TOOL" --no-appstream "$APPDIR" "$OUT"
echo "build-appimage: wrote $OUT"
