---
title: Design
description: Design notes for the supervisor architecture's core contracts.
---

# Design

Contract-level documentation for the parts of the runtime that callers
depend on.

| Page | Contract |
|------|----------|
| [Provider contract](./provider-contract.md) | Capability records, stateful vs. stateless, resolution and validation |
| [Declarative provider bindings](./declarative-provider-bindings.md) | Config-only provider onboarding for compatible CLI framings |
| [Config virtual layers](./config-layers.md) | USER/PROJECTS layering, changes vs. overrides, conflict diffing |
| [Orchestrator-verified completion and A2A](./completion-and-a2a.md) | Why a worker never marks its own work done, and the A2A vocabulary that carries evidence |

```{toctree}
:hidden:

provider-contract
declarative-provider-bindings
config-layers
completion-and-a2a
```
