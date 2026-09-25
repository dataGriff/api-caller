# Working in this repository

apic is a Go CLI that runs `.http` request files for humans and AI agents.

## Layout

The package-by-package map and the request lifecycle are on the
[architecture page](docs/architecture.md), which is the one source for
both; keep it current when a package's job changes. The short version:
`cmd/apic` is the entry point; `internal/httpfile` parses and formats
`.http` files; `internal/project` discovers and validates a project;
`internal/runner` resolves variables, applies auth, sends, captures and
asserts, with `env`, `template`, `session`, `auth`, `selector` and
`assert` beneath it; `internal/output` renders for the CLI, the UI and
MCP; `internal/bdd` and `internal/phrase` are `apic test`; `internal/cli`
holds the commands (one file per command) and `internal/ui` the terminal UI; `internal/demoapi`
is the offline API and project behind `apic demo`; `examples/` are the
static sample projects CI validates and format-checks; `editors/vscode/`
is the extension, released on its own `vscode-v*` tags; `setup-apic/` the
GitHub Action; `docs/` the site, with the course under `docs/learn/` (and the migration guides under `docs/migrate/`, whose runnable blocks `task learn:check` runs too) and
the generators under `scripts/`.

Two things worth knowing that the page also says: the OpenAPI and Postman
readers are hand-written walkers, not libraries, and the VS Code extension
never parses `.http` files itself (it reads `--json`).

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
- `task docs:check`: codespell, Vale and lychee, as the docs workflow runs them (needs the three on PATH). Prose is British English: Vale's `Apic.British` rule fails on `color` or `behavior` outside code spans. A deliberate typo in an example goes in `.codespellrc`'s `ignore-words-list`; a name Vale should hold to one spelling goes in `docs/.vale/styles/config/vocabularies/Apic/accept.txt`
- `task docs:cli`: regenerate the "Commands and flags" section at the end of `docs/cli.md` from the command tree (`scripts/clidocs`; a test fails when it is stale); `task man` writes the man pages goreleaser ships
- `task docs:errors`: regenerate `docs/errors.md` from `runner.Catalogue` (`scripts/errdocs`; a test fails when it is stale). A new error gets a code from the catalogue: `runner.Usage(runner.CodeX, msg)` or `usagef(CodeX, …)`, never a bare message; a new code needs an entry in `Catalogue` and a case in `internal/cli/errors_test.go`, which runs a real command for every code
- New commands need a row in the README table, a section in `docs/cli.md` and a line in `docs/cheatsheet.md`; a new or changed flag needs `task docs:cli`. A flag's usage text must not contain backticks: cobra reads the first backticked word as the value's name (a test checks)

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
- CI's lint job also runs `codespell` over the whole tree; the docs workflow runs
  codespell, Vale (errors only: British spelling, project terms, repeated words)
  and, on pull requests, lychee for external links, before building the site.
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
- The image `ghcr.io/datagriff/apic` is `Dockerfile` built by goreleaser's `dockers_v2` for linux/amd64 and linux/arm64: the binary on `scratch` with Alpine's CA bundle and a `/tmp`, laid out by an Alpine stage that runs on the build platform, so no QEMU. `release.yml` needs `packages: write`, logs in to GHCR and smoke-runs the pushed tag; `docker_signs` signs it keylessly. `task snapshot` builds it locally (needs Docker) without pushing.
- `nfpms:` builds `.deb`, `.rpm` and `.apk` packages (amd64 and arm64) with the binary in `/usr/bin`, the man pages, shell completions (generated by a `before` hook into `completions/`, per-packager paths for zsh) and the licence files; they are in `checksums.txt`, so the signature covers them. CI's `packages` job builds them with a snapshot and installs each on its distribution. There is no package repository.
- `install.sh` and `setup-apic/action.yml` build their URLs from the tag with the leading `v` stripped, which is what goreleaser's `.Version` gives; changing `archives.name_template` breaks both.
- After each release, `release.yml` force-moves the major tag (`v0` today, `v1` later) to the released commit so `setup-apic@v0` follows the newest release. The job only runs for tags containing a dot, so the moving tag never starts a release of its own.
- The VS Code extension is released separately, on `vscode-v<version>` tags: `vscode-release.yml` checks the tag against `editors/vscode/package.json`, runs the extension's tests, publishes to the Marketplace and Open VSX (secrets `VSCE_PAT` and `OVSX_PAT`; without them the publish steps are skipped) and attaches the `.vsix` to a GitHub release with the matching `CHANGELOG.md` section as notes. A binary tag never touches the extension and an extension tag never touches the binary; `release.yml` matches `v*`, which `vscode-v*` does not start with. The extension's `MIN_VERSION` in `src/apic.ts` names the oldest binary it drives; raise it when the extension starts to depend on a newer `--json` key.
