# Lesson 11: Real APIs and the Taskfile front door

**Goal.** Run apic against three real services with the three example
projects, keep the credentials out of git, and make `task` the way your
team runs all of it.

## You will need

- apic installed ([lesson 1](01-first-request.md)) and a network
  connection: this lesson leaves the demo API behind
- The repository's `examples/` directory. Clone it, or download it:

```sh
git clone --depth 1 https://github.com/dataGriff/api-caller
cd api-caller
```

- For step 2, a GitHub account; for step 3, a Spotify account. Step 1
  needs neither.
- [Task](https://taskfile.dev) for step 5, and `jq` for the checkpoint.

## Steps

### 1. httpbin: no account, two kinds of auth

[httpbin.org](https://httpbin.org) is a public echo service: it answers
with what you sent, which makes it a good first real target. The
`httpbin` example has three environments, and `dev` points at it:

```sh
apic list -C examples/httpbin
apic run auth.http -C examples/httpbin --env dev
```

```
✓ login        200  312 ms
✓ whoami       200  298 ms
✓ basic-auth   200  301 ms
✓ bearer-auth  200  295 ms

4 passed · 4 requests · 1.2 s
```

The timings are the network's, and the first request is the interesting
one. httpbin has no login, so `login` posts a token it made up itself,
`demo-{{$uuid}}`, and captures it back out of the echo:

```http
### Log in and keep the token
# @name login
# @assert status == 200
# @capture token = body.$.json.token
POST {{baseUrl}}/post
Content-Type: application/json

{"user": "{{user}}", "password": "{{password}}", "token": "demo-{{$uuid}}"}
```

`whoami` then sends it to `/bearer`, which accepts any bearer token and
answers `authenticated: true`, and `basic-auth` lets `# @auth basic`
encode the credentials for `/basic-auth/alice/s3cret`. Nothing here is
special to httpbin; it is the same capture and auth from lessons 3 and 6
on the open internet. The second file has a deliberate expected 404:

```sh
apic run users.http -C examples/httpbin --env dev --keep-going
```

```
✓ get-user     200  305 ms
✓ create-user  200  299 ms
✓ not-found    404  290 ms

3 passed · 3 requests · 0.9 s
```

`not-found` asserts `status == 404`, so it passes: an assertion is about
what you expect, not about 2xx. The password for the `basic-auth`
request is in the example's `http-client.private.env.json`, and that
file is committed on purpose, which is the subject of the next step.

### 2. GitHub: a personal access token in the private file

The `github` example reads one repo through the REST API with a bearer
token. Its private file ships with a placeholder:

```sh
cat examples/github/http-client.private.env.json
```

```json
{
  "dev": { "token": "ghp_replace-with-your-own-token" }
}
```

Two things about that file. The repository's root `.gitignore` ignores
every `http-client.private.env.json`, then re-includes the ones under
`examples/` so the placeholders can be committed; in your own project the
ignore line stands alone, and `apic init` writes it for you. And apic
writes the file with mode `0600` when it creates one, so nobody else on
the machine reads it either.

Make a token: on GitHub, **Settings → Developer settings → Personal
access tokens → Fine-grained tokens**, with no permissions at all. The
three requests read public data; the token only raises the rate limit
and identifies you to `/user`. If you have the `gh` CLI, `gh auth token`
prints one you already have. Put it in the file, then:

```sh
apic run repo.http -C examples/github
```

```
✓ whoami       200  180 ms
✓ get-repo     200  160 ms
✓ list-issues  200  210 ms

3 passed · 3 requests · 0.6 s
```

The `env: dev` in `apic.yaml` means no `--env` is needed. The example
also carries a feature file, and `apic test` runs it against the same
requests:

```sh
apic test -C examples/github
```

```
3 scenarios (3 passed)
9 steps (9 passed)
```

### 3. Spotify: OAuth2 client credentials

The `spotify` example is the OAuth2 flow from [lesson 6](06-authentication.md)
against a real authorization server. Create an app at
[developer.spotify.com/dashboard](https://developer.spotify.com/dashboard)
(any name, any redirect URI; the client-credentials grant never uses it)
and copy its client id and secret into the private file:

```json
{
  "dev": {
    "clientId": "your-client-id",
    "clientSecret": "your-client-secret"
  }
}
```

The request declares everything else:

```http
### Search for an artist
# @name search-artist
# @auth oauth2 tokenUrl={{tokenUrl}} clientId={{clientId}} clientSecret={{clientSecret}} grant=client_credentials clientAuth=basic
# @assert status == 200
# @assert body.$.artists.items[0].name exists
GET {{baseUrl}}/v1/search
    ?q=radiohead
    &type=artist
    &market={{market}}
```

`clientAuth=basic` is the detail that matters for Spotify: it wants the
id and secret in a Basic header on the token request rather than in the
form body, and that one word is the difference between a token and a
401. Run the file:

```sh
apic run search.http -C examples/spotify
```

```
✓ search-artist  200  420 ms
✓ new-releases   200  260 ms

2 passed · 2 requests · 0.7 s
```

The token was fetched once, before the first request, and cached in the
session for its lifetime; `apic session -C examples/spotify` shows it
described (`$oauth2:… = token, expires in 1h0m0s`), never printed. The
second request reused it. When you show this on a screen or paste it into a
ticket, add `--redact`: the URLs stay, and every header value, body and
captured value becomes `***`.

### 4. What each example needs, in one place

| Project | Environment | Private file holds | Where it comes from |
|---|---|---|---|
| `examples/httpbin` | `dev` (httpbin.org), `local`, `staging` | `password` | Committed; any value works |
| `examples/github` | `dev` | `token` | A fine-grained PAT with no permissions, or `gh auth token` |
| `examples/spotify` | `dev` | `clientId`, `clientSecret` | An app in the Spotify developer dashboard |

Every example is checked by CI with `apic validate` and `apic fmt --check`
but never run there: real credentials do not belong in CI for a sample.
Your own project is different, and [lesson 8](08-ci.md) is how its
secrets travel.

### 5. The Taskfile front door

Once the requests are files, the question is how a team runs them
without each person remembering flags. The repository's own `Taskfile.yml`
answers it for the examples:

```sh
task example:github
task example:spotify
```

The pattern behind those tasks is short, and the
[Taskfile guide](../taskfile.md) has the long version. A project's
`Taskfile.yml` looks like this:

```yaml
version: "3"

vars:
  ENV: '{{.ENV | default "dev"}}'

tasks:
  api:
    desc: Run any request, e.g. task api -- get-user --env staging
    dir: api
    cmds: [apic run {{.CLI_ARGS}}]

  login:
    desc: Log in and keep the token for the tasks below
    dir: api
    cmds: [apic run login --env {{.ENV}}]

  smoke:
    desc: Run every request in smoke.http
    dir: api
    cmds: [apic run smoke.http --env {{.ENV}}]

  api:check:
    desc: Validate the request files and run the smoke flow, as CI does
    dir: api
    cmds:
      - apic validate
      - apic run smoke.http --env {{.ENV}} --json --redact
```

Three details carry the design. `dir: api` on every task puts apic in
the same directory each time, so they all share one `.apic/session.json`
and `task login` once serves `task smoke` afterwards. `{{.CLI_ARGS}}`
makes `task api -- whoami --env dev` behave exactly like `apic run`, so
the Taskfile adds a front door without hiding the tool. And
`task api:check` is the one command a newcomer or a CI job runs, with
the secret arriving through the environment:

```sh
APIC_VAR_password="$API_PASSWORD" task api:check ENV=staging
```

Exit codes pass straight through, so a failed assertion fails the task.

## Checkpoint

The Spotify flow runs green with your own credentials:

```sh
apic run search.http -C examples/spotify --json | jq -s 'map(.ok) | all'
```

```
true
```

If you skipped Spotify, the GitHub file is the same check:
`apic run repo.http -C examples/github --json | jq -s 'map(.ok) | all'`.

## Exercise

Add a fourth example project for an API you use, with one flow and one
feature. Start it with `apic init`, which writes the config, the env
files, the `.gitignore` lines and a first request; point it at the demo
API first if the real one needs credentials you do not have to hand.

??? example "Solution"
    <!-- learn -->
    ```sh
    apic init my-api --base-url http://localhost:8089 --env local
    apic run ping -C my-api | tail -1
    ```

    ```
    wrote my-api/apic.yaml
    wrote my-api/http-client.env.json
    wrote my-api/http-client.private.env.json
    wrote my-api/api.http
    wrote my-api/features/smoke.feature
    wrote my-api/.gitignore
    ✓ status == 200
    ```

    `api.http` has `ping` and a `get-thing` that needs an `apiKey` from the
    private file and an `id`, and `features/smoke.feature` has one
    scenario on `ping`. Replace `baseUrl` with the real API, rewrite
    `get-thing` as its first real request, give the flow a second
    request that uses a capture from the first, and add a scenario for
    it. `apic validate`, `apic run api.http` and `apic test` are the three
    checks; when all three pass, the project is ready for a
    `Taskfile.yml` like the one in step 5.

## Going further

- [The example projects](https://github.com/dataGriff/api-caller/tree/main/examples),
  each with a README-level note in its files
- [Using apic with Taskfile](../taskfile.md): every pattern, including
  piping output between tasks and installing apic from a task
- [Cookbook](../cookbook.md), for the recipes these examples are
  simplified from
- [Authentication](../auth.md), for the OAuth2 grants and options beyond
  client credentials

??? note "Episode script"
    **Length.** 10 minutes; live calls with credentials blurred, `--redact`
    on screen where possible.

    **Cold open (0:00).** `apic run search.http -C examples/spotify`,
    two green lines against a real API, no code. "Everything so far was
    the demo. Now the internet."

    **Talking points.**

    1. httpbin: the made-up token captured out of the echo, `/bearer`,
       `# @auth basic`; the expected 404.
    2. The private file: the placeholder, the `.gitignore` rule and the
       re-include for the examples, mode 0600.
    3. GitHub: a fine-grained token with no permissions; `repo.http`;
       `apic test` on the feature.
    4. Spotify: the app, `clientAuth=basic`, the cached token described in
       `apic session`, `--redact` on screen.
    5. The table of what each example needs; why CI validates but never
       runs them.
    6. The Taskfile: `dir:`, `CLI_ARGS`, `api:check`, the secret through
       the environment.
    7. Checkpoint, then the fourth example exercise with `apic init`.

    **Shot list.** One terminal for the runs, with the private files
    opened in an editor and blurred; the GitHub token page and the
    Spotify dashboard prerecorded. No tape for this lesson (real network).

    **Chapters.** `0:00 Real APIs` · `0:40 httpbin` · `2:40 The private
    file` · `3:40 GitHub` · `5:10 Spotify and OAuth2` · `7:00 What each
    needs` · `7:40 The Taskfile front door` · `9:10 Checkpoint and
    exercise`.

    **Description.** From the [episode template](_episode-template.md).
