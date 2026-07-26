#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

cat > "$TMP/gh" <<'FAKEGH'
#!/usr/bin/env bash
set -euo pipefail
kind="${1:?}"; shift
include_status=0
for argument in "$@"; do
  [[ "$argument" == "--include" ]] && include_status=1
done
if [[ "$kind" == api ]]; then
  request="${*: -1}"
  if [[ "$request" == repos/jbcom/radioactive-ralph/contents/.release-please-manifest.json* ]]; then
    version=1.2.3
    [[ "${FAKE_SOURCE_SUPERSEDED:-0}" == 1 ]] && version=1.2.4
    printf '{".":"%s"}\n' "$version"
    exit 0
  fi
  if [[ "$request" == repos/jbcom/pkgs/contents/* ]]; then
    path="${request#repos/jbcom/pkgs/contents/}"
    ref="${path##*ref=}"
    path="${path%%\?*}"
    [[ "$ref" != main ]] || ref=refs/heads/main
    if git --git-dir="$FAKE_REMOTE" show "${ref}:${path}"; then
      exit 0
    fi
    ((include_status == 0)) || printf 'HTTP/2.0 404 Not Found\n'
    echo "gh: Not Found (HTTP 404)" >&2
    exit 1
  fi
  if [[ "$request" == repos/jbcom/pkgs/commits/*/check-runs* ]]; then
    head="${request#repos/jbcom/pkgs/commits/}"; head="${head%%/*}"
    app=15368
    [[ "${FAKE_SPOOF_APP:-0}" == 1 ]] && app=99999
    printf '{"check_runs":[{"name":"validate","status":"completed","conclusion":"success","details_url":"https://github.com/jbcom/pkgs/actions/runs/1001/job/1","app":{"id":%s,"slug":"github-actions"}},{"name":"build-site","status":"completed","conclusion":"success","details_url":"https://github.com/jbcom/pkgs/actions/runs/1002/job/2","app":{"id":15368,"slug":"github-actions"}}],"head":"%s"}\n' "$app" "$head"
    exit 0
  fi
  if [[ "$request" == repos/jbcom/pkgs/git/commits/* ]]; then
    printf '{"parents":[{"sha":"%s"}]}\n' "$PRIOR_OID"
    exit 0
  fi
  if [[ "$request" == repos/jbcom/pkgs/actions/runs/1001 || "$request" == repos/jbcom/pkgs/actions/runs/1002 ]]; then
    head="$(git --git-dir="$FAKE_REMOTE" rev-parse refs/heads/chore/rollback-radioactive-ralph-1.2.3)"
    if [[ "$request" == *1001 ]]; then
      name="Validate packages"; path=".github/workflows/validate-packages.yml"
    else
      name=CI; path=".github/workflows/ci.yml"
    fi
    printf '{"name":"%s","path":"%s","event":"pull_request","head_sha":"%s","status":"completed","conclusion":"success","repository":{"full_name":"jbcom/pkgs"},"head_repository":{"full_name":"jbcom/pkgs"}}\n' "$name" "$path" "$head"
    exit 0
  fi
fi
if [[ "$kind" == pr && "${1:-}" == create ]]; then
  exit 0
fi
if [[ "$kind" == pr && "${1:-}" == list && "$*" == *"--state merged"* ]]; then
  printf '[{"number":41,"baseRefName":"main","headRefName":"chore/update-radioactive-ralph-1.2.3","headRepositoryOwner":{"login":"jbcom"},"isCrossRepository":false,"files":[{"path":"Casks/radioactive-ralph.rb"},{"path":"Casks/radioactive-ralph-gui.rb"},{"path":"bucket/radioactive-ralph.json"}],"mergeCommit":{"oid":"%s"}}]\n' "$FAILED_OID"
  exit 0
fi
if [[ "$kind" == pr && "${1:-}" == list ]]; then
  head="$(git --git-dir="$FAKE_REMOTE" rev-parse refs/heads/chore/rollback-radioactive-ralph-1.2.3)"
  printf '[{"number":42,"baseRefName":"main","headRefName":"chore/rollback-radioactive-ralph-1.2.3","headRefOid":"%s","headRepositoryOwner":{"login":"jbcom"},"isCrossRepository":false,"files":[{"path":"Casks/radioactive-ralph.rb"},{"path":"Casks/radioactive-ralph-gui.rb"},{"path":"bucket/radioactive-ralph.json"}]}]\n' "$head"
  exit 0
fi
if [[ "$kind" == pr && "${1:-}" == checks ]]; then
  [[ "${FAKE_CHECK_FAILURE:-0}" != 1 ]]
  exit 0
fi
if [[ "$kind" == pr && "${1:-}" == view ]]; then
  if [[ "${FAKE_HEAD_MUTATION:-0}" == 1 ]]; then
    printf 'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\n'
  else
    git --git-dir="$FAKE_REMOTE" rev-parse refs/heads/chore/rollback-radioactive-ralph-1.2.3
  fi
  exit 0
fi
if [[ "$kind" == pr && "${1:-}" == merge ]]; then
  head="$(git --git-dir="$FAKE_REMOTE" rev-parse refs/heads/chore/rollback-radioactive-ralph-1.2.3)"
  git --git-dir="$FAKE_REMOTE" update-ref refs/heads/main "$head"
  exit 0
fi
echo "fake gh: unexpected $kind $*" >&2
exit 1
FAKEGH
chmod +x "$TMP/gh"

setup_remote() {
  prior_gui_state="${1:-present}"
  CASE="$TMP/case-$RANDOM"
  FAKE_REMOTE="$CASE/pkgs.git"
  seed="$CASE/seed"
  mkdir -p "$CASE"
  git init -q --bare "$FAKE_REMOTE"
  git init -q "$seed"
  git -C "$seed" config user.name test
  git -C "$seed" config user.email test@example.invalid
  mkdir -p "$seed/Casks" "$seed/bucket"
  printf 'cli old\n' > "$seed/Casks/radioactive-ralph.rb"
  if [[ "$prior_gui_state" == present ]]; then
    printf 'gui old\n' > "$seed/Casks/radioactive-ralph-gui.rb"
  fi
  printf '{"version":"1.2.2"}\n' > "$seed/bucket/radioactive-ralph.json"
  printf 'base\n' > "$seed/unrelated.txt"
  git -C "$seed" add .
  git -C "$seed" commit -q -m prior
  SEALED_PRIOR_OID="$(git -C "$seed" rev-parse HEAD)"
  provenance_dir="$CASE/provenance"
  mkdir -p "$provenance_dir/Casks" "$provenance_dir/bucket"
  provenance_files='{}'
  provenance_members=(provenance.json)
  for path in Casks/radioactive-ralph.rb Casks/radioactive-ralph-gui.rb bucket/radioactive-ralph.json; do
    if git -C "$seed" cat-file -e "${SEALED_PRIOR_OID}:${path}" 2>/dev/null; then
      git -C "$seed" show "${SEALED_PRIOR_OID}:${path}" > "$provenance_dir/$path"
      digest="$(sha256sum "$provenance_dir/$path" | awk '{print $1}')"
      provenance_files="$(jq -c --arg path "$path" --arg sha "$digest" \
        '. + {($path): {state:"present", sha256:$sha}}' \
        <<<"$provenance_files")"
      provenance_members+=("$path")
    else
      provenance_files="$(jq -c --arg path "$path" \
        '. + {($path): {state:"missing"}}' <<<"$provenance_files")"
    fi
  done
  jq -n --arg prior "$SEALED_PRIOR_OID" --argjson files "$provenance_files" \
    '{schema:2,release_version:"1.2.3",prior_main_oid:$prior,files:$files}' \
    > "$provenance_dir/provenance.json"
  PROVENANCE="$CASE/package-rollback.tar.gz"
  tar -czf "$PROVENANCE" -C "$provenance_dir" "${provenance_members[@]}"
  # Legitimate package-main work between sealing and the release merge must be
  # retained. The winning squash merge's first parent, not seal-time
  # provenance, is the authoritative rollback point.
  printf 'intervening\n' > "$seed/unrelated.txt"
  git -C "$seed" commit -qam intervening
  PRIOR_OID="$(git -C "$seed" rev-parse HEAD)"
  printf 'cli failed\n' > "$seed/Casks/radioactive-ralph.rb"
  printf 'gui failed\n' > "$seed/Casks/radioactive-ralph-gui.rb"
  printf '{"version":"1.2.3"}\n' > "$seed/bucket/radioactive-ralph.json"
  git -C "$seed" add \
    Casks/radioactive-ralph.rb \
    Casks/radioactive-ralph-gui.rb \
    bucket/radioactive-ralph.json
  git -C "$seed" commit -q -m failed
  FAILED_OID="$(git -C "$seed" rev-parse HEAD)"
  # Unrelated package-repo progress after the failed release is allowed.
  printf 'advanced\n' > "$seed/unrelated.txt"
  git -C "$seed" commit -qam unrelated
  git -C "$seed" branch -M main
  git -C "$seed" remote add origin "$FAKE_REMOTE"
  git -C "$seed" push -q origin main
  git --git-dir="$FAKE_REMOTE" symbolic-ref HEAD refs/heads/main
  export FAKE_REMOTE PRIOR_OID FAILED_OID PROVENANCE
}

run_rollback() {
  PKGS_GH_TOKEN=fake-pkgs \
  RELEASE_GH_TOKEN=fake-release \
  GH_BIN="$TMP/gh" \
  PKGS_CLONE_URL="$FAKE_REMOTE" \
  CHECK_ATTEMPTS=1 \
  CHECK_SLEEP_SECONDS=0 \
    bash "$ROOT/scripts/ci/rollback_package_manifests.sh" \
      1.2.3 "$PROVENANCE" "$FAILED_OID"
}

setup_remote
run_rollback
[[ "$(git --git-dir="$FAKE_REMOTE" show main:Casks/radioactive-ralph.rb)" == "cli old" ]]
[[ "$(git --git-dir="$FAKE_REMOTE" show main:Casks/radioactive-ralph-gui.rb)" == "gui old" ]]
[[ "$(git --git-dir="$FAKE_REMOTE" show main:unrelated.txt)" == "advanced" ]]

# On the first GUI release, the prior main has no GUI cask. Compensation must
# delete the failed release's newly added cask through the protected PR.
setup_remote missing
run_rollback
if git --git-dir="$FAKE_REMOTE" cat-file -e \
  main:Casks/radioactive-ralph-gui.rb 2>/dev/null; then
  echo "expected rollback to delete the newly introduced GUI cask" >&2
  exit 1
fi
[[ "$(git --git-dir="$FAKE_REMOTE" show main:Casks/radioactive-ralph.rb)" == "cli old" ]]
[[ "$(git --git-dir="$FAKE_REMOTE" show main:unrelated.txt)" == "advanced" ]]

# A literal dash is the sole explicit provenance-skip sentinel.
setup_remote
PROVENANCE=-
run_rollback

for scenario in bad_provenance missing_provenance wrong_failed target_superseded head_mutation check_failure spoof_app; do
  setup_remote
  provenance="$PROVENANCE"; failed="$FAILED_OID"
  unset FAKE_SOURCE_SUPERSEDED FAKE_HEAD_MUTATION FAKE_CHECK_FAILURE FAKE_SPOOF_APP
  case "$scenario" in
    bad_provenance)
      printf 'invalid\n' > "$provenance"
      ;;
    missing_provenance)
      provenance="$CASE/does-not-exist.tar.gz"
      ;;
    wrong_failed) failed=bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb ;;
    target_superseded)
      seed="$CASE/seed"
      printf 'newer package\n' > "$seed/Casks/radioactive-ralph.rb"
      git -C "$seed" commit -qam superseded
      git -C "$seed" push -q origin main
      ;;
    head_mutation) export FAKE_HEAD_MUTATION=1 ;;
    check_failure) export FAKE_CHECK_FAILURE=1 ;;
    spoof_app) export FAKE_SPOOF_APP=1 ;;
  esac
  if PKGS_GH_TOKEN=fake-pkgs RELEASE_GH_TOKEN=fake-release \
    GH_BIN="$TMP/gh" PKGS_CLONE_URL="$FAKE_REMOTE" \
    FAKE_SOURCE_SUPERSEDED="${FAKE_SOURCE_SUPERSEDED:-0}" \
    FAKE_HEAD_MUTATION="${FAKE_HEAD_MUTATION:-0}" \
    FAKE_CHECK_FAILURE="${FAKE_CHECK_FAILURE:-0}" \
    FAKE_SPOOF_APP="${FAKE_SPOOF_APP:-0}" \
    CHECK_ATTEMPTS=1 CHECK_SLEEP_SECONDS=0 \
    bash "$ROOT/scripts/ci/rollback_package_manifests.sh" \
      1.2.3 "$provenance" "$failed" >/dev/null 2>&1; then
    echo "expected rollback scenario $scenario to fail closed" >&2
    exit 1
  fi
done

echo "package rollback tests: ok"
