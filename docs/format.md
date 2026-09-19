# The `.http` format apic understands

apic runs the common subset of the `.http` request format shared by VS Code
REST Client, JetBrains HTTP Client, kulala.nvim and httpyac. Everything apic
adds is a `# @directive` comment placed **before the request line**, which
those tools treat as a comment, so one file works everywhere.

```http
@baseUrl = https://api.example.com

### Log in and keep the token
# @name login
# @assert status == 200
# @capture token = body.$.access_token
POST {{baseUrl}}/auth/login
Content-Type: application/json

{"user": "{{user}}", "password": "{{password}}"}

### Fetch a user
# @name get-user
# @description Fetch a single user by id
# @assert status == 200
# @assert body.$.id == {{userId}}
GET {{baseUrl}}/users/{{userId}}
    ?expand=profile
Authorization: Bearer {{token}}
Accept: application/json
```

## Structure

| Element | Syntax |
|---|---|
| Request separator | `###` optionally followed by a title, used as the description |
| File variable | `@name = value` anywhere outside a body; last declaration wins; values may use `{{vars}}` |
| Comment | `# text` or `// text` |
| Directive | `# @key value` before the request line |
| Request line | `METHOD url [HTTP/1.1]`; a bare URL means `GET` |
| Query continuation | indented lines starting with `?` or `&` are appended to the URL |
| Headers | `Name: value` lines until the first blank line. Any RFC 7230 token character may appear in a name, except that a line starting with `#` is a comment |
| Body | everything after the blank line until the next `###` |
| Body from file | `< ./payload.json` (raw) or `<@ ./payload.json` (with `{{vars}}` substituted), relative to the `.http` file |
| Editor script blocks | `> {% … %}`, `< {% … %}` and `> ./handler.js` are skipped with a warning, not sent — apic has no scripting. `apic validate` lists them |

Files are found by walking the project root for `*.http` and `*.rest`,
skipping hidden directories, `node_modules` and `vendor`.

## Directives

| Directive | Meaning |
|---|---|
| `# @name get-user` | Name used on the command line and by MCP. Unnamed requests are addressed as `file.http#3`. |
| `# @description text` | One line shown by `list` and `describe`; defaults to the `###` title. |
| `# @capture name = selector` | After the response arrives, store the selected value as `name`. It is available to later requests in the same run and persisted in `.apic/session.json` for later invocations. |
| `# @assert selector op value` | Check the response. Failures set `ok: false` and exit code 1. |
| `# @auth type ...` | Attach credentials: `none`, `bearer`, `basic`, `aws`, `oauth2` or `exec`. See [auth.md](auth.md). |
| `# @step a user named {name} exists` | A Gherkin phrase that runs this request from a `.feature` file; `{name}` becomes a variable. Repeatable. See [testing.md](testing.md). |
| `# @no-redirect` | Do not follow 3xx redirects. |
| `# @no-session` | Do not persist this request's captures. |
| `# @timeout 10s` | Per-request timeout. |
| `# @note text` | Free text. Accepted and ignored, for REST Client compatibility. |
| `# @prompt name` | Accepted and ignored: apic never prompts. Pass the value with `--var name=...`, or put it in an env file. |

Unknown directives are reported as warnings by `apic validate` and ignored.

## Variables

`{{name}}` placeholders are resolved from these sources, first match wins:

1. `--var name=value` on the command line (or `vars` in an MCP call)
2. `APIC_VAR_name` in the shell environment
3. values captured earlier in this run
4. the session (`.apic/session.json`), per environment
5. `http-client.private.env.json` for the selected environment
6. `http-client.env.json` for the selected environment (`$shared` applies to all)
7. `.env` in the project root
8. `@name = value` in the `.http` file

`describe` shows which source each variable resolved from.

### Environment files

```json
{
  "$shared": { "userId": 42 },
  "dev":     { "baseUrl": "https://dev.example.com" },
  "prod":    { "baseUrl": "https://api.example.com" }
}
```

Select with `--env dev`, or set a default in `apic.yaml` (`env: dev`).
Keep secrets in `http-client.private.env.json` and gitignore it; apic masks
values from that file, from `.env` and from the session in `describe`, `env`
and MCP output.

### Built-ins

| Placeholder | Value |
|---|---|
| `{{$uuid}}` / `{{$guid}}` | random UUID v4 |
| `{{$timestamp}}` | Unix seconds |
| `{{$isoTimestamp}}` | RFC 3339 UTC |
| `{{$datetime rfc1123}}` / `{{$datetime iso8601}}` / `{{$datetime "2006-01-02"}}` | formatted time (Go layout for custom formats) |
| `{{$randomInt 1 100}}` | random integer in [min, max) |
| `{{$processEnv NAME}}` / `{{$env.NAME}}` | shell environment variable |
| `{{$dotenv NAME}}` | value from `.env` |

### Response references

Within a single run of several requests (a flow), a later request may read
an earlier named response directly, using REST Client syntax:

```
Authorization: Bearer {{login.response.body.$.access_token}}
X-Request-Id: {{login.response.headers.x-request-id}}
```

`@capture` is the same idea with a short name that also persists between runs.

## Selectors

Used by `@capture` and `@assert`:

| Selector | Value |
|---|---|
| `status` | status code, e.g. `200` |
| `statusText` | e.g. `OK` |
| `header.<name>` | first value of a response header, case-insensitive |
| `body` | raw body |
| `body.$` | whole body (must be JSON) |
| `body.$.<path>` | JSON path such as `body.$.items[0].id`, `body.$.items.#` (count), `body.$["key with dots"]` |
| `duration` | round-trip time in milliseconds |

## Assertion operators

`==`, `!=`, `<`, `<=`, `>`, `>=` (numeric when both sides parse as numbers),
`contains`, `startsWith`, `endsWith`, `matches` (Go regular expression),
`exists`, `not exists`. The right-hand side may contain `{{placeholders}}`
and may be quoted.

```
# @assert status < 300
# @assert body.$.items.# >= 1
# @assert header.content-type contains json
# @assert body.$.email matches ^[^@]+@example\.com$
# @assert body.$.error not exists
```

## Flows

`apic run file.http` runs every request in the file in order and stops at
the first failed assertion, failed capture or transport error (use
`--keep-going` to continue). Exit code is 1 if anything failed. `--json`
prints one JSON object per request (NDJSON).

## Project layout

```
api/
  apic.yaml                      optional: env, dir, timeout, auth.default, auth.allowExec, test.paths
  features/*.feature             Gherkin specs run by `apic test`
  http-client.env.json           public per-environment variables
  http-client.private.env.json   secrets (gitignored)
  .env                           optional KEY=value
  auth.http
  users.http
  .apic/session.json             captured values (created by apic, self-ignored)
```
