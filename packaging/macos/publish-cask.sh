#!/usr/bin/env bash
# Generate the Homebrew cask for the radioactive-ralph GUI .app and open a PR to
# jbcom/pkgs (the same cross-packager repo the formula + scoop bucket live in).
# The cask delivers the ad-hoc-signed .dmg; Homebrew strips com.apple.quarantine
# on install so Gatekeeper allows it without notarization — the OSS-app path.
#
#   publish-cask.sh <version>
#
# Both macOS release legs (arm64 + amd64) upload their arch-specific .dmg to the
# release before this runs; the cask carries on_arm/on_intel URLs+shas so each
# Mac architecture downloads the matching build.
#
# Requires: gh authenticated as a token with push access to jbcom/pkgs
# (JBCOM_PKGS_GITHUB_TOKEN, exported as GH_TOKEN).
set -euo pipefail

VERSION="${1:?usage: publish-cask.sh <version>}"
REPO="jbcom/radioactive-ralph"
PKGS="jbcom/pkgs"
TAG="v${VERSION}"
BRANCH="chore/update-radioactive-ralph-cask-${VERSION}"

WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT   # the clone carries a credentialed remote — always clean up

# Download both arch dmgs from the release and hash them (the SHAs the cask
# pins). The dmgs were uploaded by the gui-bundles macOS legs this cask job
# `needs`, so they exist by now.
declare -A SHA
for arch in arm64 amd64; do
  dmg="radioactive-ralph_${VERSION}_darwin_${arch}.dmg"
  gh release download "$TAG" --repo "$REPO" --pattern "$dmg" --dir "$WORK" --clobber
  SHA[$arch]="$(shasum -a 256 "$WORK/$dmg" | awk '{print $1}')"
done

# Keep the token out of the clone URL (it would leak in process listings / any
# stored remote). Feed it via an in-memory askpass helper instead.
ASKPASS="$WORK/askpass.sh"
printf '#!/bin/sh\nprintf "%%s" "%s"\n' "$GH_TOKEN" > "$ASKPASS"
chmod 0700 "$ASKPASS"
export GIT_ASKPASS="$ASKPASS" GIT_TERMINAL_PROMPT=0

git clone --depth 1 "https://x-access-token@github.com/${PKGS}.git" "$WORK/pkgs"
cd "$WORK/pkgs"
# Rerun-safe: if a prior failed attempt left this version branch behind, start
# from a clean local branch rather than failing on a name clash.
git checkout -B "$BRANCH"
mkdir -p Casks

cat > Casks/radioactive-ralph.rb <<CASK
cask "radioactive-ralph" do
  version "${VERSION}"

  on_arm do
    sha256 "${SHA[arm64]}"
    url "https://github.com/${REPO}/releases/download/v#{version}/radioactive-ralph_#{version}_darwin_arm64.dmg"
  end
  on_intel do
    sha256 "${SHA[amd64]}"
    url "https://github.com/${REPO}/releases/download/v#{version}/radioactive-ralph_#{version}_darwin_amd64.dmg"
  end

  name "radioactive-ralph"
  desc "Supervised-execution runtime for local AI-agent CLIs"
  homepage "https://github.com/${REPO}"

  app "radioactive-ralph.app"

  # The app is ad-hoc signed (free, no Apple Developer cert), so Gatekeeper
  # would quarantine it on first launch. Strip the quarantine attribute after
  # install so it opens cleanly — the standard OSS-cask approach for an
  # un-notarized app. (Homebrew does NOT remove quarantine by default.)
  postflight do
    system_command "/usr/bin/xattr",
                   args: ["-dr", "com.apple.quarantine", "#{appdir}/radioactive-ralph.app"],
                   sudo: false
  end

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
# Rerun-safe push: a re-run of the same version overwrites its own prior branch.
git push --force-with-lease origin "$BRANCH"

# Open a PR; jbcom/pkgs' automerge workflow takes it from there (same flow the
# goreleaser formula/scoop PRs use). A re-run where the PR already exists is a
# benign no-op rather than a failure.
if ! gh pr create --repo "$PKGS" --base main --head "$BRANCH" \
  --title "chore: update radioactive-ralph cask to ${VERSION}" \
  --body "Automated cask update for radioactive-ralph ${VERSION} (ad-hoc-signed .app; arm64 + amd64 dmgs)." 2>err.log; then
  if grep -qi "already exists" err.log; then
    echo "publish-cask: PR already open for ${BRANCH} — nothing to do"
  else
    cat err.log >&2
    exit 1
  fi
fi
echo "publish-cask: cask published for ${VERSION}"
