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
| [Deterministic execution](./deterministic-execution.md) | What is reproducible about a run — and what deliberately is not |
| [Exact provider identity](./exact-provider-identity.md) | What identifies one invocation, why strict binding compares the resolved model, and what the record does not prove |
| [Plan-adaptive concurrency](./plan-adaptive-concurrency.md) | How readiness, leaf-group partitioning, and output reservations decide what runs at once |
| [Provider write containment](./provider-write-containment.md) | What actually stops a provider writing outside the checkout, and where that guarantee does not yet hold |

```{toctree}
:hidden:

provider-contract
declarative-provider-bindings
config-layers
completion-and-a2a
deterministic-execution
exact-provider-identity
plan-adaptive-concurrency
provider-write-containment
```
