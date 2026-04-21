---
title: Design
description: Design notes for the live runtime's core contracts.
---

# Design

Contract-level documentation for the parts of the runtime that
callers depend on.

| Page | Contract |
|------|----------|
| [Provider contract](./provider-contract.md) | How the runtime binds to claude/codex/gemini; stateful vs stateless; code-defined vs config-defined |
| [Declarative provider bindings](./declarative-provider-bindings.md) | Config-only provider onboarding for compatible CLI framings |
| [Fixit plan-creation pipeline](./fixit-plan-pipeline.md) | The six-stage pipeline fixit runs to produce a plan; validation gates; fallback behavior |

```{toctree}
:hidden:

provider-contract
declarative-provider-bindings
fixit-plan-pipeline
```
