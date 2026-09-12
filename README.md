# apic

Run API requests from plain `.http` files, in the terminal, in CI, or from
an AI agent, on any platform, with one static binary.

```sh
apic run login                      # POST, capture the token
apic run get-user --env staging     # reuse the token, check assertions
apic run smoke.http --json | jq     # whole file as a flow, one JSON line per request
claude mcp add api -- apic mcp      # let an agent call the same requests as tools
```

## Why

Postman and Bruno are apps. VS Code and JetBrains `.http` files are great
until you leave the editor. `Taskfile` + `curl` runs anywhere but has no
environments, no chaining, no assertions and no output a program can read.

apic takes the `.http` format editors already understand and adds what the
terminal and agents need:

- **Environments** from `http-client.env.json` (the JetBrains / kulala / httpyac convention) plus `.env`, shell and `--var`.
- **Captured variables that persist.** `# @capture token = body.$.access_token` in `login` means the next `apic run get-user`, in a new shell or a new agent call, has `{{token}}`.
- **Assertions** with `# @assert status == 200`, and files that run as ordered flows with a pass/fail summary and exit code.
- **Agent-first output.** `--json` gives a stable object per request; `list` and `describe` make requests discoverable; errors say what to do next.
- **MCP server.** `apic mcp` exposes every request as a tool for Claude Code, Cursor and friends.
- **Escape hatches.** `apic curl <id>` prints the equivalent curl; `apic import openapi.yaml` scaffolds files from a spec.

The same file is clickable in VS Code, JetBrains and Neovim, because apic's
additions are comments.

## Install

```sh
# Go 1.25+
go install github.com/dataGriff/api-caller/cmd/apic@latest

# Linux / macOS
curl -fsSL https://raw.githubusercontent.com/dataGriff/api-caller/main/install.sh | sh

# Windows and everything else: download from GitHub Releases
```

## 60-second tour

```http
# api/auth.http
### Log in and keep the token
# @name login
# @assert status == 200
# @capture token = body.$.access_token
POST {{baseUrl}}/auth/login
Content-Type: application/json

{"user": "{{user}}", "password": "{{password}}"}

### Who am I
# @name whoami
# @assert status == 200
# @assert body.$.email endsWith @example.com
GET {{baseUrl}}/me
Authorization: Bearer {{token}}
```

```json
// api/http-client.env.json
{ "dev": { "baseUrl": "https://dev.example.com", "user": "alice" } }
// api/http-client.private.env.json  (gitignored)
{ "dev": { "password": "s3cret" } }
```

```
$ cd api
$ apic list
ID      METHOD  URL                       FILE          DESCRIPTION
login   POST    {{baseUrl}}/auth/login    auth.http:6   Log in and keep the token
whoami  GET     {{baseUrl}}/me            auth.http:14  Who am I

$ apic run whoami --env dev
error: auth.http:14: missing variable
  {{token}}: it is captured by request "login"; run `apic run login` first, or pass --var token=...

$ apic run login --env dev
POST https://dev.example.com/auth/login
200 OK · 87 ms · 412 B
{ "access_token": "eyJ…" }
✓ status == 200
↳ token = eyJ…

$ apic run whoami --env dev
GET https://dev.example.com/me
200 OK · 41 ms · 96 B
{ "email": "alice@example.com" }
✓ status == 200
✓ body.$.email endsWith @example.com
```

Set `env: dev` in `api/apic.yaml` to drop the `--env` flag.

## Commands

| Command | What it does |
|---|---|
| `apic run <id \| file.http \| file.http#id>...` | Send a request, or a file in order as a flow. `--json`, `--body-only`, `-v` headers, `--var k=v`, `--env`, `--keep-going`, `--no-session`. |
| `apic list` | Every request: id, method, URL template, file:line, description. |
| `apic describe <id>` | Variables the request needs and where each comes from, captures, asserts, and whether it is ready. |
| `apic env` | Environments found and the variables in effect (secrets masked). |
| `apic session [clear]` | Captured values stored in `.apic/session.json`. |
| `apic curl <id>` | Equivalent curl command with variables resolved. |
| `apic import <openapi.yaml>` | One `.http` per tag, one named request per operation, example bodies from schemas. |
| `apic validate` | Parse every file and report problems; non-zero exit on errors. Use it in CI. |
| `apic mcp` | Serve the project to AI agents over MCP (stdio). |

All commands take `--json` and `-C <dir>`. Colour is disabled when output is
not a terminal or `NO_COLOR` is set.

**Exit codes:** `0` ok · `1` assertion or capture failed · `2` usage, parse error or missing variable · `3` network error.

## For agents

Shell: `apic list --json`, `apic describe <id> --json`, `apic run <id> --json`.
MCP: `claude mcp add api -- apic mcp --dir ./api --env dev`.
See [docs/agents.md](docs/agents.md) for the JSON contract and a snippet to
paste into your project's `AGENTS.md`.

## The format

See [docs/format.md](docs/format.md) for the full spec: structure,
directives, variable precedence, built-ins, selectors and assertion
operators. Short version: standard `.http`, plus

```
# @name id                      # @capture name = selector
# @description one line         # @assert selector op value
# @no-redirect  # @no-session   # @timeout 10s
```

## Keeping a Taskfile

If your team already runs `task api:...`, keep it and delegate:

```yaml
tasks:
  api:
    desc: Run an API request, e.g. task api -- get-user --env staging
    cmds: [apic run {{.CLI_ARGS}}]
```

## Development

```sh
task build && ./bin/apic list -C examples/httpbin
task test
task lint
```

Tests run against local `httptest` servers; no network is needed. The
`examples/httpbin` project targets httpbin.org for a live demo.
