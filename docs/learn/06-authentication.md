# Lesson 6: Authentication

**Goal.** Attach credentials to requests with `# @auth`, per request and
per project, and know what apic shows and what it hides.

## You will need

- apic installed ([lesson 1](01-first-request.md))
- `apic demo` running in another terminal, or start it now:

```sh
apic demo --out apic-demo
```

The demo API accepts any bearer token, HTTP basic auth for `alice`, an
API key, and a client-credentials token endpoint, so every kind of auth in
this lesson runs offline. The two real providers at the end are prose.

## Steps

### 1. Bearer, by hand and by directive

You have been doing auth since lesson 1. `whoami` writes the header
itself:

```http
GET {{baseUrl}}/me
Authorization: Bearer {{token}}
```

The directive form does the same thing:

```http
# @auth bearer {{token}}
GET {{baseUrl}}/me
```

Two differences. A directive can be a project default (step 5), so you do
not repeat the header in every request. And whatever apic adds for you is
never printed: only headers you wrote yourself appear in `-v`, and even
those are masked when they carry a credential:

<!-- learn -->
```sh
apic run login -C apic-demo > /dev/null
apic run whoami -C apic-demo -v | head -3
```

```
GET http://localhost:8089/me
Authorization: ***

```

(The login first is so that `whoami`'s `# @ref login` has nothing to do;
on a fresh session it would run `login` and print that report before its
own.)

### 2. Basic

Basic auth is the one you cannot write as a plain header, because the
value is base64 of `user:password`. Let apic do it:

```http
# @auth basic {{user}} {{password}}
GET {{baseUrl}}/basic-auth/{{user}}/{{password}}
```

<!-- learn -->
```sh
apic run basic-auth -C apic-demo
```

```
GET http://localhost:8089/basic-auth/alice/s3cret
200 OK · 1 ms · 23 B

{
  "authenticated": true
}

✓ status == 200
✓ body.$.authenticated == true
```

`apic curl` maps every auth type onto curl where curl has an equivalent,
which for basic is `--user`:

<!-- learn -->
```sh
apic curl basic-auth -C apic-demo
```

```
curl -sS \
  --user 'alice:s3cret' \
  'http://localhost:8089/basic-auth/alice/s3cret'
```

### 3. An API key

An API key is a header with a secret in it. Write the header, keep the
value in the private file, and apic masks it because of where it came
from:

```http
GET {{baseUrl}}/keyed
X-Api-Key: {{apiKey}}
```

<!-- learn -->
```sh
apic run api-key -C apic-demo -v | head -3
```

```
GET http://localhost:8089/keyed
X-Api-Key: ***

```

`Authorization`, `X-Api-Key`, `Cookie` and a few other header names are
always masked, and so is any header whose value came from a secret source:
the private env file, `.env`, the session, a capture, `--var` or
`APIC_VAR_`.

### 4. OAuth2 client credentials

Machine-to-machine OAuth2 is one line. The demo has a token endpoint:

```http
# @auth oauth2 tokenUrl={{baseUrl}}/oauth/token clientId={{clientId}} clientSecret={{clientSecret}}
GET {{baseUrl}}/me
```

`describe` shows the spec and whether its variables resolve, before
anything is sent:

<!-- learn -->
```sh
apic describe oauth2-token -C apic-demo
```

```
GET {{baseUrl}}/me
Client-credentials token, fetched and cached automatically
file: auth.http:34

auth
  oauth2 tokenUrl={{baseUrl}}/oauth/token clientId={{clientId}} clientSecret={{clientSecret}} (request)

variables
  ✓ baseUrl       http://localhost:8089  http-client.env.json [local]
  ✓ clientId      demo-client  auth.http:2 @clientId
  ✓ clientSecret  ***  http-client.private.env.json [local]
```

Run it, then look at the session:

<!-- learn -->
```sh
apic run oauth2-token -C apic-demo > /dev/null
apic session -C apic-demo
```

```
local
  ↳ $oauth2:ba9f21f030e5a6f3 = token, expires in 1h0m0s
  ↳ token = mock-token
```

apic fetched a token from the token endpoint, sent the request with it,
and cached the token in the session keyed by token URL, client id, grant
and scope. Every later request with the same spec reuses it until a minute
before it expires, then refreshes it. `apic session clear` forgets it;
`--no-session` fetches a fresh one every time.

In `--json`, the request records which auth type applied and nothing else
about it:

<!-- learn -->
```sh
apic run oauth2-token -C apic-demo --json | grep -o '"auth":"[a-z0-9]*"'
```

