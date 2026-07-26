#!/usr/bin/env bash
# Wait until every required jbcom/pkgs manifest is merged to main at the exact
# release version. Opening a package PR is not publication; this gate observes
# the consumer-facing branch that Homebrew and Scoop actually read.

set -euo pipefail

VERSION="${1:?usage: wait_for_package_publication.sh <version>}"
PKGS_REPO="${PKGS_REPO:-jbcom/pkgs}"
RELEASE_REPO="${RELEASE_REPO:-jbcom/radioactive-ralph}"
EXPECTED_OWNER="${PKGS_REPO%%/*}"
EXPECTED_ACTIONS_APP_ID=15368
GH_BIN="${GH_BIN:-gh}"
COSIGN_BIN="${COSIGN_BIN:-cosign}"
PKGS_GH_TOKEN="${PKGS_GH_TOKEN:-}"
RELEASE_GH_TOKEN="${RELEASE_GH_TOKEN:-}"
MAX_ATTEMPTS="${MAX_ATTEMPTS:-60}"
SLEEP_SECONDS="${SLEEP_SECONDS:-10}"
PR_ATTEMPTS="${PR_ATTEMPTS:-12}"
PR_SLEEP_SECONDS="${PR_SLEEP_SECONDS:-5}"
CHECK_ATTEMPTS="${CHECK_ATTEMPTS:-60}"
CHECK_SLEEP_SECONDS="${CHECK_SLEEP_SECONDS:-10}"
PACKAGE_GATE_MODE="${PACKAGE_GATE_MODE:-merge}"
BASE_PACKAGE_BRANCH="chore/update-radioactive-ralph-${VERSION}"
PACKAGE_BRANCH_OVERRIDE="${PACKAGE_BRANCH:-}"
PACKAGE_BRANCH="${PACKAGE_BRANCH:-$BASE_PACKAGE_BRANCH}"
CACHE_DIR="$(mktemp -d)"
PACKAGE_PAYLOAD_DIR="${CACHE_DIR}/package-manifests"
trap 'rm -rf "$CACHE_DIR"' EXIT

if [[ "$EXPECTED_OWNER" == "$PKGS_REPO" ]]; then
  echo "package publication: PKGS_REPO must be owner/name: $PKGS_REPO" >&2
  exit 1
fi
if [[ -z "$PKGS_GH_TOKEN" || -z "$RELEASE_GH_TOKEN" ]]; then
  echo "package publication: PKGS_GH_TOKEN and RELEASE_GH_TOKEN are required" >&2
  exit 1
fi

if [[ ! "$VERSION" =~ ^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$ ]]; then
  echo "package publication: invalid stable version: $VERSION" >&2
  exit 1
fi

pkgs_gh() {
  GH_TOKEN="$PKGS_GH_TOKEN" "$GH_BIN" "$@"
}

resolve_package_attempt_branch() {
  [[ "${RESOLVE_PACKAGE_ATTEMPT_BRANCH:-0}" == "1" &&
     -z "${PACKAGE_BRANCH_OVERRIDE:-}" ]] || return 0
  local prs branches count
  prs="$(pkgs_gh pr list --repo "$PKGS_REPO" --state open \
    --limit 1000 \
    --json baseRefName,headRefName,headRepositoryOwner,isCrossRepository)"
  branches="$(jq -r --arg base "$BASE_PACKAGE_BRANCH" \
    --arg owner "$EXPECTED_OWNER" \
    '.[] | select(
      .baseRefName == "main" and
      (.headRefName == $base or (
        (.headRefName | startswith($base + "-attempt-")) and
        (.headRefName | ltrimstr($base + "-attempt-") |
          test("^[1-9][0-9]*$"))
      )) and
      .headRepositoryOwner.login == $owner and .isCrossRepository == false
    ) | .headRefName' <<<"$prs")"
  count="$(grep -c . <<<"$branches" || true)"
  if [[ "$count" == 0 ]] &&
    required_is_current package &&
    resolve_winning_release_merge; then
    PACKAGE_BRANCH="$BASE_PACKAGE_BRANCH"
    echo "package publication: recovered already-merged sealed package payload"
    return 0
  fi
  [[ "$count" == 1 ]] || {
    echo "package publication: expected one open release attempt, found $count" >&2
    return 1
  }
  PACKAGE_BRANCH="$branches"
}

release_gh() {
  GH_TOKEN="$RELEASE_GH_TOKEN" "$GH_BIN" "$@"
}

require_current_release_manifest() {
  local manifest
  manifest="$(release_gh api \
    -H "Accept: application/vnd.github.raw+json" \
    "repos/${RELEASE_REPO}/contents/.release-please-manifest.json?ref=main")" ||
    return 1
  [[ "$(jq -er '."."' <<<"$manifest")" == "$VERSION" ]] || {
    echo "package publication: source main no longer intends release $VERSION" >&2
    return 1
  }
}

