# CLI reference

```
apic [command] [flags]
```

All commands are non-interactive: apic never prompts. The one exception is
`apic ui`, the terminal UI, which refuses to start unless it has a terminal
to draw on. Colour is off when stdout is not a terminal, when `NO_COLOR` is
set, when `--no-color` is given, or when `--json` is used.

## Global flags

| Flag | Meaning |
|---|---|
| `-C, --dir <path>` | Project root holding the `.http` files and env files. Default `.`. Relative `.http` paths on the command line are resolved against it. |
| `-e, --env <name>` | Environment from `http-client.env.json`. Defaults to `env:` in `apic.yaml`, else none (only `$shared` values apply). An unknown name is an error listing the known ones. |
| `--var name=value` | Override a variable. Repeatable. Highest precedence. |
| `--json` | Machine-readable output. See each command for the shape. |
| `--no-color` | Disable colour. |
| `--no-session` | Do not read or write `.apic/session.json`. |
| `--timeout <duration>` | Request timeout, e.g. `10s`. Default 30s or `timeout:` in `apic.yaml`. `# @timeout` on a request wins. |
| `--insecure` | Skip TLS certificate verification. |
| `--redact` | Mask every request header value, the body, query-string values and captured values in `run` output. Use it in CI logs that are stored. Sensitive headers (`Authorization`, `Cookie`, API-key headers, and any header whose value came from a secret source) are masked even without it. |

## Exit codes

| Code | Meaning |
|---|---|
| 0 | Success. For `run`, every assertion and capture passed. For `validate`, no errors. |
| 1 | `run`: at least one assertion or capture failed. |
| 2 | Usage error: unknown command or request, parse error in a `.http` file, unknown environment, missing variable, bad `--var`. `validate` with errors. |
| 3 | Transport error: DNS, connection refused, TLS failure, timeout. |

Errors are printed to stderr prefixed with `error:`; stdout stays clean for
piping.

## Request targets

Several commands take a target:

| Target | Meaning |
|---|---|
| `get-user` | The request with `# @name get-user`. An error if the name is used in more than one file. |
| `users.http` | Every request in the file, in order. |
| `users.http#get-user` | A named request in a specific file. |
| `users.http#3` | The third request in the file. Unnamed requests are listed with this id. |

Files are found by walking the project root for `*.http` and `*.rest`,
skipping hidden directories, `node_modules` and `vendor`. `apic.yaml` can
narrow this with `dir: api`.

## apic run

```
apic run <target>... [-v] [--body-only] [--keep-going]
```

Sends requests and reports status, timing, body, captures and assertions.
Several targets run in the order given. More than one request is a flow:
it stops at the first failed assertion, failed capture or transport error
unless `--keep-going` is set, and prints a pass/fail summary.

Captured values are available to later requests in the same run, and are
saved to `.apic/session.json` for the current environment (unless
`--no-session` or the request has `# @no-session`).

| Flag | Meaning |
|---|---|
| `-v, --verbose` | Show request headers and body, and response headers. |
| `--body-only` | Print only the response body, pretty-printed when JSON. For piping. |
| `--keep-going` | In a flow, continue after a failure. |

Examples:

```sh
apic run login
apic run get-user --env staging --var userId=42
apic run auth.http users.http --json
apic run get-user --body-only | jq .email
```

`--json` prints one object per request (NDJSON for flows):

```json
{
  "ok": true,
  "request": {
    "name": "get-user", "file": "users.http", "line": 10,
    "method": "GET", "url": "https://dev.example.com/users/42",
    "headers": {"Accept": "application/json", "Authorization": "Bearer eyJ..."},
    "body": "",
    "auth": "aws"
  },
  "response": {
    "status": 200, "status_text": "OK",
    "headers": {"content-type": "application/json"},
    "body": {"id": 42, "email": "alice@example.com"},
    "duration_ms": 87, "size": 412
  },
  "captures": {"email": "alice@example.com"},
  "asserts": [
    {"expr": "status == 200", "pass": true, "actual": "200", "expected": "200"},
    {"expr": "body.$.id == 42", "pass": true, "actual": "42", "expected": "42"}
  ]
}
```

