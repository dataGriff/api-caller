# Verifying a release

Every apic release ships three things that let you check a download before you
trust it:

| File | What it is |
|---|---|
| `checksums.txt` | SHA-256 of every archive in the release |
| `checksums.txt.sig` + `checksums.txt.pem` | a [cosign](https://docs.sigstore.dev/) signature over `checksums.txt`, and the certificate it was made with |
| `<archive>.sbom.json` | an SPDX 2.3 SBOM listing every Go module compiled into that archive |

`install.sh` already checks the archive against `checksums.txt` and refuses to
install on a mismatch. That catches a corrupted or truncated download. It does
**not** prove who produced the release — for that, verify the signature.

## Verify the signature

Signing is keyless: there is no apic private key. cosign gets a short-lived
certificate from Sigstore's CA, bound to the identity of the GitHub Actions
workflow that built the release, and records the signature in a public
transparency log. Verifying checks that chain.

You need [cosign](https://github.com/sigstore/cosign#installation).

```sh
VERSION=v0.1.0
BASE="https://github.com/dataGriff/api-caller/releases/download/$VERSION"

curl -fsSLO "$BASE/checksums.txt"
curl -fsSLO "$BASE/checksums.txt.sig"
curl -fsSLO "$BASE/checksums.txt.pem"

cosign verify-blob checksums.txt \
  --signature checksums.txt.sig \
  --certificate checksums.txt.pem \
  --certificate-identity-regexp '^https://github\.com/dataGriff/api-caller/\.github/workflows/release\.yml@refs/tags/' \
  --certificate-oidc-issuer 'https://token.actions.githubusercontent.com'
```

Expect `Verified OK`.

The two `--certificate-*` flags are the point of the exercise, so do not drop
them: they say *this was signed by the release workflow in this repository,
running on a tag*. Without them cosign will happily confirm that **somebody**
signed the file.

Then check your archive against the file you just verified:

```sh
sha256sum --ignore-missing -c checksums.txt   # macOS: shasum -a 256 -c
```

That chain — signature vouches for `checksums.txt`, `checksums.txt` vouches for
the archive — is why only one file needs signing.

## Verify the container image

The image `ghcr.io/datagriff/apic` is signed by the same workflow, keyless,
so the same two identity flags apply:

```sh
cosign verify ghcr.io/datagriff/apic:0.1.0 \
  --certificate-identity-regexp '^https://github\.com/dataGriff/api-caller/\.github/workflows/release\.yml@refs/tags/' \
  --certificate-oidc-issuer 'https://token.actions.githubusercontent.com'
```

cosign prints the verified signatures as JSON, and exits non-zero if there
are none from that identity. To pin what you run to what you verified,
use the digest it reports: `ghcr.io/datagriff/apic@sha256:…`.

The image also carries an SBOM attestation, which buildx attached when it
built it:

```sh
docker buildx imagetools inspect ghcr.io/datagriff/apic:0.1.0 --format '{{ json .SBOM }}'
```

## Read the SBOM

Each archive has a matching `<archive>.sbom.json` listing the module versions
compiled into it, which is more reliable than reading this repository's
`go.mod`: it describes the artifact you actually downloaded.

```sh
curl -fsSLO "$BASE/apic_0.1.0_linux_amd64.tar.gz.sbom.json"

# What is in it, and at which version?
jq -r '.packages[] | "\(.name) \(.versionInfo)"' apic_0.1.0_linux_amd64.tar.gz.sbom.json | sort
```

The module set differs by platform — cobra pulls in `mousetrap` on Windows only
— which is why the SBOM is per archive rather than per release.

Tools that consume SPDX (Grype, Trivy, Dependency-Track) can scan it directly:

```sh
grype sbom:apic_0.1.0_linux_amd64.tar.gz.sbom.json
```

## Licences

Every archive also contains `THIRD_PARTY_NOTICES.md`, reproducing the full
licence text of every module compiled into that binary, alongside apic's own
`LICENSE`. Regenerate it from a checkout with `task notices`.

## If verification fails

A checksum mismatch or a failed signature on a file you downloaded from the
releases page is worth reporting — see [SECURITY.md](https://github.com/dataGriff/api-caller/blob/main/SECURITY.md).
Please do not open a public issue for it.
