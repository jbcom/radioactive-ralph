#!/usr/bin/env bash
# Restore the exact pre-release package bytes through protected main. The
# unique winning squash merge's first parent is authoritative; signed draft
# provenance is an integrity cross-check only.
set -euo pipefail

VERSION="${1:?usage: rollback_package_manifests.sh <version> <provenance-or-dash> <failed-merge-oid>}"
PROVENANCE="${2:?usage: rollback_package_manifests.sh <version> <provenance-or-dash> <failed-merge-oid>}"
FAILED_MERGE_OID="${3:?usage: rollback_package_manifests.sh <version> <provenance-or-dash> <failed-merge-oid>}"
PKGS_REPO="${PKGS_REPO:-jbcom/pkgs}"
PKGS_GH_TOKEN="${PKGS_GH_TOKEN:-}"
PKGS_CLONE_URL="${PKGS_CLONE_URL:-https://x-access-token@github.com/${PKGS_REPO}.git}"
GH_BIN="${GH_BIN:-gh}"
CHECK_ATTEMPTS="${CHECK_ATTEMPTS:-60}"
CHECK_SLEEP_SECONDS="${CHECK_SLEEP_SECONDS:-10}"
ROLLBACK_ATTEMPTS="${ROLLBACK_ATTEMPTS:-3}"
EXPECTED_ACTIONS_APP_ID=15368
VERSION_BRANCH="chore/update-radioactive-ralph-${VERSION}"
BRANCH="chore/rollback-radioactive-ralph-${VERSION}"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
FILES=(
  Casks/radioactive-ralph.rb
  Casks/radioactive-ralph-gui.rb
  bucket/radioactive-ralph.json
)

[[ "$VERSION" =~ ^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$ ]]
[[ "$FAILED_MERGE_OID" =~ ^[0-9a-f]{40,64}$ ]]
[[ -n "$PKGS_GH_TOKEN" ]] || {
  echo "package rollback: PKGS_GH_TOKEN is required" >&2
  exit 1
}

