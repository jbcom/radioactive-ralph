#!/usr/bin/env bash
# Canonical release-copy contract for the native Windows package surfaces.
#
# This file is sourced by both the generated-artifact smoke and the
# publication verifier so those gates cannot drift into different definitions
# of safe native Windows package metadata and post-install guidance.

if [[ "${RALPH_PACKAGE_GUIDANCE_CONTRACT_LOADED:-0}" == "1" ]]; then
  return 0
fi
readonly RALPH_PACKAGE_GUIDANCE_CONTRACT_LOADED=1

readonly RALPH_NATIVE_WINDOWS_PACKAGE_SHORT_DESCRIPTION_CONTRACT='Native Windows foreground supervisor/client control plane only'

read -r -d '' RALPH_NATIVE_WINDOWS_PACKAGE_LONG_DESCRIPTION_CONTRACT <<'EOF' || true
This native Windows package provides only Radioactive Ralph's foreground
supervisor/client control plane through radioactive_ralph --supervisor.
Native Windows SCM install/start and provider-backed workers are disabled.
Use the Linux build inside WSL2 with systemd --user for functional Claude,
Codex, and OpenCode worker execution.
EOF
readonly RALPH_NATIVE_WINDOWS_PACKAGE_LONG_DESCRIPTION_CONTRACT

readonly RALPH_SCOOP_POST_INSTALL_CONTRACT_JSON='[
  "Write-Host '\''Native Windows provides only the foreground supervisor/client control plane:'\''",
  "Write-Host '\''  radioactive_ralph --supervisor'\''",
  "Write-Host '\''Native Windows SCM install/start and provider-backed workers are disabled.'\''",
  "Write-Host '\''For provider-backed execution, install the Linux build inside WSL2 and use systemd --user.'\''",
  "Write-Host '\''See https://jonbogaty.com/radioactive-ralph/getting-started/ for the full platform-specific setup flow.'\''"
]'

read -r -d '' RALPH_GORELEASER_FOOTER_CONTRACT <<'EOF' || true
## Install

Homebrew cask (macOS):
```
brew tap jbcom/pkgs https://github.com/jbcom/pkgs
brew install --cask radioactive-ralph
```

Scoop (Windows):
```
scoop bucket add jbcom https://github.com/jbcom/pkgs
scoop install radioactive-ralph
```

curl | sh:
```
curl -sSL https://jonbogaty.com/radioactive-ralph/install.sh | sh
```

First run (macOS, Linux, or the Linux build inside WSL2):
```
radioactive_ralph service install
radioactive_ralph --init
radioactive_ralph
```

Native Windows provides only the foreground supervisor/client control
plane:
```
radioactive_ralph --supervisor
```
Native Windows SCM install/start and provider-backed workers are disabled.
Use the Linux build inside WSL2 for functional provider-backed execution.
EOF
readonly RALPH_GORELEASER_FOOTER_CONTRACT

ralph_validate_scoop_manifest_contract() {
  jq -e \
    --arg description "$RALPH_NATIVE_WINDOWS_PACKAGE_SHORT_DESCRIPTION_CONTRACT" \
    --argjson post_install "$RALPH_SCOOP_POST_INSTALL_CONTRACT_JSON" '
    .description == $description and
    .post_install == $post_install
  ' >/dev/null
}

ralph_validate_scoop_manifest_contract_file() {
  local manifest="${1:?Scoop manifest path required}"
  ralph_validate_scoop_manifest_contract < "$manifest"
}

ralph_extract_unique_yaml_scalar() {
  local config="${1:?YAML config path required}"
  local key="${2:?YAML scalar key required}"
  local marker="    ${key}: "
  local count
  count="$(grep -c "^${marker}" "$config" || true)"
  if [[ "$count" != "1" ]]; then
    echo "package guidance: expected exactly one $key scalar in $config" >&2
    return 1
  fi
  awk -v marker="$marker" '
    index($0, marker) == 1 {
      print substr($0, length(marker) + 1)
    }
  ' "$config"
}

ralph_extract_unique_yaml_literal() {
  local config="${1:?YAML config path required}"
  local key="${2:?YAML literal key required}"
  local marker="    ${key}: |"
  local count
  count="$(grep -Fxc "$marker" "$config" || true)"
  if [[ "$count" != "1" ]]; then
    echo "package guidance: expected exactly one $key literal in $config" >&2
    return 1
  fi
  awk -v marker="$marker" '
    $0 == marker {
      in_literal = 1
      next
    }
    in_literal {
      if ($0 == "") {
        print
        next
      }
      if ($0 ~ /^      /) {
        sub(/^      /, "")
        print
        next
      }
      exit
    }
  ' "$config" | sed 's/\r$//; s/[[:space:]]*$//'
}

ralph_validate_native_windows_package_config_contract() {
  local config="${1:?GoReleaser config path required}"
  local short_key="${2:?native Windows short-description key required}"
  local short_description long_description
  short_description="$(
    ralph_extract_unique_yaml_scalar "$config" "$short_key"
  )" || return 1
  long_description="$(
    ralph_extract_unique_yaml_literal "$config" description
  )" || return 1
  if [[ "$short_description" != "$RALPH_NATIVE_WINDOWS_PACKAGE_SHORT_DESCRIPTION_CONTRACT" ||
        "$long_description" != "$RALPH_NATIVE_WINDOWS_PACKAGE_LONG_DESCRIPTION_CONTRACT" ]]; then
    echo "package guidance: $config does not match the exact native Windows package-description contract" >&2
    return 1
  fi
}

ralph_validate_winget_config_contract() {
  local config="${1:?primary GoReleaser config path required}"
  ralph_validate_native_windows_package_config_contract \
    "$config" short_description
}

ralph_validate_chocolatey_config_contract() {
  local config="${1:?Chocolatey GoReleaser config path required}"
  ralph_validate_native_windows_package_config_contract \
    "$config" summary
}

ralph_extract_goreleaser_release_footer() {
  local config="${1:?goreleaser config path required}"
  awk '
    /^release:$/ {
      in_release = 1
      next
    }
    in_release && /^  footer: \|$/ {
      in_footer = 1
      next
    }
    in_footer {
      if ($0 == "") {
        print
        next
      }
      if ($0 ~ /^    /) {
        sub(/^    /, "")
        print
        next
      }
      exit
    }
  ' "$config" | sed 's/\r$//; s/[[:space:]]*$//'
}

ralph_validate_goreleaser_release_footer() {
  local config="${1:?goreleaser config path required}"
  local actual
  actual="$(ralph_extract_goreleaser_release_footer "$config")"
  if [[ "$actual" != "$RALPH_GORELEASER_FOOTER_CONTRACT" ]]; then
    echo "package guidance: GoReleaser footer does not match the platform-split release contract" >&2
    return 1
  fi
}

ralph_validate_release_body_footer() {
  local body
  body="$(sed 's/\r$//')" || return 1
  if [[ "$body" == "$RALPH_GORELEASER_FOOTER_CONTRACT" ||
        "$body" == *$'\n'"$RALPH_GORELEASER_FOOTER_CONTRACT" ]]; then
    return 0
  fi
  echo "package guidance: release body does not end with the exact platform-split footer contract" >&2
  return 1
}