fetch_ref_file() {
  local path="$1"
  local ref="$2"
  pkgs_gh api \
    -H "Accept: application/vnd.github.raw+json" \
    "repos/${PKGS_REPO}/contents/${path}?ref=${ref}"
}

fetch_main_file() {
  local path="$1"
  fetch_ref_file "$path" main
}

resolve_main_oid() {
  local oid
  oid="$(pkgs_gh api \
    --jq '.object.sha' \
    "repos/${PKGS_REPO}/git/ref/heads/main")" || return 1
  [[ "$oid" =~ ^[0-9a-f]{40,64}$ ]] || return 1
  printf '%s\n' "$oid"
}

cache_release_asset() {
  local asset="$1"
  [[ "$asset" != */* && "$asset" != "." && "$asset" != ".." ]] || return 1
  local destination="${CACHE_DIR}/${asset}"
  if [[ ! -f "$destination" ]]; then
    local partial="${destination}.partial"
    rm -f "$partial"
    release_gh release download "v${VERSION}" \
      --repo "$RELEASE_REPO" \
      --pattern "$asset" \
      --output "$partial" || {
        rm -f "$partial"
        return 1
      }
    [[ -f "$partial" ]] || return 1
    mv "$partial" "$destination"
  fi
  printf '%s\n' "$destination"
}

cask_version() {
  local path="$1"
  local content
  content="$(fetch_main_file "$path")" || return 1
  printf '%s\n' "$content" |
    sed -nE 's/^[[:space:]]*version "([^"]+)".*/\1/p' |
    head -n 1
}

scoop_version() {
  local content
  content="$(fetch_main_file "bucket/radioactive-ralph.json")" || return 1
  printf '%s\n' "$content" | jq -er '.version'
}

cli_is_current_at_ref() {
  local ref="$1"
  local cask scoop
  cask="$(fetch_ref_file "Casks/radioactive-ralph.rb" "$ref")" || return 1
  scoop="$(fetch_ref_file "bucket/radioactive-ralph.json" "$ref")" || return 1
  [[ "$(sed -nE 's/^[[:space:]]*version "([^"]+)".*/\1/p' <<< "$cask" |
      head -n 1)" == "$VERSION" ]] || return 1
  [[ "$(jq -er '.version' <<< "$scoop")" == "$VERSION" ]] || return 1
}

gui_is_current_at_ref() {
  local ref="$1"
  local cask
  cask="$(fetch_ref_file "Casks/radioactive-ralph-gui.rb" "$ref")" || return 1
  [[ "$(sed -nE 's/^[[:space:]]*version "([^"]+)".*/\1/p' <<< "$cask" |
      head -n 1)" == "$VERSION" ]] || return 1
}

required_is_current_at_ref() {
  local role="$1"
  local ref="$2"
  case "$role" in
    package)
      cli_is_current_at_ref "$ref" || return 1
      validate_release_manifests cli "$ref" || return 1
      gui_is_current_at_ref "$ref" || return 1
      validate_release_manifests gui "$ref"
      ;;
    *)
      echo "package publication: unknown package role: $1" >&2
      return 1
      ;;
  esac
}

required_is_current() {
  local role="$1"
  local before after
  before="$(resolve_main_oid)" || return 1
  required_is_current_at_ref "$role" "$before" || return 1
  after="$(resolve_main_oid)" || return 1
  [[ "$before" == "$after" ]] || return 1
}

VERIFIED_MAIN_OID=""
all_required_are_current() {
  local before after
  before="$(resolve_main_oid)" || return 1
  required_is_current_at_ref package "$before" || return 1
  after="$(resolve_main_oid)" || return 1
  [[ "$before" == "$after" ]] || return 1
  VERIFIED_MAIN_OID="$before"
}

report_pr() {
  local branch="$1"
  echo "package publication: PR state for $branch" >&2
  pkgs_gh pr list \
    --repo "$PKGS_REPO" \
    --state all \
    --head "$branch" \
    --json number,state,mergedAt,url \
    --jq '.[] | "  #\(.number) state=\(.state) mergedAt=\(.mergedAt // "none") \(.url)"' \
    >&2 || true
}

report_checks() {
  local pr_json="$1"
  printf '%s\n' "$pr_json" |
    jq -r '.statusCheckRollup[] |
      if .__typename == "CheckRun" then
        "  \(.name) status=\(.status) conclusion=\(.conclusion // "none")"
      else
        "  \(.context) state=\(.state // "none")"
      end' >&2
}

validate_required_action_check() {
  local check_runs="$1"
  local check_name="$2"
  local workflow_name="$3"
  local workflow_path="$4"
  local head_oid="$5"
  local candidate count details_url run_id run

  candidate="$(printf '%s\n' "$check_runs" | jq \
    --arg name "$check_name" \
    --argjson app_id "$EXPECTED_ACTIONS_APP_ID" \
    '[.check_runs[] | select(
      .name == $name and
      .app.id == $app_id and
      .app.slug == "github-actions" and
      .status == "completed" and
      .conclusion == "success"
    )]')" || return 1
  count="$(printf '%s\n' "$candidate" | jq 'length')" || return 1
  [[ "$count" == "1" ]] || return 1
  details_url="$(printf '%s\n' "$candidate" | jq -r '.[0].details_url')" ||
    return 1
  if [[ ! "$details_url" =~ ^https://github\.com/${PKGS_REPO}/actions/runs/([0-9]+)/job/[0-9]+$ ]]; then
    return 1
  fi
  run_id="${BASH_REMATCH[1]}"
  run="$(pkgs_gh api \
    "repos/${PKGS_REPO}/actions/runs/${run_id}")" || return 1
  printf '%s\n' "$run" | jq -e \
    --arg workflow_name "$workflow_name" \
    --arg workflow_path "$workflow_path" \
    --arg head_oid "$head_oid" \
    --arg repo "$PKGS_REPO" \
    '.name == $workflow_name and
     .path == $workflow_path and
     .event == "pull_request" and
     .head_sha == $head_oid and
     .status == "completed" and
     .conclusion == "success" and
     .repository.full_name == $repo and
     .head_repository.full_name == $repo' >/dev/null
}

validate_changed_files() {
  local pr_json="$1"
  local role="$2"
  local actual
  actual="$(printf '%s\n' "$pr_json" | jq -r '.files[].path' | LC_ALL=C sort)"
  local expected
  case "$role" in
    cli)
      expected="$(printf '%s\n' \
        "Casks/radioactive-ralph.rb" \
        "bucket/radioactive-ralph.json" |
        LC_ALL=C sort)"
      ;;
    package)
      expected="$(printf '%s\n' \
        "Casks/radioactive-ralph.rb" \
        "Casks/radioactive-ralph-gui.rb" \
        "bucket/radioactive-ralph.json" |
        LC_ALL=C sort)"
      ;;
    *)
      return 1
      ;;
  esac
  if [[ "$actual" != "$expected" ]]; then
    echo "package publication: $role PR changed-file set is not exact" >&2
    printf 'expected:\n%s\nactual:\n%s\n' "$expected" "$actual" >&2
    return 1
  fi
}

release_checksum() {
  local checksums="$1"
  local asset="$2"
  local matches
  matches="$(awk -v asset="$asset" \
    '$2 == asset || $2 == "*" asset {print $1}' <<< "$checksums")" ||
    return 1
  [[ "$matches" =~ ^[0-9a-f]{64}$ ]] || return 1
  printf '%s\n' "$matches"
}

sha256_file() {
  local path="$1"
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$path" | awk '{print $1}'
  else
    shasum -a 256 "$path" | awk '{print $1}'
  fi
}

verify_asset_bytes() {
  local asset="$1"
  local expected_sha="$2"
  local path actual_sha
  path="$(cache_release_asset "$asset")" || return 1
  actual_sha="$(sha256_file "$path")" || return 1
  [[ "$actual_sha" == "$expected_sha" ]] || return 1
}

PACKAGE_PAYLOAD_VERIFIED=0
ensure_package_payload_integrity() {
  if [[ "$PACKAGE_PAYLOAD_VERIFIED" == "1" ]]; then
    return 0
  fi

  local package_path package_bundle_path package_listing package_expected
  package_path="$(cache_release_asset package-manifests.tar.gz)" || return 1
  package_bundle_path="$(
    cache_release_asset package-manifests.tar.gz.sigstore.json
  )" || return 1
  "$COSIGN_BIN" verify-blob "$package_path" \
    --bundle "$package_bundle_path" \
    --certificate-identity \
    "https://github.com/${RELEASE_REPO}/.github/workflows/release.yml@refs/tags/v${VERSION}" \
    --certificate-oidc-issuer "https://token.actions.githubusercontent.com" \
    >/dev/null || return 1
  package_listing="$(tar -tzf "$package_path" | LC_ALL=C sort)" || return 1
  package_expected="$(printf '%s\n' \
    Casks/radioactive-ralph.rb \
    Casks/radioactive-ralph-gui.rb \
    bucket/radioactive-ralph.json | LC_ALL=C sort)"
  [[ "$package_listing" == "$package_expected" ]] || return 1
  mkdir -p "$PACKAGE_PAYLOAD_DIR"
  tar -xzf "$package_path" -C "$PACKAGE_PAYLOAD_DIR" || return 1
  PACKAGE_PAYLOAD_VERIFIED=1
}

