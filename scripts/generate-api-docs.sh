#!/usr/bin/env bash
# Generate Go API reference markdown via gomarkdoc into docs/api for
# the repo-root Sphinx site.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
OUT_DIR="$REPO_ROOT/docs/api"

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

# Emit one markdown file per Go package into the content tree. Running
# gomarkdoc over the whole module at once can be memory-heavy on local
# machines, so enumerate packages and render each one independently.
# {{.Dir}} expands to the package's directory path relative to the
# module root (e.g., "internal/variant").
cd "$REPO_ROOT"
package_dirs="$(go list -f '{{.Dir}}' ./cmd/... ./internal/...)"
while IFS= read -r pkg_dir; do
  [[ -z "$pkg_dir" ]] && continue
  rel_pkg="${pkg_dir#$REPO_ROOT/}"
  pkg="./$rel_pkg"
  # Build the GUI-tagged surface too: internal/gui's views/theme/app are behind
  # `//go:build gui`, so without the tag its API page shows only the Fyne-free
  # Controller. --tags gui is harmless for every other package.
  if ! gomarkdoc \
    --tags gui \
    --output "$OUT_DIR/{{.Dir}}.md" \
    --repository.url "https://github.com/jbcom/radioactive-ralph" \
    --repository.default-branch main \
    --repository.path / \
    "$pkg" 2>&1 | sed "s#^#  $pkg: #"; then
    echo "gomarkdoc failed for $pkg" >&2
    exit 1
  fi
done <<< "$package_dirs"

# gomarkdoc emits raw markdown. Prepend a simple title frontmatter so
# MyST/Sphinx gets a stable page title even when the generated H1 is
# package-only.
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
      echo "---"
      echo ""
      cat "$file"
    } > "$tmp"
    mv "$tmp" "$file"
  fi
done

toc_entries=$(
  find "$OUT_DIR" -name "*.md" ! -name "index.md" -print \
    | sed "s#^$OUT_DIR/##" \
    | sed 's#\.md$##' \
    | sort
)

cat > "$OUT_DIR/index.md" <<'MDEOF'
---
title: Go API reference
description: Auto-generated Go package documentation.
---

This section is **generated** from Go doc comments via
[gomarkdoc](https://github.com/princjef/gomarkdoc). Do not edit
files under `docs/api/` directly. Changes will be
overwritten on the next build.

To improve this reference, edit the doc comments in the corresponding
`.go` file and regenerate via `make docs-api` from the repo root.

## Organization

The reference mirrors the Go source tree:

- **cmd/radioactive_ralph/** — CLI entry points and subcommand handlers
- **internal/** — everything else — agent + watchdog, store, supervisor +
  IPC, orchestrator, plan engine, providers, config, service, TUI, GUI, etc.

Each package page lists constants, variables, functions, types, and
their public methods with signatures and associated doc comments.

```{toctree}
:hidden:
MDEOF

while IFS= read -r entry; do
  [[ -z "$entry" ]] && continue
  printf '%s\n' "$entry" >> "$OUT_DIR/index.md"
done <<< "$toc_entries"

cat >> "$OUT_DIR/index.md" <<'MDEOF'
```
MDEOF

count=$(find "$OUT_DIR" -name "*.md" | wc -l | tr -d ' ')
echo "✓ Generated ${count} API reference page(s) in ${OUT_DIR#$REPO_ROOT/}"
