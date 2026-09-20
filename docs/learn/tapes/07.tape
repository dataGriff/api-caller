# Lesson 7: behaviour tests with Gherkin. Renders the terminal parts of
# the episode with VHS: vhs docs/learn/tapes/07.tape
# Run `apic demo --out apic-demo` in another terminal first, and write the
# feature files from the lesson page as it goes.
Output docs/learn/tapes/07.gif
Set Shell "bash"
Set FontSize 16
Set Width 1200
Set Height 720
Set Theme "Catppuccin Mocha"
Set TypingSpeed 40ms

Type "apic test -C apic-demo --format progress" Enter Sleep 4s
Type "clear" Enter
Type "apic test --steps -C apic-demo | head -40" Enter Sleep 6s
Type "clear" Enter
Type "cat apic-demo/features/first.feature" Enter Sleep 3s
Type "apic test features/first.feature -C apic-demo" Enter Sleep 4s
Type "clear" Enter
Type "cat apic-demo/features/lesson7.feature" Enter Sleep 5s
Type "apic test features/lesson7.feature -C apic-demo | tail -3" Enter Sleep 4s
Type "clear" Enter
Type "apic test features/tagged.feature -C apic-demo --tags '@smoke && ~@slow' | tail -3" Enter Sleep 4s
Type "clear" Enter
Type "apic test features/undefined.feature -C apic-demo | tail -12" Enter Sleep 5s
Type "clear" Enter
Type "apic test -C apic-demo --format junit --output report.xml && head -2 report.xml" Enter Sleep 4s
Type "apic test -C apic-demo --format progress | grep scenarios" Enter Sleep 3s
