# Using apic from an AI agent

apic is designed so an agent can drive an API with the same files a human
uses in their editor. Two integration styles are available.

## 1. Shell (works with any agent that can run commands)

Put this in the project's `AGENTS.md` or `CLAUDE.md`:

```markdown
## Calling the API

Requests live in `api/*.http` and are run with `apic` (install: see README).

- `apic list --json`: every request with id, method, URL and description
- `apic describe <id> --json`: variables the request needs and whether it is ready
- `apic run <id> --json`: send it; prints one JSON object with `ok`, `request`, `response`, `captures`, `asserts`
- `apic run <id> --body-only`: just the response body, for piping to jq
- `apic run <file>.http --json`: run a whole file in order as a flow (NDJSON)
- `apic run <id> --var name=value`: override a variable
- `apic run <id> --env staging`: pick an environment from http-client.env.json
- `apic curl <id>`: the equivalent curl command
- `apic test --json`: run the Gherkin features in features/; `apic test --steps --json` lists the steps you may use
- `apic validate --json`: parse every file and report problems before running anything

Exit codes: 0 ok, 1 assertion failed, 2 usage/parse/missing variable, 3 network.
Values captured with `# @capture` (like a login token) persist in `.apic/session.json`,
so run `login` once and dependent requests will find the token. A request that declares
`# @ref login` runs login by itself when the token is missing. If a request reports a
missing variable, the error says which request captures it.
```

The `--json` shape is stable:

```json
{
  "ok": true,
  "request": {"name": "get-user", "file": "users.http", "line": 10, "method": "GET", "url": "https://...", "headers": {"Accept": "application/json"}},
  "response": {"status": 200, "status_text": "OK", "headers": {"content-type": "application/json"}, "body": {"id": 42}, "duration_ms": 87, "size": 412},
  "captures": {"email": "a@b.c"},
  "asserts": [{"expr": "status == 200", "pass": true, "actual": "200", "expected": "200"}],
  "errors": []
}
```

`response.body` is parsed JSON when the body is JSON, otherwise a string.
`request.auth` names the auth type applied (`aws`, `oauth2` and so on) without
exposing credentials, and sensitive request headers are shown as `***`;
see [auth.md](auth.md). URL, body and captures are real values so an agent
can chain them, and `set-cookie` is masked in the response headers. When the
output is going into a stored log rather than to an agent, add `--redact`,
which masks the response body and headers as well as the request — so a
redacted run is for logs, not for chaining.

A session usually looks like this:

```console
$ apic list --json | jq -r '.requests[] | "\(.id)\t\(.method) \(.url)"'
login   POST {{baseUrl}}/auth/login
whoami  GET {{baseUrl}}/me

$ apic describe whoami --json | jq '{ready, missing: [.variables[] | select(.missing) | .name]}'
{"ready": false, "missing": ["token"]}

$ apic run login --json | jq '{ok, captured: .captures}'
{"ok": true, "captured": {"token": "mock-token"}}

$ apic run whoami --json | jq '{ok, status: .response.status, body: .response.body}'
{"ok": true, "status": 200, "body": {"email": "alice@example.com"}}
```

The agent never has to guess: `describe` says what is missing and which
request provides it, and the token persists, so `login` is run once rather
than before every call. When `whoami` declares `# @ref login`, even that
step goes away: `apic run whoami --json` prints `login`'s object and then
`whoami`'s, and `describe` reports `"ready": true` with
`"ref_runs": true` on the token.

## 2. MCP (Claude Code, Cursor, Windsurf, any MCP client)

`apic mcp` serves the project over stdio. Register it once:

```sh
# Claude Code
claude mcp add api -- apic mcp --dir ./api --env dev
```

```json
// Cursor (.cursor/mcp.json), Windsurf, and any other MCP client
{"mcpServers": {"api": {"command": "apic", "args": ["mcp", "--dir", "./api", "--env", "dev"]}}}
```

Tools exposed:

| Tool | Purpose |
|---|---|
| `list_requests` | ids, methods, URL templates, descriptions, captures, asserts |
| `describe_request {name, env?}` | variables and sources, `ready` flag |
| `run_request {name, env?, vars?}` | send one request; returns the same JSON as `apic run --json` |
| `run_file {file, env?, vars?, keep_going?}` | run a file as a flow |
| `list_environments {env?}` | environments and effective variables (secrets masked) |
| `clear_session {env?, all?}` | forget captured values |
| `run_features {paths?, tags?, env?, vars?, use_session?}` | run Gherkin features; returns pass/fail counts and the failing steps. Scenarios are isolated unless `use_session` shares `.apic/session.json` with the other tools (see [testing.md](testing.md)) |

Each `.http` file is also exposed as a resource so the agent can read the
definitions. Only the project's own `.http` and `.rest` files can be read this
way: any other path is refused, so `http-client.private.env.json`, `.env` and
`.apic/session.json` are never served to the agent even though they sit inside
the project. The readable set is checked against the project on each read, so a
file added after the server started can be read (it is not listed until the
server restarts). Assertion failures return `ok: false` rather than a tool error;
transport and usage problems return an error message the agent can act on.

## Writing requests as an agent

The format is plain text, so an agent can add requests too. Keep to the
subset in [format.md](format.md), give every request a `# @name`, add an
`# @assert status == 200` and run `apic validate` afterwards. `apic init`
writes a correct skeleton to start from, and the
[cheat sheet](cheatsheet.md) fits in a prompt.
