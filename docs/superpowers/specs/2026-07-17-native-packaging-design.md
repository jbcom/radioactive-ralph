# Native Installers & Packaging — Design

**Date:** 2026-07-17
**Status:** design (autonomous authoring under the desktop-app mandate). Two
sub-scopes; the second is gated on user-only credentials — see "Credential
gates".
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

## Scope B — gated on USER-ONLY credentials

These cannot be completed autonomously; each needs something only the user can
provide. They are **built unsigned** in Scope A's runners so the pipeline is
ready, and signing is switched on when the credential lands:

1. **macOS notarized `.app`/`.dmg`.** Notarization requires an **Apple Developer
   account** ($99/yr) + a Developer ID Application certificate + an app-specific
   password / notarytool API key. *(Blocker: purchase + interactive Apple
   credential — user only.)* Until then, ship the unsigned `.dmg` with a
   documented Gatekeeper-bypass note.
2. **Windows MSI/MSIX Authenticode-signed.** Signing needs a **code-signing
   certificate** (purchased from a CA, or an Azure Trusted Signing account).
   *(Blocker: purchase — user only.)* Until then, ship the unsigned MSI; SmartScreen
   will warn.

The pipeline produces the artifacts either way; the signing steps are guarded on
the secret being present (the same `secrets.X != ''` gate pattern release.yml
already uses for `CHOCOLATEY_API_KEY`), so nothing breaks when the cert is absent
and everything signs the moment it's added.

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

## Credential gates — summary for the user

To finish Scope B, the user needs to provide (each is a genuine block per the
autonomy rules — purchase / interactive credential):

- **Apple:** Developer Program membership + Developer ID cert + notarytool
  credential → enables notarized `.app`/`.dmg`.
- **Windows:** an Authenticode code-signing cert (or Azure Trusted Signing) →
  enables signed MSI/MSIX.

Everything in Scope A ships without them.
