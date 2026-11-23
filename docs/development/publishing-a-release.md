# Publishing a release

Each release is built, signed, and published by
[`desktop-release.yml`](../../.github/workflows/desktop-release.yml) on macOS runners. It is the one
workflow that may use macOS: a repository test (`TestNoMacOSRunners`) holds every other to
ubuntu, and this one to a single pinned `macos-<version>` label. GoFortress still runs every
check on the tag, but publishes nothing itself (`ENABLE_RELEASE=false` in
`.github/env/90-project.env`), so a release never shows without its files. Another repository
test (`TestGoFortressReadsEnableRelease`) fails while that line is set but GoFortress's synced
`fortress.yml` and `fortress-setup-config.yml` don't read `ENABLE_RELEASE`: until they do,
GoFortress would publish a tag's release before its files exist, and an immutable release
could never get them.

**Before the first tag** (and to set it all up again). Keep the keys in a folder outside the
repository that only you can read, such as `~/release-keys`.

1. **The release key** signs each release's update manifest. Every installed copy trusts the
   public half its build pins, and nothing else.

   ```bash
   mkdir -m 700 ~/release-keys
   magex app:keygen out=$HOME/release-keys
   ```

   It writes the private half to `release.key` and prints the public half. Pin that line as
   `update.ReleaseKey` in `internal/update/key.go`, keeping the `// gitleaks:allow` at the end
   of its line (the secret scan would take the public key for a secret), and its SHA-256 as
   `pinnedTrustRoot` in `internal/update/releasekey_test.go`, two deliberate edits a test holds
   together, in a pull request:

   ```bash
   printf %s '<the public key line>' | shasum -a 256
   ```

   While no key is pinned, a release build's updates are off and the release targets refuse.
2. **The code-signing certificate**, self-signed and valid for ten years, gives every release
   the same identity, so macOS keeps the permissions a user granted across updates. Use macOS's
   own `/usr/bin/openssl`; OpenSSL 3 or newer needs `-legacy` on the second command, or
   `security import` refuses the file.

   ```bash
   cd ~/release-keys
   /usr/bin/openssl req -x509 -newkey rsa:2048 -sha256 -days 3650 -nodes \
     -keyout sign.key -out sign.crt -subj "/CN=Nilbox Release Signing" \
     -addext "basicConstraints=critical,CA:false" -addext "keyUsage=critical,digitalSignature" \
     -addext "extendedKeyUsage=critical,codeSigning"
   /usr/bin/openssl pkcs12 -export -inkey sign.key -in sign.crt -name "Nilbox Release Signing" -out sign.p12
   ```

   The second command asks for the `.p12` file's password. To sign local builds with the same
   identity, import `sign.p12` into your login keychain too.
3. Create the `release` environment (Settings → Environments → New environment). Under
   **Deployment branches and tags**, choose **Selected branches and tags** and add one **Tag**
   rule, `v*`, and nothing else. Create it before any tag is pushed: when a workflow names an
   environment that doesn't exist, GitHub creates it with no rule at all.
4. Add its secrets and its variable:

   | Name | Kind | What it is |
   |---|---|---|
   | `UPDATE_SIGNING_KEY` | Secret | The release key's private half, the PEM file `magex app:keygen` wrote |
   | `CODESIGN_P12` | Secret | The code-signing certificate and its key, a `.p12` file in base64. Export it with macOS's `/usr/bin/openssl`, or with `-legacy` from OpenSSL 3 or newer, or `security import` refuses it |
   | `CODESIGN_P12_PASSWORD` | Secret | The `.p12` file's password |
   | `CODESIGN_IDENTITY` | Variable | The code-signing identity's name (or SHA-1), passed to `app:signRelease` |

   ```bash
   gh secret set UPDATE_SIGNING_KEY --env release --repo mrz1836/nilbox < ~/release-keys/release.key
   base64 -i ~/release-keys/sign.p12 | gh secret set CODESIGN_P12 --env release --repo mrz1836/nilbox
   gh secret set CODESIGN_P12_PASSWORD --env release --repo mrz1836/nilbox
   gh variable set CODESIGN_IDENTITY --env release --repo mrz1836/nilbox --body "Nilbox Release Signing"
   ```

   The third command asks for the `.p12` file's password. `gh secret list --env release` and
   `gh variable list --env release` (with `--repo`) show what is set, never the values.
