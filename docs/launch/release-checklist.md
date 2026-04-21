---
title: Release checklist
lastUpdated: 2026-04-16
---

# Release Checklist

Execute top-to-bottom. Every step has a concrete verification — no
"looks good" signoffs.

## 1. Pre-tag verification

- [ ] `main` is green — all required CI checks passing on the latest
      commit
- [ ] `git status` on a clean checkout is empty
- [ ] `go test ./...` passes locally
- [ ] `go vet ./...` is clean
- [ ] `golangci-lint run` is clean
- [ ] `bash scripts/validate-docs.sh` passes
- [ ] `python3 -m tox -e docs` builds the Sphinx site cleanly, without
      unexpected warnings
- [ ] No `CHANGELOG.md` entries for the upcoming version left as `[TBD]`
- [ ] No open `P0` issues in the milestone

## 2. GoReleaser dry-run

Run a local snapshot and inspect every artifact before tagging.

```sh
goreleaser release --snapshot --clean --skip=sign,publish
```

Expected outputs under `dist/`:

- [ ] `radioactive_ralph_<ver>_darwin_amd64.tar.gz`
- [ ] `radioactive_ralph_<ver>_darwin_arm64.tar.gz`
- [ ] `radioactive_ralph_<ver>_linux_amd64.tar.gz`
- [ ] `radioactive_ralph_<ver>_linux_arm64.tar.gz`
- [ ] `radioactive_ralph_<ver>_windows_amd64.zip`
- [ ] `checksums.txt` with SHA-256 for each archive
- [ ] `homebrew/Formula/radioactive-ralph.rb` (class `RadioactiveRalph`)
- [ ] `scoop/bucket/radioactive-ralph.json`

Smoke the macOS-arm64 binary locally:

```sh
./dist/radioactive_ralph_darwin_arm64_v8.0/radioactive_ralph --version
./dist/radioactive_ralph_darwin_arm64_v8.0/radioactive_ralph --help
```

- [ ] `--version` prints `<ver> (<commit>, built <iso-timestamp>)`
- [ ] `--help` lists: `init`, `run`, `status`, `attach`, `stop`,
      `doctor`, `service`, `plan`, `tui`

## 3. Docs ↔ artifacts parity

Every documented install command must match a real artifact. This is
what `docs/getting-started/index.md` and the root `README.md` promise;
confirm each one.

- [ ] `brew tap jbcom/pkgs https://github.com/jbcom/pkgs && brew install radioactive-ralph` —
      verified against `dist/homebrew/Formula/radioactive-ralph.rb`
      (formula class `RadioactiveRalph`, install name
      `radioactive-ralph`, binary installed as `radioactive_ralph`).
      The explicit URL form is required because the repo is
      named `pkgs`, not `homebrew-pkgs`.
- [ ] `scoop bucket add jbcom https://github.com/jbcom/pkgs && scoop install radioactive-ralph` —
      verified against `dist/scoop/bucket/radioactive-ralph.json`
- [ ] `curl -sSL https://jonbogaty.com/radioactive-ralph/install.sh | sh` —
      verified against `site/public/install.sh`: `BIN` matches binary
      name, `ARCHIVE` template matches GoReleaser naming
- [ ] Public install docs expose only the stable install surface:
      Homebrew, Scoop, and the curl installer.

## 4. Tag + push

```sh
git tag v<MAJ>.<MIN>.<PATCH>
git push origin v<MAJ>.<MIN>.<PATCH>
```

- [ ] Tag pushed to origin
- [ ] `Release` workflow triggered on the tag
- [ ] Optional Chocolatey job is either disabled or recorded separately
      from the stable install-surface gate

## 5. Post-tag hosted verification

The hosted release workflow runs `goreleaser release --clean` on
ubuntu-latest and (conditionally) `goreleaser release --clean
--config .goreleaser.chocolatey.yaml` on windows-latest.

- [ ] ubuntu-latest release job green
- [ ] GitHub release created with all 5 archives + `checksums.txt`
      attached
- [ ] Homebrew formula landed at
      <https://github.com/jbcom/pkgs/blob/main/Formula/radioactive-ralph.rb>
- [ ] Scoop manifest landed at
      <https://github.com/jbcom/pkgs/blob/main/bucket/radioactive-ralph.json>
- [ ] If `vars.ENABLE_CHOCOLATEY=true` and `secrets.CHOCOLATEY_API_KEY`
      set: nupkg published to <https://community.chocolatey.org/packages/radioactive-ralph>

## 5a. Manual workflow dispatches (PRD § 4.2 native-host validation)

Two workflow-dispatch jobs must be run manually — they require live
secrets and real host runners that we don't want firing on every
commit. Run both before calling a release validated.

