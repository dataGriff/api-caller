# Cheat sheet

Everything apic understands, on one page. Each section links to the full
explanation.

## A file

```http
@baseUrl = https://api.example.com          # file variable, last one wins

### Log in and keep the token                # separator; the title is the description
# @name login                                # id used on the command line and by MCP
# @assert status == 200
# @capture token = body.$.access_token
POST {{baseUrl}}/auth/login
Content-Type: application/json

{"user": "{{user}}", "password": "{{password}}"}
```

## Commands

| Command | What it does |
|---|---|
| `apic run <target>...` | Send requests; several targets run in order as a flow |
| `apic ui` | [Terminal UI](tui.md); `--demo` needs no project |
| `apic test [paths]` | Run [Gherkin features](testing.md) |
| `apic list [pattern]` | Every request, filtered by id, URL, file or description |
| `apic describe <id>` | Variables, sources, captures, asserts, readiness |
| `apic env` | Environments and the variables in effect |
| `apic session [clear]` | Captured values; `clear --all` for every environment |
| `apic curl <id>` | The equivalent curl command |
| `apic init [dir]` | Scaffold a project |
| `apic import <spec>` | `.http` files from an OpenAPI 3 document |
| `apic validate` | Parse everything and report problems (CI) |
| `apic mcp` | Serve the project to agents over MCP |
| `apic demo` | Scaffold and serve the bundled fake API |

**Targets:** `get-user` (by name) · `users.http` (whole file as a flow) ·
`users.http#get-user` · `users.http#3` (third request).

**Global flags:** `-C/--dir`, `-e/--env`, `--var k=v`, `--json`,
`--no-color`, `--timeout`, `--no-session`, `--insecure`, `--redact`.

**Exit codes:** `0` ok · `1` assertion or capture failed · `2` usage, parse
error, unknown request or missing variable · `3` network error.

## Directives

Written as comments before the request line, so editors ignore them.

| Directive | Meaning |
|---|---|
| `# @name get-user` | Id for the command line, MCP and features |
| `# @description text` | One line shown by `list` and `describe` |
| `# @capture name = selector` | Store a value from the response for later runs |
| `# @assert selector op value` | Check the response; failures exit 1 |
| `# @auth type ...` | `none`, `bearer`, `basic`, `aws`, `oauth2`, `exec`; see [auth](auth.md) |
| `# @step a user named {name} exists` | Gherkin phrase that runs this request |
| `# @no-redirect` | Do not follow 3xx |
| `# @no-session` | Do not persist this request's captures |
| `# @timeout 10s` | Per-request timeout |
| `# @note text` | Free text, ignored (REST Client compatibility) |
| `# @prompt name` | Ignored; pass the value with `--var name=...` instead |

Unknown directives are warnings from `apic validate`, not errors.

## Variable precedence

First match wins:

1. `--var name=value` (or `vars` in an MCP call)
2. `APIC_VAR_name` in the shell environment
3. values captured earlier in this run
4. the session, `.apic/session.json`, per environment
5. `http-client.private.env.json` for the environment
6. `http-client.env.json` for the environment (`$shared` applies to all)
7. `.env` in the project root
8. `@name = value` in the `.http` file

`apic describe <id>` prints the source each variable actually resolved from.

## Built-in placeholders

| Placeholder | Value |
|---|---|
| `{{$uuid}}` / `{{$guid}}` | random UUID v4 |
| `{{$timestamp}}` | Unix seconds |
| `{{$isoTimestamp}}` | RFC 3339 UTC |
| `{{$datetime rfc1123\|iso8601\|"2006-01-02"}}` | formatted time |
| `{{$randomInt 1 100}}` | random integer in [min, max) |
| `{{$processEnv NAME}}` / `{{$env.NAME}}` | shell environment variable |
| `{{$dotenv NAME}}` | value from `.env` |
| `{{login.response.body.$.token}}` | an earlier response in the same flow |

## Selectors

| Selector | Value |
|---|---|
| `status` | status code |
| `statusText` | e.g. `OK` |
| `header.<name>` | first value of a response header |
| `body` | raw body |
| `body.$` | whole JSON body |
| `body.$.<path>` | `body.$.items[0].id`, `body.$.items.#` (count), `body.$["key.with.dots"]` |
| `duration` | round-trip time in milliseconds |

## Assertion operators

`==` `!=` `<` `<=` `>` `>=` (numeric when both sides are numbers) ·
`contains` · `startsWith` · `endsWith` · `matches` (Go regexp) · `exists` ·
`not exists`

```
# @assert status < 300
# @assert body.$.items.# >= 1
# @assert header.content-type contains json
# @assert body.$.email matches ^[^@]+@example\.com$
# @assert body.$.error not exists
```

## Gherkin steps

Built-in vocabulary, usable in any `.feature` file:

```gherkin
Given the environment is "staging"
And the variable "userId" is "42"
And the variables:
  | userId | 42 |
When I run "get-user"
And I run "get-user" with:
  | userId | 7 |
And I run the file "users.http"
Then the response status is 200
And the response status is not 500
And the response is successful          # or a client error, a server error
And the response body "$.name" is "alice"
And the response header "content-type" contains "json"
And the response body "$.id" exists
And the response body is:
  """
  {"id": 42}
  """
And the response time is under 500 ms
When I capture the response body "$.id" as "userId"
```

`# @step` phrases on requests add your own wording. `apic test --steps` lists
everything available in the current project.

## Project layout

```
api/
  apic.yaml                      env, dir, timeout, auth.default, auth.allowExec, test.paths
  features/*.feature             Gherkin specs run by `apic test`
  http-client.env.json           public per-environment variables
  http-client.private.env.json   secrets (gitignored)
  .env                           optional KEY=value
  auth.http
  users.http
  .apic/session.json             captured values (created by apic, self-ignored)
```

## Terminal UI keys

<kbd>enter</kbd> run · <kbd>f</kbd> run the file · <kbd>a</kbd> run all ·
<kbd>/</kbd> filter · <kbd>1</kbd>-<kbd>4</kbd> tabs · <kbd>H</kbd> headers ·
<kbd>c</kbd> curl · <kbd>e</kbd> environment · <kbd>r</kbd> reload ·
<kbd>o</kbd> edit · <kbd>x</kbd> clear session · <kbd>?</kbd> help ·
<kbd>q</kbd> quit. Full list in [the TUI guide](tui.md#keys).
