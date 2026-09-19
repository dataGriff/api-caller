# Security policy

apic handles credentials: API tokens, AWS keys, OAuth2 client secrets and
whatever else a `.http` file needs to authenticate. It reads them from files,
holds them in memory, caches them on disk and prints them to terminals and CI
logs. Bugs in any of that are worth reporting.

## Reporting a vulnerability

**Please do not open a public issue.**

Use GitHub's private advisory form:
<https://github.com/dataGriff/api-caller/security/advisories/new>

It lets you share details with the maintainer without the report being public,
and it becomes the advisory if a fix ships.

Useful things to include: the apic version (`apic version`), what you did, what
happened, and what an attacker gets out of it. A `.http` file that reproduces it
is ideal — please redact your own credentials from it first.

You should get an acknowledgement within a week. apic is maintained by one
person in their own time, so please read that as a genuine best effort rather
than a service-level commitment.

## Scope

In scope, and the areas most worth looking at:

- **A credential reaching somewhere it should not.** Printed in output that is
  not masked, written to a file that is not `0600`, sent to a host that was not
  the one addressed, or handed to an AI agent over the MCP server.
- **`--redact` failing to redact.** It is meant to make output safe to store in
  a CI log.
- **The MCP server serving something it should not.** It exposes a project to
  an LLM; it should serve that project's `.http` files and nothing else.
- **Path traversal** out of the project root, via `< ./body-file` references,
  MCP resource URIs or `apic import` output paths.
- **Auth correctness** — the AWS SigV4 signer and the OAuth2 token cache are
  both hand-written.
- **The release pipeline**, including anything that would let someone else
  produce an artifact that verifies as ours.

Out of scope:

- Running an untrusted `.http` file. A `.http` file is executable input: it can
  make apic send any request anywhere. `# @auth exec` runs a command and is
  gated behind `auth.allowExec` in `apic.yaml` for that reason. Treat a `.http`
  file the way you would treat a shell script.
- `--insecure`, which disables TLS verification because you asked it to.
- Vulnerabilities in a dependency with no path to reaching apic. CI runs
  `govulncheck`, which reports only what the binary actually calls; a finding it
  flags is in scope, one it dismisses generally is not.

## Verifying a release

Every release is signed, and each archive ships an SBOM. Checking a download
before you trust it is documented in
[docs/verifying.md](docs/verifying.md).

## What apic does to protect credentials

Not a guarantee, but it is what the current design intends, and a gap between
this list and the behaviour is a bug worth reporting:

- The private env file and `.apic/session.json` are written `0600`, in a `0700`
  directory that gitignores itself. `apic init` gitignores every credential file
  it knows about.
- Sensitive request headers (`Authorization`, `Cookie`, API-key headers) and
  sensitive response headers (`Set-Cookie`, `WWW-Authenticate`) are masked in
  output whether or not `--redact` is passed.
- `--redact` additionally masks both bodies, every header value, query values,
  captured values and assertion values, keeping only status, timing and
  pass/fail — so a redacted run is safe to store but not useful for chaining.
- Credential headers apic sets are dropped when a redirect leaves the host
  originally addressed.
- `apic curl --redact` emits shell placeholders rather than live credentials.
- The MCP server serves only the project's own `.http` files.
