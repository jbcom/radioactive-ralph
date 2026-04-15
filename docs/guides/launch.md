---
title: Launch
lastUpdated: 2026-04-15
---

# Launch Plan — radioactive-ralph

Everything needed before the LinkedIn / Hacker News drop.

## Checklist

### Visual assets
- [x] Hero image relocated to `assets/brand/ralph-mascot.png`
- [ ] Social preview uploaded (`assets/social-preview.png`)
- [ ] Demo GIF recorded (`assets/demo.gif`)
- [ ] Per-variant icon set (`assets/variants/*.svg`)
- [ ] Architecture diagram SVG (`assets/architecture.svg`)

### Documentation and packaging
- [x] Root README stabilized for GitHub + PyPI rendering
- [x] Persona docs reworked with structured above-the-fold tables
- [x] Docs IA reorganized around `getting-started`, `guides`, `variants`, and `reference`
- [x] Docs publishing split from release automation
- [ ] `brew install radioactive-ralph` tested from a clean machine

### Demo verification
- [ ] `radioactive_ralph run --variant fixit --advise` turns a plain-English ask into a durable plan plus `.radioactive-ralph/plans/<topic>-advisor.md`
- [ ] `radioactive_ralph run --variant green --foreground` runs end-to-end in single-cycle mode
- [ ] `radioactive_ralph run --variant red --foreground` handles a known CI failure cleanly
- [ ] `radioactive_ralph run --variant fixit --foreground` respects the ROI budget settings
- [ ] `radioactive_ralph status`, `radioactive_ralph plan ls`, and `radioactive_ralph mcp status` return promptly on empty state

## Social links

- Docs: <https://jonbogaty.com/radioactive-ralph/>
- GitHub: <https://github.com/jbcom/radioactive-ralph>
- LinkedIn: <https://linkedin.com/in/jonbogaty>
