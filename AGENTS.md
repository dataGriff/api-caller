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
- `internal/demoapi`: fake in-memory API (auth, API key, a todos CRUD resource with filtering and validation, polled jobs, multipart upload, GraphQL, a CSV report, a slow route, health; the route list is on `New`) and its embedded example project (`project/`), backing the `apic demo` command and its test suite. Every route exists so a lesson or a guide has something offline to run against; keep the project's requests and `features/todos.feature` in step with it. This is the offline example; it's scaffolded on demand (`apic demo` writes it to `./apic-demo`), not a static copy under `examples/`
- `examples/`: static sample projects, each `apic validate`-checked in CI (`validate-examples` job). `httpbin` (basic/bearer auth, needs network but no keys), `github` (bearer auth against a real token, `repo.http`), `spotify` (`oauth2` client-credentials against a real app, `search.http`); `github` and `spotify` need the reader's own credentials in their `http-client.private.env.json` to run live; see `examples/README.md`
- `setup-apic/`: the composite GitHub Action (`uses: dataGriff/api-caller/setup-apic@v0`) that installs a release with the same checksum verification as `install.sh`, on all three runner OSes. `.github/workflows/action-test.yml` runs it against the latest release when it or `install.sh` changes
- `docs/`: the documentation site (MkDocs Material, `mkdocs.yml`, published to GitHub Pages by `.github/workflows/docs.yml`). `docs/assets/apic-ui.svg` and `apic-run.svg` are terminal screenshots generated from real output, not hand-drawn
- `scripts/shot`: the generator behind those screenshots (`task shots`). It serves the demo API in-process, drives the UI through `ui.Model.Press` and renders the frames the CLI and the UI really write, ANSI and all, as SVG. Regenerate them whenever anything on screen changes
- `scripts/schemas`: generates `docs/schemas/*.json`, the JSON schemas for `apic.yaml` (reflected from `project.Config`, every key needs a description in the generator), the env files and the session file (`task schemas`). Its test fails when the committed files are stale, so a new `apic.yaml` key means a description and a regeneration

## Commands

- `task build` / `go build -o bin/apic ./cmd/apic`
- `task test` / `go test ./...`: tests use `net/http/httptest`, no network needed
- `task lint`: gofmt, go vet, golangci-lint (config in `.golangci.yml`)
- `task fmt`: actually format the tree (`lint` only checks)
- `task race`: `go test -race ./...`
- `task cover`: write `coverage.out` and print the per-package summary
- `task vuln`: govulncheck against the dependency tree
- `task validate-examples`: the example projects, as CI checks them
- `task licences`: LICENSE present, and every compiled-in module has one
- `task check`: what CI runs — lint, test, race, licences and validate-examples
- `task clean`: remove `bin/`, `dist/`, `site/`, `coverage.out`, notices
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

## CI and linting

- `.golangci.yml` runs the standard set plus `bodyclose`, `copyloopvar`,
  `errorlint`, `gosec`, `misspell` and `unparam`. Each was measured against the
  tree before being enabled, so the config is quiet: a new finding means
  something. Deliberately left off: `nilerr` (its hits in `cli/test.go` are
  correct), `predeclared` and `usestdlibvars` (pure style).
- A `//nolint:gosec` needs a reason on the same line saying why the call is
  safe. The existing ones mark deliberate decisions — reading the file the user
  named, scaffolding a project directory the user then edits, `$randomInt` not
  being a nonce — and are the place to look before adding another.
- `errorlint` is on because exit codes are a public contract: `runner.ExitCode`
  uses `errors.As`, so wrapping a `TransportError` cannot silently turn a
  documented 3 into a 2. There is a test for exactly that.
- CI also runs `govulncheck`, CodeQL (weekly and per PR) and a coverage job
  that uploads `coverage.out` as an artifact. Dependabot watches `gomod` and
  `github-actions` weekly.
- Workflows declare `permissions: contents: read` at the top and widen only
  where a job needs it (`docs.yml` for Pages, `codeql.yml` for
  security-events, `release.yml` for the release upload).

## Licensing

- apic is MIT (`LICENSE`, root). Keep it there: CI's `licences` job fails without it, and goreleaser ships it in every archive.
- `task notices` (`scripts/notices.sh`) regenerates `THIRD_PARTY_NOTICES.md`, which is generated rather than committed (it is in `.gitignore`); goreleaser runs it before packaging.
- The generator unions the module set across every released GOOS/GOARCH, not just the host: cobra pulls in `mousetrap` on Windows only. It also reproduces each module's `NOTICE` (required by Apache-2.0 4(d)) and `PATENTS` files, and exits non-zero if a module has no licence file at all.
- Keep new dependencies permissive (MIT, BSD, Apache-2.0; the MPL-2.0 modules godog pulls in are the existing exception). Anything reciprocal — GPL or LGPL — would change apic's own terms, so it is off the table for a statically linked binary.
- Write the AWS signer and the OpenAPI reader style of code from the spec, not by copying from another project; the tree carries no third-party source files and should stay that way.

## Releasing

- Releases are signed with cosign, keyless: the certificate is bound to the release workflow's OIDC identity, so `release.yml` needs `id-token: write` on the job. There is no private key.
- Only `checksums.txt` is signed. It names every archive with its SHA-256, so one signature covers the release; verifying is a two-step chain, documented in `docs/verifying.md`.
- Each archive gets an SPDX 2.3 SBOM from syft, per archive rather than per release because the module set differs by platform (cobra pulls in `mousetrap` on Windows only).
- `cosign` and `syft` are installed by `release.yml`; neither ships with the runner or with goreleaser-action. goreleaser tries to sign even on a snapshot and fails hard without cosign, so `task snapshot` passes `--skip=sign,sbom` — a local snapshot is a build sanity check, not a release.
- Before a tag: `task check`, then `task snapshot` to prove archive names, contents and version injection. `apic version` from an extracted archive must report the version, not `dev` — a typo in the ldflags path fails silently.
- `install.sh` and `setup-apic/action.yml` build their URLs from the tag with the leading `v` stripped, which is what goreleaser's `.Version` gives; changing `archives.name_template` breaks both.
- After each release, `release.yml` force-moves the major tag (`v0` today, `v1` later) to the released commit so `setup-apic@v0` follows the newest release. The job only runs for tags containing a dot, so the moving tag never starts a release of its own.
