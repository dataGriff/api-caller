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

!!! tip "Try it offline"
    `apic demo` writes an example project that includes `features/todos.feature`,
    so `apic test -C apic-demo` runs a real suite against the bundled fake
    API with no network access.

Standard Gherkin structure works as you would expect: `Background`,
`Scenario Outline` with `Examples`, doc strings, data tables and tags.

```gherkin
  Scenario Outline: Any id echoes back
    When I fetch user <id>
    Then the response body "$.args.id" is "<id>"
    Examples:
      | id  |
      | 1   |
      | 999 |
```

## Phrases on requests

Add `# @step` lines to a request. `{name}` placeholders become variables
for that request; each matches a quoted string or a bare word. A placeholder
written in quotes (`I say "{text}"`) matches only quoted text, spaces
included. Placeholder names follow the variable grammar: letters, digits,
`_`, `.` and `-`.

```http
### Create a user
# @name create-user
# @step a user named {name} exists
# @assert status == 201
# @capture userId = body.$.id
POST {{baseUrl}}/users
Content-Type: application/json

{"name": "{{name}}"}

### Fetch a user
# @name get-user
# @step I fetch the user
# @step I fetch user {userId}
GET {{baseUrl}}/users/{{userId}}
Authorization: Bearer {{token}}
```

A phrase does exactly what `I run "<id>"` does: it sends the request,
fails the step if any `# @assert` or `# @capture` on it fails, and makes
captured values available to later steps. The phrase's parameters apply to
that request only; they are not visible to later steps. `apic validate` reports a phrase
that could match the same text as another phrase or as a built-in step
(for example `# @step I run {x}`, or `I do {x}` next to `I {x} foo`, which
both match "I do foo"), since godog would treat such steps as ambiguous.
The check is deliberately cautious and may flag a pair that would not
collide in practice; rephrase one of them. A phrase may declare at most six
parameters. `apic list` and `describe`
show phrases.

Phrases are matched whatever Gherkin keyword introduces them (`Given`,
`When`, `Then`, `And`, `But`).

## Built-in steps

Values in quotes may contain `{{variables}}`, which resolve like any other
apic variable: environment files, `--var`, captures made earlier in the
scenario, and built-ins such as `{{$uuid}}`.

### Setup

| Step | Effect |
|---|---|
| `Given the environment is "staging"` | Switch this scenario to another environment. Variables set by steps, values captured so far, the last response and the session carry over; environment variables come from the new environment. |
| `Given the variable "userId" is "42"` | Set a variable. Highest precedence. |
| `Given the variables:` + table | Set several. Two columns; a `name | value` header row is optional. Rows apply top to bottom, so a value may reference the rows above it. |

### Running requests

| Step | Effect |
|---|---|
| `When I run "get-user"` | Send a request by id (`name`, `file.http#name` or `file.http#3`). |
| `When I run "get-user" with:` + table | Same, with variables set for this request only (rows apply in order, and the table may also supply the target itself). |
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
| `Then the response cookie "sid" exists` / `is "..."` | A cookie the response set (`Set-Cookie`); same operators, plus `exists` and `does not exist`. With the [cookie jar](format.md#cookies) on, each scenario has its own jar; `--use-session` shares the stored one. |
| `Then the response body "$.error" exists` / `does not exist` | Presence. |
| `Then the response body is:` + doc string | Semantic JSON equality: key order and whitespace do not matter, extra keys fail. |
| `Then the response body contains:` + doc string | JSON subset: every key in the doc string must be present and equal; arrays must match in length and order; extra keys in the response are fine. |
| `Then the response time is under 500 ms` | Round-trip time. |

Body paths use the same JSONPath as `# @assert`: `$.a.b`,
`$.items[0].id`, `$.items[-1]`, `$.items[1:3]`, `$..id`,
`$.items[?(@.done == true)].id`, `.length` or `.#` (count), `$` for the
whole body ([all the forms](format.md#body-paths)). The leading `$.` may
be omitted (`"name"` means `$.name`, `"..id"` means `$..id`). Header
steps take `"set-cookie.#"` for how many values a header has and
`"link[1]"` for one of them. A step's value is quoted, so to check a
set of matches, count it or look for one member:
`the response body "items[?(@.done == true)].id" contains "c"`.

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
| `--format html --output report.html` | One self-contained HTML file: summary, every feature, scenario and step with its error, light and dark, no external assets. The thing to attach to a CI run or send to someone who does not read JUnit. |
| `--format cucumber` or `--json` | Cucumber JSON, the format most reporting tools accept. |

Exit codes follow the rest of apic: `0` every scenario passed · `1` at
least one failed on an assertion (undefined steps count as failures and are
never silently skipped) · `2` a definition problem: no features found, a
feature path outside the project root, unknown environment, unknown request,
missing variable or bad phrase · `3` a server could not be reached. When a
run has both kinds of problem, the definition problem (2) wins, then
transport (3), then assertion failures (1). A tag expression that selects no
scenarios exits 0 with zero scenarios reported.

With `--redact`, step failure messages hide expected and actual values,
URLs show masked query values, and every value that came from a secret
source (private env file, `.env`, the session, captures) is masked wherever
the report mentions it, including in step text, tables and doc strings.
In the cucumber JSON report only string values are masked, so numbers and
structure are untouched. The report is written when the run completes rather
than streamed, so a value captured late in the run is masked in earlier lines
too. Values shorter than three characters are not
substituted in report text, since masking a lone digit would corrupt the
report itself; they are still never printed by error messages. Values
passed as `APIC_VAR_*` or `--var` are treated as secrets.

```yaml
# GitHub Actions
- uses: dataGriff/api-caller/setup-apic@v0
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

The MCP server exposes `run_features {paths?, tags?, env?, vars?,
use_session?}`, which returns a summary the agent can act on. Scenarios run
in isolated sessions unless `use_session` is true, which shares
`.apic/session.json` with `run_request` so captures flow between the tools
the way `--use-session` does on the command line:

```json
{"ok": false, "scenarios": 4, "passed": 3, "failed": 1, "skipped": 0, "undefined": 0,
 "failures": [{"feature": "Users", "scenario": "Fetch a user by id",
               "step": "Then the response status is 200",
               "status": "failed", "error": "expected status == 200, got \"404\"\n  GET https://..."}]}
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
