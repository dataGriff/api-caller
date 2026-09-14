# Using apic with Taskfile

If your team already runs things with [Task](https://taskfile.dev), keep
it. apic is a plain CLI with clean exit codes, so Task becomes the front
door and the `.http` files hold the requests.

## Before and after

A curl-based task carries the whole request inline and has to be edited to
change a header:

```yaml
get-user:
  cmds:
    - curl -sS -H "Authorization: Bearer $TOKEN" "$BASE_URL/users/$ID"
```

With apic the request lives in `api/users.http`, where an editor can also
send it and an agent can read it:

```http
### Fetch a user
# @name get-user
# @assert status == 200
GET {{baseUrl}}/users/{{userId}}
Authorization: Bearer {{token}}
```

and the task is a one-line reference:

```yaml
get-user:
  dir: api
  cmds: [apic run get-user --var userId={{.ID}}]
```

## Patterns

### Passthrough

`task api -- get-user --env staging` behaves exactly like `apic run`:

```yaml
version: "3"
tasks:
  api:
    desc: Run an API request, e.g. task api -- get-user --env staging
    dir: api
    cmds:
      - apic run {{.CLI_ARGS}}
```

### Named tasks with an environment variable

```yaml
vars:
  ENV: '{{.ENV | default "dev"}}'

tasks:
  login:
    desc: Log in and store the token for later tasks
    dir: api
    cmds: [apic run login --env {{.ENV}}]

  user:
    desc: task user ID=42 [ENV=staging]
    dir: api
    cmds: [apic run get-user --env {{.ENV}} --var userId={{.ID}}]

  smoke:
    desc: Run every request in smoke.http; fails if any assertion fails
    dir: api
    cmds: [apic run smoke.http --env {{.ENV}}]
```

Run `task login ENV=staging` once, then `task user ID=7 ENV=staging` as often
as you like. The token lives in apic's session, so you do not need `deps:
[login]` on every task. Add it only where a stale token would be confusing:

```yaml
  user:
    deps: [login]
    cmds: [apic run get-user --var userId={{.ID}}]
```

### Piping output into other tasks

```yaml
  order-id:
    dir: api
    cmds:
      - apic run create-order --body-only | jq -r .id > .order-id

  order-status:
    dir: api
    deps: [order-id]
    cmds:
      - apic run get-order --var orderId=$(cat .order-id) --json | jq .response.status
```

Or let apic keep the value instead of a temp file, with
`# @capture orderId = body.$.id` on `create-order`; then `get-order` can use
`{{orderId}}` directly.

### Checks in CI

```yaml
  api:check:
    desc: Validate request files and run the smoke flow
    dir: api
    cmds:
      - apic validate
      - apic run smoke.http --env {{.ENV}} --json
```

Secrets arrive through the environment rather than files:

```sh
APIC_VAR_password="$API_PASSWORD" task api:check ENV=staging
```

### curl fallback

For a machine that has Task and curl but not apic:

```yaml
  api:curl:
    desc: Print the curl for a request, e.g. task api:curl -- get-user
    dir: api
    cmds: [apic curl {{.CLI_ARGS}}]
```

### Installing apic from a task

```yaml
  api:install:
    desc: Install apic if it is missing
    status: [command -v apic]
    cmds:
      - curl -fsSL https://raw.githubusercontent.com/dataGriff/api-caller/main/install.sh | sh
```

Add `deps: [api:install]` to the api tasks and a fresh checkout works with
one `task`.

## Things to watch

- **Working directory.** `.apic/session.json` lives where apic runs. Set
  `dir: api` on every apic task so all of them share one session.
- **Quoting.** Task runs commands through its own shell interpreter. Quote
  `--var` values containing spaces or `#`: `--var 'note=hello world'`.
- **Exit codes.** Task fails a task on non-zero exit, so a failed assertion
  (exit 1) fails the task, which is what you want in CI. Use
  `ignore_error: true` on a task if you only want the report.
- **JSON in Task output.** Task prefixes nothing to stdout, so
  `task api -- get-user --json | jq` works. Use `silent: true` to hide the
  echoed command line.

## The repo's own Taskfile

`Taskfile.yml` at the root of this repository builds apic and drives two
example projects with it: `task example:demo` runs the bundled offline demo
end to end, and `task example` runs `examples/httpbin` against httpbin.org
(`ENV=dev`, needs network). Both are small working instances of these
patterns.
