# radioactive-ralph demo GIF — vhs tape
#
# Records a ~30s terminal demo for the README and social posts.
# Run with: vhs scripts/demo.tape
# Install vhs: https://github.com/charmbracelet/vhs
#
# Determinism:
#   - All Sleep durations are fixed
#   - Output path is committed so re-recording overwrites in place
#   - The ralph commands are read-only — they do not mutate state

Output assets/demo.gif

# ── Window + typography ────────────────────────────────────────────────────
Set Shell "bash"
Set FontFamily "JetBrains Mono"
Set FontSize 18
Set Width 1200
Set Height 720
Set Padding 24
Set Theme "Catppuccin Mocha"
Set TypingSpeed 40ms
Set PlaybackSpeed 1.0

# ── Opening frame: clean prompt ────────────────────────────────────────────
Hide
Type "clear"
Enter
Show
Sleep 800ms

# ── 1. Status — show cycle count, active runs, recent Ralph events ─────────
Type "ralph status"
Sleep 400ms
Enter
Sleep 3s

# ── 2. Discover — show classified work items across repos ─────────────────
Type "ralph discover"
Sleep 400ms
Enter
Sleep 5s

# ── 3. PR list — show classified open PRs ─────────────────────────────────
Type "ralph pr list"
Sleep 400ms
Enter
Sleep 5s

# ── 4. Show the plugin variants line ──────────────────────────────────────
Type "ralph install-skill --help"
Sleep 400ms
Enter
Sleep 4s

# ── 5. Final poster frame — linger on the help output ─────────────────────
Sleep 3s
