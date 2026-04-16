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
| [Declarative provider bindings](./declarative-provider-bindings.md) | Post-v1 direction for config-only provider onboarding (design only; not shipped) |
| [Fixit plan-creation pipeline](./fixit-plan-pipeline.md) | The six-stage pipeline fixit runs to produce a plan; validation gates; fallback behavior |
