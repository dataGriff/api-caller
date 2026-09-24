# How apic compares

apic is deliberately narrow: run `.http` files anywhere, for humans and
agents, from one static binary. This page is an honest look at where that
lands against the tools you might otherwise use. To move a project across,
see the migration guides: from [Postman](migrate/postman.md),
[Bruno](migrate/bruno.md), [Hurl](migrate/hurl.md),
[httpyac](migrate/httpyac.md), and [curl and Taskfile](migrate/curl.md).

## Summary

"Editors" is VS Code REST Client and the JetBrains HTTP Client inside the
IDE; `ijhttp` is JetBrains' command-line runner for the same files, and
[Kulala.nvim](https://github.com/mistweaverco/kulala.nvim) runs them in
Neovim. The other tools' cells follow their own documentation as of
September 2026; they move fast, so a correction is a welcome pull request.

| | Taskfile + curl | Editors | ijhttp | Kulala.nvim | httpyac | Bruno | Hurl | apic |
|---|---|---|---|---|---|---|---|---|
| Runs with nothing installed but a binary | curl is everywhere | editor only | needs a JVM (or Docker) | needs Neovim and curl | needs Node | needs Node for the CLI | yes | yes |
| Request files editors can send with a click | no | yes (`.http`) | yes (`.http`) | yes (`.http`) | yes (`.http`) | Bruno app / extension (`.bru`) | no | yes (`.http`) |
| Environments | shell vars | env files | env files | env files, `.env` | env files, `.env` | yes | `--variables-file` | env files, `.env`, shell, `--var` |
| Capture and reuse values | shell plumbing | in-editor only | JS handlers, in one run | request references and scripts, in the editor | yes, in one run | JS scripts, in one run | yes, in one run | yes, and persisted between runs |
| Assertions | none | none (JetBrains: JS) | JS (`client.test`) | JS or Lua | yes, plus JS | yes | yes | yes: JSONPath filters, types, length, JSON Schema |
| Structured output for programs | curl's | no | JUnit report | no | `--json`, JUnit | reports (JSON, JUnit, HTML) | JSON report | JSON per request, NDJSON for flows, stable error codes |
| Discovery (`list`, `describe`) | `task --list` | file tree | no | in the editor | no | GUI | no | yes |
| Never prompts | yes | n/a | yes | n/a | picker unless `--all`/`--name` | yes | yes | yes |
| MCP server for agents | no | no | no | no | no | no | no | yes |
| Gherkin features without a Cucumber runtime | no | no | no | no | no | no | no | yes (`apic test`) |
| Data-driven runs | shell loop | no | no | no | `@loop` | CSV and JSON in the CLI | no | yes (`run --data`) |
| OpenAPI import | no | no | no | no | no | yes | no | yes |
| Postman import | no | no | no | no | no | yes | no | yes (`apic import`, with environments and simple tests) |
| curl export and import | is curl | yes | no | yes | extension | GUI | no | yes, plus HTTPie, PowerShell, Python, JavaScript and Go snippets |
| Scripting | shell | JetBrains JS | JavaScript | JavaScript, Lua | JavaScript | JavaScript | no | no |
| Auth helpers | via curl flags | some | basic, digest, OAuth2 (`Security.Auth`) | via curl, OAuth2 (`Security.Auth`) | OAuth2 (all flows), AWS, basic, digest | OAuth2, AWS, basic, digest | basic, AWS, digest | AWS SigV4 (no SDK), OAuth2 (client credentials, password, device code, authorization code with PKCE; JetBrains `Security.Auth` and `$auth.token`), digest, API key, basic, bearer, exec |
| Cookie jar | via curl flags | some | yes | via curl | yes | yes | yes | yes (opt-in, per environment) |
| Proxy | `-x` | IDE settings | `--proxy` | via curl | yes | yes | `-x` | yes (`--proxy`, `proxy:` and `noProxy:` in apic.yaml, `HTTP(S)_PROXY`) |
| Client certificates, private CAs | via curl flags | JetBrains | `SSLConfiguration` | via curl | yes | yes | yes | yes (`tls:` in apic.yaml, flags, JetBrains `SSLConfiguration`) |
| Multipart uploads with file parts | `curl -F` | yes | yes | yes | yes | yes | yes | yes |
| HTTP version on the request line | `--http2` | JetBrains | yes | via curl | yes | no | `--http2` | yes (HTTP/1.1, HTTP/2) |
| GraphQL, gRPC, WebSocket | curl for GraphQL | GraphQL (JetBrains: all three) | GraphQL, WebSocket | GraphQL, gRPC, WebSocket | GraphQL, gRPC, WS, MQTT, AMQP | yes | GraphQL | GraphQL; the rest are [not planned](#deliberately-not-planned) |
| GUI | no | the editor | no | Neovim | VS Code extension | yes | no | terminal UI (`apic ui`) and a VS Code extension |

What is coming is tracked in the parity epics:
[the `.http` dialect](https://github.com/dataGriff/api-caller/issues/24),
[the runner, auth and CLI](https://github.com/dataGriff/api-caller/issues/25),
[distribution](https://github.com/dataGriff/api-caller/issues/26) and
[the VS Code extension](https://github.com/dataGriff/api-caller/issues/29).

## Size

A stripped apic binary is about 16 MB, in the same range as `task` or `yq`
and well below `gh`, `kubectl` or `terraform`. The largest pieces are the
MCP SDK and the Gherkin runner; the OpenAPI importer, the AWS signing and
the terminal UI's event loop are written in-tree to keep them small.

## Against Taskfile + curl

This is what apic replaces. You keep Task if you like it
([taskfile.md](taskfile.md)); the requests move out of shell strings into
`.http` files. You gain environments, a persisted token, assertions,
`--json`, `list`/`describe`, and files an editor can send. You lose
nothing: `apic curl` gives the curl back.

## Against VS Code REST Client and JetBrains HTTP Client

apic uses their format, so this is not either/or. The editors give humans
the click experience; apic gives the same files to the terminal, CI and
agents. What the editors have that apic does not: JetBrains' JavaScript response
handlers, and the in-editor response viewer. apic reads the common subset
and ignores what it does not know, so a file with editor-only features still
parses: a `> {% … %}` response handler or a `< {% … %}` pre-request script is
skipped rather than sent, and `apic validate` lists what was skipped. A
`>> ./file` line, which both editors use to save the response, works the
same under apic, and so do JetBrains' `SSLConfiguration` and
`Security.Auth` blocks in the env files and `{{$auth.token("name")}}`.

## Against ijhttp

`ijhttp` is the JetBrains HTTP Client on the command line: the same files,
the same env files, JavaScript handlers and a JUnit report, which makes it
the most faithful way to run a JetBrains project in CI. It needs a JVM or
the Docker image, keeps no state between invocations, and has no
discovery, JSON output or agent integration. apic runs the same files
without the JavaScript (handlers are skipped and `apic validate` says
so), and reads `SSLConfiguration`, `Security.Auth` and `$auth.token`
from the same env files. A project can use both: the IDE and `ijhttp` for
the scripted checks, apic for agents and the terminal.

## Against Kulala.nvim

Kulala.nvim is the `.http` client for Neovim: it sends through curl,
reads the JetBrains env files, chains requests by reference, runs
JavaScript or Lua scripts, and reaches gRPC and WebSocket through
`grpcurl` and `websocat`. It is an editor experience, like REST Client
is for VS Code. apic is not a replacement for it but a partner: the same
files run under apic in CI and for agents, and apic's editor notes for
Neovim are in [editors.md](editors.md).

## Against httpyac

httpyac is the closest existing tool: it runs the same `.http` files, reads
the same `http-client.env.json` and `.env`, and ships a VS Code extension.
It is far richer: JavaScript blocks and handlers, `@loop` and `@import`,
every OAuth2 flow, AWS and digest auth, GraphQL, gRPC, WebSocket, MQTT and
AMQP, JUnit output, and a plugin system. Its `# @ref` and `# @forceRef`
mean the same thing in apic. If Node is acceptable everywhere you run requests, take it
seriously.

apic differs in the ways that matter for agents and locked-down machines: it
is one binary with no Node; captured values and tokens persist between
invocations; it never opens an interactive picker; it has `list`,
`describe` and the missing-variable hints; and it has an MCP server, curl
export and OpenAPI import in the CLI. The two can share one project:
httpyac in the editor, apic for agents and CI.

## Against Bruno

Bruno is a full product: a GUI, JavaScript pre and post request scripts,
OAuth2 flows, secret manager integrations, GraphQL, gRPC and WebSocket, CI
reporters, and Postman import. If you want all of that, use Bruno.

apic's advantages are specific:

- **No Node.** The `bru` CLI is an npm package. apic is one binary.
- **Editor-native files.** `.bru` needs Bruno or its extension. `.http` is
  understood by VS Code, JetBrains and Neovim out of the box.
- **State between invocations.** `bru run` is stateless; a token set in one
  run is gone in the next. apic's session keeps captures per environment,
  which is what an agent issuing one command at a time needs.
- **Built for agents.** `list`, `describe`, missing-variable hints, a stable
  JSON object per request, and an MCP server in the box.

If Node is already everywhere and your team lives in the Bruno app, apic's
edge is thin. A small MCP wrapper around `bru` would get you part of the
agent story.

## Against Hurl

Hurl is the closest single-binary alternative: a plain-text format with
assertions and captures, fast, well tested, and richer than apic in
protocol details. The contract checks Hurl users write (`isInteger`,
`count == 3`, a JSON Schema) have apic equivalents: `isInteger`,
`length == 3`, `matchesSchema`. If you do not care about the
`.http` format, editor support, persisted sessions or MCP, Hurl is a fine
choice.

apic differs in three ways: the files are standard `.http` and clickable in
editors; captures persist between runs; and there is a discovery and MCP
layer for agents. Hurl also runs a file top to bottom only, where apic can
address one named request. Both retry a request until its assertions pass
(Hurl's `[Options] retry:`, apic's `# @retry`).

## Against Postman

Postman is a hosted product with collaboration, mocking, monitoring and a
cloud workspace. apic is a local file runner with none of that. The overlap
is only "send a request with variables and check the result", and there
apic's answer is plain files in git, no account, no runtime.
`apic import collection.postman.json` brings a collection across: folders,
requests, variables, environments, auth and the simple `pm.test` checks;
pre-request scripts and the rest of the test scripts are listed as notes,
since apic has no scripting. Their usual jobs have homes: a computed value
comes from `--var` or `APIC_VAR_*`, a token from `# @auth` or a `# @ref`
request, a check from `# @assert`.

## What apic does not do yet

So you are not surprised later:

- OAuth2 is the four common grants (client credentials, password, device
  code, authorization code with PKCE), AWS SigV4, digest, API keys, basic
  and command-provided tokens; see [auth.md](auth.md). Implicit and
  hybrid flows are not there, and the browser flow is for a person at a
  terminal, never an agent.
- A GraphQL query is sent the way the editors write it (the `GRAPHQL`
  method or `X-REQUEST-TYPE: GraphQL`, becoming a JSON POST), but there is
  no GraphQL schema tooling.
- Reports are limited to what `apic test` emits (pretty, progress, cucumber
  JSON, JUnit, HTML) and `run --json` or `run --report`.
- Installing is `go install`, the install script, a release archive or the
  [GitHub Action](getting-started.md#7-put-it-in-ci); package managers are on the
  [distribution epic](https://github.com/dataGriff/api-caller/issues/26).

## Deliberately not planned

These come up often enough to answer once. Each would make apic a
different tool, so they are not on any roadmap, and each has an escape
hatch:

| Not planned | Why | Instead |
|---|---|---|
| Scripting and response handlers | The moment a request file can compute things, it stops being a file an editor can send and a reader can trust. | `--json` into a shell script or `jq`; `# @capture` and `# @assert` for the common jobs; `# @auth exec` for credentials that come from a tool. |
| Custom step definitions | The vocabulary plus `# @step` phrases on requests is the whole language, which keeps features runnable by anyone with the binary and no project code. | A `# @step` phrase on a request, or a shell script around `apic run --json`. |
| gRPC | Another protocol with its own schema and tooling; apic is an HTTP runner. | `grpcurl`, or Kulala.nvim and httpyac, which drive it. |
| WebSocket | A conversation rather than a request and a response, which the `.http` model and the one-result-per-request JSON do not fit. | `websocat`, or the tools above. |
| MQTT and AMQP | Messaging, not HTTP. | The broker's own client, or httpyac. |
| A GUI beyond the terminal UI and the VS Code extension | The editors already are the GUI for `.http` files; apic's job is everywhere else. | `apic ui`, the [VS Code extension](editors.md), or any editor that reads `.http`. |
| A hosted workspace | apic is plain files in git with no account; collaboration is the repository. | Git, pull requests and CI with the [GitHub Action](getting-started.md#7-put-it-in-ci). |
