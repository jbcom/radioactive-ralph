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
- [ ] `brew install radioactive-ralph` tested from a clean machine

### Demo verification
- [ ] `radioactive_ralph --supervisor` (or `service install`) launches the supervisor cleanly
- [ ] `radioactive_ralph --init` registers a fresh project in the user-level database
- [ ] `radioactive_ralph` (client) renders the read-only TUI against a live supervisor
- [ ] `radioactive_ralph doctor` behaves cleanly on empty state

## Social links

- Docs: <https://jonbogaty.com/radioactive-ralph/>
- GitHub: <https://github.com/jbcom/radioactive-ralph>
- LinkedIn: <https://linkedin.com/in/jonbogaty>
