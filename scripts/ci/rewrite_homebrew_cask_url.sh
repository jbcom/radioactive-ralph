#!/usr/bin/env bash
set -euo pipefail

CASK_PATH="${1:?usage: rewrite_homebrew_cask_url.sh <cask> <version> <asset> <cached-absolute-path>}"
VERSION="${2:?usage: rewrite_homebrew_cask_url.sh <cask> <version> <asset> <cached-absolute-path>}"
ASSET="${3:?usage: rewrite_homebrew_cask_url.sh <cask> <version> <asset> <cached-absolute-path>}"
CACHED_PATH="${4:?usage: rewrite_homebrew_cask_url.sh <cask> <version> <asset> <cached-absolute-path>}"
RELEASE_REPO="${RELEASE_REPO:-jbcom/radioactive-ralph}"

[[ -f "$CASK_PATH" ]]
[[ "$VERSION" =~ ^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$ ]]
[[ "$ASSET" =~ ^[A-Za-z0-9._-]+$ && "$ASSET" == *"$VERSION"* ]]
[[ "$CACHED_PATH" == /* && -f "$CACHED_PATH" ]]
[[ "$RELEASE_REPO" =~ ^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$ ]]

version_template='#{version}'
template_asset="${ASSET//$VERSION/$version_template}"
literal_url="https://github.com/${RELEASE_REPO}/releases/download/v${VERSION}/${template_asset}\""
interpolated_url="https://github.com/${RELEASE_REPO}/releases/download/v${version_template}/${template_asset}\""
replacement="file://${CACHED_PATH}\""
rewritten="$(mktemp)"
trap 'rm -f "$rewritten"' EXIT

awk \
  -v literal="$literal_url" \
  -v interpolated="$interpolated_url" \
  -v replacement="$replacement" '
  function occurrences(haystack, needle, rest, at, found) {
    rest = haystack
    while ((at = index(rest, needle)) != 0) {
      found++
      rest = substr(rest, at + length(needle))
    }
    return found
  }
  {
    lines[NR] = $0
    count += occurrences($0, literal)
    count += occurrences($0, interpolated)
  }
  END {
    if (count != 1) {
      printf "cask URL rewrite: expected one trusted URL for cached asset, found %d\n", count > "/dev/stderr"
      exit 3
    }
    for (line_number = 1; line_number <= NR; line_number++) {
      line = lines[line_number]
      at = index(line, literal)
      needle = literal
      if (at == 0) {
        at = index(line, interpolated)
        needle = interpolated
      }
      if (at != 0) {
        line = substr(line, 1, at - 1) replacement substr(line, at + length(needle))
      }
      print line
    }
  }
' "$CASK_PATH" > "$rewritten"

[[ "$(grep -Foc "url \"file://${CACHED_PATH}\"" "$rewritten")" == 1 ]]
if grep -Fq "$literal_url" "$rewritten" ||
  grep -Fq "$interpolated_url" "$rewritten"; then
  echo "cask URL rewrite: trusted private draft URL remains after rewrite" >&2
  exit 1
fi

mv "$rewritten" "$CASK_PATH"
trap - EXIT
