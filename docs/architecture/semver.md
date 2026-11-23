# internal/semver

[Docs](../README.md) › [Architecture](../README.md#architecture) · Code: [`internal/semver`](../../internal/semver)

`internal/semver/` reads semantic versions and compares them by precedence (`Parse`,
`Compare`, `Version.Prerelease`), held to semver 2.0 (no leading zero in a number, a
prerelease's numeric identifiers included), for the integrations' tool versions and the
app's updates alike. It is a leaf.
