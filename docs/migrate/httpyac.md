# Migrating from httpyac

httpyac and apic read the same files: `.http` requests separated by
`###`, `# @name`, `http-client.env.json` with `$shared`, `.env`, and the
same `{{$uuid}}`-style built-ins. Much of an httpyac project runs under
apic unchanged, and the two can share a project, httpyac in the editor and
apic for agents and CI. What needs changing is httpyac's JavaScript
(script blocks and `??` tests) and a handful of directives apic does not
have. `apic validate` finds each one with its line and column.

## How the concepts map

| httpyac | apic |
|---|---|
| `###`, `# @name`, request line, headers, body | the same |
| `# @ref login`, `# @forceRef login` | the same, including `{{login.response.body.$.token}}` after a `# @ref`; see [dependencies](../format.md#dependencies) |
| `{{login.token}}` (a named response's parsed body) | `{{login.response.body.$.token}}`, or `# @capture token = body.$.token` on `login` and `{{token}}` everywhere |
| `# @import ./other.http` | not needed: every `.http` file in the project is loaded, and `# @ref` reaches across files |
| `?? status == 200` | `# @assert status == 200` above the request line |
| `?? header content-type includes json` | `# @assert header.content-type contains json` |
| `?? duration < 500` | `# @assert duration < 500` |
| `?? body == hello` | `# @assert body == hello` |
| `?? js response.parsedBody.id == 42` | `# @assert body.$.id == 42` |
| `{{ exports.token = response.parsedBody.token }}` | `# @capture token = body.$.token` |
| Other `{{ … }}` script blocks | see [what does not carry over](#what-does-not-carry-over) |
| `# @loop for 3`, `# @loop for item of items` | `apic run --data rows.csv`, one run per row |
| `# @sleep 1000` (milliseconds) | `# @sleep 1s` (a Go duration) |
| `# @timeout 5000` (milliseconds) | `# @timeout 5s` |
| `# @no-cookie-jar` | `# @no-cookies`, when the [cookie jar](../format.md#cookies) is on |
| `Authorization: Basic user:pass`, `Digest user pass` | `# @auth basic user pass`, `# @auth digest user pass` |
| `Authorization: openid client_credentials local`, OAuth2 variables | `# @auth oauth2 …`, or a JetBrains `Security.Auth` block; see [authentication](../auth.md) |
| AWS signing variables | `# @auth aws region=… service=…` |
| `httpyac send file.http --all` | `apic run file.http` |
| `httpyac send file.http --name login` | `apic run login` |
| `--env dev`, `--var key=value` | `--env dev`, `--var key=value` |
| `--json`, `--junit` | `apic run --json`, `apic test --format junit` |
| `.httpyac.json`, `httpyac.config.js` | `apic.yaml` for the settings apic has |
| `env/*.env` files per environment | environments in `http-client.env.json`; a root `.env` is still read |

## A worked example

An httpyac file with the usual mix: an import, a file-level script, a
`??` test, a response handler that exports the token, a `# @ref` and a
loop:

```http
# @import ./shared.http

{{
  exports.started = Date.now();
}}

###
# @name login
POST {{baseUrl}}/auth/login
Content-Type: application/json

{"user": "alice", "password": "{{password}}"}

?? status == 200

{{
  exports.token = response.parsedBody.access_token;
}}

###
# @name todos
# @ref login
# @loop for 3
GET {{baseUrl}}/todos
Authorization: Bearer {{token}}

?? status == 200
```

apic reads the directives it knows, warns about the ones it does not, and
refuses the script blocks, which it cannot tell from a malformed request:

```text
todos.http:1:3: warning: unknown directive @import (ignored) (unknown-directive)
todos.http:4:3: error: expected a header (`Name: value`) or a blank line before the body, got "exports.started = Date.now();" (bad-header)
todos.http:5:1: error: expected a header (`Name: value`) or a blank line before the body, got "}}" (bad-header)
todos.http:23:3: warning: unknown directive @loop (ignored) (unknown-directive)
✗ 1 file, 3 requests, 2 errors, 2 warnings
```

The `??` lines and the script after the body are worse: they sit where
the body is, so they would be sent as part of it. Take them out as you
turn them into directives. The same file, for apic (with `apic demo`
running in another terminal):

<!-- learn -->
```sh
mkdir -p from-httpyac
cat > from-httpyac/http-client.env.json <<'EOF'
{
  "$shared": { "baseUrl": "http://localhost:8089" },
  "local": {}
}
EOF
cat > from-httpyac/.env <<'EOF'
password=s3cret
EOF
cat > from-httpyac/todos.http <<'EOF'
###
# @name login
# @assert status == 200
POST {{baseUrl}}/auth/login
Content-Type: application/json

{"user": "alice", "password": "{{password}}"}

###
# @name todos
# @ref login
# @assert status == 200
GET {{baseUrl}}/todos
Authorization: Bearer {{login.response.body.$.access_token}}
EOF
apic validate -C from-httpyac
apic run todos -C from-httpyac --env local --no-session | grep -E '^(POST|GET|✓)'
```

```text
✓ 1 file, 2 requests, no problems
POST http://localhost:8089/auth/login
✓ status == 200
GET http://localhost:8089/todos
✓ status == 200
```

`# @ref login` meant the same in both tools, and still does: `todos`
reads `login`'s response, so `login` runs first. The file keeps working in
httpyac, which reads `# @assert` as an unknown comment and `.env` and
`http-client.env.json` as before.

The `# @loop for 3` became nothing here. To run a request once per row of
data, give `apic run` a CSV file:

<!-- learn -->
```sh
printf 'page\n1\n2\n3\n' > from-httpyac/pages.csv
cat >> from-httpyac/todos.http <<'EOF'

###
# @name todos-page
# @ref login
# @assert status == 200
GET {{baseUrl}}/todos?page={{page}}&limit=1
Authorization: Bearer {{login.response.body.$.access_token}}
EOF
apic run todos-page -C from-httpyac --env local --no-session --data from-httpyac/pages.csv | tail -1
```

## What does not carry over

- **JavaScript.** Script blocks, response handlers, `?? js` tests with
  logic, hooks and plugins. apic has no scripting,
  [deliberately](../comparison.md#deliberately-not-planned); what the
  scripts did is usually a `# @capture`, an `# @assert` or a `--var`, and
  the rest belongs in a shell script around `apic run --json`.
- **`# @import`.** Not needed for requests (the whole project is loaded),
  but a variable defined in an imported file must move to
  `http-client.env.json` or to the file that uses it.
- **gRPC, WebSocket, MQTT, AMQP and server-sent events.** apic is HTTP only.
- **`# @loop`.** `apic run --data` covers a loop over data; a loop over
  a computed list needs a shell loop.
- **httpyac's config files.** `apic.yaml` has the settings apic has:
  default environment, timeout, retry, proxy, TLS, auth defaults.

## Checklist

- [ ] `apic validate` over the project, and every error and warning dealt with
- [ ] `??` tests to `# @assert` above the request line
- [ ] Response handlers that export values to `# @capture` (or `{{name.response…}}` after a `# @ref`)
- [ ] Script blocks removed, their jobs moved to directives, `--var` or a shell script
- [ ] `# @sleep` and `# @timeout` values given units
- [ ] `Authorization:` shorthands to `# @auth`
- [ ] `# @loop` to `apic run --data`
- [ ] `apic fmt --check` passes, and the files still open in httpyac

See also: the [`.http` format](../format.md),
[response references](../format.md#response-references) and
[apic compared with httpyac](../comparison.md#against-httpyac).
