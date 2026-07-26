#!/usr/bin/env bash
# Perform the Administration-authorized immutable-release preflight locally and
# dispatch the bounded v0.22.0 recovery without creating or moving any tag.
set -euo pipefail

PHASE="${1:?usage: dispatch_release_recovery.sh <prepare|publish> v0.22.0 [prepared-run-id]}"
TAG="${2:?usage: dispatch_release_recovery.sh <prepare|publish> v0.22.0 [prepared-run-id]}"
PREPARED_RUN_ID="${3:-0}"
REPOSITORY="${RELEASE_REPO:-jbcom/radioactive-ralph}"
GH_BIN="${GH_BIN:-gh}"
EXPECTED_ACTOR_ID=2650679

[[ "$PHASE" == "prepare" || "$PHASE" == "publish" ]] || {
  echo "release recovery: phase must be prepare or publish" >&2
  exit 1
}
[[ "$TAG" == "v0.22.0" ]] || {
  echo "release recovery: only the existing v0.22.0 handoff is authorized" >&2
  exit 1
}
actor_id="$("$GH_BIN" api user --jq '.id')"
[[ "$actor_id" == "$EXPECTED_ACTOR_ID" ]] || {
  echo "release recovery: authenticated GitHub actor is not the authorized release administrator" >&2
  exit 1
}
if [[ "$PHASE" == "publish" ]]; then
  [[ "$PREPARED_RUN_ID" =~ ^[1-9][0-9]*$ ]] || {
    echo "release recovery: publish requires the successful prepare run ID" >&2
    exit 1
  }
else
  PREPARED_RUN_ID=0
fi

response="$("$GH_BIN" api --include \
  -H "X-GitHub-Api-Version: 2026-03-10" \
  "repos/${REPOSITORY}/immutable-releases")"
body="$(awk 'body { print } /^\r?$/ { body=1 }' <<<"$response")"
jq -e '.enabled == true' <<<"$body" >/dev/null || {
  echo "release recovery: repository immutable releases are not enabled" >&2
  exit 1
}

etag="$(awk 'tolower($1) == "etag:" { sub(/\r$/, "", $2); print $2 }' \
  <<<"$response" | tail -n 1)"
request_id="$(awk 'tolower($1) == "x-github-request-id:" {
  sub(/\r$/, "", $2); print $2
}' <<<"$response" | tail -n 1)"
server_date="$(awk 'tolower($1) == "date:" {
  sub(/^[^:]+:[[:space:]]*/, ""); sub(/\r$/, ""); print
}' <<<"$response" | tail -n 1)"
[[ -n "$etag" && "$request_id" =~ ^[A-F0-9]+(:[A-F0-9]+)+$ ]]

if checked_at="$(date -j -u -f "%a, %d %b %Y %H:%M:%S GMT" \
  "$server_date" +%s 2>/dev/null)"; then
  :
else
  checked_at="$(date -u -d "$server_date" +%s)"
fi
if command -v sha256sum >/dev/null 2>&1; then
  etag_sha256="$(printf '%s' "$etag" | sha256sum | awk '{print $1}')"
else
  etag_sha256="$(printf '%s' "$etag" | shasum -a 256 | awk '{print $1}')"
fi

source_commit="$("$GH_BIN" api --jq '.object.sha' \
  "repos/${REPOSITORY}/git/ref/tags/${TAG}")"
[[ "$source_commit" =~ ^[0-9a-f]{40}$ ]]
release_view="$("$GH_BIN" release view "$TAG" --repo "$REPOSITORY" \
  --json databaseId,isDraft,isPrerelease,tagName,targetCommitish)"
release_id="$(jq -er '.databaseId | select(type == "number" and . > 0)' \
  <<<"$release_view")"
release="$("$GH_BIN" api \
  "repos/${REPOSITORY}/releases/${release_id}")"
jq -e --argjson id "$release_id" --arg tag "$TAG" --arg source "$source_commit" \
  '.id == $id and .draft == true and .prerelease == false and
   .tag_name == $tag and .target_commitish == $source' \
  <<<"$release" >/dev/null

workflow_commit="$("$GH_BIN" api --jq '.object.sha' \
  "repos/${REPOSITORY}/git/ref/heads/main")"
[[ "$workflow_commit" =~ ^[0-9a-f]{40}$ ]]
evidence="$(jq -cn \
  --arg repository "$REPOSITORY" \
  --arg tag "$TAG" \
  --arg source "$source_commit" \
  --argjson release_id "$release_id" \
  --arg phase "$PHASE" \
  --arg request_id "$request_id" \
  --arg etag_sha256 "$etag_sha256" \
  --argjson checked_at "$checked_at" \
  '{
    schema: 1,
    repository: $repository,
    tag: $tag,
    source_commit: $source,
    release_id: $release_id,
    phase: $phase,
    immutable_releases_enabled: true,
    checked_at: $checked_at,
    etag_sha256: $etag_sha256,
    request_id: $request_id
  }')"

dispatch_output="$("$GH_BIN" workflow run release.yml \
  --repo "$REPOSITORY" --ref main \
  --field phase="$PHASE" \
  --field tag="$TAG" \
  --field source_commit="$source_commit" \
  --field release_id="$release_id" \
  --field workflow_commit="$workflow_commit" \
  --field prepared_run_id="$PREPARED_RUN_ID" \
  --field immutable_releases_preflight="$evidence")"

printf '%s\n' "$dispatch_output"
echo "release recovery: dispatched $PHASE for $TAG from main workflow $workflow_commit"
