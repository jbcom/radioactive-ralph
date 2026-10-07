#!/usr/bin/env bash

set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT
REMOTE="$TMP/pkgs.git"
SEED="$TMP/seed"
SOURCE="$TMP/generated"
STATE="$TMP/state"
FAKE_GH="$TMP/gh"
BRANCH="chore/update-radioactive-ralph-1.2.3"
REF="refs/heads/$BRANCH"

git init -q --bare "$REMOTE"
git init -q "$SEED"
git -C "$SEED" config user.name test
git -C "$SEED" config user.email test@example.invalid
mkdir -p "$SEED/scripts" "$SEED/src/data/directory"
printf 'seed\n' > "$SEED/README.md"
cat > "$SEED/scripts/generate-directory.mjs" <<'GENERATOR'
import { mkdir, readdir, writeFile } from 'node:fs/promises';

const casks = (await readdir('Casks')).sort();
const manifests = (await readdir('bucket')).sort();
await mkdir('src/data/directory', { recursive: true });
await writeFile(
  'src/data/directory/directory.json',
  `${JSON.stringify({ casks, manifests }, null, 2)}\n`,
);
GENERATOR
printf '{"casks":[],"manifests":[]}\n' > "$SEED/src/data/directory/directory.json"
git -C "$SEED" add README.md
git -C "$SEED" add scripts/generate-directory.mjs src/data/directory/directory.json
git -C "$SEED" commit -q -m seed
git -C "$SEED" branch -M main
git -C "$SEED" remote add origin "$REMOTE"
git -C "$SEED" push -q origin main
git --git-dir="$REMOTE" symbolic-ref HEAD refs/heads/main

mkdir -p "$SOURCE/homebrew/Casks" "$SOURCE/scoop/bucket" "$STATE"
printf 'generated CLI cask\n' > "$SOURCE/homebrew/Casks/radioactive-ralph.rb"
printf '{"version":"1.2.3"}\n' > "$SOURCE/scoop/bucket/radioactive-ralph.json"

cat > "$FAKE_GH" <<'FAKEGH'
#!/usr/bin/env bash
set -euo pipefail

if [[ "${1:-}" == "release" && "${2:-}" == "view" ]]; then
  printf '%s\n' '{"assets":[{"name":"package-manifests.tar.gz","apiUrl":"assets/package-manifests.tar.gz"},{"name":"package-manifests.tar.gz.sigstore.json","apiUrl":"assets/package-manifests.tar.gz.sigstore.json"}]}'
  exit 0
fi

if [[ "${1:-}" == "api" ]]; then
  pattern="${*: -1}"
  pattern="${pattern#assets/}"
  if [[ "$pattern" == "package-manifests.tar.gz" ]]; then
    payload="$(mktemp -d)"
    mkdir -p "$payload/Casks" "$payload/bucket"
    printf 'generated CLI cask\n' > "$payload/Casks/radioactive-ralph.rb"
    printf 'cask "radioactive-ralph-gui" do\nend\n' \
      > "$payload/Casks/radioactive-ralph-gui.rb"
    printf '{"version":"1.2.3"}\n' > "$payload/bucket/radioactive-ralph.json"
    tar -czf - -C "$payload" \
      Casks/radioactive-ralph.rb Casks/radioactive-ralph-gui.rb \
      bucket/radioactive-ralph.json
    rm -rf "$payload"
  else
    printf '{"fixture":true}\n'
  fi
  exit 0
fi

if [[ "${1:-}" == "pr" && "${2:-}" == "create" ]]; then
  count_file="$FAKE_STATE/pr-create-count"
  count=0
  [[ -e "$count_file" ]] && count="$(<"$count_file")"
  count=$((count + 1))
  printf '%s\n' "$count" > "$count_file"
  if ((count > 1)); then
    echo "a pull request already exists" >&2
    exit 1
  fi
  exit 0
fi

if [[ "${1:-}" == "pr" && "${2:-}" == "list" ]]; then
  printf '%s\n' '[{"number":42,"headRepositoryOwner":{"login":"jbcom"},"isCrossRepository":false}]'
  exit 0
