# Native Installers & Packaging — Design

**Date:** 2026-07-17
**Status:** design (autonomous authoring under the desktop-app mandate).
**Revision (2026-07-17):** the earlier "Scope B is blocked on paid Apple/MS
credentials" framing was WRONG. OSS projects sign for free: macOS ships as a
**Homebrew cask** (ad-hoc-signed `.app`, quarantine stripped on install — no
Apple Developer account), and Windows Authenticode is free via the **SignPath
Foundation** OSS program. No purchase from Apple or Microsoft is required. See
"Signing — the OSS way".
**Related:** [Fyne GUI client design](2026-07-17-fyne-gui-client-design.md),
`.goreleaser.yaml`, `.github/workflows/release.yml`.

## Goal

Ship radioactive-ralph as real, installable software on every platform — both
the CLI (already largely done) and the GUI desktop app (new) — with the widest
set of native package formats each ecosystem expects, verifiable by signature.

## What already exists (baseline on main)

- `.goreleaser.yaml`: CGO-off CLI binaries for 6 GOOS/GOARCH, `.tar.gz`/`.zip`
  archives, `checksums.txt` **cosign-signed** (keyless), Homebrew + Scoop
  manifests published to `jbcom/pkgs` via PR.
- `.goreleaser.chocolatey.yaml`: Chocolatey publish from a Windows runner.
- `site/public/install.sh`: curl-pipe installer — resolves latest, downloads the
  arch archive, verifies its SHA-256 against `checksums.txt`, extracts the CLI.
- `release.yml`: one ubuntu goreleaser job + one windows chocolatey job.
- Icon source: `assets/brand/ralph-mascot.png`.

## The central design fork: how to build the GUI-enabled binaries

The CLI ships CGO-off; the GUI needs `-tags gui` **with CGO and the platform GL
stack**. That is the whole difficulty of this item. Three options considered:

1. **goreleaser with per-platform CGO builds + native runners.** Add a second
   goreleaser `build` (id `radioactive_ralph_gui`, `-tags gui`, `CGO_ENABLED=1`)
   and run goreleaser on a **matrix of native runners** (macos for darwin, ubuntu
   for linux, windows for windows) so each GUI binary links against its own OS
   toolchain — no cross-CGO. Downsides: goreleaser's single-run model fights a
   per-OS matrix; you split into per-OS configs and merge.
2. **`fyne-cross` (Docker-based cross-compile).** Fyne's official cross tool
   builds `.app`/`.dmg`, `.exe`+installer, and Linux tarballs from one Linux
   host via Docker images carrying each toolchain. Clean for GUI bundles, but
   it's a *second* release tool bolted alongside goreleaser, and macOS
   notarization still can't happen inside Linux Docker.
3. **Split by concern (RECOMMENDED).** Keep **goreleaser** as the CLI + Linux
   packaging authority (it already is), and add GUI-app bundling per-OS on
   native runners using each platform's idiomatic tool:
   - macOS: `fyne package` on a macos runner → `.app`, wrapped to `.dmg`.
   - Windows: `fyne package` on a windows runner → `.exe`; MSI via `wix`.
   - Linux: `fyne package` → tarball, repacked as AppImage; ship `.desktop`.

   Rationale: no cross-CGO, each OS uses its native toolchain, goreleaser stays
   the CLI/signing/manifest authority, and `fyne package` is the tool that
   actually knows how to bundle a Fyne app per platform. The GUI bundles are
   uploaded as additional release assets alongside goreleaser's CLI archives.

**Decision:** Option 3. Recorded in decisions.ndjson.

## Scope A — credential-free, do now

These need **no new credentials** (reuse the existing cosign keyless + tokens):

1. **goreleaser `nfpms`** → `.deb` + `.rpm` for the CLI, for linux amd64/arm64.
   Straight goreleaser config addition; artifacts cosign-signed like the
   archives. Includes a `radioactive_ralph` bin in `/usr/bin`.
2. **winget manifest** → goreleaser's `winget` publisher, PR'd to a fork of
   `microsoft/winget-pkgs` (or staged in `jbcom/pkgs` for manual submission).
   Uses the existing `JBCOM_PKGS_GITHUB_TOKEN`.
3. **Linux `.desktop` + AppImage** for the GUI. The `.desktop` file
   (`radioactive-ralph.desktop`, Exec=`radioactive_ralph gui`, the mascot icon)
   is committed; the AppImage is built on the ubuntu GUI runner from the
   `-tags gui` binary via `appimagetool`. **AppImages are unsigned by
   convention** (verified by the release checksum instead), so no cert needed.
