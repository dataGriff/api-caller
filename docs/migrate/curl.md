# Migrating from curl and Taskfile

This is the move apic was made for: requests that live as curl commands in
a Taskfile, a Makefile, a wiki page or shell history, with the token
pasted in by hand. Each command becomes a named request in a `.http` file
that editors can send, agents can read and CI can run, and Task, if you
use it, stays as the front door.

## How the concepts map

| curl and a Taskfile | apic |
|---|---|
| A curl command | a `### ` block with `# @name` in a `.http` file |
| `-X POST`, `-H`, `-d`, `-F`, `-u` | the request line, headers, body, a multipart body, `# @auth basic` |
| `$BASE_URL`, `$TOKEN` in the shell | `{{baseUrl}}` from `http-client.env.json`, `{{token}}` captured by a login request |
| `TOKEN=$(curl … \| jq -r .token)` | `# @capture token = body.$.access_token`, kept in the session between commands |
| `curl -f`, `\| grep`, `\| jq -e` to check a result | `# @assert status == 200`, `# @assert body.$.id exists` |
| `-k`, `--cacert`, `--cert`, `-x` | `--insecure`, `--cacert`, `--cert`, `--proxy`, or `tls:` and `proxy:` in `apic.yaml` |
| A task per request | `task api -- <name>`, one passthrough task (see [Taskfile](../taskfile.md)) |
| `task --list` | `apic list`, with methods, URLs and descriptions |
| Copy the command from the wiki | `apic curl <name>` prints it back, runnable as printed; `--redact` for one that is safe to paste into a log |

## Turn each command into a request

`apic import --curl` reads a curl command, in either quote style and
with backslash continuations, and appends a request to a file. With
`apic demo` running in another terminal:

<!-- learn -->
```sh
mkdir -p from-curl
cat > from-curl/http-client.env.json <<'EOF'
{
  "local": { "baseUrl": "http://localhost:8089" }
}
EOF
printf 'env: local\n' > from-curl/apic.yaml
apic import -C from-curl --into auth.http --name login \
  --curl 'curl -s -X POST http://localhost:8089/auth/login -H "Content-Type: application/json" -d '"'"'{"user":"alice","password":"s3cret"}'"'"''
apic import -C from-curl --into todos.http --name list-todos \
  --curl 'curl -s http://localhost:8089/todos -H "Authorization: Bearer mock-token"'
cat from-curl/auth.http from-curl/todos.http
```

```text
added login to auth.http
added list-todos to todos.http
### POST /auth/login
# @name login
# @assert status == 200
POST {{baseUrl}}/auth/login
Content-Type: application/json

{"user":"alice","password":"s3cret"}
### GET /todos
# @name list-todos
# @assert status == 200
GET {{baseUrl}}/todos
Authorization: Bearer mock-token
```

The host became `{{baseUrl}}` because the environment's `baseUrl`
prefixes it, and each request got a status check. What the importer
cannot know is where the secrets come from: the password and the token
are still pasted in.

## Take the secrets out

Move the password to the private env file, which stays out of git,
capture the token from the login response, and let `list-todos` log in by
itself when it has none:

<!-- learn -->
```sh
cat > from-curl/http-client.private.env.json <<'EOF'
{
  "local": { "password": "s3cret" }
}
EOF
cat > from-curl/auth.http <<'EOF'
### Log in and keep the token
# @name login
# @assert status == 200
# @capture token = body.$.access_token
POST {{baseUrl}}/auth/login
Content-Type: application/json

{"user": "alice", "password": "{{password}}"}
EOF
cat > from-curl/todos.http <<'EOF'
### List todos
# @name list-todos
# @ref login
# @assert status == 200
GET {{baseUrl}}/todos
Authorization: Bearer {{token}}
EOF
apic session clear -C from-curl
apic run list-todos -C from-curl | grep -E '^(POST|GET|✓|↳)'
```

```text
POST http://localhost:8089/auth/login
✓ status == 200
↳ token = mock-token
↳ ran login first (# @ref)
GET http://localhost:8089/todos
✓ status == 200
```

## Keep Task as the front door

A Taskfile of curl commands:

```yaml
version: "3"
tasks:
  login:
    cmds:
      - curl -s -X POST "$BASE_URL/auth/login" -H "Content-Type: application/json" -d "{\"user\":\"alice\",\"password\":\"$PASSWORD\"}" | jq -r .access_token > .token
  todos:
    cmds:
      - curl -sf "$BASE_URL/todos" -H "Authorization: Bearer $(cat .token)"
```

becomes one task that passes its arguments through, plus named tasks for
the ones people type every day:

```yaml
version: "3"
tasks:
  api:
    desc: Run an API request, e.g. task api -- list-todos --env staging
    dir: from-curl
    cmds:
      - apic run {{.CLI_ARGS}}
  todos:
    desc: List the todos
    dir: from-curl
    cmds:
      - apic run list-todos
```

The `.token` file, the `jq` and the `-f` are gone: the session holds the
token, `# @ref` fetches it, and `apic run` exits non-zero when an
assertion fails (1), a request cannot be built (2) or the network fails
(3). More patterns are in [Using apic with Taskfile](../taskfile.md).

## And back to curl

For the machine with no apic, or a bug report, `apic curl` prints the
command again with every variable filled in, so it runs as printed. With
`--redact`, credentials become shell placeholders such as `$TOKEN` and the
other values are masked, for pasting into a ticket or a log:

<!-- learn -->
```sh
apic curl list-todos -C from-curl
apic curl list-todos -C from-curl --redact
```

```text
curl -sS \
  -H 'Authorization: Bearer mock-token' \
  'http://localhost:8089/todos'
curl -sS \
  -H 'Authorization: ***' \
  'http://localhost:8089/todos'
```

`apic snippet list-todos --lang python` does the same for HTTPie,
PowerShell, Python, JavaScript and Go.

## What does not carry over

- **Shell logic between commands.** Loops, conditionals and computed
  values stay in the shell, around `apic run --json`, which prints one
  JSON object per request for `jq`.
- **curl's own output flags** (`-o`, `-w`, `-v`, `--trace`). apic has
  `-v`, `--json`, `--body-only` and `--output <file>`; the importer notes
  each flag it drops.
- **Protocols other than HTTP.** apic does not do FTP, SMTP or the rest
  of what curl speaks.

## Checklist

- [ ] `apic import --curl '…' --into <file>.http --name <name>` for each command
- [ ] Hosts to `{{baseUrl}}` in `http-client.env.json`, one environment per target
- [ ] Passwords and keys to `http-client.private.env.json`; `.apic/` and the private file in `.gitignore`
- [ ] Hand-pasted tokens replaced with `# @capture` on the login and `# @ref login` where it is needed
- [ ] Checks done with `grep` or `jq -e` moved to `# @assert`
- [ ] Taskfile tasks reduced to `apic run <name>`
- [ ] `apic validate` is clean and `apic fmt --check` passes

See also: [`apic import --curl`](../cli.md#apic-import),
[`apic curl`](../cli.md#apic-curl), [`apic snippet`](../cli.md#apic-snippet)
and [Using apic with Taskfile](../taskfile.md).
