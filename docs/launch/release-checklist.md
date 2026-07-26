---
title: Release checklist
lastUpdated: 2026-07-25
---

# Release checklist

Execute top-to-bottom. A stable release is one immutable transaction, not a
draft-to-prerelease promotion.

## 1. Pre-tag controls

- [ ] `main` is clean and `go test ./...`, `go vet ./...`,
      `golangci-lint run`, `bash scripts/validate-docs.sh`, and
      `python3 -m tox -e docs` pass.
- [ ] `RELEASE_PLEASE_GITHUB_TOKEN` is provisioned only for Release Please in
      this repository.
- [ ] `PKGS_GITHUB_TOKEN` can read, create PRs in, and squash-merge checked
      heads in `jbcom/pkgs`; it is not used for this repository's releases.
- [ ] Repository immutable releases are enabled.
- [ ] `radioactive-ralph` main protection is strict, applies to administrators,
      requires PRs, linear history, conversation resolution, and all 25
      GitHub-Actions-app-ID-`15368` contexts:
      `Test (ubuntu-latest)`, `Test (macos-latest)`,
      `Test (windows-latest)`, `E2E (CI-feasible)`,
      `GUI (ubuntu-latest)`, `GUI (macos-latest)`,
      `Build (linux/amd64)`, `Build (linux/arm64)`,
      `Build (darwin/amd64)`, `Build (darwin/arm64)`,
      `Build (windows/amd64)`, `Build (windows/arm64)`, `Lint`,
      `Workflow lint`, `Vulnerability scan`, `Docs`, `Packaging lint`,
      `Package artifacts`, `Package artifacts (arm64)`,
      `Package GUI (ubuntu-latest)`,
      `Package GUI (macos-latest)`, `Package GUI (macos-15-intel)`,
      `Package GUI (windows-latest)`, `Analyze (actions)`, and
      `Analyze (javascript-typescript)`.
- [ ] `jbcom/pkgs` main requires protected `validate` and `build-site` checks
      from GitHub Actions app ID `15368`.

## 2. Snapshot proof

```sh
goreleaser release --snapshot --clean --skip=sign,publish
```

The snapshot build itself is portable. The native archive/deb/rpm/installer
smoke is Linux-only and must run on both Ubuntu x86_64 and Ubuntu arm64:

```sh
bash scripts/ci/smoke_goreleaser_artifacts.sh
```

On macOS or Windows the smoke helper fails immediately with a clear platform
error; use the two native `Package artifacts` CI jobs for that proof.

- [ ] Five CLI archives, amd64/arm64 `.deb` and `.rpm`, `checksums.txt`,
      Homebrew CLI cask, and Scoop manifest are exact.
- [ ] The four native `Package GUI` contexts produce and execute both macOS
      DMGs, the Linux AppImage, and Windows EXE.
- [ ] Actual amd64 and arm64 deb/rpm clean-install and execution proof exists.
- [ ] `radioactive_ralph --version` and `--help` match the intended version and
      CLI surface.

## 3. Stable install surface

- [ ] Homebrew CLI:
      `brew tap jbcom/pkgs https://github.com/jbcom/pkgs &&
      brew install --cask radioactive-ralph`.
- [ ] Homebrew GUI on both Apple Silicon and Intel:
      `brew install --cask radioactive-ralph-gui`.
- [ ] Scoop:
      `scoop bucket add jbcom https://github.com/jbcom/pkgs &&
      scoop install radioactive-ralph`.
- [ ] Debian/Ubuntu:
      `sudo apt install ./radioactive-ralph_<version>_linux_<arch>.deb`.
- [ ] Fedora/RHEL:
      `sudo dnf install ./radioactive-ralph_<version>_linux_<arch>.rpm`.
- [ ] The curl installer and AppImage match their signed release manifests.
- [ ] winget remains generated-only. Chocolatey remains optional and can run
      only after the immutable GitHub release has published successfully; it
      is not part of the stable gate.

## 4. Release Please handoff

Do not create or move a release tag manually.

- [ ] Ruleset **Release tags are admin-created** (ID `19751997`) is active,
      targets `tag`, includes `refs/tags/v*`, has the creation rule, and grants
      only `OrganizationAdmin` an `always` bypass.
- [ ] Ruleset **Release tags cannot move or be deleted** (ID `19752322`) is
      active, targets `tag`, includes `refs/tags/v*`, has update and deletion
      rules (the update and deletion rules), and has no bypass actors.
- [ ] Merging the Release Please PR creates the forced stable tag and one
      non-prerelease draft.
- [ ] `release-admission` binds tag, event SHA, draft target, manifest version,
      `origin/main`, the dedicated package secret, and the live immutable-release
      repository setting before any publisher runs.
- [ ] A public prerelease is rejected. There is no public staging state.

## 5. Draft rendezvous and seal

All prepublication verification uses authenticated draft downloads. Package
install steps receive no GitHub token: a preceding fetch step caches the exact
PR-head manifests and draft assets, then credentials are unset before Homebrew
or Scoop executes package content.

- [ ] GoReleaser uploads nine CLI/native-package deliverables plus the signed
      consolidated `checksums.txt`.
- [ ] Four GUI jobs upload four deliverables; one signer uploads the consolidated
      `gui-checksums.txt` and its Sigstore bundle.
- [ ] `package-rollback.tar.gz` contains the exact original three package files,
      their hashes, and original package-main OID; its workflow-identity
      signature verifies. A rerun reuses it and never resets “prior” to a bad
      already-merged main.
