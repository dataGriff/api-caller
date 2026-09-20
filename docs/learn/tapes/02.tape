# Lesson 2: variables and environments. Renders the terminal parts of the
# episode with VHS: vhs docs/learn/tapes/02.tape
# Run `apic demo --out apic-demo` in another terminal first, and apply the
# env-file edits from the lesson page as it goes.
Output docs/learn/tapes/02.gif
Set Shell "bash"
Set FontSize 16
Set Width 1200
Set Height 720
Set Theme "Catppuccin Mocha"
Set TypingSpeed 40ms

Type "apic describe login -C apic-demo" Enter Sleep 4s
Type "clear" Enter
Type "cat apic-demo/http-client.env.json" Enter Sleep 3s
Type "apic env -C apic-demo" Enter Sleep 4s
Type "clear" Enter
Type "apic describe login -C apic-demo --env staging" Enter Sleep 4s
Type "clear" Enter
Type "echo user=dave > apic-demo/.env" Enter
Type "apic describe login -C apic-demo | grep user" Enter Sleep 3s
Type "apic describe login -C apic-demo --var user=erin | grep user" Enter Sleep 3s
Type "APIC_VAR_user=frank apic describe login -C apic-demo | grep user" Enter Sleep 3s
Type "rm apic-demo/.env" Enter
Type "clear" Enter
Type "apic describe whoami -C apic-demo --var baseUrl=http://example.com --json | jq '.variables[] | select(.name==\"baseUrl\") | .source'" Enter Sleep 4s
