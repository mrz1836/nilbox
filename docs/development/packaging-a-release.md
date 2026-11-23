# Packaging a release

[Docs](../README.md) › [Development](../README.md#development) · See also: [Publishing a release](publishing-a-release.md)

`magex app:package` builds the files a GitHub release of the app attaches, on a Mac, into
`bin/release/<version>/`:

| File | What it is |
|---|---|
| `Nilbox_<version>_darwin_universal.zip` | The bundle, zipped with `ditto -c -k --norsrc --keepParent`: what updates install. The target checks that it holds exactly `Nilbox.app`, with nothing beside it |
| `Nilbox.dmg` | The disk image first installs open: the bundle beside a link to Applications, compressed as ULMO (LZMA, about a third smaller than UDZO), and signed with the same identity. Its name holds no version, so a link to the latest release's copy never changes |
| `SHA256SUMS` | The SHA-256 of the zip and the disk image, for checking a download by hand (`shasum -a 256 -c SHA256SUMS`) |
| `manifest-darwin.json` | The update manifest: the version, the platform (`darwin-universal`), the zip's name, size, and SHA-512, and an Ed25519 signature over all of them |

It reads four environment variables:

| Variable | Meaning |
|---|---|
| `RELEASE_VERSION` | Required: the version without its `v`, such as `0.3.0` or `0.3.0-rc.1`. Its tag, `v<version>`, must name `HEAD`'s commit (other tags may sit beside it, as a release's does beside its last candidate's), and the working tree must hold no changes, untracked files included, or the target stops |
| `CODESIGN_IDENTITY` | Required: the code-signing identity, by its name or its SHA-1. Never `-`: a release is not signed ad hoc |
| `CODESIGN_KEYCHAIN` | Optional: the absolute path of a keychain file that holds the identity, such as CI's temporary one. The same path goes to `security` and `codesign`, so a relative one is refused |
| `UPDATE_SIGNING_KEY` | Required: the path to the release key's private half, the PEM file `magex app:keygen` wrote |

Before it builds anything, it checks the version, the identity, the keychain, that the private
key is the other half of the key the app pins (`update.ReleaseKey`), the tag, and that the
working tree is clean, untracked files included, whatever your git config hides from `git
status`. Then it checks the brand files, builds the frontend, and builds the desktop binary
twice, for `arm64` and `amd64`, with `-X main.flavor=release`, combining the two with `lipo` and
checking `lipo -archs`. The bundle's `CFBundleShortVersionString` is the version's numeric core
(`0.3.0` for `0.3.0-rc.1`), which is what macOS expects; the version the app reports keeps the
whole. Every folder and executable in the bundle gets mode 0755 and every other file 0644,
whatever the umask, so the zip and the disk image hold the same modes on any machine. It signs
the bundle without a timestamp (`--timestamp=none`, which asks no server), verifies it
(`codesign --verify --deep --strict`), zips it and checks the zip (one top-level bundle, every
name clean and listed once, and no links), makes, signs, and verifies the disk image, writes
`SHA256SUMS`, then signs the manifest and verifies it against the pinned key, so a private key
that doesn't match the app's fails here, never in a user's hands. Last, it checks the files as
`magex app:verify` does and prints each one's size. While no release key is pinned, it refuses
with "no release key pinned yet".

## In two halves

`app:package` is `magex app:buildRelease`, then `magex app:signRelease`, in one target, and each
half also runs on its own, which is how the release workflow keeps every secret away from the
build's tools.

- `app:buildRelease` reads `RELEASE_VERSION` alone. It checks the version, the tag, and the
  working tree as `app:package` does, refuses while no release key is pinned, and builds the
  universal bundle, unsigned, alone in `bin/unsigned/<version>/`. It needs no secret.
- `app:signRelease bundle=<path>` takes the absolute path of that bundle, outside
  `bin/release/<version>/`, and reads all four variables. It makes every check `app:package`
  makes but the build's, then checks the bundle before it touches the keychain: named
  `Nilbox.app`, alone in its folder, only folders and regular files in it (no link, the one way
  out of it), its `Info.plist` and binary there, not signed yet, and a binary `lipo -archs` reads
  both architectures in. It signs a copy in `bin/release/<version>/`, leaving the unsigned bundle
  as it was, sets the modes before signing as after (a build artifact keeps none), and builds
  the other files as `app:package` does.

## The keychain search list

`codesign` finds an identity only in a keychain on the user's search list, even one named with
`--keychain`. With `CODESIGN_KEYCHAIN` set, the target puts that keychain first on the list once the
bundle is built, while it signs, and puts the list back exactly as it was when it ends, fails, or is
interrupted. On Control-C the target then stops as the build's tools do and cleans up after itself;
on `SIGTERM` or `SIGHUP` it ends at once, and the next run clears what it left in the release's
folder. It never looks for the identity with `security find-identity -v`, which counts a self-signed
identity as not valid though it signs.

## The release key

`magex app:keygen out=<folder>` writes a new Ed25519 private key to `<folder>/release.key` (PKCS#8
PEM, readable by you alone) and prints only its public half, in base64, to pin as
`update.ReleaseKey`. It refuses a folder inside the repository, comparing folders as files, so
another case of the repository's name, a link, or a firmlink doesn't get past it, and never
overwrites a key, and `.gitignore` keeps `*.key`, `release.key`, and `*.p12` out all the same. Keep
the private key backed up: without it, no install can verify an update again. Setting releases up
from the start, and replacing the key or the certificate, are in [Publishing a
release](publishing-a-release.md).

`magex app:verify dir=<folder>` checks a release's folder as the app will: its
`manifest-darwin.json` (at most 16 KiB, no field the format lacks, every field in form, for
`darwin-universal`, and signed by the pinned key), then the zip it names, by its size and
SHA-512. With `version=<version>` (its `v` optional), it also refuses a manifest of any other
version, so a stale pair can't pass for this release.
