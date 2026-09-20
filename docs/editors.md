# Editors

apic runs the `.http` format that editors already understand, and
everything it adds is a comment. So the same file is clickable in VS Code,
JetBrains IDEs and Neovim, and runnable by apic from the terminal, CI and
an agent. This page is what each editor gives you and what apic's own
extension adds.

## VS Code

Two extensions, and they cooperate:

- **REST Client** (`humao.rest-client`) provides the `http` language, the
  highlighting, and "Send Request" above each request. apic's `# @` lines
  are comments to it, so a file with assertions and captures still sends.
- **apic** (`dataGriff.apic`, in `editors/vscode` of the repository)
  layers apic on top. **Run**, **Describe** and **Copy as curl** sit above
  every request, and **Run file as flow** at the top of a file; a run goes
  through apic's runner, assertions, captures and `# @ref` dependencies
  included, marks the request line `✓ 200 · 12 ms`, and opens a response
  panel beside the editor with the body highlighted, every assertion's
  actual against expected, and the captures. `apic validate` runs when a
  request file, `apic.yaml` or an env file is saved, and its findings land
  in the Problems panel at the exact span with quick fixes for a mistyped
  directive, a duplicate name and a missing body file. The environment for
  the project is picked from the status bar. Session and request views,
  completions and hovers, Test Explorer for `.feature` files and the
  Marketplace release follow, tracked in the
  [VS Code epic](https://github.com/dataGriff/api-caller/issues/29).

Until the extension is on the Marketplace, build it from the repository:

```sh
cd editors/vscode
npm ci
npm run package        # writes apic-<version>.vsix
code --install-extension apic-*.vsix
```

`apic.yaml` gets completion and validation from the YAML extension through
the schema modeline `apic init` writes; the env files can be associated
with [their schema](cli.md#configuration-file) in `json.schemas`.

## JetBrains IDEs

The built-in HTTP Client sends the same files. It ignores apic's
directives, and apic ignores its `> {% … %}` response handlers (and says
so in `apic validate`). Both read `http-client.env.json` and
`http-client.private.env.json`, so one set of environments serves both.

## Neovim

[kulala.nvim](https://github.com/mistweaverco/kulala.nvim) sends `.http`
files and reads the same env files. apic's terminal UI (`apic ui`) and the
CLI are the natural companions there; `apic ui` opens the request under
the cursor in `$EDITOR` with <kbd>o</kbd> and reloads when you return.

## Any editor

Highlight or not, the terminal is always there:

```sh
apic run get-user           # the request under your cursor, by name
apic validate --format text # problems with line and column
apic ui                     # the terminal UI, next to the editor
```
