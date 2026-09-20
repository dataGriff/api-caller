# Lesson 12: Editors

**Goal.** Send the same request from VS Code, from a JetBrains IDE, from
Neovim and from `apic run`, and know what each one ignores in the file.

## You will need

- apic installed ([lesson 1](01-first-request.md))
- `apic demo` running in another terminal, or start it now:

```sh
apic demo --out apic-demo
```

- VS Code for steps 1 and 2. Steps 3 and 4 are for readers of those
  editors; skip what you do not use.

## Steps

### 1. VS Code with REST Client only

Open `apic-demo/` in VS Code with the
[REST Client](https://marketplace.visualstudio.com/items?itemName=humao.rest-client)
extension installed and open `auth.http`. Above `POST {{baseUrl}}/auth/login`
there is a **Send Request** link; click it and the response opens in a
panel beside the file. Nothing about the file changed for that to work,
and that is the premise of the whole course: a `.http` file is the
editor's format, and everything apic adds is a `#` comment the editor
does not read.

Three things worth noticing while you are there:

- `{{baseUrl}}` resolved, because REST Client reads the same
  `http-client.env.json` (pick the environment with the status-bar item
  it adds, **No Environment** by default).
- `# @name login` is REST Client's own directive, which apic adopted
  rather than inventing another. Both use it to name a request, and
  REST Client lets a later request refer to
  `{{login.response.body.$.access_token}}`, which apic also resolves.
- `# @assert`, `# @capture`, `# @ref` and `# @step` are comments to REST
  Client, and `# @prompt`, which is REST Client's, is ignored by apic:
  apic never prompts, so the value comes from `--var` or an env file
  instead.

### 2. The apic extension

Install **apic** from the Marketplace or Open VSX (search for `apic`), or
from the `.vsix` on the [releases page](https://github.com/dataGriff/api-caller/releases?q=vscode).
It does not replace REST Client; it layers on top, and the two lenses sit
side by side above every request:

- **Run** sends the request through apic's runner, so the assertions are
  evaluated, the captures stored in `.apic/session.json`, and a `# @ref`
  dependency run first. The request line gets `✓ 200 · 12 ms` or
  `✗ 404 · 40 ms`, and the response panel shows the body highlighted,
  each assertion with actual against expected, and the captures. Run
  `whoami` on a fresh session and watch `login` run before it.
  <kbd>ctrl+alt+shift+r</kbd> (<kbd>cmd+alt+shift+r</kbd> on a Mac) runs
  the request under the cursor; REST Client keeps <kbd>ctrl+alt+r</kbd>.
- **Describe** shows what `apic describe` shows: every variable, its
  source, and whether the request is ready.
- **Copy as curl** puts the `apic curl` output on the clipboard.
- **Run file as flow**, at the top of the file, is `apic run todos.http`
  with the summary in the panel.

Away from the lenses:

- The **Problems** panel shows what `apic validate` reports, at the exact
  span, whenever a request file, `apic.yaml` or an env file is saved.
  Misspell a selector in an assertion and the squiggle appears with the
  code linked to the docs; a quick fix offers the nearest known directive
  for a mistyped one.
- The **apic** view in the activity bar lists every request by file with
  a ready icon from `apic describe`, and the **Session** view shows the
  captured values (a cached OAuth2 or `exec` token as its description,
  a captured value as itself, so treat a screen share the way you would
  treat `apic session`) with **Clear session** above them.
- The status bar names the environment in effect; **apic: Select
  environment** changes it, and every lens passes it as `--env`.
- **Format Document** runs `apic fmt` on the file, and with
  `editor.formatOnSave` the files stay in the canonical order from
  [lesson 9](09-import-export.md)'s imports.

Test Explorer integration for `.feature` files, completions and hovers
for directives and variables are tracked in the
[VS Code epic](https://github.com/dataGriff/api-caller/issues/29). Until
then `apic test` from the terminal is the way to run features.

### 3. JetBrains IDEs

IntelliJ, WebStorm, GoLand and the rest ship the HTTP Client, and it
opens the same files. Open `apic-demo/` in one, open `auth.http`, and the
gutter icon on `login` sends it. It reads `http-client.env.json` and
`http-client.private.env.json` too, so one set of environments serves
the IDE and apic, and its `SSLConfiguration` block for client
certificates is honoured by apic as well ([auth](../auth.md#tls-and-client-certificates)).

What the IDE adds that apic does not read is scripting: a
`> {% … %}` block after a request runs JavaScript in the IDE. apic has no
scripting on purpose, and says so rather than failing. Write a request
with a handler and validate it:

<!-- learn -->
```sh
mkdir -p jetbrains
cat > jetbrains/login.http <<'EOF'
### Log in
# @name login
# @assert status == 200
# @capture token = body.$.access_token
POST http://localhost:8089/auth/login
Content-Type: application/json

{"user": "alice", "password": "s3cret"}

> {%
  client.global.set("token", response.body.access_token);
%}
EOF
apic validate -C jetbrains
apic run login -C jetbrains | tail -2
```

```
login.http:10:1: warning: ignoring response handler block (apic has no scripting; see docs/comparison.md) (editor-script)
! 1 file, 1 request, 1 warning
✓ status == 200
↳ token = mock-token
```

The handler's job in the IDE, keeping the token, is what `# @capture`
does for apic in the line above it, so one file serves both: the IDE
runs its script and ignores the directive, apic runs the directive and
ignores the script. `apic validate` warns so the overlap is visible, and
the warning does not fail CI.

### 4. Neovim

[kulala.nvim](https://github.com/mistweaverco/kulala.nvim) sends `.http`
files from Neovim and reads the same env files. Its companion in the
terminal is `apic ui` ([lesson 5](05-terminal-ui.md)): <kbd>o</kbd> opens
the request under the cursor in `$EDITOR`, and the UI reloads the file
when you come back, so the editor writes and apic runs, in two panes of
the same terminal.

### 5. The same request, four ways

Whichever editor sent `login`, the file did not change, and apic runs it
the same:

<!-- learn -->
```sh
apic run login -C apic-demo | tail -2
```

```
✓ status == 200
↳ token = mock-token
```

The editors give you a click, highlighting and a response pane; apic
gives the same file its assertions, its session, CI and an agent. That
split is why the format is plain text with comments and nothing else.

## Checkpoint

`login` sent from REST Client, from the apic extension and from
`apic run`, all with status 200. From the terminal, the last of the
three:

<!-- learn -->
```sh
apic run login -C apic-demo --json | grep -o '"status":200'
```

```
"status":200
```

## Exercise

In VS Code with the apic extension, write a new file `mine.http` in the
demo project with two requests: `health`, and `me` with `# @ref login` and
`# @auth bearer {{token}}`. Run the file as a flow from the CodeLens.

??? example "Solution"
    ```http
    ### Is the API up?
    # @name health
    # @assert status == 200
    GET {{baseUrl}}/health

    ### Who am I
    # @name me
    # @ref login
    # @auth bearer {{token}}
    # @assert status == 200
    # @assert body.$.email endsWith "example.com"
    GET {{baseUrl}}/me
    ```

    The directive lines are highlighted as you type them; save the file
    and the Requests view shows `me` with the ready icon, because `# @ref`
    covers the token. **Run file as flow** at the top runs `health`, then
    `login` on behalf of `me`, then `me`, and the panel shows three green
    rows. From the terminal the same file is `apic run mine.http -C apic-demo`.

## Going further

- [Editors](../editors.md): what each editor gives you, and how to build
  the extension from source
- [Structure](../format.md#structure) in the format guide, where the
  `> {% %}` blocks are described, and the [comparison](../comparison.md)
  with the editors' own features
- The extension's own [README](https://github.com/dataGriff/api-caller/tree/main/editors/vscode)
  for every setting

??? note "Episode script"
    **Length.** 10 minutes; three editors on screen.

    **Cold open (0:00).** The same `auth.http` open in VS Code, a
    JetBrains IDE and Neovim, tiled; three sends, three 200s. "One file.
    Same file."

    **Talking points.**

    1. REST Client alone: Send Request, the env picker, `# @name` being
       theirs, `# @prompt` being theirs and ignored by apic.
    2. The apic extension: Run on `whoami` with `login` running first;
       Describe; Problems from a misspelled selector; the Session view;
       the environment in the status bar; Format Document.
    3. JetBrains: the gutter send; the handler block and the
       `editor-script` warning; `# @capture` beside the script.
    4. Neovim: kulala, and `apic ui` with <kbd>o</kbd>.
    5. `apic run login` from the terminal on the untouched file.
    6. Checkpoint, then the two-request exercise run as a flow.

    **Shot list.** Three editor windows prerecorded, each on `auth.http`;
    one terminal for the validate and run steps. No tape for this lesson.

    **Chapters.** `0:00 One file` · `0:40 REST Client` · `2:30 The apic
    extension` · `5:30 JetBrains and handler blocks` · `7:10 Neovim` ·
    `8:00 The same request, four ways` · `8:50 Checkpoint and exercise`.

    **Description.** From the [episode template](_episode-template.md).
