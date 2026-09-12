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
| `oauth2` | `# @auth oauth2 tokenUrl=.. clientId=.. ...` | Client credentials, password or device code grant; token cached and refreshed |
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

## aws

```http
# @auth aws
# @auth aws service=s3 region=us-east-1
# @auth aws profile=prod
```

Requests are signed with AWS Signature Version 4. Credentials and the
region come from the AWS SDK's default chain, in this order:

1. `AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY` and optional `AWS_SESSION_TOKEN`
2. the shared config and credentials files (`~/.aws/config`, `~/.aws/credentials`), using `AWS_PROFILE` or `profile=`
3. SSO sessions (`aws sso login`), assumed roles, web identity tokens
4. ECS task and EC2 instance metadata

So whatever already works for the AWS CLI works for apic with no extra
configuration. `region=` overrides `AWS_REGION` and the profile's region;
if none is set the request fails with a clear message. `service=` defaults
to `execute-api` (API Gateway); use `s3`, `lambda`, `es`, `aoss`,
`appsync` and so on for other services.

The signature covers the method, path, query, the body hash and the headers
present when the request is signed, and the body is sent with
`X-Amz-Content-Sha256`. Signing happens after every `{{variable}}` is
substituted, so a signed request can still use captured values.

## oauth2

```http
# @auth oauth2 tokenUrl={{tokenUrl}} clientId={{clientId}} clientSecret={{clientSecret}} scope="api.read api.write"
```

| Option | Meaning |
|---|---|
| `tokenUrl=` | Token endpoint. Required. |
| `clientId=` | Client id. Required. |
| `clientSecret=` | Client secret, if the client is confidential. |
| `grant=` | `client_credentials` (default), `password` or `device_code`. |
| `scope=` | Space-separated scopes; quote them. |
| `audience=` | Sent as `audience` (Auth0, some others). |
| `username=`, `password=` | For `grant=password`. |
| `deviceUrl=` | Device authorization endpoint, for `grant=device_code`. |
| `clientAuth=` | `body` (default: `client_id`/`client_secret` in the form) or `basic` (HTTP basic auth on the token request). |

The access token is cached in the session (`.apic/session.json`) for the
current environment, keyed by token URL, client id, grant, scope, username
and audience. It is reused until a minute before it expires, then refreshed
with the refresh token when the server issued one, otherwise re-requested.
`apic session` shows the cached entries and their remaining lifetime;
`apic session clear` forgets them. `--no-session` fetches a fresh token on
every run.

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

Authorization code and PKCE flows, which need a browser redirect back to a
local port, are not supported.

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

## curl export

`apic curl <id>` maps auth onto curl where curl has an equivalent:

| Type | curl flags |
|---|---|
| `bearer` | `-H 'Authorization: Bearer …'` |
| `basic` | `--user 'user:password'` |
| `aws` | `--aws-sigv4 'aws:amz:<region>:<service>' --user "$AWS_ACCESS_KEY_ID:$AWS_SECRET_ACCESS_KEY"` plus the session token header |
| `oauth2` | a placeholder `$TOKEN` header with a comment naming the token URL |
| `exec` | `-H "Authorization: Bearer $(command)"` |

## For agents

`describe_request` and `apic describe --json` include `auth` (the spec
template) and `auth_source` (`request` or `apic.yaml`). Cached tokens
never appear in `list_environments`, `env` or `describe` output. An agent
never needs to handle credentials itself: with `aws`, `oauth2` or `exec`
configured, `run_request` just works.
