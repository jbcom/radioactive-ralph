package releasecontrol

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func readRepositoryFile(t *testing.T, path string) string {
	t.Helper()

	_, source, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate release contract test source")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(source), "..", ".."))
	content, err := os.ReadFile(filepath.Join(root, path))
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return strings.ReplaceAll(string(content), "\r\n", "\n")
}

func requireContains(t *testing.T, content, fragment, path string) {
	t.Helper()
	if !strings.Contains(content, fragment) {
		t.Errorf("%s must contain %q", path, fragment)
	}
}

func requireNotContains(t *testing.T, content, fragment, path string) {
	t.Helper()
	if strings.Contains(content, fragment) {
		t.Errorf("%s must not contain %q", path, fragment)
	}
}

func workflowJob(workflow, name string) (string, bool) {
	marker := "\n  " + name + ":\n"
	start := strings.Index(workflow, marker)
	if start == -1 {
		return "", false
	}
	body := workflow[start+len(marker):]
	lines := strings.Split(body, "\n")
	for i, line := range lines {
		if i > 0 &&
			strings.HasPrefix(line, "  ") &&
			!strings.HasPrefix(line, "    ") &&
			strings.HasSuffix(line, ":") {
			return strings.Join(lines[:i], "\n"), true
		}
	}
	return body, true
}

func requireWorkflowJob(t *testing.T, workflow, name, path string) string {
	t.Helper()
	job, ok := workflowJob(workflow, name)
	if !ok {
		t.Fatalf("%s must define the %s job", path, name)
	}
	return job
}

func TestWorkflowJobMissingOldShapeDoesNotPanic(t *testing.T) {
	oldShape := "name: Release\njobs:\n  goreleaser:\n    runs-on: ubuntu-latest\n"
	if _, ok := workflowJob(oldShape, "publish-release"); ok {
		t.Fatal("old workflow shape unexpectedly contained publish-release")
	}
}

func TestReleasePleaseCreatesTaggedDraftFromManifestConfig(t *testing.T) {
	const configPath = "release-please-config.json"
	var config map[string]any
	if err := json.Unmarshal([]byte(readRepositoryFile(t, configPath)), &config); err != nil {
		t.Fatalf("parse %s: %v", configPath, err)
	}
	if config["draft"] != true {
		t.Errorf("%s draft = %v, want true", configPath, config["draft"])
	}
	if config["force-tag-creation"] != true {
		t.Errorf("%s force-tag-creation = %v, want true", configPath, config["force-tag-creation"])
	}

	const workflowPath = ".github/workflows/cd.yml"
	workflow := readRepositoryFile(t, workflowPath)
	requireContains(t, workflow, "config-file: release-please-config.json", workflowPath)
	requireContains(t, workflow, "manifest-file: .release-please-manifest.json", workflowPath)
	requireNotContains(t, workflow, "release-type:", workflowPath)
	requireNotContains(t, workflow, "skip-github-release: true", workflowPath)
}

func TestGoReleaserReusesDraftAndWritesCurrentCosignBundle(t *testing.T) {
	const path = ".goreleaser.yaml"
	config := readRepositoryFile(t, path)

	requireContains(t, config, `signature: "${artifact}.sigstore.json"`, path)
	requireContains(t, config, `- "--bundle=${signature}"`, path)
	requireContains(t, config, "artifacts: checksum", path)
	requireNotContains(t, config, "--output-signature", path)
	requireNotContains(t, config, "--output-certificate", path)
	requireNotContains(t, config, "certificate:", path)

	releaseSection := strings.SplitN(config, "\nrelease:\n", 2)
	if len(releaseSection) != 2 {
		t.Fatalf("%s has no release section", path)
	}
	requireContains(t, releaseSection[1], "draft: true", path)
	requireContains(t, releaseSection[1], "use_existing_draft: true", path)
	requireContains(t, releaseSection[1], "replace_existing_artifacts: true", path)
	if got := strings.Count(config, "skip_upload: true"); got != 3 {
		t.Errorf("%s skip_upload count = %d, want 3", path, got)
	}
}

