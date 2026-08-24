package releasecontrol

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"sort"
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

// admissionAllowedGitHubCommands is the exhaustive set of `gh` subcommand paths
// release-admission may invoke. Anything else -- a release mutation, or any
// write-capable API call -- fails the contract regardless of how its flags are
// spelled or ordered.
var admissionAllowedGitHubCommands = map[string]bool{
	"release view": true,
}

// requireAdmissionGitHubCommandsAllowed enforces the admission command boundary
// as an allowlist over the actual `gh` invocations in the job, rather than as a
// blocklist of literal flag spellings. A blocklist is defeated by reordering
// flags ("gh api -f x=1 --method PATCH"), by passing fields separately, or by
// any mutating subcommand nobody thought to enumerate.
func requireAdmissionGitHubCommandsAllowed(t *testing.T, job, path string) {
	t.Helper()

	for _, invocation := range extractGitHubInvocations(job) {
		// Any explicit method override or field argument makes the call
		// write-capable in gh's own semantics (fields imply POST).
		for _, writeFlag := range []string{"--method", "-X", "-f", "-F", "--field", "--raw-field", "--input"} {
			for _, tok := range invocation.args {
				if tok == writeFlag {
					t.Errorf("%s release-admission must not issue write-capable gh call %q (flag %q)",
						path, invocation.line, writeFlag)
				}
			}
		}
		if !admissionAllowedGitHubCommands[invocation.subcommand] {
			t.Errorf("%s release-admission gh subcommand %q is not in the admission allowlist %v (line %q)",
				path, invocation.subcommand, keysOf(admissionAllowedGitHubCommands), invocation.line)
		}
	}
}

type githubInvocation struct {
	subcommand string
	args       []string
	line       string
}

// extractGitHubInvocations finds each `gh` call in a workflow job body and
// returns its two-word subcommand path plus its argument tokens. Line
// continuations are joined so a call split across lines is still analyzed whole.
func extractGitHubInvocations(job string) []githubInvocation {
	joined := strings.ReplaceAll(job, "\\\n", " ")
	var out []githubInvocation
	for _, raw := range strings.Split(joined, "\n") {
		line := strings.TrimSpace(raw)
		if strings.HasPrefix(line, "#") {
			continue
		}
		for _, seg := range splitShellSegments(line) {
			if invocation, ok := parseGitHubInvocation(seg); ok {
				out = append(out, invocation)
			}
		}
	}
	return out
}

func parseGitHubInvocation(segment string) (githubInvocation, bool) {
	fields := strings.Fields(segment)
	for i, field := range fields {
		// Match the gh binary itself, including VAR=x gh ... and $(gh ...
		if strings.TrimLeft(field, "\"'$(`") != "gh" {
			continue
		}
		if i+1 >= len(fields) {
			return githubInvocation{}, false
		}
		args := fields[i+1:]
		return githubInvocation{
			subcommand: githubSubcommand(args),
			args:       args,
			line:       strings.TrimSpace(segment),
		}, true
	}
	return githubInvocation{}, false
}

func githubSubcommand(args []string) string {
	words := make([]string, 0, 2)
	for _, arg := range args {
		if !strings.HasPrefix(arg, "-") {
			words = append(words, strings.Trim(arg, `"'`))
		}
		if len(words) == 2 {
			break
		}
	}
	return strings.Join(words, " ")
}

// splitShellSegments breaks a shell line on pipes, logical operators, and
// command substitution so each `gh` call is inspected independently.
func splitShellSegments(line string) []string {
	replacer := strings.NewReplacer("|", "\n", "&&", "\n", "||", "\n", ";", "\n", "$(", "\n", "`", "\n")
	return strings.Split(replacer.Replace(line), "\n")
}