COSIGN_VERIFIED=0
ensure_release_integrity() {
  if [[ "$COSIGN_VERIFIED" == "1" ]]; then
    return 0
  fi

  local checksums_path bundle_path gui_checksums_path gui_bundle_path
  checksums_path="$(cache_release_asset checksums.txt)" || return 1
  bundle_path="$(cache_release_asset checksums.txt.sigstore.json)" || return 1
  gui_checksums_path="$(cache_release_asset gui-checksums.txt)" || return 1
  gui_bundle_path="$(cache_release_asset gui-checksums.txt.sigstore.json)" ||
    return 1
  "$COSIGN_BIN" verify-blob "$checksums_path" \
    --bundle "$bundle_path" \
    --certificate-identity \
    "https://github.com/${RELEASE_REPO}/.github/workflows/release.yml@refs/tags/v${VERSION}" \
    --certificate-oidc-issuer "https://token.actions.githubusercontent.com" \
    >/dev/null || return 1
  "$COSIGN_BIN" verify-blob "$gui_checksums_path" \
    --bundle "$gui_bundle_path" \
    --certificate-identity \
    "https://github.com/${RELEASE_REPO}/.github/workflows/release.yml@refs/tags/v${VERSION}" \
    --certificate-oidc-issuer "https://token.actions.githubusercontent.com" \
    >/dev/null || return 1
  ensure_package_payload_integrity || return 1

  local checksums gui_checksums release_asset expected_sha
  checksums="$(<"$checksums_path")"
  gui_checksums="$(<"$gui_checksums_path")"
  for release_asset in \
    "radioactive_ralph_${VERSION}_darwin_amd64.tar.gz" \
    "radioactive_ralph_${VERSION}_darwin_arm64.tar.gz" \
    "radioactive_ralph_${VERSION}_linux_amd64.tar.gz" \
    "radioactive_ralph_${VERSION}_linux_arm64.tar.gz" \
    "radioactive_ralph_${VERSION}_windows_amd64.zip" \
    "radioactive-ralph_${VERSION}_linux_amd64.deb" \
    "radioactive-ralph_${VERSION}_linux_arm64.deb" \
    "radioactive-ralph_${VERSION}_linux_amd64.rpm" \
    "radioactive-ralph_${VERSION}_linux_arm64.rpm"; do
    expected_sha="$(release_checksum "$checksums" "$release_asset")" ||
      return 1
    verify_asset_bytes "$release_asset" "$expected_sha" || return 1
  done

  for release_asset in \
    "radioactive-ralph_${VERSION}_darwin_amd64.dmg" \
    "radioactive-ralph_${VERSION}_darwin_arm64.dmg" \
    "radioactive-ralph_${VERSION}_linux_x86_64.AppImage" \
    "radioactive-ralph.exe"; do
    expected_sha="$(release_checksum "$gui_checksums" "$release_asset")" ||
      return 1
    verify_asset_bytes "$release_asset" "$expected_sha" || return 1
  done

  COSIGN_VERIFIED=1
}

