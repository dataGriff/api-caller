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
| Multipart body | `Content-Type: multipart/form-data; boundary=X` with the parts written between `--X` lines; a part whose content is `< ./file` sends that file's bytes. See [Multipart uploads](#multipart-uploads) |
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
| `# @ref login` | Run `login` first when this request is missing a variable (once per invocation). Repeatable. See [Dependencies](#dependencies). |
| `# @forceRef login` | Run `login` first every time this request runs. Repeatable. |
| `# @no-redirect` | Do not follow 3xx redirects. |
| `# @no-session` | Do not persist this request's captures. |
| `# @no-cookies` | Send no cookies with this request and keep none it sets, when the [cookie jar](#cookies) is on. |
| `# @timeout 10s` | Per-request timeout. |
| `# @retry 10 2s` | Re-send until every assertion passes, up to 10 times, 2s apart (default 1s). See [Retries](#retries). |
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
| `cookie.<name>` | value of a cookie the response set (`Set-Cookie`), whether or not the jar is on |
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

## Dependencies

A request that needs a value another request captures can say so, and apic
runs that request first when the value is missing:

```http
### Log in and keep the token
# @name login
# @capture token = body.$.access_token
POST {{baseUrl}}/auth/login

### Who am I
# @name whoami
# @ref login
GET {{baseUrl}}/me
Authorization: Bearer {{token}}
```

`apic run whoami` on a fresh session runs `login`, then `whoami`; with the
token already in the session it runs only `whoami`. A `# @ref` runs at most
once per invocation, so a flow that needs `login` twice logs in once, and
the target's own `# @ref` lines apply too. `# @forceRef login` runs `login`
first every time, for a token that must be fresh. The target is any run
target (`login`, `auth.http#login`) that names exactly one request; a target
that does not, or a chain that leads back to itself, is an error `apic
validate` reports as `bad-ref` or `ref-cycle`.

A dependency that fails (an assertion, a capture, the network) stops the
request that depends on it: the run reports the dependency's result, then
the request as failed without sending it. The output shows what ran first
(`↳ ran login first (# @ref)` in the terminal, `ran_first` in `--json`,
see [cli.md](cli.md#apic-run)).

## Retries

A request whose assertions describe a state the API will reach, not the one
it is in, can wait for it:

```http
### Poll until the job is done
# @name wait-for-job
# @retry 10 2s
# @assert status == 200
# @assert body.$.state == done
GET {{baseUrl}}/jobs/{{jobId}}
```

`# @retry <attempts> [interval]` sends the request again until every
assertion passes or the attempts are spent, waiting `interval` between
attempts (a Go duration such as `500ms` or `2s`; default `1s`). A transport
error counts as a failed attempt too. Each failed attempt prints a line as
it happens (`attempt 1/10 · body.$.state == done: got "running"`), the
report of the attempt that counted says how many it took, and `--json`
carries `attempts`. Captures and the session are written from that final
attempt only, and every attempt gets the full `# @timeout`.

`retry:` in `apic.yaml` sets a default for requests without their own
`# @retry`, `--retry "<attempts> [interval]"` overrides that for one run,
and `--no-retry` sends everything once. The order is directive, flag, file.
`apic validate` reports a policy it cannot read as `bad-retry`.

## Cookies

apic sends no cookies unless a jar is switched on, with `cookies: true` in
`apic.yaml` or `--cookies` on the command line. With it on, a cookie a
response sets is sent with later requests to the same site, in the same
run and in later ones: the jar is stored per environment in
`.apic/cookies.json`, beside the session, so a login that answers with a
session cookie works like one that answers with a token.

```http
### Log in with a form
# @name login
# @assert status == 204
# @assert cookie.sid exists
POST {{baseUrl}}/login
Content-Type: application/x-www-form-urlencoded

user={{user}}&password={{password}}

### The cookie goes out by itself
# @name me
# @assert status == 200
GET {{baseUrl}}/me
```

Which cookies go where follows Go's `net/http/cookiejar`: domain, path,
`Secure` and expiry are honoured; there is no public-suffix list, so a
cookie set for `example.com` is sent to every host under it. A cookie
without an expiry is kept until `apic session clear`, like a captured
value. `# @no-cookies` exempts one request, `--no-session` keeps the jar in
memory for one command, and `apic test` gives every scenario its own empty
jar unless `--use-session` shares the stored one. `Cookie` and `Set-Cookie`
headers are masked in output whether or not `--redact` is set.

## Multipart uploads

A `multipart/form-data` body is written the way REST Client and JetBrains
write it, with the boundary declared in the header and each part between
delimiter lines:

```http
### Upload a report
# @name upload-report
# @assert status == 201
POST {{baseUrl}}/upload
Content-Type: multipart/form-data; boundary=WebAppBoundary

--WebAppBoundary
Content-Disposition: form-data; name="title"

Quarterly report for {{user}}
--WebAppBoundary
Content-Disposition: form-data; name="file"; filename="report.pdf"
Content-Type: application/pdf

< ./report.pdf
--WebAppBoundary--
```

apic reads the parts and assembles the body itself, so a part whose only
content is `< ./report.pdf` sends the file's bytes (binary-safe, with
`Content-Length` set), not the reference as text. `<@ ./file` substitutes
`{{variables}}` inside the file first, and text parts and part headers are
templates like the rest of the request. Paths are relative to the `.http`
file and confined to the project root, like a whole-body `< file`.

`apic validate` reports a body under a `multipart/form-data` content type
that has no boundary, or whose parts are not laid out between the
delimiters (`bad-multipart`), and a part file that does not exist
(`missing-body-file`). In output the body is shown as
`<multipart: 2 parts, 1 file>` rather than its bytes; `apic curl` turns the
parts into `--form-string` and `-F name=@file` options.

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