### Service managers — launchd / systemd-user / SCM

```sh
gh workflow run service-managers.yml --ref v<MAJ>.<MIN>.<PATCH>
```

- [ ] macOS launchd job green (registers + starts + stops a plist
      under `~/Library/LaunchAgents/`)
- [ ] Linux systemd-user job green (registers + starts + stops a
      unit under `~/.config/systemd/user/`)
- [ ] Windows SCM job green (registers + starts + stops an SCM
      service, elevated)

Scripts at `scripts/ci/smoke_{launchd,systemd_user}.sh` and
`scripts/ci/smoke_windows_scm.ps1`. These are the same shell loops an
operator would run by hand, so a green job here matches real-host
behavior.

### Live provider smoke

Requires all shipped-provider secrets at repo level:
`ANTHROPIC_API_KEY`, `OPENAI_API_KEY`, and either `GEMINI_API_KEY` or
`GOOGLE_API_KEY`. For Codex, the workflow converts `OPENAI_API_KEY`
into a headless `codex login` before enabling the live smoke.

```sh
gh workflow run provider-live.yml --ref v<MAJ>.<MIN>.<PATCH>
```

- [ ] Claude round-trip + model-sanity + runner-turn tests pass
- [ ] Codex runner-turn passes; the headless `codex login` preflight
      must succeed
- [ ] Gemini runner-turn passes

Provider skips are acceptable for local development, but not for the
stable-release gate.

## 6. Install-path smoke

Perform at least two of the following from a clean shell / machine.

### Homebrew (macOS / Linux)

```sh
brew untap jbcom/pkgs 2>/dev/null
brew tap jbcom/pkgs https://github.com/jbcom/pkgs
brew install radioactive-ralph
radioactive_ralph --version
```

- [ ] Install succeeds
- [ ] Version reported matches the tag
- [ ] `brew info radioactive-ralph` shows the caveat text with the
      post-install instructions

### curl installer (macOS / Linux)

```sh
mkdir -p ~/tmp/install-smoke && cd ~/tmp/install-smoke
curl -sSL https://jonbogaty.com/radioactive-ralph/install.sh | sh -s -- --install-dir "$PWD"
./radioactive_ralph --version
```

- [ ] Installer downloads correct archive
- [ ] Checksum verification passes
- [ ] Binary works

### Scoop (Windows)

```powershell
scoop bucket add jbcom https://github.com/jbcom/pkgs
scoop install radioactive-ralph
radioactive_ralph --version
```

- [ ] Install succeeds
- [ ] post_install echo lines are visible
- [ ] Version reported matches the tag

## 7. Post-install operator-flow smoke

Against the freshly-installed binary in a clean tmp repo:

```sh
mkdir -p ~/tmp/ralph-smoke && cd ~/tmp/ralph-smoke && git init -q
radioactive_ralph init --yes
radioactive_ralph doctor
radioactive_ralph service start &
sleep 2
radioactive_ralph status --json
radioactive_ralph stop
```

- [ ] `init` scaffolds `.radioactive-ralph/` + `plans/index.md`
- [ ] `doctor` reports OK on git, provider CLI, service-manager
- [ ] `service start` spins up without IPC errors (Unix socket or
      Windows named pipe, as platform dictates)
- [ ] `status --json` prints a well-formed record with `repo_path`
- [ ] `stop` shuts the service down cleanly

## 8. Release notes

- [ ] `CHANGELOG.md` has a section for the new version
- [ ] GitHub release body includes the changelog section verbatim
- [ ] Release marked `Latest`

## 9. Rollback plan (if step 6 or 7 fails)

If a post-tag smoke fails and the bug is in the release artifact:

1. Mark the GitHub release as a **pre-release** to hide it from
   consumers that follow "latest"
2. Revert the Homebrew formula:
   ```sh
   gh api -X DELETE repos/jbcom/pkgs/contents/Formula/radioactive-ralph.rb \
     -f message="rollback v<ver>" -f sha=<prev-sha>
   ```
3. Revert the Scoop manifest the same way
4. Open a follow-up patch release; do NOT delete the tag (keeps the
   git history honest)

Do NOT `git tag -d` a pushed tag. If the tag itself needs to move,
open a new patch release and deprecate the bad one in the release
notes.

## 10. Tooling warning

- The `brews:` block in `.goreleaser.yaml` is deprecated in GoReleaser
  v2 in favor of `homebrew_casks`. `goreleaser check` must still pass
  and the generated formula must still match the documented Homebrew
  install path. If the installed GoReleaser version stops accepting
  `brews:`, migrate before tagging.
