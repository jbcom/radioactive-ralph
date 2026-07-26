#!/usr/bin/env bash

set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

cat > "$TMP/gh" <<'FAKEGH'
#!/usr/bin/env bash
set -euo pipefail

command_name="${1:?}"
shift

if [[ "$command_name" == "release" && "${1:-}" == "download" ]]; then
  pattern=""
  output=""
  while (($#)); do
    case "$1" in
      --pattern) pattern="${2:?}"; shift 2 ;;
      --output) output="${2:?}"; shift 2 ;;
      *) shift ;;
    esac
  done
  printf '%s\n' "$pattern" >> "$FAKE_STATE_DIR/download.log"
  if [[ "${FAKE_MISSING_ASSET:-0}" == "1" &&
        "$pattern" == "radioactive_ralph_${FAKE_VERSION:?}_linux_arm64.tar.gz" ]]; then
    exit 1
  fi
  [[ -n "$output" ]] || {
    echo "fake gh: release output path missing" >&2
    exit 1
  }
  release_sha='e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855'
  case "$pattern" in
    checksums.txt)
      {
        for platform in darwin_amd64 darwin_arm64 linux_amd64 linux_arm64; do
          printf '%s  radioactive_ralph_%s_%s.tar.gz\n' \
            "$release_sha" "${FAKE_VERSION:?}" "$platform"
        done
        printf '%s  radioactive_ralph_%s_windows_amd64.zip\n' \
          "$release_sha" "${FAKE_VERSION:?}"
        for format in deb rpm; do
          for arch in amd64 arm64; do
            printf '%s  radioactive-ralph_%s_linux_%s.%s\n' \
              "$release_sha" "${FAKE_VERSION:?}" "$arch" "$format"
          done
        done
      } > "$output"
      ;;
    checksums.txt.sigstore.json)
      printf '{"fixture":"sigstore bundle"}\n' > "$output"
      ;;
    gui-checksums.txt)
      {
        printf '%s  radioactive-ralph_%s_darwin_amd64.dmg\n' \
          "$release_sha" "${FAKE_VERSION:?}"
        printf '%s  radioactive-ralph_%s_darwin_arm64.dmg\n' \
          "$release_sha" "${FAKE_VERSION:?}"
        printf '%s  radioactive-ralph_%s_linux_x86_64.AppImage\n' \
          "$release_sha" "${FAKE_VERSION:?}"
        printf '%s  radioactive-ralph.exe\n' "$release_sha"
      } > "$output"
      ;;
    gui-checksums.txt.sigstore.json)
      printf '{"fixture":"gui sigstore bundle"}\n' > "$output"
      ;;
    package-manifests.tar.gz)
      payload="$(mktemp -d)"
      mkdir -p "$payload/Casks" "$payload/bucket"
      clean_env=(
        -u FAKE_BAD_HOST
        -u FAKE_BAD_ARTIFACT
        -u FAKE_GUI_BAD_HOST
        -u FAKE_GUI_BAD_ARTIFACT
        -u FAKE_BAD_POSTFLIGHT
        -u FAKE_CLI_HOOK
        -u FAKE_GUI_HOOK
        -u FAKE_SCOOP_PRE_INSTALL
        -u FAKE_SCOOP_INSTALLER
        -u FAKE_CLI_BAD_HASH
        -u FAKE_GUI_BAD_HASH
        -u FAKE_TAMPER_MAIN
      )
      env "${clean_env[@]}" "$0" api \
        "repos/jbcom/pkgs/contents/Casks/radioactive-ralph.rb?ref=cli-oid-1" \
        > "$payload/Casks/radioactive-ralph.rb"
      env "${clean_env[@]}" "$0" api \
        "repos/jbcom/pkgs/contents/Casks/radioactive-ralph-gui.rb?ref=cli-oid-1" \
        > "$payload/Casks/radioactive-ralph-gui.rb"
      env "${clean_env[@]}" "$0" api \
        "repos/jbcom/pkgs/contents/bucket/radioactive-ralph.json?ref=cli-oid-1" \
        > "$payload/bucket/radioactive-ralph.json"
      tar -czf "$output" -C "$payload" \
        Casks/radioactive-ralph.rb \
        Casks/radioactive-ralph-gui.rb \
        bucket/radioactive-ralph.json
      rm -rf "$payload"
      ;;
    package-manifests.tar.gz.sigstore.json)
      printf '{"fixture":"package sigstore bundle"}\n' > "$output"
      ;;
    radioactive_ralph_*.tar.gz|radioactive_ralph_*.zip|radioactive-ralph_*.deb|radioactive-ralph_*.rpm)
      if [[ "${FAKE_STALE_CLI_ASSET:-0}" == "1" ]]; then
        printf 'stale CLI bytes\n' > "$output"
      else
        : > "$output"
      fi
      ;;
    radioactive-ralph_*.dmg|radioactive-ralph_*.AppImage|radioactive-ralph.exe)
      if [[ "${FAKE_CLOBBERED_GUI_ASSET:-0}" == "1" ]]; then
        printf 'clobbered GUI bytes\n' > "$output"
      else
        : > "$output"
      fi
      ;;
    *)
      echo "fake gh: unexpected release asset $pattern" >&2
      exit 1
      ;;
  esac
  exit 0
fi

