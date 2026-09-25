# Cookbook

Worked recipes for the jobs that come up most. Each one is self-contained;
copy the parts you need. Recipes that use the fake API from `apic demo` can
be run as they stand.

## Running against real APIs

`examples/` in the repo has three runnable projects, each targeting a real
public API instead of `apic demo`'s in-process fake:

```sh
apic run auth.http users.http -C examples/httpbin --env dev   # basic + bearer, no account needed
apic run repo.http -C examples/github --env dev                # bearer, needs your own PAT
apic run search.http -C examples/spotify --env dev             # oauth2 client_credentials, needs your own app
```

For `github` and `spotify`, drop your own credentials into that project's
`http-client.private.env.json` first; see `examples/README.md`.

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

To not have to remember it, let the request say what it needs:

```http
### Anything else
# @name whoami
# @ref login
# @assert status == 200
GET {{baseUrl}}/me
Authorization: Bearer {{token}}
```

Now `apic run whoami` logs in first when `{{token}}` is missing and goes
straight to `/me` when it is not. `# @forceRef login` logs in every time,
for a token that must not be reused. See
[format.md](format.md#dependencies).

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
- uses: dataGriff/api-caller/setup-apic@v0
- run: apic validate -C api --format github
- run: apic run auth.http smoke.http -C api --env staging --json --redact
  env:
    APIC_VAR_password: ${{ secrets.API_PASSWORD }}
```

- `setup-apic` installs a release verified against `checksums.txt`, cached
  per version and platform, on every runner OS; `with: version: v0.1.2`
  pins one. Elsewhere, `install.sh` does the same.
- `APIC_VAR_<name>` overrides any variable from the shell, so no secret
  needs to reach the runner's disk.
- `--redact` masks header values, bodies, query values and captured values
  in the stored log. Sensitive headers are masked even without it.
- `--json` gives one object per request for whatever reads the log next.
- The exit code fails the step: 1 for a failed assertion, 3 for a network
  error.

For a report a person opens rather than a log a tool parses, add
`--report` (or `apic test --format html --output`) and upload the file as
an artifact; it is one HTML file with no external assets, redacted the
same way:

```yaml
- run: apic run auth.http smoke.http -C api --env staging --redact --report smoke.html
  env:
    APIC_VAR_password: ${{ secrets.API_PASSWORD }}
- uses: actions/upload-artifact@v7
  if: always()
  with:
    name: smoke-report
    path: smoke.html
```

`apic validate` on its own is a cheap pull-request check: it parses every
file, reports duplicate names, bad selectors, unknown auth options and
missing body files, and exits 2 if anything is an error. With
`--format github` each finding becomes an annotation on the pull request
at the exact line and column:

```yaml
- run: apic validate -C api --format github
```

### In a container

`ghcr.io/datagriff/apic` is the same binary on `scratch`, with the CA
bundle it needs for TLS: about 7 MB, for amd64 and arm64, tagged with the
version (`0.2.0` and `v0.2.0`), the minor (`v0.2`) and `latest`. Mount the
project at `/work` and pass secrets as `APIC_VAR_*` variables:

```sh
docker run --rm -v "$PWD/api:/work" -e APIC_VAR_password \
  ghcr.io/datagriff/apic run auth.http smoke.http --env staging --json --redact
```

- Files apic writes (`.apic/session.json`, a report) belong to root unless
  you add `--user "$(id -u):$(id -g)"`.
- `apic ui` needs a terminal: `docker run --rm -it …`.
- `apic mcp --http 0.0.0.0:8765` with `-p 8765:8765` serves an agent
  outside the container (set a token, as for any non-loopback address).
  `apic demo` listens on the container's loopback only, so it is for the
  container's own use.

The image has no shell, so it cannot be a CI system's job image where the
job runs a script inside it (GitLab's `image:`, an Azure container job).
There, copy the binary into the image you already use:

```dockerfile
FROM node:22
COPY --from=ghcr.io/datagriff/apic:v0.2 /apic /usr/local/bin/apic
```

or run the installer in the job:

```yaml
# .gitlab-ci.yml
smoke:
  image: alpine:3.22
  script:
    - apk add --no-cache curl
    - curl -fsSL https://raw.githubusercontent.com/dataGriff/api-caller/main/install.sh | sh
    - apic run auth.http smoke.http -C api --env staging --json --redact
```

The image is signed like the release archives;
[verifying.md](verifying.md#verify-the-container-image) has the command.

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
{ "prod": { "tokenUrl": "https://login.example.com/oauth2/v2.0/token", "clientId": "..." } }
// http-client.private.env.json
{ "prod": { "clientSecret": "..." } }
```

The token is fetched on first use, cached in the session for the
environment, reused until a minute before it expires and then refreshed.
`apic session` shows what is cached and for how long. This is the same flow
for Entra ID, Okta, Auth0, Keycloak and Cognito. See
[authentication](auth.md#oauth2) for the other grants.

## Sign in as yourself

```http
### My repositories, as me
# @name my-repos
# @auth oauth2 grant=authorization_code authUrl={{authUrl}} tokenUrl={{tokenUrl}} clientId={{clientId}} scope="repo read:user"
# @assert status == 200
GET https://api.github.com/user/repos
```

```json
// http-client.env.json
{ "dev": { "authUrl": "https://github.com/login/oauth/authorize", "tokenUrl": "https://github.com/login/oauth/access_token", "clientId": "Iv1..." } }
```

Run `apic run my-repos` once from a terminal: a browser opens on the
provider's sign-in page, and the token comes back through a loopback
redirect (register `http://127.0.0.1:<port>/callback` with the app, with
`redirectPort=` on the directive when the provider wants the exact port).
From then on the cached token, refreshed while its refresh token lasts,
serves `--json`, agents and CI alike; only a fresh sign-in needs the
terminal again.

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

Say what "ready" looks like and how long to wait for it:

```http
### Wait for the job
# @name job-status
# @retry 30 2s
# @assert status == 200
# @assert body.$.state == done
GET {{baseUrl}}/jobs/{{jobId}}
```

`apic run job-status` sends the request until both assertions pass or thirty
attempts are spent, printing a line per failed attempt, and exits 1 if the
job never gets there. `retry:` in `apic.yaml` or `--retry "30 2s"` applies
the same policy to every request without its own; see
[format.md](format.md#retries).

When the condition is not one assertion can express, the shell still works,
with the request kept in the file where everyone can see it:

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

## Upload a file

A multipart form, the way the editors write it: the boundary in the header,
each part between delimiter lines, and a file part whose content is a
`< file` reference. apic sends the file's bytes, not the text.

```http
### Upload a report with a title
# @name upload-report
# @assert status == 201
# @assert body.$.files[0].filename == report.pdf
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

Text parts are templates, so `{{user}}` resolves as anywhere else. `apic
curl upload-report` prints the same upload as `--form-string` and
`-F file=@report.pdf` options, and `apic validate` fails when the part file
is missing. The demo API's `POST /upload` echoes the parts back, so
`apic demo` gives you something to try this against.

When the whole body is a file rather than a form, the reference stands
alone:

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

## Download a file

A `>>` line after the body saves the response body, bytes as they came,
so a CSV export or an image lands on disk intact:

```http
### Daily report
# @name daily-report
# @assert status == 200
# @assert header.content-type contains text/csv
GET {{baseUrl}}/reports/daily.csv

>>! ./fixtures/daily.csv
```

`>>` creates the file and fails if it exists; `>>!` overwrites. The path
is relative to the `.http` file and stays inside the project; the run
says `↳ saved to fixtures/daily.csv`. For a one-off without editing the
file:

```sh
apic run daily-report --output /tmp/daily.csv
```

A binary body is summarised in the output (`binary body · 12 KB ·
image/png`) rather than printed, and under `--json` it travels as
base64 with `"body_encoding": "base64"`. A saved file is created `0600`
when a secret (a private variable, a captured token, a credential) went
into the request, since the response may carry one back.

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

## Run one request for every row

```csv
id,name
7,alice
8,bob
```

```http
### Update a user
# @name update-user
# @assert status == 200
# @assert body.$.name == {{name}}
PATCH {{baseUrl}}/users/{{id}}
Content-Type: application/json

{"name": "{{name}}"}
```

```sh
apic run update-user --data users.csv --keep-going --report users.html
```

Each row is one iteration, its columns variables, so the assertion checks
each user against its own row. Iterations do not share captures unless
`--data-share-session`; see [Data-driven runs](cli.md#data-driven-runs).

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

## Start from a curl command

Every API's docs have a curl example; paste it and get a named request:

```sh
apic import --curl 'curl -X POST https://api.example.com/todos \
  -H "Content-Type: application/json" \
  -d "{\"title\": \"x\"}"' --into todos.http --name create-todo
```

The host becomes `{{baseUrl}}` when the environment has one, `-u` becomes
`# @auth basic`, `-F` becomes a multipart body, and anything apic cannot
carry (`-o`, `--retry`, an unknown flag) is a note on stderr rather than a
failure. On a Mac, `pbpaste | apic import --curl - --into todos.http`
takes the command straight from the clipboard.

## Migrate a Postman collection

```sh
apic import My-API.postman_collection.json -o api --postman-env staging.postman_environment.json
apic validate -C api
apic list -C api
```

Folders become files, requests keep their names in kebab-case, the
collection's variables land in `$shared` and each environment export
becomes an environment, secrets in the private file. Bearer, basic, AWS
and OAuth2 client-credentials auth become `# @auth` lines, an API key
becomes its header, and the simple `pm.test` checks (`to.have.status`,
`pm.expect(jsonData.x).to.eql(...)`, header checks) become `# @assert`
lines, with `pm.environment.set("token", jsonData.token)` becoming
`# @capture`. What has no equivalent, pre-request scripts and the rest of
the test scripts, is printed as `note` lines so nothing is lost silently;
the [comparison](comparison.md#against-postman) says what to do instead.

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
