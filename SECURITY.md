# Security Policy

radioactive-ralph is an autonomous development tool that can inspect repositories,
run local commands, interact with GitHub, and coordinate AI agents. That makes
security reports especially important for issues involving command execution,
credential handling, repository isolation, and prompt-driven behavior.

## Supported Versions

This project is currently pre-1.0 and changes quickly. Security fixes are made
against the actively developed codebase first.

| Version | Supported |
| ------- | --------- |
| `main` branch | :white_check_mark: |
| latest `0.x` release | :white_check_mark: |
| older `0.x` releases | :x: |
| forks / modified deployments | best effort only |

In practice:

- security fixes land on `main` first
- the latest published release may receive a follow-up patch release
- we do not guarantee backports for older pre-1.0 releases

If you need a fix, please be prepared to upgrade to the current supported
release or to `main`.

## What to Report

Please report issues that could reasonably lead to unauthorized access,
execution, disclosure, or trust-boundary bypasses, including:

- arbitrary command execution
- prompt injection that causes unintended tool use or cross-repo actions
- secret exposure in logs, state files, config, or generated artifacts
- path traversal or unsafe file writes
- unsafe subprocess, shell, or GitHub CLI invocation
- privilege escalation between repositories, branches, users, or agents
- insecure handling of tokens, SSH keys, API keys, or GitHub credentials
- dependency or supply-chain issues with demonstrable impact on this project

Reports about AI-assisted behavior are in scope when they produce a concrete
security impact, not just surprising model output.

## Out of Scope

The following are generally not treated as security vulnerabilities unless they
include a clear, reproducible security consequence:

- harmless hallucinations or low-quality model output
- prompt jailbreaks that do not cross a trust boundary or trigger a protected action
- bugs that only affect a knowingly modified local fork
- missing best practices without an exploit path
- issues in third-party services, models, or CLIs that this project does not control
- denial-of-wallet or excessive token usage without a broader security impact

If you are unsure whether something is in scope, report it privately anyway.

## How to Report a Vulnerability

Please use **GitHub's private vulnerability reporting** for this repository if it
is available in the Security tab.

If private reporting is unavailable, do **not** open a public issue with exploit
details. Instead, open a minimal GitHub issue requesting a private contact
channel, or contact the maintainer through the contact method listed on their
GitHub profile. Keep the initial message high level until a private channel is
established.

Please include:

- affected version, commit SHA, or whether the issue is on `main`
- deployment mode: Claude Code plugin or standalone daemon
- installation path: `pip`, `uvx`, editable checkout, or packaged release
- operating system and environment details
- reproduction steps or proof of concept
- impact assessment
- whether credentials, repository contents, prompts, or model/tool outputs are involved
- any proposed mitigation

Please redact secrets before sending logs, transcripts, screenshots, or state
files.

## Response Expectations

We will try to:

- acknowledge receipt within **5 business days**
- provide an initial triage update within **10 business days**
- keep you informed about whether the report is accepted, needs more detail, or is out of scope

For accepted reports, remediation timing depends on severity, exploitability,
and maintainer availability. Critical issues will be prioritized first.

## Disclosure Process

For validated reports, our normal process is:

1. reproduce and confirm the issue
2. assess impact and affected versions
3. prepare and test a fix
4. publish the fix
5. coordinate public disclosure after users have a reasonable chance to upgrade

If a CVE is appropriate, we may request one through GitHub Security Advisories
or another standard CNA workflow.

## Researcher Guidelines

We support good-faith security research. Please:

- avoid accessing data that is not yours
- avoid destructive testing on public infrastructure
- use the minimum proof necessary to demonstrate impact
- stop and report once you confirm a vulnerability
- give maintainers reasonable time to investigate and fix the issue before public disclosure

## AI-Specific Safety Notes

Because this project automates AI agents that can act on repositories and local
tools, we especially value reports involving:

- unsafe trust in untrusted prompt or repository content
- agent behavior that bypasses intended approval or isolation boundaries
- cross-repository data leakage
- unsafe persistence of transcripts, plans, or state containing sensitive data
- instructions that can coerce dangerous tool execution despite stated safeguards

When reporting these issues, include the smallest prompt, repository fixture, or
workflow that reproduces the unsafe behavior.
