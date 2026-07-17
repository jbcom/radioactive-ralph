# Packaging

Native packaging assets and notes. See the design spec at
`docs/superpowers/specs/2026-07-17-native-packaging-design.md`.

## Layout

- `linux/radioactive-ralph.desktop` — the freedesktop launcher entry for the
  GUI (`Exec=radioactive_ralph gui`). Shipped in the AppImage and the `.deb`/
  `.rpm` GUI packages; validated in CI with `desktop-file-validate`.

## What is built where

| Format | Tool | Runner | Signing |
|---|---|---|---|
| `.tar.gz`/`.zip` (CLI) | goreleaser archives | ubuntu | cosign (checksums) |
| `.deb`/`.rpm` (CLI) | goreleaser nfpms | ubuntu | cosign (checksums) |
| Homebrew / Scoop / Chocolatey / winget (CLI) | goreleaser publishers | ubuntu / windows | — (manifest) |
| AppImage + `.desktop` (GUI) | `fyne package` + `appimagetool` (pinned+SHA-verified) | ubuntu (`-tags gui`, CGO) | unsigned by convention; per-bundle `.sha256` sidecar |
| `.app` Homebrew cask (GUI) | `fyne package` + `codesign -s -` | macos (`-tags gui`, CGO) | ad-hoc (free); cask `postflight` strips quarantine — no Apple account |
| `.exe` (GUI) | `fyne package` | windows (`-tags gui`, CGO) | optional SignPath OSS signing when the `SIGNPATH_*` secret is set (else unsigned) |

(The `.deb`/`.rpm` rows above are CLI-only — there is no GUI deb/rpm build; the
GUI Linux delivery is the AppImage.)

## Icon

The app icon derives from `assets/brand/ralph-mascot.png` (1402×1122). The
per-OS packaging step squares/resizes it to the format each platform wants
(`.icns` for macOS, `.ico` for Windows, a 512×512 PNG for Linux) — the source
brand asset is not committed pre-squared so there is one source of truth.

## Signing — the OSS way (free, no purchase)

Open source does not pay for code signing. Neither Apple nor Microsoft charges
for the path we use:

- **macOS** — the `.app` is **ad-hoc signed** (`codesign --sign -`, free) and
  shipped as a **Homebrew cask**. Homebrew strips `com.apple.quarantine` on
  install, so Gatekeeper allows it without notarization. No Apple Developer
  Program membership. The direct-download `.dmg` is best-effort (it will show a
  Gatekeeper prompt); the cask is the blessed install.
- **Windows** — Authenticode signing is free through the
  [SignPath Foundation](https://signpath.io/solutions/open-source-community) OSS
  program (radioactive-ralph is MIT-licensed + public → qualifies). The only
  user action is a **one-time signup** and adding a `SIGNPATH_*` repo secret —
  not a purchase. Until the secret exists the MSI ships unsigned; the signing
  step is gated on `secrets.SIGNPATH_* != ''` (same pattern as the Chocolatey
  job), so it turns on automatically once the token is added.

Everything else (deb/rpm/AppImage checksums, all CLI package managers) is
already automatic via the existing cosign-keyless + token flow.
