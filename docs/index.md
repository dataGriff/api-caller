---
hide:
  - navigation
---

<div class="apic-hero">
<img src="assets/logo.svg" width="72" alt="">

<p class="apic-tagline">apic is epic</p>

<p>Run API requests from plain <code>.http</code> files, in the terminal, in CI, or from an AI agent, on any platform, with one static binary.</p>

<img src="assets/apic-demo.svg" alt="apic ui --demo: send a request, run a file as a flow, read the checks, see what was captured">
</div>

```sh
apic ui --demo                      # a fake API and a UI to poke it with, no setup
apic run login                      # POST, capture the token
apic run get-user --env staging     # reuse the token, check assertions
apic run smoke.http --json | jq     # whole file as a flow, one JSON line per request
apic test                           # run Gherkin features against the same requests
claude mcp add api -- apic mcp      # let an agent call the same requests as tools
```

## 30 seconds to epic

=== "Go"

    ```sh
    go install github.com/dataGriff/api-caller/cmd/apic@latest
    apic ui --demo
    ```

=== "Linux / macOS"

    ```sh
    curl -fsSL https://raw.githubusercontent.com/dataGriff/api-caller/main/install.sh | sh
    apic ui --demo
    ```

=== "Windows"

    Download the zip from [GitHub Releases](https://github.com/dataGriff/api-caller/releases),
    put `apic.exe` on your `PATH`, then:

    ```powershell
    apic ui --demo
    ```

`--demo` serves a small fake API inside the same process and opens the
[terminal UI](tui.md) on an example project that targets it. There is
nothing to sign up for or clone, and nothing is left behind when you quit.

!!! tip "Prefer the plain CLI?"
    `apic demo` writes the same example project to `./apic-demo` and serves
    the API, so you can drive it by hand:
    `apic run login whoami -C apic-demo`.

## Why it's epic

<div class="apic-cards">

<a href="format/#variables">
<strong>Environments that travel</strong>
<span>http-client.env.json, .env, the shell and --var, with one precedence order you can print.</span>
</a>

<a href="format/#directives">
<strong>Tokens that persist</strong>
<span>Capture a value once; the next run, in a new shell or a new agent call, already has it.</span>
</a>

<a href="format/#assertion-operators">
<strong>Assertions and flows</strong>
<span>Check status, headers and JSON paths. A file runs in order with a pass/fail summary and an exit code.</span>
</a>

<a href="auth/">
<strong>Auth a text file cannot do</strong>
<span>AWS SigV4 with no SDK, OAuth2 with caching and refresh, basic, bearer, or any CLI that prints a token.</span>
</a>

<a href="testing/">
<strong>Gherkin without Cucumber</strong>
<span>Features run against the same requests with a built-in vocabulary. JUnit and cucumber reports included.</span>
</a>

<a href="agents/">
<strong>Built for agents</strong>
<span>Stable --json per request, discoverable requests, errors that say what to do next, and an MCP server.</span>
</a>

</div>

The files stay ordinary `.http` files: everything apic adds is a comment, so
VS Code, JetBrains and Neovim still send them with one click.

## Where to go next

| Guide | Read it when |
|---|---|
| [Getting started](getting-started.md) | You are setting up apic for a project for the first time. |
| [Terminal UI](tui.md) | You want to browse and run requests interactively. |
| [Cheat sheet](cheatsheet.md) | You want every directive, selector and operator on one page. |
| [The `.http` format](format.md) | You are writing or editing request files. |
| [Authentication](auth.md) | Your API needs AWS SigV4, OAuth2, basic auth or a token from a CLI. |
| [Testing with Gherkin](testing.md) | You want `.feature` files to describe API behaviour. |
| [Cookbook](cookbook.md) | You have a specific job to do and want a worked recipe. |
| [CLI reference](cli.md) | You want every command, flag, output shape and exit code. |
| [Using apic from an AI agent](agents.md) | You want Claude Code, Cursor or a shell-driven agent to call your API. |
| [FAQ](faq.md) | Something is not behaving and you want the short answer. |
| [Taskfile integration](taskfile.md) | You already drive things with `task`. |
| [Comparison](comparison.md) | You are deciding between apic, Bruno, Hurl, Postman or curl. |

## Source

[github.com/dataGriff/api-caller](https://github.com/dataGriff/api-caller)
