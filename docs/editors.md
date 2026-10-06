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
- **apic** (`dataGriff.apic`, on the
  [Marketplace](https://marketplace.visualstudio.com/items?itemName=dataGriff.apic)
  and [Open VSX](https://open-vsx.org/extension/dataGriff/apic)) layers
  apic on top. **Run**, **Describe** and **Copy as curl** sit above every
  request, and **Run file as flow** at the top of a file; a run goes
  through apic's runner, assertions, captures and `# @ref` dependencies
  included, marks the request line `✓ 200 · 12 ms`, and opens a response
  panel beside the editor with the body highlighted, every assertion's
  actual against expected, and the captures. `apic validate` runs when a
  request file, `apic.yaml` or an env file changes on disk, and its
  findings land in the Problems panel at the exact span with quick fixes
  for a mistyped directive, a duplicate name and a missing body file. An
  **apic** view in the activity bar lists every request by file with a
  ready/not-ready icon and shows the session (captured values and
  cookies) for the environment in effect, which the status bar names and
  **apic: Select environment** changes. **Format Document** goes through
  `apic fmt`, and the bundled schemas validate `apic.yaml` (with the YAML
  extension), the env files and the session file. Typing `# @` offers
  every directive with its shape filled in, `{{` offers the variables of
  the environment in effect (source alongside, secrets masked), the
  session's captures, the built-ins and `<name>.response.…` references,
  `# @assert` and `# @capture x =` offer the selectors and, after
  `body.$.`, the keys of that request's last response; hovering a
  `{{placeholder}}` shows its value and source, or which request
  captures it. The **Test Explorer** lists every `.feature` file under
  the project's test paths with its scenarios and example rows, runs
  them through `apic test` in the environment in effect (a tag
  expression on request), and shows a failing step's message at its
  line; a `# @step` line in a request file says how many scenarios use
  its phrase and opens them. With **apic.languageServer.enable** on, the
  diagnostics, completion and hover come from [`apic lsp`](#any-editor-with-an-lsp-client)
  instead, so problems show as you type rather than on save.

Install it from the Marketplace or Open VSX (search for **apic**), or
from the `.vsix` attached to a `vscode-v*` entry on the
[releases page](https://github.com/dataGriff/api-caller/releases?q=vscode):

```sh
code --install-extension apic-<version>.vsix
```

To build it from the repository instead: `cd editors/vscode && npm ci &&
npm run package`.

## JetBrains IDEs

The built-in HTTP Client sends the same files. It ignores apic's
directives, and apic ignores its `> {% … %}` response handlers (and says
so in `apic validate`). Both read `http-client.env.json` and
`http-client.private.env.json`, so one set of environments serves both.

## Neovim

[kulala.nvim](https://github.com/mistweaverco/kulala.nvim) sends `.http`
files and reads the same env files. Add `apic lsp` (below) for problems,
completion and hover as you type, and apic's terminal UI is the natural
companion: `apic ui` opens the request under the cursor in `$EDITOR` with
<kbd>o</kbd> and reloads when you return.

## Any editor with an LSP client

`apic lsp` is a language server on stdin/stdout, built into the binary,
so any editor with an LSP client gets what the VS Code extension has:

- **Diagnostics as you type**: `apic validate`'s findings for the buffer
  you are editing, saved or not, at the exact span and with their codes,
  and for `apic.yaml` and the env files when they change on disk.
- **Completion**: directives after `# @`, variables inside `{{` (the
  environment's, with their source and secrets masked, the session's
  captures, the built-ins and `<name>.response.…` references), selectors
  after `# @assert` and `# @capture x =`, and after `body.$.` the keys of
  that request's last response (from a run in the editor, or the
  [response history](cli.md#apic-history)); operators, `# @auth` types
  and `# @ref` targets.
- **Hover** on a `{{placeholder}}`: its value and where it came from, or
  which request captures it.
- **Code lenses** above every request: Run, Describe and curl, run by the
  server, with a one-line result and the full report in the editor's log
  (curl with its credentials as shell placeholders, as
  `apic curl --redact` prints it).
- **Formatting** through `apic fmt`.

The project is found from each file: the nearest directory holding
`apic.yaml` or an `http-client` env file, without leaving the workspace
folder. The environment is `env` in the client's initialisation options,
else `--env` on the command, else `apic.yaml`'s `env:`; the
[CLI reference](cli.md#apic-lsp) lists the other options.

**Neovim** (0.11 or later):

```lua
vim.filetype.add({ extension = { http = "http", rest = "http" } })
vim.lsp.config("apic", {
  cmd = { "apic", "lsp" },
  filetypes = { "http" },
  root_markers = { "apic.yaml", "http-client.env.json", ".git" },
  init_options = { env = "dev" }, -- optional
})
vim.lsp.enable("apic")
```

**Helix**, in `languages.toml`:

```toml
[language-server.apic]
command = "apic"
args = ["lsp"]

[[language]]
name = "http"
scope = "source.http"
file-types = ["http", "rest"]
roots = ["apic.yaml", "http-client.env.json"]
comment-token = "#"
language-servers = ["apic"]
```

**JetBrains IDEs**: install the [LSP4IJ](https://plugins.jetbrains.com/plugin/23257-lsp4ij)
plugin, add a new language server with the command `apic lsp`, and map
it to the file name patterns `*.http` and `*.rest`. The built-in HTTP
Client keeps sending the requests; the server adds apic's diagnostics,
completion and hover beside it.

**Emacs** with Eglot:

```elisp
(add-to-list 'eglot-server-programs '((restclient-mode http-mode) . ("apic" "lsp")))
```

**Zed** starts language servers from extensions, and there is no apic
extension for Zed yet; the terminal commands below work there today.

## Any editor

Highlight or not, the terminal is always there:

```sh
apic run get-user           # the request under your cursor, by name
apic validate --format text # problems with line and column
apic ui                     # the terminal UI, next to the editor
```