func TestPublicReleaseWaitsForAllRequiredArtifacts(t *testing.T) {
	const path = ".github/workflows/release.yml"
	workflow := readRepositoryFile(t, path)

	requireContains(t, workflow, "group: release-${{ github.ref }}", path)
	requireContains(t, workflow, "cancel-in-progress: false", path)

	requireNotContains(t, workflow, "\n  stage-release:\n", path)
	requireNotContains(t, workflow, "-F prerelease=true", path)
	provenanceJob := requireWorkflowJob(t, workflow, "package-rollback-provenance", path)
	requireContains(t, provenanceJob, "prepare_package_rollback_provenance.sh", path)
	sealJob := requireWorkflowJob(t, workflow, "release-seal", path)
	requireContains(t, sealJob, "prepare_release_seal.sh", path)

	publishJob := requireWorkflowJob(t, workflow, "publish-release", path)
	requireContains(t, publishJob, "RELEASE_GH_TOKEN: ${{ github.token }}", path)
	requireContains(t, publishJob, "PKGS_GH_TOKEN: ${{ secrets.PKGS_GITHUB_TOKEN }}", path)
	requireContains(t, publishJob, `immutable-releases`, path)
	requireContains(t, publishJob, "bash scripts/ci/verify_release_assets.sh", path)
	requireContains(t, publishJob, "PACKAGE_GATE_MODE=resolve-merged", path)
	requireContains(t, publishJob, "package_release_merge_oid", path)
	requireContains(t, publishJob, `.target_commitish == $target`, path)
	requireContains(t, publishJob, "-F prerelease=false", path)
	requireContains(t, publishJob, "-f make_latest=true", path)
	requireContains(t, publishJob, ".immutable == true", path)
	requireContains(t, publishJob, "releases/latest", path)
	requireContains(t, publishJob, "rollback_package_manifests.sh", path)

	lastAssets := strings.LastIndex(publishJob, "bash scripts/ci/verify_release_assets.sh")
	lastPackageRecheck := strings.LastIndex(publishJob, "PACKAGE_GATE_MODE=recheck-current")
	lastImmutable := strings.LastIndex(publishJob, `"repos/${GITHUB_REPOSITORY}/immutable-releases"`)
	lastManifest := strings.LastIndex(publishJob, `contents/.release-please-manifest.json?ref=main`)
	lastTag := strings.LastIndex(publishJob, `git/ref/tags/${GITHUB_REF_NAME}`)
	lastRelease := strings.LastIndex(publishJob, `release="$(GH_TOKEN="$RELEASE_GH_TOKEN" gh api`)
	promotion := strings.Index(publishJob, "-f make_latest=true")
	for label, index := range map[string]int{
		"sealed assets":     lastAssets,
		"package recheck":   lastPackageRecheck,
		"immutable setting": lastImmutable,
		"source manifest":   lastManifest,
		"tag":               lastTag,
		"release":           lastRelease,
		"promotion":         promotion,
	} {
		if index == -1 {
			t.Errorf("publish-release missing final %s operation", label)
		}
	}
	if lastAssets >= lastPackageRecheck ||
		lastPackageRecheck >= lastImmutable ||
		lastImmutable >= lastManifest ||
		lastManifest >= lastTag ||
		lastTag >= lastRelease ||
		lastRelease >= promotion {
		t.Error("publish-release must finish the 23-asset verifier, recheck current package ownership/bytes, then freshly reread every release authority immediately before promotion")
	}

	chocolatey := requireWorkflowJob(t, workflow, "chocolatey", path)
	requireContains(t, chocolatey, "needs: publish-release", path)
	publishedAudit := requireWorkflowJob(t, workflow, "published-rerun-audit", path)
	requireContains(t, publishedAudit, "PACKAGE_GATE_MODE=resolve-historical", path)
}

func TestStableAdmissionPrecedesAllPublishers(t *testing.T) {
	const path = ".github/workflows/release.yml"
	workflow := readRepositoryFile(t, path)

	admission := requireWorkflowJob(t, workflow, "release-admission", path)
	requireContains(t, admission, `^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`, path)
	requireContains(t, admission, "contents: read", path)
	requireContains(t, admission, "fetch-depth: 0", path)
	requireContains(t, admission, `+refs/heads/main:refs/remotes/origin/main`, path)
	requireContains(t, admission, `git rev-parse "${GITHUB_REF_NAME}^{commit}"`, path)
	requireContains(t, admission, `git rev-parse "${GITHUB_SHA}^{commit}"`, path)
	requireContains(t, admission, `git merge-base --is-ancestor "$tag_commit" origin/main`, path)
	requireContains(t, admission, `jq -er '."."' .release-please-manifest.json`, path)
	requireContains(t, admission, `"v${manifest_version}" != "$GITHUB_REF_NAME"`, path)
	requireContains(t, admission, "--json isDraft,isPrerelease,tagName,targetCommitish", path)
	requireContains(t, admission, `"$tag_name" != "$GITHUB_REF_NAME" || "$target_commitish" != "$TAG_COMMIT"`, path)
	requireContains(t, admission, `"$is_draft" == "true" && "$is_prerelease" == "false"`, path)
	requireContains(t, admission, "public prerelease or ambiguous release state is invalid", path)
	requireContains(t, admission, "immutable-releases", path)

	goreleaser := requireWorkflowJob(t, workflow, "goreleaser", path)
	requireContains(t, goreleaser, "needs: release-admission", path)
	requireNotContains(t, admission, "goreleaser/goreleaser-action@", path)
}

