# Lesson 1: Install apic and send your first request

**Goal.** Install apic, run a request from a `.http` file against the demo
API, and read the file that described it.

## You will need

- A terminal. Nothing else: this lesson installs apic and the demo API
  ships inside it.

## Steps

### 1. Install apic

One static binary, no runtime. Pick your platform:

=== "Linux / macOS"

    ```sh
    curl -fsSL https://raw.githubusercontent.com/dataGriff/api-caller/main/install.sh | sh
    ```

    The installer puts `apic` in `~/.local/bin` (or `/usr/local/bin` when
    that is writable), verifies the release checksum, and prints where it
    landed. If the directory is not on your `PATH`, it says so.

=== "Windows"

    Download the zip for your architecture from
    [GitHub Releases](https://github.com/dataGriff/api-caller/releases/latest),
    unzip it, and put `apic.exe` somewhere on your `PATH`. `winget`, Scoop
    and a PowerShell installer are on the way; the release page says when.

=== "Go"

    ```sh
    go install github.com/dataGriff/api-caller/cmd/apic@latest   # Go 1.25 or newer
    ```

    This builds from source into `$(go env GOPATH)/bin`.

In GitHub Actions, the [`setup-apic` action](../cookbook.md#a-smoke-test-in-ci-that-never-leaks-secrets) installs
it in one step; lesson 8 uses it.

Check it answers:

<!-- learn -->
```sh
apic version
```

```
apic v0.2.0 · 1a2b3c4 · go1.25.7 linux/amd64
apic is epic · https://datagriff.github.io/api-caller/
```

Your version and platform will differ; the shape is what matters.

### 2. Thirty seconds in the UI

Before any files, see the tool move. `apic ui --demo` serves a fake API
inside the process and opens the terminal UI on an example project that
targets it:

```sh
apic ui --demo
```

Four keys are enough for now:

| Key | What it does |
|---|---|
| <kbd>enter</kbd> | Send the request under the cursor |
| <kbd>f</kbd> | Run the whole file the cursor is in, as a flow |
| <kbd>?</kbd> | Every key |
| <kbd>q</kbd> | Quit |

Press <kbd>enter</kbd> on `login`. The right pane shows the status, the
body and a green check. Press <kbd>j</kbd> to move down to `whoami` and
<kbd>enter</kbd> again: it uses the token `login` just captured. Press
<kbd>q</kbd>. Lesson 5 is the full tour; the rest of this course works from
the command line, where every step is a command you can copy.

### 3. Start the demo API and look at the project

`apic demo` writes the same example project to a directory and serves the
API it targets. Leave it running in one terminal:

```sh
apic demo --out apic-demo
```

```
apic demo: serving http://localhost:8089, project written to apic-demo
try: apic run login whoami -C apic-demo --env local
```

In a second terminal, list what the project contains. `-C` tells every apic
command which directory the project is in:

<!-- learn -->
```sh
apic list -C apic-demo
```

```
ID              METHOD  URL                        LINE  DESCRIPTION                                                               PHRASES
auth.http
login           POST    {{baseUrl}}/auth/login     :9    Log in and keep the token                                                 I am logged in
whoami          GET     {{baseUrl}}/me             :20   Who am I, using the token captured by login (which runs first if needed)  I ask who I am
basic-auth      GET     {{baseUrl}}/basic-auth/{{user}}/{{password}}  :28   Basic auth, encoded for you
…
todos.http
list-todos      GET     {{baseUrl}}/todos          :8    List todos                                                                I list the todos
…

24 requests in 4 files · apic describe <id> · apic run <id> · apic ui
```

Four files, twenty-four requests, each with an id you can type. The
`PHRASES` column is for lesson 7.

### 4. Describe a request before sending it

`describe` shows everything apic knows about a request without sending it:
the method and URL, every variable it needs and where each one comes from,
and whether it is ready.

<!-- learn -->
```sh
apic describe login -C apic-demo
```

```
POST {{baseUrl}}/auth/login
Log in and keep the token
file: auth.http:9

headers
  Content-Type: application/json

body
  {"user": "{{user}}", "password": "{{password}}"}

variables
  ✓ baseUrl   http://localhost:8089  http-client.env.json [local]
  ✓ user      alice  auth.http:1 @user
  ✓ password  ***  http-client.private.env.json [local]

captures
  ↳ token = body.$.access_token

asserts
  status == 200

╭──────────────────────────────╮
│ ready {{baseUrl}}/auth/login │
╰──────────────────────────────╯
```

Three variables, three different sources, and a password shown as `***`
because it came from the private file. Lesson 2 is about that column.

### 5. Send it

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

Status, timing, size, the body, the assertion that passed, and the value
that was captured. That last line is the one to remember: `token` is now
stored, and every later request that says `{{token}}` will find it.

### 6. Read the file

Open `apic-demo/auth.http` in any editor. The first request is:

```http
@user = alice
@clientId = demo-client

### Log in and keep the token
# @name login
# @step I am logged in
# @assert status == 200
# @capture token = body.$.access_token
POST {{baseUrl}}/auth/login
Content-Type: application/json

{"user": "{{user}}", "password": "{{password}}"}
```

Line by line:

| Line | What it is |
|---|---|
| `@user = alice` | A file variable, available to every request in the file as `{{user}}`. |
| `### Log in and keep the token` | The separator that starts a request, and its title. |
| `# @name login` | The id. Without it the request would be `auth.http#1`. |
| `# @step I am logged in` | A Gherkin phrase for lesson 7. Ignore it for now. |
| `# @assert status == 200` | A check on the response. Lesson 4. |
| `# @capture token = body.$.access_token` | Keep a value from the response. Lesson 3. |
| `POST {{baseUrl}}/auth/login` | The request line: method and URL, with a variable. |
| `Content-Type: application/json` | Headers, until the first blank line. |
| `{"user": …}` | The body, until the next `###`. |

Every line starting with `#` is a comment to any other tool that reads
`.http` files, so this file still works in VS Code and JetBrains. The
`# @` lines are apic's directives, and the
[cheatsheet](../cheatsheet.md#directives) lists every one.

### 7. Exit codes

Scripts and CI read the exit code before anything else. `0` means every
assertion passed:

<!-- learn -->
```sh
apic run login -C apic-demo > /dev/null; echo "exit $?"
```

```
exit 0
```

The demo project has one request that fails its assertion on purpose:

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

`1` is an assertion that failed. `2` is a mistake before anything was sent
(a missing variable, a request id that does not exist) and `3` is the
network. The [exit codes](../cli.md#exit-codes) table is the contract.

## Checkpoint

`--json` prints one JSON object per request. If this prints `"ok":true`,
apic is installed, the demo is up, and you have sent a request through
both:

<!-- learn -->
```sh
apic run login -C apic-demo --json | grep -o '"ok":true'
```

```
"ok":true
```

(With `jq` installed, `apic run login -C apic-demo --json | jq .ok` prints
`true`.)

## Exercise

The demo API has a `GET /health` endpoint that needs no token. Add a
request for it, named `ping`, to `apic-demo/auth.http` (or to a new file
in the same directory), and run it with `apic run ping -C apic-demo`.

??? example "Solution"
    Appending a request to a new file keeps the shipped one clean:

    <!-- learn -->
    ```sh
    cat >> apic-demo/mine.http <<'EOF'
    ### Is the API up?
    # @name ping
    # @assert status == 200
    GET {{baseUrl}}/health
    EOF
    apic run ping -C apic-demo
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
    ```

    `{{baseUrl}}` resolves in any file in the project because it comes from
    `http-client.env.json`, not from the file. The `###` line, `# @name`
    and the request line are the whole request; headers and a body are
    only there when the request needs them.

## Going further

- [Getting started](../getting-started.md), the same steps for a real project
- [The terminal UI](../tui.md), every key
- [`apic run`](../cli.md#apic-run) and the [exit codes](../cli.md#exit-codes)

??? note "Episode script"
    **Length.** 8 to 10 minutes.

    **Cold open (0:00).** An empty terminal. "In ten minutes you will have
    sent a request from a text file, and understood every line of the
    file."

    **Talking points.**

    1. Install: thirty seconds per platform, cut short. `apic version`
       proves it.
    2. `apic ui --demo`: enter on login, j, enter on whoami. "That second
       request used the token the first one captured. Hold that thought
       for lesson 3."
    3. `apic demo` in terminal one; `list`, `describe login`, `run login`
       in terminal two. Point at the three variable sources and the `***`.
    4. The file, line by line, with the table on screen.
    5. Exit codes: 0, then the deliberate 404 and its `✗` line, then the
       `1`.
    6. The checkpoint, and the `--json` line it comes from.

    **Shot list.** Per-OS install as three short picture-in-picture clips;
    then one terminal, 100x30, font size 16. `docs/learn/tapes/01.tape`
    reproduces the terminal parts. Zoom on `↳ token = mock-token` after
    step 5 and on `exit 1` in step 7.

    **Chapters.** `0:00 Why` · `0:30 Install` · `1:40 The UI in thirty
    seconds` · `3:00 list, describe, run` · `5:20 The file, line by line`
    · `7:10 Exit codes` · `8:10 Checkpoint` · `8:40 Exercise and lesson 2`.

    **Description.** From the [episode template](_episode-template.md).
