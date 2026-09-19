# Contributing

Thanks for looking. Issues and pull requests are welcome.

If you are an AI agent working in this repository, read
[AGENTS.md](AGENTS.md) instead — it is the same ground in more detail, plus the
layout and the conventions that are easy to break.

## Getting set up

You need Go (the version in `go.mod`) and, for the shortcuts below,
[Task](https://taskfile.dev).

```sh
git clone https://github.com/dataGriff/api-caller
cd api-caller
task build          # -> bin/apic
task test
```

No network is needed: the tests run against `net/http/httptest` servers, and
`task example:demo` exercises the whole tool end to end against a fake API
served in-process.

```sh
task --list         # every task, with descriptions
task check          # what CI runs: lint, test, race, licences, examples
```

Run `task check` before opening a pull request. It is the same set CI runs, so
a green run locally usually means a green run there.

Other tasks worth knowing: `task fmt` (lint only *checks* formatting),
`task cover`, `task vuln`, `task snapshot` (build the release archives without
publishing), `task docs` (preview the documentation site).

## What makes a change easy to accept

- **A test next to it.** Parser and runner changes especially. A good test is
  one that fails without the fix — worth actually checking by reverting the fix
  and watching it go red.
- **Docs updated in the same change.** A new command needs a row in the README
  table, a section in `docs/cli.md` and a line in `docs/cheatsheet.md`; a new
  `.http` feature needs `docs/format.md`.
- **A commit message that says why.** What changed is in the diff; the reason is
  not.

## Writing a course lesson

The **Learn** tab of the docs is the "From zero to apic" course, one page
per lesson under `docs/learn/`. [Writing a lesson](docs/learn/_template.md)
is the template and the rules; the short version:

- Second person, one concept per lesson, every command copy-pasteable with
  its output shown, every step against `apic demo`.
- Put `<!-- learn -->` on the line before a fenced block that should be
  run in CI. `task learn:check` builds apic, serves the demo and runs those
  blocks in page order; CI does the same on every pull request.
- The episode script and chapter timestamps live on the page, collapsed at
  the end, so the video and the text cannot drift apart.

## Conventions that are easy to trip over

- **The `.http` dialect stays compatible** with VS Code REST Client and
  JetBrains. New features go in `# @directive` comments before the request
  line, never as new syntax in the request itself, so the same file still works
  in an editor.
- **`--json` output and exit codes are a public contract.** Change them only
  with a note in the README.
- **Every command works non-interactively** and respects `--json`. `apic ui` is
  the one deliberate exception.
- **The dependency list stays small.** A few hundred lines of code beats a large
  SDK — the AWS SigV4 signer and the OpenAPI reader are the precedents. Check
  the binary size with `task build && ls -la bin/apic` if you add one, and
  expect to justify it.
- **Nothing with an init-time terminal query** goes in the binary. Bubble Tea
  was tried and removed because its package `init` queries the terminal for its
  background colour with a five-second timeout, which every apic command would
  have paid on terminals that do not answer.
- **Credentials are the thing to be careful with.** If your change touches
  output, redaction, the session file or the MCP server, re-read
  [SECURITY.md](SECURITY.md) — the guarantees listed there are meant to hold.

## Linting

`.golangci.yml` runs the standard set plus `bodyclose`, `copyloopvar`,
`errorlint`, `gosec`, `misspell` and `unparam`. Each was measured against the
tree before being enabled, so the config is quiet on purpose: a new finding
usually means something.

A `//nolint` needs a reason on the same line saying why the call is safe. The
existing ones are the place to look before adding another.

## Reporting a security problem

Not here — see [SECURITY.md](SECURITY.md). Please do not open a public issue for
a vulnerability.