func TestPackagePublicationRequiresExactMergedMainVersions(t *testing.T) {
	const workflowPath = ".github/workflows/release.yml"
	workflow := readRepositoryFile(t, workflowPath)
	headJob := requireWorkflowJob(t, workflow, "verify-package-heads", workflowPath)
	requireContains(t, headJob, "PACKAGE_GATE_MODE: verify-heads", workflowPath)
	verifyJob := requireWorkflowJob(t, workflow, "publish-release", workflowPath)
	requireContains(t, verifyJob, "EXPECTED_PACKAGE_HEAD_OID:", workflowPath)
	requireContains(t, verifyJob, `wait_for_package_publication.sh "$VERSION"`, workflowPath)

	const scriptPath = "scripts/ci/wait_for_package_publication.sh"
	script := readRepositoryFile(t, scriptPath)
	for _, path := range []string{
		"Casks/radioactive-ralph.rb",
		"Casks/radioactive-ralph-gui.rb",
		"bucket/radioactive-ralph.json",
	} {
		requireContains(t, script, path, scriptPath)
	}
	requireContains(t, script, `fetch_ref_file "$path" main`, scriptPath)
	requireContains(t, script, `before="$(resolve_main_oid)" || return 1`, scriptPath)
	requireContains(t, script, `required_is_current_at_ref package "$before"`, scriptPath)
	requireContains(t, script, `after="$(resolve_main_oid)" || return 1`, scriptPath)
	requireContains(t, script, `[[ "$before" == "$after" ]] || return 1`, scriptPath)
	requireContains(t, script, `if all_required_are_current; then`, scriptPath)
	requireContains(t, script, `pkgs_main_oid=$VERIFIED_MAIN_OID`, scriptPath)
	requireContains(t, script, "BASE_PACKAGE_BRANCH=\"chore/update-radioactive-ralph-${VERSION}\"", scriptPath)
	requireNotContains(t, script, "chore/update-radioactive-ralph-gui-cask-${VERSION}", scriptPath)
	requireContains(t, script, "--json number,baseRefName,headRefName,headRefOid,headRepositoryOwner,isCrossRepository,state,url", scriptPath)
	requireContains(t, script, `.baseRefName == "main"`, scriptPath)
	requireContains(t, script, `.headRepositoryOwner.login == $owner`, scriptPath)
	requireContains(t, script, `.isCrossRepository == false`, scriptPath)
	requireContains(t, script, "validate_changed_files", scriptPath)
	requireContains(t, script, "validate_release_manifests", scriptPath)
	requireContains(t, script, `repos/${PKGS_REPO}/contents/${path}?ref=${ref}`, scriptPath)
	requireContains(t, script, `release_gh release download "v${VERSION}"`, scriptPath)
	requireContains(t, script, `gui-checksums.txt.sigstore.json`, scriptPath)
	requireContains(t, script, `"$COSIGN_BIN" verify-blob "$gui_checksums_path"`, scriptPath)
	requireContains(t, script, `release_checksum "$checksums" "$release_asset"`, scriptPath)
	requireContains(t, script, `release_checksum "$gui_checksums" "$release_asset"`, scriptPath)
	requireContains(t, script, `radioactive-ralph_${VERSION}_linux_amd64.deb`, scriptPath)
	requireContains(t, script, `radioactive-ralph_${VERSION}_linux_arm64.rpm`, scriptPath)
	requireContains(t, script, `radioactive-ralph_${VERSION}_linux_x86_64.AppImage`, scriptPath)
	requireContains(t, script, `radioactive-ralph.exe`, scriptPath)
	requireContains(t, script, `package-manifests.tar.gz`, scriptPath)
	requireContains(t, script, `package-manifests.tar.gz.sigstore.json`, scriptPath)
	requireContains(t, script, `package_listing="$(tar -tzf "$package_path"`, scriptPath)
	requireContains(t, script, `cmp -s "$PACKAGE_PAYLOAD_DIR/Casks/radioactive-ralph.rb"`, scriptPath)
	requireContains(t, script, `--raw-field path="$path"`, scriptPath)
	requireContains(t, script, `package_release_merge_oid=$WINNING_RELEASE_MERGE_OID`, scriptPath)
	requireContains(t, script, `.mergeCommit.oid == $merge`, scriptPath)
	requireContains(t, script, `test("^[1-9][0-9]*$")`, scriptPath)
	requireContains(t, script, `.published_at | strings`, scriptPath)
	requireContains(t, script, `.mergedAt | strings`, scriptPath)
	requireContains(t, script, `merged_epoch >= published_epoch`, scriptPath)
	requireContains(t, script, `merged_epoch == latest_epoch`, scriptPath)
	requireContains(t, script, `PACKAGE_GATE_MODE" == "recheck-current`, scriptPath)
	requireContains(t, script, `EXPECTED_PACKAGE_RELEASE_MERGE_OID`, scriptPath)
	requireContains(t, script, `latest_target_paths_equal "$expected_oid"`, scriptPath)
	requireContains(t, script, `package_payload_matches_ref main`, scriptPath)
	requireContains(t, script, `EXPECTED_ACTIONS_APP_ID=15368`, scriptPath)
	requireContains(t, script, `.app.slug == "github-actions"`, scriptPath)
	requireContains(t, script, `.github/workflows/validate-packages.yml`, scriptPath)
	requireContains(t, script, `.github/workflows/ci.yml`, scriptPath)
	requireContains(t, script, `.event == "pull_request"`, scriptPath)
	requireContains(t, script, `.head_sha == $head_oid`, scriptPath)
	for _, conclusion := range []string{
		"ACTION_REQUIRED",
		"CANCELLED",
		"FAILURE",
		"TIMED_OUT",
	} {
		requireContains(t, script, `.conclusion == "`+conclusion+`"`, scriptPath)
	}
	requireNotContains(t, script, "\n        --auto \\", scriptPath)
	requireContains(t, script, "--squash", scriptPath)
	requireContains(t, script, `--match-head-commit "$VERIFIED_HEAD_OID"`, scriptPath)

	const behaviorPath = "scripts/ci/test_wait_for_package_publication.sh"
	behavior := readRepositoryFile(t, behaviorPath)
	for _, adversary := range []string{
		"FAKE_GUI_BAD_HOST",
		"FAKE_GUI_BAD_ARTIFACT",
		"FAKE_BAD_POSTFLIGHT",
		"FAKE_CLI_BAD_HASH",
		"FAKE_GUI_BAD_HASH",
		"FAKE_TAMPER_MAIN",
		"FAKE_MAIN_RACE",
		"FAKE_COSIGN_FAILURE",
		"FAKE_MISSING_ASSET",
		"FAKE_STALE_CLI_ASSET",
		"FAKE_CLOBBERED_GUI_ASSET",
		"FAKE_SPOOFED_CHECK_APP",
		"FAKE_WRONG_WORKFLOW_PATH",
		"FAKE_WRONG_WORKFLOW_HEAD",
		"FAKE_CLI_HOOK",
		"FAKE_GUI_HOOK",
		"FAKE_SCOOP_PRE_INSTALL",
		"FAKE_SCOOP_INSTALLER",
		"FAKE_ALREADY_MERGED",
		"FAKE_INTERVENING_MAIN",
		"FAKE_SPLIT_PATH_OWNER",
		"FAKE_AMBIGUOUS_WINNER",
		"FAKE_SAME_PREFIX_UNSEALED",
		"FAKE_MULTIPLE_ATTEMPTS",
		"FAKE_MULTIPLE_EXACT_ATTEMPTS",
		"FAKE_POST_PUBLIC_ATTEMPT",
		"FAKE_SINGLE_AT_PUBLICATION",
		"FAKE_EQUAL_MERGED_AT",
		"FAKE_MISSING_MERGED_AT",
		"FAKE_INVALID_MERGED_AT",
		"final-ownership-drift",
		"patch-called",
	} {
		requireContains(t, behavior, adversary, behaviorPath)
	}

	const assetsPath = "scripts/ci/verify_release_assets.sh"
	assets := readRepositoryFile(t, assetsPath)
	requireContains(t, assets, "exact 23-asset immutable set and 13 deliverables", assetsPath)
	requireContains(t, assets, "checksums.txt.sigstore.json", assetsPath)
	requireContains(t, assets, "gui-checksums.txt.sigstore.json", assetsPath)
	requireContains(t, assets, "package-rollback.tar.gz.sigstore.json", assetsPath)
	requireContains(t, assets, "package-manifests.tar.gz.sigstore.json", assetsPath)
	requireContains(t, assets, "release-seal.json.sigstore.json", assetsPath)
}