fi

if [[ "${1:-}" == "pr" && "${2:-}" == "merge" ]]; then
  [[ "${3:-}" == "42" ]]
  [[ "$*" == *"--auto --merge"* ]]
  printf '%s\n' merged >> "$FAKE_STATE/pr-merge-log"
  exit 0
fi

echo "fake gh: unexpected invocation: $*" >&2
exit 1
FAKEGH
chmod +x "$FAKE_GH"
cat > "$TMP/cosign" <<'FAKECOSIGN'
#!/usr/bin/env bash
set -euo pipefail
[[ "$*" == *"verify-blob"* ]]
[[ "$*" == *"release.yml@refs/tags/v1.2.3"* ]]
FAKECOSIGN
chmod +x "$TMP/cosign"

run_publisher() {
    GH_BIN="$FAKE_GH" \
    COSIGN_BIN="$TMP/cosign" \
    PKGS_GH_TOKEN=test-pkgs-token \
    RELEASE_GH_TOKEN=test-release-token \
    FAKE_STATE="$STATE" \
    SOURCE_ROOT="$SOURCE" \
    PKGS_CLONE_URL="$REMOTE" \
    bash "$ROOT/packaging/publish-cli-manifests.sh" 1.2.3
}

# A legacy broad GH_TOKEN never substitutes for either named authority.
if GH_BIN="$FAKE_GH" COSIGN_BIN="$TMP/cosign" GH_TOKEN=legacy-broad \
  RELEASE_GH_TOKEN=test-release-token \
  SOURCE_ROOT="$SOURCE" PKGS_CLONE_URL="$REMOTE" \
  bash "$ROOT/packaging/publish-cli-manifests.sh" 1.2.3 \
  >/dev/null 2>&1; then
  echo "expected missing PKGS_GH_TOKEN to fail despite GH_TOKEN" >&2
  exit 1
fi
if GH_BIN="$FAKE_GH" COSIGN_BIN="$TMP/cosign" GH_TOKEN=legacy-broad \
  PKGS_GH_TOKEN=test-pkgs-token \
  SOURCE_ROOT="$SOURCE" PKGS_CLONE_URL="$REMOTE" \
  bash "$ROOT/packaging/publish-cli-manifests.sh" 1.2.3 \
  >/dev/null 2>&1; then
  echo "expected missing RELEASE_GH_TOKEN to fail despite GH_TOKEN" >&2
  exit 1
fi

run_publisher
first_branch="$(git --git-dir="$REMOTE" rev-parse "$REF")"
[[ "$(<"$STATE/pr-create-count")" == "1" ]]
[[ "$(wc -l < "$STATE/pr-merge-log" | tr -d ' ')" == "1" ]]
git --git-dir="$REMOTE" show "$first_branch:Casks/radioactive-ralph-gui.rb" |
  grep -F 'cask "radioactive-ralph-gui"' >/dev/null
git --git-dir="$REMOTE" show "$first_branch:src/data/directory/directory.json" |
  grep -F 'radioactive-ralph-gui.rb' >/dev/null

mv "$SOURCE" "$TMP/generated-initial"
mkdir -p "$SOURCE"
run_publisher
second_branch="$(git --git-dir="$REMOTE" rev-parse "$REF")"
[[ -n "$second_branch" ]]
[[ "$(<"$STATE/pr-create-count")" == "2" ]]
[[ "$(wc -l < "$STATE/pr-merge-log" | tr -d ' ')" == "2" ]]

git --git-dir="$REMOTE" update-ref refs/heads/main "$second_branch"
run_publisher
third_branch="$(git --git-dir="$REMOTE" rev-parse "$REF")"
[[ "$third_branch" == "$second_branch" ]]
[[ "$(<"$STATE/pr-create-count")" == "2" ]]
[[ "$(wc -l < "$STATE/pr-merge-log" | tr -d ' ')" == "2" ]]
[[ -n "$first_branch" ]]

echo "CLI manifest rerun tests: ok"
