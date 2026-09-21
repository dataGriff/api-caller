# FAQ

Short answers to the things that come up. Anything longer lives in the
guides these link to.

## Why does it say a variable is missing?

Because nothing provided it. The error names the request that would:

```
error: users.http:14: missing variable
  {{token}}: it is captured by request "login"; run `apic run login` first, or pass --var token=...
```

`apic describe <id>` shows every variable the request needs, the source each
one resolved from, and whether the request is ready to send. The
[precedence order](cheatsheet.md#variable-precedence) decides which source
wins. Put `# @ref login` on the request and apic runs `login` itself when
the token is missing; see [dependencies](format.md#dependencies).

## Why does a variable have the wrong value?

Something earlier in the precedence order is providing it. Common culprits:
a stale captured value in the session, or an `APIC_VAR_` variable left in
the shell. `apic describe <id>` prints the winning source for each variable;
`apic session clear` forgets captured values for the current environment.

## Do captured values leak between environments?

No. The session stores them per environment, so `dev` and `staging` keep
separate tokens. `apic session` lists them grouped by environment, and
`apic session clear --all` clears the lot.

## How do I keep secrets out of CI logs?

Two things, and use both:

- Pass secrets through the shell as `APIC_VAR_<name>` instead of writing
  them to a file on the runner.
- Add `--redact`, which masks header values, bodies, query values and
  captured values in the output. Sensitive headers (`Authorization`,
  `Cookie`, API-key headers, and anything whose value came from a secret
  source) are masked even without it.

Recipe: [a smoke test in CI that never leaks secrets](cookbook.md#a-smoke-test-in-ci-that-never-leaks-secrets).

## My AWS request comes back 403

The signature is computed from the service, region, method, path, query and
body, so a 403 usually means one of those does not match what the API
expects:

- `service=` defaults to `execute-api`. For anything else (`s3`, `lambda`,
  `es`) say so explicitly.
- The region must be the API's region, not your default one.
- Check the credentials apic will use: it reads the environment first, then
  the named profile, then the AWS CLI. `aws sts get-caller-identity
  --profile <name>` tells you who you are.
- An expired SSO session is reported with the CLI's own message, usually
  "run aws sso login".

Details in [authentication](auth.md#aws).

## How do I send requests through a proxy?

`HTTP_PROXY`, `HTTPS_PROXY` and `NO_PROXY` are honoured the way curl
honours them. For a proxy that should beat the environment, or one that
belongs to a project, set it explicitly:

```sh
apic run get-user --proxy http://127.0.0.1:8080     # Burp, mitmproxy, Charles
apic run get-user --proxy socks5://127.0.0.1:1080
apic run get-user --no-proxy                        # ignore every setting for one run
```

```yaml
# apic.yaml
proxy: http://proxy.internal:3128
noProxy: [localhost, .internal]
```

`--proxy` beats `apic.yaml`, which beats the environment. Credentials go in
the URL (`http://user:pass@proxy:3128`) and are shown as `***` wherever
apic reports the proxy: `describe`, `env --json`, `run -v` and
`run --json`. Debugging a TLS API through an intercepting proxy also
needs its CA: `--cacert mitmproxy-ca.pem`, or `--insecure` for a one-off.

## Can apic do the OAuth2 authorization code flow?

No. Flows that need a browser redirect back to a local port are not
supported. Client credentials, password and device code are, and the token
is cached and refreshed for you. For a human signing in at a terminal, use
`grant=device_code`. See [authentication](auth.md#oauth2).

## A Gherkin step comes out undefined

The phrase does not match anything apic knows. Run `apic test --steps` to
print the built-in vocabulary and every `# @step` phrase the project
declares, then make the feature match one of them exactly. Undefined steps
fail the run on purpose: a test that silently skips is worse than a red one.

## Can I write my own step definitions?

No, and that is deliberate. The vocabulary plus `# @step` phrases on
requests is the whole language, which keeps features runnable by anyone with
the binary and no project-specific code. When you need logic, put it in a
shell script around `apic run --json`.

## Why is there no scripting?

Because the moment a request file can compute things, it stops being a file
an editor can send and a reader can trust. apic's escape hatches are
`--json` for programs, `apic curl` for one-off surgery, and `# @auth exec`
for credentials that come from a tool.

## `apic ui` will not start

It needs an interactive terminal, and says which condition failed:

- stdout is a pipe or file: use `apic run` or `apic list --json` instead.
- `--json` was given: the UI has no machine output by design.
- On Windows, use Windows Terminal or PowerShell; Git Bash's mintty needs
  `winpty apic ui`.
- Below 70x16 it reports the terminal is too small rather than drawing a
  broken screen.

## Where is the colour?

Colour is switched off when stdout is not a terminal, when `NO_COLOR` is
set, when `--no-color` is given, and whenever `--json` is used. That is why
piping to a file gives clean text with no escape codes.

## Can I pin the version the installer fetches?

Yes:

```sh
APIC_VERSION=v1.2.3 curl -fsSL https://raw.githubusercontent.com/dataGriff/api-caller/main/install.sh | sh
APIC_INSTALL_DIR=~/bin curl -fsSL ... | sh
```

The installer downloads the release archive for your platform, verifies it
against the published `checksums.txt`, and refuses to install on a mismatch.

## Does apic work with VS Code and JetBrains files?

That is the point. apic runs the common subset of the `.http` format shared
by VS Code REST Client, JetBrains HTTP Client, kulala.nvim and httpyac, and
everything it adds is a `# @directive` comment those tools ignore. `# @note`
and `# @prompt` from REST Client are accepted and ignored, so a file that
uses them still parses (pass prompted values with `--var`).

## What is not supported?

No scripting, no browser-based OAuth2 flows, no GraphQL or gRPC tooling
beyond plain HTTP, and no HTML report. The honest full list is in
[the comparison](comparison.md#what-apic-does-not-do).
