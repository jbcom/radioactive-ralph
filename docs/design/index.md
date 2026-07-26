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
| [Deterministic execution](./deterministic-execution.md) | Additive v3 source locking, CAS, isolated attempts, artifact lineage, publication, and completion |
| [Exact provider identity](./exact-provider-identity.md) | Executable, invocation, endpoint, and local-model attestation |
| [Plan v2 adaptive concurrency follow-on](./plan-v2-adaptive-concurrency.md) | Blocking seam for measured global, work-class, provider, and independence-domain admission |

```{toctree}
:hidden:

provider-contract
declarative-provider-bindings
config-layers
completion-and-a2a
deterministic-execution
exact-provider-identity
plan-v2-adaptive-concurrency
```
