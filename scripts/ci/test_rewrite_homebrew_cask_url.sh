#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

mkdir -p "$TMP/assets"
cli_asset="radioactive_ralph_1.2.3_darwin_arm64.tar.gz"
gui_asset="radioactive-ralph_1.2.3_darwin_arm64.dmg"
: > "$TMP/assets/$cli_asset"
: > "$TMP/assets/$gui_asset"

cat > "$TMP/cli.rb" <<'CASK'
cask "radioactive-ralph" do
  version "1.2.3"
  on_intel do
    url "https://github.com/jbcom/radioactive-ralph/releases/download/v1.2.3/radioactive_ralph_#{version}_darwin_amd64.tar.gz"
  end
  on_arm do
    url "https://github.com/jbcom/radioactive-ralph/releases/download/v1.2.3/radioactive_ralph_#{version}_darwin_arm64.tar.gz"
  end
end
CASK

bash "$ROOT/scripts/ci/rewrite_homebrew_cask_url.sh" \
  "$TMP/cli.rb" 1.2.3 "$cli_asset" "$TMP/assets/$cli_asset"
grep -Fq "url \"file://$TMP/assets/$cli_asset\"" "$TMP/cli.rb"
grep -Fq 'radioactive_ralph_#{version}_darwin_amd64.tar.gz' "$TMP/cli.rb"
if grep -Fq 'radioactive_ralph_#{version}_darwin_arm64.tar.gz' "$TMP/cli.rb"; then
  echo "CLI arm64 draft URL remained after rewrite" >&2
  exit 1
fi

cat > "$TMP/gui.rb" <<'CASK'
cask "radioactive-ralph-gui" do
  version "1.2.3"
  on_arm do
    url "https://github.com/jbcom/radioactive-ralph/releases/download/v#{version}/radioactive-ralph_#{version}_darwin_arm64.dmg"
  end
  on_intel do
    url "https://github.com/jbcom/radioactive-ralph/releases/download/v#{version}/radioactive-ralph_#{version}_darwin_amd64.dmg"
  end
end
CASK
cp "$TMP/gui.rb" "$TMP/gui-source.rb"

bash "$ROOT/scripts/ci/rewrite_homebrew_cask_url.sh" \
  "$TMP/gui.rb" 1.2.3 "$gui_asset" "$TMP/assets/$gui_asset"
grep -Fq "url \"file://$TMP/assets/$gui_asset\"" "$TMP/gui.rb"
grep -Fq 'radioactive-ralph_#{version}_darwin_amd64.dmg' "$TMP/gui.rb"
if grep -Fq 'radioactive-ralph_#{version}_darwin_arm64.dmg' "$TMP/gui.rb"; then
  echo "GUI arm64 draft URL remained after rewrite" >&2
  exit 1
fi

cat > "$TMP/untrusted.rb" <<'CASK'
cask "radioactive-ralph-gui" do
  version "1.2.3"
  url "https://attacker.invalid/releases/download/v#{version}/radioactive-ralph_#{version}_darwin_arm64.dmg"
end
CASK
cp "$TMP/untrusted.rb" "$TMP/untrusted-before.rb"
if bash "$ROOT/scripts/ci/rewrite_homebrew_cask_url.sh" \
  "$TMP/untrusted.rb" 1.2.3 "$gui_asset" "$TMP/assets/$gui_asset" \
  >/dev/null 2>&1; then
  echo "expected untrusted cask URL to fail" >&2
  exit 1
fi
cmp -s "$TMP/untrusted.rb" "$TMP/untrusted-before.rb"

cat "$TMP/gui-source.rb" "$TMP/gui-source.rb" > "$TMP/ambiguous.rb"
cp "$TMP/ambiguous.rb" "$TMP/ambiguous-before.rb"
if bash "$ROOT/scripts/ci/rewrite_homebrew_cask_url.sh" \
  "$TMP/ambiguous.rb" 1.2.3 "$gui_asset" "$TMP/assets/$gui_asset" \
  >/dev/null 2>&1; then
  echo "expected duplicate cached cask URL target to fail" >&2
  exit 1
fi
cmp -s "$TMP/ambiguous.rb" "$TMP/ambiguous-before.rb"

trusted_gui_url='url "https://github.com/jbcom/radioactive-ralph/releases/download/v#{version}/radioactive-ralph_#{version}_darwin_arm64.dmg"'
printf 'cask "duplicate" do %s %s end\n' \
  "$trusted_gui_url" "$trusted_gui_url" > "$TMP/same-line.rb"
cp "$TMP/same-line.rb" "$TMP/same-line-before.rb"
if bash "$ROOT/scripts/ci/rewrite_homebrew_cask_url.sh" \
  "$TMP/same-line.rb" 1.2.3 "$gui_asset" "$TMP/assets/$gui_asset" \
  >/dev/null 2>&1; then
  echo "expected same-line duplicate cask URL target to fail" >&2
  exit 1
fi
cmp -s "$TMP/same-line.rb" "$TMP/same-line-before.rb"

echo "Homebrew cached cask URL rewrite tests: ok"