package_payload_matches_ref() {
  local ref="$1"
  ensure_package_payload_integrity || return 1
  cmp -s "$PACKAGE_PAYLOAD_DIR/Casks/radioactive-ralph.rb" \
    <(fetch_ref_file "Casks/radioactive-ralph.rb" "$ref") &&
    cmp -s "$PACKAGE_PAYLOAD_DIR/Casks/radioactive-ralph-gui.rb" \
      <(fetch_ref_file "Casks/radioactive-ralph-gui.rb" "$ref") &&
    cmp -s "$PACKAGE_PAYLOAD_DIR/bucket/radioactive-ralph.json" \
      <(fetch_ref_file "bucket/radioactive-ralph.json" "$ref")
}

validate_release_manifests() {
  local role="$1"
  local ref="$2"
  ensure_release_integrity || return 1
  case "$role" in
    cli)
      cmp -s "$PACKAGE_PAYLOAD_DIR/Casks/radioactive-ralph.rb" \
        <(fetch_ref_file "Casks/radioactive-ralph.rb" "$ref") &&
        cmp -s "$PACKAGE_PAYLOAD_DIR/bucket/radioactive-ralph.json" \
          <(fetch_ref_file "bucket/radioactive-ralph.json" "$ref")
      ;;
    gui)
      cmp -s "$PACKAGE_PAYLOAD_DIR/Casks/radioactive-ralph-gui.rb" \
        <(fetch_ref_file "Casks/radioactive-ralph-gui.rb" "$ref")
      ;;
    *)
      return 1
      ;;
  esac
}

latest_target_paths_equal() {
  local expected_oid="$1"
  local path oid
  for path in \
    Casks/radioactive-ralph.rb \
    Casks/radioactive-ralph-gui.rb \
    bucket/radioactive-ralph.json; do
    oid="$(pkgs_gh api --method GET \
      --raw-field sha=main \
      --raw-field path="$path" \
      --raw-field per_page=1 \
      --jq '.[0].sha' \
      "repos/${PKGS_REPO}/commits")" || return 1
    [[ "$oid" == "$expected_oid" ]] || {
      echo "package publication: current target-path owner drifted for $path" >&2
      return 1
    }
  done
}

