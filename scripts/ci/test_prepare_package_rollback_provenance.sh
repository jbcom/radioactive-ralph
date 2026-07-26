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
if [[ "$kind" == api &&
      "$request" == repos/jbcom/radioactive-ralph/releases/123 ]]; then
  jq -cn --arg target "$RELEASE_SOURCE_COMMIT" '{
    id: 123, tag_name: "v1.2.3", target_commitish: $target,
    draft: true, prerelease: false, immutable: false
  }'
  exit 0
fi
if [[ "$kind" == api &&
      "$*" == *"--paginate --slurp repos/jbcom/radioactive-ralph/releases/123/assets?per_page=100"* ]]; then
  find "$FAKE_STATE_DIR" -maxdepth 1 -type f \
    \( -name package-rollback.tar.gz -o \
       -name package-rollback.tar.gz.sigstore.json \) \
    -exec basename {} \; | LC_ALL=C sort |
    jq -Rn '[inputs | {id: (if . == "package-rollback.tar.gz" then 21 else 22 end), name: ., state: "uploaded"}] | [.]'
  exit 0
fi
if [[ "$kind" == api &&
      "$request" == repos/jbcom/radioactive-ralph/releases/assets/21 ]]; then
  cat "$FAKE_STATE_DIR/package-rollback.tar.gz"
  exit 0
fi
if [[ "$kind" == api &&
      "$request" == repos/jbcom/radioactive-ralph/releases/assets/22 ]]; then
  cat "$FAKE_STATE_DIR/package-rollback.tar.gz.sigstore.json"
  exit 0
fi
if [[ "$kind" == api && "$*" == *"uploads.github.com"* &&
      "$*" == *"assets?name=package-rollback.tar.gz --input "* ]]; then
  cp "${*: -1}" "$FAKE_STATE_DIR/package-rollback.tar.gz"
  printf '%s\n' '{"id":21,"name":"package-rollback.tar.gz","state":"uploaded"}'
  exit 0
fi
if [[ "$kind" == api && "$*" == *"uploads.github.com"* &&
      "$*" == *"assets?name=package-rollback.tar.gz.sigstore.json --input "* ]]; then
  cp "${*: -1}" "$FAKE_STATE_DIR/package-rollback.tar.gz.sigstore.json"
  touch "$FAKE_STATE_DIR/uploaded"
  printf '%s\n' '{"id":22,"name":"package-rollback.tar.gz.sigstore.json","state":"uploaded"}'
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

if [[ "${1:-}" == verify-blob ]]; then
  [[ "$*" == *"--certificate-github-workflow-sha ${RELEASE_WORKFLOW_COMMIT:?}"* ]]
  exit 0
fi
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
  RELEASE_ID=123 \
  RELEASE_SOURCE_COMMIT=bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb \
  RELEASE_WORKFLOW_COMMIT=cccccccccccccccccccccccccccccccccccccccc \
    bash "$ROOT/scripts/ci/prepare_package_rollback_provenance.sh" 1.2.3
}

state="$TMP/success"
mkdir "$state"
run_prepare "$state"
[[ -e "$state/uploaded" ]]
run_prepare "$state"

mv "$state/package-rollback.tar.gz.sigstore.json" "$TMP/original-bundle"
run_prepare "$state"
[[ -f "$state/package-rollback.tar.gz.sigstore.json" ]]

mv "$state/package-rollback.tar.gz" "$TMP/original-archive"
if run_prepare "$state" >/dev/null 2>&1; then
  echo "expected orphaned rollback signature bundle to fail" >&2
  exit 1
fi
mv "$TMP/original-archive" "$state/package-rollback.tar.gz"

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
