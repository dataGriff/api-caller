# How apic compares

apic is deliberately narrow: run `.http` files anywhere, for humans and
agents, from one static binary. This page is an honest look at where that
lands against the tools you might otherwise use.

## Summary

| | Taskfile + curl | VS Code REST Client / JetBrains | Bruno | Hurl | apic |
|---|---|---|---|---|---|
| Runs with nothing installed but a binary | curl is everywhere | editor only | needs Node for the CLI | yes | yes |
| Request files editors can send with a click | no | yes (`.http`) | Bruno app / extension (`.bru`) | no | yes (`.http`) |
| Environments | shell vars | env files | yes | `--variables-file` | env files, `.env`, shell, `--var` |
| Capture and reuse values | shell plumbing | in-editor only | JS scripts, in one run | yes, in one run | yes, and persisted between runs |
| Assertions | none | none (JetBrains: JS) | yes | yes | yes |
| Structured output for programs | curl's | no | reports (JSON, JUnit, HTML) | JSON report | JSON per request, NDJSON for flows |
| Discovery (`list`, `describe`) | `task --list` | file tree | GUI | no | yes |
| MCP server for agents | no | no | no | no | yes |
| OpenAPI import | no | no | yes | no | yes |
| curl export | is curl | yes | GUI | no | yes |
| Scripting | shell | JetBrains JS | JavaScript | no | no |
| OAuth2 helpers, cookie jar, client certs | via curl flags | some | yes | some | no |
| GraphQL, gRPC, WebSocket | curl for GraphQL | GraphQL | yes | GraphQL | no |
| GUI | no | the editor | yes | no | no |

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
- No OAuth2 flow helpers, cookie jar or client certificates. Token
  endpoints work as ordinary requests with `@capture`.
- HTTP only. No GraphQL-specific tooling (a GraphQL query is just a POST),
  no gRPC, no WebSocket.
- No GUI and no response viewer beyond the terminal; the editors cover
  that.
- No test reporters beyond exit codes and JSON. Pipe `--json` through `jq`
  for JUnit if you need it.
