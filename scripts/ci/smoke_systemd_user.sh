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

if ! systemctl --user show-environment >/dev/null 2>&1; then
  echo "systemd-user unavailable; skipping smoke"
  exit 0
fi

# The rewritten runtime is a single PER-USER supervisor keyed off the XDG
# state root — there is no per-repo service instance anymore (see
# internal/service's package doc comment). This smoke installs it as a
# systemd --user unit (with RALPH_STATE_DIR pointed at an isolated dir via
# --env so the managed supervisor never touches the operator's real
# state), starts it, confirms the plain client sees a live supervisor,
# stops it, and confirms the client reports it gone.
tmpdir="$(mktemp -d)"
project="$tmpdir/project"
home="$tmpdir/home"
state="$tmpdir/state"
mkdir -p "$project" "$home" "$state"

export HOME="$home"

cleanup() {
  if [[ -n "${unit_name:-}" ]]; then
    systemctl --user stop "$unit_name" >/dev/null 2>&1 || true
  fi
  "$bin" service uninstall >/dev/null 2>&1 || true
  systemctl --user daemon-reload >/dev/null 2>&1 || true
  rm -rf "$tmpdir"
}
trap cleanup EXIT

(cd "$project" && RALPH_STATE_DIR="$state" "$bin" --init >/dev/null)

install_out="$("$bin" service install --radioactive_ralph-bin "$bin" --env "RALPH_STATE_DIR=$state")"
install_path="$(echo "$install_out" | awk '{print $NF}')"
if [[ ! -f "$install_path" ]]; then
  echo "expected installed unit at $install_path" >&2
  echo "$install_out" >&2
  exit 1
fi

unit_name="$(basename "$install_path")"
systemctl --user daemon-reload
systemctl --user start "$unit_name"

ready=0
for _ in $(seq 1 30); do
  if RALPH_STATE_DIR="$state" bash -c "cd '$project' && '$bin'" 2>/dev/null | grep -q "supervisor is up"; then
    ready=1
    break
  fi
  sleep 1
done
if [[ "$ready" -ne 1 ]]; then
  systemctl --user status "$unit_name" >&2 || true
  echo "systemd-user-managed supervisor never became ready" >&2
  exit 1
fi

systemctl --user stop "$unit_name"

stopped=0
for _ in $(seq 1 30); do
  if ! RALPH_STATE_DIR="$state" bash -c "cd '$project' && '$bin'" 2>/dev/null | grep -q "supervisor is up"; then
    stopped=1
    break
  fi
  sleep 1
done
if [[ "$stopped" -ne 1 ]]; then
  systemctl --user status "$unit_name" >&2 || true
  echo "systemd-user-managed supervisor never stopped" >&2
  exit 1
fi

echo "systemd-user smoke: ok"
