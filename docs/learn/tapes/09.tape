# Lesson 9: from OpenAPI to a project, and back to curl. Renders the
# terminal parts of the episode with VHS: vhs docs/learn/tapes/09.tape
# Run `apic demo --out apic-demo` in another terminal first, write
# todos-openapi.yaml and the two Postman files from the lesson page into
# the current directory, and put the lesson's step-3 files (auth.http and
# the tidied todos.http) in ./tidy/: the hidden `cp` below stands in for
# the editing the episode shows in a split.
Output docs/learn/tapes/09.gif
Set Shell "bash"
Set FontSize 16
Set Width 1200
Set Height 720
Set Theme "Catppuccin Mocha"
Set TypingSpeed 40ms

Type "apic import todos-openapi.yaml -o todos-api" Enter Sleep 3s
Type "cat todos-api/todos.http" Enter Sleep 5s
Type "clear" Enter
Type "apic validate -C todos-api && apic list -C todos-api" Enter Sleep 4s
Type "apic run health -C todos-api" Enter Sleep 4s
Type "printf 'env: dev\n' > todos-api/apic.yaml && apic run health -C todos-api" Enter Sleep 4s
Type "clear" Enter
Type "apic run list-todos -C todos-api" Enter Sleep 4s
Hide
Type "cp tidy/auth.http tidy/todos.http todos-api/ && clear" Enter
Show
Type "cat todos-api/todos.http" Enter Sleep 5s
Type "apic run todos.http -C todos-api | tail -5" Enter Sleep 4s
Type "clear" Enter
Type "apic import todos.postman_collection.json -o postman-api --postman-env local.postman_environment.json" Enter Sleep 5s
Type "cat postman-api/auth.http" Enter Sleep 5s
Type "clear" Enter
Type "apic import --curl 'curl -X PUT http://localhost:8089/todos/1 -H \"Authorization: Bearer mock-token\" -H \"Content-Type: application/json\" -d '\"'\"'{\"done\":true}'\"'\"'' --into todos.http --name finish-todo -C todos-api" Enter Sleep 3s
Type "tail -8 todos-api/todos.http" Enter Sleep 4s
Type "clear" Enter
Type "apic curl get-todo -C todos-api" Enter Sleep 3s
Type "apic curl get-todo -C todos-api | sh" Enter Sleep 3s
Type "apic curl get-todo -C todos-api --redact" Enter Sleep 4s
Type "apic run todos.http -C todos-api --json | jq -s 'map(.ok) | all'" Enter Sleep 4s
