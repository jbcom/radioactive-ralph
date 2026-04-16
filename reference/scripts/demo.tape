# Archived demo tape for the Python reference snapshot.
#
# Do not use this file for current launch assets. The live tape lives at:
#   ../../scripts/demo.tape
#
# This archival tape renders a single poster frame that points operators back
# to the shipped demo tooling instead of replaying the old plugin/MCP-era CLI.

Output assets/demo.gif

Set Shell "bash"
Set FontFamily "JetBrains Mono"
Set FontSize 18
Set Width 1200
Set Height 720
Set Padding 24
Set Theme "Catppuccin Mocha"
Set TypingSpeed 20ms
Set PlaybackSpeed 1.0

Hide
Type "clear"
Enter
Show
Sleep 700ms

Type "printf 'reference/scripts/demo.tape is archival only.\\n\\nUse ../../scripts/demo.tape and ../../scripts/record-demo.sh for the live radioactive-ralph demo flow.\\n'"
Sleep 300ms
Enter
Sleep 5s