func TestReleaseToolingIsPinnedAndPermissionsAreLeastPrivilege(t *testing.T) {
	const path = ".github/workflows/release.yml"
	workflow := readRepositoryFile(t, path)
	requireContains(t, workflow, "permissions: {}", path)
	requireNotContains(t, workflow, "packages: write", path)
	requireNotContains(t, workflow, "fyne.io/tools/cmd/fyne@latest", path)
	requireContains(t, workflow, "fyne.io/tools/cmd/fyne@v1.7.2", path)
	requireNotContains(t, workflow, `version: "~> v2"`, path)
	requireContains(t, workflow, "version: v2.17.0", path)

	goreleaser := requireWorkflowJob(t, workflow, "goreleaser", path)
	requireContains(t, goreleaser, "contents: write", path)
	requireContains(t, goreleaser, "id-token: write", path)

	for _, name := range []string{"gui-bundles", "chocolatey", "verify-package-heads"} {
		job := requireWorkflowJob(t, workflow, name, path)
		requireContains(t, job, "contents: read", path)
		requireNotContains(t, job, "id-token: write", path)
	}
	gui := requireWorkflowJob(t, workflow, "gui-bundles", path)
	requireContains(t, gui, "actions: read", path)
	requireNotContains(t, gui, "contents: write", path)
	signer := requireWorkflowJob(t, workflow, "sign-gui-checksums", path)
	requireContains(t, signer, "contents: write", path)
	requireContains(t, signer, "id-token: write", path)
	requireNotContains(t, workflow, "CI_GITHUB_TOKEN", path)

	const cdPath = ".github/workflows/cd.yml"
	cd := readRepositoryFile(t, cdPath)
	requireContains(t, cd, "permissions: {}", cdPath)
	releasePlease := requireWorkflowJob(t, cd, "release-please", cdPath)
	requireContains(t, releasePlease, "secrets.RELEASE_PLEASE_GITHUB_TOKEN", cdPath)
	requireNotContains(t, releasePlease, "pages: write", cdPath)
	requireNotContains(t, releasePlease, "id-token: write", cdPath)
	buildDocs := requireWorkflowJob(t, cd, "build-docs", cdPath)
	requireContains(t, buildDocs, "contents: read", cdPath)
	requireNotContains(t, buildDocs, "pages: write", cdPath)
	requireNotContains(t, buildDocs, "id-token: write", cdPath)
	requireContains(t, buildDocs, "github.com/princjef/gomarkdoc/cmd/gomarkdoc@v1.1.0", cdPath)
	requireContains(t, buildDocs, `"tox==4.53.0"`, cdPath)
	requireContains(t, buildDocs, "actions/upload-pages-artifact@", cdPath)
	publishDocs := requireWorkflowJob(t, cd, "publish-docs", cdPath)
	requireContains(t, publishDocs, "needs: build-docs", cdPath)
	requireContains(t, publishDocs, "pages: write", cdPath)
	requireContains(t, publishDocs, "id-token: write", cdPath)
	requireNotContains(t, publishDocs, "contents: read", cdPath)
	requireNotContains(t, publishDocs, "actions/checkout@", cdPath)
	requireNotContains(t, publishDocs, "go install", cdPath)
	requireNotContains(t, publishDocs, "pip install", cdPath)
	requireNotContains(t, publishDocs, "tox -e docs", cdPath)
	requireNotContains(t, publishDocs, "actions/upload-pages-artifact@", cdPath)
	requireContains(t, publishDocs, "actions/deploy-pages@", cdPath)

	const ciPath = ".github/workflows/ci.yml"
	ci := readRepositoryFile(t, ciPath)
	requireNotContains(t, ci, "@latest", ciPath)
	requireNotContains(t, ci, `version: "~> v2"`, ciPath)
	requireContains(t, ci, "version: v2.17.0", ciPath)
	requireNotContains(t, ci, "gomarkdoc@latest", ciPath)
	requireContains(t, ci, "gomarkdoc@v1.1.0", ciPath)
	requireContains(t, ci, "@anthropic-ai/claude-code@2.1.220", ciPath)
	requireContains(t, ci, "version: v2.12.2", ciPath)
	requireContains(t, ci, "actionlint@v1.7.12", ciPath)
	requireContains(t, ci, "govulncheck@v1.6.0", ciPath)
	requireContains(t, ci, "4b6d6bcb4819be4fe209e807726e83be12da3190", ciPath)

	const providerPath = ".github/workflows/provider-live.yml"
	provider := readRepositoryFile(t, providerPath)
	requireContains(t, provider, "@anthropic-ai/claude-code@2.1.220", providerPath)
	requireContains(t, provider, "@openai/codex@0.145.0", providerPath)
	requireNotContains(t, provider, "@latest", providerPath)
	requireNotContains(t, provider, "GEMINI_API_KEY", providerPath)
	requireNotContains(t, provider, "GOOGLE_API_KEY", providerPath)
	claudeJob := requireWorkflowJob(t, provider, "live-claude", providerPath)
	codexJob := requireWorkflowJob(t, provider, "live-codex", providerPath)
	requireContains(t, claudeJob, "ANTHROPIC_API_KEY", providerPath)
	requireNotContains(t, claudeJob, "OPENAI_API_KEY", providerPath)
	requireNotContains(t, claudeJob, "CODEX_HOME", providerPath)
	requireContains(t, codexJob, "OPENAI_API_KEY", providerPath)
	requireNotContains(t, codexJob, "ANTHROPIC_API_KEY", providerPath)
	requireContains(t, codexJob, `CODEX_HOME: ${{ runner.temp }}/codex-home`, providerPath)
	requireContains(t, codexJob, "codex logout", providerPath)
	requireContains(t, codexJob, `test "$CODEX_HOME" = "$RUNNER_TEMP/codex-home"`, providerPath)

	const toxPath = "tox.ini"
	tox := readRepositoryFile(t, toxPath)
	requireContains(t, tox, "requires = tox==4.53.0", toxPath)
	requireContains(t, tox, "-r docs/requirements.lock", toxPath)
}

