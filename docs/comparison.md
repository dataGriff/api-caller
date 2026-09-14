# How apic compares

apic is deliberately narrow: run `.http` files anywhere, for humans and
agents, from one static binary. This page is an honest look at where that
lands against the tools you might otherwise use.

## Summary

| | Taskfile + curl | VS Code REST Client / JetBrains | httpyac | Bruno | Hurl | apic |
|---|---|---|---|---|---|---|
| Runs with nothing installed but a binary | curl is everywhere | editor only | needs Node | needs Node for the CLI | yes | yes |
| Request files editors can send with a click | no | yes (`.http`) | yes (`.http`) | Bruno app / extension (`.bru`) | no | yes (`.http`) |
| Environments | shell vars | env files | env files, `.env` | yes | `--variables-file` | env files, `.env`, shell, `--var` |
| Capture and reuse values | shell plumbing | in-editor only | yes, in one run | JS scripts, in one run | yes, in one run | yes, and persisted between runs |
| Assertions | none | none (JetBrains: JS) | yes, plus JS | yes | yes | yes |
| Structured output for programs | curl's | no | `--json`, JUnit | reports (JSON, JUnit, HTML) | JSON report | JSON per request, NDJSON for flows |
| Discovery (`list`, `describe`) | `task --list` | file tree | no | GUI | no | yes |
| Never prompts | yes | n/a | picker unless `--all`/`--name` | yes | yes | yes |
| MCP server for agents | no | no | no | no | no | yes |
| Gherkin features without a Cucumber runtime | no | no | no | no | no | yes (`apic test`) |
| OpenAPI import | no | no | no | yes | no | yes |
| curl export | is curl | yes | extension | GUI | no | yes |
| Scripting | shell | JetBrains JS | JavaScript | JavaScript | no | no |
| Auth helpers | via curl flags | some | OAuth2 (all flows), AWS, basic, digest | OAuth2, AWS, basic, digest | basic, AWS, digest | AWS SigV4 (no SDK), OAuth2 (client credentials, password, device code), basic, bearer, exec |
| Cookie jar, client certs | via curl flags | some | yes | yes | yes | no |
| GraphQL, gRPC, WebSocket | curl for GraphQL | GraphQL | GraphQL, gRPC, WS, MQTT, AMQP | yes | GraphQL | no |
| GUI | no | the editor | VS Code extension | yes | no | terminal UI (`apic ui`) |

## Size

A stripped apic binary is about 15 MB, in the same range as `task` or `yq`
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
agents. JetBrains has its own CLI runner (`ijhttp`), which needs a JVM.
What the editors have that apic does not: JetBrains' JavaScript response
handlers, and the in-editor response viewer. apic reads the common subset
and ignores what it does not know, so a file with editor-only features still
parses; run `apic validate` to see what is skipped.

## Against httpyac

httpyac is the closest existing tool: it runs the same `.http` files, reads
the same `http-client.env.json` and `.env`, and ships a VS Code extension.
It is far richer: JavaScript blocks and handlers, `# @ref` to run
dependencies automatically, `@loop` and `@import`, every OAuth2 flow, AWS
and digest auth, GraphQL, gRPC, WebSocket, MQTT and AMQP, JUnit output, and
a plugin system. If Node is acceptable everywhere you run requests, take it
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
assertion predicates and protocol details. If you do not care about the
`.http` format, editor support, persisted sessions or MCP, Hurl is a fine
choice.

apic differs in three ways: the files are standard `.http` and clickable in
editors; captures persist between runs; and there is a discovery and MCP
layer for agents. Hurl also runs a file top to bottom only, where apic can
address one named request.

## Against Postman

Postman is a hosted product with collaboration, mocking, monitoring and a
cloud workspace. apic is a local file runner with none of that. The overlap
is only "send a request with variables and check the result", and there
apic's answer is plain files in git, no account, no runtime.

## What apic does not do

So you are not surprised later:

- No scripting. Anything needing computed signatures, loops or conditional
  logic has nowhere to go. The escape hatch is a shell script around
  `apic run --json`.
- No browser-based OAuth2 flows (authorization code, PKCE), no cookie jar,
  no client certificates. Client credentials, password and device code
  grants, AWS SigV4, basic and command-provided tokens are covered in
  [auth.md](auth.md).
- HTTP only. No GraphQL-specific tooling (a GraphQL query is just a POST),
  no gRPC, no WebSocket.
- No GUI and no response viewer beyond the terminal; the editors cover
  that.
- Reports are limited to what `apic test` emits (pretty, progress, cucumber
  JSON, JUnit) and `run --json`; there is no HTML report.
