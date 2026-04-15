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