5. **Back up** `release.key`, `sign.p12`, and its password, in a password manager. Without
   `release.key`, no installed copy can verify an update again (see *Replacing the release key
   or the certificate*, below); a certificate is easier to replace.
6. Recommended: a tag ruleset on `refs/tags/v*` (Settings → Rules → Rulesets) that limits who
   may create, update, or delete release tags. Anyone who can push a `v*` tag runs this
   workflow, as the tagged commit holds it, with the environment's secrets.
7. Recommended: under Settings → Actions → General, require actions to be pinned to a
   full-length commit SHA, as every workflow here pins them.

**How to release.**

1. Merge to `master`.
2. Tag it from the app's own [Release](../guide/release.md) page, or with `magex version:bump bump=patch
   push=true`. A tag with a suffix, such as `v0.4.0-rc.1`, is a prerelease.
3. The tag starts two workflows at once: **GoFortress** runs every check, and **Desktop
   release** builds the app meanwhile.
4. Once GoFortress passes, Desktop release signs the app, drafts the release, attaches the zip,
   the disk image, `SHA256SUMS`, and the update manifest, checks them on GitHub, and publishes
   it, all together.
5. The app's Release page follows both runs and says *published* at that moment, when the update
   is live.

**What the workflow does,** on a `v*` tag and nothing else, in four jobs:

1. **Check the tag** (ubuntu, read-only). The tag must be a release version as the app reads
   one (`update.ValidVersion`, with its `v`: `v0.4.0` or `v0.4.0-rc.1`, never `v01.4.0` or
   `v0.4.0-..`), read only from the environment; a repository test holds the workflow's pattern
   to the app's rule. If the tag already has a published release holding
   `manifest-darwin.json`, an earlier attempt of this workflow published it, so nothing is built
   again and only the last job runs. A published release without that file stops the run:
   GoFortress most likely published it, so check that `ENABLE_RELEASE=false` reaches its setup,
   whose summary says whether release publishing is on.
2. **Build the unsigned app** (macOS, read-only, no secret anywhere in the job). It checks out the
   tag and stops unless its commit is on the default branch, a guard against tagging the wrong
   commit. It sets up Go, pnpm, and Node with no dependency or build cache, and magex from its
   GitHub release, checked against that release's checksums, never from a cache, first on
   `PATH` so nothing shadows it; mage is built from the module proxy, checked against Go's
   checksum database. Then it runs `magex app:buildRelease` and hands the unsigned bundle, in
   a tar, to the next job as an artifact kept for 7 days.
3. **Sign and publish** (macOS, the `release` environment, `contents: write` and `actions:
   read`).
   - First, before the artifact or any secret, it looks for a published release of the tag
     holding `manifest-darwin.json`: if an earlier attempt published it, the job has nothing
     left to do and succeeds at once.
   - It checks the `CODESIGN_IDENTITY` variable and the default branch again, and sets up Go
     and magex the same way, with no pnpm and no Node. Then it has mage compile the
     magefiles into a binary, before any secret exists; with `MAGEFILE_HASHFAST` set for the
     job, every later magex call runs that binary as it is, so nothing is compiled while the
     keychain is unlocked.
   - It waits for GoFortress's run for the tag, which has been checking the same commit
     meanwhile. A poll that fails is tried again; if the run fails, nothing is published, and
     if the workflow loses track of it, it says so rather than calling it a failure.
   - It unpacks the artifact (one bundle, plainly named), makes a keychain of its own in the
     runner's temporary folder, imports the code-signing certificate, checks that an identity
     whose SHA-1 or whole name is `CODESIGN_IDENTITY` is there (with `security
     find-identity`, never `-v`), writes the release key there, readable by the runner alone,
     runs `magex app:signRelease`, and deletes the keychain and the key at once, however the
     job ends. The secrets exist only from the keychain's making to its deletion. Meanwhile
     only the magefiles binary compiled before runs, which is this repository's code and the
     Go modules it links as `go.sum` pins them, with Apple's tools: no npm or Node code runs
     in this job at all.
   - It checks the files with `magex app:verify`, then creates the release as a draft, which no
     one else sees, titled `Release <tag>`: the install note first, then the notes GitHub
     generates from the pull requests merged since the last release, marked a prerelease for a
     tag with a suffix. A draft an earlier attempt left is reused.
   - It attaches the zip, the disk image, and `SHA256SUMS`, then the manifest last, downloads
     them back, and checks the manifest and the zip with `magex app:verify` and every file
     byte for byte against what it built.
   - It refuses to publish a draft titled otherwise, or holding other files than those four.
     Then it publishes it, as the latest release only when it is a stable release newer than
     the latest one, compared number by number; a prerelease never is.
4. **Check the latest release** (ubuntu, read-only). For a stable release that became the
   latest, it checks that `releases/latest/download/manifest-darwin.json` serves the new
   version. It is a job of its own, so trying it again signs nothing.

`GH_TOKEN` reaches only the steps that run `gh`, never one that runs magex, mage, Go, or pnpm.

**Trying again.** Use Actions' **Re-run failed jobs** on the Desktop release run. It keeps the
tag, the workflow as the tag holds it, and the secrets, so the workflow needs no manual trigger.
A failed build builds again; a failed publish job waits for GoFortress, signs again, reuses the
draft it left, and replaces its files; once the release is published, a re-run finds it, says
an earlier attempt published it, and only checks the latest release. The unsigned bundle the
publish job signs is kept for 7 days; after that, use **Re-run all jobs**, which builds it
again.

**When something goes wrong.**

- **GoFortress failed on the tag:** nothing was published. Fix it or re-run it, and once it is
  green, **Re-run failed jobs** on Desktop release. A broken tag is fixed forward with a new tag;
  a pushed tag is never moved.
- **Desktop release failed partway:** nothing is public until its last steps. At worst a draft
  exists, which no one else sees. Fix the cause, then **Re-run failed jobs**.
- **A bad release shipped:** fix it forward with a patch release; installed copies take only a
  newer version, so they update once it is out. A published release can't be changed: the
  repository's releases are immutable, so its files can't be replaced or deleted. To keep more
  copies from taking it meanwhile, mark the release before it as the latest again (`gh release
  edit <earlier tag> --latest`), so `releases/latest/download/manifest-darwin.json` serves that
  release's older version, which offers no update.
- **The release key leaked or was lost, or the certificate expired:** see below.

**Replacing the release key or the certificate.**

- **The release key** (lost, leaked, or only suspected): installed copies trust only the key
  their build pins, and a release's manifest must verify with the key of the build being
  released, so there is no handing over from one key to the next. Every copy on an older
  version needs one reinstall by hand. A leaked key is worse than a lost one, since whoever
  holds it can sign a manifest every installed copy trusts, so replace it at once.
  1. Make a new key in a new folder, as in *Before the first tag*: `magex app:keygen
     out=<new folder>`.
  2. Pin its public half, and that line's SHA-256, in `internal/update/key.go` and
     `internal/update/releasekey_test.go`, in a pull request, and merge it.
  3. Replace the secret, before the next tag: `gh secret set UPDATE_SIGNING_KEY --env release
     --repo mrz1836/nilbox < <new folder>/release.key`. A publish job that still signs with the
     old key fails its own check against the newly pinned one, and publishes nothing.
  4. Tag a release, and say in its notes, and in the README, that a copy on an older version
     must download and install it by hand once.
  5. Back up the new key, and destroy the old one.
- **The certificate** (expired after ten years, lost, or replaced): updates still install,
  since their trust is the release key, not the certificate. But the app's identity changes, so
  macOS asks each user once more for what they had allowed it (Automation, to open launches in
  their terminal). Make a new one as in *Before the first tag*, then replace `CODESIGN_P12` and
  `CODESIGN_P12_PASSWORD`, and `CODESIGN_IDENTITY` too if its name changed. The move dialog's
  **Open it** checks a copy already in Applications against the running app's signature, so it
  won't open an older copy signed with the old certificate; move the new one there instead.
