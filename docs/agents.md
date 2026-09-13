# Using apic from an AI agent

apic is designed so an agent can drive an API with the same files a human
uses in their editor. Two integration styles are available.

## 1. Shell (works with any agent that can run commands)

Put this in the project's `AGENTS.md` or `CLAUDE.md`:

```markdown
## Calling the API

Requests live in `api/*.http` and are run with `apic` (install: see README).

- `apic list --json` — every request with id, method, URL and description
- `apic describe <id> --json` — variables the request needs and whether it is ready
- `apic run <id> --json` — send it; prints one JSON object with `ok`, `request`, `response`, `captures`, `asserts`
- `apic run <id> --body-only` — just the response body, for piping to jq
- `apic run <file>.http --json` — run a whole file in order as a flow (NDJSON)
- `apic run <id> --var name=value` — override a variable
- `apic run <id> --env staging` — pick an environment from http-client.env.json
- `apic curl <id>` — the equivalent curl command
- `apic test --json` — run the Gherkin features in features/; `apic test --steps --json` lists the steps you may use

Exit codes: 0 ok, 1 assertion failed, 2 usage/parse/missing variable, 3 network.
Values captured with `# @capture` (like a login token) persist in `.apic/session.json`,
so run `login` once and dependent requests will find the token. If a request reports a
missing variable, the error says which request captures it.
```

The `--json` shape is stable:

```json
{
  "ok": true,
  "request": {"name": "get-user", "file": "users.http", "line": 10, "method": "GET", "url": "https://…", "headers": {"Accept": "application/json"}},
  "response": {"status": 200, "status_text": "OK", "headers": {"content-type": "application/json"}, "body": {"id": 42}, "duration_ms": 87, "size": 412},
  "captures": {"email": "a@b.c"},
  "asserts": [{"expr": "status == 200", "pass": true, "actual": "200", "expected": "200"}],
  "errors": []
}
```

`response.body` is parsed JSON when the body is JSON, otherwise a string.
`request.auth` names the auth type applied (`aws`, `oauth2`, …) without
exposing credentials, and sensitive request headers are shown as `***`;
see [auth.md](auth.md). URL, body and captures are real values so an agent
can chain them. When the output is going into a stored log rather than to
an agent, add `--redact`.

## 2. MCP (Claude Code, Cursor, Windsurf, any MCP client)

`apic mcp` serves the project over stdio. Register it once:

```sh
# Claude Code
claude mcp add api -- apic mcp --dir ./api --env dev

# Generic MCP client config
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
| `run_features {paths?, tags?, env?, vars?}` | run Gherkin features; returns pass/fail counts and the failing steps (see [testing.md](testing.md)) |

Each `.http` file is also exposed as a resource so the agent can read the
definitions. Assertion failures return `ok: false` rather than a tool error;
transport and usage problems return an error message the agent can act on.

## Writing requests as an agent

The format is plain text, so an agent can add requests too. Keep to the
subset in [format.md](format.md), give every request a `# @name`, add an
`# @assert status == 200` and run `apic validate` afterwards.
