#!/usr/bin/env bash
# task-batch-flush.sh — flushes in-memory task-batch state to disk before
# compaction. Idempotent; safe to re-run.
#
# State location respects CLAUDE.md's "state lives in XDG, never in
# the repo" rule. Directory resolution order:
#
#   1. $RALPH_TASK_BATCH_STATE_DIR (explicit override, for tests)
#   2. $XDG_STATE_HOME/claude-code/task-batch   (Linux, WSL)
#   3. $HOME/Library/Application Support/claude-code/task-batch  (macOS)
#   4. $HOME/.local/state/claude-code/task-batch                  (POSIX default)
#
# The batch JSON the task-batch skill writes *also* goes here rather
# than under $REPO/.claude/state/, so the hook no longer leaves
# runtime markers inside tracked repositories.

set -euo pipefail

resolve_state_dir() {
    if [[ -n "${RALPH_TASK_BATCH_STATE_DIR:-}" ]]; then
        printf '%s' "$RALPH_TASK_BATCH_STATE_DIR"
        return
    fi
    local os; os="$(uname -s)"
    case "$os" in
        Darwin)
            printf '%s/Library/Application Support/claude-code/task-batch' "$HOME"
            ;;
        Linux|*BSD|*)
            local base="${XDG_STATE_HOME:-$HOME/.local/state}"
            printf '%s/claude-code/task-batch' "$base"
            ;;
    esac
}

STATE_DIR="$(resolve_state_dir)"
mkdir -p "$STATE_DIR"

date -u +"%Y-%m-%dT%H:%M:%SZ" > "$STATE_DIR/.last-compaction"

exit 0