4. **install.sh audit** — confirm it verifies against the cosign-signed
   `checksums.txt` end-to-end, and extend it to optionally verify the cosign
   signature+certificate (not just the SHA) when `cosign` is present. Document
   the GUI install path (the app bundles are not curl-pipe installable; point at
   the native installers).

## Scope B — GUI app bundles (no purchase required)

The GUI desktop bundles. Signing is handled the OSS way (next section), so none
of this needs a paid Apple/Microsoft account:

1. **macOS `.app` via a Homebrew cask.** `fyne package` on a macos runner
   produces `radioactive-ralph.app`, ad-hoc-signed (`codesign -s -`, free). It
   is delivered as a **Homebrew cask** (a `casks/` entry in `jbcom/pkgs`, beside
   the existing formula): `brew install --cask radioactive-ralph`. Homebrew
   strips the `com.apple.quarantine` xattr on install, so Gatekeeper does not
   block it despite the absence of notarization — the standard OSS-app path. A
   `.dmg` is also produced as a release asset for direct download (that path
   shows a Gatekeeper prompt; the cask is the recommended install).
2. **Windows `.exe`/MSI.** `fyne package` + `wix` on a windows runner. Signed
   with the **SignPath Foundation** OSS certificate (next section) so SmartScreen
   is clean.
3. **Linux AppImage** (from Scope A) is the GUI delivery; AppImages are unsigned
   by convention and verified by the release checksum.

The signing steps are guarded on the corresponding secret being present (the
same `secrets.X != ''` gate release.yml uses for `CHOCOLATEY_API_KEY`), so the
pipeline builds bundles unsigned until signing is wired, then signs
automatically once the secret lands.

## Signing — the OSS way (free, no purchase)

**Open source does not pay for code signing.**

- **macOS:** an ad-hoc signature (`codesign --sign -`) is free and sufficient
  *when delivered through Homebrew*, because the cask install removes the
  quarantine attribute. No Apple Developer Program membership, no Developer ID
  cert, no notarization for the cask path. (Notarization would only matter for a
  double-click-from-browser `.dmg`; the cask is the blessed install and the
  `.dmg` is best-effort.)
- **Windows:** the [SignPath Foundation](https://signpath.io/solutions/open-source-community)
  gives **free OV Authenticode signing** to qualifying OSS projects
  (radioactive-ralph is MIT-licensed and public — it qualifies). SignPath signs
  through a managed pipeline triggered from the release workflow; the publisher
  shown is "SignPath Foundation". Clears SmartScreen at zero cost.

The one genuine user action is a one-time **signup** (not a purchase): enroll the
repo in SignPath's OSS program, then add the `SIGNPATH_*` token as a repo secret.
Until that token exists the Windows bundle ships unsigned; the signing step is
gated on the secret, so it turns on the moment the token is added.

## Testing / verification

- `goreleaser release --snapshot --clean` locally produces the CLI archives +
  the new `.deb`/`.rpm` without publishing — assert the nfpm artifacts exist and
  install cleanly in a throwaway container (`dpkg -i` / `rpm -i` → `radioactive_ralph --version`).
- The `-tags gui` binary + AppImage build on the ubuntu GUI runner; smoke-run
  `--version` (the GUI itself needs a display, but `--version` proves the bundle
  links).
- `.desktop` validated with `desktop-file-validate`.
- install.sh: shellcheck clean; a dry-run against a snapshot checksums file.

## Out of scope

- Auto-update / Sparkle / MSIX auto-update channels.
- Flatpak / Snap (AppImage covers portable Linux; revisit if requested).
- The GUI app's own in-app "check for updates".

## Summary for the user

Nothing here requires paying Apple or Microsoft:

- **macOS** ships as a **Homebrew cask** (ad-hoc-signed `.app`, quarantine
  stripped on install) — no Apple Developer account.
- **Windows** signs via the **free SignPath Foundation OSS program** — no
  purchased cert. The only user action is a one-time signup + adding a
  `SIGNPATH_*` repo secret; the pipeline signs automatically once it exists and
  ships unsigned until then.
- **Linux** (deb/rpm/AppImage) and all the CLI package managers are fully
  automatic via the existing cosign-keyless + token flow.

So the whole packaging item can be built and shipped autonomously; the SignPath
enrollment is the single optional signup that upgrades Windows from
"SmartScreen-warns" to "clean", and can be added any time.
