#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT
PRIOR_OID=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
TAR_BIN_OVERRIDE=tar
if command -v gtar >/dev/null 2>&1; then
  TAR_BIN_OVERRIDE=gtar
fi

cat > "$TMP/gh" <<'FAKEGH'
#!/usr/bin/env bash
set -euo pipefail

kind="${1:?}"
shift
request="${*: -1}"
include_status=0
for argument in "$@"; do
  [[ "$argument" == "--include" ]] && include_status=1
done
if [[ "$kind" == release && "${1:-}" == view ]]; then
  [[ "$*" == \
    'view v1.2.3 --repo jbcom/radioactive-ralph --json isDraft,isPrerelease,tagName,assets' ]] || {
    echo "fake gh: unexpected release view $*" >&2
    exit 1
  }
  printf '{"isDraft":true,"isPrerelease":false,"tagName":"v1.2.3","assets":[{"name":"checksums.txt"}]}\n'
  exit 0
fi
if [[ "$kind" == release && "${1:-}" == upload ]]; then
  for argument in "$@"; do
    case "$argument" in
      */package-rollback.tar.gz|*/package-rollback.tar.gz.sigstore.json)
        cp "$argument" "$FAKE_STATE_DIR/$(basename "$argument")"
        ;;
    esac
  done
  touch "$FAKE_STATE_DIR/uploaded"
  exit 0
fi
if [[ "$kind" == api &&
      "$request" == repos/jbcom/pkgs/git/ref/heads/main ]]; then
  printf '%s\n' "$PRIOR_OID"
  exit 0
fi
if [[ "$kind" == api && "$request" == repos/jbcom/pkgs/contents/* ]]; then
  case "$request" in
    *Casks/radioactive-ralph.rb*)
      printf 'cli prior\n'
      ;;
    *Casks/radioactive-ralph-gui.rb*)
      if [[ "${FAKE_CONTENT_FAILURE:-0}" == 1 ]]; then
        ((include_status == 0)) || printf 'HTTP/2.0 500 Internal Server Error\n'
        echo "gh: server failure (HTTP 500)" >&2
      else
        ((include_status == 0)) || printf 'HTTP/2.0 404 Not Found\n'
        echo "gh: Not Found (HTTP 404)" >&2
      fi
      exit 1
      ;;
    *bucket/radioactive-ralph.json*)
      printf '{"version":"1.2.2"}\n'
      ;;
    *)
      exit 1
      ;;
  esac
  exit 0
fi
echo "fake gh: unexpected $kind $*" >&2
exit 1
FAKEGH
chmod +x "$TMP/gh"

cat > "$TMP/cosign" <<'FAKECOSIGN'
#!/usr/bin/env bash
set -euo pipefail

[[ "${1:-}" == sign-blob ]]
bundle=""
while (($#)); do
  case "$1" in
    --bundle)
      bundle="${2:?}"
      shift 2
      ;;
    *)
      shift
      ;;
  esac
done
[[ -n "$bundle" ]]
printf '{"fixture":"signed"}\n' > "$bundle"
FAKECOSIGN
chmod +x "$TMP/cosign"

run_prepare() {
  PKGS_GH_TOKEN=fake-pkgs \
  RELEASE_GH_TOKEN=fake-release \
  GH_BIN="$TMP/gh" \
  COSIGN_BIN="$TMP/cosign" \
  TAR_BIN="$TAR_BIN_OVERRIDE" \
  FAKE_STATE_DIR="$1" \
  PRIOR_OID="$PRIOR_OID" \
    bash "$ROOT/scripts/ci/prepare_package_rollback_provenance.sh" 1.2.3
}

state="$TMP/success"
mkdir "$state"
run_prepare "$state"
[[ -e "$state/uploaded" ]]

listing="$(tar -tzf "$state/package-rollback.tar.gz" | LC_ALL=C sort)"
expected="$(printf '%s\n' \
  provenance.json \
  Casks/radioactive-ralph.rb \
  bucket/radioactive-ralph.json | LC_ALL=C sort)"
[[ "$listing" == "$expected" ]]

mkdir "$state/extracted"
tar -xzf "$state/package-rollback.tar.gz" -C "$state/extracted"
jq -e --arg prior "$PRIOR_OID" '
  .schema == 2 and .release_version == "1.2.3" and
  .prior_main_oid == $prior and
  .files["Casks/radioactive-ralph.rb"].state == "present" and
  (.files["Casks/radioactive-ralph.rb"].sha256 |
    test("^[0-9a-f]{64}$")) and
  .files["Casks/radioactive-ralph-gui.rb"] == {"state":"missing"} and
  .files["bucket/radioactive-ralph.json"].state == "present" and
  (.files["bucket/radioactive-ralph.json"].sha256 |
    test("^[0-9a-f]{64}$"))
' "$state/extracted/provenance.json" >/dev/null
[[ ! -e "$state/extracted/Casks/radioactive-ralph-gui.rb" ]]

failed_state="$TMP/fetch-failure"
mkdir "$failed_state"
if FAKE_CONTENT_FAILURE=1 run_prepare "$failed_state" >/dev/null 2>&1; then
  echo "expected non-404 prior-manifest fetch failure to fail closed" >&2
  exit 1
fi
[[ ! -e "$failed_state/uploaded" ]]

echo "package rollback provenance tests: ok"