recheck_current_package_ownership() {
  local expected_oid="${EXPECTED_PACKAGE_RELEASE_MERGE_OID:-}"
  [[ "$expected_oid" =~ ^[0-9a-f]{40,64}$ ]] || {
    echo "package publication: EXPECTED_PACKAGE_RELEASE_MERGE_OID is required" >&2
    return 1
  }
  ensure_package_payload_integrity || return 1
  latest_target_paths_equal "$expected_oid" || return 1
  package_payload_matches_ref main || return 1
  # Close the path-history/content race without repeating the full 23-asset
  # verifier. Any target-path write during the byte reads fails this pass.
  latest_target_paths_equal "$expected_oid" || return 1
}

WINNING_RELEASE_MERGE_OID=""
resolve_winning_release_merge() {
  local path oid common_oid="" prs candidates count pr head_oid check_runs
  for path in \
    Casks/radioactive-ralph.rb \
    Casks/radioactive-ralph-gui.rb \
    bucket/radioactive-ralph.json; do
    oid="$(pkgs_gh api --method GET \
      --raw-field sha=main \
      --raw-field path="$path" \
      --raw-field per_page=1 \
      --jq '.[0].sha' \
      "repos/${PKGS_REPO}/commits")" || return 1
    [[ "$oid" =~ ^[0-9a-f]{40,64}$ ]] || return 1
    if [[ -z "$common_oid" ]]; then
      common_oid="$oid"
    elif [[ "$oid" != "$common_oid" ]]; then
      echo "package publication: target paths have different latest commits" >&2
      return 1
    fi
  done

  prs="$(pkgs_gh pr list --repo "$PKGS_REPO" --state merged --limit 1000 \
    --json number,baseRefName,headRefName,headRefOid,headRepositoryOwner,isCrossRepository,files,mergeCommit)"
  candidates="$(jq \
    --arg base "$BASE_PACKAGE_BRANCH" \
    --arg owner "$EXPECTED_OWNER" \
    --arg merge "$common_oid" \
    '[.[] | select(
      .baseRefName == "main" and
      (.headRefName == $base or (
        (.headRefName | startswith($base + "-attempt-")) and
        (.headRefName | ltrimstr($base + "-attempt-") |
          test("^[1-9][0-9]*$"))
      )) and
      .headRepositoryOwner.login == $owner and
      .isCrossRepository == false and
      .mergeCommit.oid == $merge and
      ([.files[].path] | sort) == [
        "Casks/radioactive-ralph-gui.rb",
        "Casks/radioactive-ralph.rb",
        "bucket/radioactive-ralph.json"
      ]
    )]' <<<"$prs")" || return 1
  count="$(jq 'length' <<<"$candidates")"
  [[ "$count" == 1 ]] || {
    echo "package publication: winning package merge ownership is ambiguous ($count)" >&2
    return 1
  }
  pr="$(jq '.[0]' <<<"$candidates")"
  validate_changed_files "$pr" package || return 1
  required_is_current_at_ref package "$common_oid" || return 1
  head_oid="$(jq -er '.headRefOid' <<<"$pr")" || return 1
  check_runs="$(pkgs_gh api \
    -H "Accept: application/vnd.github+json" \
    "repos/${PKGS_REPO}/commits/${head_oid}/check-runs?filter=latest&per_page=100")" ||
    return 1
  validate_required_action_check "$check_runs" validate \
    "Validate packages" ".github/workflows/validate-packages.yml" "$head_oid" ||
    return 1
  validate_required_action_check "$check_runs" build-site \
    CI ".github/workflows/ci.yml" "$head_oid" || return 1
  WINNING_RELEASE_MERGE_OID="$common_oid"
}

