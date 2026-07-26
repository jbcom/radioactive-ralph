#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT
ASSETS="$TMP/assets"
mkdir "$ASSETS"
VERSION=0.22.0
SOURCE_COMMIT=bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb
WORKFLOW_COMMIT=cccccccccccccccccccccccccccccccccccccccc
expected=(
  "radioactive_ralph_${VERSION}_darwin_amd64.tar.gz"
  "radioactive_ralph_${VERSION}_darwin_arm64.tar.gz"
  "radioactive_ralph_${VERSION}_linux_amd64.tar.gz"
  "radioactive_ralph_${VERSION}_linux_arm64.tar.gz"
  "radioactive_ralph_${VERSION}_windows_amd64.zip"
  "radioactive-ralph_${VERSION}_linux_amd64.deb"
  "radioactive-ralph_${VERSION}_linux_arm64.deb"
  "radioactive-ralph_${VERSION}_linux_amd64.rpm"
  "radioactive-ralph_${VERSION}_linux_arm64.rpm"
  "radioactive-ralph_${VERSION}_darwin_amd64.dmg"
  "radioactive-ralph_${VERSION}_darwin_arm64.dmg"
  "radioactive-ralph_${VERSION}_linux_x86_64.AppImage"
  radioactive-ralph.exe
  checksums.txt
  checksums.txt.sigstore.json
  gui-checksums.txt
  gui-checksums.txt.sigstore.json
  package-rollback.tar.gz
  package-rollback.tar.gz.sigstore.json
  package-manifests.tar.gz
  package-manifests.tar.gz.sigstore.json
)
for asset in "${expected[@]}"; do
  printf 'fixture for %s\n' "$asset" > "$ASSETS/$asset"
done

cat > "$TMP/gh" <<'FAKEGH'
#!/usr/bin/env bash
set -euo pipefail
[[ "${1:-}" == api ]]
if [[ "$*" == *"uploads.github.com"*"assets?name="* ]]; then
  name=""
  for argument in "$@"; do
    case "$argument" in
      repos/*/releases/*/assets?name=*)
        name="${argument#*assets?name=}"
        ;;
    esac
  done
  [[ -n "$name" ]]
  input="${*: -1}"
  cp "$input" "$FAKE_ASSETS/$name"
  mapfile -t names < <(
    find "$FAKE_ASSETS" -maxdepth 1 -type f -exec basename {} \; |
      LC_ALL=C sort
  )
  id=0
  for index in "${!names[@]}"; do
    [[ "${names[$index]}" == "$name" ]] && id=$((index + 1))
  done
  jq -cn --arg name "$name" --argjson id "$id" \
    '{id:$id,name:$name,state:"uploaded"}'
  exit 0
fi
endpoint="${*: -1}"
case "$endpoint" in
  repos/jbcom/radioactive-ralph/releases/123)
    jq -cn --arg target "$RELEASE_SOURCE_COMMIT" '{
      id: 123, tag_name: "v0.22.0", target_commitish: $target,
      draft: true, prerelease: false, immutable: false
    }'
    ;;
  repos/jbcom/radioactive-ralph/releases/123/assets?per_page=100)
    find "$FAKE_ASSETS" -maxdepth 1 -type f -exec basename {} \; |
      LC_ALL=C sort |
      jq -Rn '
        [inputs] |
        to_entries |
        map({id: (.key + 1), name: .value, state: "uploaded"}) |
        [.]
      '
    ;;
  repos/jbcom/radioactive-ralph/releases/assets/*)
    id="${endpoint##*/}"
    mapfile -t names < <(
      find "$FAKE_ASSETS" -maxdepth 1 -type f -exec basename {} \; |
        LC_ALL=C sort
    )
    [[ "$id" =~ ^[1-9][0-9]*$ && "$id" -le "${#names[@]}" ]]
    cat "$FAKE_ASSETS/${names[$((id - 1))]}"
    ;;
  *)
    echo "unexpected fake gh invocation: $*" >&2
    exit 1
    ;;
esac
FAKEGH
chmod +x "$TMP/gh"

cat > "$TMP/cosign" <<'FAKECOSIGN'
#!/usr/bin/env bash
set -euo pipefail
case "${1:-}" in
  sign-blob)
    while (($#)); do
      if [[ "$1" == --bundle ]]; then
        printf '{"fixture":"signed"}\n' > "${2:?}"
        exit 0
      fi
      shift
    done
    ;;
  verify-blob)
    [[ "$*" == *"release.yml@refs/heads/main"* ]]
    [[ "$*" == *"--certificate-github-workflow-sha ${WORKFLOW_COMMIT:?}"* ]]
    exit 0
    ;;
esac
exit 1
FAKECOSIGN
chmod +x "$TMP/cosign"

run_prepare() {
  local workflow_commit="$WORKFLOW_COMMIT"
  FAKE_ASSETS="$ASSETS" \
  GH_BIN="$TMP/gh" \
  COSIGN_BIN="$TMP/cosign" \
  RELEASE_GH_TOKEN=fake \
  RELEASE_REPO=jbcom/radioactive-ralph \
  RELEASE_ID=123 \
  RELEASE_SOURCE_COMMIT="$SOURCE_COMMIT" \
  WORKFLOW_COMMIT="$workflow_commit" \
    bash "$ROOT/scripts/ci/prepare_release_seal.sh" \
      "$VERSION" "$SOURCE_COMMIT" 123 "$workflow_commit"
}

run_prepare
test -f "$ASSETS/release-seal.json"
test -f "$ASSETS/release-seal.json.sigstore.json"
first_seal="$(shasum -a 256 "$ASSETS/release-seal.json" | awk '{print $1}')"
run_prepare
[[ "$(shasum -a 256 "$ASSETS/release-seal.json" | awk '{print $1}')" == \
  "$first_seal" ]]

rm "$ASSETS/release-seal.json.sigstore.json"
run_prepare
test -f "$ASSETS/release-seal.json.sigstore.json"

rm "$ASSETS/release-seal.json.sigstore.json"
printf 'changed fixture\n' >> "$ASSETS/checksums.txt"
if run_prepare >/dev/null 2>&1; then
  echo "expected changed bytes under a partial seal to fail" >&2
  exit 1
fi

echo "release seal resumability tests: ok"
