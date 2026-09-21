# Lesson 10: Agents and MCP

**Goal.** Let an AI agent discover, describe and run your requests, from
the shell through the JSON contract and over MCP as tools, without ever
handing it a secret.

## You will need

- apic installed ([lesson 1](01-first-request.md))
- `apic demo` running in another terminal, or start it now:

```sh
apic demo --out apic-demo
```

- `jq`, because that is what an agent reaches for too
- For step 5 only: an MCP client such as Claude Code, Cursor or Windsurf.
  Every other step works with a plain shell, and so does the checkpoint.

## Steps

### 1. The contract an agent reads

An agent that can run shell commands needs three things from a tool:
output it can parse, exit codes it can branch on, and no prompts that
would leave it hanging. apic gives all three to every command, and the
[agents guide](../agents.md) has the paragraph to paste into your
project's `AGENTS.md` or `CLAUDE.md`. The short version:

```markdown
Requests live in `*.http` and are run with `apic`.
- `apic list --json`: every request with id, method, URL and description
- `apic describe <id> --json`: the variables it needs and whether it is ready
- `apic run <id> --json`: send it; one JSON object with ok, request, response, captures, asserts
- `apic validate --json`: check every file before running anything
Exit codes: 0 ok, 1 assertion failed, 2 usage/parse/missing variable, 3 network.
Captured values persist, so run `login` once; a missing variable names the request that captures it.
```

That is the whole briefing. Watch it work by running the commands the
way an agent would, with `jq` picking the fields it cares about:

<!-- learn -->
```sh
apic list -C apic-demo --json | jq -r '.requests[] | "\(.id)\t\(.method) \(.url)"' | head -5
```

```
login	POST {{baseUrl}}/auth/login
whoami	GET {{baseUrl}}/me
basic-auth	GET {{baseUrl}}/basic-auth/{{user}}/{{password}}
oauth2-token	GET {{baseUrl}}/me
api-key	GET {{baseUrl}}/keyed
```

### 2. What the agent sees when something is missing

The moment the design shows itself is a request that is not ready yet.
Start from an empty session, so nothing has been captured:

<!-- learn -->
```sh
apic session clear -C apic-demo
apic describe get-todo -C apic-demo --json | jq -c '{ready, missing: [.variables[] | select(.missing) | {name, captured_by}]}'
```

```
{"ready":false,"missing":[{"name":"todoId","captured_by":"create-todo"},{"name":"token","captured_by":"login"},{"name":"todoTitle","captured_by":"create-todo"}]}
```

`describe` does not just say what is missing; it says which request
produces each value. An agent that runs the request anyway gets the same
answer as JSON on stdout, the human message on stderr, and exit code 2:

<!-- learn -->
```sh
apic run get-todo -C apic-demo --json 2>/dev/null | jq -c '{ok, errors}'
apic run get-todo -C apic-demo --json >/dev/null 2>&1 || echo "exit $?"
```

```
{"ok":false,"errors":["todos.http:42: missing variables\n  {{todoId}}: it is captured by request \"create-todo\"; run `apic run create-todo` first, or pass --var todoId=...\n  {{token}}: it is captured by request \"login\"; run `apic run login` first, or pass --var token=..."]}
exit 2
```

Every error apic can produce is written for that reader: what is wrong,
what to run, or what to pass. So the agent runs what it was told to, and
reads the captures out of the answer:

<!-- learn -->
```sh
apic run login -C apic-demo --json | jq -c '{ok, captures}'
apic run create-todo -C apic-demo --json --var title="From an agent" | jq -c '{ok, status: .response.status, captures}'
apic run get-todo -C apic-demo --json | jq -c '{ok, body: .response.body}'
```

```
{"ok":true,"captures":{"token":"mock-token"}}
{"ok":true,"status":201,"captures":{"todoId":"3","todoTitle":"From an agent"}}
{"ok":true,"body":{"id":"3","title":"From an agent","done":false}}
```

`response.body` is parsed JSON when the response is JSON, so the agent
chains values without string handling (the id is `3` on a fresh demo,
higher if you created todos in earlier lessons), and the captures persist, so it
logs in once per session rather than before every call.

### 3. `# @ref` takes even that step away

`whoami` declares `# @ref login` ([lesson 3](03-capture-session-flows.md)),
so the agent does not need to know about the dependency at all:

<!-- learn -->
```sh
apic session clear -C apic-demo
apic describe whoami -C apic-demo --json | jq -c '{ready, refs, token: [.variables[] | select(.name == "token") | {missing, captured_by, ref_runs}]}'
apic run whoami -C apic-demo --json | jq -c '{name: .request.name, ok, status: .response.status}'
```

