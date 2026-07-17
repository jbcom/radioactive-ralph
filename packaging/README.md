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
| AppImage + `.desktop` (GUI) | `fyne package` + `appimagetool` | ubuntu (`-tags gui`, CGO) | unsigned by convention; verified by release checksum |
| `.app`/`.dmg` (GUI) | `fyne package` + `create-dmg` | macos (`-tags gui`, CGO) | **needs Apple Developer cert — see Credential gates** |
| `.exe`/MSI (GUI) | `fyne package` + `wix` | windows (`-tags gui`, CGO) | **needs Authenticode cert — see Credential gates** |

## Icon

The app icon derives from `assets/brand/ralph-mascot.png` (1402×1122). The
per-OS packaging step squares/resizes it to the format each platform wants
(`.icns` for macOS, `.ico` for Windows, a 512×512 PNG for Linux) — the source
brand asset is not committed pre-squared so there is one source of truth.

## Credential gates (Scope B — user-only)

Signing the macOS and Windows GUI installers cannot be done without credentials
only the repository owner can provide:

- **macOS notarization** — an Apple Developer Program membership + a Developer ID
  Application certificate + a notarytool credential (app-specific password or API
  key). Until configured, the `.dmg` ships unsigned (Gatekeeper will warn).
- **Windows Authenticode** — a code-signing certificate (from a CA or Azure
  Trusted Signing). Until configured, the MSI ships unsigned (SmartScreen will
  warn).

The release pipeline builds these artifacts unsigned regardless; the signing
steps are guarded on the corresponding secret being present (the same
`secrets.X != ''` gate the Chocolatey job uses), so they switch on the moment
the credential is added — no pipeline change needed.
