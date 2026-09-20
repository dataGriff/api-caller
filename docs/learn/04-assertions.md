# Lesson 4: Assertions, validation and polling

**Goal.** Assert on the status, headers and JSON of a response, read a
failure, validate a project before running it, and wait for an API to
reach a state.

## You will need

- apic installed ([lesson 1](01-first-request.md))
- `apic demo` running in another terminal, or start it now:

```sh
apic demo --out apic-demo
```

- A token in the session, so the requests that need one have it:

<!-- learn -->
```sh
apic run login -C apic-demo > /dev/null
```

## Steps

### 1. A request that checks itself

Every `# @assert` line is a check on the response. `health` in
`explore.http` has three:

```http
# @assert status == 200
# @assert body.$.status == ok
# @assert body.$.version exists
```

<!-- learn -->
```sh
apic run health -C apic-demo
```

```
GET http://localhost:8089/health
200 OK · 1 ms · 73 B

{
  "service": "apic-demo",
  "status": "ok",
  "uptime_seconds": 12,
  "version": "v0.2.0"
}

✓ status == 200
✓ body.$.status == ok
✓ body.$.version exists
```

Each assertion is `selector operator value`. The left side picks something
from the response, the operator compares, the right side is what you
expect (or nothing, for `exists`).

### 2. Reading a failure

`not-found` asks for a 404 and asserts a 200, on purpose:

<!-- learn -->
```sh
apic run not-found -C apic-demo || echo "exit $?"
```

```
GET http://localhost:8089/status/404
404 Not Found · 1 ms · 15 B

{
  "status": 404
}

✗ status == 200 (actual: 404)
exit 1
```

The `✗` line shows the expression and what the selector actually found.
Exit code `1` is what a script or CI reads. Nothing else changes: the
response is still printed, and any capture on the request still happens.

### 3. Selectors

The left side can be any of these:

| Selector | Picks |
|---|---|
| `status` | The status code, `404` |
| `statusText` | The reason, `Not Found` |
| `duration` | Milliseconds the request took |
| `header.<name>` | A response header, case-insensitive |
| `body` | The raw body as text |
| `body.$` | The body as JSON |
| `body.$.a.b` | A path into it |
| `body.$[0]` | An array element |
| `body.$.items.#` | How many elements `items` has |

`list-open-todos` uses four of them on one response:

```http
# @assert status == 200
# @assert header.x-total-count >= 1
# @assert body.$.# <= 5
# @assert body.$[0].done == false
```

<!-- learn -->
```sh
apic run list-open-todos -C apic-demo -v
```

```
GET http://localhost:8089/todos?done=false&page=1&limit=5
Authorization: ***

200 OK · 1 ms · 90 B
content-type: application/json
x-page: 1
x-total-count: 2

[
  {
    "id": "1",
    "title": "Buy milk",
    "done": false
  }
]

✓ status == 200
✓ header.x-total-count >= 1
✓ body.$.# <= 5
✓ body.$[0].done == false
```

`-v` shows the headers, so you can see `x-total-count` the second
assertion read. `body.$.#` counted the array. `body.$[0].done` walked
into its first element.

The body does not have to be JSON. `daily-report` fetches CSV and asserts
on the header and the raw text:

```http
# @assert header.content-type contains text/csv
# @assert body startsWith date,total,done,open
```

### 4. Operators

| Operator | Meaning |
|---|---|
| `==`, `!=` | Equal, not equal. Numbers compare as numbers, so `200 == 200.0` |
| `<`, `<=`, `>`, `>=` | Numeric only; a non-number on either side fails with a reason |
| `contains`, `startsWith`, `endsWith` | Substring checks on the text |
| `matches` | A regular expression, `body.$.email matches ^[^@]+@example\.com$` |
| `exists`, `not exists` | The selector found something, or found nothing; no right side |

The right side can hold a variable: `body.$.id == {{userId}}` compares
against whatever `userId` resolves to.

### 5. Errors are responses too

An API's error shapes deserve assertions as much as its happy path. The
demo answers an empty title with a 422 and a field map, and
`create-todo-invalid` pins that down:

```http
# @assert status == 422
# @assert body.$.error == "validation failed"
# @assert body.$.fields.title exists
```

<!-- learn -->
```sh
apic run create-todo-invalid -C apic-demo
```

```
POST http://localhost:8089/todos
422 Unprocessable Entity · 1 ms · 69 B

{
  "error": "validation failed",
  "fields": {
    "title": "must not be empty"
  }
}

✓ status == 422
✓ body.$.error == validation failed
✓ body.$.fields.title exists
```

Quotes around a value are optional and stripped, so `"validation failed"`
and `validation failed` mean the same thing; quote when the value has
leading or trailing spaces.

In `apic ui`, the <kbd>3</kbd> key opens the checks tab, which lists every
assertion of the selected request with its actual and expected values side
by side, and lesson 5 spends time there.

### 6. Catching mistakes before a run

An assertion with a typo in its selector fails every time, and a name
used twice makes `apic run <name>` ambiguous. `apic validate` finds both
without sending anything. Break a file on purpose:

<!-- learn -->
```sh
cat > apic-demo/broken.http <<'EOF'
### The same name as a request in explore.http
# @name health
# @assert stauts == 200
GET {{baseUrl}}/health
EOF
apic validate -C apic-demo || echo "exit $?"
```

```
broken.http:2:9: warning: request name "health" is also used elsewhere; `apic run health` will be ambiguous (duplicate-name)
broken.http:3:11: error: assert "stauts == 200": unknown selector "stauts" (unknown-selector)
explore.http:2:9: warning: request name "health" is also used elsewhere; `apic run health` will be ambiguous (duplicate-name)
✗ 5 files, 25 requests, 1 error, 2 warnings
exit 2
```