```
{"ready":true,"refs":["login"],"token":[{"missing":true,"captured_by":"login","ref_runs":true}]}
{"name":"login","ok":true,"status":200}
{"name":"whoami","ok":true,"status":200}
```

`ready` is true although the token is missing, because `ref_runs` says
the dependency will handle it, and the run prints one object per request
that was sent, `login` first. An agent reading NDJSON sees exactly what
happened.

### 4. A session, condensed

Give an agent the demo project and the briefing from step 1, and ask it
to "mark the todo called Buy milk as done". A typical session, with the
tool calls shortened to what matters:

```console
$ apic list --json | jq -r '.requests[] | select(.file == "todos.http") | .id'
list-todos
…
update-todo

$ apic describe update-todo --json | jq '{ready, missing: [.variables[] | select(.missing) | .captured_by]}'
{"ready": false, "missing": ["create-todo", "login"]}

$ apic run list-todos --json | jq 'first(.response.body[] | select(.title == "Buy milk") | .id)'
"1"

$ apic run update-todo --json --var todoId=1 | jq '{ok, done: .response.body.done}'
{"ok": true, "done": true}
```

Notice what the agent did not have to do: read the `.http` files, guess a
URL, or ask you for a token. It found the request by name, learned what
it needed, got the id from a listing rather than creating a new todo, and
passed it with `--var` because the value it wanted was not the one
`create-todo` would have captured. Every step was a documented command
with a documented shape.

### 5. Over MCP

The Model Context Protocol is the other door: the same project as tools
an MCP client calls directly, with no shell in between. Register the
server once:

```sh
# Claude Code
claude mcp add api -- apic mcp --dir ./apic-demo --env local
```

```json
// Cursor (.cursor/mcp.json), Windsurf, and any other MCP client
{"mcpServers": {"api": {"command": "apic", "args": ["mcp", "--dir", "./apic-demo", "--env", "local"]}}}
```

The client then sees nine tools:

| Tool | What it does |
|---|---|
| `list_requests` | ids, methods, URL templates, descriptions, captures, asserts |
| `describe_request {name}` | variables and sources, the `ready` flag, the refs |
| `run_request {name, vars?}` | send one request; the same JSON as `apic run --json`, with `ran_first` for `# @ref` |
| `run_file {file, keep_going?}` | run a file as a flow |
| `run_features {paths?, tags?}` | run the Gherkin features; pass/fail counts and the failing steps |
| `list_environments` | environments and the variables in effect, secrets masked |
| `clear_session {all?}` | forget captured values and cookies |
| `validate_project {}` | check every `.http` file, with file, line, column and code, without sending anything |
| `curl_request {name, raw?}` | the equivalent curl command, masked unless `raw` |

and every `.http` file as a resource it can read, so it can look at a
request's definition before calling it, or write a new one in the same
style. The server speaks JSON-RPC over stdin and stdout, which means you
can talk to it from a shell as well. Three lines, the MCP handshake and
one call, list the tools:

<!-- learn -->
```sh
(printf '%s\n' \
  '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-03-26","capabilities":{},"clientInfo":{"name":"lesson","version":"0"}}}' \
  '{"jsonrpc":"2.0","method":"notifications/initialized"}' \
  '{"jsonrpc":"2.0","id":2,"method":"tools/list"}'; sleep 1) \
  | apic mcp --dir apic-demo 2>/dev/null | jq -r 'select(.id == 2) | .result.tools[].name'
```

```
clear_session
curl_request
describe_request
list_environments
list_requests
run_features
run_file
run_request
validate_project
```

### 6. What the agent can never read

The resources are the `.http` files and nothing else. The private env
file, `.env` and `.apic/session.json` sit in the same directory, and a
read of any of them is refused:

<!-- learn -->
```sh
(printf '%s\n' \
  '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-03-26","capabilities":{},"clientInfo":{"name":"lesson","version":"0"}}}' \
  '{"jsonrpc":"2.0","method":"notifications/initialized"}' \
  "{\"jsonrpc\":\"2.0\",\"id\":3,\"method\":\"resources/read\",\"params\":{\"uri\":\"file://$PWD/apic-demo/http-client.private.env.json\"}}"; sleep 1) \
  | apic mcp --dir apic-demo 2>/dev/null | jq -c 'select(.id == 3) | .error.message'
```

```
"Resource not found"
```