func TestCLIAndGUICasksHaveDistinctTokensAndFiles(t *testing.T) {
	const goreleaserPath = ".goreleaser.yaml"
	goreleaser := readRepositoryFile(t, goreleaserPath)
	requireContains(t, goreleaser, "homebrew_casks:\n  - name: radioactive-ralph", goreleaserPath)

	const publisherPath = "packaging/publish-cli-manifests.sh"
	publisher := readRepositoryFile(t, publisherPath)
	requireContains(t, publisher, "Casks/radioactive-ralph-gui.rb", publisherPath)
	requireContains(t, publisher, "Casks/radioactive-ralph.rb", publisherPath)
	requireContains(t, publisher, "bucket/radioactive-ralph.json", publisherPath)
	requireContains(t, publisher, "package-manifests.tar.gz", publisherPath)
	requireContains(t, publisher, `"$COSIGN_BIN" verify-blob`, publisherPath)
	requireContains(t, publisher, `push-version-branch.sh" "$BRANCH"`, publisherPath)
	requireNotContains(t, publisher, "git push --force-with-lease origin", publisherPath)

	const bundlePath = "scripts/ci/prepare_package_manifests_bundle.sh"
	bundle := readRepositoryFile(t, bundlePath)
	requireContains(t, bundle, `cask "radioactive-ralph-gui"`, bundlePath)

	const pushHelperPath = "packaging/macos/push-version-branch.sh"
	pushHelper := readRepositoryFile(t, pushHelperPath)
	requireContains(t, pushHelper, `git ls-remote --heads "$REMOTE" "$REF"`, pushHelperPath)
	requireContains(t, pushHelper, `--force-with-lease=${REF}:${remote_sha}`, pushHelperPath)
	requireContains(t, pushHelper, `--force-with-lease=${REF}:`, pushHelperPath)
	requireContains(t, pushHelper, `git push "$lease" "$REMOTE" "HEAD:${REF}"`, pushHelperPath)
}

