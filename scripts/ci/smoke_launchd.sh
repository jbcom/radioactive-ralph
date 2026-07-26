#!/usr/bin/env bash
set -euo pipefail

if [[ $# -ne 2 ]]; then
  echo "usage: $0 /absolute/path/to/radioactive_ralph expected-go-architecture" >&2
  exit 2
fi

bin="$1"
expected_arch="$2"
if [[ ! -x "$bin" ]]; then
  echo "binary is not executable: $bin" >&2
  exit 2
fi
if [[ "${GITHUB_ACTIONS:-}" != "true" ]]; then
  echo "launchd Homebrew PATH smoke is restricted to an ephemeral GitHub Actions runner" >&2
  exit 2
fi

case "$(uname -m)" in
  arm64)
    actual_arch="arm64"
    homebrew_bin="/opt/homebrew/bin"
    ;;
  x86_64)
    actual_arch="amd64"
    homebrew_bin="/usr/local/bin"
    ;;
  *)
    echo "unsupported macOS architecture for Homebrew PATH smoke: $(uname -m)" >&2
    exit 2
    ;;
esac
if [[ "$actual_arch" != "$expected_arch" ]]; then
  echo "runner architecture is $actual_arch, expected $expected_arch" >&2
  exit 1
fi
if [[ ! -d "$homebrew_bin" || ! -w "$homebrew_bin" ]]; then
  echo "expected writable hosted-runner Homebrew bin: $homebrew_bin" >&2
  exit 1
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

created_codex=0
created_opencode=0
cleanup() {
  "$bin" service uninstall >/dev/null 2>&1 || true
  if [[ "$created_codex" -eq 1 && -L "$homebrew_bin/codex" &&
    "$(readlink "$homebrew_bin/codex")" == "/usr/bin/true" ]]; then
    rm -f "$homebrew_bin/codex"
  fi
  if [[ "$created_opencode" -eq 1 && -L "$homebrew_bin/opencode" &&
    "$(readlink "$homebrew_bin/opencode")" == "/usr/bin/true" ]]; then
    rm -f "$homebrew_bin/opencode"
  fi
  rm -rf "$tmpdir"
}
trap cleanup EXIT

if [[ ! -e "$homebrew_bin/codex" && ! -L "$homebrew_bin/codex" ]]; then
  ln -s /usr/bin/true "$homebrew_bin/codex"
  created_codex=1
fi
if [[ ! -e "$homebrew_bin/opencode" && ! -L "$homebrew_bin/opencode" ]]; then
  ln -s /usr/bin/true "$homebrew_bin/opencode"
  created_opencode=1
fi

export PATH="$homebrew_bin:/usr/bin:/bin:/usr/sbin:/sbin"

(cd "$project" && "$bin" --init >/dev/null)

install_out="$("$bin" service install --bin "$bin" --env "RALPH_STATE_DIR=$state" --env "HOME=$home")"
install_path="$(echo "$install_out" | awk '{print $NF}')"
if [[ ! -f "$install_path" ]]; then
  echo "expected installed plist at $install_path" >&2
  echo "$install_out" >&2
  exit 1
fi
persisted_path="$(/usr/libexec/PlistBuddy -c 'Print :EnvironmentVariables:PATH' "$install_path")"
case ":$persisted_path:" in
  *":$homebrew_bin:"*) ;;
  *)
    echo "persisted launchd PATH omitted trusted Homebrew bin $homebrew_bin: $persisted_path" >&2
    exit 1
    ;;
esac
for provider in codex opencode; do
  resolved="$(env -i PATH="$persisted_path" /usr/bin/which "$provider")"
  if [[ "$resolved" != "$homebrew_bin/$provider" || ! -x "$resolved" ]]; then
    echo "persisted launchd PATH resolved $provider to $resolved, want $homebrew_bin/$provider" >&2
    exit 1
  fi
done
"$bin" service status | grep -qi "installed" || {
  echo "service status did not report installed" >&2
  exit 1
}

label="$(basename "$install_path" .plist)"
domain="gui/$(id -u)"

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

"$bin" service uninstall >/dev/null

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
