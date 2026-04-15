---
title: Launch
updated: 2026-04-10
status: current
domain: creative
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
- [x] Skill READMEs reworked with structured above-the-fold tables
- [x] Docs IA reorganized around `getting-started`, `guides`, `variants`, and `reference`
- [x] Docs publishing split from tag-based PyPI release
- [ ] `uvx radioactive-ralph run` tested from a clean machine

### Demo verification
- [ ] `/green-ralph` runs end-to-end in single-cycle mode
- [ ] `/red-ralph` handles a known CI failure cleanly
- [ ] `/fixit-ralph --cycles 1` prints the bill
- [ ] `ralph status`, `ralph discover`, and `ralph pr list` return promptly on empty state

## Social links

- Docs: <https://jonbogaty.com/radioactive-ralph/>
- GitHub: <https://github.com/jbcom/radioactive-ralph>
- LinkedIn: <https://linkedin.com/in/jonbogaty>
