# Build targets

| Target | What it does |
|---|---|
| `magex frontend:install` | Installs the frontend dependencies exactly as `pnpm-lock.yaml` pins them |
| `magex frontend:build` | Type-checks the frontend and builds it into `frontend/dist` |
| `magex frontend:gate` | The frontend gate: frozen-lockfile install, format check, lint, typecheck, tests with coverage, the render profile through the React Compiler, and the production build into `frontend/dist`, which fails on a JS chunk over `chunkSizeWarningLimit` (`vite.config.ts`), then checks first paint against its budgets (`magefiles/firstpaint.go`), stopping at the first failure (`magefiles/frontend.go`; CI runs this same target) |
| `magex brand:generate` | Regenerates the theme tokens, frontend brand constants, `build/darwin/Info.plist`, and the icon artwork (the app icon `build/appicon.svg` and the menu bar icon `build/trayicon.svg`) from `internal/brand` |
| `magex brand:icons` | Renders `build/appicon.svg` into the bundle's icon set `build/darwin/icons.icns` and the development Dock icon `cmd/nilbox/appicon.png`, and `build/trayicon.svg` into the menu bar icon `cmd/nilbox/trayicon.png` (needs `rsvg-convert`: `brew install librsvg`); run it after changing the mark or the artwork |
| `magex app:bindings` | Regenerates the committed TypeScript bindings in `frontend/bindings`, writing each Go string type with constants as a union of its values; fails, naming it, on a bound type whose constants are declared in more than one file, since the generator orders those differently from run to run |
| `magex app:bindingsCheck` | Regenerates the bindings into a temporary directory and fails on any drift, or on such a type |
| `magex app:lint` | Lints the desktop-tagged Go code (the desktop shell and the frontend embed), as a release builds it and again with [the inspector](inspector.md) |
| `magex app:test` | Runs the desktop-tagged tests, which run the desktop runtime itself (a bound method's error reaching the window worded), as a release builds it and again with [the inspector](inspector.md); `magex test` builds without the tag |
| `magex app:dev` | Runs the app against the Vite dev server with hot reload; `magex app:dev fresh=true` runs it on a fresh [sandbox](#a-sandbox-on-fresh-data) data folder, so the [first-run setup](../guide/first-run-setup.md) shows |
| `magex app:build` | Builds the production bundle from source, [Nilbox Dev](#nilbox-dev-a-build-from-source), as `bin/Nilbox Dev.app`, and signs it ad hoc for local use |
| `magex app:open` | Opens `bin/Nilbox Dev.app` as it is, building it first when it is missing; a running copy is only brought to the front |
| `magex app:run` | Builds `bin/Nilbox Dev.app`, quits the Nilbox Dev already running, and opens the fresh build: the one command after a change |
| `magex app:fresh` | Opens `bin/Nilbox Dev.app` on a fresh [sandbox](#a-sandbox-on-fresh-data) data folder, your prompts and launch defaults copied in, leaving your own data as it is; `magex app:run` goes back to it |
| `magex app:reset` | [Starts Nilbox Dev over](../guide/starting-over.md) on your own data, which the release shares: quits it, asks for the reset, and opens it again, which carries the reset out |
| `magex app:check` | Runs `app:bindingsCheck`, `app:lint`, `app:test`, and `app:build` |
| `magex inspect:<command>` | Looks inside a running build from source through [the inspector](inspector.md) it carries: its log, events, page console, your trail of clicks, the live database, the window's page, and a picture of the window |
| `magex app:package` | Builds a release into `bin/release/<version>/`: a universal bundle (Apple silicon and Intel) signed with a code-signing identity, its zip, a signed `Nilbox.dmg`, `SHA256SUMS`, and the signed update manifest, checked against the release key the app pins ([Packaging a release](packaging-a-release.md)); `app:buildRelease`, then `app:signRelease`, in one target |
| `magex app:buildRelease` | The first half of `app:package`, which needs no secret: builds the release's universal bundle, unsigned, alone in `bin/unsigned/<version>/` |
| `magex app:signRelease bundle=<path>` | The second half: checks the unsigned bundle at the absolute `<path>` that `app:buildRelease` built, signs a copy of it, and builds the release's other files in `bin/release/<version>/`, as `app:package` does |
| `magex app:keygen out=<folder>` | Makes the release key: writes its Ed25519 private half to `<folder>/release.key` (`~/` allowed), outside the repository, and prints the public half to pin |
| `magex app:verify dir=<folder> [version=<version>]` | Checks a release's `manifest-darwin.json` against the release key the app pins, and the zip it names by its size and SHA-512, as the app will; with `version`, it also refuses a manifest of another version |
| `magex app:rehearse case=normal\|tampered\|broken` | Builds a pair of rehearsal builds to try an update end to end on this Mac: A (`0.0.1`) as `bin/rehearsal/A/Nilbox.app`, and B (`0.0.2`) as a release's zip and signed manifest in `bin/rehearsal/feed/`, which A reads as its releases; signed with `CODESIGN_IDENTITY` (and `CODESIGN_KEYCHAIN`), and prints the steps ([Rehearsing an update](rehearsing-an-update.md)) |
| `magex db:generate` | Regenerates each table owner's typed queries from its `queries/*.sql` and the migrations, with the sqlc version `.github/env/90-project.env` pins |
| `magex db:check` | Fails when the generated queries are stale or a query does not prepare against the schema the migrations build |
| `magex measure:startup` | Startup to the first frame on screen, over six launches |
| `magex measure:refresh [searches=5]` | Five live, read-only inbox refreshes and the API budget they imply; with `searches`, each also reads that many routine searches over your account, and the report adds the watch passes |
| `magex measure:launch` | One real launch in iTerm2, timed until the agent runs in the clone |
| `magex measure:size` | The size of the app binary and the bundle, `bin/Nilbox Dev.app` |
| `magex measure:all` | Every measurement: size, startup, refresh, and launch, in that order |
| `magex autopilot:evals [runs=<n>] [case=<glob>] [model=<model>] [budget=<dollars>]` | Runs the eval suite (`internal/describeit/evals`) of the plugin [Describe it](../guide/autopilot/your-own-claude-code.md) installs in your own Claude Code, against this build's plugin: Claude Code's `claude plugin eval` in a temporary folder, the plugin's tools answering from the suite's mocks, so nothing reaches your data, its spend capped at $5 unless `budget` says; run it by hand after the catalog or the skill's words change, since the gate never does |

A typical first build:

```bash
cd ~/projects/nilbox
magex app:run
```

Before opening a pull request, run the Go gate, then the frontend gate and the query check,
then (on a Mac) the desktop checks:

```bash
magex format:fix && magex lint && magex test:race && go-pre-commit run --all-files
magex frontend:gate
magex db:check
magex app:check
```

**Signing.** `magex app:build` signs the bundle ad hoc (`codesign --force --sign -`), which is
enough to run it on the machine that built it. A release (`magex app:package`) is signed with a
self-signed code-signing identity instead, so every release keeps the same designated
requirement. Nothing is notarized.

**Versions.** `magex app:build` stamps the build's versions from git: the release version
(the latest tag, `0.0.0` before the first release), a build number (the commit count), and the
`git describe` commit, such as `v0.1.0-3-gabc1234`, or the bare commit hash before the first
tag, with `-dirty` when the working tree had uncommitted changes. Settings → About shows all
three, with the macOS version and the processor architecture, the version of each coding
agent installed, read with its own `--version` (`claude --version`, `codex --version`), and,
under **Tools**, the GitHub CLI's (`gh --version`), whose sign-in the app borrows, and Git's
(`git --version`), which makes every launch's worktree and tells which clone is which repository. Each is read
once per build of its command and kept until the command changes, as when it updates itself,
so opening About again runs nothing; hover a version for where its command is. Its **Report a
bug** button opens a new GitHub issue with all of it filled in: the versions only, never their
paths. The bundle's `Info.plist` carries the
release version, only its numeric core for a prerelease (`0.2.0` for `0.2.0-rc.1`), and the
build number too. macOS caches app icons by that build number, so a
new commit is what refreshes the icon Stage Manager and Finder show. An icon change you have
not committed yet keeps the old build number; if a stale icon persists, clear the cache with
`sudo rm -rf /Library/Caches/com.apple.iconservices.store` and then
`killall Dock WindowManager`.

**Firewalls.** The app talks to GitHub: `api.github.com` on every refresh, and
`avatars.githubusercontent.com` to download profile pictures: yours once, the pictures of the
people an issue you open shows, and those of the inbox cards' authors (unless **Settings →
Cards** turns them off), each person's downloaded by login and each bot's from the address
GitHub gives for it, without your token, kept in the database, and downloaded again at most
once a week. The [service status](../guide/window.md) check reads the status pages of the services you
watch, `www.githubstatus.com`, `status.claude.com`, and `status.openai.com`, each sent only the
request for its summary. A per-app firewall must
allow `bin/Nilbox Dev.app/Contents/MacOS/nilbox` (a release's is
`/Applications/Nilbox.app/Contents/MacOS/nilbox`) to reach `api.github.com`, or every refresh
times out. Blocking the avatar host only means initials show instead of pictures, and blocking a
status page only means the check says it couldn't reach it.

## Nilbox Dev, a build from source

A build from source (`magex app:build`, and the `app:run`, `app:open`, `app:fresh`, and
`app:reset` that build it) is **Nilbox Dev**: its bundle is `bin/Nilbox Dev.app`, named Nilbox
Dev, with the bundle ID `com.mrz1836.nilbox.dev`, so macOS never takes it for the release
(`/Applications/Nilbox.app`, `com.mrz1836.nilbox`). Spotlight, notifications, privacy prompts,
and Open With tell the two apart, and Nilbox Dev asks for its own permissions (notifications,
Automation) once. Only the bundle's copy of the `Info.plist` changes: the committed `build/darwin/Info.plist`
stays the release's, and `app:build` stamps the name, the ID, and the Automation prompt's
words into its copy (`devStamps` in `magefiles/bundle.go`, from `brand.DevName` and
`brand.DevBundleID`) before it signs the bundle. The binary inside keeps its name, `nilbox`, and
a release and a rehearsal stay Nilbox.

Nilbox Dev runs on the release's data, `~/Library/Application Support/Nilbox`: the same
settings, prompts, launch defaults, triage, and database. So don't run the release and Nilbox
Dev at the same time: both would refresh, sync your GitHub notifications, and write that one
database. `magex app:run` quits a running Nilbox Dev, never the release, so quit the release
first. `magex app:dev` runs a bare binary, with no bundle, on that same data too.

A bundle a build before Nilbox Dev left at `bin/Nilbox.app`, under the release's name and ID,
is removed by the next `magex app:build` once nothing runs from it, so Spotlight stops offering
it.

## A sandbox on fresh data

`magex app:fresh` opens the app on a fresh data folder of its own, so you can try the
first-run setup, or anything else, on clean data while your own data stays exactly as it is:
it builds the bundle when it is missing, quits the running copy, makes
`$TMPDIR/nilbox-fresh-*` (removing the ones it made before), copies your `prompts.json` and
`launch-defaults.json` in, and opens the app with `--data-dir` pointing at it. A **Test data**
chip beside the logo marks the window, so the sandbox is never mistaken for your app;
`magex app:run` goes back to your own data. `magex app:dev fresh=true` does the same with hot
reload, for building the setup itself; banners show as unavailable there, as in any
development run.
