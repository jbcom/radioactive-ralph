#!/usr/bin/env bash
# radioactive-ralph one-shot installer
#
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/jbcom/radioactive-ralph/main/scripts/install.sh | bash
#
# What it does:
#   1. Detects platform (macOS / Linux)
#   2. Ensures uv is installed (installs via official installer if missing)
#   3. Installs radioactive-ralph via `uv tool install`
#   4. If `claude` CLI is present, registers the marketplace and installs the plugin
#   5. Prints a Ralph-voice welcome message
#
# Safety:
#   - Idempotent: safe to re-run
#   - Does not touch your shell config (you'll need to add ~/.local/bin to PATH yourself)
#   - Prints every command before running it
#   - Exits on first error

set -euo pipefail

# ── Formatting ──────────────────────────────────────────────────────────────
readonly GREEN=$'\033[0;32m'
readonly BRIGHT_GREEN=$'\033[1;32m'
readonly YELLOW=$'\033[0;33m'
readonly RED=$'\033[0;31m'
readonly BRIGHT_WHITE=$'\033[1;37m'
readonly DIM=$'\033[2m'
readonly RESET=$'\033[0m'

step() { printf "%s→%s %s\n" "${BRIGHT_GREEN}" "${RESET}" "$1"; }
info() { printf "%s·%s %s\n" "${DIM}" "${RESET}" "$1"; }
warn() { printf "%s!%s %s\n" "${YELLOW}" "${RESET}" "$1"; }
err() { printf "%s✗%s %s\n" "${RED}" "${RESET}" "$1" >&2; }
ralph() { printf "%s[ralph]%s %s\n" "${GREEN}" "${RESET}" "$1"; }

# ── Banner ──────────────────────────────────────────────────────────────────
cat <<EOF

${BRIGHT_GREEN}┌─────────────────────────────────────────────────────────┐${RESET}
${BRIGHT_GREEN}│${RESET}                                                         ${BRIGHT_GREEN}│${RESET}
${BRIGHT_GREEN}│${RESET}  ${BRIGHT_WHITE}radioactive-ralph${RESET}                                      ${BRIGHT_GREEN}│${RESET}
${BRIGHT_GREEN}│${RESET}  ${DIM}autonomous continuous development orchestrator${RESET}        ${BRIGHT_GREEN}│${RESET}
${BRIGHT_GREEN}│${RESET}                                                         ${BRIGHT_GREEN}│${RESET}
${BRIGHT_GREEN}└─────────────────────────────────────────────────────────┘${RESET}

EOF
ralph "I dressed myself! And now I'm installing!"
echo

# ── Platform detection ──────────────────────────────────────────────────────
step "Detecting platform"
PLATFORM="$(uname -s)"
ARCH="$(uname -m)"
case "${PLATFORM}" in
    Darwin)
        info "macOS (${ARCH})"
        ;;
    Linux)
        info "Linux (${ARCH})"
        ;;
    *)
        err "Unsupported platform: ${PLATFORM}"
        err "radioactive-ralph supports macOS and Linux. For Windows, use WSL2."
        exit 1
        ;;
esac
echo

# ── Prerequisites ───────────────────────────────────────────────────────────
step "Checking prerequisites"

have() { command -v "$1" >/dev/null 2>&1; }

if ! have python3; then
    err "python3 not found — radioactive-ralph requires Python 3.12+"
    err "Install Python first, then re-run this script."
    exit 1
fi

PYTHON_VERSION="$(python3 -c 'import sys; print(f"{sys.version_info.major}.{sys.version_info.minor}")')"
info "python3 ${PYTHON_VERSION} found"

if [[ "$(printf '%s\n%s\n' "3.12" "${PYTHON_VERSION}" | sort -V | head -n1)" != "3.12" ]]; then
    err "Python 3.12+ required (found ${PYTHON_VERSION})"
    exit 1
fi

if have git; then
    info "git found"
else
    warn "git not found — recommended for daemon mode"
fi

if have gh; then
    info "gh CLI found"
else
    warn "gh CLI not found — required for daemon mode. Install: https://cli.github.com"
fi

if have claude; then
    info "claude CLI found — will register marketplace and install plugin"
    HAS_CLAUDE=1
else
    warn "claude CLI not found — skipping plugin install (daemon mode only)"
    warn "To use the /green-ralph, /red-ralph, /professor-ralph (etc.) skills, install Claude Code first:"
    warn "  https://docs.claude.com/claude-code"
    HAS_CLAUDE=0
fi
echo

