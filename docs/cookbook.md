# Cookbook

Worked recipes for the jobs that come up most. Each one is self-contained;
copy the parts you need. Recipes that use the fake API from `apic demo` can
be run as they stand.

## Log in once and reuse the token everywhere

The pattern apic is built around: one request captures the token, every
other request uses it, and the value survives between invocations.

```http
### Log in
# @name login
# @assert status == 200
# @capture token = body.$.access_token
POST {{baseUrl}}/auth/login
Content-Type: application/json

{"user": "{{user}}", "password": "{{password}}"}

### Anything else
# @name whoami
# @assert status == 200
GET {{baseUrl}}/me
Authorization: Bearer {{token}}
```

```sh
apic run login      # once
apic run whoami     # now, tomorrow, in another shell
```

The token lands in `.apic/session.json` under the current environment.
`apic session` shows it, `apic session clear` forgets it, and `--no-session`
ignores it for one run. If you forget the login, the error says which
request would have captured the missing value.

## Create, read, update, delete in one flow

Requests in a file run in order and pass values down the chain, so a full
resource lifecycle is one command.

```http
### Create a todo
# @name create-todo
# @assert status == 201
# @capture todoId = body.$.id
POST {{baseUrl}}/todos
Authorization: Bearer {{token}}
Content-Type: application/json

{"title": "Write docs"}

### Read it back
# @name get-todo
# @assert status == 200
# @assert body.$.title == "Write docs"
GET {{baseUrl}}/todos/{{todoId}}
Authorization: Bearer {{token}}

### Mark it done
# @name update-todo
# @assert status == 200
# @assert body.$.done == true
PUT {{baseUrl}}/todos/{{todoId}}
Authorization: Bearer {{token}}
Content-Type: application/json

{"done": true}

### Delete it
# @name delete-todo
# @assert status == 204
DELETE {{baseUrl}}/todos/{{todoId}}
Authorization: Bearer {{token}}
```

```sh
apic run todos.http              # stops at the first failure
apic run todos.http --keep-going # runs everything, then reports
```

This exact file ships with `apic demo`, so
`apic run todos.http -C apic-demo` works offline.

## A smoke test in CI that never leaks secrets

```yaml
# .github/workflows/smoke.yml
- run: curl -fsSL https://raw.githubusercontent.com/dataGriff/api-caller/main/install.sh | sh
- run: apic validate -C api
- run: apic run auth.http smoke.http -C api --env staging --json --redact
  env:
    APIC_VAR_password: ${{ secrets.API_PASSWORD }}
```

- `APIC_VAR_<name>` overrides any variable from the shell, so no secret
  needs to reach the runner's disk.
- `--redact` masks header values, bodies, query values and captured values
  in the stored log. Sensitive headers are masked even without it.
- `--json` gives one object per request for whatever reads the log next.
- The exit code fails the step: 1 for a failed assertion, 3 for a network
  error.

`apic validate` on its own is a cheap pull-request check: it parses every
file, reports duplicate names, bad selectors, unknown auth options and
missing body files, and exits 2 if anything is an error.

## AWS API Gateway with SigV4

```http
### List orders from a private API
# @name list-orders
# @auth aws service=execute-api region=eu-west-2
# @assert status == 200
GET https://abc123.execute-api.eu-west-2.amazonaws.com/prod/orders
Accept: application/json
```

apic signs with your existing credentials: environment variables, a named
profile, or an SSO session through the AWS CLI. Nothing apic-specific to
configure, and no AWS SDK in the binary.

```sh
aws sso login --profile prod
apic run list-orders --var profile=prod   # or: # @auth aws profile=prod
```

Set it once for a whole project in `apic.yaml` instead of on every request:

```yaml
auth:
  default: aws service=execute-api region=eu-west-2
```

Signing happens after variables are substituted, so a signed request can
still use captured values. Other services work the same way with
`service=s3`, `service=lambda`, `service=es` and so on.

## OAuth2 client credentials

```http
### Anything behind a machine-to-machine token
# @name report
# @auth oauth2 tokenUrl={{tokenUrl}} clientId={{clientId}} clientSecret={{clientSecret}} scope="api.read"
# @assert status == 200
GET {{baseUrl}}/reports/daily
```

```json
// http-client.env.json
{ "prod": { "tokenUrl": "https://login.example.com/oauth2/v2.0/token", "clientId": "…" } }
// http-client.private.env.json
{ "prod": { "clientSecret": "…" } }
```

