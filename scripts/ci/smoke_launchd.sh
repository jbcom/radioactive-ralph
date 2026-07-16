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

# The rewritten runtime is a single PER-USER supervisor keyed off the XDG
# state root — there is no per-repo service instance anymore (see
# internal/service's package doc comment). This smoke installs it as a
# launchd user agent, starts it via launchctl, confirms the plain client
# sees a live supervisor, stops it, and confirms the client reports it gone.
tmpdir="$(mktemp -d /tmp/ralph-launchd-XXXXXX)"
project="$tmpdir/project"
home="$tmpdir/home"
state="$tmpdir/state"
mkdir -p "$project" "$home" "$state"

export HOME="$home"
export RALPH_STATE_DIR="$state"

cleanup() {
  if [[ -n "${label:-}" ]]; then
    launchctl bootout "gui/$(id -u)/$label" >/dev/null 2>&1 || true
  fi
  "$bin" service uninstall >/dev/null 2>&1 || true
  rm -rf "$tmpdir"
}
trap cleanup EXIT

(cd "$project" && "$bin" --init >/dev/null)

install_out="$("$bin" service install --radioactive_ralph-bin "$bin" --env "RALPH_STATE_DIR=$state" --env "HOME=$home")"
install_path="$(echo "$install_out" | awk '{print $NF}')"
if [[ ! -f "$install_path" ]]; then
  echo "expected installed plist at $install_path" >&2
  echo "$install_out" >&2
  exit 1
fi
"$bin" service status | grep -qi "installed" || {
  echo "service status did not report installed" >&2
  exit 1
}

label="$(basename "$install_path" .plist)"
domain="gui/$(id -u)"

launchctl bootstrap "$domain" "$install_path"
launchctl kickstart -k "$domain/$label" >/dev/null 2>&1 || true

ready=0
for _ in $(seq 1 30); do
  if (cd "$project" && "$bin") 2>/dev/null | grep -q "supervisor is up"; then
    ready=1
    break
  fi
  sleep 1
done
if [[ "$ready" -ne 1 ]]; then
  launchctl print "$domain/$label" >&2 || true
  echo "launchd-managed supervisor never became ready" >&2
  exit 1
fi

launchctl bootout "$domain/$label"
label=""

stopped=0
for _ in $(seq 1 30); do
  if ! (cd "$project" && "$bin") 2>/dev/null | grep -q "supervisor is up"; then
    stopped=1
    break
  fi
  sleep 1
done
if [[ "$stopped" -ne 1 ]]; then
  echo "launchd-managed supervisor never stopped" >&2
  exit 1
fi

echo "launchd smoke: ok"