# ── Ensure uv is installed ──────────────────────────────────────────────────
step "Ensuring uv is installed"
if have uv; then
    UV_VERSION="$(uv --version 2>&1 | awk '{print $2}')"
    info "uv ${UV_VERSION} already installed"
else
    info "uv not found — installing via official astral-sh installer"
    curl -LsSf https://astral.sh/uv/install.sh | sh

    # uv installs to ~/.local/bin by default — ensure PATH picks it up for this session
    if [[ -x "${HOME}/.local/bin/uv" ]]; then
        export PATH="${HOME}/.local/bin:${PATH}"
    elif [[ -x "${HOME}/.cargo/bin/uv" ]]; then
        export PATH="${HOME}/.cargo/bin:${PATH}"
    fi

    if ! have uv; then
        err "uv installation failed — please install manually: https://docs.astral.sh/uv/getting-started/installation/"
        exit 1
    fi
    info "uv installed"
fi
echo

# ── Install radioactive-ralph ───────────────────────────────────────────────
step "Installing radioactive-ralph (via uv tool)"
if uv tool list 2>/dev/null | grep -q '^radioactive-ralph'; then
    info "radioactive-ralph already installed — upgrading"
    uv tool upgrade radioactive-ralph
else
    uv tool install radioactive-ralph
fi

RALPH_BIN="$(uv tool list 2>/dev/null | awk '/^-.*ralph/{print $2; exit}' || true)"
if have ralph; then
    info "ralph command available: $(command -v ralph)"
else
    warn "ralph command not in PATH yet — you may need to add ~/.local/bin to your PATH"
    warn "Add this to your shell config:"
    warn "  export PATH=\"\$HOME/.local/bin:\$PATH\""
fi
echo

# ── Install Claude Code plugin ──────────────────────────────────────────────
if [[ "${HAS_CLAUDE}" == "1" ]]; then
    step "Registering radioactive-ralph marketplace with Claude Code"
    if claude plugins marketplace list 2>/dev/null | grep -q 'radioactive-ralph'; then
        info "marketplace already registered"
    else
        claude plugins marketplace add github:jbcom/radioactive-ralph || {
            warn "Failed to register marketplace automatically"
            warn "Run manually: claude plugins marketplace add github:jbcom/radioactive-ralph"
        }
    fi

    step "Installing the radioactive-ralph plugin"
    if claude plugin list 2>/dev/null | grep -q 'radioactive-ralph'; then
        info "plugin already installed"
    else
        claude plugin install radioactive-ralph@radioactive-ralph || {
            warn "Failed to install plugin automatically"
            warn "Run manually: claude plugin install radioactive-ralph@radioactive-ralph"
        }
    fi
    echo
fi

# ── Done ────────────────────────────────────────────────────────────────────
cat <<EOF

${BRIGHT_GREEN}┌─────────────────────────────────────────────────────────┐${RESET}
${BRIGHT_GREEN}│${RESET}  ${BRIGHT_WHITE}radioactive-ralph is installed${RESET}                         ${BRIGHT_GREEN}│${RESET}
${BRIGHT_GREEN}└─────────────────────────────────────────────────────────┘${RESET}

  ${BRIGHT_WHITE}Daemon mode:${RESET}
    ${DIM}$${RESET} ralph run                    ${DIM}# start the external daemon${RESET}
    ${DIM}$${RESET} ralph status                 ${DIM}# show state${RESET}
    ${DIM}$${RESET} ralph pr list                ${DIM}# classify open PRs${RESET}

  ${BRIGHT_WHITE}Claude Code plugin mode:${RESET}
    ${DIM}Inside a Claude Code session:${RESET}
    ${DIM}>${RESET} /green-ralph                 ${DIM}# the classic — unlimited loop${RESET}
    ${DIM}>${RESET} /red-ralph                   ${DIM}# triage: fix what's broken, exit${RESET}
    ${DIM}>${RESET} /professor-ralph             ${DIM}# plan before you act${RESET}
    ${DIM}>${RESET} /blue-ralph                  ${DIM}# read-only review${RESET}
    ${DIM}>${RESET} /joe-fixit-ralph --cycles 3  ${DIM}# N cycles, budget report${RESET}
    ${DIM}See:${RESET} https://github.com/jbcom/radioactive-ralph/tree/main/skills

  ${BRIGHT_WHITE}Config:${RESET}
    ${DIM}$${RESET} mkdir -p ~/.radioactive-ralph
    ${DIM}$${RESET} \$EDITOR ~/.radioactive-ralph/config.toml
    ${DIM}See README for the format.${RESET}

EOF
ralph "I'm learnding! Well, actually I'm installed. But later I'll be learnding."
ralph "Oh boy, work! That's where I'm a developer!"
echo
