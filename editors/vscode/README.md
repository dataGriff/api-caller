# apic for VS Code

[![Visual Studio Marketplace](https://img.shields.io/visual-studio-marketplace/v/dataGriff.apic?label=Marketplace)](https://marketplace.visualstudio.com/items?itemName=dataGriff.apic)
[![Open VSX](https://img.shields.io/open-vsx/v/dataGriff/apic?label=Open%20VSX)](https://open-vsx.org/extension/dataGriff/apic)

Run the `.http` files you already have with [apic](https://datagriff.github.io/api-caller/):
environments, captured variables that persist, assertions, AWS and OAuth2
auth, Gherkin tests and an MCP server, from one static binary. This
extension drives that binary from the editor, so what you see here is what
CI and your agents get from the same files.

It works alongside REST Client: that extension keeps its language, its
highlighting and its "Send Request"; this one layers apic's directives and
commands on top.

## What it does

- **Run, Describe and Copy as curl** above every request, and **Run file
  as flow** at the top of a file. Run sends the request through apic's
  runner, assertions, captures and `# @ref` dependencies included, and
  marks the request line `✓ 200 · 12 ms` or `✗ 404 · 40 ms`.
  <kbd>ctrl+alt+shift+r</kbd> (<kbd>cmd+alt+shift+r</kbd>) runs the request
  under the cursor; REST Client keeps <kbd>ctrl+alt+r</kbd> for its own
  Send Request.
- **A response panel** beside the editor: status, timing and size, headers
  (collapsed), the body pretty-printed and highlighted with a raw toggle and
  a save button, every assertion with actual against expected, captures,
  errors. A flow shows its pass/fail summary and one collapsible block per
  request. **apic: Show last response** brings it back.
- **Problems from `apic validate`**: every error and warning apic reports
  (bad selectors, unknown directives, duplicate names, missing body files,
  `# @ref` cycles) appears in the Problems panel at the exact span, with
  its code linked to the docs, refreshed when a request file, `apic.yaml`
  or an env file changes on disk. A project apic cannot load at all (a
  broken `apic.yaml`) shows that one error on the file. Quick fixes change
  a mistyped directive to the nearest known one, rename a duplicate, or
  create a missing body file. The status bar shows the count.
- **An apic view** in the activity bar. **Requests** lists every request
  of the project by file with a ready/not-ready icon (from
  `apic describe`), Run, Describe and Copy as curl inline, and opens the
  request in the editor on a click. **Session** shows the captured values
  and the cookies in the jar for the environment in effect, tokens as apic
  describes them and cookie values never; **Clear session** forgets them
  after a confirmation.
- **The environment** in the status bar: apic.yaml's own default, or the
  one you picked with **apic: Select environment**, passed as `--env` to
  every command.
- **Format Document** for `.http` files through `apic fmt`: directive
  order, header case, JSON bodies, blank lines. With
  `editor.formatOnSave` the files stay canonical.
- **Schemas** for `apic.yaml` (with the
  [YAML extension](https://marketplace.visualstudio.com/items?itemName=redhat.vscode-yaml)),
  `http-client.env.json`, the private file and `.apic/session.json`, so
  the config files get completion and validation, offline.
- Highlights apic's directive lines inside the `http` language, with
  `{{variables}}`, selectors and operators picked out.
- Finds the `apic` binary (on `PATH`, or `apic.path`), checks its version
  and points at the install page when it is missing or too old, and works
  out the project root for a file.

Coming next, tracked in the
[VS Code epic](https://github.com/dataGriff/api-caller/issues/29):
completions and hovers, Test Explorer for `.feature` files, and a
language server for diagnostics as you type.

## Install

From the Marketplace or Open VSX, search for **apic**. Or from a
`.vsix`: every release on the
[releases page](https://github.com/dataGriff/api-caller/releases?q=vscode)
tagged `vscode-v*` carries one; `code --install-extension apic-<version>.vsix`.

## Requirements

apic 0.1.2 or newer on your `PATH`, or its location in the `apic.path`
setting. Install: [getting started](https://datagriff.github.io/api-caller/getting-started/#1-install).
Spans in the Problems panel, `# @ref`, the Session view's cookies and
**Format Document** need the release that carries them (0.2).

## Settings

| Setting | Meaning |
|---|---|
| `apic.path` | Path to the binary. Empty means the first `apic` on `PATH`. |
| `apic.projectDir` | Project root passed as `-C`. Empty means the nearest directory above the active file with `apic.yaml` or `http-client.env.json`, else the workspace folder. |
| `apic.codeLens.enable` | Show the lenses above requests. Default on. |
| `apic.run.extraArgs` | Extra arguments for every run from the editor, for example `["--redact"]`. |
| `apic.run.verbose` | Pass `-v`, so the panel shows request and response headers. |
| `apic.validate.auto` | Validate on activation and whenever a request file, `apic.yaml` or an env file changes on disk. Off, only **apic: Validate the project** runs it. Default on. |
| `apic.validate.debounceMs` | Wait this long after a change before validating, so a burst becomes one run. Default 300. |
| `apic.format.enable` | Offer **Format Document** through `apic fmt`. Default on. |

apic validates from disk, so an unsaved buffer keeps the findings of its
last save; validating as you type needs a language server, which is
tracked separately.

## Developing

```sh
cd editors/vscode
npm ci
npm run check      # lint, typecheck, build, package
npm run test:unit  # the pure logic, under plain node
npm test           # builds, then runs the tests in a real VS Code
```

Press F5 in VS Code with this folder open to run the extension against the
fixture project in `src/test/fixture`.

## Releasing

The extension is versioned on its own. Bump `version` in `package.json`,
add the section to `CHANGELOG.md`, and push a tag `vscode-v<version>`:
the `vscode-release` workflow tests, packages, publishes to the
Marketplace and Open VSX (when the `VSCE_PAT` and `OVSX_PAT` secrets are
set) and attaches the `.vsix` to a GitHub release with the changelog
section as its notes.