func keysOf(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
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
	sealScript := readRepositoryFile(t, "scripts/ci/prepare_release_seal.sh")
	requireContains(t, sealScript, "release view \"v${VERSION}\" --repo \"$RELEASE_REPO\"", "scripts/ci/prepare_release_seal.sh")

	publishJob := requireWorkflowJob(t, workflow, "publish-release", path)
	requireContains(t, publishJob, "RELEASE_GH_TOKEN: ${{ github.token }}", path)
	if got := strings.Count(publishJob,
		`CI_GITHUB_TOKEN="${{ secrets.CI_GITHUB_TOKEN }}" \`); got != 2 {
		t.Errorf("%s command-scoped CI credential count = %d, want 2", path, got)
	}
	requireContains(t, publishJob, "PKGS_GH_TOKEN: ${{ secrets.PKGS_GITHUB_TOKEN }}", path)
	if got := strings.Count(publishJob, "bash scripts/ci/require_immutable_releases.sh"); got != 2 {
		t.Errorf("%s publish-release immutable-release gate count = %d, want 2", path, got)
	}
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
	lastImmutable := strings.LastIndex(publishJob, "bash scripts/ci/require_immutable_releases.sh")
	lastManifest := strings.LastIndex(publishJob, `contents/.release-please-manifest.json?ref=main`)
	lastTag := strings.LastIndex(publishJob, `git/ref/tags/${GITHUB_REF_NAME}`)
	lastRelease := strings.LastIndex(publishJob, `release="$(GH_TOKEN="$RELEASE_GH_TOKEN" gh release view`)
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
	// Admission is read-only. The built-in token never carries write access here,
	// so no step in this job can mutate a release even if a helper tried to. The
	// Release Please draft is read with CI_GITHUB_TOKEN, because GitHub hides
	// drafts from tokens lacking repository push access.
	requireContains(t, admission, "contents: read", path)
	requireNotContains(t, admission, "contents: write", path)
	requireContains(t, admission, "persist-credentials: false", path)
	requireAdmissionGitHubCommandsAllowed(t, admission, path)
	requireContains(t, admission, "fetch-depth: 0", path)
	requireContains(t, admission, `+refs/heads/main:refs/remotes/origin/main`, path)
	requireContains(t, admission, `git rev-parse "${GITHUB_REF_NAME}^{commit}"`, path)
	requireContains(t, admission, `git rev-parse "${GITHUB_SHA}^{commit}"`, path)
	requireContains(t, admission, `git merge-base --is-ancestor "$tag_commit" origin/main`, path)
	requireContains(t, admission, `jq -er '."."' .release-please-manifest.json`, path)
	requireContains(t, admission, `"v${manifest_version}" != "$GITHUB_REF_NAME"`, path)
	requireContains(t, admission, "--json isDraft,isPrerelease,tagName,targetCommitish", path)
	// No write-capable built-in token is exposed anywhere in admission. Because
	// the job is contents: read, github.token cannot mutate; assert it is not
	// wired in at all so a future permissions bump cannot silently re-grant it.
	if got := strings.Count(admission, "GH_TOKEN: ${{ github.token }}"); got != 0 {
		t.Errorf("%s built-in token mapping count = %d, want 0", path, got)
	}
	requireContains(t, admission,
		"- name: Require exact Release Please release state\n        id: release_state\n        env:\n          GH_TOKEN: ${{ secrets.CI_GITHUB_TOKEN }}",
		path)
	requireContains(t, admission, `"$tag_name" != "$GITHUB_REF_NAME" || "$target_commitish" != "$TAG_COMMIT"`, path)
	requireContains(t, admission, `"$is_draft" == "true" && "$is_prerelease" == "false"`, path)
	requireContains(t, admission, "public prerelease or ambiguous release state is invalid", path)
	requireContains(t, admission, "CI_GITHUB_TOKEN_SET: ${{ secrets.CI_GITHUB_TOKEN != '' }}", path)
	requireContains(t, admission, "CI_GITHUB_TOKEN: ${{ secrets.CI_GITHUB_TOKEN }}", path)
	requireContains(t, admission, "bash scripts/ci/require_immutable_releases.sh", path)
	requireNotContains(t, admission, "GH_TOKEN: ${{ github.token }}\n        run: bash scripts/ci/require_immutable_releases.sh", path)
	secretPresence := strings.Index(admission, "Verify release secrets are set")
	stableTag := strings.Index(admission, "Require stable SemVer tag")
	checkout := strings.Index(admission, "actions/checkout@")
	tagProvenance := strings.Index(admission, "Require tag commit on main")
	immutableGate := strings.Index(admission, "Require repository immutable releases")
	releaseState := strings.Index(admission, "Require exact Release Please release state")
	if secretPresence == -1 || stableTag == -1 || checkout == -1 ||
		tagProvenance == -1 || immutableGate == -1 || releaseState == -1 {
		t.Fatalf("%s missing one or more release-admission boundaries", path)
	}
	if secretPresence >= stableTag || stableTag >= checkout ||
		checkout >= tagProvenance || tagProvenance >= immutableGate ||
		immutableGate >= releaseState {
		t.Error("release-admission must validate secret presence and tag syntax, check out without secrets, prove exact protected-main provenance, and only then expose CI_GITHUB_TOKEN to repository code")
	}

	goreleaser := requireWorkflowJob(t, workflow, "goreleaser", path)
	requireContains(t, goreleaser, "needs: release-admission", path)
	requireNotContains(t, admission, "goreleaser/goreleaser-action@", path)
}

func TestImmutableReleaseAuthorityIsManagedAndInvokedExactlyThreeTimes(t *testing.T) {
	const workflowPath = ".github/workflows/release.yml"
	workflow := readRepositoryFile(t, workflowPath)
	if got := strings.Count(workflow, "bash scripts/ci/require_immutable_releases.sh"); got != 3 {
		t.Errorf("%s immutable-release gate count = %d, want 3", workflowPath, got)
	}
	if got := strings.Count(workflow,
		"CI_GITHUB_TOKEN: ${{ secrets.CI_GITHUB_TOKEN }}"); got != 1 {
		t.Errorf("%s immutable-release step credential mapping count = %d, want 1", workflowPath, got)
	}
	if got := strings.Count(workflow,
		`CI_GITHUB_TOKEN="${{ secrets.CI_GITHUB_TOKEN }}" \`); got != 2 {
		t.Errorf("%s immutable-release command credential count = %d, want 2", workflowPath, got)
	}
	requireNotContains(t, workflow,
		`"repos/${GITHUB_REPOSITORY}/immutable-releases"`,
		workflowPath)

	const helperPath = "scripts/ci/require_immutable_releases.sh"
	helper := readRepositoryFile(t, helperPath)
	if got := strings.Count(helper, `"repos/${GITHUB_REPOSITORY}/immutable-releases"`); got != 1 {
		t.Errorf("%s immutable-release endpoint count = %d, want 1", helperPath, got)
	}
	requireContains(t, helper,
		`GH_TOKEN="$CI_GITHUB_TOKEN" "$GH_BIN" api`,
		helperPath)
	requireNotContains(t, helper, "github.token", helperPath)
	requireNotContains(t, helper, "RELEASE_GH_TOKEN", helperPath)

	admission := requireWorkflowJob(t, workflow, "release-admission", workflowPath)
	publish := requireWorkflowJob(t, workflow, "publish-release", workflowPath)
	requireContains(t, admission,
		"CI_GITHUB_TOKEN: ${{ secrets.CI_GITHUB_TOKEN }}",
		workflowPath)
	requireNotContains(t, publish,
		"CI_GITHUB_TOKEN: ${{ secrets.CI_GITHUB_TOKEN }}",
		workflowPath)
}

func TestImmutableReleaseAuthorityPreflightIsDefaultBranchReadOnlyAndExactMain(t *testing.T) {
	const path = ".github/workflows/release-authority-preflight.yml"
	workflow := readRepositoryFile(t, path)

	requireContains(t, workflow, "push:", path)
	requireContains(t, workflow, "branches: [main]", path)
	requireContains(t, workflow, "repository_dispatch:", path)
	requireContains(t, workflow, "types: [release_authority_preflight]", path)
	requireNotContains(t, workflow, "workflow_dispatch:", path)
	requireNotContains(t, workflow, "pull_request:", path)
	requireContains(t, workflow, "permissions: {}", path)
	requireContains(t, workflow, "contents: read", path)
	requireContains(t, workflow, `"$GITHUB_REF" != "refs/heads/main"`, path)
	requireContains(t, workflow, `"$event_commit" != "$current_main"`, path)
	requireContains(t, workflow, "CI_GITHUB_TOKEN: ${{ secrets.CI_GITHUB_TOKEN }}", path)
	if got := strings.Count(workflow, "bash scripts/ci/require_immutable_releases.sh"); got != 1 {
		t.Errorf("%s immutable-release gate count = %d, want 1", path, got)
	}
	requireNotContains(t, workflow, "contents: write", path)
	requireNotContains(t, workflow, "releases/", path)
	requireNotContains(t, workflow, "release create", path)
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
	requireContains(t, script, `.body | strings`, scriptPath)
	requireContains(t, script,
		`ralph_validate_release_body_footer <<<"$release_body"`,
		scriptPath)
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
		"FAKE_UNSAFE_SCOOP_GUIDANCE",
		"FAKE_UNSAFE_SCOOP_SC_START",
		"FAKE_UNSAFE_SCOOP_START_SERVICE",
		"FAKE_UNSAFE_SCOOP_NATIVE_PROVIDERS",
		"FAKE_ALREADY_MERGED",
		"FAKE_INTERVENING_MAIN",
		"FAKE_SPLIT_PATH_OWNER",
		"FAKE_AMBIGUOUS_WINNER",
		"FAKE_SAME_PREFIX_UNSEALED",
		"FAKE_MULTIPLE_ATTEMPTS",
		"FAKE_MULTIPLE_EXACT_ATTEMPTS",
		"FAKE_UNSAFE_RELEASE_FOOTER",
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
	requireContains(t, signer, `gh release upload "$GITHUB_REF_NAME" --repo "$GITHUB_REPOSITORY"`, path)
	// 5 references: the admission presence check, the three immutable-release
	// gates, and the admission draft read (which use CI_GITHUB_TOKEN because the
	// built-in token is contents: read and cannot see draft releases).
	if got := strings.Count(workflow, "secrets.CI_GITHUB_TOKEN"); got != 5 {
		t.Errorf("%s CI_GITHUB_TOKEN secret references = %d, want 5", path, got)
	}
	verify := requireWorkflowJob(t, workflow, "verify-sealed-release", path)
	requireContains(t, verify, "contents: write", path)
	requireContains(t, verify, "RELEASE_GH_TOKEN: ${{ github.token }}", path)
	requireNotContains(t, verify, "secrets.CI_GITHUB_TOKEN", path)
	requireNotContains(t, workflow, "\nenv:\n  CI_GITHUB_TOKEN:", path)

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
	requireContains(t, buildDocs, "pnpm/action-setup@0977fd99725f1db4007ccb2928dbb4e90d06cc86", cdPath)
	requireContains(t, buildDocs, "actions/setup-node@", cdPath)
	requireContains(t, buildDocs, "make docs-check", cdPath)
	requireNotContains(t, buildDocs, "gomarkdoc", cdPath)
	requireNotContains(t, buildDocs, "tox", cdPath)
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
	requireContains(t, ci, "pnpm/action-setup@0977fd99725f1db4007ccb2928dbb4e90d06cc86", ciPath)
	requireNotContains(t, ci, "gomarkdoc", ciPath)
	requireContains(t, ci, "@anthropic-ai/claude-code@2.1.220", ciPath)
	requireContains(t, ci, "version: v2.12.2", ciPath)
	requireContains(t, ci, "actionlint@v1.7.12", ciPath)
	requireContains(t, ci, "govulncheck@v1.6.0", ciPath)
	requireContains(t, ci, "4b6d6bcb4819be4fe209e807726e83be12da3190", ciPath)
	packagingJob := requireWorkflowJob(t, ci, "packaging", ciPath)
	requireContains(t, packagingJob, "Validate primary GoReleaser config", ciPath)
	requireContains(t, packagingJob, "args: check\n", ciPath)
	requireContains(t, packagingJob, "Validate Chocolatey GoReleaser config", ciPath)
	requireContains(t, packagingJob,
		"args: check --config .goreleaser.chocolatey.yaml",
		ciPath)
	if got := strings.Count(packagingJob, "goreleaser/goreleaser-action@"); got != 2 {
		t.Errorf("%s packaging GoReleaser check count = %d, want 2", ciPath, got)
	}

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

	for _, retired := range []string{"tox.ini", "docs/requirements.lock", "docs/conf.py"} {
		if _, err := os.Stat(filepath.Join("..", "..", retired)); !os.IsNotExist(err) {
			t.Errorf("retired Sourcey predecessor %s must be absent, stat error = %v", retired, err)
		}
	}
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

	immutable := readRepositoryFile(t, "scripts/ci/require_immutable_releases.sh")
	requireContains(t, immutable, "CI_GITHUB_TOKEN is not configured", "scripts/ci/require_immutable_releases.sh")
	requireContains(t, immutable, "Administration read", "scripts/ci/require_immutable_releases.sh")
	requireContains(t, immutable, `GH_TOKEN="$CI_GITHUB_TOKEN"`, "scripts/ci/require_immutable_releases.sh")
	requireNotContains(t, immutable, `${GH_TOKEN:-`, "scripts/ci/require_immutable_releases.sh")
	requireNotContains(t, immutable, "RELEASE_GH_TOKEN", "scripts/ci/require_immutable_releases.sh")
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

func TestWindowsPackageGuidanceUsesOneExactFailClosedContract(t *testing.T) {
	const helperPath = "scripts/ci/package_guidance_contract.sh"
	helper := readRepositoryFile(t, helperPath)
	for _, clause := range []string{
		"radioactive_ralph --supervisor",
		"Native Windows SCM install/start and provider-backed workers are disabled.",
		"Linux build inside WSL2",
		"RALPH_NATIVE_WINDOWS_PACKAGE_SHORT_DESCRIPTION_CONTRACT",
		"RALPH_NATIVE_WINDOWS_PACKAGE_LONG_DESCRIPTION_CONTRACT",
		"RALPH_GORELEASER_FOOTER_CONTRACT",
		".description == $description",
		".post_install == $post_install",
		"ralph_validate_winget_config_contract",
		"ralph_validate_chocolatey_config_contract",
	} {
		requireContains(t, helper, clause, helperPath)
	}

	for _, path := range []string{
		"scripts/ci/smoke_goreleaser_artifacts.sh",
		"scripts/ci/wait_for_package_publication.sh",
	} {
		content := readRepositoryFile(t, path)
		requireContains(t, content, "package_guidance_contract.sh", path)
		requireContains(t, content, "ralph_validate_scoop_manifest_contract", path)
		requireNotContains(t, content, `contains("service install")`, path)
	}

	const behaviorPath = "scripts/ci/test_package_guidance_contract.sh"
	behavior := readRepositoryFile(t, behaviorPath)
	for _, adversary := range []string{
		"sc.exe start radioactive_ralph-supervisor",
		"Start-Service radioactive_ralph-supervisor",
		"Native Windows provider workers are supported.",
		"Supervised-execution runtime for local AI-agent CLIs",
		"Native Windows SCM install/start and provider-backed workers are supported.",
		"ralph_validate_winget_config_contract",
		"ralph_validate_chocolatey_config_contract",
		"ralph_validate_goreleaser_release_footer",
	} {
		requireContains(t, behavior, adversary, behaviorPath)
	}

	const publicationPath = "scripts/ci/wait_for_package_publication.sh"
	publication := readRepositoryFile(t, publicationPath)
	requireContains(t, publication, `.body | strings`, publicationPath)
	requireContains(t, publication,
		`ralph_validate_release_body_footer <<<"$release_body"`,
		publicationPath)

	const chocolateyPath = ".goreleaser.chocolatey.yaml"
	chocolatey := readRepositoryFile(t, chocolateyPath)
	requireContains(t, chocolatey,
		`url_template: "https://github.com/jbcom/radioactive-ralph/releases/download/{{ .Tag }}/{{ .ArtifactName }}"`,
		chocolateyPath)
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
		"scripts/ci/test_require_immutable_releases.sh",
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