- [ ] `package-manifests.tar.gz` and its workflow-identity signature preserve
      the exact generated CLI cask, derived GUI cask, and generated Scoop bytes.
      Initial publication and sealed reruns both consume this archive; neither
      reconstructs an approximation from transient Actions artifacts.
- [ ] `release-seal.json` is created last and signs source/tag/tool pins plus the
      name, size, and SHA-256 of every other immutable asset.
- [ ] The exact immutable asset set is 23 assets: 13 deliverables, two checksum
      manifests and bundles, rollback provenance and bundle, exact package
      manifests and bundle, and release seal and bundle. The seal inventories
      all 21 assets that precede it.
- [ ] Admission distinguishes `draft-unsealed` from `draft-sealed`. A sealed
      rerun skips every clobbering build/uploader, verifies the seal signature
      and every byte before package mutation, then resumes from durable GitHub
      state. Partial or inconsistent seal state is quarantined.
- [ ] Verify each signature with:

  ```sh
  cosign verify-blob <manifest> \
    --bundle <manifest>.sigstore.json \
    --certificate-identity "https://github.com/jbcom/radioactive-ralph/.github/workflows/release.yml@refs/tags/v<MAJ>.<MIN>.<PATCH>" \
    --certificate-oidc-issuer "https://token.actions.githubusercontent.com"
  ```

- [ ] The atomic package PR changes exactly
      `Casks/radioactive-ralph.rb`,
      `Casks/radioactive-ralph-gui.rb`, and
      `bucket/radioactive-ralph.json`.
- [ ] The package gate binds same-repository ownership, `main` base, exact
      changed files, exact release URLs/hashes, exact checked head, and required
      workflow/app provenance.
- [ ] Authenticated cached Homebrew and Scoop premerge smokes pass without any
      package-write credential in the install/execution step.

## 6. Final transaction

The final transaction orders its state changes and authority reads exactly:

1. squash-merge only the checked atomic package PR head;
2. identify the unique winning package merge and complete the slow exact
   23-asset verification;
3. after that slow verifier, run a lightweight current package recheck that
   requires all three target paths' latest commit to still equal the proven
   winning merge OID and current `main` bytes to equal the small signed
   `package-manifests.tar.gz` payload;
4. immediately after the lightweight package recheck, freshly require the live
   immutable-release setting, source `main` version, tag SHA, and exact draft
   target/state, then PATCH once with `draft=false`, `prerelease=false`, and
   `make_latest=true`;
5. treat the fresh post-PATCH release read as authoritative when the PATCH
   response is absent or uncertain, accepting only the exact immutable stable
   state and otherwise compensating an exact draft or quarantining ambiguity;
   and require `/releases/latest` to identify that release.

- [ ] No public prerelease existed.
- [ ] The current package-main OID and the actual winning release squash-merge
      OID are recorded separately. Official rollback consumes the winning merge
      OID; its first parent is authoritative for the actual package state
      immediately before that merge. Signed seal-time provenance is only an
      integrity cross-check and never overrides ancestry.
- [ ] The release is stable, immutable, and Latest.
- [ ] A published rerun is read-only: it verifies the seal, immutable assets,
      and historical atomic merge. The selected attempt must have a valid
      `mergedAt` strictly before the immutable release `published_at`; equality
      is rejected because GitHub timestamps are second-granular. It requires
      Latest only while this is still the highest intended version.

## 7. Public observational smokes

GitHub cannot prove anonymous final release URLs before publication, and a
cross-repository PR merge plus GitHub release publish cannot be one database
transaction. That public-network window is unavoidable and explicit.

- [ ] Official Homebrew CLI/GUI installs and executions pass on Apple Silicon
      and Intel.
- [ ] Official Scoop install and execution pass.
- [ ] Anonymous curl, amd64/arm64 deb, amd64/arm64 rpm, and amd64 AppImage
      checks pass.
- [ ] Provider-live uses Claude Code `2.1.220` and Codex `0.145.0` in separate
      jobs; provider secrets exist only on their own live invocation/auth steps,
      and Codex uses then destroys a temporary `CODEX_HOME`.
- [ ] launchd, systemd-user, and Windows SCM manual host smokes pass.

## 8. Compensation and terminal versions

Before publication, any failure after package merge runs protected rollback.
Rollback derives the prior state from the unique winning exact package PR's
squash-merge first parent; signed seal-time provenance is only a cross-check.
It retries safely if strict package `main` advances for unrelated work. After
compensation, a rerun uses a deterministic new attempt branch built from the
sealed bytes and current package main.

After publication, assets and tag are immutable. If an official public-channel
smoke fails, protected rollback removes the broken package-manager pointers and
the version is **terminal**. Record the failure prominently and issue a new
patch immediately. Never claim that a same-tag rerun repairs the public release.

Do not move/delete the tag, mutate assets, turn the release into a prerelease,
or use Contents API `DELETE` calls for package rollback.

## 9. Invariants

- Release Please remains manifest-mode, force-tagged, and draft.
- The only public transition is draft to immutable stable/latest.
- `checksums.txt`, `gui-checksums.txt`, rollback provenance, and the release
  seal remain consolidated workflow-identity-signed artifacts.
- Package creation is one atomic PR; package merge is not release publication.
- Current-repository operations use the built-in token. Cross-repository package
  operations use only `PKGS_GITHUB_TOKEN`. Release Please uses only
  `RELEASE_PLEASE_GITHUB_TOKEN`.
- Fyne stays pinned at `v1.7.2`, GoReleaser at `v2.17.0`, Claude Code at
  `2.1.220`, and Codex at `0.145.0`.
