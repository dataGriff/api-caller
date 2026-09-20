# Lesson 3: Capture, the session and flows

**Goal.** Capture a value from a response, reuse it in any later command,
and run a whole file as a flow.

## You will need

- apic installed ([lesson 1](01-first-request.md))
- `apic demo` running in another terminal, or start it now:

```sh
apic demo --out apic-demo
```

## Steps

### 1. The error you get without a token

Start clean, then ask for the todo list, which needs a token:

<!-- learn -->
```sh
apic session clear -C apic-demo
apic run list-todos -C apic-demo || echo "exit $?"
```

```
session cleared
GET {{baseUrl}}/todos
error: todos.http:8: missing variable
  {{token}}: it is captured by request "login"; run `apic run login` first, or pass --var token=...
exit 2
```

Nothing was sent: exit code `2` means apic stopped before the network. The
error names the variable, the request that provides it, and the two ways
to fix it. Take the first.

### 2. Capture

`login` ends with a directive you saw in lesson 1:

```http
# @capture token = body.$.access_token
```

`body.$.access_token` is a selector: the JSON body, then the path
`access_token`. When the response arrives, whatever is there is stored
under the name `token`:

<!-- learn -->
```sh
apic run login -C apic-demo
```

```
POST http://localhost:8089/auth/login
200 OK · 1 ms · 30 B

{
  "access_token": "mock-token"
}

✓ status == 200
↳ token = mock-token
```

Now the request from step 1 works, in this shell or any other, with no
flag:

<!-- learn -->
```sh
apic run list-todos -C apic-demo --json | grep -o '"ok":true'
```

```
"ok":true
```

### 3. Where it went

<!-- learn -->
```sh
apic session -C apic-demo
```

```
local
  ↳ token = mock-token
```

Captured values live in `.apic/session.json` under the project, keyed by
environment, which is why lesson 2's `staging` token did not mix with
`local`'s:

<!-- learn -->
```sh
cat apic-demo/.apic/session.json
```

```json
{
  "local": {
    "token": "mock-token"
  }
}
```

apic writes a `.gitignore` next to it, so the file never reaches a
commit:

<!-- learn -->
```sh
cat apic-demo/.apic/.gitignore
```

```
# created by apic; session state must not be committed
*
```

### 4. Forgetting

`session clear` empties the current environment (`--all` for every
environment). `--no-session` on a command neither reads nor writes the
file, which is what a CI job or a one-off check wants:

<!-- learn -->
```sh
apic session clear -C apic-demo
apic run login -C apic-demo --no-session > /dev/null
apic session -C apic-demo --json
```

```
session cleared
{}
```

`login` ran and captured its token for that one command, and nothing was
kept. A request can also opt out on its own with `# @no-session`, for a
value that must never be reused.

### 5. Reaching into an earlier response

Within one command, a request can read another's response directly with
`{{<name>.response.<selector>}}`, without a capture. Write a request that
uses `login`'s body:

<!-- learn -->
```sh
cat > apic-demo/chain.http <<'EOF'
### Who am I, reading login's response directly
# @name me-by-ref
# @assert status == 200
GET {{baseUrl}}/me
Authorization: Bearer {{login.response.body.$.access_token}}
EOF
apic run login me-by-ref -C apic-demo --no-session
```

```
…
GET http://localhost:8089/me
200 OK · 0 ms · 30 B

{
  "email": "alice@example.com"
}

✓ status == 200

✓ login      200  1 ms
✓ me-by-ref  200  0 ms

2 passed · 2 requests · 1 ms
```

This only works when `login` ran earlier in the same command, so it suits
a flow in a file more than a request you run alone. `# @capture` is the
one to reach for by default: it survives the command.

### 6. A file is a flow

Naming a file instead of a request runs every request in it, in order,
and prints a summary. `todos.http` creates a todo, reads it, updates it,
deletes it and checks the delete stuck, each request using the id the
first one captured:

<!-- learn -->
```sh
apic run login -C apic-demo > /dev/null
apic run todos.http -C apic-demo || echo "exit $?"
```

```
GET http://localhost:8089/todos
200 OK · 1 ms · 97 B
…
POST http://localhost:8089/todos
201 Created · 0 ms · 45 B
…
✓ status == 201
✓ body.$.title == Write docs
↳ todoId = 3
↳ todoTitle = Write docs
…
GET http://localhost:8089/status/404
404 Not Found · 0 ms · 15 B

✗ status == 200 (actual: 404)

✓ list-todos           200  1 ms
✓ list-open-todos      200  0 ms
✓ create-todo          201  0 ms
✓ get-todo             200  0 ms
✓ update-todo          200  0 ms
✓ get-updated          200  0 ms
✓ delete-todo          204  0 ms
✓ get-deleted          404  0 ms
✓ create-todo-invalid  422  0 ms
✗ not-found            404  0 ms  status == 200 (actual: 404)

1 failed, 9 passed · 10 requests · 1 ms
exit 1
```

Ten requests ran, then the flow stopped: `not-found` is the request that
fails on purpose, and a flow stops at the first failure so a broken step
does not cascade. The exit code is `1`. There are two more requests in the
file; `--keep-going` runs them anyway and reports everything:

<!-- learn -->
```sh
apic run todos.http -C apic-demo --keep-going | tail -15 || true
```

```
✓ list-todos           200  1 ms
✓ list-open-todos      200  0 ms
✓ create-todo          201  0 ms
✓ get-todo             200  0 ms
✓ update-todo          200  0 ms
✓ get-updated          200  0 ms
✓ delete-todo          204  0 ms
✓ get-deleted          404  0 ms
✓ create-todo-invalid  422  0 ms
✗ not-found            404  0 ms  status == 200 (actual: 404)
✓ redirect-followed    200  0 ms
✓ redirect-raw         302  0 ms

1 failed, 11 passed · 12 requests · 1 ms
```

Captures made during a flow are kept like any other, so `todoId` is in
the session now, even though the todo it named was deleted.

### 7. Let the request ask for what it needs

Step 1's error asked you to run `login` yourself. A request can say that
for you. `whoami` in `auth.http` carries:

```http
# @ref login
```

Clear the session and run it alone:

<!-- learn -->
```sh
apic session clear -C apic-demo
apic run whoami -C apic-demo
```

```
session cleared
POST http://localhost:8089/auth/login
200 OK · 1 ms · 30 B

{
  "access_token": "mock-token"
}

✓ status == 200
↳ token = mock-token
↳ ran login first (# @ref)

GET http://localhost:8089/me
200 OK · 0 ms · 30 B

{
  "email": "alice@example.com"
}

✓ status == 200
✓ body.$.email endsWith @example.com
```

`{{token}}` was missing, `login` is the request that captures it, so apic
ran `login` first and then `whoami`. Run `whoami` again and only `whoami`
goes out, because the token is there now. `# @forceRef login` would run it
every time, for a token that must be fresh. `describe` knows about it too:
a missing variable a `# @ref` supplies does not make a request "not ready".

## Checkpoint

After a login, the session holds exactly one `token` for `local`:

<!-- learn -->
```sh
apic run login -C apic-demo > /dev/null && apic session -C apic-demo --json | grep -c '"token"'
```

```
1
```

(With `jq`: `apic session -C apic-demo --json | jq .local.token` prints
`"mock-token"`, and `null` after `apic session clear`.)

## Exercise

Write a two-request file of your own: create a todo, capture its `id`, and
fetch it back by that id. The demo API answers `POST /todos` with the todo
it created, `id` included. Run the file as a flow.

??? example "Solution"
    <!-- learn -->
    ```sh
    cat > apic-demo/mytodo.http <<'EOF'
    ### Create a todo and keep its id
    # @name my-create
    # @ref login
    # @assert status == 201
    # @capture myId = body.$.id
    POST {{baseUrl}}/todos
    Authorization: Bearer {{token}}
    Content-Type: application/json

    {"title": "Finish lesson 3"}

    ### Fetch it back
    # @name my-fetch
    # @assert status == 200
    # @assert body.$.title == Finish lesson 3
    GET {{baseUrl}}/todos/{{myId}}
    Authorization: Bearer {{token}}
    EOF
    apic run mytodo.http -C apic-demo | tail -5
    ```

    ```
    ✓ my-create  201  1 ms
    ✓ my-fetch   200  0 ms

    2 passed · 2 requests · 1 ms
    ```

    `myId` is captured by the first request and used by the second in the
    same flow. The `# @ref login` on the first means the file works from a
    cleared session too. `jobs.http` in the demo project does the same for
    a background job, and lesson 4 comes back to it.

## Going further

- [Log in once and reuse the token everywhere](../cookbook.md#log-in-once-and-reuse-the-token-everywhere)
- [Create, read, update, delete in one flow](../cookbook.md#create-read-update-delete-in-one-flow)
- [Dependencies](../format.md#dependencies) and [flows](../format.md#flows) in the format guide
- [`apic session`](../cli.md#apic-session)

??? note "Episode script"
    **Length.** 10 to 12 minutes.

    **Cold open (0:00).** `apic run list-todos` and the missing-variable
    error. "apic knows which request would fix this. By the end, the
    request will know too."

    **Talking points.**

    1. The error: exit 2, nothing sent, the request that captures it.
    2. `# @capture`, the `↳ token` line, and list-todos working with no
       flag.
    3. `apic session`, the file, the `.gitignore` it ships with.
    4. `session clear`, `--no-session`, `# @no-session`: three ways to
       forget.
    5. Response references: one command, two requests, no capture.
    6. `todos.http` as a flow: stops at not-found, exit 1, then
       `--keep-going` and the twelve-row summary.
    7. `# @ref login`: clear, run whoami, watch login go first. Run it
       again and it does not.
    8. Checkpoint and the exercise (create then fetch by id).

    **Shot list.** One terminal, 100x30, font size 16.
    `docs/learn/tapes/03.tape` reproduces the terminal parts. Zoom on
    `↳ token = mock-token`, on the `✗ not-found` row, and on `↳ ran login
    first (# @ref)`.

    **Chapters.** `0:00 The missing token` · `1:00 Capture` · `2:30 The
    session file` · `4:00 Forgetting` · `5:20 Response references` · `6:30
    A file is a flow` · `8:40 # @ref` · `10:20 Checkpoint and exercise`.

    **Description.** From the [episode template](_episode-template.md).
