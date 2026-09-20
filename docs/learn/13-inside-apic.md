# Lesson 13: How apic works inside, and your first contribution

**Goal.** Follow a request through the packages, know the three contracts
every change is checked against, and add a small directive end to end
with `task check` green.

## You will need

- apic installed ([lesson 1](01-first-request.md)) and `apic demo`
  running, for the one command below that looks at the tool from
  outside
- Go (the version in `go.mod`) and [Task](https://taskfile.dev) for
  everything else, and a clone of the repository:

```sh
git clone https://github.com/dataGriff/api-caller
cd api-caller
task build      # -> bin/apic
task test       # no network: every test runs against httptest servers
```

## Steps

### 1. The lifecycle

The [architecture page](../architecture.md) has the diagram; this is the
walk. A request goes through eight stages, and each is one package:

1. **Parse** (`internal/httpfile`): the file is split on `###`, each
   block's `# @` comments become directives, then the request line,
   headers and body are read. Problems become diagnostics with a line, a
   column and a code; the parser never gives up on a file.
2. **Discover** (`internal/project`): `apic.yaml`, the env files, every
   `.http` file under the root, indexed by name. `Validate` adds the
   project-level checks you met in lesson 4.
3. **Resolve** (`internal/runner` with `template`, `env`, `session`):
   every `{{placeholder}}` looked up in the precedence order from
   lesson 2; a missing one stops here with exit 2, unless a `# @ref`
   names the request that captures it.
4. **Apply auth** (`internal/auth`): `# @auth` becomes a header, a SigV4
   signature, a cached OAuth2 token or a command's output.
5. **Send** (`net/http`): one attempt per `# @retry` round, with the
   timeout, the TLS settings and the cookie jar.
6. **Capture and assert** (`internal/selector`, `internal/assert`): the
   body is read once; selectors pull values out and assertions are
   evaluated. A failure is exit 1.
7. **Persist** (`internal/session`): captures and cookies to `.apic/`.
8. **Render** (`internal/output`): the same `Result` becomes the
   terminal report, the `--json` line, the UI panes and the MCP result.

You can see stage 8's contract from outside, and it is the one an agent,
the UI and the VS Code extension all read. Every key of a `list` entry:

<!-- learn -->
```sh
apic list -C apic-demo --json | jq -c '.requests[0] | keys'
```

```
["asserts","captures","description","file","id","line","method","name","steps","url"]
```

(`refs` appears on `whoami`, which declares one; a key that is empty is
left out.) Keep that list in mind: the directive you add in step 4 will
put a key on it.

### 2. How the code is tested

Three habits run through the tests, and a change that follows them is
easy to accept:

- **The parser has a golden file.** `internal/httpfile/testdata/sample.http`
  holds one of everything the parser understands, and `TestParseSample`
  in `parse_test.go` checks the fields it produces. A new piece of syntax
  is a case in that file first.
- **The runner talks to `httptest`.** `internal/runner/runner_test.go`
  starts an `httptest.NewServer` per test with handlers that play the
  API, so a test can hand back a 401, a redirect or a slow body without
  a network. The same goes for `internal/bdd` and `internal/mcp`.
- **The UI is driven headlessly.** `internal/ui/headless.go` has `Press`
  and `Resize`; tests and the screenshot generator (`task shots`) call
  them, so a screenshot in the docs is what the UI draws.

```sh
go test ./internal/httpfile/ -run TestParseSample -v
```

```
=== RUN   TestParseSample
--- PASS: TestParseSample (0.00s)
PASS
```

### 3. The three contracts

Every review asks the same three questions, and the tree enforces each:

| Contract | Enforced by |
|---|---|
| **The dialect is REST Client's and JetBrains'.** Everything apic adds is a `# @` comment. | The golden file; `TestGrammarMatchesKnownDirectives`, which fails when the VS Code grammar and the parser disagree on the directive list; `validate` warning rather than failing on an unknown directive. |
| **`--json` and the exit codes are stable.** Keys are only added; 0, 1, 2 and 3 keep their meanings. | `runner.ExitCode` uses `errors.As`, `errorlint` is on so a wrapped `TransportError` cannot become a 2, and `TestExitCodeUnwraps` pins it. |
| **Every command is non-interactive.** `apic ui` is the one exception. | Every CLI test runs with no terminal; the device-code and `exec` prompts go to stderr and never block on stdin. |

`task check` is what CI runs: lint (with `errorlint`, `gosec`,
`bodyclose` and friends), tests, the race detector, the licence check and
the examples. A green `task check` locally is usually a green pull
request.

### 4. Add a directive: `# @tag`

The [architecture page](../architecture.md#how-to-add-things) has the
checklist; walk it with a small, real directive. `# @tag smoke` labels a
request, repeatable, and `apic list --json` surfaces the tags so an agent
or a script can pick the smoke set. Five edits and a test.

**Parse it.** The parser already keeps every `# @key value` on the
request as a `Directive`; it only needs to know the name, or `validate`
warns about it. In `internal/httpfile/parse.go`, one line in
`KnownDirectives`:

```go
"tag": "label for filtering: `# @tag smoke`, repeatable",
```

and a case in the golden file, `testdata/sample.http`, as a new block
at the end of the file:

```http
### Tagged
# @name tagged
# @tag smoke
# @tag users
GET {{baseUrl}}/health
```

At the end, not in the middle: `TestParseSample` pins the line numbers
of the multipart parts in the last request, so two lines inserted above
them would fail it. It also counts the requests, so the count goes from
6 to 7, and one assertion that `f.Requests[6]` carries two `tag`
directives pins the new case.

**Tell the grammar.** `editors/vscode/syntaxes/apic-directives.injection.json`
lists the known directives in one alternation; add `tag` to it, or
`TestGrammarMatchesKnownDirectives` fails on the next `go test`. That
test is the dialect contract at work.

**Surface it.** `apic list` builds its entries in `internal/cli/inspect.go`.
Give `listEntry` a field and fill it from the directives:

```go
Tags []string `json:"tags,omitempty"`
```

```go
for _, d := range r.Directives {
    if d.Key == "tag" {
        e.Tags = append(e.Tags, d.Value)
    }
}
```

`omitempty` keeps the shape identical for every request without tags,
which is what "keys are only added" means in practice.

**Give it a place in the canonical order.** `directiveRank` in
`internal/httpfile/format.go` orders the known directives for `apic fmt`;
a tag reads best beside `description`, so slot it there. Without a rank
it would still work, sorted after the known ones.

**Document it.** A row in the directive table of `docs/format.md`, a
line in `docs/cheatsheet.md`, and `tags` in the `list --json` note of
`docs/cli.md`.

**Test it.** `internal/cli/ref_test.go` shows the pattern: write a
project to a temp directory, run `list --json`, assert on the text. Add
a request with two tags and assert on `"tags": ["smoke", "users"]`.
Then:

```sh
task check
```

```
task: [lint] golangci-lint run ./...
0 issues.
task: [test] go test ./...
ok      github.com/dataGriff/api-caller/internal/cli
ok      github.com/dataGriff/api-caller/internal/httpfile
…
```

The whole change is under fifty lines, and every one of them has a test
or a check that would have caught its absence: that is what the
checklist is for.

### 5. Open the pull request

`CONTRIBUTING.md` is short, and the parts that matter for a first change:

- A test next to it, that fails without the fix. Revert the fix once and
  watch it go red; that is the proof.
- Docs in the same change: a new directive needs `format.md` and the
  cheat sheet, a new command the README table, `cli.md` and the cheat
  sheet.
- A commit message that says why. The diff already says what.
- `task check` green before you push. If the UI changed, `task shots`;
  if a lesson changed, `task learn:check`.

Push a branch and open the pull request; CI runs the same `task check`
plus the docs build, the course harness and the extension's tests.

## Checkpoint

`task check` is green with the new directive and its test, and:

```sh
bin/apic list -C apic-demo --json | jq -c '[.requests[] | select(.tags)] | length'
```

prints the number of requests you tagged in the demo project (the
bundled project has none until you add some, so tag one and see `1`).

## Exercise

Pick a `P3` issue from the
[tracker](https://github.com/dataGriff/api-caller/issues?q=is%3Aissue+is%3Aopen+label%3AP3)
and open a pull request for it. `P3` issues are small and self-contained
on purpose.

??? example "Solution"
    There is no single one. A good first pull request has the shape of
    step 4: a change of a few dozen lines, a test that fails without it,
    the docs it needs, and a commit message that starts with the reason.
    Say in the description what you checked and how; reviewers read that
    first.

## Going further

- [Architecture](../architecture.md): the diagram, the package map and
  the checklists for a directive, a Gherkin step, a command and a config
  key
- [AGENTS.md](https://github.com/dataGriff/api-caller/blob/main/AGENTS.md):
  the conventions, including the ones that are easy to trip over
- [CONTRIBUTING.md](https://github.com/dataGriff/api-caller/blob/main/CONTRIBUTING.md)
  and [SECURITY.md](https://github.com/dataGriff/api-caller/blob/main/SECURITY.md)

??? note "Episode script"
    **Length.** 12 minutes; a code walkthrough in an editor.

    **Cold open (0:00).** The lifecycle diagram, then `apic run whoami`
    with `login` running first. "Eight stages, eight packages. Here is
    where each of those lines came from."

    **Talking points.**

    1. The lifecycle, one package per stage, with the file open for each.
    2. The golden file, an `httptest` handler in a runner test, `Press`
       in a UI test.
    3. The three contracts and the test behind each; `task check`.
    4. `# @tag` end to end: `KnownDirectives`, the golden case, the
       grammar test going red then green, `listEntry`, `directiveRank`,
       the docs rows, the CLI test.
    5. `task check` green; the pull request and what CONTRIBUTING asks.
    6. Checkpoint, then the `P3` exercise.

    **Shot list.** An editor with the repository open, the terminal in a
    split; the diagram from the architecture page full-screen for the
    open. No tape for this lesson.

    **Chapters.** `0:00 Eight stages` · `2:00 How it is tested` · `4:00
    The three contracts` · `5:30 Add # @tag` · `10:00 The pull request`
    · `11:00 Checkpoint and exercise`.

    **Description.** From the [episode template](_episode-template.md).
