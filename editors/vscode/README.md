# apic for VS Code

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
  <kbd>ctrl+alt+r</kbd> (<kbd>cmd+alt+r</kbd>) runs the request under the
  cursor.
- **A response panel** beside the editor: status, timing and size, headers
  (collapsed), the body pretty-printed and highlighted with a raw toggle and
  a save button, every assertion with actual against expected, captures,
  errors. A flow shows its pass/fail summary and one collapsible block per
  request. **apic: Show last response** brings it back.
- **Problems from `apic validate`**: every error and warning apic reports
  (bad selectors, unknown directives, duplicate names, missing body files,
  `# @ref` cycles) appears in the Problems panel at the exact span, with
  its code linked to the docs, refreshed when a request file, `apic.yaml`
  or an env file is saved. Quick fixes change a mistyped directive to the
  nearest known one, rename a duplicate, or create a missing body file. The
  status bar shows the count.
- **An environment** per project, picked from the status bar or **apic:
  Pick environment**, passed as `--env` to every command.
- Highlights apic's directive lines inside the `http` language, with
  `{{variables}}`, selectors and operators picked out.
- Finds the `apic` binary (on `PATH`, or `apic.path`), checks its version
  and points at the install page when it is missing or too old, and works
  out the project root for a file.

Coming next, tracked in the
[VS Code epic](https://github.com/dataGriff/api-caller/issues/29): session
and request views, completions and hovers, Test Explorer for `.feature`
files, and a Marketplace release.

## Requirements

apic 0.1.2 or newer on your `PATH`, or its location in the `apic.path`
setting. Install: [getting started](https://datagriff.github.io/api-caller/getting-started/#1-install).
Spans in the Problems panel and `# @ref` need 0.2.

## Settings

| Setting | Meaning |
|---|---|
| `apic.path` | Path to the binary. Empty means the first `apic` on `PATH`. |
| `apic.projectDir` | Project root passed as `-C`. Empty means the nearest directory above the active file with `apic.yaml` or `http-client.env.json`, else the workspace folder. |
| `apic.codeLens.enable` | Show the lenses above requests. Default on. |
| `apic.run.extraArgs` | Extra arguments for every run from the editor, for example `["--redact"]`. |
| `apic.run.verbose` | Pass `-v`, so the panel shows request and response headers. |
| `apic.validate.onSave` | Validate the project when a request file, `apic.yaml` or an env file is opened or saved. Default on. |
| `apic.validate.debounceMs` | Wait this long after a save before validating. Default 300. |

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
