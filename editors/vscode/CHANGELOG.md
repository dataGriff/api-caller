# Changelog

## Unreleased

- CodeLens above every request: **Run**, **Describe**, **Copy as curl**,
  and **Run file as flow** at the top of a file; `ctrl+alt+r` runs the
  request under the cursor. After a run the request line shows
  `✓ 200 · 12 ms`.
- A response panel: status, headers, highlighted body with raw toggle and
  save, assertions with actual against expected, captures, errors; flows
  with a summary; a "redacted" badge; **apic: Show last response**.
- Problems from `apic validate` on open and save, with spans and codes,
  quick fixes for a mistyped directive, a duplicate name and a missing body
  file, and a problem count in the status bar.
- An environment picker per project, in the status bar.
- First scaffold: directive highlighting injected into the `http` language,
  binary discovery with a version check and install prompt, project root
  discovery, and the **apic: Show version** command.
