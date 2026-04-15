---
title: Design
lastUpdated: 2026-04-15
---

# Design — radioactive-ralph

## Vision

radioactive-ralph is a little orchestration creature with a lot of different
personalities who really wants to help any way he can.

The product should feel like:

- one binary you install
- one repo you initialize
- one durable planning/runtime surface
- many personalities you can ask Ralph to inhabit

## The simplification

The repo got into trouble by trying to support too many identities at once:

- plugin
- marketplace add-on packaging
- binary
- HTTP MCP server
- provider-specific implementation
- provider-agnostic future

That is too many stories for one tool.

The correct story is:

- **binary first**
- **personas in code**
- **Claude via MCP**
- **provider abstraction later**

## Personality matters

The personalities are not fluff. They are a usable operator model.

Green, grey, red, blue, professor, fixit, immortal, savage, old-man, and
world-breaker are different ways Ralph can help. The product should preserve
that voice and intent while keeping the implementation source of truth inside
the binary.

## Provider direction

The long-term design goal is a declarative provider layer in repo config so a
repo can bind Ralph to whatever agent CLI it wants, provided it defines:

- how to run the tool
- how to set model
- how to set effort
- how to append the persona/system prompt
- how to pass the operator/user prompt
- what structured output format the runtime should parse

Claude is simply the first supported provider.
