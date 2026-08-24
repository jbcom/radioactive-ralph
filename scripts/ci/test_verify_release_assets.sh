#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT
EMPTY_SHA=e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855

cat > "$TMP/gh" <<'FAKEGH'
#!/usr/bin/env bash
set -euo pipefail
kind="${1:?}"
shift
if [[ "$kind" == "api" ]]; then
  printf 'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\n'
  exit 0
fi
[[ "$kind" == "release" ]]
subcommand="${1:?}"
shift
assets=(
  radioactive_ralph_1.2.3_darwin_amd64.tar.gz
  radioactive_ralph_1.2.3_darwin_arm64.tar.gz
  radioactive_ralph_1.2.3_linux_amd64.tar.gz
  radioactive_ralph_1.2.3_linux_arm64.tar.gz
  radioactive_ralph_1.2.3_windows_amd64.zip
  radioactive-ralph_1.2.3_linux_amd64.deb
  radioactive-ralph_1.2.3_linux_arm64.deb
  radioactive-ralph_1.2.3_linux_amd64.rpm
  radioactive-ralph_1.2.3_linux_arm64.rpm
  radioactive-ralph_1.2.3_darwin_amd64.dmg
  radioactive-ralph_1.2.3_darwin_arm64.dmg
  radioactive-ralph_1.2.3_linux_x86_64.AppImage
  radioactive-ralph.exe
  checksums.txt
  checksums.txt.sigstore.json
  gui-checksums.txt
  gui-checksums.txt.sigstore.json
  package-rollback.tar.gz
  package-rollback.tar.gz.sigstore.json
  package-manifests.tar.gz
  package-manifests.tar.gz.sigstore.json
  release-seal.json
  release-seal.json.sigstore.json
)
if [[ "$subcommand" == "view" ]]; then
  if [[ " $* " == *' targetCommitish '* ]]; then
    printf 'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\n'
    exit 0
  fi
  for asset in "${assets[@]}"; do
    [[ "${FAKE_MISSING_ASSET:-}" == "$asset" ]] || printf '%s\n' "$asset"
  done
  [[ "${FAKE_EXTRA_ASSET:-0}" == "1" ]] && printf 'attacker.bin\n'
  exit 0
fi
[[ "$subcommand" == "download" ]]
pattern=""
output=""
while (($#)); do
  case "$1" in
    --pattern) pattern="${2:?}"; shift 2 ;;
    --output) output="${2:?}"; shift 2 ;;
    *) shift ;;
  esac
done
case "$pattern" in
  checksums.txt)
    for asset in "${assets[@]:0:9}"; do
      printf '%s *%s\n' "$EMPTY_SHA" "$asset"
    done > "$output"
    ;;
  gui-checksums.txt)
    for asset in "${assets[@]:9:4}"; do
      printf '%s *%s\n' "$EMPTY_SHA" "$asset"
    done > "$output"
    ;;
  package-rollback.tar.gz)
    payload="$(mktemp -d)"
    mkdir -p "$payload/Casks" "$payload/bucket"
    : > "$payload/Casks/radioactive-ralph.rb"
    : > "$payload/bucket/radioactive-ralph.json"
    printf '{"schema":2,"release_version":"1.2.3","prior_main_oid":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","files":{"Casks/radioactive-ralph.rb":{"state":"present","sha256":"%s"},"Casks/radioactive-ralph-gui.rb":{"state":"missing"},"bucket/radioactive-ralph.json":{"state":"present","sha256":"%s"}}}\n' \
      "$EMPTY_SHA" "$EMPTY_SHA" > "$payload/provenance.json"
    tar -czf "$output" -C "$payload" provenance.json \
      Casks/radioactive-ralph.rb bucket/radioactive-ralph.json
    rm -rf "$payload"
    ;;
  package-manifests.tar.gz)
    payload="$(mktemp -d)"
    mkdir -p "$payload/Casks" "$payload/bucket"
    : > "$payload/Casks/radioactive-ralph.rb"
    : > "$payload/Casks/radioactive-ralph-gui.rb"
    : > "$payload/bucket/radioactive-ralph.json"
    tar -czf "$output" -C "$payload" \
      Casks/radioactive-ralph.rb Casks/radioactive-ralph-gui.rb \
      bucket/radioactive-ralph.json
    rm -rf "$payload"
    ;;
  release-seal.json)
    output_dir="$(dirname "$output")"
    seal_assets='[]'
    for asset in "${assets[@]:0:21}"; do
      digest="$(sha256sum "$output_dir/$asset" | awk '{print $1}')"
      size="$(wc -c < "$output_dir/$asset" | tr -d ' ')"
      seal_assets="$(jq -c --arg name "$asset" --arg sha "$digest" \
        --argjson size "$size" '. + [{name:$name,size:$size,sha256:$sha}]' \
        <<<"$seal_assets")"
    done
    jq -n --argjson assets "$seal_assets" \
      '{schema:1,release_version:"1.2.3",tag:"v1.2.3",
        source_commit:"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
        workflow:"release.yml",assets:$assets}' > "$output"
    ;;
  *.sigstore.json) printf '{"fixture":true}\n' > "$output" ;;
  *)
    if [[ "${FAKE_CLOBBER_ASSET:-}" == "$pattern" ]]; then
      printf 'clobbered\n' > "$output"
    else
      : > "$output"
    fi
    ;;
esac
FAKEGH
chmod +x "$TMP/gh"

cat > "$TMP/cosign" <<'FAKECOSIGN'
#!/usr/bin/env bash
set -euo pipefail
[[ "${FAKE_COSIGN_FAILURE:-0}" != "1" ]]
[[ "$*" == *"--certificate-identity https://github.com/jbcom/radioactive-ralph/.github/workflows/release.yml@refs/tags/v1.2.3"* ]]
[[ "$*" == *"--certificate-oidc-issuer https://token.actions.githubusercontent.com"* ]]
FAKECOSIGN
chmod +x "$TMP/cosign"

export EMPTY_SHA
export GH_BIN="$TMP/gh"
export COSIGN_BIN="$TMP/cosign"
export RELEASE_GH_TOKEN=fake-release-token

bash "$ROOT/scripts/ci/verify_release_assets.sh" 1.2.3

if FAKE_MISSING_ASSET=radioactive-ralph.exe \
  bash "$ROOT/scripts/ci/verify_release_assets.sh" 1.2.3 >/dev/null 2>&1; then
  echo "expected missing asset to fail" >&2
  exit 1
fi
if FAKE_EXTRA_ASSET=1 \
  bash "$ROOT/scripts/ci/verify_release_assets.sh" 1.2.3 >/dev/null 2>&1; then
  echo "expected extra asset to fail" >&2
  exit 1
fi
if FAKE_COSIGN_FAILURE=1 \
  bash "$ROOT/scripts/ci/verify_release_assets.sh" 1.2.3 >/dev/null 2>&1; then
  echo "expected Sigstore failure to fail" >&2
  exit 1
fi
if FAKE_CLOBBER_ASSET=radioactive-ralph.exe \
  bash "$ROOT/scripts/ci/verify_release_assets.sh" 1.2.3 >/dev/null 2>&1; then
  echo "expected clobbered bytes to fail" >&2
  exit 1
fi

echo "release asset verifier tests: ok"
