# Example projects

Static `.http` projects, each runnable with `apic run -C examples/<name>` or
`apic ui -C examples/<name>`. All three are validated (structure only, no
network) by CI's `validate-examples` job.

| Project | Auth demonstrated | What you need |
|---|---|---|
| [`httpbin`](httpbin) | `basic`, `bearer` | Nothing but network — httpbin.org needs no account |
| [`github`](github) | `bearer` | A GitHub personal access token in `github/http-client.private.env.json` |
| [`spotify`](spotify) | `oauth2` (`client_credentials`) | A Spotify app's client id/secret in `spotify/http-client.private.env.json` |

Prefer zero setup? `apic demo` scaffolds and serves its own fake API
offline — see the root README.
