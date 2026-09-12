# apic

Run API requests from plain `.http` files, in the terminal, in CI, or from
an AI agent, on any platform, with one static binary.

```sh
apic run login                      # POST, capture the token
apic run get-user --env staging     # reuse the token, check assertions
apic run smoke.http --json | jq     # whole file as a flow, one JSON line per request
claude mcp add api -- apic mcp      # let an agent call the same requests as tools
```

The files are the same `.http` files VS Code, JetBrains and Neovim can send
with one click. apic adds environments, captured values that persist between
runs, assertions, AWS and OAuth2 auth, JSON output, curl export, OpenAPI import and an MCP server,
all as comments the editors ignore.

## Install

```sh
go install github.com/dataGriff/api-caller/cmd/apic@latest            # Go 1.25+
curl -fsSL https://raw.githubusercontent.com/dataGriff/api-caller/main/install.sh | sh   # Linux, macOS
```

Windows: download the zip from [GitHub Releases](https://github.com/dataGriff/api-caller/releases).

## Guides

| Guide | Read it when |
|---|---|
| [Getting started](getting-started.md) | You are setting up apic for a project for the first time. |
| [CLI reference](cli.md) | You want every command, flag, output shape and exit code. |
| [The `.http` format](format.md) | You are writing or editing request files: directives, variables, selectors, assertions. |
| [Authentication](auth.md) | Your API needs AWS SigV4, OAuth2, basic auth or a token from a CLI. |
| [Using apic from an AI agent](agents.md) | You want Claude Code, Cursor or a shell-driven agent to call your API. |
| [Taskfile integration](taskfile.md) | You already drive things with `task` and want to keep that front door. |
| [Comparison with other tools](comparison.md) | You are deciding between apic, Bruno, Hurl, Postman or curl. |

## Source

[github.com/dataGriff/api-caller](https://github.com/dataGriff/api-caller)
