# Working in this repository

apic is a Go CLI that runs `.http` request files for humans and AI agents.

## Layout

- `cmd/apic` — entry point
- `internal/httpfile` — `.http` parser (AST in `ast.go`, parser in `parse.go`, golden file in `testdata/`)
- `internal/project` — file discovery, `apic.yaml`, request lookup, `validate`
- `internal/env` — `http-client.env.json`, private env file, `.env`
- `internal/template` — `{{placeholder}}` substitution
- `internal/selector` — `status`, `header.x`, `body.$.path` selectors
- `internal/assert` — assertion parser and evaluator
- `internal/session` — `.apic/session.json` persistence of captured values and cached tokens
- `internal/auth` — `# @auth` spec parsing and application: bearer, basic, AWS SigV4 (own signer in `sigv4.go`, credentials in `awscreds.go`; no AWS SDK), OAuth2 grants, exec
- `internal/runner` — variable precedence, request execution, captures, asserts, flows, `describe`
- `internal/output` — human and JSON renderers
- `internal/curlexport`, `internal/openapi`, `internal/mcp` — the `curl`, `import` and `mcp` commands
- `internal/cli` — cobra commands, including `demo`
- `internal/demoapi` — fake in-memory API (auth + a todos CRUD resource) and its embedded example project (`project/`), backing the `apic demo` command and its test suite
- `examples/httpbin` — sample project targeting the real httpbin.org, for a live demo (needs network); `docs/` — the documentation site (MkDocs Material, `mkdocs.yml`, published to GitHub Pages by `.github/workflows/docs.yml`)

## Commands

- `task build` / `go build -o bin/apic ./cmd/apic`
- `task test` / `go test ./...` — tests use `net/http/httptest`, no network needed
- `task lint` — gofmt, go vet, golangci-lint (config in `.golangci.yml`)
- `task check` — what CI runs
- `task notices` — regenerate THIRD_PARTY_NOTICES.md (goreleaser runs this before packaging)
- `task docs` / `task docs:build` — preview or strictly build the docs site (`pip install "mkdocs<2" "mkdocs-material<10"`)

## Conventions

- Keep the `.http` dialect compatible with VS Code REST Client and JetBrains: new features go in `# @directive` comments before the request line, never new syntax in the request itself. Document any addition in `docs/format.md`.
- The `--json` output shape and exit codes are a public contract; change them only with a note in the README.
- Every command must work non-interactively (no prompts) and respect `--json`.
- Keep the dependency list small: prefer a few hundred lines of code over a large SDK (the AWS signer is the precedent).
- Add a test next to any parser or runner change; parser cases go in `internal/httpfile/testdata/sample.http`.