func TestPremergeHomebrewSmokeRewritesExactInterpolatedURLs(t *testing.T) {
	const workflowPath = ".github/workflows/release.yml"
	workflow := readRepositoryFile(t, workflowPath)
	job := requireWorkflowJob(t, workflow, "premerge-package-smoke", workflowPath)
	requireContains(t, job, "scripts/ci/rewrite_homebrew_cask_url.sh", workflowPath)
	requireContains(t, job, `"radioactive_ralph_${VERSION}_darwin_arm64.tar.gz"`, workflowPath)
	requireContains(t, job, `"radioactive-ralph_${VERSION}_darwin_arm64.dmg"`, workflowPath)
	requireContains(t, job, "persist-credentials: false", workflowPath)
	requireContains(t, job, "unset GH_TOKEN PKGS_GH_TOKEN RELEASE_GH_TOKEN", workflowPath)
	requireContains(t, job, `git config --local --get-regexp '^http\..*\.extraheader$'`, workflowPath)

	const helperPath = "scripts/ci/rewrite_homebrew_cask_url.sh"
	helper := readRepositoryFile(t, helperPath)
	requireContains(t, helper, `version_template='#{version}'`, helperPath)
	requireContains(t, helper, `releases/download/v${VERSION}/${template_asset}`, helperPath)
	requireContains(t, helper, `releases/download/v${version_template}/${template_asset}`, helperPath)
	requireContains(t, helper, `if (count != 1)`, helperPath)
	requireContains(t, helper, `replacement="file://${CACHED_PATH}\""`, helperPath)
}

