# Lesson 2: Variables and environments

**Goal.** Know every place a `{{variable}}` can come from, and which one
wins when two of them have a value.

## You will need

- apic installed ([lesson 1](01-first-request.md))
- `apic demo` running in another terminal, or start it now:

```sh
apic demo --out apic-demo
```

## Steps

### 1. The file itself

The top of `apic-demo/auth.http` declares two variables:

```http
@user = alice
@clientId = demo-client
```

They are available to every request in that file, and `describe` says so
in its source column:

<!-- learn -->
```sh
apic describe login -C apic-demo
```

```
variables
  ✓ baseUrl   http://localhost:8089  http-client.env.json [local]
  ✓ user      alice  auth.http:1 @user
  ✓ password  ***  http-client.private.env.json [local]
```

Three variables, three sources. This lesson is the story of that column.

### 2. The environment file

`baseUrl` comes from `http-client.env.json`, the same file VS Code's REST
Client and JetBrains read:

<!-- learn -->
```sh
cat apic-demo/http-client.env.json
```

```json
{
  "local": {
    "baseUrl": "http://localhost:8089"
  }
}
```

One environment, `local`, one variable. A real project has `dev`,
`staging` and `prod` here, each with its own `baseUrl`. Values every
environment shares go under `$shared`.

Give the demo a second environment. Move `baseUrl` to `$shared` so both
environments use the same API, and give `staging` a different user (the
first line reads the URL back from the current file, so the port stays
whatever your demo is using):

<!-- learn -->
```sh
base=$(grep -o 'http://[^"]*' apic-demo/http-client.env.json | head -1)
cat > apic-demo/http-client.env.json <<EOF
{
  "\$shared": { "baseUrl": "$base" },
  "local":    {},
  "staging":  { "user": "bob" }
}
EOF
apic env -C apic-demo
```

```
environments: local* staging
files: http-client.env.json, http-client.private.env.json

variables
  ✓ apiKey        ***  http-client.private.env.json [local]
  ✓ baseUrl       http://localhost:8089  http-client.env.json [local]
  ✓ clientSecret  ***  http-client.private.env.json [local]
  ✓ password      ***  http-client.private.env.json [local]
  ✓ token         ***  session
```

`apic env` lists the environments (the `*` marks the current one) and the
variables it sees for it. A `$shared` value is reported under the
environment it was resolved for.

### 3. Switching environment

`--env` picks one:

<!-- learn -->
```sh
apic describe login -C apic-demo --env staging
```

```
variables
  ✗ password  missing  pass --var password=...
  ✓ baseUrl   http://localhost:8089  http-client.env.json [staging]
  ✓ user      bob  http-client.env.json [staging]

╭────────────────────────────────╮
│ not ready missing {{password}} │
╰────────────────────────────────╯
```

Two things happened. `user` is now `bob` from the `staging` block, which
beats the file's `@user = alice`. And `password` is missing, because the
private file only knows `local`. Missing variables come first, with the
way to fix them.

Why did lesson 1 never need `--env`? Because `apic.yaml` sets the default:

<!-- learn -->
```sh
cat apic-demo/apic.yaml
```

```yaml
# Default environment when --env is not given.
env: local
```

### 4. Secrets

The private file has the same shape and holds the values that must not be
committed:

<!-- learn -->
```sh
cat apic-demo/http-client.private.env.json
```

```json
{
  "local": {
    "password": "s3cret",
    "clientSecret": "demo-secret",
    "apiKey": "demo-key"
  }
}
```

`apic init` adds it to `.gitignore`; the demo project is a scratch
directory, so it does not. Anything from this file is shown as `***` in
`describe`, `env` and `run` output, so a secret never lands in a terminal
log by accident.

Give `staging` a password too:

<!-- learn -->
```sh
cat > apic-demo/http-client.private.env.json <<'EOF'
{
  "local":   { "password": "s3cret", "clientSecret": "demo-secret", "apiKey": "demo-key" },
  "staging": { "password": "s3cret", "clientSecret": "demo-secret", "apiKey": "demo-key" }
}
EOF
apic describe login -C apic-demo --env staging
```

```
variables
  ✓ baseUrl   http://localhost:8089  http-client.env.json [staging]
  ✓ user      bob  http-client.env.json [staging]
  ✓ password  ***  http-client.private.env.json [staging]

╭──────────────────────────────╮
│ ready {{baseUrl}}/auth/login │
╰──────────────────────────────╯
```

Ready. The demo API only knows `alice`, so sending this gets a `401`,
which is exactly what a wrong environment looks like:

<!-- learn -->
```sh
apic run login -C apic-demo --env staging || echo "exit $?"
```

```
POST http://localhost:8089/auth/login
401 Unauthorized · 1 ms · 27 B

{
  "error": "bad credentials"
}

✗ status == 200 (actual: 401)
✗ capture token: nothing at body.$.access_token
exit 1
```

The exercise fixes it.

### 5. `.env`, `--var` and the shell

Three more sources, from the least to the most specific. A `.env` file in
the project root, `KEY=value` per line, is read for every environment:

<!-- learn -->
```sh
echo "user=dave" > apic-demo/.env
apic describe login -C apic-demo | grep user
```

```
  ✓ user      ***  .env
```

`dave` beat the file's `alice` (an env file beats `.env`, and `.env` beats
a file variable; `local` has no `user`, so `.env` won). Values from `.env`
are treated as secrets, hence the `***`.

