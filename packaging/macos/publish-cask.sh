#!/usr/bin/env bash
# Generate the Homebrew cask for the radioactive-ralph GUI .app and open a PR to
# jbcom/pkgs (the same cross-packager repo the formula + scoop bucket live in).
# The cask delivers the ad-hoc-signed .dmg; Homebrew strips com.apple.quarantine
# on install so Gatekeeper allows it without notarization — the OSS-app path.
#
#   publish-cask.sh <version> <dmg-path>
#
# Requires: gh authenticated as a token with push access to jbcom/pkgs
# (JBCOM_PKGS_GITHUB_TOKEN, exported as GH_TOKEN).
set -euo pipefail

VERSION="${1:?usage: publish-cask.sh <version> <dmg-path>}"
DMG="${2:?usage: publish-cask.sh <version> <dmg-path>}"
REPO="jbcom/radioactive-ralph"
PKGS="jbcom/pkgs"
BRANCH="chore/update-radioactive-ralph-cask-${VERSION}"

if [ ! -f "$DMG" ]; then
  echo "publish-cask: dmg not found: $DMG" >&2
  exit 1
fi

SHA="$(shasum -a 256 "$DMG" | awk '{print $1}')"

WORK="$(mktemp -d)"
git clone --depth 1 "https://x-access-token:${GH_TOKEN}@github.com/${PKGS}.git" "$WORK/pkgs"
cd "$WORK/pkgs"
git checkout -b "$BRANCH"
mkdir -p Casks

cat > Casks/radioactive-ralph.rb <<CASK
cask "radioactive-ralph" do
  version "${VERSION}"
  sha256 "${SHA}"

  url "https://github.com/${REPO}/releases/download/v#{version}/radioactive-ralph_#{version}_darwin.dmg"
  name "radioactive-ralph"
  desc "Supervised-execution runtime for local AI-agent CLIs"
  homepage "https://github.com/${REPO}"

  app "radioactive-ralph.app"

  caveats <<~EOS
    Start the supervisor and register a project, then launch the app:

      radioactive_ralph service install
      cd /path/to/repo && radioactive_ralph --init

    The desktop app and the terminal UI are peers on the same local supervisor.
  EOS
end
CASK

git add Casks/radioactive-ralph.rb
git -c user.name=jbcom-bot -c user.email=noreply@jonbogaty.com \
  commit -m "chore: update radioactive-ralph cask to ${VERSION}"
git push origin "$BRANCH"

# Open a PR; jbcom/pkgs' automerge workflow takes it from there (same flow the
# goreleaser formula/scoop PRs use).
gh pr create --repo "$PKGS" --base main --head "$BRANCH" \
  --title "chore: update radioactive-ralph cask to ${VERSION}" \
  --body "Automated cask update for radioactive-ralph ${VERSION} (ad-hoc-signed .app via .dmg)."
echo "publish-cask: opened cask PR for ${VERSION}"
