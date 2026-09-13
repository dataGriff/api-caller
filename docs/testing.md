# Testing with Gherkin features

`apic test` runs Cucumber-style `.feature` files against the requests in
your `.http` files. There is no Cucumber runtime to install and no step
code to write: apic ships a fixed step vocabulary, and each request can
declare the phrases that run it. The `.http` files are the API layer; the
features describe behaviour on top of it; humans, CI and agents all run the
same files with one binary.

```gherkin
Feature: Users
  Background:
    Given I am logged in

  Scenario: Fetch a user by id
    Given a user named "alice" exists
    When I fetch the user
    Then the response status is 200
    And the response body "$.name" is "alice"
    And the response body "$.id" is "{{userId}}"
```

```sh
apic test                                   # features/ under the project root
apic test features/users.feature --env staging --tags "@smoke && ~@slow"
apic test --format junit --output report.xml
apic test --json | jq                       # cucumber JSON report
apic test --steps                           # the vocabulary and your phrases
```

## Phrases on requests

Add `# @step` lines to a request. `{name}` placeholders become variables
for that request; each matches a quoted string or a bare word.

```http
### Create a user
# @name create-user
# @step a user named {name} exists
# @step a {role} named {name} exists
# @assert status == 201
# @capture userId = body.$.id
POST {{baseUrl}}/users
Content-Type: application/json

{"name": "{{name}}", "role": "{{role}}"}

### Fetch a user
# @name get-user
# @step I fetch the user
# @step I fetch user {userId}
GET {{baseUrl}}/users/{{userId}}
Authorization: Bearer {{token}}
```

A phrase does exactly what `I run "<id>"` does: it sends the request,
fails the step if any `# @assert` or `# @capture` on it fails, and makes
captured values available to later steps. `apic validate` reports a phrase
declared on two requests, and `apic list` and `describe` show them.

Phrases are matched whatever Gherkin keyword introduces them (`Given`,
`When`, `Then`, `And`, `But`).

## Built-in steps

Values in quotes may contain `{{variables}}`, which resolve like any other
apic variable: environment files, `--var`, captures made earlier in the
scenario, and built-ins such as `{{$uuid}}`.

### Setup

| Step | Effect |
|---|---|
| `Given the environment is "staging"` | Switch this scenario to another environment (fresh state). |
| `Given the variable "userId" is "42"` | Set a variable. Highest precedence. |
| `Given the variables:` + table | Set several. Two columns; a `name | value` header row is optional. |

### Running requests

| Step | Effect |
|---|---|
| `When I run "get-user"` | Send a request by id (`name`, `file.http#name` or `file.http#3`). |
| `When I run "get-user" with:` + table | Same, with variables set first. |
| `When I run the file "smoke.http"` | Send every request in the file in order; stops at the first failure. |

A run step fails when the request cannot be sent (missing variable,
network), when any `# @assert` on it fails, or when a `# @capture` finds
nothing. The error shows the request line, status, failed assertions and a
body excerpt.

### Checking the response

The "response" is always the last request sent in the scenario.

| Step | Effect |
|---|---|
| `Then the response status is 200` / `is not 500` | Status code. |
| `Then the response is successful` | 2xx. Also `a client error` (4xx), `a server error` (5xx). |
| `Then the response body "$.items[0].id" is "7"` | Compare a JSON value. Operators: `is`, `equals`, `is not`, `contains`, `starts with`, `ends with`, `matches` (regular expression). Numbers compare numerically. |
| `Then the response header "content-type" contains "json"` | Same operators on a header (case-insensitive name). |
| `Then the response body "$.error" exists` / `does not exist` | Presence. |
| `Then the response body is:` + doc string | Semantic JSON equality: key order and whitespace do not matter, extra keys fail. |
| `Then the response body contains:` + doc string | JSON subset: every key in the doc string must be present and equal; arrays must match in length and order; extra keys in the response are fine. |
| `Then the response time is under 500 ms` | Round-trip time. |

Body paths use the same selector syntax as `# @assert`: `$.a.b`,
`$.items[0].id`, `$.items.#` (count), `$` for the whole body. The leading
`$.` may be omitted (`"name"` means `$.name`).

### Capturing

| Step | Effect |
|---|---|
| `When I capture the response body "$.id" as "userId"` | Store a value for later steps. Also `header`. |

`# @capture` lines on requests do this automatically.

## Scenario state

Every scenario starts from a clean, in-memory session: nothing captured in
an earlier scenario or by `apic run` is visible, and nothing a test does is
written to `.apic/session.json`. That keeps scenarios independent and
means a `Background` that logs in runs once per scenario. Pass
`--use-session` to share the persisted session instead, for example to
avoid repeated logins against a slow identity provider.

OAuth2 tokens obtained through `# @auth` are cached within a scenario.

## Reports and CI

| Flag | Output |
|---|---|
| `--format pretty` (default) | Coloured, readable; colour off when not a terminal or `NO_COLOR` is set. |
| `--format progress` | One character per step. |
| `--format junit --output report.xml` | JUnit XML for CI dashboards. |
| `--format cucumber` or `--json` | Cucumber JSON, the format most reporting tools accept. |

Exit codes follow the rest of apic: `0` every scenario passed · `1` at
least one failed on an assertion (undefined steps count as failures and are
never silently skipped) · `2` a definition problem: no features found, a
feature path outside the project root, unknown environment, unknown request,
missing variable or bad phrase · `3` a server could not be reached. When a
run has both kinds of problem, the definition problem (2) wins, then
transport (3), then assertion failures (1). A tag expression that selects no
scenarios exits 0 with zero scenarios reported.

With `--redact`, step failure messages hide expected and actual values, and
every value that came from a secret source (private env file, `.env`, the
session, captures) is masked wherever the report mentions it, including in
step text, tables and doc strings.

```yaml
# GitHub Actions
- run: apic test -C api --env staging --format junit --output report.xml --redact
  env:
    APIC_VAR_password: ${{ secrets.API_PASSWORD }}
- uses: dorny/test-reporter@v1
  if: always()
  with:
    name: API features
    path: report.xml
    reporter: java-junit
```

## For agents

The MCP server exposes `run_features {paths?, tags?, env?, vars?}`, which
returns a summary the agent can act on:

```json
{"ok": false, "scenarios": 4, "passed": 3, "failed": 1, "undefined": 0,
 "failures": [{"feature": "Users", "scenario": "Fetch a user by id",
               "step": "Then the response status is 200",
               "status": "failed", "error": "expected status == 200, got \"404\"\n  GET https://…"}]}
```

From a shell, `apic test --json` gives the full cucumber report and
`apic test --steps --json` lists the vocabulary and phrases, so an agent
can write features using only steps that exist.

## Keeping your existing Cucumber

If your team already runs cucumber-js, Cucumber-JVM or behave, apic still
helps as the transport: a step definition can call
`apic run <id> --json --var k=v` and assert on the JSON, so the request
definitions live in one place. The built-in runner is simpler when you can
adopt it, because it removes the step code and the runtime.

## Limits

No custom step code and no scripting; the vocabulary and phrases are the
whole language. Scenarios run one at a time. Ad-hoc requests without a
`.http` entry are deliberately not supported: the catalogue is the point.