```
"auth":"oauth2"
```

**A real provider.** The repository's `examples/spotify` project does this
against the Spotify Web API with `grant=client_credentials
clientAuth=basic`, the client id and secret in the private file, and
`apic session` showing the cached token with its lifetime. Entra ID, Okta,
Auth0, Keycloak and Cognito all speak the same grant, so the line is the
same with a different `tokenUrl`.

**A human at a keyboard.** `grant=device_code` prints a URL and a code,
waits while you approve in a browser, and caches the token like any other.
It is for people, not agents: an unattended run would wait until the code
expired. Authorization-code flows with a browser redirect are not
supported.

### 5. A project default, and opting out

Most projects authenticate every request the same way. Say it once in
`apic.yaml` instead of on each request. Make a small project beside the
demo to see it (the demo API accepts any bearer token, so a fixed one in
the private file will do here):

<!-- learn -->
```sh
base=$(grep -o 'http://[^"]*' apic-demo/http-client.env.json | head -1)
mkdir -p apic-auth
cat > apic-auth/http-client.env.json <<EOF
{ "local": { "baseUrl": "$base" } }
EOF
cat > apic-auth/http-client.private.env.json <<'EOF'
{ "local": { "token": "mock-token" } }
EOF
cat > apic-auth/apic.yaml <<'EOF'
env: local
auth:
  default: bearer {{token}}
EOF
cat > apic-auth/api.http <<'EOF'
### Uses the project default
# @name me
# @assert status == 200
GET {{baseUrl}}/me

### Public, so it opts out
# @name health
# @auth none
# @assert status == 200
GET {{baseUrl}}/health
EOF
apic run me health -C apic-auth | tail -4
```

```
✓ me      200  1 ms
✓ health  200  0 ms

2 passed · 2 requests · 1 ms
```

`me` has no auth line and no `Authorization` header, and was sent with the
bearer token from the default. `health` said `# @auth none` and was sent
with nothing. A request with its own `# @auth basic …` would use that
instead. `describe` names the source:

<!-- learn -->
```sh
apic describe me -C apic-auth | sed -n 5,6p
```

```
auth
  bearer {{token}} (apic.yaml)
```

Because the default may hold `{{variables}}`, one project can point at a
different identity provider per environment: `tokenUrl` and `clientId` in
the public env file, `clientSecret` in the private one, and a default of
`oauth2 tokenUrl={{tokenUrl}} clientId={{clientId}} clientSecret={{clientSecret}}`.

### 6. A token from any command

When the credential comes from a tool (`gcloud`, `az`, `op`, `vault`),
`# @auth exec` runs it and uses its output:

<!-- learn -->
```sh
cat >> apic-auth/api.http <<'EOF'

### A token from a command
# @name me-exec
# @auth exec echo mock-token
# @assert status == 200
GET {{baseUrl}}/me
EOF
apic validate -C apic-auth
apic run me-exec -C apic-auth || echo "exit $?"
```

```
api.http:14:9: warning: @auth exec will be refused until apic.yaml sets auth.allowExec: true (exec-disabled)
! 1 file, 3 requests, 1 warning
GET {{baseUrl}}/me
error: api.http:16: auth: @auth exec is disabled: set `auth:
  allowExec: true` in apic.yaml to let request files run "echo mock-token"
exit 2
```

Refused, on purpose: a request file that runs commands deserves a
deliberate yes, because agents edit request files. The project opts in:

<!-- learn -->
```sh
cat > apic-auth/apic.yaml <<'EOF'
env: local
auth:
  default: bearer {{token}}
  allowExec: true
EOF
apic run me-exec -C apic-auth | head -2
```

```
GET http://localhost:8089/me
200 OK · 1 ms · 30 B
```

The command runs directly, not through a shell, and its trimmed output
becomes the `Authorization: Bearer …` header; `header=` and `prefix=`
change that, and `ttl=30m` caches the output in the session.

### 7. AWS Signature V4

Signing a request for API Gateway, S3 or Lambda is one directive, and apic
finds the keys the way the AWS CLI does: the environment, then
`~/.aws/credentials`, then the CLI itself for SSO sessions and assumed
roles. There is no AWS SDK inside apic; the signature is implemented and
checked against the official test vectors.

```http
# @auth aws service=execute-api region=eu-west-2
GET {{baseUrl}}/orders
```

Without an AWS account, `apic curl` still shows what the signed request
would be, with fake keys in the environment:

