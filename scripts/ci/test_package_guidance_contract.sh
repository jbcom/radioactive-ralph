#!/usr/bin/env bash

set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
# shellcheck source=scripts/ci/package_guidance_contract.sh
source "$ROOT/scripts/ci/package_guidance_contract.sh"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

ralph_validate_winget_config_contract "$ROOT/.goreleaser.yaml"
ralph_validate_chocolatey_config_contract "$ROOT/.goreleaser.chocolatey.yaml"

safe_manifest="$TMP/scoop-safe.json"
jq -n \
  --arg description "$RALPH_NATIVE_WINDOWS_PACKAGE_SHORT_DESCRIPTION_CONTRACT" \
  --argjson post_install "$RALPH_SCOOP_POST_INSTALL_CONTRACT_JSON" \
  '{description: $description, post_install: $post_install}' > "$safe_manifest"
ralph_validate_scoop_manifest_contract < "$safe_manifest"

full_runtime_description_fixture="$TMP/scoop-full-runtime-description.json"
jq \
  '.description = "Supervised-execution runtime for local AI-agent CLIs"' \
  "$safe_manifest" > "$full_runtime_description_fixture"
if ralph_validate_scoop_manifest_contract < "$full_runtime_description_fixture"; then
  echo "expected misleading full-runtime Scoop description to fail" >&2
  exit 1
fi

assert_unsafe_native_windows_config_fails() {
  local package_name="$1"
  local validator="$2"
  local source="$3"
  local short_key="$4"
  local unsafe_short="$TMP/${package_name}-unsafe-short.yaml"
  local unsafe_long="$TMP/${package_name}-unsafe-long.yaml"

  sed \
    "s|^    ${short_key}: .*|    ${short_key}: Supervised-execution runtime for local AI-agent CLIs|" \
    "$source" > "$unsafe_short"
  if "$validator" "$unsafe_short"; then
    echo "expected misleading full-runtime $package_name short description to fail" >&2
    exit 1
  fi

  awk '
    {
      print
      if ($0 == "      Codex, and OpenCode worker execution.") {
        print "      Native Windows SCM install/start and provider-backed workers are supported."
      }
    }
  ' "$source" > "$unsafe_long"
  if "$validator" "$unsafe_long"; then
    echo "expected contradictory $package_name long description to fail" >&2
    exit 1
  fi
}

assert_unsafe_native_windows_config_fails \
  winget \
  ralph_validate_winget_config_contract \
  "$ROOT/.goreleaser.yaml" \
  short_description
assert_unsafe_native_windows_config_fails \
  chocolatey \
  ralph_validate_chocolatey_config_contract \
  "$ROOT/.goreleaser.chocolatey.yaml" \
  summary

assert_unsafe_scoop_line_fails() {
  local name="$1"
  local line="$2"
  local fixture="$TMP/scoop-${name}.json"
  jq --arg line "$line" '.post_install += [$line]' \
    "$safe_manifest" > "$fixture"
  if ralph_validate_scoop_manifest_contract < "$fixture"; then
    echo "expected unsafe Scoop fixture $name to fail" >&2
    exit 1
  fi
}

assert_unsafe_scoop_line_fails \
  sc-start \
  "Write-Host 'Run sc.exe start radioactive_ralph-supervisor'"
assert_unsafe_scoop_line_fails \
  start-service \
  "Write-Host 'Run Start-Service radioactive_ralph-supervisor'"
assert_unsafe_scoop_line_fails \
  native-providers \
  "Write-Host 'Native Windows provider workers are supported.'"

ralph_validate_goreleaser_release_footer "$ROOT/.goreleaser.yaml"
safe_release_body="$(
  printf '# Changelog\n\n- Generated release notes.\n\n%s' \
    "$RALPH_GORELEASER_FOOTER_CONTRACT"
)"
ralph_validate_release_body_footer <<<"$safe_release_body"
disabled_runtime='Native Windows SCM install/start and provider-backed workers are disabled.'
enabled_runtime='Native Windows SCM install/start and provider-backed workers are supported.'
unsafe_release_body="${safe_release_body/"$disabled_runtime"/"$enabled_runtime"}"
if ralph_validate_release_body_footer <<<"$unsafe_release_body"; then
  echo "expected contradictory immutable release footer to fail" >&2
  exit 1
fi

assert_unsafe_footer_fails() {
  local name="$1"
  local search="$2"
  local replacement="$3"
  local fixture="$TMP/goreleaser-${name}.yaml"
  sed "s|$search|$replacement|" "$ROOT/.goreleaser.yaml" > "$fixture"
  if ralph_validate_goreleaser_release_footer "$fixture"; then
    echo "expected unsafe GoReleaser footer fixture $name to fail" >&2
    exit 1
  fi
}

assert_unsafe_footer_fails \
  sc-start \
  "radioactive_ralph --supervisor" \
  "sc.exe start radioactive_ralph-supervisor"
assert_unsafe_footer_fails \
  start-service \
  "radioactive_ralph --supervisor" \
  "Start-Service radioactive_ralph-supervisor"
assert_unsafe_footer_fails \
  native-providers \
  "Native Windows SCM install/start and provider-backed workers are disabled." \
  "Native Windows provider-backed workers are supported."

echo "package guidance contract tests: ok"
