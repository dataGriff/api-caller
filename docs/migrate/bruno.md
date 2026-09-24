# Migrating from Bruno

Bruno and apic agree on the important thing: requests are plain files in
git. Moving across is a translation of one text format into another, block
by block. There is no importer yet, so this page is the mapping and a
worked example; a collection of a few dozen requests takes an afternoon.

## How the concepts map

| Bruno | apic |
|---|---|
| Collection folder with `bruno.json` | a project: a directory with `.http` files and `apic.yaml` |
| A `.bru` file (one request) | a `### ` block with `# @name` in a `.http` file; put a folder's requests in one file |
| `meta { name, seq }` | `# @name`; order in the file is the run order |
| `get { url }`, `post { url, body: json }` | the request line: `POST {{baseUrl}}/todos` |
| `headers { … }` | header lines under the request line |
| `body:json { … }`, `body:form-urlencoded`, `body:multipart-form` | the body after a blank line; [multipart](../format.md#multipart-uploads) for file parts |
| `auth:bearer`, `auth:basic`, `auth:awsv4`, `auth:digest`, `auth:oauth2` | `# @auth bearer …`, `basic`, `aws`, `digest`, `oauth2`; see [authentication](../auth.md) |
| Collection-level auth in `collection.bru` | `auth.default` in `apic.yaml` |
| `environments/local.bru` `vars { … }` | the `local` environment in `http-client.env.json` |
| `vars:secret [ … ]` | `http-client.private.env.json`, kept out of git |
| `.env` and `{{process.env.NAME}}` | `.env` in the project root, read as variables, or `{{$dotenv NAME}}` |
| `vars:pre-request { x: 1 }` | a file variable `@x = 1`, or `--var x=1` |
| `vars:post-response { token: res.body.access_token }` | `# @capture token = body.$.access_token`, kept between runs |
| `assert { res.status: eq 200 }` | `# @assert status == 200` |
| `res.body.items: length 3`, `isString`, `isNumber`, `isEmpty`, `isNull` | `# @assert body.$.items length == 3`, and the same predicates |
| `eq`, `neq`, `gt`, `gte`, `lt`, `lte` | `==`, `!=`, `>`, `>=`, `<`, `<=` |
| `contains`, `startsWith`, `endsWith`, `matches` | the same names |
| `isDefined`, `isUndefined` | `exists`, `not exists` |
| `notContains`, `notMatches`, `in`, `between`, `isTruthy`, `isJson` | no direct form: assert the positive case, or two bounds for `between` |
| `bru run --env local` | `apic run <file>.http --env local` |
| `bru run --csv-file-path rows.csv` | `apic run --data rows.csv` |
| `--reporter-json`, `--reporter-junit`, `--reporter-html` | `apic run --json`, `apic test --format junit`, `apic run --report report.html` |
| Script, tests and docs blocks | see [what does not carry over](#what-does-not-carry-over) |

## A worked example

Two Bruno requests against the demo API, a log-in that keeps the token and
a list that uses it:

```text
meta {
  name: Log in
  seq: 1
}

post {
  url: {{baseUrl}}/auth/login
  body: json
}

body:json {
  {"user": "alice", "password": "{{password}}"}
}

vars:post-response {
  token: res.body.access_token
}

assert {
  res.status: eq 200
}
```

```text
meta {
  name: Open todos
  seq: 2
}

get {
  url: {{baseUrl}}/todos?done=false
  auth: bearer
}

auth:bearer {
  token: {{token}}
}

assert {
  res.status: eq 200
  res.body: isArray
  res.body[0].done: eq false
}
```

and `environments/local.bru`:

```text
vars {
  baseUrl: http://localhost:8089
}
vars:secret [
  password
]
```

In apic, both requests go in one file, the environment in
`http-client.env.json` and the secret in the private file (with
`apic demo` running in another terminal):

<!-- learn -->
```sh
mkdir -p from-bruno
cat > from-bruno/http-client.env.json <<'EOF'
{
  "local": { "baseUrl": "http://localhost:8089" }
}
EOF
cat > from-bruno/http-client.private.env.json <<'EOF'
{
  "local": { "password": "s3cret" }
}
EOF
printf 'env: local\n' > from-bruno/apic.yaml
cat > from-bruno/todos.http <<'EOF'
### Log in
# @name log-in
# @assert status == 200
# @capture token = body.$.access_token
POST {{baseUrl}}/auth/login
Content-Type: application/json

{"user": "alice", "password": "{{password}}"}

### Open todos
# @name open-todos
# @ref log-in
# @auth bearer {{token}}
# @assert status == 200
# @assert body.$ isArray
# @assert body.$[0].done == false
GET {{baseUrl}}/todos?done=false
EOF
apic validate -C from-bruno
apic run todos.http -C from-bruno | tail -4
```

```text
✓ 1 file, 2 requests, no problems
✓ log-in      200  1 ms
✓ open-todos  200  0 ms

2 passed · 2 requests · 1 ms
```

`# @ref log-in` is the part Bruno has no word for: `open-todos` now logs
in by itself when there is no token, so `apic run open-todos` works on its
own, and the token survives to the next command.

## What does not carry over

- **Scripts and tests blocks.** `script:pre-request`, `script:post-response`
  and `tests` are JavaScript; apic has none,
  [deliberately](../comparison.md#deliberately-not-planned). Most of what
  they do is a `# @capture`, a `# @assert` or a `--var`; the rest belongs
  in a shell script around `apic run --json`.
- **`docs` blocks.** Use the `### title` line (it becomes the request's
  description in `apic list`) or `#` comments.
- **gRPC and WebSocket requests.** apic is HTTP only.
- **The Bruno app.** `apic ui` is a terminal UI; the
  [VS Code extension](../editors.md) sends `.http` files with a click, as
  JetBrains and Neovim do.

## Checklist

- [ ] One `.http` file per Bruno folder, a `### ` block per `.bru` file, in `seq` order
- [ ] `environments/*.bru` to `http-client.env.json`, secrets to `http-client.private.env.json`
- [ ] `vars:post-response` to `# @capture`, `assert` to `# @assert`
- [ ] Collection auth to `auth.default` in `apic.yaml`
- [ ] `# @ref` on requests that need a token
- [ ] Every script and tests block rewritten or dropped on purpose
- [ ] `apic validate` is clean and `apic fmt --check` passes
- [ ] `bru run` in CI replaced with `apic run --json` or `apic test`

See also: the [`.http` format](../format.md), the
[assertion operators](../cheatsheet.md#assertion-operators) and
[apic compared with Bruno](../comparison.md#against-bruno).
