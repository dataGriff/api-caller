# Licence

apic is released under the **MIT Licence**. The full text lives in
[`LICENSE`](https://github.com/dataGriff/api-caller/blob/main/LICENSE) in the
repository, and a copy ships inside every release archive.

In short: use it, change it, ship it inside your own products, commercial or
not. Keep the copyright notice with any substantial copy of the source, and
understand that it comes with no warranty.

## Third-party modules

apic is a single static binary, so the open source modules it is built from
travel inside it. Each keeps its own licence, and all of them are permissive:

| Licence | Modules |
|---|---|
| MIT | the majority, including cobra's dependency tree, godog, gjson, lipgloss, termenv and `gopkg.in/yaml.v3` |
| BSD-3-Clause | `golang.org/x/...`, `github.com/google/uuid`, `spf13/pflag`, `yosida95/uritemplate` |
| Apache-2.0 | `spf13/cobra`, `modelcontextprotocol/go-sdk` |
| MPL-2.0 | the HashiCorp modules godog pulls in (`go-memdb`, `go-immutable-radix`, `golang-lru`) |

Nothing in the tree is copyleft in a way that reaches your use of apic: MPL-2.0
is file-level and applies only to those modules' own files, which apic does not
modify.

Every release archive contains `THIRD_PARTY_NOTICES.md`, which reproduces each
module's licence in full, along with the `NOTICE` and `PATENTS` files some of
them ship. Regenerate it from a checkout at any time:

```sh
task notices
```

The generator walks the module set for **every** released platform, so the
notices are complete whichever archive you downloaded.