resolve_historical_release_merge() {
  local release published_at published_epoch
  local prs candidates count index pr oid head_oid check_runs
  local merged_at merged_epoch latest_epoch=-1 latest_oid=""
  ensure_release_integrity || return 1
  release="$(release_gh api \
    "repos/${RELEASE_REPO}/releases/tags/v${VERSION}")" || return 1
  published_at="$(jq -er --arg tag "v${VERSION}" \
    'select(
      .draft == false and .prerelease == false and .immutable == true and
      .tag_name == $tag
    ) | .published_at | strings' <<<"$release")" || {
    echo "package publication: immutable release publication time is missing" >&2
    return 1
  }
  published_epoch="$(jq -en --arg timestamp "$published_at" \
    '$timestamp | fromdateiso8601')" || {
    echo "package publication: immutable release publication time is invalid" >&2
    return 1
  }
  prs="$(pkgs_gh pr list --repo "$PKGS_REPO" --state merged --limit 1000 \
    --json number,baseRefName,headRefName,headRefOid,headRepositoryOwner,isCrossRepository,files,mergeCommit,mergedAt)"
  candidates="$(jq \
    --arg base "$BASE_PACKAGE_BRANCH" \
    --arg owner "$EXPECTED_OWNER" \
    '[.[] | select(
      .baseRefName == "main" and
      (.headRefName == $base or (
        (.headRefName | startswith($base + "-attempt-")) and
        (.headRefName | ltrimstr($base + "-attempt-") |
          test("^[1-9][0-9]*$"))
      )) and
      .headRepositoryOwner.login == $owner and
      .isCrossRepository == false and
      .mergeCommit.oid != null and
      ([.files[].path] | sort) == [
        "Casks/radioactive-ralph-gui.rb",
        "Casks/radioactive-ralph.rb",
        "bucket/radioactive-ralph.json"
      ]
    )]' <<<"$prs")" || return 1
  count="$(jq 'length' <<<"$candidates")" || return 1
  for ((index = 0; index < count; index++)); do
    pr="$(jq -c ".[$index]" <<<"$candidates")" || return 1
    oid="$(jq -er '.mergeCommit.oid' <<<"$pr")" || return 1
    [[ "$oid" =~ ^[0-9a-f]{40,64}$ ]] || continue
    required_is_current_at_ref package "$oid" || continue
    head_oid="$(jq -er '.headRefOid' <<<"$pr")" || continue
    check_runs="$(pkgs_gh api \
      -H "Accept: application/vnd.github+json" \
      "repos/${PKGS_REPO}/commits/${head_oid}/check-runs?filter=latest&per_page=100")" ||
      continue
    validate_required_action_check "$check_runs" validate \
      "Validate packages" ".github/workflows/validate-packages.yml" "$head_oid" ||
      continue
    validate_required_action_check "$check_runs" build-site \
      CI ".github/workflows/ci.yml" "$head_oid" || continue
    merged_at="$(jq -er '.mergedAt | strings' <<<"$pr")" || {
      echo "package publication: trusted historical merge time is missing" >&2
      return 1
    }
    merged_epoch="$(jq -en --arg timestamp "$merged_at" \
      '$timestamp | fromdateiso8601')" || {
      echo "package publication: trusted historical merge time is invalid" >&2
      return 1
    }
    if ((merged_epoch >= published_epoch)); then
      echo "package publication: trusted exact attempt did not merge strictly before immutable publication" >&2
      return 1
    fi
    if ((merged_epoch > latest_epoch)); then
      latest_epoch="$merged_epoch"
      latest_oid="$oid"
    elif ((merged_epoch == latest_epoch)); then
      echo "package publication: historical merge ordering is ambiguous" >&2
      return 1
    fi
  done
  [[ -n "$latest_oid" ]] || {
    echo "package publication: no trusted historical release merge matched signed bytes" >&2
    return 1
  }
  WINNING_RELEASE_MERGE_OID="$latest_oid"
}

