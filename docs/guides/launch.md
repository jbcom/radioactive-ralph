---
title: Launch (marketing)
lastUpdated: 2026-07-16
---

# Launch Plan — radioactive-ralph

Everything needed before the LinkedIn / Hacker News drop. This is the
**marketing** side of a launch — visual assets, demo, social links.

- Release-engineering checklist (tag, package, smoke, rollback):
  [`launch/release-checklist`](../launch/release-checklist.md)
- Operator runbooks (install, auth, service, troubleshooting):
  [`runbooks/`](../runbooks/index.md)

## Checklist

### Visual assets
- [x] Hero image relocated to `assets/brand/ralph-mascot.png`
- [ ] Social preview uploaded (`assets/social-preview.png`)
- [ ] Demo GIF recorded (`assets/demo.gif`)
- [ ] Architecture diagram SVG (`assets/architecture.svg`)

### Documentation and packaging
- [x] Root README stabilized for GitHub + package-manager install guidance
- [x] Docs realigned to the supervisor + dumb-client architecture
- [x] Docs publishing split from release automation
- [ ] `brew install --cask radioactive-ralph` tested from a clean machine

### Demo verification
- [ ] On macOS/Linux, `radioactive_ralph service install` launches the user-service supervisor cleanly with its configured service environment
- [ ] On native Windows, `$env:RALPH_MAX_PARALLEL = "N"; radioactive_ralph --supervisor` launches the foreground control plane; `radioactive_ralph` connects as its client, and no SCM install/start path is claimed
- [ ] In WSL2, the Linux `systemd --user` service launches provider-backed execution cleanly
- [ ] `radioactive_ralph --init` registers a fresh project in the user-level database
- [ ] `radioactive_ralph` (client) renders the read-only TUI against a live supervisor
- [ ] `radioactive_ralph doctor` behaves cleanly on empty state

## Social links

- Docs: <https://jonbogaty.com/radioactive-ralph/>
- GitHub: <https://github.com/jbcom/radioactive-ralph>
- LinkedIn: <https://linkedin.com/in/jonbogaty>