`--var` on the command line beats everything:

<!-- learn -->
```sh
apic describe login -C apic-demo --var user=erin | grep user
```

```
  ✓ user      ***  --var
```

And `APIC_VAR_<name>` in the shell sits just under `--var`, which is how CI
passes a secret in without writing a file:

<!-- learn -->
```sh
APIC_VAR_user=frank apic describe login -C apic-demo | grep user
rm apic-demo/.env
```

```
  ✓ user      ***  shell APIC_VAR_user
```

### 6. The order

Highest first. The first source that has the name wins:

| Source | Example |
|---|---|
| `--var name=value` | `apic run login --var user=erin` |
| `APIC_VAR_name` in the shell | `APIC_VAR_user=frank apic run login` |
| Captured earlier in this run | `# @capture token = …` on a request that ran first |
| The session | `.apic/session.json`, from a previous run |
| `http-client.private.env.json` | the current environment, then `$shared` |
| `http-client.env.json` | the current environment, then `$shared` |
| `.env` | `user=dave` |
| `@name = value` in the file | `@user = alice` |

Lesson 3 is about the two in the middle.

### 7. Built-in values

Some values are generated per request rather than looked up: `{{$uuid}}`,
`{{$isoTimestamp}}`, `{{$timestamp}}`, `{{$randomInt}}` and a few more.
Put one in a body:

<!-- learn -->
```sh
cat > apic-demo/builtins.http <<'EOF'
### A todo with a title that is never the same twice
# @name create-unique-todo
# @assert status == 201
POST {{baseUrl}}/todos
Authorization: Bearer {{token}}
Content-Type: application/json

{"title": "todo {{$uuid}} at {{$isoTimestamp}}"}
EOF
apic run login create-unique-todo -C apic-demo
```

```
POST http://localhost:8089/todos
201 Created · 1 ms · 108 B

{
  "id": "3",
  "title": "todo 0b6c1a2e-7d2f-4c1e-9a58-3f6c2b1e8d40 at 2026-09-20T05:42:21Z",
  "done": false
}

✓ status == 201
```

`login` went first in the same command, so `{{token}}` was captured
before the todo was created. The [cheatsheet](../cheatsheet.md#built-in-placeholders)
lists every built-in.

## Checkpoint

`describe --json` reports the source of each variable. Override `baseUrl`
from the command line and confirm the command line won:

<!-- learn -->
```sh
apic describe whoami -C apic-demo --var baseUrl=http://example.com --json | grep -o '"source": "--var"'
```

```
"source": "--var"
```

(With `jq`: `… --json | jq '.variables[] | select(.name=="baseUrl") | .source'`
prints `"--var"`.)

## Exercise

Make `staging` log in. The demo API accepts only `alice`, so `staging`
needs that user. Then log in with `--env staging` and show that its token
is kept apart from `local`'s.

??? example "Solution"
    Change the user in the `staging` block, then log in there:

    <!-- learn -->
    ```sh
    base=$(grep -o 'http://[^"]*' apic-demo/http-client.env.json | head -1)
    cat > apic-demo/http-client.env.json <<EOF
    {
      "\$shared": { "baseUrl": "$base" },
      "local":    {},
      "staging":  { "user": "alice" }
    }
    EOF
    apic run login -C apic-demo --env staging > /dev/null
    apic session -C apic-demo
    ```

    ```
    local
      ↳ token = mock-token
    staging
      ↳ token = mock-token
    ```

    Captured values are stored per environment, so `local` and `staging`
    each have their own `token`. Two real environments would have two
    different tokens, and `apic run whoami --env staging` would use the
    right one.

## Going further

- [Variables](../format.md#variables) in the format guide
- [Variable precedence](../cheatsheet.md#variable-precedence) on the cheatsheet
- [Why does a variable have the wrong value?](../faq.md#why-does-a-variable-have-the-wrong-value) in the FAQ
- [Two environments, one command](../cookbook.md#two-environments-one-command)

??? note "Episode script"
    **Length.** 10 minutes.

    **Cold open (0:00).** `apic describe login` with the three-source
    variable table on screen. "Same request, three places the values come
    from. By the end you will know all eight, and the order."

    **Talking points.**

    1. File variables: `@user = alice`, and the `auth.http:1 @user` source.
    2. The env file: one environment now, `$shared` and `staging` added,
       `apic env` listing both.
    3. `--env staging`: user changes, password goes missing. `apic.yaml`
       is why `--env` was never needed before.
    4. Secrets: the private file, `***` everywhere, the 401 from a wrong
       environment.
    5. `.env`, `--var`, `APIC_VAR_`: three commands, watch the source
       column change each time.
    6. The order, as a table, read top to bottom once.
    7. Built-ins in a body.
    8. Checkpoint, then the exercise (a second environment with its own
       session).

    **Shot list.** One terminal, 100x30, font size 16, with the env file
    open in an editor split for steps 2 to 4. `docs/learn/tapes/02.tape`
    reproduces the terminal parts. Zoom on the source column after every
    change.

    **Chapters.** `0:00 Where do values come from` · `0:40 File variables`
    · `1:30 The environment file` · `3:00 --env and apic.yaml` · `4:20
    Secrets` · `6:00 .env, --var, the shell` · `7:30 The order` · `8:20
    Built-ins` · `9:00 Checkpoint and exercise`.

    **Description.** From the [episode template](_episode-template.md).