- `request.auth` names the auth type applied, when any; credentials apic adds are never included.
- `request.headers` are the headers written in the file, with sensitive values shown as `***` (see `--redact` above). URL, body and captures are shown in full unless `--redact` is set.
- `response.body` is parsed JSON when the body is JSON, otherwise a string.
- `response.headers` keys are lower-case; multiple values are joined with `, `.
- `errors` (omitted when empty) lists failed captures and other problems.
- `ok` is false when any assertion or capture failed.
- When a request could not be sent at all (missing variable, network), the
  error goes to stderr and the exit code is 2 or 3; in a flow, the earlier
  results are still printed.

## apic test

```
apic test [path|file.feature]... [--format f] [--output file] [--tags expr] [--stop-on-failure] [--use-session] [--steps]
```

Runs Gherkin feature files against the project's requests with a built-in
step vocabulary and the `# @step` phrases declared on requests. Default
path is `features/` under the project root, or `test.paths` in `apic.yaml`.
Each scenario gets an isolated in-memory session. See [testing.md](testing.md)
for the vocabulary.

| Flag | Meaning |
|---|---|
| `-f, --format` | `pretty` (default), `progress`, `cucumber`, `junit`. `--json` selects `cucumber`. |
| `-o, --output <file>` | Write the report to a file. |
| `-t, --tags <expr>` | Tag expression, e.g. `"@smoke && ~@slow"`. |
| `--stop-on-failure` | Stop after the first failed scenario; the remaining scenarios are reported as skipped. |
| `--use-session` | Share `.apic/session.json` instead of isolating each scenario. |
| `--steps` | Print the vocabulary and this project's phrases (`--json` for machine form) and exit. |

Exit codes: `0` all passed · `1` failures or undefined steps · `2` no
features, a feature path outside the project, unknown environment, unknown
request, missing variable, bad phrase or bad flag · `3` a server could not
be reached. Feature paths must lie inside the project root.

## apic ui

```
apic ui [--demo]
```

Opens the [terminal UI](tui.md): requests on the left, each row carrying its
status and round trip once it has run, and preview, response, checks and
session tabs on the right. <kbd>enter</kbd> runs the selected request,
<kbd>f</kbd> runs its file as a flow, <kbd>e</kbd> switches environment,
<kbd>J</kbd>/<kbd>K</kbd> scroll the right pane, <kbd>?</kbd> lists every key.

| Flag | Meaning |
|---|---|
| `--demo` | Serve the bundled fake API in-process and open the UI on its example project. Needs no project and no network. |

It is the only interactive command, and the only one without `--json`: with
`--json`, or when stdout is not a terminal, it exits 2 and points at
`apic run` and `apic list` instead. All the other global flags (`-C`,
`--env`, `--var`, `--redact`, `--timeout`, `--insecure`, `--no-session`)
work as usual.

## apic list

```
apic list [pattern]
```

Every request in the project in file order: id, method, URL template,
`file:line` and description, grouped by file. A pattern keeps only the
requests whose id, URL, file or description contains it, case-insensitively.

`--json`:

```json
{
  "root": "/abs/path/api",
  "requests": [
    {"id": "login", "name": "login", "method": "POST", "url": "{{baseUrl}}/auth/login",
     "file": "auth.http", "line": 6, "description": "Log in and keep the token",
     "captures": ["token"], "asserts": 1}
  ]
}
```

`name` is omitted for unnamed requests; `id` is then `file.http#N`. `steps`
lists the request's `# @step` phrases when it has any. A pattern filters the
`requests` array; the shape does not change.

## apic describe

```
apic describe <target>
```

Shows one request's method, URL template, headers, body, every variable it
references with the source it resolved from, its captures and asserts, and
whether it is ready to run. Missing variables come first, with the request
that captures them if there is one.

`--json`:

