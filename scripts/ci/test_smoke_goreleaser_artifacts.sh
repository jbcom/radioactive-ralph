#!/usr/bin/env bash

set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

if SMOKE_UNAME_S=Darwin \
  bash "$ROOT/scripts/ci/smoke_goreleaser_artifacts.sh" \
  >"$TMP/stdout" 2>"$TMP/stderr"; then
  echo "expected non-Linux artifact smoke to fail before inspecting dist" >&2
  exit 1
fi
grep -F \
  "artifact smoke: requires Ubuntu Linux on x86_64/amd64 or aarch64/arm64; got Darwin" \
  "$TMP/stderr" >/dev/null

echo "artifact smoke platform tests: ok"
