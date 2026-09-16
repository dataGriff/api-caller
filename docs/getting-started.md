# Getting started

This walks through installing apic, describing an API in `.http` files,
logging in once and reusing the token, wiring it into CI, and handing the
same files to an AI agent. It takes about ten minutes.

## 1. Install

=== "Go"

    ```sh
    go install github.com/dataGriff/api-caller/cmd/apic@latest   # Go 1.25 or newer
    ```

=== "Linux / macOS"

    ```sh
    curl -fsSL https://raw.githubusercontent.com/dataGriff/api-caller/main/install.sh | sh
    ```

    `APIC_VERSION=v1.2.3` pins a version, `APIC_INSTALL_DIR=~/bin` chooses
    where it lands. The installer verifies the release checksum.

=== "Windows"

    Download the zip from [GitHub Releases](https://github.com/dataGriff/api-caller/releases)
    and put `apic.exe` on your `PATH`.

Check it with `apic version`.

## 2. See it work before writing anything

```sh
apic ui --demo
```

That serves a small fake API inside the same process and opens the
[terminal UI](tui.md) on an example project pointing at it. Press
<kbd>enter</kbd> to send the request under the cursor, <kbd>f</kbd> to run a
whole file as a flow, <kbd>?</kbd> for every key, <kbd>q</kbd> to quit. Nothing
is left behind.

The same example works from the plain CLI: `apic demo` writes it to
`./apic-demo` and serves the API, then in another terminal:

```sh
apic run login whoami -C apic-demo
apic run todos.http -C apic-demo --keep-going
apic test -C apic-demo
```

Read `apic-demo/auth.http` and `apic-demo/todos.http` to see the files
behind those commands.

## 3. Create a project

A project is any directory containing `.http` files. Keeping them in an
`api/` folder next to your code works well. `apic init` writes a working
starting point:

```sh
apic init api --base-url https://dev.example.com --env dev
```

```
api/
  apic.yaml
  http-client.env.json
  http-client.private.env.json
  api.http
  features/smoke.feature
  .gitignore
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

`http-client.private.env.json` has the same shape and holds secrets. `apic
init` gitignores it for you. apic masks values from this file wherever it
prints variables.

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

## 4. Write requests

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

Check the files parse, and see what you have:

```sh
cd api
apic validate
apic list
apic list user     # only requests matching "user"
```

## 5. Run requests

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

{ "access_token": "eyJ..." }

✓ status == 200
↳ token = eyJ...
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

!!! tip "Or drive it interactively"
    `apic ui` shows the same project with the requests on the left and the
    response, checks and session on the right, and marks which requests are
    ready to run. See [the terminal UI](tui.md).

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

## 6. Use the output in scripts

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

## 7. Put it in CI

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

## 8. Add authentication

If the API needs more than a bearer token, put it in the file or the
project config and apic handles it at send time:

```http
# @auth aws service=execute-api region=eu-west-2
# @auth basic {{user}} {{password}}
# @auth oauth2 tokenUrl={{tokenUrl}} clientId={{clientId}} clientSecret={{clientSecret}}
```

AWS uses your existing credentials (environment, profiles, SSO via the AWS
CLI); OAuth2 tokens are cached and refreshed. See [auth.md](auth.md).

## 9. Describe behaviour in Gherkin

Add phrases to requests and write features; apic runs them with no
Cucumber runtime:

```http
### Create a user
# @name create-user
# @step a user named {name} exists
POST {{baseUrl}}/users
...

### Fetch a user
# @name get-user
# @step I fetch the user
GET {{baseUrl}}/users/{{userId}}
```

```gherkin
Feature: Users
  Scenario: Fetch a user by id
    Given I run "login"
    And a user named "alice" exists
    When I fetch the user
    Then the response status is 200
    And the response body "$.name" is "alice"
```

```sh
apic test                                  # features/ under the project
apic test --format junit --output report.xml
```

See [testing.md](testing.md) for the full vocabulary.

## 10. Hand it to an agent

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

- [cheatsheet.md](cheatsheet.md): every directive, selector and operator on one page
- [cookbook.md](cookbook.md): worked recipes for CI, AWS, OAuth2, uploads and polling
- [format.md](format.md): everything the `.http` dialect supports
- [cli.md](cli.md): every command and flag
- [tui.md](tui.md): the terminal UI
- [faq.md](faq.md): short answers when something misbehaves
