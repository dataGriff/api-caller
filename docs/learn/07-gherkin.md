# Lesson 7: Behaviour tests with Gherkin

**Goal.** Write a feature file that exercises the demo API using the
built-in steps and the phrases you declare on requests, and read its
report.

## You will need

- apic installed ([lesson 1](01-first-request.md))
- `apic demo` running in another terminal, or start it now:

```sh
apic demo --out apic-demo
```

## Steps

### 1. Run the suite that ships with the demo

The demo project has a `features/` directory with one file in it.
`apic test` runs every `.feature` file under it:

<!-- learn -->
```sh
apic test -C apic-demo | tail -3
```

```
8 scenarios (8 passed)
47 steps (47 passed)
30.251252ms
```

Open `apic-demo/features/todos.feature`. The first scenario reads:

```gherkin
Feature: Todos
  Background:
    Given I am logged in

  Scenario: The list always has something in it
    When I list the todos
    Then the response status is 200
    And the response body "$.#" is not "0"
```

No step definitions, no Cucumber runtime, no glue code. Every line is
either a step apic ships or a phrase a request in this project declared.
That is the whole language, and it is the thing no other `.http` tool has.

### 2. Read the vocabulary

<!-- learn -->
```sh
apic test --steps -C apic-demo
```

```
built-in steps
  the environment is "<name>"
      switch the scenario to another environment
  the variable "<name>" is "<value>"
      set a variable for later steps
  I run "<request>"
      send a request by id; fails if its # @assert or # @capture fail
  I run "<request>" with: (table name | value)
      send a request with variables
  I run the file "<file.http>"
      send every request in a file, stopping at the first failure
  the response status is <n> / is not <n>
      status code
  the response body "<path>" is "<value>"
      also: is not, contains, starts with, ends with, matches; <path> like $.items[0].id
  the response header "<name>" is "<value>"
      same operators as for the body
  the response body "<path>" exists / does not exist
      presence of a value
  the response body is: (doc string)
      semantic JSON equality
  the response body contains: (doc string)
      JSON subset match
  …

phrases declared in this project
  I am logged in
      runs login (auth.http)
  I list the todos
      runs list-todos (todos.http)
  a todo titled {title} is created
      runs create-todo (todos.http)
  I wait for the job
      runs wait-for-job (jobs.http)
  …
```

Two halves. The built-in steps run requests by id and check the last
response. The phrases are the `# @step` lines on requests in this project,
so the second half is yours to grow.

### 3. Your first feature

The smallest possible scenario runs a request by id and checks the
status:

<!-- learn -->
```sh
cat > apic-demo/features/first.feature <<'EOF'
Feature: Logging in
  Scenario: Good credentials get a token
    When I run "login"
    Then the response status is 200
    And the response body "$.access_token" exists
EOF
apic test features/first.feature -C apic-demo | tail -3
```

```
1 scenarios (1 passed)
3 steps (3 passed)
2.1ms
```

Feature paths are relative to the project root, like everything else
under `-C`. A run step fails when the request cannot be sent, when any
`# @assert` on it fails, or when a `# @capture` finds nothing, so the
assertions you wrote in lesson 4 are already part of every scenario that
runs the request.

### 4. Phrases, tables, doc strings and outlines

`I run "create-todo"` works, but `a todo titled "Learn Gherkin" is
created` reads like behaviour. `create-todo` declares that phrase:

```http
### Create a todo
# @name create-todo
# @step a todo titled {title} is created
```

`{title}` becomes a variable for that request. The built-in
`I run "…" with:` step does the same with a table, for requests without a
phrase. Doc strings compare the whole body, and a `Scenario Outline` runs
once per row of its `Examples`:

<!-- learn -->
```sh
cat > apic-demo/features/lesson7.feature <<'EOF'
Feature: Todos, my way
  Background:
    Given I am logged in

  Scenario: Create a todo and read it back
    When a todo titled "Learn Gherkin" is created
    Then the response status is 201
    And the response body "$.title" is "Learn Gherkin"
    When I fetch the todo
    Then the response body contains:
      """
      {"title": "Learn Gherkin", "done": false}
      """

  Scenario Outline: The starter todos can be fetched by id
    When I run "get-todo" with:
      | todoId    | <id>    |
      | todoTitle | <title> |
    Then the response status is 200
    And the response body "$.title" is "<title>"
    Examples:
      | id | title              |
      | 1  | Buy milk           |
      | 2  | Read the apic docs |
EOF
apic test features/lesson7.feature -C apic-demo | tail -3
```

```
3 scenarios (3 passed)
14 steps (14 passed)
9.8ms
```

`I fetch the todo` used `todoId` and `todoTitle` that `create-todo`
captured a step earlier; captures flow through a scenario the way they
flow through a run. `the response body contains:` is a subset match, so
the `id` the API added does not have to be in the doc string; `the
response body is:` would demand the whole thing. The outline supplied both
variables `get-todo` needs from its table, twice.

### 5. Tags

A tag on a feature or a scenario, and `--tags` to choose:

<!-- learn -->
```sh
cat > apic-demo/features/tagged.feature <<'EOF'
@smoke
Feature: Tagged
  Scenario: The API answers
    When the API is up
    Then the response status is 200

  @slow
  Scenario: The slow route takes its time
    When I run "slow"
    Then the response status is 200
EOF
apic test features/tagged.feature -C apic-demo --tags '@smoke && ~@slow' | tail -3
```

```
1 scenarios (1 passed)
2 steps (2 passed)
1.5ms
```

`,` is or, `&&` is and, `~` is not. A smoke suite in CI is a tag
expression away.

### 6. Every scenario starts clean

Scenarios do not share state with each other or with `apic run`. Each one
begins with an empty in-memory session, and nothing it captures reaches
`.apic/session.json`. Even with a token in your session from earlier,
a scenario that skips the login has none:

<!-- learn -->
```sh
apic run login -C apic-demo > /dev/null
cat > apic-demo/features/iso.feature <<'EOF'
Feature: Isolation
  Scenario: Needs a token but never logged in
    When I list the todos
    Then the response status is 200
EOF
apic test features/iso.feature -C apic-demo --format progress || echo "exit $?"
```

```
F- 2

--- Failed steps:

  Scenario: Needs a token but never logged in # features/iso.feature:2
    When I list the todos # features/iso.feature:3
      Error: todos.http:8: missing variable
  {{token}}: it is captured by request "login"; run `apic run login` first, or pass --var token=...

1 scenarios (1 failed)
2 steps (1 failed, 1 skipped)
2.0ms
error: todos.http:8: missing variable
  {{token}}: it is captured by request "login"; run `apic run login` first, or pass --var token=...
exit 2
```

That is why the shipped feature has `Background: Given I am logged in`: it
runs once per scenario. `--use-session` shares the persisted session
instead, for a slow identity provider you would rather not hit twenty
times:

<!-- learn -->
```sh
apic test features/iso.feature -C apic-demo --use-session | tail -3
rm apic-demo/features/iso.feature
```

```
1 scenarios (1 passed)
2 steps (2 passed)
1.7ms
```

### 7. A step that does not exist

A misspelt or invented step is never silently skipped:

<!-- learn -->
```sh
cat > apic-demo/features/undefined.feature <<'EOF'
Feature: A typo
  Scenario: Undefined
    When I do something apic has never heard of
    Then the response status is 200
EOF
(apic test features/undefined.feature -C apic-demo --format progress || echo "exit $?") | grep -E "scenarios|steps|exit"
rm apic-demo/features/undefined.feature
```

```
1 scenarios (1 undefined)
2 steps (1 undefined, 1 skipped)
You can implement step definitions for undefined steps with these snippets:
exit 1
```

(The snippets it offers are godog's, the Gherkin engine inside apic, and
are not something apic can use.)

An undefined step counts as a failure, and `--steps` is where to look for
the wording that exists. There is no way to add step code, and that is the
point: the vocabulary plus your phrases is the whole language, so a
feature is always readable by someone who knows neither Go nor apic.

