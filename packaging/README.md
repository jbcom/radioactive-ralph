# Packaging

Native packaging assets and notes. See the design spec at
`docs/superpowers/specs/2026-07-17-native-packaging-design.md`.

## Layout

- `linux/radioactive-ralph.desktop` — the freedesktop launcher entry for the
  GUI (`Exec=radioactive_ralph gui`). Shipped in the AppImage and validated in
  CI with `desktop-file-validate`; `.deb`/`.rpm` packages remain CLI-only.
- `wsl/` — the Dockerfile, `wsl.conf`, and `build-rootfs.sh` that produce
  `rootfs.tar.gz` for the bundled `radioactive-ralph` WSL2 distro (see
  `docs/superpowers/specs/2026-08-20-windows-wsl2-dispatch-design.md`). Built
  once per release, not on end-user machines — Docker is a release-pipeline
  dependency here, never an install-time one.

## What is built where

| Format | Tool | Runner | Signing |
|---|---|---|---|
| `.tar.gz`/`.zip` (CLI) | goreleaser archives | ubuntu | cosign (checksums) |
| `.deb`/`.rpm` (CLI) | goreleaser nfpms | ubuntu | cosign (checksums) |
| Homebrew cask `radioactive-ralph` / Scoop (CLI; native Windows is foreground supervisor/client control plane only) | goreleaser publishers | ubuntu | — (manifest) |
| winget manifests (generated only; not staged or submitted; native Windows is foreground supervisor/client control plane only) | goreleaser generation | ubuntu | — (manifest) |
| Chocolatey package (optional, only after immutable GitHub publication and outside the stable install-surface gate; native Windows is foreground supervisor/client control plane only) | goreleaser publisher | windows | — (manifest) |
| AppImage + `.desktop` (GUI) | `fyne package` v1.7.2 + `appimagetool` (pinned+SHA-verified) | ubuntu (`-tags gui`, CGO) | unsigned by convention; covered by signed consolidated `gui-checksums.txt` |
| `.app` Homebrew cask `radioactive-ralph-gui` (GUI) | `fyne package` v1.7.2 + `codesign -s -` | macos (`-tags gui`, CGO) | ad-hoc (free); cask `postflight` strips quarantine — no Apple account |
| `.exe` (GUI) | `fyne package` v1.7.2 | windows (`-tags gui`, CGO) | optional SignPath OSS signing when the `SIGNPATH_*` secret is set (else unsigned) |

(The `.deb`/`.rpm` rows above are CLI-only — there is no GUI deb/rpm build; the
GUI Linux delivery is the AppImage.)

Native Windows packages expose only the foreground supervisor/client control
plane; SCM install/start stays disabled (see the Windows SCM safety design
spec — unrelated to and unchanged by the point below). Provider-backed
dispatch on Windows now routes through a bundled, auto-provisioned
`radioactive-ralph` WSL2 distro (`wsl/`, per the WSL2 dispatch design spec) —
built and imported automatically, not the manual "set up WSL2 + `systemd
--user` yourself" story this table previously pointed at.

## Icon

The app icon derives from `assets/brand/ralph-mascot.png` (1402×1122). The
per-OS packaging step squares/resizes it to the format each platform wants
(`.icns` for macOS, `.ico` for Windows, a 512×512 PNG for Linux) — the source
brand asset is not committed pre-squared so there is one source of truth.

## Signing — the OSS way (free, no purchase)

Open source does not pay for code signing. Neither Apple nor Microsoft charges
for the path we use:

- **macOS** — the `.app` is **ad-hoc signed** (`codesign --sign -`, free) and
  shipped as a **Homebrew cask**. The custom GUI cask's `postflight` explicitly
  strips `com.apple.quarantine` after install, so Gatekeeper allows it without
  notarization; Homebrew does not do this by default. No Apple Developer
  Program membership. The direct-download `.dmg` is best-effort (it will show
  a Gatekeeper prompt); the cask is the blessed install.
- **Windows** — Authenticode signing is free through the
  [SignPath Foundation](https://signpath.io/solutions/open-source-community) OSS
  program (radioactive-ralph is MIT-licensed + public → qualifies). The only
  user action is a **one-time signup** and adding a `SIGNPATH_*` repo secret —
  not a purchase. Until the secret exists the `.exe` ships unsigned; the signing
  step is gated on `secrets.SIGNPATH_* != ''` (same pattern as the Chocolatey
  job), so it turns on automatically once the token is added.

The GoReleaser CLI archives and deb/rpm packages are covered by the keyless
Sigstore signature on `checksums.txt`. All GUI bundles share one consolidated
`gui-checksums.txt` and workflow-identity Sigstore bundle. Homebrew and Scoop manifests are
schema- and byte-checked against those verified release assets. winget is only
generated in `dist/`, and Chocolatey remains an optional publisher that runs
only after immutable GitHub publication, outside the stable install-surface
gate.
