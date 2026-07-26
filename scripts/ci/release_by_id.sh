#!/usr/bin/env bash
# Exact GitHub Release helpers. Draft releases are intentionally absent from
# GET /releases/tags/{tag}; after admission, every mutable operation therefore
# stays bound to the numeric release ID discovered from the draft-aware list.

ralph_release_require_id() {
  if [[ ! "${RELEASE_ID:-}" =~ ^[1-9][0-9]*$ ]]; then
    echo "release id: RELEASE_ID must be a positive decimal integer" >&2
    return 1
  fi
  if [[ -z "${RELEASE_REPO:-}" ]]; then
    echo "release id: RELEASE_REPO is required" >&2
    return 1
  fi
  if ! declare -F release_gh >/dev/null; then
    echo "release id: caller must define release_gh" >&2
    return 1
  fi
}

ralph_release_by_id() {
  local expected_tag="${1:?expected tag is required}"
  local expected_target="${2:?expected target commit is required}"
  local expected_state="${3:-either}"
  local release

  ralph_release_require_id
  [[ "$expected_tag" =~ ^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$ ]]
  [[ "$expected_target" =~ ^[0-9a-f]{40,64}$ ]]
  case "$expected_state" in
    draft)
      ;;
    published)
      ;;
    either)
      ;;
    *)
      echo "release id: expected state must be draft, published, or either" >&2
      return 1
      ;;
  esac

  if ! release="$(release_gh api \
    "repos/${RELEASE_REPO}/releases/${RELEASE_ID}")"; then
    echo "release id: failed to read admitted numeric release" >&2
    return 1
  fi
  jq -ce \
    --argjson id "$RELEASE_ID" \
    --arg tag "$expected_tag" \
    --arg target "$expected_target" \
    '
      .id == $id and
      .tag_name == $tag and
      .target_commitish == $target
    ' <<<"$release" >/dev/null || {
      echo "release id: numeric release does not match exact tag, target, and state" >&2
      return 1
    }
  case "$expected_state" in
    draft)
      jq -e '.draft == true and .prerelease == false' <<<"$release" >/dev/null
      ;;
    published)
      jq -e \
        '.draft == false and .prerelease == false and .immutable == true' \
        <<<"$release" >/dev/null
      ;;
    either)
      jq -e \
        '.prerelease == false and
         ((.draft == true) or (.draft == false and .immutable == true))' \
        <<<"$release" >/dev/null
      ;;
  esac || {
    echo "release id: numeric release does not match exact tag, target, and state" >&2
    return 1
  }
  printf '%s\n' "$release"
}

ralph_release_assets_by_id() {
  local pages
  ralph_release_require_id
  if ! pages="$(release_gh api --paginate --slurp \
    "repos/${RELEASE_REPO}/releases/${RELEASE_ID}/assets?per_page=100")"; then
    echo "release id: failed to list assets for admitted numeric release" >&2
    return 1
  fi
  jq -ce '
    if type == "array" and all(.[]; type == "array") then
      flatten
    else
      error("release asset pages were not arrays")
    end
  ' <<<"$pages"
}

ralph_release_asset_by_name() {
  local name="${1:?asset name is required}"
  local assets
  assets="$(ralph_release_assets_by_id)"
  jq -ce --arg name "$name" '
    [.[] | select(.name == $name)] as $matches |
    if ($matches | length) == 1 and
       ($matches[0].id | type) == "number" and
       $matches[0].id > 0 and
       $matches[0].state == "uploaded"
    then $matches[0]
    else error("release asset name is absent, duplicated, or not uploaded")
    end
  ' <<<"$assets"
}

ralph_release_download_asset() {
  local expected_tag="${1:?expected tag is required}"
  local expected_target="${2:?expected target commit is required}"
  local expected_state="${3:?expected state is required}"
  local name="${4:?asset name is required}"
  local destination="${5:?destination is required}"
  local asset asset_id partial

  ralph_release_by_id "$expected_tag" "$expected_target" "$expected_state" \
    >/dev/null
  [[ ! -e "$destination" ]] || {
    echo "release id: refusing to overwrite download destination: $destination" >&2
    return 1
  }
  asset="$(ralph_release_asset_by_name "$name")"
  asset_id="$(jq -er '.id' <<<"$asset")"
  partial="$(mktemp "${destination}.partial.XXXXXX")"
  if ! release_gh api -H "Accept: application/octet-stream" \
    "repos/${RELEASE_REPO}/releases/assets/${asset_id}" >"$partial"; then
    rm -f "$partial"
    return 1
  fi
  mv "$partial" "$destination"
}

ralph_release_upload_asset() {
  local expected_tag="${1:?expected tag is required}"
  local expected_target="${2:?expected target commit is required}"
  local path="${3:?asset path is required}"
  local mode="${4:-create}"
  local name encoded response uploaded assets matches count prior_id

  ralph_release_by_id "$expected_tag" "$expected_target" draft >/dev/null
  case "$mode" in
    create|replace-before-seal) ;;
    *)
      echo "release id: upload mode must be create or replace-before-seal" >&2
      return 1
      ;;
  esac
  [[ -f "$path" && ! -L "$path" ]] || {
    echo "release id: upload input must be a regular non-symlink file: $path" >&2
    return 1
  }
  name="$(basename "$path")"
  encoded="$(jq -rn --arg value "$name" '$value | @uri')"
  assets="$(ralph_release_assets_by_id)"
  matches="$(jq -ce --arg name "$name" \
    '[.[] | select(.name == $name)]' <<<"$assets")"
  count="$(jq -er 'length' <<<"$matches")"
  if ((count > 1)); then
    echo "release id: duplicate existing asset names quarantine upload: $name" >&2
    return 1
  fi
  if ((count == 1)); then
    if [[ "$mode" == create ]]; then
      echo "release id: refusing to replace existing asset: $name" >&2
      return 1
    fi
    prior_id="$(jq -er \
      '.[0] | select(.state == "uploaded") | .id |
       select(type == "number" and . > 0)' <<<"$matches")"
    if ! release_gh api --method DELETE \
      "repos/${RELEASE_REPO}/releases/assets/${prior_id}"; then
      echo "release id: failed to delete exact pre-seal asset: $name" >&2
      return 1
    fi
    ralph_release_by_id "$expected_tag" "$expected_target" draft >/dev/null
    assets="$(ralph_release_assets_by_id)"
    if jq -e --arg name "$name" \
      'any(.[]; .name == $name)' <<<"$assets" >/dev/null; then
      echo "release id: replaced asset still exists after exact delete: $name" >&2
      return 1
    fi
  fi
  if ! response="$(release_gh api --method POST --hostname uploads.github.com \
    -H "Content-Type: application/octet-stream" \
    "repos/${RELEASE_REPO}/releases/${RELEASE_ID}/assets?name=${encoded}" \
    --input "$path")"; then
    echo "release id: failed to upload exact asset: $name" >&2
    return 1
  fi
  jq -e --arg name "$name" \
    '.id > 0 and .name == $name and .state == "uploaded"' \
    <<<"$response" >/dev/null
  ralph_release_by_id "$expected_tag" "$expected_target" draft >/dev/null
  uploaded="$(ralph_release_asset_by_name "$name")"
  jq -e --argjson id "$(jq -er '.id' <<<"$response")" \
    '.id == $id' <<<"$uploaded" >/dev/null
  printf '%s\n' "$response"
}