### 8. Reports

`--format` picks the report: `pretty` (the default), `progress`, `junit`
for CI dashboards, and `cucumber` (or `--json`) for the tools that read
Cucumber JSON:

<!-- learn -->
```sh
apic test -C apic-demo --format junit --output report.xml
head -2 report.xml
rm report.xml
```

```
<?xml version="1.0" encoding="UTF-8"?>
<testsuites name="apic" tests="14" skipped="0" failures="0" errors="0" time="0.026">
```

Exit codes follow the rest of apic: `0` every scenario passed, `1` a
failure or an undefined step, `2` a definition problem, `3` a server that
could not be reached. The same suite is one MCP call away for an agent:
`run_features` returns the summary and the failures, and `apic test
--steps --json` gives an agent the vocabulary to write a feature with.

## Checkpoint

Every feature in the project passes, yours included:

<!-- learn -->
```sh
apic test -C apic-demo --format progress | grep scenarios
```

```
14 scenarios (14 passed)
```

(The count includes the features you wrote; `apic test -C apic-demo
--json | jq '[.[] | .elements[] | .steps[] | .result.status] | all(. == "passed")'`
prints `true`.)

## Exercise

Write a feature for the jobs resource: submit a job, then wait until it is
done. `jobs.http` already declares the phrases, and `wait-for-job` carries
the `# @retry` from lesson 4, so the waiting is one step.

??? example "Solution"
    <!-- learn -->
    ```sh
    cat > apic-demo/features/jobs.feature <<'EOF'
    Feature: Background jobs
      Background:
        Given I am logged in

      Scenario: A job is done after a couple of polls
        When a job is submitted
        Then the response status is 202
        And the response body "$.state" is "queued"
        When I wait for the job
        Then the response body "$.state" is "done"
    EOF
    apic test features/jobs.feature -C apic-demo | tail -3
    ```

    ```
    1 scenarios (1 passed)
    5 steps (5 passed)
    0.5s
    ```

    `a job is submitted` runs `create-job`, which captures `jobId`. `I wait
    for the job` runs `wait-for-job`, whose `# @retry 5 500ms` sends it
    until `state == done`; the step takes as long as the retries take and
    passes once they do. The half second in the timing is the one wait
    between the first and second attempt.

## Going further

- [Testing with Gherkin](../testing.md), every step and option
- [Describe behaviour, then test it](../cookbook.md#describe-behaviour-then-test-it)
- [`apic test`](../cli.md#apic-test) in the reference

??? note "Episode script"
    **Length.** 12 minutes.

    **Cold open (0:00).** `todos.feature` on screen, then `apic test`
    passing. "Eight scenarios, forty-seven steps, and not one line of step
    code. Here is how."

    **Talking points.**

    1. The shipped suite, and the first scenario read aloud.
    2. `--steps`: the two halves of the language.
    3. The first feature: three lines.
    4. Phrases on requests, `with:` tables, `contains:` doc strings, a
       `Scenario Outline`.
    5. Tags and a smoke expression.
    6. Isolation: the scenario that skipped login, and `--use-session`.
    7. An undefined step, and why there is no step code to add.
    8. Reports, exit codes, the `run_features` MCP tool in one breath.
    9. Checkpoint and the jobs exercise, with `# @retry` doing the
       waiting.

    **Shot list.** One terminal, 100x30, font size 16, the feature file
    open in an editor split for steps 3 to 5.
    `docs/learn/tapes/07.tape` reproduces the terminal parts.

    **Chapters.** `0:00 No step code` · `0:50 The shipped suite` · `2:00
    The vocabulary` · `3:00 Your first feature` · `4:00 Phrases, tables,
    doc strings, outlines` · `6:30 Tags` · `7:20 Every scenario starts
    clean` · `8:40 Undefined steps` · `9:40 Reports` · `10:40 Checkpoint
    and exercise`.

    **Description.** From the [episode template](_episode-template.md).