The same line holds everywhere an agent looks: `list_environments` masks
secrets as `apic env` does, `run_request` shows sensitive request headers
as `***`, and the tokens `# @auth oauth2` and `exec` cache in the session
are described (`$oauth2:… = token, expires in 1h0m0s`), never printed.
What the agent does get is real URLs, bodies and captured values, the
token that `login` captured included, because it needs them to chain
calls: step 2 printed `mock-token` for that reason. When the output is
going into a stored log rather than to an agent, add `--redact`, which
masks those too; a redacted run is for logs, not for chaining.

### 7. The agent as an author

Because the format is plain text, an agent can add requests, and
`validate --json` (or the `validate_project` tool) tells it where it went
wrong with a line and a column:

<!-- learn -->
```sh
mkdir -p agent-draft
printf '### Health\n# @name health\n# @assert stauts == 200\nGET http://localhost:8089/health\n' > agent-draft/health.http
apic validate -C agent-draft --json | jq -c '.diagnostics[] | {path, line, column, code, message}'
```

```
{"path":"health.http","line":3,"column":11,"code":"unknown-selector","message":"assert \"stauts == 200\": unknown selector \"stauts\""}
```

For features, `apic test --steps --json` lists every step the agent may
use, so it writes scenarios from a vocabulary that exists rather than one
it imagines:

<!-- learn -->
```sh
apic test --steps -C apic-demo --json | jq -r '.[].pattern' | head -4
```

```
the environment is "<name>"
the variable "<name>" is "<value>"
the variables: (table name | value)
I run "<request>"
```

and `run_features` over MCP (or `apic test --json` from the shell) hands
back the failing steps with their errors, which is what the agent needs
to fix the request or the scenario.

## Checkpoint

The MCP server answers a `tools/list` over stdio with the nine tools:

<!-- learn -->
```sh
(printf '%s\n' \
  '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-03-26","capabilities":{},"clientInfo":{"name":"lesson","version":"0"}}}' \
  '{"jsonrpc":"2.0","method":"notifications/initialized"}' \
  '{"jsonrpc":"2.0","id":2,"method":"tools/list"}'; sleep 1) \
  | apic mcp --dir apic-demo 2>/dev/null | jq 'select(.id == 2) | .result.tools | length'
```

```
9
```

## Exercise

Ask an agent, over MCP or the shell, to add pagination assertions to
`list-todos` in the demo project and make them pass. Give it only the
briefing from step 1 and the request's name.

??? example "Solution"
    A good agent reads `todos.http` (as a resource, or with `cat`), runs
    `list-open-todos` to see what the API returns, notices the
    `X-Total-Count` and `X-Page` headers, and adds:

    ```http
    # @assert header.x-total-count >= 1
    # @assert body.$.# <= 5
    ```

    then runs `apic validate --json` and `apic run list-todos --json` and
    reports the `asserts` array. The demo's `list-open-todos` already
    carries these, so the agent can also be judged on whether it looked
    next door first. If it wrote an assertion with a selector that does
    not exist, `validate` told it the column before the request was ever
    sent.

## Going further

- [Using apic from an AI agent](../agents.md): the full briefing, the
  `--json` shape, and the MCP tool reference
- [For agents](../testing.md#for-agents) in the testing guide:
  `run_features` and the steps vocabulary
- [`apic mcp`](../cli.md#apic-mcp) in the reference

??? note "Episode script"
    **Length.** 12 minutes; includes a real agent session, sped up.

    **Cold open (0:00).** An agent stuck: a chat transcript where it
    guesses a URL, gets a 401, and asks the user for a token. "The tool
    was never built for it. Here is one that was."

    **Talking points.**

    1. The three things an agent needs and the one-paragraph briefing.
    2. `describe` on a request that is not ready: `captured_by`; the run
       that fails with a JSON object and exit 2; the agent doing what the
       error said.
    3. `# @ref`: `ready: true` with `ref_runs`, two objects from one run.
    4. The recorded session with Claude Code, sped up: list, describe,
       list-todos for the id, update-todo with `--var`.
    5. `claude mcp add`, the nine tools, a request as a resource; the
       stdio handshake from a shell.
    6. The refused read of the private env file; masking versus
       `--redact`.
    7. The agent writing a request: `validate --json` with the column;
       `--steps --json` for features.
    8. Checkpoint, then the pagination exercise.

    **Shot list.** A terminal for steps 1 to 3 and 5 to 7; the agent
    session prerecorded in Claude Code, at 2x, with the tool calls
    expanded. No tape for this lesson.

    **Chapters.** `0:00 The stuck agent` · `0:40 The briefing` · `1:40
    Not ready, and what to do` · `3:30 # @ref` · `4:20 A real session`
    · `6:40 MCP` · `8:40 What it cannot read` · `9:40 The agent as
    author` · `11:00 Checkpoint and exercise`.

    **Description.** From the [episode template](_episode-template.md).