VERIFIED_HEAD_OID=""
wait_for_required_checks() {
  local number="$1"
  local branch="$2"
  local expected_oid="$3"
  local role="$4"

  for ((attempt = 1; attempt <= CHECK_ATTEMPTS; attempt++)); do
    local pr
    pr="$(pkgs_gh pr view "$number" \
      --repo "$PKGS_REPO" \
      --json number,state,baseRefName,headRefName,headRefOid,headRepositoryOwner,isCrossRepository,files,statusCheckRollup)"

    if ! printf '%s\n' "$pr" | jq -e \
      --arg branch "$branch" \
      --arg owner "$EXPECTED_OWNER" \
      '.state == "OPEN" and
       .headRefName == $branch and
       .baseRefName == "main" and
       .headRepositoryOwner.login == $owner and
       .isCrossRepository == false' >/dev/null; then
      echo "package publication: PR $number no longer matches the trusted release branch" >&2
      return 1
    fi

    local current_oid
    current_oid="$(printf '%s\n' "$pr" | jq -r '.headRefOid')"
    if [[ -z "$current_oid" || "$current_oid" == "null" ]]; then
      echo "package publication: PR $number has no head commit" >&2
      return 1
    fi
    if [[ "$current_oid" != "$expected_oid" ]]; then
      echo "package publication: PR $number head changed; restarting checks on $current_oid"
      expected_oid="$current_oid"
      if ((attempt < CHECK_ATTEMPTS)); then
        sleep "$CHECK_SLEEP_SECONDS"
        continue
      fi
    fi

    if ! validate_changed_files "$pr" "$role"; then
      return 1
    fi
    if ! required_is_current_at_ref "$role" "$current_oid"; then
      echo "package publication: $role manifests failed release-side validation at $current_oid" >&2
      return 1
    fi

    local failed
    failed="$(printf '%s\n' "$pr" | jq '[
      .statusCheckRollup[] |
      select(
        (.__typename == "CheckRun" and (
          .conclusion == "ACTION_REQUIRED" or
          .conclusion == "CANCELLED" or
          .conclusion == "FAILURE" or
          .conclusion == "STALE" or
          .conclusion == "STARTUP_FAILURE" or
          .conclusion == "TIMED_OUT"
        )) or
        (.__typename == "StatusContext" and (
          .state == "ERROR" or .state == "FAILURE"
        ))
      )
    ] | length')"
    if [[ "$failed" != "0" ]]; then
      echo "package publication: PR $number has failed checks" >&2
      report_checks "$pr"
      return 1
    fi

    local pending
    pending="$(printf '%s\n' "$pr" | jq '[
      .statusCheckRollup[] |
      select(
        (.__typename == "CheckRun" and .status != "COMPLETED") or
        (.__typename == "StatusContext" and .state != "SUCCESS")
      )
    ] | length')"
    local validate_ok=false
    local build_site_ok=false
    if [[ "$pending" == "0" ]]; then
      local check_runs
      check_runs="$(pkgs_gh api \
        -H "Accept: application/vnd.github+json" \
        "repos/${PKGS_REPO}/commits/${current_oid}/check-runs?filter=latest&per_page=100")" ||
        return 1
      if validate_required_action_check \
        "$check_runs" \
        validate \
        "Validate packages" \
        ".github/workflows/validate-packages.yml" \
        "$current_oid"; then
        validate_ok=true
      fi
      if validate_required_action_check \
        "$check_runs" \
        build-site \
        CI \
        ".github/workflows/ci.yml" \
        "$current_oid"; then
        build_site_ok=true
      fi
    fi

    if [[ "$pending" == "0" &&
          "$validate_ok" == "true" &&
          "$build_site_ok" == "true" ]]; then
      VERIFIED_HEAD_OID="$current_oid"
      echo "package publication: PR $number checks passed at $VERIFIED_HEAD_OID"
      return 0
    fi

    echo "package publication: waiting for PR $number checks ($attempt/$CHECK_ATTEMPTS): pending=$pending validate=$validate_ok build-site=$build_site_ok"
    if ((attempt < CHECK_ATTEMPTS)); then
      sleep "$CHECK_SLEEP_SECONDS"
    fi
  done

  echo "package publication: PR $number checks did not complete" >&2
  return 1
}

merge_checked_pr() {
  local branch="$1"
  local role="$2"

  for ((attempt = 1; attempt <= PR_ATTEMPTS; attempt++)); do
    if required_is_current "$role"; then
      echo "package publication: $branch is already merged at $VERSION"
      resolve_winning_release_merge || return 1
      if [[ "$PACKAGE_GATE_MODE" == "verify-heads" ]]; then
        VERIFIED_PACKAGE_HEAD_OID="$WINNING_RELEASE_MERGE_OID"
      fi
      return 0
    fi

    local prs
    prs="$(pkgs_gh pr list \
      --repo "$PKGS_REPO" \
      --state open \
      --head "$branch" \
      --limit 100 \
      --json number,baseRefName,headRefName,headRefOid,headRepositoryOwner,isCrossRepository,state,url)"

    local exact
    exact="$(printf '%s\n' "$prs" |
      jq \
        --arg branch "$branch" \
        --arg owner "$EXPECTED_OWNER" \
        '[.[] | select(
          .headRefName == $branch and
          .baseRefName == "main" and
          .headRepositoryOwner.login == $owner and
          .isCrossRepository == false
        )]')"
    local count
    count="$(printf '%s\n' "$exact" | jq 'length')"

    if [[ "$count" == "1" ]]; then
      local number
      number="$(printf '%s\n' "$exact" | jq -r '.[0].number')"
      local head_oid
      head_oid="$(printf '%s\n' "$exact" | jq -r '.[0].headRefOid')"
      VERIFIED_HEAD_OID=""
      wait_for_required_checks "$number" "$branch" "$head_oid" "$role"
      if [[ "$PACKAGE_GATE_MODE" == "verify-heads" ]]; then
        VERIFIED_PACKAGE_HEAD_OID="$VERIFIED_HEAD_OID"
        echo "package publication: verified $role PR head $VERIFIED_HEAD_OID without merging"
        return 0
      fi
      local expected_head=""
      expected_head="${EXPECTED_PACKAGE_HEAD_OID:-}"
      if [[ -n "$expected_head" && "$VERIFIED_HEAD_OID" != "$expected_head" ]]; then
        echo "package publication: $role PR head changed after consumer smoke" >&2
        return 1
      fi
      require_current_release_manifest || return 1
      echo "package publication: squash-merging checked PR $PKGS_REPO#$number ($branch)"
      if ! pkgs_gh pr merge \
        --repo "$PKGS_REPO" \
        --squash \
        --match-head-commit "$VERIFIED_HEAD_OID" \
        "$number"; then
        # A concurrent merge is benign only if the consumer-facing files prove
        # it reached main at the exact requested version.
        if required_is_current "$role"; then
          return 0
        fi
        echo "package publication: failed to squash-merge checked PR $number" >&2
        return 1
      fi
      return 0
    fi

    if [[ "$count" != "0" ]]; then
      echo "package publication: ambiguous open PRs for exact branch $branch" >&2
      printf '%s\n' "$exact" >&2
      return 1
    fi

    if ((attempt < PR_ATTEMPTS)); then
      echo "package publication: waiting for exact PR branch $branch ($attempt/$PR_ATTEMPTS)"
      sleep "$PR_SLEEP_SECONDS"
    fi
  done

  report_pr "$branch"
  echo "package publication: no open PR found for exact branch $branch" >&2
  return 1
}