<!-- learn -->
```sh
cat >> apic-auth/api.http <<'EOF'

### Signed for API Gateway
# @name signed
# @auth aws service=execute-api region=eu-west-2
GET {{baseUrl}}/health
EOF
AWS_ACCESS_KEY_ID=AKIAEXAMPLE AWS_SECRET_ACCESS_KEY=example apic curl signed -C apic-auth
```

```
curl -sS \
  --aws-sigv4 'aws:amz:eu-west-2:execute-api' \
  --user "$AWS_ACCESS_KEY_ID:$AWS_SECRET_ACCESS_KEY" \
  ${AWS_SESSION_TOKEN:+-H} \
  ${AWS_SESSION_TOKEN:+x-amz-security-token:$AWS_SESSION_TOKEN} \
  'http://localhost:8089/health'
```

Against a real API Gateway with a real key pair, `apic run signed` sends
the `Authorization: AWS4-HMAC-SHA256 …` header, `X-Amz-Date` and
`X-Amz-Content-Sha256`, and shows none of them in its output. The
[cookbook](../cookbook.md#aws-api-gateway-with-sigv4) has the full recipe
and the [FAQ](../faq.md#my-aws-request-comes-back-403) the usual reasons
for a 403.

## Checkpoint

The OAuth2 request goes through the whole flow, token endpoint included,
and reports the auth type it used:

<!-- learn -->
```sh
apic run oauth2-token -C apic-demo --json | grep -o '"auth":"oauth2"'
```

```
"auth":"oauth2"
```

## Exercise

Move the demo project onto a default. Set `auth.default: bearer {{token}}`
in `apic-demo/apic.yaml`, delete every `Authorization: Bearer {{token}}`
header from `todos.http`, and make `apic run todos.http --keep-going` and
`apic test` pass as before. One request will need to opt out; find it.

??? example "Solution"
    `apic-demo/apic.yaml`:

    ```yaml
    env: local
    auth:
      default: bearer {{token}}
    ```

    Delete the `Authorization:` line from each request in `todos.http`.
    Then run `apic run todos.http -C apic-demo --keep-going` and it fails
    at once: `login` now needs `{{token}}` too, because the default
    applies to every request without its own `# @auth`, and `login` is the
    request that produces the token. It opts out:

    ```http
    ### Log in and keep the token
    # @name login
    # @auth none
    ```

    Now the flow shows `1 failed, 11 passed` (the deliberate `not-found`)
    and `apic test -C apic-demo` passes every scenario. Requests with their
    own `# @auth`, like `basic-auth` and `oauth2-token`, were never
    affected: a request's own directive wins over the default.

## Going further

- [Authentication](../auth.md), every type and option
- [OAuth2 client credentials](../cookbook.md#oauth2-client-credentials) and
  [AWS API Gateway with SigV4](../cookbook.md#aws-api-gateway-with-sigv4) in the cookbook
- [What appears in output](../auth.md#what-appears-in-output)

??? note "Episode script"
    **Length.** 14 minutes. The Spotify and AWS segments are prerecorded
    and short.

    **Cold open (0:00).** `apic run whoami -v` with `Authorization: ***`.
    "apic will add a credential to a request five different ways, and
    never print one. Here are all five."

    **Talking points.**

    1. Bearer by hand and by directive; why the directive exists.
    2. Basic on the demo, and `apic curl` turning it into `--user`.
    3. The API key header, masked because of where the value came from.
    4. OAuth2: `describe`, run, `apic session` with the cached token and
       its lifetime, `"auth":"oauth2"` in JSON. Cut to Spotify for thirty
       seconds: the same line, a real token.
    5. A project default in `apic.yaml`, `# @auth none`, `describe`
       naming the source.
    6. `exec`: refused, then allowed, and why it is off by default.
    7. AWS: the directive, `apic curl` with fake keys, then a prerecorded
       call to a real API Gateway.
    8. Checkpoint, then the exercise: the default on the demo and the one
       request that must opt out.

    **Shot list.** One terminal, 100x30, font size 16;
    `docs/learn/tapes/06.tape` reproduces the demo-only parts. Picture in
    picture for the Spotify developer dashboard and the AWS console.

    **Chapters.** `0:00 Five ways, never printed` · `0:50 Bearer` · `2:00
    Basic` · `3:00 API keys` · `4:00 OAuth2` · `7:00 A project default` ·
    `8:40 exec` · `10:20 AWS SigV4` · `12:30 Checkpoint and exercise`.

    **Description.** From the [episode template](_episode-template.md).