func TestReleaseHelpersRequireNamedTokenAuthorities(t *testing.T) {
	for _, path := range []string{
		"packaging/publish-cli-manifests.sh",
		"scripts/ci/prepare_package_manifests_bundle.sh",
		"scripts/ci/prepare_package_rollback_provenance.sh",
		"scripts/ci/prepare_release_seal.sh",
		"scripts/ci/rollback_package_manifests.sh",
		"scripts/ci/verify_release_assets.sh",
		"scripts/ci/wait_for_package_publication.sh",
	} {
		content := readRepositoryFile(t, path)
		requireNotContains(t, content, `${GH_TOKEN:-`, path)
		requireNotContains(t, content, `:-"$GH_TOKEN"`, path)
		requireNotContains(t, content, `:-${GH_TOKEN`, path)
	}

	rollback := readRepositoryFile(t, "scripts/ci/rollback_package_manifests.sh")
	requireContains(t, rollback, "PKGS_GH_TOKEN is required", "scripts/ci/rollback_package_manifests.sh")
	requireContains(t, rollback, `(umask 0077 && printf`, "scripts/ci/rollback_package_manifests.sh")
	requireContains(t, rollback, `"$PKGS_GH_TOKEN" > "$ASKPASS")`, "scripts/ci/rollback_package_manifests.sh")
	requireNotContains(t, rollback, "RELEASE_GH_TOKEN", "scripts/ci/rollback_package_manifests.sh")

	publisher := readRepositoryFile(t, "packaging/publish-cli-manifests.sh")
	requireContains(t, publisher, "PKGS_GH_TOKEN is required", "packaging/publish-cli-manifests.sh")
	requireContains(t, publisher, "RELEASE_GH_TOKEN is required", "packaging/publish-cli-manifests.sh")
}

func TestCosignChecklistPinsWorkflowIdentityAndIssuer(t *testing.T) {
	const path = "docs/launch/release-checklist.md"
	checklist := readRepositoryFile(t, path)
	requireContains(t, checklist, "--certificate-identity", path)
	requireContains(t, checklist, "https://github.com/jbcom/radioactive-ralph/.github/workflows/release.yml@refs/tags/v<MAJ>.<MIN>.<PATCH>", path)
	requireContains(t, checklist, "--certificate-oidc-issuer", path)
	requireContains(t, checklist, "https://token.actions.githubusercontent.com", path)
}

func TestReleaseChecklistPinsExternalTagRulesetContract(t *testing.T) {
	const path = "docs/launch/release-checklist.md"
	checklist := readRepositoryFile(t, path)
	for _, contract := range []string{
		"Release tags are admin-created",
		"ID `19751997`",
		"targets `tag`",
		"`refs/tags/v*`",
		"creation rule",
		"`OrganizationAdmin` an `always` bypass",
		"Release tags cannot move or be deleted",
		"ID `19752322`",
		"update and deletion rules",
		"no bypass actors",
	} {
		requireContains(t, checklist, contract, path)
	}
}

func TestStableInstallDocsDoNotAdvertiseUnpublishedManagers(t *testing.T) {
	for _, path := range []string{
		"README.md",
		".goreleaser.yaml",
		"docs/install.sh",
		"packaging/README.md",
		"docs/superpowers/PILLARS.md",
	} {
		content := readRepositoryFile(t, path)
		for _, unsupported := range []string{
			"winget install jbcom.radioactive-ralph",
			"choco install radioactive-ralph",
			"use Scoop or Chocolatey instead",
			"a winget publisher",
			"all CLI package managers",
		} {
			requireNotContains(t, content, unsupported, path)
		}
	}
}

func TestPackagingDocsMatchImplementedGUIDelivery(t *testing.T) {
	const path = "packaging/README.md"
	packaging := readRepositoryFile(t, path)
	requireContains(t, packaging, "custom GUI cask's `postflight` explicitly", path)
	requireContains(t, packaging, "Homebrew does not do this by default", path)
	requireContains(t, packaging, "the `.exe` ships unsigned", path)
	requireNotContains(t, packaging, "Homebrew strips `com.apple.quarantine`", path)
	requireNotContains(t, packaging, "the MSI ships unsigned", path)
}

