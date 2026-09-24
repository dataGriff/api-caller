# Authentication

apic can attach credentials to a request with a `# @auth` directive, or to
every request in a project with `auth.default` in `apic.yaml`. Like every
apic directive it is a comment, so the files still work in VS Code,
JetBrains and Neovim.

```http
### List orders (signed with AWS SigV4)
# @name list-orders
# @auth aws service=execute-api region=eu-west-2
GET {{baseUrl}}/orders
```

| Type | Spec | What it does |
|---|---|---|
| `none` | `# @auth none` | Send no credentials. Overrides a project default. |
| `bearer` | `# @auth bearer {{token}}` | `Authorization: Bearer <token>` |
| `basic` | `# @auth basic {{user}} {{password}}` | HTTP basic auth (base64 done for you) |
| `aws` | `# @auth aws [service=execute-api] [region=..] [profile=..]` | AWS Signature V4 using the standard credential chain |
| `oauth2` | `# @auth oauth2 tokenUrl=.. clientId=.. ...` | Client credentials, password, device code or authorization code (PKCE) grant; token cached and refreshed |
| `exec` | `# @auth exec <command> [args]` | Run a command and use its output as the token |

Values in a spec may contain `{{placeholders}}`; they resolve like any other
variable, so secrets belong in `http-client.private.env.json`, `.env` or
`APIC_VAR_*` shell variables. Quote values with spaces: `scope="read write"`.

## Project default

```yaml
# apic.yaml
auth:
  default: aws region=eu-west-2 service=execute-api
  allowExec: false
```

Every request without its own `# @auth` uses the default. A request opts
out with `# @auth none` or picks something else with its own directive.
Because the default may contain `{{variables}}`, one project can point at
different identity providers per environment:

```yaml
auth:
  default: oauth2 tokenUrl={{tokenUrl}} clientId={{clientId}} clientSecret={{clientSecret}}
```

with `tokenUrl` and `clientId` in `http-client.env.json` per environment
and `clientSecret` in the private file.

`apic describe <id>` shows which spec applies and whether its variables
resolve.

## bearer and basic

```http
# @auth bearer {{token}}
# @auth basic {{user}} {{password}}
```

`bearer` is the same as writing the `Authorization` header yourself; it
exists so it can be a project default. `basic` base64-encodes
`user:password`, which is the part you cannot do in a plain header.

See `examples/github/repo.http` for `bearer` against the real GitHub API.

## apikey

```http
# @auth apikey {{key}}                                  # header X-Api-Key, no prefix
# @auth apikey {{key}} header=X-Auth-Token
# @auth apikey {{key}} query=api_key
# @auth apikey {{key}} header=Authorization prefix="Token "
```

The most common credential on the internet, as a type rather than a
header you write by hand, so it can be the project default:

```yaml
auth:
  default: apikey {{apiKey}}
```

`header=` names the header (default `X-Api-Key`) and `prefix=` puts text
before the key; `query=` sends it as a query parameter instead, which
some APIs insist on. Wherever apic shows the request the key is not
there: the header is set on the wire only, and in the query form the URL
apic prints is the one from the file. A key that contains `=` is still
one argument.

## digest

```http
# @auth digest {{user}} {{password}}
```

HTTP Digest authentication (RFC 7616). The server answers the first
request with a `401` and a challenge; apic computes the response from the
credentials, the server's nonce and the request, and sends the request
again with the `Authorization` header, so the password never travels.
`MD5`, `MD5-sess`, `SHA-256` and `SHA-256-sess` are implemented, with
`qop=auth` and `auth-int` (chosen for requests with a body when the
server offers it), `opaque`, `userhash` and stale-nonce retry. The
challenge is remembered for the rest of the invocation, so later requests
to the same server go out authenticated on the first try with the nonce
count going up. `run -v` says which happened (`auth: digest: 401 challenge
answered (2 requests)`), and `--json` reports `"auth": "digest"`.

## aws

```http
# @auth aws
# @auth aws service=s3 region=us-east-1
# @auth aws profile=prod
```

Requests are signed with AWS Signature Version 4 (header authentication).
apic does not embed the AWS SDK; it implements the signature itself, checked
against the official AWS test vectors, and finds credentials the same way
the AWS CLI does:

1. `AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY` and optional `AWS_SESSION_TOKEN` in the environment (skipped when `profile=` is given explicitly)
2. static keys for the profile in `~/.aws/credentials` or `~/.aws/config` (`AWS_SHARED_CREDENTIALS_FILE` and `AWS_CONFIG_FILE` are honoured)
3. `aws configure export-credentials --profile <name>` when the AWS CLI v2 is installed, which resolves SSO sessions, assumed roles, `credential_process` and everything else the CLI supports

The profile is `profile=`, else `AWS_PROFILE`, else `default`. The region
is `region=`, else `AWS_REGION`, else `AWS_DEFAULT_REGION`, else the
profile's `region`; with none of those the request fails with a clear
message. `service=` defaults to `execute-api` (API Gateway); use `s3`,
`lambda`, `es`, `aoss`, `appsync` and so on for other services.

So on a developer machine with `aws sso login` done, or in CI with keys in
the environment, `# @auth aws` works with no apic-specific setup. What is
not covered without the CLI is instance-metadata and ECS task credentials;
install the CLI there or export keys into the environment.

The signature covers the method, path, query, the body hash and the headers
present when the request is signed, and the body is sent with
`X-Amz-Content-Sha256`. Signing happens after every `{{variable}}` is
substituted, so a signed request can still use captured values. If the CLI
reports an expired SSO session, its message (for example "run aws sso
login") is passed through.

## oauth2

```http
# @auth oauth2 tokenUrl={{tokenUrl}} clientId={{clientId}} clientSecret={{clientSecret}} scope="api.read api.write"
```

| Option | Meaning |
|---|---|
| `tokenUrl=` | Token endpoint. Required. |
| `clientId=` | Client id. Required. |
| `clientSecret=` | Client secret, if the client is confidential. |
| `grant=` | `client_credentials` (default), `password`, `device_code` or `authorization_code`. |
| `scope=` | Space-separated scopes; quote them. |
| `audience=` | Sent as `audience` (Auth0, some others). |
| `username=`, `password=` | For `grant=password`. |
| `deviceUrl=` | Device authorization endpoint, for `grant=device_code`. |
| `authUrl=` | Authorization endpoint, for `grant=authorization_code`. |
| `redirectPort=` | Loopback port for the redirect, for `grant=authorization_code`. Default: a free one. |
| `redirectUrl=` | The whole redirect URI instead, as the provider has it registered: `http://localhost:<port>/<path>`. apic listens on that port and path and sends the URI as written. |
| `clientAuth=` | `body` (default: `client_id`/`client_secret` in the form) or `basic` (HTTP basic auth on the token request). |

The access token is cached in the session (`.apic/session.json`) for the
current environment, keyed by token URL, client id, grant, scope, username
and audience. It is reused until a minute before it expires, then refreshed
with the refresh token when the server issued one, otherwise re-requested.
`apic session` shows the cached entries and their remaining lifetime;
`apic session clear` forgets them. `--no-session` fetches a fresh token on
every run.

See `examples/spotify/search.http` for `client_credentials` with
`clientAuth=basic` against the real Spotify Web API.

This covers Entra ID (Azure AD), Okta, Auth0, Keycloak, Cognito and most
other providers for machine-to-machine access, since they all speak the
client credentials grant.

### Device code

```http
# @auth oauth2 grant=device_code tokenUrl={{tokenUrl}} deviceUrl={{deviceUrl}} clientId={{clientId}} scope=openid
```

For a human at a terminal signing in as themselves. apic prints a URL and a
code on stderr, waits while you approve in a browser, then continues.
The token is cached like any other, so you sign in once per expiry. Do not
use this from an unattended agent; it will wait until the code expires.

### Authorization code with PKCE

```http
# @auth oauth2 grant=authorization_code authUrl={{authUrl}} tokenUrl={{tokenUrl}} clientId={{clientId}} scope="openid profile" redirectPort=8976
```

The flow behind "sign in as yourself" for most user-facing APIs (Google,
GitHub Apps, Entra ID delegated scopes), for providers that do not offer
device code. apic listens on a loopback port, opens the provider's
sign-in page in your browser (and prints the URL on stderr in case it
cannot), receives the code on the redirect, and exchanges it for a token:

```
To sign in, open https://login.example.com/authorize?client_id=...
(the redirect URI is http://127.0.0.1:8976/callback; register it with the provider if it asks)
Waiting for the browser...
```

PKCE (`S256`) is always on, so a public client needs no `clientSecret=`;
give one for a confidential client. `redirectPort=` fixes the port when
the provider wants the exact redirect URI registered
(`http://127.0.0.1:<port>/callback`); without it apic picks a free one.
The `state` of the redirect is checked, and the wait gives up after five
minutes.

The token is cached and refreshed exactly like the other grants, so the
browser is needed once per refresh-token lifetime. And it is a human
flow: under `--json`, from the MCP server, from `apic test`, or when
stdin or stderr is not a terminal, apic never starts a browser. With no
cached token the request then fails with exit 2 and a message saying to
run it once interactively; after that the cached token serves every
unattended run until the refresh token expires.

### JetBrains projects

A project set up for JetBrains HTTP Client keeps its OAuth2 settings in the
env files, under `Security.Auth`, and asks for a token in the request:

```json
// http-client.env.json
{
  "dev": {
    "Security": {
      "Auth": {
        "my-api": {
          "Type": "OAuth2",
          "Grant Type": "Client Credentials",
          "Token URL": "https://login.example.com/oauth2/token",
          "Client ID": "{{clientId}}",
          "Scope": "api.read",
          "Custom Request Parameters": {"audience": "api://my-api"}
        }
      }
    }
  }
}

// http-client.private.env.json
{"dev": {"Security": {"Auth": {"my-api": {"Client Secret": "…"}}}}}
```

```http
GET {{baseUrl}}/orders
Authorization: Bearer {{$auth.token("my-api")}}
```

apic runs those files as they are. `{{$auth.token("name")}}` runs the
configuration's grant through the same code and the same session cache as
`# @auth oauth2`, so a token is fetched once and refreshed as above, and
`{{$auth.idToken("name")}}` gives the ID token (ask for the `openid` scope).
The private file's fields are merged over the public file's, an
environment's over `$shared`'s, so the secret can live where it belongs.
The value is a secret: it is masked wherever the request is shown.

| JetBrains field | Maps to |
|---|---|
| `Type` | `OAuth2`; anything else is refused. |
| `Grant Type` | `Client Credentials`, `Password`, `Device Authorization` or `Authorization Code`. `Implicit` is refused (OAuth 2.1 drops it); use Authorization Code, which apic always runs with PKCE. |
| `Token URL`, `Auth URL`, `Device Auth URL` | `tokenUrl=`, `authUrl=`, `deviceUrl=` |
| `Client ID`, `Client Secret`, `Scope` | `clientId=`, `clientSecret=`, `scope=` |
| `Username`, `Password` | `username=`, `password=` for the Password grant |
| `Redirect URL` | `redirectUrl=`: apic listens there for the browser's redirect |
| `Client Credentials` | `basic` is `clientAuth=basic`; `in body` (the default) and `none` send the client id in the form |
| `Custom Request Parameters` | `audience` is sent; other parameters are reported by `apic validate` and not sent |
| `Use ID Token` | `$auth.token` gives the ID token |
| `PKCE`, `Acquire Automatically` | Accepted: apic always uses PKCE and fetches a token when a request needs one |

String fields may use `{{placeholders}}`. `apic env` lists the
configurations of the current environment with the spec each maps to
(secrets masked); `apic describe` names the configuration a placeholder
uses without fetching a token; `apic validate` reports a configuration apic
cannot use as `bad-auth-config` and each field it ignores as
`unknown-auth-key`.

## exec

```http
# @auth exec gcloud auth print-access-token
# @auth exec az account get-access-token --query=accessToken --output=tsv ttl=30m
# @auth exec op read op://vault/api/token header=X-Api-Key prefix=
```

Runs the command (directly, not through a shell) and uses its trimmed
stdout as the credential. Options after the command:

| Option | Meaning |
|---|---|
| `header=` | Header to set. Default `Authorization`. |
| `prefix=` | Text before the value. Default `Bearer`; `prefix=` (empty) sends the raw value. |
| `ttl=` | Cache the output in the session for this long, e.g. `ttl=30m`. Off by default. |

Because this executes commands found in a text file, and agents edit text
files, it is disabled unless the project opts in:

```yaml
# apic.yaml
auth:
  allowExec: true
```

Without it, `apic validate` warns and `apic run` refuses. Use `exec` for
Google Cloud, Azure CLI sessions, password managers, Vault, or any provider
apic does not know about.

## TLS and client certificates

An API behind a private CA, or an mTLS gateway that asks for a client
certificate, is configured in `apic.yaml`; paths are relative to the
project root and confined to it:

```yaml
tls:
  caFile: certs/internal-ca.pem     # trusted in addition to the system roots
  certFile: certs/client.pem        # presented when the server asks
  keyFile: certs/client-key.pem     # defaults to certFile when both are in one file
  verifyHost: true                  # the default; --insecure turns it off for one command
  hosts:                            # per-host overrides, by name or *.suffix
    api.internal.example.com: {certFile: certs/internal.pem, keyFile: certs/internal-key.pem}
    "*.dev.example.com": {verifyHost: false}
```

For one command, `--cacert`, `--cert` and `--key` override all of that,
and `--insecure` still means what it means. A JetBrains project needs no
change: the `SSLConfiguration` block in `http-client.env.json` or the
private file is read for the selected environment (`clientCertificate`
with `path` and `keyPath`, and `verifyHostCertificate`); a key marked
`hasCertificatePassphrase` is refused, since apic never prompts. The
precedence is the flags, then a matching `tls.hosts` entry, then the
environment's `SSLConfiguration`, then `tls`.

Keys are PEM. A key file anyone on the machine can read is refused with
its mode, the way ssh refuses one: `chmod 600` it. PKCS#12 bundles are not
read; convert with `openssl pkcs12 -in client.p12 -out client.pem -nodes`.

`apic describe` shows a `tls` line when a certificate, a CA or `--insecure`
applies to the request's host, `run -v` prints the same line, `--json`
carries it as `request.tls` (`ca_file`, `cert_file`, `key_file`,
`insecure`), and `apic curl` maps it to `--cacert`, `--cert`, `--key` and
`--insecure`. OAuth2 token endpoints use the project-wide settings, not a
host override.

## curl export

`apic curl <id>` maps auth onto curl where curl has an equivalent:

| Type | curl flags |
|---|---|
| `bearer` | `-H 'Authorization: Bearer <token>'` |
| `basic` | `--user 'user:password'` |
| `apikey` | `-H 'X-Api-Key: key'` (or the header named), or `--url-query 'name=key'` for the query form (curl 7.87 or newer) |
| `digest` | `--digest --user 'user:password'` |
| `aws` | `--aws-sigv4 'aws:amz:<region>:<service>' --user "$AWS_ACCESS_KEY_ID:$AWS_SECRET_ACCESS_KEY"` plus the session token header |
| `oauth2` | a placeholder `$TOKEN` header with a comment naming the token URL |
| `exec` | `-H "Authorization: Bearer $(command)"` |

## For agents

`describe_request` and `apic describe --json` include `auth` (the spec
template) and `auth_source` (`request` or `apic.yaml`). Cached tokens
never appear in `list_environments`, `env` or `describe` output, and the
credentials apic adds (the `Authorization` header, signatures, tokens) are
never part of `run` output: they are set on the wire only. An agent never
needs to handle credentials itself: with `aws`, `oauth2` or `exec`
configured, `run_request` just works.

## What appears in output

Headers you write yourself are shown in `run -v` and `--json`, except that
`Authorization`, `Proxy-Authorization`, `Cookie`, `X-Api-Key`,
`X-Auth-Token`, `Api-Key`, `X-Amz-Security-Token` and any header whose
value came from a secret source (the private env file, `.env`, the session
or a capture) are shown as `***`. URL, body and captured values are shown
in full so scripts can use them. Pass `--redact` to mask every header
value, the body, query-string values and captures, which is the right
setting for CI logs that are stored.
