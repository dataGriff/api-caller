# Lesson 5: the terminal UI tour. Renders the episode with VHS:
#   vhs docs/learn/tapes/05.tape
# Run `apic demo --out apic-demo` in another terminal first. Based on
# docs/assets/demo.tape, extended with the session, filter and environment
# flows the lesson walks through. The editor round trip (`o`, edit, `r`)
# is recorded by hand: it depends on the editor on the recording machine.
Output docs/learn/tapes/05.gif
Set Shell "bash"
Set FontSize 16
Set Width 1300
Set Height 720
Set Padding 18
Set Theme "Catppuccin Mocha"
Env EDITOR "vi"

Type "apic ui -C apic-demo" Sleep 500ms Enter Sleep 3s
Type "j" Sleep 800ms
Type "k" Sleep 800ms
Enter Sleep 2500ms                       # run login: marks flip to ready
Type "H" Sleep 2s                        # headers
Type "H" Sleep 500ms
Type "c" Sleep 2s                        # curl
Type "c" Sleep 500ms
Type "/todo" Sleep 1s Enter Sleep 1s     # filter to todos.http
Type "f" Sleep 4s                        # run the file as a flow
Type "G" Sleep 500ms
Type "kk" Sleep 500ms                    # not-found
Type "3" Sleep 2500ms                    # checks: actual vs expected
Type "JJJ" Sleep 1500ms
Type "4" Sleep 2s                        # session
Type "x" Sleep 1s Type "y" Sleep 1500ms  # clear it: marks go back to ○
Escape Sleep 500ms                       # clear the filter
Type "g" Sleep 500ms
Enter Sleep 2s                           # login again
Type "e" Sleep 2s                        # next environment
Type "e" Sleep 1s
Type "?" Sleep 2500ms
Escape Sleep 500ms
Type "q" Sleep 1s
