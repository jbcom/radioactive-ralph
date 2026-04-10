---
title: Security Policy
updated: 2026-04-10
status: current
---

# Security Policy

## Supported versions

Only the latest release on PyPI is actively supported with security patches.

| Version | Supported |
|---------|-----------|
| Latest  | Yes       |
| Older   | No        |

## Reporting a vulnerability

**Do not open a public GitHub issue for security vulnerabilities.**

Report vulnerabilities privately via [GitHub Security Advisories](https://github.com/jbcom/radioactive-ralph/security/advisories/new).

Include:
- A clear description of the vulnerability
- Steps to reproduce
- Potential impact
- Any suggested fixes

You will receive a response within 72 hours. We aim to release a patch within 14 days for confirmed issues.

## Threat model

radioactive-ralph runs as a local daemon with access to:

- Your local git repositories
- API tokens (GitHub/GitLab/Gitea/Forgejo) from environment variables
- The `claude` CLI (spawned as a subprocess)
- Your filesystem (state file, repo paths)

**It is designed for local, single-user use.** Running it with credentials that have write access to production infrastructure is your responsibility. Use scoped tokens with the minimum required permissions.

## Token handling

- Tokens are read from environment variables at startup and never written to disk
- API error responses are logged by HTTP status code only — response bodies are never logged
- The state file (`~/.radioactive-ralph/state.json`) does not contain tokens

## Responsible disclosure

We follow [coordinated vulnerability disclosure](https://en.wikipedia.org/wiki/Coordinated_vulnerability_disclosure). Please give us reasonable time to fix before public disclosure.
