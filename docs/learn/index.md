# From zero to apic

A course in fourteen short lessons, each with a matching YouTube episode.
You start with nothing installed and finish able to describe an API in
`.http` files, run it from the terminal, CI and an AI agent, and test it
with Gherkin. Every lesson runs against `apic demo`, the fake API that
ships inside apic, so you never need an account or a network connection to
follow along. A few lessons add real APIs at the end for the full picture.

## How the lessons work

Each lesson is one page with the same shape, so you always know where you
are:

| Section | What it is for |
|---|---|
| **Goal** | One sentence: what you can do when you finish. |
| **You will need** | apic version, whether `apic demo` should be running, anything else. |
| **Steps** | Numbered, each with the command to type and the output to expect. Copy, paste, compare. |
| **Checkpoint** | One command whose output proves the lesson took. |
| **Exercise** | One task to do on your own, with a collapsed solution. |
| **Going further** | Links into the guides for the depth the lesson skipped. |
| **Episode script** | Collapsed at the end: what the video says and shows, with chapter timestamps. |

The commands in every lesson are run in CI against the demo API, so a
lesson cannot quietly stop working when apic changes.

## Syllabus

| # | Lesson | You will learn | Episode |
|---|---|---|---|
| 0 | [What apic is and why](00-what-is-apic.md) | The problem, the idea, what the course builds | coming soon |
| 1 | [Install apic and send your first request](01-first-request.md) | Install, `apic ui --demo`, the anatomy of a `.http` file, `run`, `list`, `describe`, exit codes | coming soon |
| 2 | Variables and environments | Env files, secrets, `--var`, `APIC_VAR_`, the precedence order | coming soon |
| 3 | Capture, the session and flows | `# @capture`, the session file, running a file, `# @ref` | coming soon |
| 4 | Assertions, validation and polling | Selectors, operators, `validate`, `# @retry` | coming soon |
| 5 | The terminal UI tour | Every key with a purpose | coming soon |
| 6 | Authentication | bearer, basic, API keys, OAuth2, AWS SigV4, `exec` | coming soon |
| 7 | Behaviour tests with Gherkin | `# @step`, features, reports | coming soon |
| 8 | CI without leaking secrets | GitHub Actions, `--redact`, JUnit | coming soon |
| 9 | From OpenAPI to a project, and back to curl | `import`, `curl`, Postman import | coming soon |
| 10 | Agents and MCP | The shell contract, `claude mcp add` | coming soon |
| 11 | Real APIs and the Taskfile front door | The GitHub and Spotify examples | coming soon |
| 12 | Editors | The VS Code extension, JetBrains, Neovim | coming soon |
| 13 | How apic works inside, and contributing | Architecture, a first pull request | coming soon |

Lessons appear in the navigation as they are written; the episode column
links to the video once it is recorded. Each lesson is tracked in the
[course epic](https://github.com/dataGriff/api-caller/issues/28) on GitHub.

Start with [lesson 0](00-what-is-apic.md), or jump straight to
[lesson 1](01-first-request.md) if you already know why you are here.

## Before lesson 1

Nothing. Lesson 1 installs apic. If you want a head start:

```sh
go install github.com/dataGriff/api-caller/cmd/apic@latest   # or see Getting started
apic ui --demo
```

Press <kbd>enter</kbd> to send the request under the cursor, <kbd>?</kbd>
for the keys, <kbd>q</kbd> to quit. That is the whole tool in thirty
seconds; the course is the other twelve hours of what you can do with it.

## Writing a lesson

Contributions are welcome. [Writing a lesson](_template.md) is the page
template with the rules every lesson follows, and the
[episode template](_episode-template.md) is the YouTube description that
goes with it.
