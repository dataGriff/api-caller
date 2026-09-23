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

## Bodies

| Body | Syntax |
|---|---|
| Inline | Everything after the blank line, `{{vars}}` substituted |
| From a file | `< ./payload.json` as it is, `<@ ./payload.json` with `{{vars}}` substituted |
| Multipart upload | `Content-Type: multipart/form-data; boundary=X`, parts between `--X` lines, `< ./report.pdf` as a part's content; see [format](format.md#multipart-uploads) |
| GraphQL | `GRAPHQL {{baseUrl}}/graphql` (or `X-REQUEST-TYPE: GraphQL`), the query as the body, variables as a JSON object after a blank line; sent as a JSON POST; see [format](format.md#graphql) |
| Save the response | `>> ./out.json` (create) or `>>! ./out.json` (overwrite) after the body; `apic run --output file` for one run; see [format](format.md#saving-a-response) |

## Commands

| Command | What it does |
|---|---|
| `apic run <target>...` | Send requests; several targets run in order as a flow; `--output file` saves one response body |
| `apic ui` | [Terminal UI](tui.md); `--demo` needs no project |
| `apic test [paths]` | Run [Gherkin features](testing.md) |
| `apic list [pattern]` | Every request, filtered by id, URL, file or description |
| `apic describe <id>` | Variables, sources, captures, asserts, readiness |
| `apic env` | Environments and the variables in effect |
| `apic session [clear]` | Captured values; `clear --all` for every environment |
| `apic curl <id>` | The equivalent curl command |
| `apic init [dir]` | Scaffold a project |
| `apic import <spec>` | `.http` files from an OpenAPI 3 document or a Postman collection (`--postman-env` for its environments) |
| `apic import --curl '<cmd>' --into f.http` | One request block from a curl command |
| `apic validate` | Parse everything and report problems (CI); `--format github\|sarif` |
| `apic fmt [--check\|--diff]` | Canonical formatting for `.http` files; `-` filters stdin |
| `apic mcp` | Serve the project to agents over MCP |
| `apic demo` | Scaffold and serve the bundled fake API |

**Targets:** `get-user` (by name) · `users.http` (whole file as a flow) ·
`users.http#get-user` · `users.http#3` (third request).

**Global flags:** `-C/--dir`, `-e/--env`, `--var k=v`, `--json`,
`--no-color`, `--timeout`, `--no-session`, `--insecure`, `--cacert`,
`--cert`, `--key`, `--redact`, `--cookies`.

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
| `# @auth type ...` | `none`, `bearer`, `basic`, `apikey`, `digest`, `aws`, `oauth2`, `exec`; see [auth](auth.md) |
| `# @step a user named {name} exists` | Gherkin phrase that runs this request |
| `# @ref login` | Run `login` first when a variable is missing |
| `# @forceRef login` | Run `login` first every time |
| `# @no-redirect` | Do not follow 3xx |
| `# @no-session` | Do not persist this request's captures |
| `# @no-cookies` | Send and keep no cookies for this request |
| `# @timeout 10s` | Per-request timeout |
| `# @retry 10 2s` | Re-send until the assertions pass, up to 10 times, 2s apart |
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
| `{{$timestamp}}` / `{{$timestamp -1 d}}` | Unix seconds, optional offset (`s m h d w M Q y ms`) |
| `{{$isoTimestamp}}` | RFC 3339 UTC |
| `{{$datetime rfc1123\|iso8601\|"2006-01-02" [1 h]}}` | formatted UTC time, optional offset |
| `{{$localDatetime [format] [offset]}}` | the same in the local zone |
| `{{$randomInt 1 100}}` / `{{$random.integer(1, 100)}}` | random integer in [min, max) |
| `{{$random.float(0, 1)}}`, `$random.alphabetic(n)`, `alphanumeric(n)`, `hexadecimal(n)`, `email`, `uuid` | JetBrains' random family |
| `{{$processEnv NAME}}` / `{{$env.NAME}}` | shell environment variable |
| `{{$dotenv NAME}}` | value from `.env` |
| `{{$projectRoot}}` | absolute project root |
| `{{login.response.body.$.token}}` | an earlier response in the same flow |

## Selectors

| Selector | Value |
|---|---|
| `status` | status code |
| `statusText` | e.g. `OK` |
| `header.<name>` | first value of a response header; `.#` counts its values, `[1]` picks one |
| `cookie.<name>` | value of a cookie the response set |
| `body` | raw body |
| `body.$` | whole JSON body |
| `body.$.<path>` | `body.$.items[0].id`, `[-1]`, `[1:3]`, `[*]`, `body.$..id`, `body.$.items[?(@.done == true)].id`, `.#` or `.length` (count), `body.$["key.with.dots"]`; see [format](format.md#body-paths) |
| `duration` | round-trip time in milliseconds |

## Assertion operators

`==` `!=` `<` `<=` `>` `>=` (numeric when both sides are numbers) ·
`contains` · `startsWith` · `endsWith` · `matches` (Go regexp) · `exists` ·
`not exists` · `isString` `isNumber` `isInteger` `isBoolean` `isArray`
`isObject` `isNull` `isEmpty` (and `not …`) · `length <op> <n>` ·
`matchesSchema <file.json>`

```
# @assert status < 300
# @assert body.$.items.# >= 1
# @assert header.content-type contains json
# @assert body.$.email matches ^[^@]+@example\.com$
# @assert body.$.error not exists
# @assert body.$.id isInteger
# @assert body.$.items length == 3
# @assert body.$ matchesSchema ./schemas/user.json
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
And the response cookie "sid" exists
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
  apic.yaml                      env, dir, timeout, retry, cookies, tls, auth.default, auth.allowExec, test.paths
  features/*.feature             Gherkin specs run by `apic test`
  http-client.env.json           public per-environment variables
  http-client.private.env.json   secrets (gitignored)
  .env                           optional KEY=value
  auth.http
  users.http
  .apic/session.json             captured values (created by apic, self-ignored)
  .apic/cookies.json             the cookie jar, when cookies: true
```

## Terminal UI keys

<kbd>enter</kbd> run · <kbd>f</kbd> run the file · <kbd>a</kbd> run all ·
<kbd>/</kbd> filter · <kbd>1</kbd>-<kbd>4</kbd> tabs · <kbd>H</kbd> headers ·
<kbd>c</kbd> curl · <kbd>e</kbd> environment · <kbd>r</kbd> reload ·
<kbd>o</kbd> edit · <kbd>x</kbd> clear session · <kbd>?</kbd> help ·
<kbd>q</kbd> quit. Full list in [the TUI guide](tui.md#keys).
