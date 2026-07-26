#!/usr/bin/env bash
# Smoke the actual GoReleaser output: archive, deb, rpm, and docs installer.
# Run after either a snapshot or tagged release build has populated dist/.
set -euo pipefail

HOST_OS="${SMOKE_UNAME_S:-$(uname -s)}"
if [[ "$HOST_OS" != "Linux" ]]; then
  echo "artifact smoke: requires Ubuntu Linux on x86_64/amd64 or aarch64/arm64; got $HOST_OS" >&2
  exit 1
fi

CONTROL_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
ROOT="${SOURCE_ROOT:-$CONTROL_ROOT}"
# shellcheck source=scripts/ci/package_guidance_contract.sh
source "$ROOT/scripts/ci/package_guidance_contract.sh"
VERSION="$(jq -er '.version' "$ROOT/dist/metadata.json")"
SCOOP_MANIFEST="$ROOT/dist/scoop/bucket/radioactive-ralph.json"
if ! ralph_validate_scoop_manifest_contract_file "$SCOOP_MANIFEST"; then
  echo "artifact smoke: Scoop manifest violates the native Windows support contract" >&2
  exit 1
fi
ralph_validate_goreleaser_release_footer "$ROOT/.goreleaser.yaml"
SMOKE="$(mktemp -d)"
trap '[[ -n "${SERVER_PID:-}" ]] && kill "$SERVER_PID" 2>/dev/null || true; rm -rf "$SMOKE"' EXIT
case "$(uname -m)" in
  x86_64|amd64) ARCH=amd64 ;;
  aarch64|arm64) ARCH=arm64 ;;
  *)
    echo "artifact smoke: unsupported native architecture $(uname -m)" >&2
    exit 1
    ;;
esac

tar -xzf "$ROOT/dist/radioactive_ralph_${VERSION}_linux_${ARCH}.tar.gz" \
  -C "$SMOKE"
"$SMOKE/radioactive_ralph" --version | grep -F "$VERSION"

sudo apt-get update
sudo apt-get install -y \
  "$ROOT/dist/radioactive-ralph_${VERSION}_linux_${ARCH}.deb"
radioactive_ralph --version | grep -F "$VERSION"
sudo dpkg --remove radioactive-ralph
sudo apt-get install -y rpm
sudo rpm --install "$ROOT/dist/radioactive-ralph_${VERSION}_linux_${ARCH}.rpm"
radioactive_ralph --version | grep -F "$VERSION"
sudo rpm --erase radioactive-ralph

TAG="v${VERSION}"
mkdir -p "$SMOKE/web/$TAG" "$SMOKE/bin" "$SMOKE/fake-bin"
cp "$ROOT/dist/radioactive_ralph_${VERSION}_linux_${ARCH}.tar.gz" \
  "$ROOT/dist/checksums.txt" \
  "$SMOKE/web/$TAG/"
if [[ -f "$ROOT/dist/checksums.txt.sigstore.json" ]]; then
  cp "$ROOT/dist/checksums.txt.sigstore.json" "$SMOKE/web/$TAG/"
else
  printf '{"snapshot":true}\n' > "$SMOKE/web/$TAG/checksums.txt.sigstore.json"
  cat > "$SMOKE/fake-bin/cosign" <<'COSIGN'
#!/usr/bin/env bash
set -euo pipefail
[[ "$*" == *"--certificate-identity https://github.com/jbcom/radioactive-ralph/.github/workflows/release.yml@refs/tags/"* ]]
[[ "$*" == *"--certificate-oidc-issuer https://token.actions.githubusercontent.com"* ]]
COSIGN
  chmod +x "$SMOKE/fake-bin/cosign"
fi
python3 -m http.server 8123 \
  --bind 127.0.0.1 \
  --directory "$SMOKE/web" \
  >"$SMOKE/http.log" 2>&1 &
SERVER_PID=$!
for attempt in {1..20}; do
  curl -fsS "http://127.0.0.1:8123/$TAG/checksums.txt" >/dev/null && break
  [[ "$attempt" != 20 ]] || {
    cat "$SMOKE/http.log" >&2
    exit 1
  }
  sleep 0.25
done
PATH="$SMOKE/fake-bin:$PATH" \
RADIOACTIVE_RALPH_RELEASE_BASE_URL="http://127.0.0.1:8123" \
  sh "$CONTROL_ROOT/docs/install.sh" \
    --version "$TAG" \
    --install-dir "$SMOKE/bin"
"$SMOKE/bin/radioactive_ralph" --version | grep -F "$VERSION"
