# A VHS script (https://github.com/charmbracelet/vhs) for recording the
# animated demo: `vhs docs/assets/demo.tape`. The checked-in SVGs are
# generated from real output and need no tooling; this is here for when a
# moving picture is wanted.
Output docs/assets/demo.gif

Set Shell "bash"
Set FontSize 14
Set Width 1100
Set Height 640
Set Padding 18
Set Theme "Catppuccin Mocha"

Type "apic ui --demo"  Sleep 500ms  Enter  Sleep 3s
Enter                  Sleep 2s      # run the selected request
Type "j"               Sleep 500ms
Type "f"               Sleep 3s      # run the whole file as a flow
Type "3"               Sleep 2s      # the checks tab
Type "JJJJ"            Sleep 1500ms  # scroll the pane a line at a time
Type "4"               Sleep 2s      # the session tab
Type "/todo"           Sleep 1s  Enter  Sleep 1500ms
Enter                  Sleep 2500ms
Type "?"               Sleep 2500ms
Escape                 Sleep 500ms
Type "q"               Sleep 1s
