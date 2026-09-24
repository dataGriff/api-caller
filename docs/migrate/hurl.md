# Migrating from Hurl

Hurl and apic are close cousins: one binary each, plain-text files,
captures and assertions. The move is mostly mechanical. A Hurl entry's
sections become `# @` directives above an `.http` request, and the files
become ones VS Code, JetBrains and Neovim can send with a click. Two
things change for the better on the way: captures persist between runs,
and a single named request can be run on its own.

## How the concepts map

| Hurl | apic |
|---|---|
| A `.hurl` file | a `.http` file; `apic run file.http` runs it top to bottom as a flow |
| An entry (request plus response section) | a `### ` block with `# @name`, so it can also run alone |
| `HTTP 200` | `# @assert status == 200` |
| `[Captures]` `token: jsonpath "$.token"` | `# @capture token = body.$.token` |
| `header "Location"`, `cookie "sid"` | `header.location`, `cookie.sid` |
| `jsonpath "$.items[0].id"` | `body.$.items[0].id` (filters, slices and `..` too; see [body paths](../format.md#body-paths)) |
| `status`, `duration`, `body` | `status`, `duration`, `body` |
| `[Asserts]` `jsonpath "$.id" == 42` | `# @assert body.$.id == 42` |
| `==`, `!=`, `>`, `>=`, `<`, `<=` | the same |
| `contains`, `startsWith`, `endsWith`, `matches` | the same names |
| `exists`, `not exists` | the same |
| `isString`, `isInteger`, `isBoolean`, `isCollection`, `isEmpty` | the same, with `isArray` or `isObject` for `isCollection` |
| `jsonpath "$.items" count == 3` | `# @assert body.$.items length == 3` |
| `[Options]` `retry: 10` `retry-interval: 2000` | `# @retry 10 2s` |
| `[Options]` `delay: 500` | `# @sleep 500ms` |
| `[Options]` `http2: true` | `GET https://… HTTP/2` on the request line |
| `[Options]` `insecure: true`, `cacert`, `cert`, `key` | `--insecure`, or `tls:` in `apic.yaml` and `--cacert`, `--cert`, `--key` |
| `[BasicAuth]` | `# @auth basic user pass` |
| `[FormParams]`, `[MultipartFormData]` | a urlencoded body, or a [multipart body](../format.md#multipart-uploads) |
| `{{name}}` with `--variable name=value` | `{{name}}` with `--var name=value` |
| `--variables-file vars.env` | an environment in `http-client.env.json`, or `.env` |
| `HURL_name` environment variables | `APIC_VAR_name` |
| `--test`, `--glob` | `apic run a.http b.http --keep-going` |
| `--report-junit`, `--report-html`, `--report-json` | `apic test --format junit` for features; `apic run --report report.html`; `apic run --json` |
| `--json` (one line per file) | `apic run --json` (one object per request) |

## A worked example

A Hurl file against the demo API:

```text
POST http://localhost:8089/auth/login
{"user": "alice", "password": "{{password}}"}
HTTP 200
[Captures]
token: jsonpath "$.access_token"

GET http://localhost:8089/todos?done=false
Authorization: Bearer {{token}}
HTTP 200
[Asserts]
header "X-Total-Count" exists
jsonpath "$" count >= 1
jsonpath "$[0].done" == false
jsonpath "$[0].id" isString
```

The same two requests in apic (with `apic demo` running in another
terminal). The host moves into the environment, each entry gets a name,
and each section line becomes a directive:

<!-- learn -->
```sh
mkdir -p from-hurl
cat > from-hurl/http-client.env.json <<'EOF'
{
  "local": { "baseUrl": "http://localhost:8089" }
}
EOF
cat > from-hurl/todos.http <<'EOF'
### Log in
# @name login
# @assert status == 200
# @capture token = body.$.access_token
POST {{baseUrl}}/auth/login
Content-Type: application/json

{"user": "alice", "password": "{{password}}"}

### The open todos
# @name open-todos
# @assert status == 200
# @assert header.x-total-count exists
# @assert body.$ length >= 1
# @assert body.$[0].done == false
# @assert body.$[0].id isString
GET {{baseUrl}}/todos?done=false
Authorization: Bearer {{token}}
EOF
apic run todos.http -C from-hurl --env local --var password=s3cret | tail -4
```

```text
✓ login       200  1 ms
✓ open-todos  200  0 ms

2 passed · 2 requests · 1 ms
```

Hurl adds `Content-Type: application/json` to a JSON body by itself; in a
`.http` file, write the header, since editors send exactly what the file
says. Then, because the token was captured into the session, the second
request runs on its own, which a Hurl file cannot do:

<!-- learn -->
```sh
apic run open-todos -C from-hurl --env local | tail -1
```

## What does not carry over

- **XPath, `sha256`, `md5` and the filter chain** (`jsonpath "$.x" split "," nth 1`).
  apic's selectors are JSONPath, headers, cookies, status, duration and the
  raw body; a check that needs a transformation goes in a shell script
  around `apic run --json`.
- **`[Options]` for curl internals** such as `compressed`, `path-as-is` or
  `unix-socket`. apic is not built on libcurl.
- **`--test` reports per file.** apic reports per request; `--keep-going`
  and the summary line give the same pass/fail picture.
- **Hurl's own format.** apic reads `.http` only; the two formats are
  close enough that a search-and-replace does most of it.

## Checklist

- [ ] One `.http` file per `.hurl` file, a `### ` block and `# @name` per entry
- [ ] `HTTP <code>` to `# @assert status == <code>`
- [ ] `[Captures]` to `# @capture`, `[Asserts]` to `# @assert`, `jsonpath "$…"` to `body.$…`
- [ ] Hosts to `{{baseUrl}}` in `http-client.env.json`; `--variables-file` to an environment
- [ ] `[Options] retry` to `# @retry`
- [ ] A `Content-Type` header on each JSON body
- [ ] `apic validate` is clean and `apic fmt --check` passes
- [ ] `hurl --test` in CI replaced with `apic run *.http --keep-going`

See also: the [assertion operators](../cheatsheet.md#assertion-operators),
[`# @retry`](../format.md#retries) and
[apic compared with Hurl](../comparison.md#against-hurl).
