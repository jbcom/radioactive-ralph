#!/usr/bin/env bash
# Resolve the Sigstore workflow identity for a stable release. v0.22.0 is the
# one bounded exception: its tag was created before the recovery dispatch
# existed, so its signatures originate from the reviewed main-branch workflow.

ralph_release_workflow_identity() {
  local repository="${1:?repository is required}"
  local version="${2:?version is required}"
  local identity

  if [[ "$version" == "0.22.0" ]]; then
    identity="https://github.com/${repository}/.github/workflows/release.yml@refs/heads/main"
  else
    identity="https://github.com/${repository}/.github/workflows/release.yml@refs/tags/v${version}"
  fi

  [[ "$identity" == \
    "https://github.com/${repository}/.github/workflows/release.yml@refs/"* ]]
  printf '%s\n' "$identity"
}