Each line is `file:line:column`, the severity, the message and a code in
parentheses. Editors and CI use the columns and codes: `--format github`
turns them into annotations on a pull request, `--format sarif` feeds a
code-scanning tool. Errors make `validate` exit `2`; warnings alone do not.

Fix it by deleting the file, and confirm:

<!-- learn -->
```sh
rm apic-demo/broken.http
apic validate -C apic-demo
```

```
✓ 4 files, 24 requests, no problems
```

(The counts include any files you added in the exercises.)

### 7. Waiting for a state

Some assertions describe where an API is going, not where it is. The
demo's job API moves a job from `queued` to `running` to `done`, one step
per poll. Without help, you would loop in the shell:

```sh
until apic run get-job -C apic-demo --json | grep -q '"state":"done"'; do sleep 1; done
```

`wait-for-job` in `jobs.http` says the same thing in one line:

```http
# @retry 5 500ms
# @assert status == 200
# @assert body.$.state == done
```

Submit a job, then run it:

<!-- learn -->
```sh
apic run create-job -C apic-demo > /dev/null
apic run wait-for-job -C apic-demo
```

```
attempt 1/5 · body.$.state == done: got "running"
GET http://localhost:8089/jobs/1
200 OK · 1 ms · 26 B · 2 attempts

{
  "id": "1",
  "state": "done"
}

✓ status == 200
✓ body.$.state == done
```

The first attempt found `running`, apic printed why and waited half a
second, and the second attempt passed. Up to five would have been tried,
and if none passed the last one would be reported as the failure, exit
code `1`. A transport error counts as a failed attempt too. `--retry
"5 500ms"` applies a policy to requests without their own, and
`--no-retry` switches every policy off.

### 8. Assertions for programs

`--json` carries every assertion with its outcome, so a script can say
which one failed rather than only that one did:

<!-- learn -->
```sh
apic run not-found -C apic-demo --json || true
```

```json
{"ok":false,"request":{"name":"not-found",…},"response":{"status":404,…},
 "asserts":[{"expr":"status == 200","pass":false,"actual":"404","expected":"200"}]}
```

`ok` is the one field to read first; `asserts[]` has the rest. A retried
request adds `"attempts": 2`.

## Checkpoint

Run the todo file with `--keep-going` and count the requests that failed.
Exactly one does, the deliberate `not-found`:

<!-- learn -->
```sh
apic run todos.http -C apic-demo --keep-going --json | grep -c '"ok":false'
```

```
1
```

(With `jq`: `… --json | jq -s 'map(select(.ok | not)) | length'` prints
`1`, and `jq -s 'map(.ok) | all'` prints `false` because of that one.)

## Exercise

Write a request for the second page of todos, two per page
(`GET {{baseUrl}}/todos?page=1&limit=2`), with three assertions: the number
of todos on the page, a response header, and a value inside the first
todo.

??? example "Solution"
    <!-- learn -->
    ```sh
    cat > apic-demo/paging.http <<'EOF'
    ### The first page of todos, two at a time
    # @name first-page
    # @ref login
    # @assert status == 200
    # @assert body.$.# <= 2
    # @assert header.x-page == 1
    # @assert body.$[0].id exists
    GET {{baseUrl}}/todos
        ?page=1
        &limit=2
    Authorization: Bearer {{token}}
    EOF
    apic run first-page -C apic-demo | tail -4
    ```

    ```
    ✓ status == 200
    ✓ body.$.# <= 2
    ✓ header.x-page == 1
    ✓ body.$[0].id exists
    ```

    `body.$.#` counts the array, `header.x-page` reads the header the API
    sets for the page you asked for, and `body.$[0].id` walks into the
    first element. The query string is written on continuation lines,
    which is easier to read than one long URL.

## Going further

- [Selectors](../format.md#selectors) and [assertion operators](../format.md#assertion-operators)
- [Retries](../format.md#retries) in the format guide
- [`apic validate`](../cli.md#apic-validate), its formats and every diagnostic code
- [Poll until something is ready](../cookbook.md#poll-until-something-is-ready)

??? note "Episode script"
    **Length.** 12 minutes.

    **Cold open (0:00).** `apic run not-found` and the `✗ status == 200
    (actual: 404)` line. "One line in the file, and the request is a test.
    This lesson is every kind of line you can write there."

    **Talking points.**

    1. `health`: three assertions, three green checks, the shape
       `selector operator value`.
    2. The failure: actual versus expected, exit 1.
    3. Selectors on `list-open-todos` with `-v`: status, a header, a
       count, a path into the first element. CSV with `body startsWith`.
    4. Operators, as a table, with the numeric rule.
    5. Errors are responses: the 422 and its field map.
    6. `apic validate` on a broken file: the columns, the codes, exit 2;
       delete it, `no problems`.
    7. `# @retry` on `wait-for-job`: the attempt line, `2 attempts`, and
       the shell loop it replaces.
    8. `--json` with the asserts array; the checkpoint; the exercise.

    **Shot list.** One terminal, 100x30, font size 16.
    `docs/learn/tapes/04.tape` reproduces the terminal parts. Zoom on the
    `✗` line in step 2, the `3:11` column in step 6, and `attempt 1/5` in
    step 7.

    **Chapters.** `0:00 One line makes it a test` · `0:50 A request that
    checks itself` · `1:50 Reading a failure` · `3:00 Selectors` · `5:00
    Operators` · `6:00 Errors are responses too` · `7:20 validate` ·
    `9:00 Waiting with # @retry` · `10:40 --json` · `11:20 Checkpoint and
    exercise`.

    **Description.** From the [episode template](_episode-template.md).
