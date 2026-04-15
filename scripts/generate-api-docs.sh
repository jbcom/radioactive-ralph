#!/usr/bin/env bash
# Generate Go API reference markdown via gomarkdoc and drop it into
# the Starlight content collection where Starlight's autogenerate:
# directory: 'api' picks it up.
#
# Runs on every site build (pre-build step) and manually on demand.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
OUT_DIR="$REPO_ROOT/site/src/content/docs/api"

# Ensure gomarkdoc is available. Users get a clean install hint
# rather than a cryptic command-not-found failure.
if ! command -v gomarkdoc >/dev/null 2>&1; then
  if [ -x "$HOME/go/bin/gomarkdoc" ]; then
    export PATH="$HOME/go/bin:$PATH"
  elif [ -x "$HOME/.asdf/installs/golang/1.26.2/bin/gomarkdoc" ]; then
    export PATH="$HOME/.asdf/installs/golang/1.26.2/bin:$PATH"
  else
    echo "gomarkdoc not found. Install with:" >&2
    echo "  go install github.com/princjef/gomarkdoc/cmd/gomarkdoc@latest" >&2
    exit 1
  fi
fi

# Clean out prior output — gomarkdoc doesn't remove stale files, so
# a deleted package would leave a ghost markdown file.
rm -rf "$OUT_DIR"
mkdir -p "$OUT_DIR"

# Emit one markdown file per Go package into the content tree.
# {{.Dir}} expands to the package's directory path relative to the
# module root (e.g., "internal/variant"); we prepend it with the
# flattening via sed below.
cd "$REPO_ROOT"
gomarkdoc \
  --output "$OUT_DIR/{{.Dir}}.md" \
  --repository.url "https://github.com/jbcom/radioactive-ralph" \
  --repository.default-branch main \
  --repository.path / \
  ./cmd/... \
  ./internal/... 2>&1 | sed 's/^/  /' || {
    echo "gomarkdoc failed" >&2
    exit 1
  }

# Flatten: gomarkdoc emits cmd/radioactive_ralph/main.md inside a
# nested directory tree. Starlight's autogenerate works on any depth
# so we leave the tree intact — but we need to add frontmatter to
# each generated file because gomarkdoc emits raw markdown without
# it and Starlight needs a title.
find "$OUT_DIR" -name "*.md" | while read -r file; do
  # Pull the package name from the first H1 (e.g., "# variant").
  pkg_name=$(awk '/^# /{print $2; exit}' "$file")
  # Derive the Go package path from the file location.
  rel_path="${file#$OUT_DIR/}"
  go_path="${rel_path%.md}"

  # Prepend frontmatter unless already present.
  if ! head -1 "$file" | grep -q '^---$'; then
    tmp="$(mktemp)"
    {
      echo "---"
      echo "title: ${go_path}"
      echo "description: Go API reference for the ${pkg_name} package."
      echo "sidebar:"
      echo "  label: ${pkg_name}"
      echo "---"
      echo ""
      cat "$file"
    } > "$tmp"
    mv "$tmp" "$file"
  fi
done

# Emit an index so Starlight's sidebar autogenerate has a landing.
cat > "$OUT_DIR/index.md" <<'MDEOF'
---
title: Go API reference
description: Auto-generated Go package documentation.
sidebar:
  label: Overview
  order: 0
---

This section is **generated** from Go doc comments via
[gomarkdoc](https://github.com/princjef/gomarkdoc). Do not edit
files under `site/src/content/docs/api/` directly — changes will be
overwritten on the next build.

To improve this reference, edit the doc comments in the corresponding
`.go` file and regenerate via `make docs-api` from the repo root.

## Organization

The reference mirrors the Go source tree:

- **cmd/radioactive_ralph/** — CLI entry points and subcommand handlers
- **internal/** — everything else — config, session, supervisor, fixit,
  variant, multiplexer, IPC, service, workspace, etc.

Each package page lists constants, variables, functions, types, and
their public methods with signatures and associated doc comments.
MDEOF

count=$(find "$OUT_DIR" -name "*.md" | wc -l | tr -d ' ')
echo "✓ Generated ${count} API reference page(s) in ${OUT_DIR#$REPO_ROOT/}"
