# apic for VS Code

Run the `.http` files you already have with [apic](https://datagriff.github.io/api-caller/):
environments, captured variables that persist, assertions, AWS and OAuth2
auth, Gherkin tests and an MCP server, from one static binary. This
extension drives that binary from the editor, so what you see here is what
CI and your agents get from the same files.

It works alongside REST Client: that extension keeps its language, its
highlighting and its "Send Request"; this one layers apic's directives and
commands on top.

## What it does today

This is the first release, the scaffold the rest is built on:

- Highlights apic's `# @name`, `# @assert`, `# @capture`, `# @auth`,
  `# @step` and the other directive lines, with `{{variables}}`, selectors
  and operators picked out, without taking over the `http` language.
- Finds the `apic` binary (on `PATH`, or `apic.path`), checks its version
  and points at the install page when it is missing or too old.
- Works out the project root for a file: the nearest directory above it
  holding `apic.yaml` or `http-client.env.json`, or `apic.projectDir`.
- **apic: Show version** in the command palette.

Coming next, each tracked in the
[VS Code epic](https://github.com/dataGriff/api-caller/issues/29): run,
describe and copy-as-curl CodeLens above each request, diagnostics from
`apic validate`, a response viewer, an environment picker with session and
request views, completions and hovers, Test Explorer for `.feature` files.

## Requirements

apic 0.1.2 or newer on your `PATH`, or its location in the `apic.path`
setting. Install: [getting started](https://datagriff.github.io/api-caller/getting-started/#1-install).

## Settings

| Setting | Meaning |
|---|---|
| `apic.path` | Path to the binary. Empty means the first `apic` on `PATH`. |
| `apic.projectDir` | Project root passed as `-C`. Empty means the nearest directory above the active file with `apic.yaml` or `http-client.env.json`, else the workspace folder. |

## Developing

```sh
cd editors/vscode
npm ci
npm run check      # lint, typecheck, build, package
npm test           # builds, then runs the tests in a real VS Code
```

Press F5 in VS Code with this folder open to run the extension against the
fixture project in `src/test/fixture`.
