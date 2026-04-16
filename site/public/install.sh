#!/bin/sh
# radioactive-ralph curl-pipe installer.
#
# Usage:
#   curl -sSL https://jonbogaty.com/radioactive-ralph/install.sh | sh
#   curl -sSL https://jonbogaty.com/radioactive-ralph/install.sh | sh -s -- --version v0.7.0
#
# Downloads the appropriate GitHub release archive, verifies the
# checksum, extracts radioactive_ralph into $INSTALL_DIR (default
# /usr/local/bin if writable, else ~/.local/bin), and prints the
# next-step repo bootstrap guidance.

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

checksum_check() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum -c -
    return
  fi
  if command -v shasum >/dev/null 2>&1; then
    shasum -a 256 -c -
    return
  fi
  echo "install.sh: no SHA-256 checksum tool found (need sha256sum or shasum)" >&2
  exit 1
}

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

( cd "$TMP" && grep "  $ARCHIVE\$" checksums.txt | checksum_check ) || {
  echo "install.sh: checksum verification failed" >&2; exit 1
}

echo "Extracting to $INSTALL_DIR..."
tar -xzf "$TMP/$ARCHIVE" -C "$TMP"
if [ ! -s "$TMP/$BIN" ]; then
  echo "install.sh: binary not found in archive" >&2; exit 1
fi
if command -v file >/dev/null 2>&1 && ! file "$TMP/$BIN" 2>/dev/null | grep -Eq 'executable|Mach-O|ELF'; then
  echo "install.sh: warning: extracted file does not look like a native executable" >&2
fi
install -m 0755 "$TMP/$BIN" "$INSTALL_DIR/$BIN"

echo
echo "Installed $BIN $VERSION to $INSTALL_DIR"
echo

if ! printf '%s' "${PATH:-}" | tr ':' '\n' | grep -Fx "$INSTALL_DIR" >/dev/null 2>&1; then
  PROFILE="$HOME/.profile"
  if [ -n "${SHELL:-}" ]; then
    case "$(basename "$SHELL")" in
      zsh) PROFILE="$HOME/.zprofile" ;;
      bash) PROFILE="$HOME/.bash_profile" ;;
    esac
  fi
  echo "Your current PATH does not include $INSTALL_DIR"
  echo "Add this line to $PROFILE if you want the binary available in new shells:"
  echo
  echo "  export PATH=\"$INSTALL_DIR:\$PATH\""
  echo
fi

echo "Next step — initialize a repo and let Fixit seed the first plan:"
echo
echo "  cd /path/to/repo"
echo "  $INSTALL_DIR/$BIN init"
echo "  $INSTALL_DIR/$BIN run --variant fixit --advise --topic bootstrap"
echo "  $INSTALL_DIR/$BIN service start"
echo
echo "Use '$INSTALL_DIR/$BIN tui' for the repo cockpit once the service is running."
