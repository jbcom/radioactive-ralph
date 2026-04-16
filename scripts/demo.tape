# radioactive-ralph demo GIF — vhs tape
#
# Records a deterministic poster-style demo for the README and launch docs.
# Run with: vhs scripts/demo.tape
# Install vhs: https://github.com/charmbracelet/vhs
#
# The tape deliberately sticks to cheap, reproducible commands that reflect the
# live CLI contract. Do not hand-record ad hoc terminal footage.

Output assets/demo.gif

Set Shell "bash"
Set FontFamily "JetBrains Mono"
Set FontSize 18
Set Width 1200
Set Height 720
Set Padding 24
Set Theme "Catppuccin Mocha"
Set TypingSpeed 35ms
Set PlaybackSpeed 1.0

Hide
Type "clear"
Enter
Show
Sleep 800ms

Type "radioactive_ralph --help"
Sleep 400ms
Enter
Sleep 4s

Type "clear"
Enter
Sleep 500ms

Type "radioactive_ralph doctor"
Sleep 400ms
Enter
Sleep 4s

Type "clear"
Enter
Sleep 500ms

Type "radioactive_ralph service --help"
Sleep 400ms
Enter
Sleep 4s

Type "clear"
Enter
Sleep 500ms

Type "radioactive_ralph plan --help"
Sleep 400ms
Enter
Sleep 4s

Type "clear"
Enter
Sleep 500ms

Type "radioactive_ralph tui --help"
Sleep 400ms
Enter
Sleep 4s

Sleep 2s