```json
{
  "name": "whoami", "id": "whoami", "file": "auth.http", "line": 16,
  "description": "Uses the token captured by login",
  "method": "GET", "url_template": "{{baseUrl}}/bearer", "url": "https://httpbin.org/bearer",
  "headers": {"Authorization": "Bearer {{token}}"},
  "variables": [
    {"name": "token", "source": "missing", "missing": true, "captured_by": "login"},
    {"name": "baseUrl", "value": "https://httpbin.org", "source": "http-client.env.json [dev]"}
  ],
  "captures": ["email = body.$.email"],
  "asserts": ["status == 200", "body.$.authenticated == true"],
  "auth": "bearer {{token}}",
  "auth_source": "apic.yaml",
  "ready": false
}
```

`auth` and `auth_source` are present when a `# @auth` directive or
`auth.default` applies; see [auth.md](auth.md).

Sources are one of `--var`, `shell APIC_VAR_<name>`, `captured this run`,
`session`, `http-client.private.env.json [env]`, `http-client.env.json [env]`,
`.env`, `<file>:<line> @<name>`, `built-in`, `response reference (flow only)`
or `missing`. Values with `"secret": true` (private env file, `.env`,
session) are shown as `***`.

## apic env

```
apic env
```

Environments found, which env files exist, the current environment, and
every variable in effect with its source. Secrets are masked.

`--json`:

```json
{
  "root": "/abs/path/api",
  "environments": ["dev", "staging"],
  "current": "dev",
  "files": ["http-client.env.json", "http-client.private.env.json", ".env"],
  "variables": [
    {"name": "baseUrl", "value": "https://dev.example.com", "source": "http-client.env.json [dev]"},
    {"name": "password", "value": "***", "source": "http-client.private.env.json [dev]", "secret": true}
  ]
}
```

## apic session

```
apic session
apic session clear [--all]
```

`session` prints captured values per environment from `.apic/session.json`
(in clear text, since this is the one place you may need to see them).
Tokens cached by `# @auth oauth2` and `# @auth exec ttl=` appear as
`$oauth2:<hash>` and `$exec:<hash>` entries with their remaining lifetime.
`clear` forgets the current environment's values, or every environment with
`--all`.

`--json` on `session` prints the raw map `{"<env>": {"<name>": "<value>"}}`.

## apic curl

```
apic curl <target>
```

Prints a POSIX-shell `curl` command with every variable resolved, one flag
per line. Auth is mapped onto curl's `--user` and `--aws-sigv4` flags where
possible (see [auth.md](auth.md)). Fails with exit code 2 and the usual hint if a variable is
missing. Useful for a machine without apic, for a bug report, or for
pasting into a Taskfile.

```sh
apic curl create-user --env staging
apic curl get-user | sh
```

`--json` wraps it as `{"id": "get-user", "command": "curl -sS ..."}`.

## apic validate

```
apic validate
```

Parses every `.http` file and reports errors (bad directive syntax, header
lines that are not headers, unknown selectors, missing body files,
unparsable assertions) and warnings (unknown `# @` directives, duplicate
request names). Exit code 2 when there are errors. Meant for CI and
pre-commit.

`--json`:

```json
{
  "ok": false, "files": 2, "requests": 5,
  "diagnostics": [
    {"path": "users.http", "line": 12, "severity": "error", "message": "assert \"status\": expected `<selector> <op> <value>`"}
  ]
}
```

## apic import

```
apic import <openapi.yaml|openapi.json> [-o <dir>] [--env-name <name>] [--force]
```

Scaffolds `.http` files from an OpenAPI 3 document:

- one file per tag (`pets.http`), operations without tags go to `api.http`;
- one request per operation named from `operationId` in kebab-case, else
  from method and path;
- `# @assert status == <first 2xx code>` (a `2XX` key becomes a range check;
  an operation that declares no 2xx response gets no status assertion);
- path parameters as `{{param}}`; required query and header parameters as
  `{{vars}}`, optional ones as commented lines;
- a JSON body built from the request schema, using examples, defaults and
  enums when present, `{{$uuid}}` and `{{$isoTimestamp}}` for uuid and
  date-time strings;
- `http-client.env.json` with `baseUrl` from the first non-empty server, with server variables replaced by their defaults.

Accepts OpenAPI 3.0 and 3.1 in YAML or JSON. Local `$ref` pointers
(`#/components/...`) are resolved for parameters, request bodies and
schemas; references to other files are not. Path-level parameters are
merged into each operation. Swagger 2.0 documents are rejected with a
message. Existing files are kept unless `--force` is given.

