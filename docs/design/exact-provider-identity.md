---
title: Exact provider identity
description: Immutable executable, invocation, endpoint, and local-model identity for calibrated lanes.
---

# Exact provider identity

Status: **blocking design contract**. A stored model name, effort, binary hash,
or Ollama digest is evidence only when the supervisor verifies that the exact
identity is the one actually executed.

## Execution identity

Every calibrated lane owns a canonical execution identity:

```text
schema
platform GOOS and GOARCH
absolute symlink-resolved executable path
executable SHA-256
version arguments and normalized version output
argv-construction version
non-secret runtime-config SHA-256
provider, model, and effort
optional backend identity
```

Secrets never enter the content address. The identity records only their
configured source and key name.

The production v3 implementation must resolve an absolute regular executable
at import and every dispatch, hash it, run the provider-specific version probe
through that exact path, and compare the complete record. Runners must execute
that absolute path without a second `PATH` lookup. The resulting attestation
must be stored with the attempt and task before completion evidence can be
accepted.

Version probes have closed stdin, a ten-second timeout, and a 64 KiB combined
output limit. Executable hashing has a ten-second bound. Non-regular files,
platform mismatch, path drift, bytes drift, version drift, timeout, or
oversized output fail closed.

The current v2 foundation pins and validates the absolute executable path,
bytes, version, and invocation hash before a Unix turn. It does not persist a
full attempt execution attestation or verify the spawned process image after
start. Calibrated Windows preflight fails closed until Job Object descendant
cleanup is implemented.

Where a platform exposes it, v3 must verify the spawned process image path and
re-hash the target after start. A mismatch kills the process group and
discards its result. Portable path-based process creation cannot defeat an
adversarial same-user replace-and-restore attack; stronger protection requires
content-addressed executable snapshots or platform-specific handle execution.
The implementation must state which threat boundary it proves.

## OpenCode and Ollama

An exact OpenCode/Ollama lane additionally binds:

- a calibration-specific OpenCode provider ID;
- the canonical OpenAI-compatible base URL;
- the canonical native Ollama API URL;
- exact tagged model name and full `sha256:` digest;
- Ollama server version;
- the canonical generated OpenCode config hash.

Ralph supplies a generated provider configuration and disables project config,
model-list fetching, auto-update, and default plugins for that invocation. It
clears conflicting OpenCode and Ollama routing variables. A user's ordinary
`ollama` provider key is not reused because global or managed configuration
could retarget it.

URLs require HTTPS except explicit loopback HTTP. Userinfo, query, fragment,
redirects, and ambiguous spellings are rejected.

Before and after every turn Ralph performs bounded metadata probes:

- native Ollama `/api/version`;
- native Ollama `/api/tags`, with exactly one full-name match and exact digest;
- the OpenAI-compatible `/models`, containing the exact model ID.

The HTTP client follows no redirects, uses bounded response bodies and strict
timeouts, and always closes or cancels bodies. Post-turn drift invalidates the
result while preserving spend accounting.

Ollama does not expose a request-level immutable-digest lease. Operational
drift is therefore detectable, but a malicious replace-and-restore during one
inference is outside this guarantee. Quest uses a digest-named immutable tag
and prohibits retagging while an admission reservation is active.

## Causal capability evidence

Provider identity proves which lane ran; it does not prove that the lane earned
a capability. Arbitrary calibration-record import may not mint production
capabilities from caller-authored `evidence` JSON.

Production adjudication names:

- the completed calibration fixture task and attempt;
- the fixture registry version and digest;
- the exact expected repetition count;
- every durable repetition record and assistant-output hash;
- an independent v3 adjudication task and immutable output publication;
- the capability set derived from that fixture's closed registry entry;
- the exact execution identities of fixture and adjudicator.

The supervisor loads these records itself and creates canonical evidence. It
rejects missing, duplicate, mixed-attempt, wrong-tuple, wrong-fixture,
incomplete-repetition, unpublished-adjudication, non-independent, or
caller-expanded capability claims. Direct record import remains an
experimental administrative surface and cannot create a production-capable
alias.

`await-calibration` tasks are re-admitted only after this causal transaction
commits. A unique alias still never retargets; changed evidence creates a new
alias/generation.

## Migration and compatibility

Execution identity is additive and versioned. Historical calibration rows
migrate with identity schema `0` and are unusable for strict calibrated
dispatch until re-imported. IPC exposes the full identity and attestation;
older clients cannot import records whose required semantics they do not
understand.

The migration and API update include:

- provider calibration identity fields;
- execution identity on calibration attempts;
- execution attestation on task metadata and task detail views;
- canonical content-address calculation over the new identity;
- command-level IPC minimum versions so unchanged older commands remain usable
  during a rolling client/supervisor upgrade.

## Required proof

- Changing `PATH` after admission cannot change the executed binary.
- Symlink retarget, byte replacement, version drift, platform mismatch,
  timeout, and non-regular targets block before accepted output.
- A replacement injected between preflight and start is caught by process
  attestation where supported.
- Calibration attempts and task completion point to the same execution
  identity and immutable calibration ID.
- Capability minting is derived from complete stored repetitions plus an
  independent immutable adjudication publication; arbitrary evidence JSON or
  an expanded caller capability list is rejected.
- Identity-schema-zero calibrations fail closed.
- Generated OpenCode configuration wins over conflicting user, project, and
  environment configuration.
- Wrong endpoint, model, digest, version, redirect, malformed JSON, duplicate
  model, oversized body, and timeout all fail closed under `httptest`.
- Digest drift before or after a turn rejects the result.
- A controlled local smoke proves the intended OpenCode binary, Caddy endpoint,
  Ollama server, model name, and digest without browser or audio activity.

Quest activation remains blocked until the local backend portion of this
contract is implemented and proven.