if [[ "$command_name" == "api" ]]; then
  request="${*: -1}"
  if [[ " $* " == *" --method PATCH "* ]]; then
    touch "$FAKE_STATE_DIR/patch-called"
    printf '{"patched":true}\n'
    exit 0
  fi
  if [[ "$request" == "repos/jbcom/radioactive-ralph/contents/.release-please-manifest.json?ref=main" ]]; then
    printf '{".":"%s"}\n' "${FAKE_VERSION:?}"
    exit 0
  fi
  if [[ "$request" == "repos/jbcom/radioactive-ralph/releases/tags/v${FAKE_VERSION:?}" ]]; then
    published_at="2026-07-26T04:00:00Z"
    if [[ "${FAKE_MISSING_PUBLISHED_AT:-0}" == "1" ]]; then
      published_at=""
    elif [[ "${FAKE_INVALID_PUBLISHED_AT:-0}" == "1" ]]; then
      published_at="not-a-timestamp"
    fi
    jq -cn --arg tag "v${FAKE_VERSION}" --arg published "$published_at" \
      '{
        draft: false,
        prerelease: false,
        immutable: true,
        tag_name: $tag,
        published_at: $published
      }'
    exit 0
  fi
  if [[ "$request" == repos/jbcom/pkgs/commits/*/check-runs* ]]; then
    head_oid="${request#repos/jbcom/pkgs/commits/}"
    head_oid="${head_oid%%/*}"
    printf '%s\n' "$head_oid" > "$FAKE_STATE_DIR/current-check-head"
    validate_app_id=15368
    validate_app_slug=github-actions
    if [[ "${FAKE_SPOOFED_CHECK_APP:-0}" == "1" ]]; then
      validate_app_id=99999
      validate_app_slug=attacker
    fi
    build_check=',{"name":"build-site","status":"completed","conclusion":"success","details_url":"https://github.com/jbcom/pkgs/actions/runs/1002/job/2002","app":{"id":15368,"slug":"github-actions"}}'
    if [[ "${FAKE_MISSING_REQUIRED:-0}" == "1" ]]; then
      build_check=""
    fi
    printf '{"check_runs":[{"name":"validate","status":"completed","conclusion":"success","details_url":"https://github.com/jbcom/pkgs/actions/runs/1001/job/2001","app":{"id":%s,"slug":"%s"}}%s],"fixture_head":"%s"}\n' \
      "$validate_app_id" "$validate_app_slug" "$build_check" "$head_oid"
    exit 0
  fi
  if [[ "$request" == "repos/jbcom/pkgs/actions/runs/1001" ||
        "$request" == "repos/jbcom/pkgs/actions/runs/1002" ]]; then
    if [[ "$request" == *1001 ]]; then
      workflow_name="Validate packages"
      workflow_path=".github/workflows/validate-packages.yml"
    else
      workflow_name="CI"
      workflow_path=".github/workflows/ci.yml"
    fi
    if [[ "${FAKE_WRONG_WORKFLOW_PATH:-0}" == "1" ]]; then
      workflow_path=".github/workflows/spoof.yml"
    fi
    run_head="${FAKE_VIEW_HEAD_OID:-cli-oid-1}"
    if [[ -e "$FAKE_STATE_DIR/current-check-head" ]]; then
      run_head="$(<"$FAKE_STATE_DIR/current-check-head")"
    fi
    if [[ "${FAKE_WRONG_WORKFLOW_HEAD:-0}" == "1" ]]; then
      run_head="attacker-oid"
    fi
    printf '{"name":"%s","path":"%s","event":"pull_request","head_sha":"%s","status":"completed","conclusion":"success","repository":{"full_name":"jbcom/pkgs"},"head_repository":{"full_name":"jbcom/pkgs"}}\n' \
      "$workflow_name" "$workflow_path" "$run_head"
    exit 0
  fi
  if [[ "$request" == "repos/jbcom/pkgs/git/ref/heads/main" ]]; then
    if [[ "${FAKE_MAIN_REF_FAILURE:-0}" == "1" ]]; then
      echo "fake gh: main ref unavailable" >&2
      exit 1
    fi
    main_oid='1111111111111111111111111111111111111111'
    if [[ "${FAKE_INTERVENING_MAIN:-0}" == "1" ]]; then
      main_oid='2222222222222222222222222222222222222222'
    fi
    if [[ "${FAKE_MAIN_RACE:-0}" == "1" ]]; then
      main_count_file="$FAKE_STATE_DIR/main-ref-count"
      main_count=0
      [[ -e "$main_count_file" ]] && main_count="$(<"$main_count_file")"
      main_count=$((main_count + 1))
      printf '%s\n' "$main_count" > "$main_count_file"
      if ((main_count % 2 == 0)); then
        main_oid='2222222222222222222222222222222222222222'
      fi
    fi
    printf '%s\n' "$main_oid"
    exit 0
  fi
  if [[ "$request" == "repos/jbcom/pkgs/commits" ]]; then
    path=""
    previous=""
    for arg in "$@"; do
      if [[ "$previous" == "--raw-field" && "$arg" == path=* ]]; then
        path="${arg#path=}"
      fi
      previous="$arg"
    done
    merge_oid='aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa'
    if [[ "$path" == "Casks/radioactive-ralph-gui.rb" ]] &&
      { [[ "${FAKE_SPLIT_PATH_OWNER:-0}" == "1" ]] ||
        [[ -e "$FAKE_STATE_DIR/final-ownership-drift" ]]; }; then
      merge_oid='bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb'
    fi
    printf '%s\n' "$merge_oid"
    exit 0
  fi
  cli_version="${FAKE_OLD_VERSION:?}"
  gui_version="${FAKE_OLD_VERSION:?}"
  scoop_version="${FAKE_OLD_VERSION:?}"
  if [[ (-e "$FAKE_STATE_DIR/cli-merged" ||
         "${FAKE_ALREADY_MERGED:-0}" == "1") &&
        "${FAKE_IGNORE_MERGE:-0}" != "1" ]]; then
    cli_version="${FAKE_VERSION:?}"
    scoop_version="${FAKE_VERSION:?}"
  fi
  if [[ (-e "$FAKE_STATE_DIR/gui-merged" ||
         "${FAKE_ALREADY_MERGED:-0}" == "1") &&
        "${FAKE_IGNORE_MERGE:-0}" != "1" ]]; then
    gui_version="${FAKE_VERSION:?}"
  fi
  if [[ "$request" == *"?ref=cli-oid-"* ]]; then
    cli_version="${FAKE_VERSION:?}"
    scoop_version="${FAKE_VERSION:?}"
    gui_version="${FAKE_VERSION:?}"
  fi
  if [[ "$request" == *"?ref=gui-oid-"* ]]; then
    gui_version="${FAKE_VERSION:?}"
  fi
  if [[ "$request" == *"?ref=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"* ]]; then
    cli_version="${FAKE_VERSION:?}"
    scoop_version="${FAKE_VERSION:?}"
    gui_version="${FAKE_VERSION:?}"
  fi
  if [[ "$request" == *"?ref=cccccccccccccccccccccccccccccccccccccccc"* &&
        "${FAKE_MULTIPLE_EXACT_ATTEMPTS:-0}" == "1" ]]; then
    cli_version="${FAKE_VERSION:?}"
    scoop_version="${FAKE_VERSION:?}"
    gui_version="${FAKE_VERSION:?}"
  fi
  sha='e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855'
  cli_sha="$sha"
  gui_sha="$sha"
  scoop_sha="$sha"
  if [[ "${FAKE_CLI_BAD_HASH:-0}" == "1" ]]; then
    cli_sha='bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb'
    scoop_sha="$cli_sha"
  fi
  if [[ "${FAKE_GUI_BAD_HASH:-0}" == "1" ]]; then
    gui_sha='bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb'
  fi
  release_host="https://github.com/jbcom/radioactive-ralph"
  if [[ "${FAKE_BAD_HOST:-0}" == "1" ]]; then
    release_host="https://attacker.invalid/jbcom/radioactive-ralph"
  fi
  gui_release_host="$release_host"
  if [[ "${FAKE_GUI_BAD_HOST:-0}" == "1" ]]; then
    gui_release_host="https://attacker.invalid/jbcom/radioactive-ralph"
  fi
  cli_darwin_amd64='radioactive_ralph_#{version}_darwin_amd64.tar.gz'
  if [[ "${FAKE_BAD_ARTIFACT:-0}" == "1" ]]; then
    cli_darwin_amd64='radioactive_ralph_#{version}_darwin_amd64.zip'
  fi
  if [[ "${FAKE_TAMPER_MAIN:-0}" == "1" &&
        ("$request" == *"?ref=main"* || "$request" == *"?ref=1111111111111111111111111111111111111111"*) ]]; then
    cli_darwin_amd64='radioactive_ralph_#{version}_darwin_amd64.zip'
  fi
  gui_arm64='radioactive-ralph_#{version}_darwin_arm64.dmg'
  if [[ "${FAKE_GUI_BAD_ARTIFACT:-0}" == "1" ]]; then
    gui_arm64='radioactive-ralph_#{version}_darwin_arm64.zip'
  fi
  quarantine_app='#{appdir}/radioactive-ralph.app'
  if [[ "${FAKE_BAD_POSTFLIGHT:-0}" == "1" ]]; then
    quarantine_app='#{appdir}/unrelated.app'
  fi
  case "$request" in
    *Casks/radioactive-ralph.rb*)
      cli_cask="$(cat <<CASK
# This file was generated by GoReleaser. DO NOT EDIT.
cask "radioactive-ralph" do
  version "${cli_version}"

  on_macos do
    on_intel do
      sha256 "${cli_sha}"
      url "${release_host}/releases/download/v${cli_version}/${cli_darwin_amd64}"
    end
    on_arm do
      sha256 "${cli_sha}"
      url "${release_host}/releases/download/v${cli_version}/radioactive_ralph_#{version}_darwin_arm64.tar.gz"
    end
  end

  on_linux do
    on_intel do
      sha256 "${cli_sha}"
      url "${release_host}/releases/download/v${cli_version}/radioactive_ralph_#{version}_linux_amd64.tar.gz"
    end
    on_arm do
      sha256 "${cli_sha}"
      url "${release_host}/releases/download/v${cli_version}/radioactive_ralph_#{version}_linux_arm64.tar.gz"
    end
  end

  name "radioactive-ralph"
  desc "Supervised-execution runtime for local AI-agent CLIs"
  homepage "https://github.com/jbcom/radioactive-ralph"

  livecheck do
    skip "Auto-generated on release."
  end

  binary "radioactive_ralph"

  # No zap stanza required

  caveats <<~EOS
    Next step — start the supervisor, then register a project:

      radioactive_ralph service install
      radioactive_ralph --init

    Run \`radioactive_ralph\` inside a registered project for the
    read-only TUI once the supervisor is running. Full docs:

      https://jonbogaty.com/radioactive-ralph/getting-started/
  EOS
end
CASK
)"
      if [[ "${FAKE_CLI_HOOK:-0}" == "1" ]]; then
        cli_cask="${cli_cask/  binary \"radioactive_ralph\"/  preflight do
    system_command \"\\/usr\\/bin\\/touch\", args: [\"\\/tmp\\/owned\"]
  end

  binary \"radioactive_ralph\"}"
      fi
      printf '%s\n' "$cli_cask"
      ;;
    *Casks/radioactive-ralph-gui.rb*)
      gui_cask="$(cat <<CASK
cask "radioactive-ralph-gui" do
  version "${gui_version}"

  on_arm do
    sha256 "${gui_sha}"
    url "${gui_release_host}/releases/download/v#{version}/${gui_arm64}"
  end
  on_intel do
    sha256 "${gui_sha}"
    url "${gui_release_host}/releases/download/v#{version}/radioactive-ralph_#{version}_darwin_amd64.dmg"
  end

  name "radioactive-ralph"
  desc "Supervised-execution runtime for local AI-agent CLIs"
  homepage "https://github.com/jbcom/radioactive-ralph"

  app "radioactive-ralph.app"

  # The app is ad-hoc signed (free, no Apple Developer cert), so Gatekeeper
  # would quarantine it on first launch. Strip the quarantine attribute after
  # install so it opens cleanly — the standard OSS-cask approach for an
  # un-notarized app. (Homebrew does NOT remove quarantine by default.)
  postflight do
    system_command "/usr/bin/xattr",
                   args: ["-dr", "com.apple.quarantine", "${quarantine_app}"],
                   sudo: false
  end

  caveats <<~EOS
    Install the CLI cask, start the supervisor, and register a project:

      brew install --cask radioactive-ralph
      radioactive_ralph service install
      cd /path/to/repo && radioactive_ralph --init

    The desktop app and the terminal UI are peers on the same local supervisor.
  EOS
end
CASK
)"
      if [[ "${FAKE_GUI_HOOK:-0}" == "1" ]]; then
        gui_cask="${gui_cask/  app \"radioactive-ralph.app\"/  app \"radioactive-ralph.app\"

  preflight do
    system_command \"\\/usr\\/bin\\/touch\", args: [\"\\/tmp\\/owned\"]
  end}"
      fi
      printf '%s\n' "$gui_cask"
      ;;
    *bucket/radioactive-ralph.json*)
      scoop_artifact="radioactive_ralph_${scoop_version}_windows_amd64.zip"
      if [[ "${FAKE_BAD_ARTIFACT:-0}" == "1" ]]; then
        scoop_artifact="radioactive_ralph_${scoop_version}_windows_arm64.zip"
      fi
      scoop="$(jq -cn \
        --arg version "$scoop_version" \
        --arg url "${release_host}/releases/download/v${scoop_version}/${scoop_artifact}" \
        --arg sha "$scoop_sha" \
        '{
          version: $version,
          architecture: {
            "64bit": {
              url: $url,
              bin: ["radioactive_ralph.exe"],
              hash: $sha
            }
          },
          homepage: "https://github.com/jbcom/radioactive-ralph",
          license: "MIT",
          description: "Supervised-execution runtime for local AI-agent CLIs",
          post_install: [
            "Write-Host \u0027Next step — start the supervisor, then register a project:\u0027",
            "Write-Host \u0027  radioactive_ralph service install\u0027",
            "Write-Host \u0027  radioactive_ralph --init\u0027",
            "Write-Host \u0027See https://jonbogaty.com/radioactive-ralph/getting-started/ for the full setup flow.\u0027"
          ]
        }')"
      if [[ "${FAKE_SCOOP_PRE_INSTALL:-0}" == "1" ]]; then
        scoop="$(jq -c '. + {pre_install: ["Write-Host owned"]}' <<< "$scoop")"
      elif [[ "${FAKE_SCOOP_INSTALLER:-0}" == "1" ]]; then
        scoop="$(jq -c '. + {installer: {script: "Write-Host owned"}}' <<< "$scoop")"
      fi
      printf '%s\n' "$scoop"
      ;;
  esac
  exit 0
fi

if [[ "$command_name" == "pr" && "${1:-}" == "list" ]]; then
  branch=""
  state=""
  while (($#)); do
    case "$1" in
      --head) branch="${2:?}"; shift 2 ;;
      --state) state="${2:?}"; shift 2 ;;
      *) shift ;;
    esac
  done
  if [[ "$state" == "merged" ]]; then
    if [[ ! -e "$FAKE_STATE_DIR/cli-merged" &&
          "${FAKE_ALREADY_MERGED:-0}" != "1" ]]; then
      printf '[]\n'
      exit 0
    fi
    winning_branch="chore/update-radioactive-ralph-${FAKE_VERSION:?}"
    if [[ "${FAKE_WINNING_ATTEMPT:-0}" == "1" ||
          "${FAKE_MULTIPLE_EXACT_ATTEMPTS:-0}" == "1" ]]; then
      winning_branch+="-attempt-2"
    fi
    if [[ "${FAKE_SAME_PREFIX_UNSEALED:-0}" == "1" ]]; then
      winning_branch="chore/update-radioactive-ralph-${FAKE_VERSION}0"
    fi
    winning_head="cli-oid-1"
    if [[ "${FAKE_HEAD_CHANGE:-0}" == "1" ]]; then
      winning_head="cli-oid-2"
    fi
    winning_merged_at="2026-07-26T03:30:00Z"
    if [[ "${FAKE_SINGLE_AT_PUBLICATION:-0}" == "1" ]]; then
      winning_merged_at="2026-07-26T04:00:00Z"
    elif [[ "${FAKE_POST_PUBLIC_ATTEMPT:-0}" == "1" ]]; then
      winning_merged_at="2026-07-26T04:30:00Z"
    elif [[ "${FAKE_MISSING_MERGED_AT:-0}" == "1" ]]; then
      winning_merged_at=""
    elif [[ "${FAKE_INVALID_MERGED_AT:-0}" == "1" ]]; then
      winning_merged_at="not-a-timestamp"
    fi
    candidate="$(jq -cn \
      --arg branch "$winning_branch" \
      --arg head "$winning_head" \
      --arg merged_at "$winning_merged_at" \
      '{
        number: 101,
        baseRefName: "main",
        headRefName: $branch,
        headRefOid: $head,
        headRepositoryOwner: {login: "jbcom"},
        isCrossRepository: false,
        files: [
          {path: "Casks/radioactive-ralph.rb"},
          {path: "Casks/radioactive-ralph-gui.rb"},
          {path: "bucket/radioactive-ralph.json"}
        ],
        mergeCommit: {oid: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
        mergedAt: $merged_at
      }')"
    if [[ "${FAKE_MULTIPLE_ATTEMPTS:-0}" == "1" ||
          "${FAKE_MULTIPLE_EXACT_ATTEMPTS:-0}" == "1" ]]; then
      old_merged_at="2026-07-26T03:00:00Z"
      if [[ "${FAKE_EQUAL_MERGED_AT:-0}" == "1" ]]; then
        old_merged_at="$winning_merged_at"
      fi
      old="$(jq -cn \
        --arg branch "chore/update-radioactive-ralph-${FAKE_VERSION}-attempt-1" \
        --arg merged_at "$old_merged_at" \
        '{
          number: 100,
          baseRefName: "main",
          headRefName: $branch,
          headRefOid: "old-head",
          headRepositoryOwner: {login: "jbcom"},
          isCrossRepository: false,
          files: [
            {path: "Casks/radioactive-ralph.rb"},
            {path: "Casks/radioactive-ralph-gui.rb"},
            {path: "bucket/radioactive-ralph.json"}
          ],
          mergeCommit: {oid: "cccccccccccccccccccccccccccccccccccccccc"},
          mergedAt: $merged_at
        }')"
      jq -cn --argjson old "$old" --argjson winning "$candidate" \
        '[$old, $winning]'
    elif [[ "${FAKE_AMBIGUOUS_WINNER:-0}" == "1" ]]; then
      jq -cn --argjson winning "$candidate" \
        '[$winning, ($winning | .number = 102)]'
    else
      jq -cn --argjson winning "$candidate" '[$winning]'
    fi
    exit 0
  fi
  if [[ "${FAKE_ALREADY_MERGED:-0}" == "1" ]]; then
    printf '[]\n'
    exit 0
  fi
  owner="jbcom"
  cross_repository=false
  if [[ "${FAKE_UNTRUSTED:-0}" == "1" ]]; then
    owner="mallory"
    cross_repository=true
  fi
  head_oid="cli-oid-1"
  if [[ "${FAKE_AMBIGUOUS:-0}" == "1" ]]; then
    printf '[{"number":101,"baseRefName":"main","headRefName":"%s","headRefOid":"%s","headRepositoryOwner":{"login":"%s"},"isCrossRepository":%s,"state":"OPEN","url":"https://example.test/101"},{"number":102,"baseRefName":"main","headRefName":"%s","headRefOid":"%s","headRepositoryOwner":{"login":"%s"},"isCrossRepository":%s,"state":"OPEN","url":"https://example.test/102"}]\n' \
      "$branch" "$head_oid" "$owner" "$cross_repository" \
      "$branch" "$head_oid" "$owner" "$cross_repository"
  else
    printf '[{"number":101,"baseRefName":"main","headRefName":"%s","headRefOid":"%s","headRepositoryOwner":{"login":"%s"},"isCrossRepository":%s,"state":"OPEN","url":"https://example.test/101"}]\n' \
      "$branch" "$head_oid" "$owner" "$cross_repository"
  fi
  exit 0
fi

if [[ "$command_name" == "pr" && "${1:-}" == "view" ]]; then
  number="${2:?}"
  branch="chore/update-radioactive-ralph-${FAKE_VERSION:?}"
  head_oid="cli-oid-1"
  if [[ "${FAKE_HEAD_CHANGE:-0}" == "1" ]]; then
    head_oid="cli-oid-2"
  fi
  printf '%s\n' "$head_oid" > "$FAKE_STATE_DIR/current-check-head"

  count_file="$FAKE_STATE_DIR/check-${number}-count"
  count=0
  [[ -e "$count_file" ]] && count="$(<"$count_file")"
  count=$((count + 1))
  printf '%s\n' "$count" > "$count_file"

  validate_status="COMPLETED"
  validate_conclusion="SUCCESS"
  if [[ "${FAKE_CHECK_FAILURE:-0}" == "1" ]]; then
    validate_conclusion="FAILURE"
  elif [[ "$count" == "1" ]]; then
    validate_status="IN_PROGRESS"
    validate_conclusion=""
  fi

  if [[ -n "$validate_conclusion" ]]; then
    validate_json="\"$validate_conclusion\""
  else
    validate_json="null"
  fi
  checks=$(printf '{"__typename":"CheckRun","name":"validate","status":"%s","conclusion":%s},{"__typename":"StatusContext","context":"CodeRabbit","state":"SUCCESS"}' \
    "$validate_status" "$validate_json")
  if [[ "${FAKE_MISSING_REQUIRED:-0}" != "1" ]]; then
    checks+=',{"__typename":"CheckRun","name":"build-site","status":"COMPLETED","conclusion":"SUCCESS"}'
  fi
  files='[{"path":"Casks/radioactive-ralph.rb"},{"path":"Casks/radioactive-ralph-gui.rb"},{"path":"bucket/radioactive-ralph.json"}]'
  if [[ "${FAKE_EXTRA_FILE:-0}" == "1" ]]; then
    files="${files%]}, {\"path\":\"stale.txt\"}]"
  fi
  printf '{"number":%s,"state":"OPEN","baseRefName":"main","headRefName":"%s","headRefOid":"%s","headRepositoryOwner":{"login":"jbcom"},"isCrossRepository":false,"files":%s,"statusCheckRollup":[%s]}\n' \
    "$number" "$branch" "$head_oid" "$files" "$checks"
  exit 0
fi

if [[ "$command_name" == "pr" && "${1:-}" == "merge" ]]; then
  number="${*: -1}"
  printf '%s\n' "$*" >> "$FAKE_STATE_DIR/merge.log"
  case "$number" in
    101) touch "$FAKE_STATE_DIR/cli-merged" "$FAKE_STATE_DIR/gui-merged" ;;
    *) exit 1 ;;
  esac
  exit 0
fi

exit 0
FAKEGH
chmod +x "$TMP/gh"

cat > "$TMP/cosign" <<'FAKECOSIGN'
#!/usr/bin/env bash
set -euo pipefail

printf '%s\n' "$*" >> "$FAKE_STATE_DIR/cosign.log"
[[ "${FAKE_COSIGN_FAILURE:-0}" != "1" ]] || exit 1
[[ "${1:-}" == "verify-blob" ]]
[[ "$*" == *"--bundle "* ]]
[[ "$*" == *"--certificate-identity https://github.com/jbcom/radioactive-ralph/.github/workflows/release.yml@refs/tags/v${FAKE_VERSION:?}"* ]]
[[ "$*" == *"--certificate-oidc-issuer https://token.actions.githubusercontent.com"* ]]
FAKECOSIGN
chmod +x "$TMP/cosign"

export GH_BIN="$TMP/gh"
export COSIGN_BIN="$TMP/cosign"
export PKGS_GH_TOKEN=fake-pkgs-token
export RELEASE_GH_TOKEN=fake-release-token
export MAX_ATTEMPTS=1
export SLEEP_SECONDS=0
export PR_ATTEMPTS=1
export PR_SLEEP_SECONDS=0
export CHECK_ATTEMPTS=2
export CHECK_SLEEP_SECONDS=0
export FAKE_STATE_DIR="$TMP/state"
export FAKE_VERSION=1.2.3
export FAKE_OLD_VERSION=1.2.2
mkdir -p "$FAKE_STATE_DIR"

# Exact PR heads can be fully verified and exported without changing pkgs main.
PACKAGE_GATE_MODE=verify-heads GITHUB_OUTPUT="$TMP/heads.out" \
  bash "$ROOT/scripts/ci/wait_for_package_publication.sh" 1.2.3
grep -Fx "package_head_oid=cli-oid-1" "$TMP/heads.out" >/dev/null
[[ ! -e "$FAKE_STATE_DIR/merge.log" ]]

rm -rf "$FAKE_STATE_DIR"
mkdir -p "$FAKE_STATE_DIR"

# Pending checks become successful, the trusted atomic PR merges with a head lease,
# and exact versions become visible on main.
GITHUB_OUTPUT="$TMP/merge.out" \
  bash "$ROOT/scripts/ci/wait_for_package_publication.sh" 1.2.3
grep -F -- "--squash --match-head-commit cli-oid-1 101" "$FAKE_STATE_DIR/merge.log" >/dev/null
grep -Fx \
  "package_release_merge_oid=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" \
  "$TMP/merge.out" >/dev/null
grep -Fx \
  "pkgs_main_oid=1111111111111111111111111111111111111111" \
  "$TMP/merge.out" >/dev/null
if grep -F -- "--auto" "$FAKE_STATE_DIR/merge.log" >/dev/null; then
  echo "package gate must explicitly merge the checked head" >&2
  exit 1
fi
[[ "$(wc -l < "$FAKE_STATE_DIR/cosign.log" | tr -d ' ')" == "3" ]]
[[ "$(wc -l < "$FAKE_STATE_DIR/download.log" | tr -d ' ')" == "19" ]]
if sort "$FAKE_STATE_DIR/download.log" | uniq -d | grep -q .; then
  echo "release asset cache downloaded an asset more than once" >&2
  exit 1
fi

# A sealed crash-after-merge rerun has no open PR. It reconstructs ownership
# from the unique durable merged PR and latest target-path history.
rm -rf "$FAKE_STATE_DIR"
mkdir -p "$FAKE_STATE_DIR"
FAKE_ALREADY_MERGED=1 RESOLVE_PACKAGE_ATTEMPT_BRANCH=1 \
  GITHUB_OUTPUT="$TMP/crash-after-merge.out" \
  bash "$ROOT/scripts/ci/wait_for_package_publication.sh" 1.2.3
grep -Fx \
  "package_release_merge_oid=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" \
  "$TMP/crash-after-merge.out" >/dev/null
[[ ! -e "$FAKE_STATE_DIR/merge.log" ]]

# An unrelated later main commit changes current-main identity but not which
# commit most recently owns all three release paths.
rm -rf "$FAKE_STATE_DIR"
mkdir -p "$FAKE_STATE_DIR"
FAKE_ALREADY_MERGED=1 FAKE_INTERVENING_MAIN=1 \
  PACKAGE_GATE_MODE=resolve-merged GITHUB_OUTPUT="$TMP/intervening-main.out" \
  bash "$ROOT/scripts/ci/wait_for_package_publication.sh" 1.2.3
grep -Fx \
  "pkgs_main_oid=2222222222222222222222222222222222222222" \
  "$TMP/intervening-main.out" >/dev/null
grep -Fx \
  "package_release_merge_oid=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" \
  "$TMP/intervening-main.out" >/dev/null

# Resolve-merged must capture current main successfully before appending either
# output. A failed main-ref lookup cannot report a partial winning result.
rm -rf "$FAKE_STATE_DIR"
mkdir -p "$FAKE_STATE_DIR"
: > "$TMP/main-ref-failure.out"
if FAKE_ALREADY_MERGED=1 FAKE_MAIN_REF_FAILURE=1 \
  PACKAGE_GATE_MODE=resolve-merged GITHUB_OUTPUT="$TMP/main-ref-failure.out" \
  bash "$ROOT/scripts/ci/wait_for_package_publication.sh" 1.2.3 \
  >/dev/null 2>&1; then
  echo "expected resolve-merged main-ref failure to fail closed" >&2
  exit 1
fi
[[ ! -s "$TMP/main-ref-failure.out" ]]

# Different latest commits for the three target paths cannot establish one
# atomic owner and must quarantine.
rm -rf "$FAKE_STATE_DIR"
mkdir -p "$FAKE_STATE_DIR"
if FAKE_ALREADY_MERGED=1 FAKE_SPLIT_PATH_OWNER=1 \
  PACKAGE_GATE_MODE=resolve-merged \
  bash "$ROOT/scripts/ci/wait_for_package_publication.sh" 1.2.3 \
  >/dev/null 2>&1; then
  echo "expected split latest target-path ownership to fail" >&2
  exit 1
fi

# Two trusted PR records claiming the same winning merge are ambiguous.
rm -rf "$FAKE_STATE_DIR"
mkdir -p "$FAKE_STATE_DIR"
if FAKE_ALREADY_MERGED=1 FAKE_AMBIGUOUS_WINNER=1 \
  PACKAGE_GATE_MODE=resolve-merged \
  bash "$ROOT/scripts/ci/wait_for_package_publication.sh" 1.2.3 \
  >/dev/null 2>&1; then
  echo "expected ambiguous winning release merge to fail" >&2
  exit 1
fi

# A same-prefix branch for another version is not a sealed release attempt.
rm -rf "$FAKE_STATE_DIR"
mkdir -p "$FAKE_STATE_DIR"
if FAKE_ALREADY_MERGED=1 FAKE_SAME_PREFIX_UNSEALED=1 \
  PACKAGE_GATE_MODE=resolve-merged \
  bash "$ROOT/scripts/ci/wait_for_package_publication.sh" 1.2.3 \
  >/dev/null 2>&1; then
  echo "expected same-prefix unsealed branch to fail exact ownership" >&2
  exit 1
fi

# Multiple historical attempts are safe when exactly one owns the common
# latest target-path commit and exact sealed bytes.
rm -rf "$FAKE_STATE_DIR"
mkdir -p "$FAKE_STATE_DIR"
FAKE_ALREADY_MERGED=1 FAKE_MULTIPLE_ATTEMPTS=1 \
  PACKAGE_GATE_MODE=resolve-merged GITHUB_OUTPUT="$TMP/multiple-attempts.out" \
  bash "$ROOT/scripts/ci/wait_for_package_publication.sh" 1.2.3
grep -Fx \
  "package_release_merge_oid=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" \
  "$TMP/multiple-attempts.out" >/dev/null
FAKE_ALREADY_MERGED=1 FAKE_MULTIPLE_ATTEMPTS=1 \
  PACKAGE_GATE_MODE=resolve-historical \
  GITHUB_OUTPUT="$TMP/historical-attempts.out" \
  bash "$ROOT/scripts/ci/wait_for_package_publication.sh" 1.2.3
grep -Fx \
  "package_release_merge_oid=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" \
  "$TMP/historical-attempts.out" >/dev/null

if FAKE_ALREADY_MERGED=1 FAKE_AMBIGUOUS_WINNER=1 \
  PACKAGE_GATE_MODE=resolve-historical \
  bash "$ROOT/scripts/ci/wait_for_package_publication.sh" 1.2.3 \
  >/dev/null 2>&1; then
  echo "expected ambiguous historical signed-byte owner to fail" >&2
  exit 1
fi

# Multiple legitimate exact attempts can exist after compensation. The unique
# latest trusted attempt strictly before immutable publication is historical.
rm -rf "$FAKE_STATE_DIR"
mkdir -p "$FAKE_STATE_DIR"
FAKE_ALREADY_MERGED=1 FAKE_MULTIPLE_EXACT_ATTEMPTS=1 \
  PACKAGE_GATE_MODE=resolve-historical \
  GITHUB_OUTPUT="$TMP/historical-latest-attempt.out" \
  bash "$ROOT/scripts/ci/wait_for_package_publication.sh" 1.2.3
grep -Fx \
  "package_release_merge_oid=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" \
  "$TMP/historical-latest-attempt.out" >/dev/null

# GitHub timestamps are second-granular. Even one otherwise-valid exact
# candidate at the exact publication second is not provably prepublication.
rm -rf "$FAKE_STATE_DIR"
mkdir -p "$FAKE_STATE_DIR"
if FAKE_ALREADY_MERGED=1 FAKE_SINGLE_AT_PUBLICATION=1 \
  PACKAGE_GATE_MODE=resolve-historical \
  bash "$ROOT/scripts/ci/wait_for_package_publication.sh" 1.2.3 \
  >/dev/null 2>&1; then
  echo "expected single exact attempt at publication second to fail" >&2
  exit 1
fi

for timestamp_case in \
  FAKE_POST_PUBLIC_ATTEMPT \
  FAKE_EQUAL_MERGED_AT \
  FAKE_MISSING_MERGED_AT \
  FAKE_INVALID_MERGED_AT \
  FAKE_MISSING_PUBLISHED_AT \
  FAKE_INVALID_PUBLISHED_AT; do
  rm -rf "$FAKE_STATE_DIR"
  mkdir -p "$FAKE_STATE_DIR"
  if env FAKE_ALREADY_MERGED=1 FAKE_MULTIPLE_EXACT_ATTEMPTS=1 \
    "$timestamp_case"=1 PACKAGE_GATE_MODE=resolve-historical \
    bash "$ROOT/scripts/ci/wait_for_package_publication.sh" 1.2.3 \
    >/dev/null 2>&1; then
    echo "expected $timestamp_case historical ambiguity to fail" >&2
    exit 1
  fi
done

# The final lightweight gate verifies only the small signed package payload,
# then double-checks current target-path ownership around exact main-byte reads.
rm -rf "$FAKE_STATE_DIR"
mkdir -p "$FAKE_STATE_DIR"
FAKE_ALREADY_MERGED=1 \
  EXPECTED_PACKAGE_RELEASE_MERGE_OID=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa \
  PACKAGE_GATE_MODE=recheck-current \
  bash "$ROOT/scripts/ci/wait_for_package_publication.sh" 1.2.3
[[ "$(wc -l < "$FAKE_STATE_DIR/cosign.log" | tr -d ' ')" == "1" ]]
[[ "$(wc -l < "$FAKE_STATE_DIR/download.log" | tr -d ' ')" == "2" ]]

# Model target ownership changing while the slow 23-asset verifier finishes.
# The final recheck exits before the following PATCH can be attempted.
rm -rf "$FAKE_STATE_DIR"
mkdir -p "$FAKE_STATE_DIR"
touch "$FAKE_STATE_DIR/final-ownership-drift"
if FAKE_ALREADY_MERGED=1 FAKE_SPLIT_PATH_OWNER=1 \
  EXPECTED_PACKAGE_RELEASE_MERGE_OID=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa \
  PACKAGE_GATE_MODE=recheck-current \
  bash "$ROOT/scripts/ci/wait_for_package_publication.sh" 1.2.3 \
  >/dev/null 2>&1 &&
  "$GH_BIN" api --method PATCH repos/example/releases/1 >/dev/null 2>&1; then
  echo "expected final ownership drift to block PATCH" >&2
  exit 1
fi
[[ ! -e "$FAKE_STATE_DIR/patch-called" ]]

rm -rf "$FAKE_STATE_DIR"
mkdir -p "$FAKE_STATE_DIR"
if FAKE_ALREADY_MERGED=1 FAKE_TAMPER_MAIN=1 \
  EXPECTED_PACKAGE_RELEASE_MERGE_OID=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa \
  PACKAGE_GATE_MODE=recheck-current \
  bash "$ROOT/scripts/ci/wait_for_package_publication.sh" 1.2.3 \
  >/dev/null 2>&1; then
  echo "expected final signed package byte drift to fail" >&2
  exit 1
fi

rm -rf "$FAKE_STATE_DIR"
mkdir -p "$FAKE_STATE_DIR"
if FAKE_IGNORE_MERGE=1 \
  bash "$ROOT/scripts/ci/wait_for_package_publication.sh" 1.2.3 >/dev/null 2>&1; then
  echo "expected stale main versions to fail after checked merge" >&2
  exit 1
fi

rm -rf "$FAKE_STATE_DIR"
mkdir -p "$FAKE_STATE_DIR"
if FAKE_CHECK_FAILURE=1 \
  bash "$ROOT/scripts/ci/wait_for_package_publication.sh" 1.2.3 >/dev/null 2>&1; then
  echo "expected failed required check to block merge" >&2
  exit 1
fi

rm -rf "$FAKE_STATE_DIR"
mkdir -p "$FAKE_STATE_DIR"
if FAKE_MISSING_REQUIRED=1 \
  bash "$ROOT/scripts/ci/wait_for_package_publication.sh" 1.2.3 >/dev/null 2>&1; then
  echo "expected missing build-site check to block merge" >&2
  exit 1
fi

rm -rf "$FAKE_STATE_DIR"
mkdir -p "$FAKE_STATE_DIR"
if FAKE_SPOOFED_CHECK_APP=1 \
  bash "$ROOT/scripts/ci/wait_for_package_publication.sh" 1.2.3 >/dev/null 2>&1; then
  echo "expected required-check name spoof from another app to fail" >&2
  exit 1
fi

rm -rf "$FAKE_STATE_DIR"
mkdir -p "$FAKE_STATE_DIR"
if FAKE_WRONG_WORKFLOW_PATH=1 \
  bash "$ROOT/scripts/ci/wait_for_package_publication.sh" 1.2.3 >/dev/null 2>&1; then
  echo "expected required check from the wrong workflow path to fail" >&2
  exit 1
fi

rm -rf "$FAKE_STATE_DIR"
mkdir -p "$FAKE_STATE_DIR"
if FAKE_WRONG_WORKFLOW_HEAD=1 \
  bash "$ROOT/scripts/ci/wait_for_package_publication.sh" 1.2.3 >/dev/null 2>&1; then
  echo "expected required workflow run for another head to fail" >&2
  exit 1
fi

rm -rf "$FAKE_STATE_DIR"
mkdir -p "$FAKE_STATE_DIR"
if FAKE_EXTRA_FILE=1 \
  bash "$ROOT/scripts/ci/wait_for_package_publication.sh" 1.2.3 >/dev/null 2>&1; then
  echo "expected stale extra PR file to fail exact allowlist" >&2
  exit 1
fi

rm -rf "$FAKE_STATE_DIR"
mkdir -p "$FAKE_STATE_DIR"
FAKE_HEAD_CHANGE=1 bash "$ROOT/scripts/ci/wait_for_package_publication.sh" 1.2.3
grep -F -- "--match-head-commit cli-oid-2 101" "$FAKE_STATE_DIR/merge.log" >/dev/null

rm -rf "$FAKE_STATE_DIR"
mkdir -p "$FAKE_STATE_DIR"
if FAKE_AMBIGUOUS=1 \
  bash "$ROOT/scripts/ci/wait_for_package_publication.sh" 1.2.3 >/dev/null 2>&1; then
  echo "expected ambiguous exact-branch PRs to fail" >&2
  exit 1
fi

rm -rf "$FAKE_STATE_DIR"
mkdir -p "$FAKE_STATE_DIR"
if FAKE_UNTRUSTED=1 \
  bash "$ROOT/scripts/ci/wait_for_package_publication.sh" 1.2.3 >/dev/null 2>&1; then
  echo "expected fork-owned exact-name PR to fail trusted PR selection" >&2
  exit 1
fi

rm -rf "$FAKE_STATE_DIR"
mkdir -p "$FAKE_STATE_DIR"
if FAKE_BAD_HOST=1 \
  bash "$ROOT/scripts/ci/wait_for_package_publication.sh" 1.2.3 >/dev/null 2>&1; then
  echo "expected attacker-hosted package URL to fail release-side validation" >&2
  exit 1
fi

rm -rf "$FAKE_STATE_DIR"
mkdir -p "$FAKE_STATE_DIR"
if FAKE_BAD_ARTIFACT=1 \
  bash "$ROOT/scripts/ci/wait_for_package_publication.sh" 1.2.3 >/dev/null 2>&1; then
  echo "expected wrong release artifact URL to fail release-side validation" >&2
  exit 1
fi

rm -rf "$FAKE_STATE_DIR"
mkdir -p "$FAKE_STATE_DIR"
if FAKE_GUI_BAD_HOST=1 \
  bash "$ROOT/scripts/ci/wait_for_package_publication.sh" 1.2.3 >/dev/null 2>&1; then
  echo "expected GUI-only attacker host to fail release-side validation" >&2
  exit 1
fi

rm -rf "$FAKE_STATE_DIR"
mkdir -p "$FAKE_STATE_DIR"
if FAKE_GUI_BAD_ARTIFACT=1 \
  bash "$ROOT/scripts/ci/wait_for_package_publication.sh" 1.2.3 >/dev/null 2>&1; then
  echo "expected GUI-only wrong artifact to fail release-side validation" >&2
  exit 1
fi

rm -rf "$FAKE_STATE_DIR"
mkdir -p "$FAKE_STATE_DIR"
if FAKE_BAD_POSTFLIGHT=1 \
  bash "$ROOT/scripts/ci/wait_for_package_publication.sh" 1.2.3 >/dev/null 2>&1; then
  echo "expected wrong GUI quarantine target to fail release-side validation" >&2
  exit 1
fi

rm -rf "$FAKE_STATE_DIR"
mkdir -p "$FAKE_STATE_DIR"
if FAKE_CLI_HOOK=1 \
  bash "$ROOT/scripts/ci/wait_for_package_publication.sh" 1.2.3 >/dev/null 2>&1; then
  echo "expected injected CLI cask hook to fail exact manifest validation" >&2
  exit 1
fi

rm -rf "$FAKE_STATE_DIR"
mkdir -p "$FAKE_STATE_DIR"
if FAKE_GUI_HOOK=1 \
  bash "$ROOT/scripts/ci/wait_for_package_publication.sh" 1.2.3 >/dev/null 2>&1; then
  echo "expected injected GUI cask hook to fail exact manifest validation" >&2
  exit 1
fi

rm -rf "$FAKE_STATE_DIR"
mkdir -p "$FAKE_STATE_DIR"
if FAKE_SCOOP_PRE_INSTALL=1 \
  bash "$ROOT/scripts/ci/wait_for_package_publication.sh" 1.2.3 >/dev/null 2>&1; then
  echo "expected injected Scoop pre_install to fail exact schema validation" >&2
  exit 1
fi

rm -rf "$FAKE_STATE_DIR"
mkdir -p "$FAKE_STATE_DIR"
if FAKE_SCOOP_INSTALLER=1 \
  bash "$ROOT/scripts/ci/wait_for_package_publication.sh" 1.2.3 >/dev/null 2>&1; then
  echo "expected injected Scoop installer script to fail exact schema validation" >&2
  exit 1
fi

rm -rf "$FAKE_STATE_DIR"
mkdir -p "$FAKE_STATE_DIR"
if FAKE_CLI_BAD_HASH=1 \
  bash "$ROOT/scripts/ci/wait_for_package_publication.sh" 1.2.3 >/dev/null 2>&1; then
  echo "expected CLI/Scoop hash mismatch against checksums.txt to fail" >&2
  exit 1
fi

rm -rf "$FAKE_STATE_DIR"
mkdir -p "$FAKE_STATE_DIR"
if FAKE_GUI_BAD_HASH=1 \
  bash "$ROOT/scripts/ci/wait_for_package_publication.sh" 1.2.3 >/dev/null 2>&1; then
  echo "expected GUI hash mismatch against gui-checksums.txt to fail" >&2
  exit 1
fi

rm -rf "$FAKE_STATE_DIR"
mkdir -p "$FAKE_STATE_DIR"
if FAKE_COSIGN_FAILURE=1 \
  bash "$ROOT/scripts/ci/wait_for_package_publication.sh" 1.2.3 >/dev/null 2>&1; then
  echo "expected invalid Sigstore bundle verification to fail" >&2
  exit 1
fi

rm -rf "$FAKE_STATE_DIR"
mkdir -p "$FAKE_STATE_DIR"
if FAKE_MISSING_ASSET=1 \
  bash "$ROOT/scripts/ci/wait_for_package_publication.sh" 1.2.3 >/dev/null 2>&1; then
  echo "expected missing referenced release asset to fail" >&2
  exit 1
fi

rm -rf "$FAKE_STATE_DIR"
mkdir -p "$FAKE_STATE_DIR"
if FAKE_STALE_CLI_ASSET=1 \
  bash "$ROOT/scripts/ci/wait_for_package_publication.sh" 1.2.3 >/dev/null 2>&1; then
  echo "expected stale CLI archive bytes to fail signed checksum validation" >&2
  exit 1
fi

rm -rf "$FAKE_STATE_DIR"
mkdir -p "$FAKE_STATE_DIR"
if FAKE_CLOBBERED_GUI_ASSET=1 \
  bash "$ROOT/scripts/ci/wait_for_package_publication.sh" 1.2.3 >/dev/null 2>&1; then
  echo "expected clobbered GUI bytes to fail signed checksum validation" >&2
  exit 1
fi

rm -rf "$FAKE_STATE_DIR"
mkdir -p "$FAKE_STATE_DIR"
if FAKE_TAMPER_MAIN=1 \
  bash "$ROOT/scripts/ci/wait_for_package_publication.sh" 1.2.3 >/dev/null 2>&1; then
  echo "expected same-version tampered main manifest to fail closed" >&2
  exit 1
fi

rm -rf "$FAKE_STATE_DIR"
mkdir -p "$FAKE_STATE_DIR"
if FAKE_MAIN_RACE=1 \
  bash "$ROOT/scripts/ci/wait_for_package_publication.sh" 1.2.3 >/dev/null 2>&1; then
  echo "expected moving pkgs main during snapshot validation to fail closed" >&2
  exit 1
fi

if bash "$ROOT/scripts/ci/wait_for_package_publication.sh" 1.2.3-rc.1 >/dev/null 2>&1; then
  echo "expected prerelease version to fail admission" >&2
  exit 1
fi

echo "package publication tests: ok"
