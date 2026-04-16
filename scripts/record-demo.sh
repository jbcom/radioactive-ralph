#!/usr/bin/env bash
# Record the radioactive-ralph demo GIF.
#
# Preferred path: vhs (https://github.com/charmbracelet/vhs) — deterministic.
# Fallback path: asciinema + agg — interactive, but still produces the same
# committed output asset path.
#
# Usage:
#   scripts/record-demo.sh
#
# Output:
#   assets/demo.gif

set -euo pipefail

readonly REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
readonly TAPE="${REPO_ROOT}/scripts/demo.tape"
readonly OUTPUT="${REPO_ROOT}/assets/demo.gif"

readonly GREEN=$'\033[0;32m'
readonly BRIGHT_GREEN=$'\033[1;32m'
readonly YELLOW=$'\033[0;33m'
readonly RED=$'\033[0;31m'
readonly DIM=$'\033[2m'
readonly RESET=$'\033[0m'

step() { printf "%s→%s %s\n" "${BRIGHT_GREEN}" "${RESET}" "$1"; }
info() { printf "%s·%s %s\n" "${DIM}" "${RESET}" "$1"; }
warn() { printf "%s!%s %s\n" "${YELLOW}" "${RESET}" "$1"; }
err()  { printf "%s✗%s %s\n" "${RED}" "${RESET}" "$1" >&2; }
ralph(){ printf "%s[ralph]%s %s\n" "${GREEN}" "${RESET}" "$1"; }

have() { command -v "$1" >/dev/null 2>&1; }

cd "${REPO_ROOT}"
mkdir -p assets

if [[ ! -f "${TAPE}" ]]; then
    err "tape file not found: ${TAPE}"
    exit 1
fi

if have vhs; then
    step "Recording demo with vhs"
    info "tape:   ${TAPE}"
    info "output: ${OUTPUT}"
    echo
    vhs "${TAPE}"
    echo
    ralph "I made a gif! Well, vhs made it. But I picked the good poses."
    info "optional: shrink with 'gifsicle -O3 --colors 128 ${OUTPUT} -o ${OUTPUT}'"
    exit 0
fi

if have asciinema && have agg; then
    warn "vhs not found — falling back to asciinema + agg"
    info "recording to /tmp/ralph-demo.cast"
    echo
    echo "  Run these commands during the recording (exit with Ctrl+D):"
    echo "    radioactive_ralph --help"
    echo "    radioactive_ralph doctor"
    echo "    radioactive_ralph service --help"
    echo "    radioactive_ralph plan --help"
    echo "    radioactive_ralph tui --help"
    echo
    asciinema rec --overwrite --cols 150 --rows 40 /tmp/ralph-demo.cast
    step "Converting cast to gif with agg"
    agg --theme monokai --font-size 18 /tmp/ralph-demo.cast "${OUTPUT}"
    ralph "I dressed myself! And the gif is in ${OUTPUT}!"
    exit 0
fi

err "Neither 'vhs' nor 'asciinema+agg' is installed."
echo
cat <<EOF
${BRIGHT_GREEN}How to install one:${RESET}

  ${DIM}# vhs (recommended — deterministic, one shot)${RESET}
  brew install vhs
  go install github.com/charmbracelet/vhs@latest

  ${DIM}# asciinema + agg (fallback — interactive)${RESET}
  brew install asciinema
  cargo install --git https://github.com/asciinema/agg

Then re-run:

  ${DIM}\$${RESET} scripts/record-demo.sh

The tape file is ${TAPE} — edit it to change what the demo shows.
EOF
exit 1
