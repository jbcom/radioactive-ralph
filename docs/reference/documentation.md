---
title: Documentation architecture
description: How radioactive-ralph builds, validates, and publishes its documentation.
---

# Documentation architecture

The project has one production documentation renderer: [Sourcey](https://sourcey.com).
It builds the authored Markdown in this directory into a static site for
`https://jonbogaty.com/radioactive-ralph/`.

## What is authored

The guides, runbooks, reference pages, design notes, and architecture records in
`docs/` are the source of truth. `docs/sourcey.config.ts` owns navigation,
branding, the `slash` URL strategy, the `/radioactive-ralph/` base path, and
the GitHub edit links.

The older Sphinx renderer and its generated `gomarkdoc` Markdown mirror were
removed during the Sourcey cutover. Their history remains available in Git; they
are not a second build or deployment path.

## Go API reference

Ralph is a CLI application, not an importable library. Sourcey's native
`godoc()` adapter extracts the supported `cmd/radioactive_ralph` API from the
Go module at build time. The runtime's `internal/` packages stay private by Go's
package boundary and are explained by the architecture and design sections.

## Build and verify

Run this from the repository root:

```bash
make docs-check
```

It installs the locked Sourcey toolchain, builds `docs/dist/`, validates
repository documentation claims, and checks the published installer is copied
byte-for-byte into the static artifact. Sourcey generates `llms.txt`,
`llms-full.txt`, `sitemap.xml`, search data, and all HTML from that same graph.

PR CI runs the same command without deployment privileges. The trusted CD job
rebuilds from `main`, uploads `docs/dist/` as the GitHub Pages artifact, and
deploys it through the `github-pages` environment.

## SonarQube Cloud analysis

The GitHub-integrated `jbcom_radioactive-ralph` project analyzes pull requests
and `main` without a repository-scoped token or a duplicate GitHub Actions
workflow. Its provider-owned `SonarCloud Code Analysis` status is required
before `main` can advance.

## Automation safety

The repository permits only an audited allowlist of GitHub Actions and requires
full commit-SHA pins at the repository setting. `Repository Policy` runs from
the trusted base branch for every pull request; external forks may not change
workflows, release configuration, dependency automation, or publication files.
Regular pull requests run with read-only CI permissions, while Pages deployment
and release publishing remain trusted-branch or tag-only operations.
