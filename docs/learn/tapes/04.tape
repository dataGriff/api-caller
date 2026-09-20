# Lesson 4: assertions, validation and polling. Renders the terminal parts
# of the episode with VHS: vhs docs/learn/tapes/04.tape
# Run `apic demo --out apic-demo` in another terminal first, and
# `apic run login -C apic-demo` once.
Output docs/learn/tapes/04.gif
Set Shell "bash"
Set FontSize 16
Set Width 1200
Set Height 720
Set Theme "Catppuccin Mocha"
Set TypingSpeed 40ms

Type "apic run health -C apic-demo" Enter Sleep 4s
Type "clear" Enter
Type "apic run not-found -C apic-demo; echo exit $?" Enter Sleep 4s
Type "clear" Enter
Type "apic run list-open-todos -C apic-demo -v" Enter Sleep 5s
Type "clear" Enter
Type "apic run create-todo-invalid -C apic-demo" Enter Sleep 4s
Type "clear" Enter
Type "printf '### dup\n# @name health\n# @assert stauts == 200\nGET {{baseUrl}}/health\n' > apic-demo/broken.http" Enter
Type "apic validate -C apic-demo" Enter Sleep 5s
Type "rm apic-demo/broken.http" Enter
Type "apic validate -C apic-demo" Enter Sleep 3s
Type "clear" Enter
Type "apic run create-job -C apic-demo > /dev/null" Enter
Type "apic run wait-for-job -C apic-demo" Enter Sleep 5s
Type "clear" Enter
Type "apic run todos.http -C apic-demo --keep-going --json | jq -s 'map(select(.ok | not)) | length'" Enter Sleep 4s
