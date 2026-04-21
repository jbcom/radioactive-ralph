---
title: Runbooks
description: Operator-facing runbooks for install, auth, service, approvals, troubleshooting.
---

# Runbooks

Task-oriented operator documentation. If you're not sure where to
start, go to [install-first-run](./install-first-run.md).

## Live runbooks

| Page | Use when |
|------|----------|
| [Install + first run](./install-first-run.md) | Fresh install on a new machine |
| [Provider auth](./provider-auth.md) | `doctor` reports a provider check fails, or you're switching providers |
| [Service install/start/stop/recover](./service.md) | Managing the durable repo runtime |
| [Approvals + handoffs](./approvals-handoffs.md) | Day-to-day plan operation |
| [Troubleshooting](./troubleshooting.md) | Something broke |
| [Platform notes](./platforms.md) | Platform-specific quirks (launchd, systemd-user, SCM, WSL) |

Runbooks are kept separate from the guides (`docs/guides/`) because
they answer "how do I do X" — guides answer "why does X work this
way."

```{toctree}
:hidden:

install-first-run
provider-auth
service
approvals-handoffs
troubleshooting
platforms
```
