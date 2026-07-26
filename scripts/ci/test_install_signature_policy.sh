#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT
mkdir -p "$TMP/bin" "$TMP/install"

cat > "$TMP/bin/uname" <<'FAKEUNAME'
#!/bin/sh
case "${1:-}" in
  -s) printf '%s\n' Darwin ;;
  -m) printf '%s\n' arm64 ;;
  *) exit 2 ;;
esac
FAKEUNAME

cat > "$TMP/bin/curl" <<'FAKECURL'
#!/bin/sh
set -eu
output=
while [ "$#" -gt 0 ]; do
  case "$1" in
    -o) output=$2; shift 2 ;;
    -*) shift ;;
    *) url=$1; shift ;;
  esac
done
: "${output:?fake curl requires -o}"
: "${url:?fake curl requires URL}"
: > "$output"
FAKECURL

chmod +x "$TMP/bin/uname" "$TMP/bin/curl"
SAFE_PATH="$TMP/bin:/usr/bin:/bin"

if PATH="$SAFE_PATH" RADIOACTIVE_RALPH_REQUIRE_SIGNATURE=invalid \
  sh "$ROOT/docs/install.sh" --version v1.2.3 --install-dir "$TMP/install" \
  >"$TMP/invalid.out" 2>&1; then
  echo "expected invalid signature policy to fail" >&2
  exit 1
fi
grep -Fq 'RADIOACTIVE_RALPH_REQUIRE_SIGNATURE must be 0 or 1' \
  "$TMP/invalid.out"

if PATH="$SAFE_PATH" RADIOACTIVE_RALPH_REQUIRE_SIGNATURE=1 \
  sh "$ROOT/docs/install.sh" --version v1.2.3 --install-dir "$TMP/install" \
  >"$TMP/strict.out" 2>&1; then
  echo "expected strict install without cosign to fail" >&2
  exit 1
fi
grep -Fq 'cosign is required when RADIOACTIVE_RALPH_REQUIRE_SIGNATURE=1' \
  "$TMP/strict.out"

cat > "$TMP/bin/cosign" <<'FAKECOSIGN'
#!/bin/sh
exit 1
FAKECOSIGN
chmod +x "$TMP/bin/cosign"

if PATH="$SAFE_PATH" RADIOACTIVE_RALPH_REQUIRE_SIGNATURE=0 \
  sh "$ROOT/docs/install.sh" --version v1.2.3 --install-dir "$TMP/install" \
  >"$TMP/bad-signature.out" 2>&1; then
  echo "expected invalid available signature to fail closed" >&2
  exit 1
fi
grep -Fq 'signed checksum verification failed' "$TMP/bad-signature.out"

FIXTURES="$TMP/fixtures"
mkdir -p "$FIXTURES/payload"
printf '#!/bin/sh\nprintf "radioactive-ralph fixture\\n"\n' \
  > "$FIXTURES/payload/radioactive_ralph"
chmod +x "$FIXTURES/payload/radioactive_ralph"
archive=radioactive_ralph_0.22.0_darwin_arm64.tar.gz
tar -czf "$FIXTURES/$archive" -C "$FIXTURES/payload" radioactive_ralph
(
  cd "$FIXTURES"
  shasum -a 256 "$archive" > checksums.txt
)
printf '{"fixture":true}\n' > "$FIXTURES/checksums.txt.sigstore.json"
printf '%s\n' \
  '{' \
  '  "schema": 2,' \
  '  "workflow_commit": "cccccccccccccccccccccccccccccccccccccccc"' \
  '}' > "$FIXTURES/release-seal.json"
printf '{"fixture":true}\n' > "$FIXTURES/release-seal.json.sigstore.json"

cat > "$TMP/bin/curl" <<'FAKECURL'
#!/bin/sh
set -eu
output=
while [ "$#" -gt 0 ]; do
  case "$1" in
    -o) output=$2; shift 2 ;;
    -*) shift ;;
    *) url=$1; shift ;;
  esac
done
: "${output:?fake curl requires -o}"
: "${url:?fake curl requires URL}"
cp "${FIXTURES:?}/${url##*/}" "$output"
FAKECURL
cat > "$TMP/bin/cosign" <<'FAKECOSIGN'
#!/bin/sh
set -eu
printf '%s\n' "$*" >> "${COSIGN_CALLS:?}"
case "$*" in
  *"--certificate-identity https://github.com/jbcom/radioactive-ralph/.github/workflows/release.yml@refs/heads/main"*) ;;
  *) exit 1 ;;
esac
case "$*" in
  *"--certificate-github-workflow-sha cccccccccccccccccccccccccccccccccccccccc"*) ;;
  *) exit 1 ;;
esac
FAKECOSIGN
chmod +x "$TMP/bin/curl" "$TMP/bin/cosign"

COSIGN_CALLS="$TMP/cosign-calls"
export FIXTURES COSIGN_CALLS
if ! PATH="$SAFE_PATH" RADIOACTIVE_RALPH_REQUIRE_SIGNATURE=1 \
  sh "$ROOT/docs/install.sh" --version v0.22.0 --install-dir "$TMP/install" \
  >"$TMP/v022.out" 2>&1; then
  cat "$TMP/v022.out" >&2
  exit 1
fi
[[ "$(grep -Fc -- '--certificate-github-workflow-sha cccccccccccccccccccccccccccccccccccccccc' \
  "$COSIGN_CALLS")" == 2 ]]

sed 's/cccccccccccccccccccccccccccccccccccccccc/dddddddddddddddddddddddddddddddddddddddd/' \
  "$FIXTURES/release-seal.json" > "$TMP/mismatched-seal"
cp "$TMP/mismatched-seal" "$FIXTURES/release-seal.json"
if PATH="$SAFE_PATH" RADIOACTIVE_RALPH_REQUIRE_SIGNATURE=1 \
  sh "$ROOT/docs/install.sh" --version v0.22.0 --install-dir "$TMP/install" \
  >"$TMP/v022-mismatch.out" 2>&1; then
  echo "expected mismatched recovery workflow commit to fail" >&2
  exit 1
fi
grep -Fq 'signed release seal verification failed' "$TMP/v022-mismatch.out"

echo "installer signature policy tests: ok"
