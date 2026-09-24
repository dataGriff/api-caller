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
| GraphQL request | `GRAPHQL {{baseUrl}}/graphql` (or a `X-REQUEST-TYPE: GraphQL` header): the body is the query, a JSON object after a blank line is the variables; see [GraphQL](#graphql) |
| Directive | `# @key value` before the request line |
| Request line | `METHOD url [version]`, the version `HTTP/1.1` or `HTTP/2`; a bare URL means `GET`. See [HTTP version](#http-version) |
| Query continuation | indented lines starting with `?` or `&` are appended to the URL |
| Headers | `Name: value` lines until the first blank line. Any RFC 7230 token character may appear in a name, except that a line starting with `#` is a comment |
| Body | everything after the blank line until the next `###` |
| Body from file | `< ./payload.json` (raw) or `<@ ./payload.json` (with `{{vars}}` substituted), relative to the `.http` file |
| Multipart body | `Content-Type: multipart/form-data; boundary=X` with the parts written between `--X` lines; a part whose content is `< ./file` sends that file's bytes. See [Multipart uploads](#multipart-uploads) |
| Save the response | `>> ./out.json` after the body writes the response body there (fails if the file exists); `>>! ./out.json` overwrites. Relative to the `.http` file, inside the project. See [Saving a response](#saving-a-response) |
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
| `# @auth type ...` | Attach credentials: `none`, `bearer`, `basic`, `apikey`, `digest`, `aws`, `oauth2` or `exec`. See [auth.md](auth.md). |
| `# @step a user named {name} exists` | A Gherkin phrase that runs this request from a `.feature` file; `{name}` becomes a variable. Repeatable. See [testing.md](testing.md). |
| `# @ref login` | Run `login` first when this request is missing a variable (once per invocation). Repeatable. See [Dependencies](#dependencies). |
| `# @forceRef login` | Run `login` first every time this request runs. Repeatable. |
| `# @no-redirect` | Do not follow 3xx redirects. |
| `# @no-session` | Do not persist this request's captures. |
| `# @no-cookies` | Send no cookies with this request and keep none it sets, when the [cookie jar](#cookies) is on. |
| `# @timeout 10s` | Per-request timeout. |
| `# @retry 10 2s` | Re-send until every assertion passes, up to 10 times, 2s apart (default 1s). See [Retries](#retries). |
| `# @sleep 2s` | Wait this long before sending, after any `# @ref` ran. See [Pauses and disabled requests](#pauses-and-disabled-requests). |
| `# @disabled` | Keep the request out of flows: running its file skips it. Asking for it by name still sends it. |
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
and MCP output. A JetBrains `SSLConfiguration` entry in either file is not
a variable: it configures a client certificate, see
[auth.md](auth.md#tls-and-client-certificates). Neither is a `Security`
block, whose `Auth` configurations requests use as
`{{$auth.token("name")}}`; see [JetBrains projects](auth.md#jetbrains-projects).

### Built-ins

| Placeholder | Value |
|---|---|
| `{{$uuid}}` / `{{$guid}}` | random UUID v4 |
| `{{$timestamp}}` / `{{$timestamp -1 d}}` | Unix seconds, with an optional offset |
| `{{$isoTimestamp}}` | RFC 3339 UTC |
| `{{$datetime rfc1123}}` / `{{$datetime iso8601}}` / `{{$datetime "2006-01-02"}}` / `{{$datetime iso8601 1 h}}` | formatted UTC time (Go layout for custom formats), with an optional offset |
| `{{$localDatetime}}` / `{{$localDatetime rfc1123 -1 d}}` | the same in the machine's own zone; the format is optional |
| `{{$randomInt 1 100}}` | random integer in [min, max) |
| `{{$random.integer(1, 100)}}` / `{{$random.float(0, 1)}}` | JetBrains' random numbers: an integer in [min, max), a float with three decimals; `(0, 1000)` without arguments |
| `{{$random.alphabetic(10)}}` / `{{$random.alphanumeric(10)}}` / `{{$random.hexadecimal(10)}}` | random text of that length (10 without arguments) |
| `{{$random.email}}` / `{{$random.uuid}}` | `<8 letters>@example.com`, a UUID v4 |
| `{{$processEnv NAME}}` / `{{$env.NAME}}` | shell environment variable |
| `{{$dotenv NAME}}` | value from `.env` |
| `{{$projectRoot}}` | the project root, absolute, for `< {{$projectRoot}}/fixtures/x.json` |
| `{{$auth.token("name")}}`, `{{$auth.idToken("name")}}` | the access or ID token of a JetBrains `Security.Auth` configuration in the env files; see [JetBrains projects](auth.md#jetbrains-projects) |

An offset is `<n> <unit>`, as REST Client writes it: `-1 d`, `2 h`,
`30 m`, `-10 s`, `500 ms`, `1 w`, `1 M` (months), `1 Q` (quarters),
`1 y`. Months, quarters and years move by the calendar, so the 31st plus
a month rolls forward the way Go's `AddDate` does. The random values are
sample data for payloads, not credentials; `$exampleServer` is a JetBrains
concept apic does not add.

### Response references

Within a single run of several requests (a flow), a later request may read
an earlier named response directly, using REST Client syntax:

```
Authorization: Bearer {{login.response.body.$.access_token}}
X-Request-Id: {{login.response.headers.x-request-id}}
```

`@capture` is the same idea with a short name that also persists between runs.

## Saving a response

A `>>` line after the body, as REST Client and JetBrains write it, saves
the response body to a file:

```
### Export
# @name export-csv
GET {{baseUrl}}/reports/daily.csv

>>! ./fixtures/daily.csv
```

`>> path` creates the file and fails (the request is not OK) when it
already exists; `>>! path` overwrites. The path is relative to the
`.http` file and must stay inside the project (`apic validate` reports
`bad-save-path` otherwise); directories are created. The bytes are
written as they came, so a binary download stays intact, and the file
is created `0600` when a secret went into the request (a private
variable, a capture, a credential), `0644` otherwise. The run's output
says `↳ saved to fixtures/daily.csv`, `--json` carries `saved_to`, and
a body that is not text is summarised (`binary body · 12 KB ·
image/png`) rather than printed. `apic run --output <file>` does the
same for one request without editing the file.

## GraphQL

Both editors have a GraphQL shape, and apic runs both. JetBrains writes
the method as `GRAPHQL`; REST Client marks a `POST` with an
`X-REQUEST-TYPE: GraphQL` header. Either way the body is the query, and
a JSON object after a blank line is the variables:

```
### Todos by state
# @name todos-by-state
# @assert body.$.data.todos.# >= 1
GRAPHQL {{baseUrl}}/graphql
Authorization: Bearer {{token}}

query Todos($done: Boolean) {
  todos(done: $done) { id title }
}

{"done": {{done}}}
```

apic sends it as a `POST` with `Content-Type: application/json` (unless
the request sets its own) and the body `{"query": "...", "variables":
{...}}`, which is what a GraphQL server reads. Placeholders resolve in
both halves, the `X-REQUEST-TYPE` header never goes on the wire, and the
variables are optional. `apic list` shows the method as written;
`describe`, `curl` and `--json` show the POST and the JSON body actually
sent. Selectors are the plain ones: `body.$.data.todos.#`. A query can
come from a file too (`<@ ./todos.graphql`); a request without a query,
or whose variables are not a JSON object, is a `bad-graphql` error in
`apic validate`.

## Selectors

Used by `@capture` and `@assert`:

| Selector | Value |
|---|---|
| `status` | status code, e.g. `200` |
| `statusText` | e.g. `OK` |
| `header.<name>` | first value of a response header, case-insensitive |
| `header.<name>.#` / `header.<name>[1]` | how many values a header has, and the n-th (from 0; `[-1]` is the last), for `Set-Cookie`, `Link` and the like |
| `cookie.<name>` | value of a cookie the response set (`Set-Cookie`), whether or not the jar is on |
| `body` | raw body |
| `body.$` | whole body (must be JSON) |
| `body.$.<path>` | a JSONPath into the body; see [Body paths](#body-paths) |
| `duration` | round-trip time in milliseconds |

### Body paths

The path after `body.$` is JSONPath, as Hurl, Postman and Bruno write it:

| Path | Selects |
|---|---|
| `body.$.items[0].id`, `body.$["key with dots"]` | a key or an index; a bare key runs to the next `.` or `[` |
| `body.$.items[-1]` | an index counted from the end |
| `body.$.items[1:3]`, `[:2]`, `[-2:]` | a slice, end exclusive |
| `body.$.items[*].id`, `body.$.meta.*` | every element or value |
| `body.$..id` | every `id` at any depth (recursive descent) |
| `body.$.items[?(@.done == true)].id` | the elements a filter keeps: `==`, `!=`, `<`, `<=`, `>`, `>=` against a number, a quoted string, `true`, `false` or `null`; `=~ /^a/` (add `i` after the closing slash to ignore case); `@.owner` alone for presence and `!@.owner` for absence; `&&` and `||` between terms; the `@` path can hold brackets of its own, as in `@.tags[0] == "red"` |
| `body.$.items.#`, `body.$.items.length` | at the end, how many: an array's elements, an object's keys, a string's characters, or the matches of a wildcard, slice, filter or `..`; a number, boolean or null has no count |

A path through a wildcard, slice, filter or `..` selects every match, and
its value is a JSON array of them (`["a","c"]`); no matches means nothing
is there, so `exists` fails and `.length` is `0`. A single value is the
text itself for a string and JSON for anything else, exactly as the
server sent it. An object with a real `length` key keeps it:
`body.$.length` reads that key, and inside a filter `@.length` on an
object is only ever that key, so `[?(@.length)]` asks whether it is
there.

A filter's `!=` also keeps the elements that lack the key, as RFC 9535
says: `[?(@.status != "done")]` keeps an element with no `status`. Every
other comparison needs the value to be there.

Paths written for gjson, which apic used before filters existed, still
select what they did: a numeric key on an array is an index
(`body.$.items.0.id`), and `#` in the middle of a path maps over an array
(`body.$.items.#.id`), so a count after it is each element's own
(`body.$.items.#.tags.#` is `[2,0]`). gjson's `#(...)` queries are not
supported; write them as filters, `body.$.items[?(@.id == 2)].name`.
Parentheses inside a filter, unions (`[0,1]`) and slice steps
(`[0:9:2]`) are not supported either; `apic validate` names the part it
cannot read.

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
# @assert body.$.items[?(@.done == true)].length == 2
# @assert header.set-cookie.# >= 1
```

### The shape of a response

These say what a value is rather than what it equals:

| Assertion | Passes when |
|---|---|
| `body.$.id isInteger` | the value is a whole number; also `isNumber`, `isString`, `isBoolean`, `isArray`, `isObject`, `isNull` |
| `body.$.id not isString` | the `not` form of any of them |
| `body.$.items isEmpty` / `not isEmpty` | an empty (or non-empty) string, array or object |
| `body.$.items length == 3` | its length compares: a string's characters, an array's elements, an object's keys; any of `==`, `!=`, `<`, `<=`, `>`, `>=` |
| `body.$ matchesSchema ./schemas/user.json` | it validates against a JSON Schema |

Types are JSON types: a status, a duration and a count are numbers;
headers, cookies, `statusText` and the raw `body` are strings. A failed
type check reports what the value is (`actual: number`).

`matchesSchema` reads a JSON Schema file (draft 2020-12 or draft-07,
`$ref` within the file) relative to the `.http` file and inside the
project; a failure reports the path in the document and the rule it
broke. `apic validate` reports a schema file that does not exist as
`missing-schema-file`.

```
### Get a user, checked for shape
# @name get-user
# @assert status == 200
# @assert body.$ matchesSchema ./schemas/user.json
# @assert body.$.id isInteger
# @assert body.$.roles not isEmpty
GET {{baseUrl}}/users/{{userId}}
```

## Flows

`apic run file.http` runs every request in the file in order and stops at
the first failed assertion, failed capture or transport error (use
`--keep-going` to continue). Exit code is 1 if anything failed. `--json`
prints one JSON object per request (NDJSON).

## HTTP version

With no version on the request line, apic does what browsers do: HTTP/2
when the server offers it over TLS, HTTP/1.1 otherwise. A version pins it:

```http
### A gateway whose HTTP/2 is broken
GET https://legacy.example.com/report HTTP/1.1

### Prove the API speaks HTTP/2
GET https://api.example.com/health HTTP/2
```

- `HTTP/1.1` (or `HTTP/1.0`, sent as 1.1) never uses HTTP/2, even when
  the server offers it.
- `HTTP/2` requires it: over `https://` the request fails with exit code 3
  when the server does not offer HTTP/2, and over plain `http://` it is a
  usage error, because apic does not send cleartext HTTP/2 (h2c).
- `HTTP/3`, or anything else, is reported by `apic validate` as
  `bad-http-version` and refused.

`run -v` puts the protocol the response came over in front of the status
(`HTTP/2.0 200 OK`), `--json` has it as `response.proto` and the pinned
version as `request.http_version`, and `apic curl` adds `--http1.1` or
`--http2`. `apic import curl` reads those two flags back onto the request
line.

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

## Pauses and disabled requests

```http
### Nightly export, slow and rate limited
# @name export
# @disabled
# @sleep 2s
POST {{baseUrl}}/exports
```

`# @sleep <duration>` waits that long (a Go duration such as `500ms` or
`2s`) before the request is sent: after any `# @ref` ran and before the
first attempt, once however many attempts a `# @retry` makes. Cancelling
the run (Ctrl-C, `esc` in the UI) cancels the wait. `describe` shows it,
and `apic validate` reports a value it cannot read as `bad-sleep`.

`# @disabled` keeps a request in the file without it running in the flow.
Running the file skips it: `apic run export.http`, `f` and `a` in the UI,
`When I run the file` in a feature and MCP's `run_file`. The flow's output
says `skipped (disabled)` and its summary counts it (`2 passed, 1
skipped`); under `--json` the request still prints its object, with
`"skipped": "disabled"`, `ok: true` and no `response`. Asking for the
request itself sends it as usual: `apic run export`,
`apic run export.http#export`, `enter` in the UI, `When I run "export"`,
MCP's `run_request`, and a `# @ref` to it. `list` marks it
(`"disabled": true`, dimmed in the UI).

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

## Canonical form

`apic fmt` rewrites a file the way this page writes them: one blank line
between blocks, directives in a fixed order (`name`, `description`,
`disabled`, `step`, `auth`, `ref`, `forceRef`, `sleep`, `retry`, `timeout`, `no-redirect`,
`no-session`, `no-cookies`, `assert`, `capture`, then the rest as
written), header names in canonical case, query continuations indented,
JSON bodies pretty-printed when they hold no placeholders. Every other
body is kept byte for byte; comments stay with the directive below them,
and unknown directives, file bodies and editor script blocks are kept as
they are. Formatting twice changes nothing. See
[cli.md](cli.md#apic-fmt).

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
