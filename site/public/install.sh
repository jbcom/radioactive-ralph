#!/bin/sh
# radioactive-ralph curl-pipe installer.
#
# Usage:
#   curl -sSL https://jonbogaty.com/radioactive-ralph/install.sh | sh
#   curl -sSL https://jonbogaty.com/radioactive-ralph/install.sh | sh -s -- --version v0.6.1
#
# Downloads the appropriate GitHub release archive, verifies the
# checksum, extracts radioactive_ralph into $INSTALL_DIR (default
# /usr/local/bin if writable, else ~/.local/bin), and prints the
# next-step MCP registration command.

set -eu

REPO="jbcom/radioactive-ralph"
BIN="radioactive_ralph"
VERSION="latest"
INSTALL_DIR=""

while [ $# -gt 0 ]; do
  case "$1" in
    --version)
      VERSION="$2"; shift 2 ;;
    --install-dir)
      INSTALL_DIR="$2"; shift 2 ;;
    --help|-h)
      sed -n '2,10p' "$0"; exit 0 ;;
    *)
      echo "install.sh: unknown argument: $1" >&2; exit 2 ;;
  esac
done

# --- platform detection -----------------------------------------------------

uname_os() {
  os=$(uname -s | tr '[:upper:]' '[:lower:]')
  case "$os" in
    darwin) echo darwin ;;
    linux)  echo linux ;;
    msys*|mingw*|cygwin*)
      echo "install.sh: Windows detected; use Scoop or Chocolatey instead" >&2
      exit 1 ;;
    *)
      echo "install.sh: unsupported OS: $os" >&2; exit 1 ;;
  esac
}

uname_arch() {
  arch=$(uname -m)
  case "$arch" in
    x86_64|amd64) echo amd64 ;;
    aarch64|arm64) echo arm64 ;;
    *)
      echo "install.sh: unsupported arch: $arch" >&2; exit 1 ;;
  esac
}

OS=$(uname_os)
ARCH=$(uname_arch)

# --- install dir -----------------------------------------------------------

pick_install_dir() {
  if [ -n "$INSTALL_DIR" ]; then
    echo "$INSTALL_DIR"; return
  fi
  if [ -w /usr/local/bin ] 2>/dev/null; then
    echo /usr/local/bin; return
  fi
  mkdir -p "$HOME/.local/bin"
  echo "$HOME/.local/bin"
}

INSTALL_DIR=$(pick_install_dir)

# --- version resolution -----------------------------------------------------

resolve_version() {
  if [ "$VERSION" = "latest" ]; then
    VERSION=$(curl -sSL \
      "https://api.github.com/repos/$REPO/releases/latest" \
      | sed -n 's/.*"tag_name": *"\([^"]*\)".*/\1/p' \
      | head -n1)
    if [ -z "$VERSION" ]; then
      echo "install.sh: could not resolve latest version" >&2; exit 1
    fi
  fi
}

resolve_version
VERSION_NO_V=${VERSION#v}

# --- download + verify + install -------------------------------------------

TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT

ARCHIVE="${BIN}_${VERSION_NO_V}_${OS}_${ARCH}.tar.gz"
URL="https://github.com/$REPO/releases/download/$VERSION/$ARCHIVE"
CHECKSUMS_URL="https://github.com/$REPO/releases/download/$VERSION/checksums.txt"

echo "Downloading $ARCHIVE..."
curl -sSL -o "$TMP/$ARCHIVE" "$URL" || {
  echo "install.sh: download failed: $URL" >&2; exit 1
}

echo "Verifying checksum..."
curl -sSL -o "$TMP/checksums.txt" "$CHECKSUMS_URL" || {
  echo "install.sh: checksum download failed: $CHECKSUMS_URL" >&2; exit 1
}

( cd "$TMP" && grep "  $ARCHIVE\$" checksums.txt | sha256sum -c - ) || {
  echo "install.sh: checksum verification failed" >&2; exit 1
}

echo "Extracting to $INSTALL_DIR..."
tar -xzf "$TMP/$ARCHIVE" -C "$TMP"
install -m 0755 "$TMP/$BIN" "$INSTALL_DIR/$BIN"

echo
echo "Installed $BIN $VERSION to $INSTALL_DIR"
echo
echo "Next step — register as an MCP server for Claude Code:"
echo
echo "  $INSTALL_DIR/$BIN mcp register"
echo
echo "See https://jonbogaty.com/radioactive-ralph/guides/transports/"
echo "for transport options (stdio vs HTTP)."
