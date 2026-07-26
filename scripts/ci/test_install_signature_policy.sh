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

echo "installer signature policy tests: ok"
