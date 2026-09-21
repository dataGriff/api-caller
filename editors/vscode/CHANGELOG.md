# Changelog

## Unreleased

- Completions: directives after `# @` with snippet bodies, variables
  inside `{{` from `apic env` and the session with built-ins and
  response references, selectors after `# @assert` and `# @capture x =`
  with the keys of the last response after `body.$.`, operators, auth
  types and request names after `# @ref`. Snippets for a request, a JSON
  request, a login-and-capture pair and the common directives.
- Hovers on `{{placeholder}}`: value, source, and for a missing one the
  request that captures it, from `apic describe`.
- Test Explorer: every `.feature` file under the project's `test.paths`
  with scenarios and example rows, run through `apic test --json` in the
  environment in effect, failing steps at their line, undefined steps
  called out, a tag expression per run or for every run, and
  `apic.test.showOutput` for apic's own pretty output. A lens above each
  `# @step` line counts the scenarios that use the phrase and opens them.

## 0.1.0

The first release. Everything runs through the apic binary's `--json`
contract; the extension never parses `.http` files itself.

- CodeLens above every request: **Run**, **Describe**, **Copy as curl**,
  and **Run file as flow** at the top of a file; `ctrl+alt+shift+r` runs
  the request under the cursor. After a run the request line shows
  `✓ 200 · 12 ms`.
- A response panel: status, headers, highlighted body with raw toggle and
  save, assertions with actual against expected, captures, errors; flows
  with a summary; a "redacted" badge; **apic: Show last response**.
- Problems from `apic validate` on activation and on every change of a
  request or config file on disk, with spans and codes, quick fixes for a
  mistyped directive, a duplicate name and a missing body file, and a
  problem count in the status bar.
- An apic view container: **Requests** (every request by file, with a
  ready/not-ready icon, Run, Describe and Copy as curl inline, a click
  opening it in the editor) and **Session** (captured values and the
  cookies in the jar for the environment in effect, with Clear session).
- The environment in the status bar, apic.yaml's own default when
  nothing is picked; **apic: Select environment** to change it.
- **Format Document** for `.http` files through `apic fmt`, so
  `editor.formatOnSave` keeps them canonical.
- Schema validation and completion for `apic.yaml` (with the YAML
  extension), `http-client.env.json`, the private file and
  `.apic/session.json`, from schemas bundled with the extension.
- Directive highlighting injected into the `http` language, binary
  discovery with a version check and install prompt, project root
  discovery, and **apic: Show version**.
