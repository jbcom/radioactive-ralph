#!/usr/bin/env bash
set -euo pipefail

if [[ $# -ne 1 ]]; then
  echo "usage: $0 /absolute/path/to/radioactive_ralph" >&2
  exit 2
fi

bin="$1"
if [[ ! -x "$bin" ]]; then
  echo "binary is not executable: $bin" >&2
  exit 2
fi

tmpdir="$(mktemp -d /tmp/ralph-launchd-XXXXXX)"
repo="$tmpdir/repo"
state="$tmpdir/state"
log="$tmpdir/launchd-smoke.log"
mkdir -p "$repo" "$state"

cleanup() {
  if [[ -n "${label:-}" ]]; then
    launchctl bootout "gui/$(id -u)/$label" >/dev/null 2>&1 || true
    launchctl bootout "gui/$(id -u)" "$install_path" >/dev/null 2>&1 || true
  fi
  if [[ -n "${repo:-}" ]]; then
    "$bin" service uninstall --repo-root "$repo" >/dev/null 2>&1 || true
  fi
  rm -rf "$tmpdir"
}
trap cleanup EXIT

RALPH_STATE_DIR="$state" "$bin" init --repo-root "$repo" --yes >/dev/null
install_path="$("$bin" service install --repo-root "$repo" --radioactive_ralph-bin "$bin" --env "RALPH_STATE_DIR=$state" | awk '{print $2}')"
"$bin" service list | grep -F "$install_path" >/dev/null

label="$(basename "$install_path" .plist)"
domain="gui/$(id -u)"

launchctl bootstrap "$domain" "$install_path"
launchctl kickstart -k "$domain/$label" >/dev/null 2>&1 || true

ready=0
for _ in $(seq 1 30); do
  if RALPH_STATE_DIR="$state" "$bin" status --repo-root "$repo" --json >"$tmpdir/status.json" 2>/dev/null; then
    ready=1
    break
  fi
  sleep 1
done
if [[ "$ready" -ne 1 ]]; then
  launchctl print "$domain/$label" >&2 || true
  echo "launchd service never became ready" >&2
  exit 1
fi

"$bin" service status --repo-root "$repo" >/dev/null

launchctl bootout "$domain/$label"

stopped=0
for _ in $(seq 1 30); do
  if ! RALPH_STATE_DIR="$state" "$bin" status --repo-root "$repo" >/dev/null 2>&1; then
    stopped=1
    break
  fi
  sleep 1
done
if [[ "$stopped" -ne 1 ]]; then
  launchctl print "$domain/$label" >&2 || true
  echo "launchd service never stopped" >&2
  exit 1
fi

echo "launchd smoke: ok" | tee "$log"