| Flag | Meaning |
|---|---|
| `-o, --out <dir>` | Output directory. Default `.`. |
| `--env-name <name>` | Environment name in the generated env file. Default `dev`. |
| `--force` | Overwrite existing files. |

`--json` prints `{"files": [...], "requests": N, "base_url": "...", "env_file": "...", "skipped": [...]}`.

## apic mcp

```
apic mcp [--dir <path>] [--env <name>]
```

Serves the project over the Model Context Protocol on stdin/stdout until
the client disconnects. Tools: `list_requests`, `describe_request`,
`run_request`, `run_file`, `run_features`, `list_environments`,
`clear_session`. Each `.http` file is a resource. `--env` sets the default environment for calls that do
not pass one. See [agents.md](agents.md).

```sh
claude mcp add api -- apic mcp --dir ./api --env dev
```

## apic demo

```
apic demo [--out <dir>] [--port <port>] [--force]
```

Writes a local example project (`apic.yaml`, `http-client.env.json`,
`http-client.private.env.json`, `auth.http`, `todos.http`) into `--out`, then
starts the bundled fake API and serves until you stop the process.

| Flag | Meaning |
|---|---|
| `-o, --out <dir>` | Output directory for the scaffolded example project. Default `apic-demo`. |
| `--port <port>` | Localhost port to serve on and to write into `http-client.env.json`. Must be `1-65535`. Default `8089`. |
| `--force` | Overwrite existing scaffold files in `--out`. |

`--json` prints one startup object and then keeps serving:

```json
{
  "out": "apic-demo",
  "url": "http://localhost:8089",
  "written": ["apic-demo/apic.yaml"],
  "skipped": [],
  "listening": true
}
```

## apic init

```
apic init [dir] [--base-url URL] [--env NAME] [--force]
```

Writes a working starting point into `dir` (default: the current
directory): `apic.yaml`, `http-client.env.json`, a `http-client.private.env.json`
with mode 0600, `api.http` with two annotated requests, a
`features/smoke.feature`, and `.gitignore` lines for the private env file
and `.apic/`. Existing files are kept unless `--force` is given, and the
`.gitignore` is appended to rather than replaced.

| Flag | Default | Meaning |
|---|---|---|
| `--base-url` | `https://api.example.com` | `baseUrl` for the environment. |
| `--env` | `dev` | Name of the first environment, also written as `env:` in `apic.yaml`. |
| `--force` | off | Overwrite files that already exist. |

`--json` prints `{"out", "env", "written", "skipped"}`.

## apic version, apic completion

`version` prints the version, commit, Go version and platform. With
`--json`:

```json
{"version": "v1.2.3", "commit": "abc1234", "date": "2026-02-01T10:00:00Z",
 "go": "go1.25.7", "os": "linux", "arch": "amd64"}
```

`completion bash|zsh|fish|powershell` prints a shell completion script:

```sh
source <(apic completion bash)
apic completion zsh > "${fpath[1]}/_apic"
```

Completion knows the project: `run`, `describe` and `curl` complete request
ids and `.http` file names, and `--env` completes the environments in
`http-client.env.json`.

## Configuration file

`apic.yaml` in the project root, all keys optional:

```yaml
env: dev        # default --env
dir: requests   # subdirectory to scan for .http files
timeout: 30s    # default request timeout
auth:
  default: aws region=eu-west-2   # applied to requests without # @auth; see auth.md
  allowExec: false                # permit # @auth exec
test:
  paths: [features, smoke.feature] # what `apic test` runs by default
```

## Files apic reads and writes

| File | Purpose |
|---|---|
| `*.http`, `*.rest` | Request definitions. |
| `*.feature` | Gherkin specs for `apic test`. |
| `apic.yaml` | Defaults. |
| `http-client.env.json` | Public per-environment variables. |
| `http-client.private.env.json` | Secret per-environment variables. Gitignore it. |
| `.env` | `KEY=value` lines; lowest precedence after file `@vars`. |
| `.apic/session.json` | Captured values per environment. Written by `run`, cleared by `session clear`. `.apic/.gitignore` is created alongside so it is never committed. |
