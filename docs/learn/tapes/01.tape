# Lesson 1: install apic and send your first request. Renders the terminal
# parts of the episode with VHS (https://github.com/charmbracelet/vhs):
#   vhs docs/learn/tapes/01.tape
# Run `apic demo --out apic-demo` in another terminal first.
Output docs/learn/tapes/01.gif
Set Shell "bash"
Set FontSize 16
Set Width 1200
Set Height 720
Set Theme "Catppuccin Mocha"
Set TypingSpeed 40ms

Type "apic version" Enter Sleep 2s
Type "clear" Enter
Type "apic list -C apic-demo" Enter Sleep 4s
Type "clear" Enter
Type "apic describe login -C apic-demo" Enter Sleep 4s
Type "clear" Enter
Type "apic run login -C apic-demo" Enter Sleep 4s
Type "clear" Enter
Type "apic run not-found -C apic-demo; echo exit $?" Enter Sleep 4s
Type "clear" Enter
Type "apic run login -C apic-demo --json | jq .ok" Enter Sleep 3s
