#!/usr/bin/env bash
# Build a native, version-stamped GUI executable and package that exact input
# with pinned Fyne tooling. Used by both PR CI and the tagged release workflow.
set -euo pipefail

VERSION="${1:?usage: build_gui_package.sh <version> <darwin|linux|windows>}"
TARGET="${2:?usage: build_gui_package.sh <version> <darwin|linux|windows>}"
[[ "$VERSION" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]]
[[ "$TARGET" == darwin || "$TARGET" == linux || "$TARGET" == windows ]]

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
BUILD_DATE="${BUILD_DATE:-$(date -u +%Y-%m-%dT%H:%M:%SZ)}"
BUILD_COMMIT="${GITHUB_SHA:-$(git -C "$ROOT" rev-parse HEAD)}"
BINARY="$ROOT/cmd/radioactive_ralph/radioactive-ralph"
[[ "$TARGET" == windows ]] && BINARY="${BINARY}.exe"
EXPECTED_VERSION="$VERSION ($BUILD_COMMIT, built $BUILD_DATE)"

CGO_ENABLED=1 go build \
  -tags gui \
  -trimpath \
  -ldflags "-s -w -X main.Version=$VERSION -X main.Commit=$BUILD_COMMIT -X main.Date=$BUILD_DATE" \
  -o "$BINARY" \
  "$ROOT/cmd/radioactive_ralph"
"$BINARY" --version | grep -F "$EXPECTED_VERSION"
go version -m "$BINARY" | grep -F $'dep\tfyne.io/fyne/v2'

pushd "$ROOT/cmd/radioactive_ralph" >/dev/null
# Fyne v1.7.2's Windows packager rebuilds after injecting fyne.syso. Re-supply
# both the GUI tag and every release ldflag so the delivered executable retains
# the same product capability and version identity as the prebuilt input.
# Fyne 1.7.2 tokenizes GOFLAGS with strings.Fields, extracts each no-space
# -ldflags= field, and combines the values into one go build -ldflags argument.
# A quoted space-containing GOFLAGS field is incompatible with that parser;
# --release supplies -s and -w itself.
CGO_ENABLED=1 \
GOFLAGS="-trimpath -ldflags=-X=main.Version=$VERSION -ldflags=-X=main.Commit=$BUILD_COMMIT -ldflags=-X=main.Date=$BUILD_DATE" \
"$(go env GOPATH)/bin/fyne" package \
  --target "$TARGET" \
  --executable "$BINARY" \
  --tags gui \
  --release \
  --icon "$ROOT/packaging/icons/radioactive-ralph.png" \
  --name radioactive-ralph \
  --app-id com.jonbogaty.radioactive-ralph \
  --app-version "$VERSION"
case "$TARGET" in
  darwin) mv radioactive-ralph.app "$ROOT/" ;;
  linux) mv radioactive-ralph.tar.xz "$ROOT/" ;;
  windows) mv radioactive-ralph.exe "$ROOT/" ;;
esac
popd >/dev/null

case "$TARGET" in
  darwin)
    delivered="$(find "$ROOT/radioactive-ralph.app/Contents/MacOS" \
      -type f -perm -u+x | head -n 1)"
    ;;
  linux)
    unpack="$(mktemp -d)"
    trap 'rm -rf "$unpack"' EXIT
    tar -xJf "$ROOT/radioactive-ralph.tar.xz" -C "$unpack"
    delivered="$(find "$unpack" -type f -path '*/bin/*' -perm -u+x | head -n 1)"
    ;;
  windows) delivered="$ROOT/radioactive-ralph.exe" ;;
esac
[[ -n "$delivered" && -x "$delivered" ]]
"$delivered" --version | grep -F "$EXPECTED_VERSION"
go version -m "$delivered" | grep -F $'dep\tfyne.io/fyne/v2'