The token is fetched on first use, cached in the session for the
environment, reused until a minute before it expires and then refreshed.
`apic session` shows what is cached and for how long. This is the same flow
for Entra ID, Okta, Auth0, Keycloak and Cognito. See
[authentication](auth.md#oauth2) for the other grants.

## A token from any CLI

When the credential comes from a tool rather than a flow:

```yaml
# apic.yaml
auth:
  allowExec: true
```

```http
# @auth exec gcloud auth print-identity-token
GET {{baseUrl}}/internal/health
```

`exec` is off unless the project opts in, because a request file that runs
commands deserves a deliberate yes.

## Poll until something is ready

apic has no loops, and deliberately so. Use the shell, and keep the request
in the file where everyone can see it:

```sh
until apic run job-status --json | jq -e '.response.body.state == "done"' >/dev/null; do
  sleep 2
done
```

Or assert the state and let a retry loop use the exit code:

```http
### Job status
# @name job-status
# @assert body.$.state == done
GET {{baseUrl}}/jobs/{{jobId}}
```

```sh
for i in $(seq 1 30); do apic run job-status && break; sleep 2; done
```

## Send a file as the body

```http
### Upload a payload written by something else
# @name import-batch
# @assert status == 202
POST {{baseUrl}}/batches
Content-Type: application/json

< ./payload.json
```

`<` sends the file as it is. `<@` substitutes `{{variables}}` inside it
first, which is handy for a fixture with an id or a timestamp in it:

```http
<@ ./templated-payload.json
```

Paths are relative to the `.http` file, and `apic validate` fails if the
file is missing.

## Follow, or do not follow, redirects

Redirects are followed by default. To assert on the redirect itself:

```http
### The redirect itself, not its destination
# @name redirect-raw
# @no-redirect
# @assert status == 302
# @assert header.location contains /status/200
GET {{baseUrl}}/redirect
```

## Give one slow endpoint more time

```http
### A report that takes a while
# @name daily-report
# @timeout 90s
# @assert status == 200
GET {{baseUrl}}/reports/daily
```

A per-request `# @timeout` beats `--timeout`, which beats `timeout:` in
`apic.yaml`, which defaults to 30 seconds.

## Two environments, one command

```json
{
  "$shared": { "userId": 42 },
  "dev":     { "baseUrl": "https://dev.example.com" },
  "staging": { "baseUrl": "https://stg.example.com" }
}
```

```sh
for env in dev staging; do
  apic run smoke.http --env "$env" --keep-going || echo "$env failed"
done
```

Each environment keeps its own captured values, so a token from `dev` is
never sent to `staging`. In the [terminal UI](tui.md), <kbd>e</kbd> switches
between them.

## Describe behaviour, then test it

```http
### Create a user
# @name create-user
# @step a user named {name} exists
# @assert status == 201
# @capture userId = body.$.id
POST {{baseUrl}}/users
Content-Type: application/json

{"name": "{{name}}"}
```

```gherkin
Feature: Users
  Scenario: A new user can be fetched
    Given I am logged in
    And a user named "alice" exists
    When I run "get-user"
    Then the response status is 200
    And the response body "$.name" is "alice"
```

```sh
apic test                                    # everything under features/
apic test --tags "@smoke && ~@slow"
apic test --format junit --output report.xml # for CI
```

`apic test --steps` prints the built-in vocabulary plus the phrases this
project declares. Full guide: [testing with Gherkin](testing.md).

## Hand the API to an agent

```sh
claude mcp add api -- apic mcp --dir ./api --env dev
```

The agent gets `list_requests`, `describe_request`, `run_request`,
`run_file`, `list_environments`, `clear_session` and `run_features`, plus
each `.http` file as a readable resource. Assertion failures come back as
`ok: false` rather than tool errors, so the agent can reason about them.

For an agent that only runs shell commands, paste the snippet from
[the agents guide](agents.md) into your `AGENTS.md` or `CLAUDE.md`.

## Drive it from a Taskfile

```yaml
tasks:
  api:
    desc: Run an API request, e.g. task api -- get-user --env staging
    dir: api
    cmds: [apic run {{.CLI_ARGS}}]

  api:check:
    desc: Validate the request files and run the smoke flow
    dir: api
    cmds:
      - apic validate
      - apic run smoke.http --env {{.ENV}} --json
```

Keep `dir:` on every apic task so they share one session. More patterns in
[the Taskfile guide](taskfile.md).

## Start from an OpenAPI document

```sh
apic import openapi.yaml -o api --env-name dev
apic validate -C api
apic list -C api
```

You get one `.http` file per tag, a named request per operation, example
bodies built from the schemas, and an env file with the server URL. Treat it
as a first draft: add `# @capture`, `# @assert` and `# @auth` where they
matter.
