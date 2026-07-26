#!/usr/bin/env bash
# Validate the audit evidence emitted by dispatch_release_recovery.sh. This is
# an operator attestation, not a substitute credential: GitHub Actions' token
# cannot receive the Administration permission required by the setting API.
set -euo pipefail

EVIDENCE="${1:?usage: verify_immutable_release_preflight.sh <json> <tag> <source-commit> <release-id> <phase> [max-age-seconds]}"
TAG="${2:?tag is required}"
SOURCE_COMMIT="${3:?source commit is required}"
EXPECTED_RELEASE_ID="${4:?release id is required}"
PHASE="${5:?phase is required}"
MAX_AGE="${6:-0}"
REPOSITORY="${GITHUB_REPOSITORY:-jbcom/radioactive-ralph}"

[[ "$TAG" == "v0.22.0" ]]
[[ "$SOURCE_COMMIT" =~ ^[0-9a-f]{40}$ ]]
[[ "$EXPECTED_RELEASE_ID" =~ ^[1-9][0-9]*$ ]]
[[ "$PHASE" == "prepare" || "$PHASE" == "publish" ]]
[[ "$MAX_AGE" =~ ^[0-9]+$ ]]

jq -e \
  --arg repository "$REPOSITORY" \
  --arg tag "$TAG" \
  --arg source "$SOURCE_COMMIT" \
  --argjson release_id "$EXPECTED_RELEASE_ID" \
  --arg phase "$PHASE" \
  '
    (keys | sort) == [
      "checked_at", "etag_sha256", "immutable_releases_enabled", "phase",
      "release_id", "repository", "request_id", "schema", "source_commit",
      "tag"
    ] and
    .schema == 1 and
    .repository == $repository and
    .tag == $tag and
    .source_commit == $source and
    .release_id == $release_id and
    .phase == $phase and
    .immutable_releases_enabled == true and
    (.checked_at | type == "number" and floor == . and . > 0) and
    (.etag_sha256 | test("^[0-9a-f]{64}$")) and
    (.request_id | test("^[A-F0-9]+(:[A-F0-9]+)+$"))
  ' <<<"$EVIDENCE" >/dev/null

if [[ "$MAX_AGE" != 0 ]]; then
  checked_at="$(jq -er '.checked_at' <<<"$EVIDENCE")"
  now="$(date -u +%s)"
  age=$((now - checked_at))
  if (( age < 0 || age > MAX_AGE )); then
    echo "immutable release preflight: evidence age ${age}s is outside 0..${MAX_AGE}s" >&2
    exit 1
  fi
fi

echo "immutable release preflight: exact v0.22.0 operator evidence verified"
