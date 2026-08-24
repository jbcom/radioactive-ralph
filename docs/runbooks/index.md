---
title: Runbooks
description: Operator-facing runbooks for install, auth, service, troubleshooting.
lastUpdated: 2026-07-26
---

# Runbooks

Task-oriented operator documentation. If you're not sure where to
start, go to [install-first-run](./install-first-run.md).

## Live runbooks

| Page | Use when |
|------|----------|
| [Install + first run](./install-first-run.md) | Fresh install on a new machine |
| [Provider auth](./provider-auth.md) | `doctor` reports a provider check fails, or you're switching providers |
| [Service install/start/stop/recover](./service.md) | Managing the macOS/Linux service; native Windows limited control plane/remediation |
| [Troubleshooting](./troubleshooting.md) | Something broke |
| [Platform notes](./platforms.md) | Platform quirks (launchd, systemd-user, native Windows limitations, functional WSL2) |

Runbooks are kept separate from the guides (`docs/guides/`) because
they answer "how do I do X" — guides answer "why does X work this
way."