[[ "$PACKAGE_GATE_MODE" == "merge" ||
   "$PACKAGE_GATE_MODE" == "verify-heads" ||
   "$PACKAGE_GATE_MODE" == "resolve-merged" ||
   "$PACKAGE_GATE_MODE" == "resolve-historical" ||
   "$PACKAGE_GATE_MODE" == "recheck-current" ]] || {
  echo "package publication: unknown PACKAGE_GATE_MODE: $PACKAGE_GATE_MODE" >&2
  exit 1
}
VERIFIED_PACKAGE_HEAD_OID=""
VERIFIED_MAIN_BEFORE=""
if [[ "$PACKAGE_GATE_MODE" == "resolve-merged" ]]; then
  resolve_winning_release_merge
  if [[ -n "${GITHUB_OUTPUT:-}" ]]; then
    {
      echo "pkgs_main_oid=$(resolve_main_oid)"
      echo "package_release_merge_oid=$WINNING_RELEASE_MERGE_OID"
    } >> "$GITHUB_OUTPUT"
  fi
  exit 0
fi
if [[ "$PACKAGE_GATE_MODE" == "resolve-historical" ]]; then
  resolve_historical_release_merge
  if [[ -n "${GITHUB_OUTPUT:-}" ]]; then
    echo "package_release_merge_oid=$WINNING_RELEASE_MERGE_OID" >> "$GITHUB_OUTPUT"
  fi
  exit 0
fi
if [[ "$PACKAGE_GATE_MODE" == "recheck-current" ]]; then
  recheck_current_package_ownership
  exit 0
fi
resolve_package_attempt_branch
if [[ "$PACKAGE_GATE_MODE" == "verify-heads" ]]; then
  VERIFIED_MAIN_BEFORE="$(resolve_main_oid)" || exit 1
fi
merge_checked_pr "$PACKAGE_BRANCH" package

if [[ "$PACKAGE_GATE_MODE" == "verify-heads" ]]; then
  [[ "$(resolve_main_oid)" == "$VERIFIED_MAIN_BEFORE" ]] || {
    echo "package publication: pkgs main changed while PR head was verified" >&2
    exit 1
  }
  if [[ -n "${GITHUB_OUTPUT:-}" ]]; then
    {
      echo "package_head_oid=$VERIFIED_PACKAGE_HEAD_OID"
      if [[ -n "$WINNING_RELEASE_MERGE_OID" ]]; then
        echo "package_release_merge_oid=$WINNING_RELEASE_MERGE_OID"
      fi
    } >> "$GITHUB_OUTPUT"
  fi
  exit 0
fi

for ((attempt = 1; attempt <= MAX_ATTEMPTS; attempt++)); do
  cli_version="$(cask_version "Casks/radioactive-ralph.rb" 2>/dev/null || true)"
  gui_version="$(cask_version "Casks/radioactive-ralph-gui.rb" 2>/dev/null || true)"
  scoop="$(scoop_version 2>/dev/null || true)"

  if all_required_are_current; then
    if ! resolve_winning_release_merge; then
      echo "package publication: exact bytes are current but winning merge is not yet provable"
      if ((attempt < MAX_ATTEMPTS)); then
        sleep "$SLEEP_SECONDS"
        continue
      fi
      break
    fi
    if [[ -n "${GITHUB_OUTPUT:-}" ]]; then
      {
        echo "pkgs_main_oid=$VERIFIED_MAIN_OID"
        echo "package_release_merge_oid=$WINNING_RELEASE_MERGE_OID"
      } >> "$GITHUB_OUTPUT"
    fi
    echo "package publication: jbcom/pkgs main contains release-bound $VERSION CLI cask, GUI cask, and Scoop manifests"
    exit 0
  fi

  echo "package publication: waiting for release-bound $VERSION ($attempt/$MAX_ATTEMPTS): CLI=${cli_version:-missing} GUI=${gui_version:-missing} Scoop=${scoop:-missing}"
  if ((attempt < MAX_ATTEMPTS)); then
    sleep "$SLEEP_SECONDS"
  fi
done

report_pr "$PACKAGE_BRANCH"
echo "package publication: exact versions did not reach jbcom/pkgs main" >&2
exit 1
