# Lesson 3: capture, the session and flows. Renders the terminal parts of
# the episode with VHS: vhs docs/learn/tapes/03.tape
# Run `apic demo --out apic-demo` in another terminal first.
Output docs/learn/tapes/03.gif
Set Shell "bash"
Set FontSize 16
Set Width 1200
Set Height 720
Set Theme "Catppuccin Mocha"
Set TypingSpeed 40ms

Type "apic session clear -C apic-demo" Enter
Type "apic run list-todos -C apic-demo" Enter Sleep 4s
Type "clear" Enter
Type "apic run login -C apic-demo" Enter Sleep 4s
Type "apic session -C apic-demo" Enter Sleep 3s
Type "cat apic-demo/.apic/session.json" Enter Sleep 3s
Type "clear" Enter
Type "apic run todos.http -C apic-demo" Enter Sleep 6s
Type "clear" Enter
Type "apic run todos.http -C apic-demo --keep-going | tail -15" Enter Sleep 5s
Type "clear" Enter
Type "apic session clear -C apic-demo" Enter
Type "apic run whoami -C apic-demo" Enter Sleep 5s
Type "clear" Enter
Type "apic session -C apic-demo --json | jq .local.token" Enter Sleep 3s