func TestHostedProviderAndGUIPackageChecksExerciseDeliveredCode(t *testing.T) {
	const providerWorkflowPath = ".github/workflows/provider-live.yml"
	providerWorkflow := readRepositoryFile(t, providerWorkflowPath)
	codexJob := requireWorkflowJob(t, providerWorkflow, "live-codex", providerWorkflowPath)
	requireContains(t, codexJob,
		`go test ./tests/integration -run '^TestLiveCodexRunnerTurn$' -count=1 -v`,
		providerWorkflowPath)

	const liveTestPath = "tests/integration/live_test.go"
	liveTest := readRepositoryFile(t, liveTestPath)
	requireContains(t, liveTest, `os.Getenv("CODEX_AUTHENTICATED") != "1"`, liveTestPath)
	requireContains(t, liveTest, `(provider.CodexRunner{}).Run`, liveTestPath)
	requireContains(t, liveTest, `OutputSchema: outputSchema`, liveTestPath)
	requireContains(t, liveTest, `context.WithTimeout(context.Background(), 2*time.Minute)`, liveTestPath)

	const guiScriptPath = "scripts/ci/build_gui_package.sh"
	guiScript := readRepositoryFile(t, guiScriptPath)
	requireContains(t, guiScript,
		`EXPECTED_VERSION="$VERSION ($BUILD_COMMIT, built $BUILD_DATE)"`,
		guiScriptPath)
	requireContains(t, guiScript,
		`GOFLAGS="-trimpath -ldflags=-X=main.Version=$VERSION -ldflags=-X=main.Commit=$BUILD_COMMIT -ldflags=-X=main.Date=$BUILD_DATE"`,
		guiScriptPath)
	if got := strings.Count(guiScript, `grep -F "$EXPECTED_VERSION"`); got != 2 {
		t.Errorf("%s exact version identity check count = %d, want 2", guiScriptPath, got)
	}
	if got := strings.Count(guiScript, `grep -F $'dep\tfyne.io/fyne/v2'`); got != 2 {
		t.Errorf("%s Fyne module check count = %d, want 2", guiScriptPath, got)
	}
}

func TestOfflinePackageSmokesAndInstallerPolicyRunInRequiredCI(t *testing.T) {
	const ciPath = ".github/workflows/ci.yml"
	ci := readRepositoryFile(t, ciPath)
	for _, testPath := range []string{
		"scripts/ci/test_install_signature_policy.sh",
		"scripts/ci/test_prepare_package_rollback_provenance.sh",
		"scripts/ci/test_premerge_scoop_server_contract.py",
	} {
		requireContains(t, ci, testPath, ciPath)
	}

	const installerPath = "docs/install.sh"
	installer := readRepositoryFile(t, installerPath)
	requireContains(t, installer,
		`REQUIRE_SIGNATURE=${RADIOACTIVE_RALPH_REQUIRE_SIGNATURE:-0}`,
		installerPath)
	requireContains(t, installer,
		`cosign is required when RADIOACTIVE_RALPH_REQUIRE_SIGNATURE=1`,
		installerPath)
	requireContains(t, installer, `signed checksum verification failed`, installerPath)

	const scoopContractPath = "scripts/ci/test_premerge_scoop_server_contract.py"
	scoopContract := readRepositoryFile(t, scoopContractPath)
	requireContains(t, scoopContract, `"'--bind', '127.0.0.1'"`, scoopContractPath)
	requireContains(t, scoopContract, `"$server.WaitForExit(10000)"`, scoopContractPath)
	requireContains(t, scoopContract, `"$server.Dispose()"`, scoopContractPath)
}

func TestRollbackProvenanceModelsMissingFilesAndExactHTTPStatus(t *testing.T) {
	for _, path := range []string{
		"scripts/ci/prepare_package_rollback_provenance.sh",
		"scripts/ci/rollback_package_manifests.sh",
	} {
		script := readRepositoryFile(t, path)
		requireContains(t, script, `--silent --include`, path)
		requireContains(t, script, `^HTTP/[0-9.]+ 404([[:space:]]|$)`, path)
		requireContains(t, script, `.schema == 2`, path)
		requireContains(t, script, `.state == "missing"`, path)
	}

	const rollbackPath = "scripts/ci/rollback_package_manifests.sh"
	rollback := readRepositoryFile(t, rollbackPath)
	requireContains(t, rollback, `git -C "$WORK/pkgs" rm -f -- "$path"`, rollbackPath)
	requireContains(t, rollback, `named provenance archive is missing`, rollbackPath)

	const verifierPath = "scripts/ci/verify_release_assets.sh"
	verifier := readRepositoryFile(t, verifierPath)
	requireContains(t, verifier, `sub(/^\*/, "", name)`, verifierPath)

	const publisherPath = "scripts/ci/wait_for_package_publication.sh"
	publisher := readRepositoryFile(t, publisherPath)
	requireContains(t, publisher, `resolve_winning_release_merge || exit 1`, publisherPath)
	requireContains(t, publisher, `resolved_main_oid="$(resolve_main_oid)" || exit 1`, publisherPath)
}
