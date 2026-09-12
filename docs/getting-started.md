# Getting started

This walks through installing apic, describing an API in `.http` files,
logging in once and reusing the token, wiring it into CI, and handing the
same files to an AI agent. It takes about ten minutes.

## 1. Install

```sh
# Go 1.25 or newer
go install github.com/dataGriff/api-caller/cmd/apic@latest

# Linux or macOS without Go
curl -fsSL https://raw.githubusercontent.com/dataGriff/api-caller/main/install.sh | sh

# Windows: download the zip from GitHub Releases and put apic.exe on your PATH
```

Check it: `apic version`. Want something to point it at right away, with no
setup? Run `apic demo` — see the [README's "Try it now"](../README.md#try-it-now)
section, which scaffolds a fake API and example `.http` files for you.

## 2. Create a project

A project is any directory containing `.http` files. Keeping them in an
`api/` folder next to your code works well.

```
api/
  apic.yaml
  http-client.env.json
  http-client.private.env.json
  auth.http
  users.http
```

`apic.yaml` sets defaults so you do not repeat flags:

```yaml
env: dev
```

`http-client.env.json` holds per-environment values. `$shared` applies to
every environment.

```json
{
  "$shared": { "userId": 42 },
  "dev":     { "baseUrl": "https://dev.example.com",  "user": "alice" },
  "staging": { "baseUrl": "https://stg.example.com",  "user": "alice" }
}
```

`http-client.private.env.json` has the same shape and holds secrets. Add it
to `.gitignore`. apic masks values from this file wherever it prints
variables.

```json
{
  "dev":     { "password": "s3cret" },
  "staging": { "password": "other" }
}
```

If you already have an OpenAPI document, let apic write the first draft:

```sh
apic import openapi.yaml -o api
```

## 3. Write requests

`api/auth.http`:

```http
### Log in and keep the token
# @name login
# @assert status == 200
# @capture token = body.$.access_token
POST {{baseUrl}}/auth/login
Content-Type: application/json

{"user": "{{user}}", "password": "{{password}}"}
```

`api/users.http`:

```http
### Fetch a user
# @name get-user
# @description Fetch a single user by id
# @assert status == 200
# @assert body.$.id == {{userId}}
GET {{baseUrl}}/users/{{userId}}
Authorization: Bearer {{token}}
Accept: application/json

### Create a user
# @name create-user
# @assert status == 201
# @capture newUserId = body.$.id
POST {{baseUrl}}/users
Content-Type: application/json

{"name": "{{user}}", "email": "{{user}}@example.com"}
```

These are ordinary `.http` files. Open them in VS Code with the REST Client
extension, in a JetBrains IDE, or in Neovim with kulala, and the "Send
Request" action works as usual. apic's `# @` lines are comments to those
tools.

Check the files parse:

```sh
cd api
apic validate
apic list
```

## 4. Run requests

```sh
apic run get-user
```

The first time, this fails with exit code 2 and tells you why:

```
error: users.http:6: missing variable
  {{token}}: it is captured by request "login"; run `apic run login` first, or pass --var token=...
```

So log in:

```sh
apic run login
```

```
POST https://dev.example.com/auth/login
200 OK · 87 ms · 412 B

{ "access_token": "eyJ…" }

✓ status == 200
↳ token = eyJ…
```

The token is now in `api/.apic/session.json` for the `dev` environment (the
directory ignores itself from git). Every later run in `dev` can use
`{{token}}` until you clear it or log in again:

```sh
apic run get-user
apic run get-user --env staging      # separate session, needs its own login
apic describe get-user               # see where each variable comes from
apic session                         # what is captured
apic session clear                   # forget it
```

Override anything for one run:

```sh
apic run get-user --var userId=7
```

Run a whole file in order as a flow. It stops at the first failure and exits
1 if anything failed:

```sh
apic run users.http
apic run users.http --keep-going
```

## 5. Use the output in scripts

```sh
apic run get-user --body-only | jq .email
apic run users.http --json | jq -c '{name: .request.name, ok, status: .response.status}'
```

Exit codes make apic safe in `set -e` scripts and CI steps:

| Code | Meaning |
|---|---|
| 0 | ok |
| 1 | an assertion or capture failed |
| 2 | usage, parse error, unknown request or missing variable |
| 3 | network error or timeout |

## 6. Put it in CI

```yaml
# .github/workflows/smoke.yml
- run: curl -fsSL https://raw.githubusercontent.com/dataGriff/api-caller/main/install.sh | sh
- run: apic validate -C api
- run: apic run auth.http users.http -C api --env staging --json --redact
  env:
    APIC_VAR_password: ${{ secrets.API_PASSWORD }}
```

`APIC_VAR_<name>` environment variables override values from the env files,
so secrets never need to be in a file on the runner. `--redact` masks
header values, bodies, query values and captures in the stored log;
sensitive headers are masked even without it. `apic run` runs targets
in the order given, so `auth.http users.http` logs in first.

## 7. Add authentication

If the API needs more than a bearer token, put it in the file or the
project config and apic handles it at send time:

```http
# @auth aws service=execute-api region=eu-west-2
# @auth basic {{user}} {{password}}
# @auth oauth2 tokenUrl={{tokenUrl}} clientId={{clientId}} clientSecret={{clientSecret}}
```

AWS uses your existing credentials (environment, profiles, SSO via the AWS
CLI); OAuth2 tokens are cached and refreshed. See [auth.md](auth.md).

## 8. Hand it to an agent

Add to your project's `AGENTS.md` or `CLAUDE.md`:

```markdown
API requests live in api/*.http and are run with apic from the api/ directory:
`apic list --json`, `apic describe <id> --json`, `apic run <id> --json`.
Run `apic run login` once before requests that need {{token}}.
```

Or register the MCP server so the agent calls requests as tools:

```sh
claude mcp add api -- apic mcp --dir ./api --env dev
```

See [agents.md](agents.md) for the details.

## Next

- [format.md](format.md): everything the `.http` dialect supports
- [cli.md](cli.md): every command and flag
- [taskfile.md](taskfile.md): keep `task` as the front door
