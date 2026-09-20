# Lesson 0: What apic is and why

**Goal.** Understand what apic is, what it is not, and what this course
builds, so that lesson 1 is worth your time.

## You will need

Nothing. This lesson has nothing to type; it is a watch-and-read. Lesson 1
installs apic.

## The problem

You have an API to call. Every tool you reach for is good at one thing and
stops there:

- **Postman and Bruno are apps.** A request lives in a workspace, not in
  your repository. Running it in CI means another product, another account,
  another export format.
- **`.http` files in VS Code or JetBrains are great until you leave the
  editor.** Click the link, read the response, done. Then you want to run
  the same file in a pipeline, from a script or from an AI agent, and there
  is no command for it.
- **curl in a Taskfile runs anywhere** and has none of the rest: no
  environments, no way to pass a token from one call to the next, no
  assertions, no output a program can read without a pile of `jq`.

So teams end up with all three: an app for exploring, an editor file for
the person who wrote the endpoint, and a shell script for CI. They drift.

## The idea

Take the format editors already understand, a plain `.http` file, and give
it one static binary that runs it everywhere:

```http
### Log in and keep the token
# @name login
# @assert status == 200
# @capture token = body.$.access_token
POST {{baseUrl}}/auth/login
Content-Type: application/json

{"user": "{{user}}", "password": "{{password}}"}

### Who am I
# @name whoami
# @ref login
# @assert status == 200
GET {{baseUrl}}/me
Authorization: Bearer {{token}}
```

Read it top to bottom. `###` starts a request and titles it. The request
line, headers and body are exactly what REST Client and JetBrains send when
you click them; they still work there. Everything apic adds is a comment,
`# @something`, which editors ignore:

- `# @name` gives the request an id you can type: `apic run login`.
- `# @assert` checks the response, so a request is also a test.
- `# @capture` keeps a value from the response, and apic remembers it
  between runs, in another shell, tomorrow, from an agent.
- `# @ref login` says "if I am missing something login provides, run login
  first".

`{{baseUrl}}` and `{{password}}` come from environment files, the same
`http-client.env.json` the editors read, with secrets in a private file that
is never committed.

That is the whole design: files in git that humans, editors, CI and agents
all run the same way.

## Sixty seconds of it

Here is the terminal UI on the project that ships inside apic, sending a
request, running a file as a flow, reading the checks and the captured
values:

<img src="../../assets/apic-demo.svg" alt="apic ui --demo: send a request, run a file as a flow, read the checks, see what was captured">

And the same project from the command line:

```console
$ apic run login whoami
POST http://localhost:8089/auth/login
200 OK · 1 ms · 30 B
{ "access_token": "mock-token" }
✓ status == 200
↳ token = mock-token

GET http://localhost:8089/me
200 OK · 0 ms · 30 B
{ "email": "alice@example.com" }
✓ status == 200
✓ body.$.email endsWith @example.com

✓ login   200  1 ms
✓ whoami  200  0 ms

2 passed · 2 requests · 1 ms
```

Add `--json` and every request becomes one JSON line a program can read.
The exit code says whether the assertions passed. That is what CI and agents
get: not a screenshot of a tool, but the same file.

## What it does, as a story

- **Environments.** `dev`, `staging`, `prod` are entries in one file;
  `--env staging` switches. Secrets sit in a second file that git ignores.
- **Tokens that persist.** Log in once. Every later command, in any shell,
  has the token, until you clear it.
- **Assertions and flows.** A request checks its own response. A file runs
  top to bottom as a flow and stops at the first failure, or keeps going and
  reports.
- **Auth a text file cannot do.** Bearer and basic are text. OAuth2 client
  credentials, AWS SigV4 and "run this CLI to get a token" are one
  `# @auth` line each.
- **Gherkin without Cucumber.** `# @step I am logged in` on a request, and a
  `.feature` file can say it. No glue code.
- **Built for agents.** `apic list`, `apic describe`, `apic run --json` and
  an MCP server, so an agent discovers and calls your API the way you do,
  with the same files.

## What it is not

apic does not script. There is no JavaScript before or after a request, no
loops, no plugins. Where other tools reach for code, apic has a directive
(`# @retry`, `# @ref`, `# @capture`) or leaves it to the shell. The
[comparison](../comparison.md) says where each tool is better, honestly.

## What the course builds

Lessons 1 to 8 work against `apic demo`, a fake API that ships inside apic,
so nothing needs an account or a network. You install apic, send a request,
add environments, capture a token, write assertions and validate a project,
tour the UI, authenticate five ways, write behaviour tests in Gherkin and
run all of it in CI without leaking a secret. Lessons 9 to 13 go outward:
importing an OpenAPI spec, driving apic from an agent, real APIs, editors,
and how apic works inside.

## Checkpoint

None. The exercise is the checkpoint.

## Exercise

Install apic before the next episode. [Lesson 1](01-first-request.md)
shows how for every platform, and takes a minute.

## Going further

- [Why it's epic](../index.md), the same story in fewer words
- [Comparison with other tools](../comparison.md)
- [The `.http` format](../format.md), if you want to read ahead

??? note "Episode script"
    **Length.** 5 to 7 minutes.

    **Cold open (0:00).** Three windows side by side: a Postman collection,
    a `.http` file in VS Code with its "Send Request" link, a Taskfile with
    a curl line. "Same API. Three copies. Which one is right?"

    **Talking points.**

    1. The three tools and where each one stops (apps, editor-only files,
       curl with no environments or chaining).
    2. The idea: the editor's file, plus comments, plus one binary. Read
       the two-request file aloud, line by line.
    3. The sixty-second tour: the animated UI, then `apic run login
       whoami` in a terminal, then the same with `--json` piped into jq.
    4. The story bullets, one line each, no detail. Environments, tokens
       that persist, assertions and flows, auth, Gherkin, agents.
    5. What it is not, in one breath, and why that is a feature.
    6. What the course builds and what you need for lesson 1 (nothing).

    **Shot list.** B-roll: `docs/assets/apic-demo.svg` full screen; a
    Postman window next to the `.http` file; `apic run login whoami` in a
    100x30 terminal; the same `.http` file in VS Code with the REST Client
    link clicked. No tape for this lesson: the terminal shots are
    `docs/learn/tapes/01.tape` from lesson 1.

    **Chapters.** `0:00 Three tools, three copies` · `1:10 One file` ·
    `2:30 Sixty seconds of apic` · `3:40 What it does` · `5:00 What it is
    not` · `5:40 The course, and lesson 1`.

    **Thumbnail text.** "APIs from plain text files".

    **Description.** From the [episode template](_episode-template.md).
