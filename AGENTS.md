# Working in this repository

apic is a Go CLI that runs `.http` request files for humans and AI agents.

## Layout

- `cmd/apic`: entry point
- `internal/httpfile`: `.http` parser (AST in `ast.go`, parser in `parse.go`, golden file in `testdata/`)
- `internal/project`: file discovery, `apic.yaml`, request lookup, `validate`
- `internal/env`: `http-client.env.json`, private env file, `.env`
- `internal/template`: `{{placeholder}}` substitution
- `internal/selector`: `status`, `header.x`, `body.$.path` selectors
- `internal/assert`: assertion parser and evaluator
- `internal/session`: `.apic/session.json` persistence of captured values and cached tokens
- `internal/auth`: `# @auth` spec parsing and application for bearer, basic, AWS SigV4 (own signer in `sigv4.go`, credentials in `awscreds.go`; no AWS SDK), OAuth2 grants and exec
- `internal/runner`: variable precedence, request execution, captures, asserts, flows, `describe`
- `internal/output`: the shared theme (`theme.go`), the string renderers used by both the CLI and the UI (`render.go`), JSON highlighting (`jsonhl.go`) and the `io.Writer` wrappers (`output.go`)
- `internal/phrase`: `# @step` phrase to regex
- `internal/bdd`: the `apic test` machinery. Godog suite, step vocabulary (`steps.go`), phrase registration, JSON matching, cucumber-report summary
- `internal/curlexport`, `internal/openapi`, `internal/mcp`: the `curl`, `import` and `mcp` commands. The OpenAPI reader is a small yaml.Node walker (`model.go`) with local `$ref` resolution; do not add an OpenAPI library for it.
- `internal/cli`: cobra commands, including `demo`, `init` and `ui`
- `internal/ui`: the `apic ui` terminal UI. Model/update/view live in `ui.go`, `list.go`, `panes.go` and `run.go`, key bindings in `keys.go`, and its own small terminal layer in `term.go` (input decoding), `viewport.go` and `program.go` (event loop). Tests drive `Update`/`View` directly, so no terminal is needed
- `internal/demoapi`: fake in-memory API (auth + a todos CRUD resource) and its embedded example project (`project/`), backing the `apic demo` command and its test suite. This is the offline example; it's scaffolded on demand (`apic demo` writes it to `./apic-demo`), not a static copy under `examples/`
- `examples/`: static sample projects, each `apic validate`-checked in CI (`validate-examples` job). `httpbin` (basic/bearer auth, needs network but no keys), `github` (bearer auth against a real token, `repo.http`), `spotify` (`oauth2` client-credentials against a real app, `search.http`); `github` and `spotify` need the reader's own credentials in their `http-client.private.env.json` to run live; see `examples/README.md`
- `docs/`: the documentation site (MkDocs Material, `mkdocs.yml`, published to GitHub Pages by `.github/workflows/docs.yml`). `docs/assets/apic-ui.svg` and `apic-run.svg` are terminal screenshots generated from real output, not hand-drawn
- `scripts/shot`: the generator behind those screenshots (`task shots`). It serves the demo API in-process, drives the UI through `ui.Model.Press` and renders the frames the CLI and the UI really write, ANSI and all, as SVG. Regenerate them whenever anything on screen changes

## Commands

- `task build` / `go build -o bin/apic ./cmd/apic`
- `task test` / `go test ./...`: tests use `net/http/httptest`, no network needed
- `task lint`: gofmt, go vet, golangci-lint (config in `.golangci.yml`)
- `task check`: what CI runs
- `task notices`: regenerate THIRD_PARTY_NOTICES.md (goreleaser runs this before packaging)
- `task shots`: regenerate `docs/assets/apic-ui.svg` and `apic-run.svg` from real output (needs port 8089 free)
- `task docs` / `task docs:build`: preview or strictly build the docs site (`pip install "mkdocs<2" "mkdocs-material<10"`)
- New commands need a row in the README table, a section in `docs/cli.md` and a line in `docs/cheatsheet.md`

## Conventions

- Keep the `.http` dialect compatible with VS Code REST Client and JetBrains: new features go in `# @directive` comments before the request line, never new syntax in the request itself. Document any addition in `docs/format.md`.
- The `--json` output shape and exit codes are a public contract; change them only with a note in the README.
- Every command must work non-interactively (no prompts) and respect `--json`. `apic ui` is the single, deliberate exception: it is interactive, has no `--json`, and exits 2 when stdout is not a terminal.
- The UI is tested through `Update` and `View`, and driven headlessly by `Press`/`Resize` in `internal/ui/headless.go`; the screenshot generator uses the same entry points, so a screenshot cannot drift from what the UI draws.
- The UI has its own terminal layer rather than a TUI framework. Bubble Tea was tried and removed: its package `init` queries the terminal for its background colour with a five-second timeout, which every apic command would pay on terminals that do not answer. Keep anything with an init-time terminal query out of the binary.
- Keep the dependency list small: prefer a few hundred lines of code over a large SDK (the AWS signer and the OpenAPI reader are the precedents). Check the stripped binary size with `task build && ls -la bin/apic` when adding a dependency.
- Add a test next to any parser or runner change; parser cases go in `internal/httpfile/testdata/sample.http`.
- A new step in the vocabulary needs: its regex and shapes in `internal/phrase/builtin.go` (`Builtin`), a handler bound by name in `internal/bdd/steps.go`, a row in `bdd.Vocabulary`, a scenario in `bdd_test.go`, and the table in `docs/testing.md`.