pkgs_gh() {
  GH_TOKEN="$PKGS_GH_TOKEN" "$GH_BIN" "$@"
}
fetch_file() {
  local path="$1" ref="$2"
  pkgs_gh api -H "Accept: application/vnd.github.raw+json" \
    "repos/${PKGS_REPO}/contents/${path}?ref=${ref}"
}
sha256_file() {
  sha256sum "$1" | awk '{print $1}'
}
target_files_still_failed() {
  local path
  for path in "${FILES[@]}"; do
    cmp -s "$WORK/failed/$path" <(fetch_file "$path" main) || return 1
  done
}
validate_action_check() {
  local check_runs="$1" check_name="$2" workflow_name="$3" workflow_path="$4" head_oid="$5"
  local candidate details_url run_id run
  candidate="$(jq \
    --arg name "$check_name" \
    --argjson app_id "$EXPECTED_ACTIONS_APP_ID" \
    '[.check_runs[] | select(
      .name == $name and .app.id == $app_id and
      .app.slug == "github-actions" and .status == "completed" and
      .conclusion == "success"
    )]' <<<"$check_runs")"
  [[ "$(jq 'length' <<<"$candidate")" == 1 ]]
  details_url="$(jq -r '.[0].details_url' <<<"$candidate")"
  [[ "$details_url" =~ ^https://github\.com/${PKGS_REPO}/actions/runs/([0-9]+)/job/[0-9]+$ ]]
  run_id="${BASH_REMATCH[1]}"
  run="$(pkgs_gh api "repos/${PKGS_REPO}/actions/runs/${run_id}")"
  jq -e \
    --arg name "$workflow_name" --arg path "$workflow_path" \
    --arg head "$head_oid" --arg repo "$PKGS_REPO" \
    '.name == $name and .path == $path and .event == "pull_request" and
     .head_sha == $head and .status == "completed" and
     .conclusion == "success" and .repository.full_name == $repo and
     .head_repository.full_name == $repo' <<<"$run" >/dev/null
}
verify_required_checks() {
  local head_oid="$1" checks
  checks="$(pkgs_gh api -H "Accept: application/vnd.github+json" \
    "repos/${PKGS_REPO}/commits/${head_oid}/check-runs?filter=latest&per_page=100")"
  validate_action_check "$checks" validate \
    "Validate packages" ".github/workflows/validate-packages.yml" "$head_oid"
  validate_action_check "$checks" build-site \
    CI ".github/workflows/ci.yml" "$head_oid"
}
exact_file_set() {
  local json="$1" actual expected
  actual="$(jq -r '.files[].path' <<<"$json" | LC_ALL=C sort)"
  expected="$(printf '%s\n' "${FILES[@]}" | LC_ALL=C sort)"
  [[ "$actual" == "$expected" ]]
}

WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT
mkdir -p "$WORK/prior/Casks" "$WORK/prior/bucket" \
  "$WORK/failed/Casks" "$WORK/failed/bucket"
for path in "${FILES[@]}"; do
  fetch_file "$path" "$FAILED_MERGE_OID" > "$WORK/failed/$path"
done

merged="$(pkgs_gh pr list --repo "$PKGS_REPO" --state merged \
  --limit 1000 \
  --json number,baseRefName,headRefName,headRepositoryOwner,isCrossRepository,files,mergeCommit)"
version_pr="$(jq -e \
  --arg branch "$VERSION_BRANCH" --arg owner "${PKGS_REPO%%/*}" \
  --arg failed "$FAILED_MERGE_OID" \
  '[.[] | select(
    .baseRefName == "main" and
    (.headRefName == $branch or (.headRefName | startswith($branch + "-attempt-"))) and
    .headRepositoryOwner.login == $owner and .isCrossRepository == false
    and .mergeCommit.oid == $failed
  )] | if length == 1 then .[0] else error("ambiguous merged version PR") end' \
  <<<"$merged")"
exact_file_set "$version_pr"
merge_oid="$(jq -er '.mergeCommit.oid' <<<"$version_pr")"
merge_commit="$(pkgs_gh api "repos/${PKGS_REPO}/git/commits/${merge_oid}")"
[[ "$(jq '.parents | length' <<<"$merge_commit")" == 1 ]]
PRIOR_MAIN_OID="$(jq -er '.parents[0].sha' <<<"$merge_commit")"
[[ "$PRIOR_MAIN_OID" =~ ^[0-9a-f]{40,64}$ ]]
for path in "${FILES[@]}"; do
  fetch_file "$path" "$PRIOR_MAIN_OID" > "$WORK/prior/$path"
done

if [[ "$PROVENANCE" != "-" && -f "$PROVENANCE" ]]; then
  listing="$(tar -tzf "$PROVENANCE" | LC_ALL=C sort)"
  expected="$(printf '%s\n' provenance.json "${FILES[@]}" | LC_ALL=C sort)"
  [[ "$listing" == "$expected" ]] || {
    echo "package rollback: provenance member set is not exact" >&2
    exit 1
  }
  mkdir -p "$WORK/sealed"
  tar -xzf "$PROVENANCE" -C "$WORK/sealed"
  sealed_prior_oid="$(jq -er \
    --arg version "$VERSION" \
    'select(.schema == 1 and .release_version == $version) | .prior_main_oid' \
    "$WORK/sealed/provenance.json")"
  [[ "$sealed_prior_oid" =~ ^[0-9a-f]{40,64}$ ]]
  for path in "${FILES[@]}"; do
    recorded="$(jq -er --arg path "$path" '.files[$path].sha256' \
      "$WORK/sealed/provenance.json")"
    [[ "$recorded" == "$(sha256_file "$WORK/sealed/$path")" ]]
  done
  if [[ "$sealed_prior_oid" == "$PRIOR_MAIN_OID" ]]; then
    for path in "${FILES[@]}"; do
      cmp -s "$WORK/sealed/$path" "$WORK/prior/$path"
    done
  else
    echo "package rollback: package main advanced after seal; using winning merge ancestry $PRIOR_MAIN_OID" >&2
  fi
fi

if ! target_files_still_failed; then
  echo "package rollback: refusing to overwrite superseded release manifests" >&2
  exit 1
fi

ASKPASS="$WORK/askpass.sh"
printf '#!/bin/sh\nprintf "%%s" "%s"\n' "$PKGS_GH_TOKEN" > "$ASKPASS"
chmod 0700 "$ASKPASS"
export GIT_ASKPASS="$ASKPASS" GIT_TERMINAL_PROMPT=0
git clone "$PKGS_CLONE_URL" "$WORK/pkgs"

for ((rollback_attempt = 1; rollback_attempt <= ROLLBACK_ATTEMPTS; rollback_attempt++)); do
  if ! target_files_still_failed; then
    echo "package rollback: source or package bytes were superseded" >&2
    exit 1
  fi
  git -C "$WORK/pkgs" fetch origin main
  git -C "$WORK/pkgs" checkout -B "$BRANCH" origin/main
  for path in "${FILES[@]}"; do
    cp "$WORK/prior/$path" "$WORK/pkgs/$path"
  done
  git -C "$WORK/pkgs" add "${FILES[@]}"
  if git -C "$WORK/pkgs" diff --cached --quiet --exit-code; then
    echo "package rollback: prior manifest bytes are already exact on main"
    exit 0
  fi
  git -C "$WORK/pkgs" \
    -c user.name=jbcom-bot \
    -c user.email=noreply@jonbogaty.com \
    commit -m "chore: roll back radioactive-ralph packages after ${VERSION}"
  (
    cd "$WORK/pkgs"
    bash "$SCRIPT_DIR/../../packaging/macos/push-version-branch.sh" "$BRANCH"
  )
  if ! pkgs_gh pr create --repo "$PKGS_REPO" --base main --head "$BRANCH" \
    --title "chore: roll back radioactive-ralph packages after ${VERSION}" \
    --body "Protected compensation for terminal release v${VERSION}; restore signed/derived prior package state ${PRIOR_MAIN_OID}." \
    2>"$WORK/pr-error.log"; then
    grep -qi "already exists" "$WORK/pr-error.log" || {
      cat "$WORK/pr-error.log" >&2
      exit 1
    }
  fi

  prs="$(pkgs_gh pr list --repo "$PKGS_REPO" --state open --head "$BRANCH" \
    --json number,baseRefName,headRefName,headRefOid,headRepositoryOwner,isCrossRepository,files)"
  pr="$(jq -e \
    --arg branch "$BRANCH" --arg owner "${PKGS_REPO%%/*}" \
    '[.[] | select(
      .baseRefName == "main" and .headRefName == $branch and
      .headRepositoryOwner.login == $owner and .isCrossRepository == false
    )] | if length == 1 then .[0] else error("ambiguous rollback PR") end' \
    <<<"$prs")"
  exact_file_set "$pr"
  number="$(jq -er '.number' <<<"$pr")"
  head_oid="$(jq -er '.headRefOid' <<<"$pr")"
  for path in "${FILES[@]}"; do
    cmp -s "$WORK/prior/$path" <(fetch_file "$path" "$head_oid")
  done

  checks_ok=false
  for ((attempt = 1; attempt <= CHECK_ATTEMPTS; attempt++)); do
    if pkgs_gh pr checks "$number" --repo "$PKGS_REPO" \
      --required --watch --fail-fast &&
      verify_required_checks "$head_oid"; then
      checks_ok=true
      break
    fi
    ((attempt < CHECK_ATTEMPTS)) && sleep "$CHECK_SLEEP_SECONDS"
  done
  [[ "$checks_ok" == true ]] || exit 1
  fresh_head="$(pkgs_gh pr view "$number" --repo "$PKGS_REPO" \
    --json headRefOid --jq .headRefOid)"
  [[ "$fresh_head" == "$head_oid" ]] || continue
  target_files_still_failed || exit 1
  if pkgs_gh pr merge --repo "$PKGS_REPO" --squash \
    --match-head-commit "$head_oid" "$number"; then
    for path in "${FILES[@]}"; do
      cmp -s "$WORK/prior/$path" <(fetch_file "$path" main)
    done
    echo "package rollback: prior exact manifest bytes restored through protected main"
    exit 0
  fi
  echo "package rollback: strict main advanced; retrying checked compensation ($rollback_attempt/$ROLLBACK_ATTEMPTS)" >&2
done

echo "package rollback: protected compensation did not merge after safe retries" >&2
exit 1
