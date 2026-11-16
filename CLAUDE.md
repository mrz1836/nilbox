# CLAUDE.md — read this first

This is **Nilbox**, a local-first desktop app (Go + Wails v3 + React) that turns a GitHub account
spanning hundreds of repositories into one focused inbox and launches coding agents (Claude Code
and OpenAI Codex) on pull requests and issues. The shared engineering rulebook is
[`.github/AGENTS.md`](.github/AGENTS.md). Read it before changing anything. The rules in this file
are Nilbox's own, and they win wherever the two differ.

<br>

## Non-negotiable rules

1. **This is a public, open-source project under the MIT license.** Never write the names of
   employers, clients, customers, companies, private projects, or private repositories into this
   repo. Never write personal machine paths (absolute home directories) or private tracker or
   ticket IDs. That applies to code, comments, tests, fixtures, docs, commit messages, branch
   names, and pull request titles and bodies. Use neutral placeholders such as `acme-org`,
   `example-repo`, and `~/projects`. A contributor's own private names can be listed in
   `.git/info/private-terms`, and the guard tests then fail wherever one appears (README,
   "Keep private names out of the repository"). Never write that file's terms anywhere else.
2. **Never bypass git hooks.** No `--no-verify`, no `-n`, no `core.hooksPath` tricks. If a hook
   fails, fix the cause.
3. **The product name lives only in `internal/brand`.** Reference `brand.Name`, `brand.Slug`, and
   friends. Never hard-code the name elsewhere.
4. **User data stays local.** Nilbox never sends a user's GitHub data, preferences, triage state,
   or caches anywhere except GitHub's own API. Installing a tool downloads its public release from
   GitHub's release download hosts (`github.DefaultDownloadHosts`), which get only the request for
   that file: no token, and nothing of the user's. Checking the status pages of the services
   it depends on (`internal/svcstatus`: GitHub's, Claude's, and OpenAI's, unless the user
   turns the check or a page off) reads each one's summary from its host in
   `github.DefaultStatusHosts`, which likewise gets only that request. Checking for an update
   of the app itself (`internal/update`, in a release build alone: a build from source never
   checks, and a rehearsal reads a local folder) makes two GETs to GitHub's release download
   hosts, the latest release's manifest and, when it is newer, its zip, with no token and
   nothing of the user's, and spends no API budget.
5. **Optimize for local-first operation.** Nilbox should minimize network requests and rely on local caches and state wherever possible, ensuring responsiveness and reducing dependency on external services. Try and use the least amount of GitHub requests necessary for any operation.
6. **No shell or Python scripts.** A build, check, or release step is a `magex` target written in
   Go (`magefiles/`, see Layout), never a `.sh`, `.py`, or other script, and a workflow runs the
   target rather than a script. `TestNoScripts` in `internal/repoguard` fails on any script file
   (by extension or `#!` line) outside the synced `.github/env/` and `.github/scripts/`, which
   are fixed in go-broadcast instead.

<br>

## The gate

Every change must pass, in order:

```bash
magex format:fix
magex lint
magex test:race
go-pre-commit run --all-files
magex frontend:gate   # format, lint, typecheck, tests, compiled render check; CI runs it too
magex db:check        # the generated queries are current and every query prepares; CI runs it too
```

After changing a query file (`internal/<owner>/internal/queries/*.sql`) or a migration, run
`magex db:generate` and commit the regenerated code with the change. Both targets run the sqlc
version `SQLC_VERSION` pins in `.github/env/90-project.env`, through `go run`, so sqlc never
enters `go.mod`.

On macOS, a change to the desktop shell, the bound services, or the frontend must also pass
`magex app:check` (bindings drift check, desktop lint, desktop tests, and the app bundle
build). No CI check runs on macOS (only `desktop-release.yml`'s jobs do, and they build
releases), so this check is local.

<br>

## Tests

Tests call `t.Parallel()` wherever it is safe, in top-level tests and subtests alike, and
`.golangci.json` enforces it with `paralleltest`, `tparallel`, and `usetesting` (`t.Context()`,
`t.TempDir()`, `t.Setenv()`, and `t.Chdir()` over their `os` and `context` counterparts). The
exception is a test that changes process-wide state: one that uses `t.Setenv` or `t.Chdir`
(Go refuses both in a parallel test), swaps `os.Stdout`, or reads what its own subtests left
behind. `paralleltest` already skips a test that calls `t.Setenv` itself; any other serial
test carries a `//nolint:paralleltest // <reason>` comment that names the process-wide state.
This deliberately overrides the synced
[`testing-standards.md`](.github/tech-conventions/testing-standards.md), which says to avoid
`t.Parallel()` outside concurrency tests; for this project, this file wins.

A test that runs real git isolates it through the runner, not the process: hand the service
under test `execpath.Exec{Env: testutil.GitEnv(home)}` (or `testutil.Git(t)` for a fresh home),
which reads none of the user's git configuration, and run fixture commands with
`testutil.RunGit`. A suite whose rigs start from the same repositories builds them once per
test binary with `testutil.GitTemplate` (or `GitTemplates`, one per key) and copies them into
each test, and `testutil.DB` likewise hands each test a copy of a database migrated once per
test binary. A test that runs an external tool runs a fake one: `testutil.FakeTool`
links the tool's name to the test binary, which replays a recorded run or a scripted behavior
when the package's `TestMain` calls `testutil.FakeToolMain` first, so no test writes a script
to run.
Tests of the real magex or go-pre-commit carry the `integration` build tag and `Integration` in
their names; the gate never runs them (`magex test:integration` does). Timer tests run in a
`testing/synctest` bubble instead of sleeping or polling, fuzz tests seed their corpus with `f.Add` (`testdata/fuzz/` is ignored), and golden
files are rewritten with the test's `-update` flag.

<br>

## Logging and errors

Log only through the `*slog.Logger` a service is given (`cmd/nilbox` builds the one logger
with `internal/logging` and passes it down; nil means discard, never `slog.Default`, and
`internal/repoguard` bans the global loggers), and turn an error into text for the window only
with `uierr.Message`, attaching a worded sentence with `uierr.New` where the error's own text
would not help the user. A bound method's error reaches the window through
`desktop.MarshalError` only because `cmd/nilbox` binds every service with `boundOptions`: the
runtime encodes a method's error with its own service's marshaler alone, never the
application's `MarshalError` option, so a service bound any other way sends the window neither
the words nor the detail. A repoguard test fails on one, and a desktop-tagged test (`magex
app:test`, part of `app:check`) calls a bound method through the runtime itself.

<br>

## Synced files

Shared tooling is synced from [go-broadcast](https://github.com/mrz1836/go-broadcast), and every
sync overwrites these files with the shared version. Anything Nilbox-specific written into them is
lost, so keep project rules in this file and project CI and tooling settings in
`.github/env/90-project.env`. The sync covers at least:

- `.github/AGENTS.md`, `.github/SECURITY.md`, `.goreleaser.yml`, `.mage.yaml`, and `codecov.yml`
- everything under `.github/workflows/`, `.github/actions/`, `.github/scripts/`,
  `.github/tech-conventions/`, `.github/ISSUE_TEMPLATE/`, `.github/docs/`, and `.vscode/`
- `.github/env/`, except `20-guardian.env`, `90-project.env`, and `99-local.env`

Files the shared source doesn't have, such as `.github/workflows/frontend.yml`,
`.github/workflows/sqlc.yml`, and `.github/workflows/desktop-release.yml`, are left alone.

Settings → About's **Report a bug** opens `.github/ISSUE_TEMPLATE/bug_report.yml` and fills in its
`environment` field: the versions, then the settings and integrations, which core words
(`internal/core/bugreport.go`, `appinfo.StateReader`) with nothing personal in them, a name or
path of the user's own told only as the default or the user's own, or counted; its test fails on
any. The form is synced, so if a sync renames that field, the button stops filling
it in. The `internal/appinfo` tests read the form and fail when that happens. The account
menu's **Report a bug** opens the same link, and its **Send feedback** opens GitHub's own blank
issue (`template=BLANK_ISSUE`, `appinfo.FeedbackURL`) with a `[Feedback]` title, a few prompts,
and the app's version filled in; a synced `config.yml` with `blank_issues_enabled: false` would
turn that link into the template chooser. The `magefiles`
tests likewise fail if the synced `10-mage-x.env` stops pinning golangci-lint, which `magex
app:lint` reads.

<br>

## Layout

The longest entries below are summaries, each ending in a link to its package's full design
notes in [`docs/architecture.md`](docs/architecture.md). Read a package's section there before
changing the package, and keep the section and its summary in step.

- `cmd/nilbox/` is the application entry point: a headless build by default, and the Wails
  desktop app with the `desktop` build tag (`main_desktop.go`). Only the desktop bootstrap
  imports Wails. `--data-dir=<absolute path>` (`appdata.FromArgs`, `appdata.DataDirFlag`),
  read before anything else, runs the app on that data directory, its logs included, in place
  of the user's own: the sandbox `magex app:fresh` and `magex app:dev fresh=true` open, which
  `core.Options.Sandbox` tells the window (`appinfo.Info.Sandbox`, a **Test data** chip). A
  reset the user asked for quits the app and opens it again from `PostShutdown`, once the
  shell has closed the database: `open -n <bundle>` (`desktop.RelaunchArgs`, pure and tested on
  Linux, which a run outside a `.app` bundle, as `magex app:dev`'s, skips), with the sandbox's
  `--data-dir` when it ran on one. `main()` runs the desktop runtime's
  `updater.HandleHelperMode()` first, since an update's helper is this binary run again,
  before anything opens the data. A rehearsal build (`main.flavor=rehearsal`, `magex
  app:rehearse`) always runs on its own data directory beside the user's
  (`appdata.ResolveRehearsal`, named `brand.RehearsalDataDirName`, or `brand.RehearsalSlug`
  on Linux, as the user's is named), whatever `--data-dir`
  says, with the **Test data** chip, since the update opens the new version with no
  arguments. `update_desktop.go` is the engine's `Installer` over the runtime's updater,
  headless, whose one provider serves the zip from the update cache with its SHA-512
  (`desktop.NewArtifact` never hands over a zip without one); its `Restart` waits up to 30
  seconds for the helper to stand by, so it never runs on the main thread. The menu bar's
  update rows are `menubar.UpdateRows` (an update that didn't install shows "<version>
  didn't install", then Try Updating to <version> Again while `InstallFailed.Retryable`, or
  else Download <version> Manually…, `UpdateManual`, which opens the release page through
  `desktop.Config.Browser`, the runtime's `Browser.OpenURL`: the version found's, or, with
  none found, the one that didn't install, `menubar.ManualURL`), their clicks
  `Shell.UpdateAction`: a check runs as tracked work, and Restart to Update, Update to X…,
  and Try Updating to X Again bring the window forward and emit
  `event.UpdateRestartRequested` (`updates:restart`), so every restart runs the window's one
  flow (`restartToUpdate` in `frontend/src/lib/useUpdate.ts`: download first, then
  `Restart(false)`, whose refusal names the runs going for the window to ask about, sheets
  counted then; with text a sheet hasn't sent, it asks first, naming the runs it lists; a
  confirmation calls `Restart(false)` again and goes on to `Restart(true)` only when the
  question named every run that refusal names, matched by id (`covers` in
  `frontend/src/lib/update.ts`), else it asks again). What the banner, Settings → About,
  and the move dialog offer is one pure rule (`offerOf` in `frontend/src/lib/update.ts`).
  The window tells a refusal it answers on its
  own (the last update still finishing, a restart finishing after a held quit, which it
  waits out quietly, and an update that failed to install for good, which it offers only the
  release page for) by the `update.RefusalError` code in its detail, never by its words. The startup dialog for data
  a newer build saved offers the newest backup this build opens (`desktop.RestoreOffer`,
  `FindRestore`; Escape quits, and no key puts the backup back, since the runtime's macOS
  alert gives a button Return or Escape, never both), puts it back on a click
  (`desktop.PutBack`, `store.RestoreBackup`, a failure worded by `desktop.PutBackFailure`
  with what to do), and opens the app again from `PostShutdown` through
  `desktop.RelaunchArgs`, as a reset does, the `open -n` bounded to five seconds
  (`relaunchWithin`, which every reopen from `PostShutdown` shares, `openAgain`).
  A build that updates attaches the engine's `Mover` too (`desktop.Mover`, over
  `execpath.Exec`): it copies the bundle with `ditto --noqtn` (`desktop.CopyArgs`), takes the
  quarantine mark ditto still copies off every file of the copy, in Go (`move_darwin.go`;
  nothing elsewhere), and reads a copy's mark there too (`translocates`: its own folder's,
  without the flag a Finder move adds); checks a copy with `codesign --verify --deep
  --strict --test-requirement =<requirement>` (`desktop.VerifyArgs`), the requirement the
  app that runs designates, read from its process ID (`desktop.RequirementArgs`, `codesign
  --display --requirements - <pid>`), so a copy signed otherwise fails, worded, and a
  requirement that can't be read holds `update.ErrOwnSignature`; words a program that wrote
  nothing by how it ended (an exit status, a signal, or no start); holds every quit while a
  move runs (`Shell.HoldQuit`: `ShouldQuit` says no and shows the window, and the quit goes
  ahead, once, as the last move lets go); and
  `Shell.OpenAfterQuit` quits the normal way, which the quit guard holds while runs go,
  keeping the copy for the quit the user lets through: **Keep running** (the bound
  `scriptrun.Service.KeepRunning`, the Go-only `event.QuitKept`) forgets it.
  `PostShutdown` runs the one command `Shell.RelaunchCommand` works out: the copy
  (`desktop.OpenArgs`, `open -n <bundle>`, the bundle-path sibling of `RelaunchArgs`), else
  this bundle after a reset, so the app never opens twice, within the same five seconds.
  The window's one move is `useMoveToApplications` (`frontend/src/lib/useMove.ts`,
  `components/MoveDialog.tsx`), which `useUpdateControl` holds: wherever `offerOf` offers
  the move (`Offer.move`: a build that updates, `update.Location.Movable`, no restart
  installing or finishing), the dialog asks on every launch, Not now for that launch, and
  the update banner's offer buttons and Settings → About's location row offer it too. A
  refused Open it turns the question back to the move, and as the work ends focus goes back
  to Not now (`ConfirmDialog`'s `busy`). The quit's question, opened over the dialog, has the
  keys: the topmost modal is the one opened last (`isTopmost` in `lib/useModalDialog.ts`),
  wherever the page renders it.
- `internal/brand/` holds product identity, design tokens, and the logo mark. The palette tests
  enforce WCAG AA contrast for every text color in both themes. `internal/brandgen/` generates
  the frontend theme tokens, `build/darwin/Info.plist`, and the icon artwork (the app icon
  `build/appicon.svg` and the menu bar icon `build/trayicon.svg`) from it (`magex
  brand:generate`); `magex brand:icons` renders the artwork into the committed icons (macOS,
  needs `rsvg-convert`). A build from source (`magex app:build`, so `app:run` and the targets
  that open its bundle) is "Nilbox Dev", `bin/Nilbox Dev.app` with `brand.DevName` and
  `brand.DevBundleID`, which `devStamps` (`magefiles/bundle.go`) stamps over the bundle's copy
  of the generated `Info.plist` alone, so macOS (Spotlight, notifications, privacy prompts, Open
  With) never takes it for the release; it keeps the release's executable name and data
  directory (`brand.DataDirName`), so it shares the user's data and must not run beside the
  release. A release and a rehearsal keep `brand.Name` and `brand.BundleID`, and `app:build`
  removes an earlier build's `bin/Nilbox.app` once nothing runs from it.
- `build/` holds that generated artwork and `build/appicon.svg.sha256`, the stamp of which
  artwork the icons were rendered from. `build/darwin/` holds the bundle's generated
  `Info.plist` and the rendered icon set, `icons.icns`.
- `internal/core/` assembles the app and builds every service the window binds: settings,
  profiles, prompts, launch defaults, inbox, review, issues, launcher, worktrees,
  notifications, app info, the API budget, sessions, integrations, script runs, release, hooks,
  repository scripts, the mirror of the user's GitHub notifications, the backup of the
  user's setup, the service status check, the first-run setup, the app's updates, and
  Autopilot's routines.
  Every `internal/*` package is plain Go, built and tested on Linux without CGO.
- Services share their building blocks rather than keep copies: `internal/repo` and
  `internal/item` name repositories, pull requests and issues, and their worktrees;
  `internal/agent` is the backend registry of the coding agents, one spec each (Claude Code,
  Codex) with its command, config variable and markers, effort levels, models, permission
  modes, reserved flags, and how a launch's options become its flags, so adding an agent adds
  a spec and the launcher, profiles, and editors read the rest from it; `internal/procs` lists
  the running processes' working directories (`lsof` on macOS, `/proc` on Linux), which tell
  a worktree some session is in, reads a process's start time (`StartedAt`), which tells a
  process from a stranger that reused its PID, and probes a PID as running, gone, or unknown
  (`Probe`, which never signals one `ValidPID` refuses and calls it unknown, so an agent's
  worktree lock naming one is kept); `internal/execpath` finds and runs external
  programs (`Finder`, `Runner`; `Search.Path` searches the login shell's PATH), and starts a
  long-lived one in its own process group (`Start`, `Proc`), writing straight into a file the
  app tails and stopping the whole group with a signal ladder; nothing else starts a process
  (`TestOnlyExecpathStartsProcesses`, with named exceptions), and nothing runs `go install` or
  `sudo` (`TestNoGoInstall`, `TestNoSudo`); `internal/shellenv` reads the user's login shell's
  environment, after the first paint, and never logs a variable's name or value; and
  `internal/clock`, `internal/periodic`, `internal/flight`, `internal/paths`,
  `internal/progress`, and `internal/textutil` hold the clock (and `clock.Sleep`, the shared
  wait that ends with its context), the interval runner, one call per key at a time, `~`
  paths, a long read's progress told to the window a few times a second (the Release page's
  list and the window's worktrees listing, `event.WorktreesProgress`), and text helpers,
  among them the app's one POSIX shell quoting: `textutil.ShellQuote`, which always quotes,
  for every word of a script the app writes, of the ssh command it hands git, and for the
  program of Repo scripts' chain line unless it is a path from `/`, and
  `textutil.ShellWord`, which leaves a word plain only when every POSIX shell reads it as
  itself (ASCII letters, digits, and `=-_./:,+@%`, not starting with `=`), for the commands
  the window shows or copies and the chain line's folders and arguments.
  A file a repository or an agent controls (a clone's `go.mod`, `t3.json`, `tasks.json`,
  magefiles, go-pre-commit config and hooks, the signing files, the `.git` file and
  `commondir` the sweep's count of worktrees follows, an agent's transcript) is read
  only through `paths.OpenRegular` (`ReadHead`, `ReadTail`): a regular file, or a link to one,
  opened without waiting and checked again once open, so a named pipe swapped in is refused
  (`paths.ErrNotRegular`) rather than hanging the read.
  `internal/testutil` holds the fakes and fixtures the test suites share (a fresh database, an
  isolated git, `Commit` for a fixture file, git templates built once per test binary, the
  `Planter`, which plants fake programs in a clone's own config for the guard tests, recorded
  runs of real tools, a runner, finder, clock, event sink, log, fake tools, and
  `TerminalScript`, the shell a fake terminal runs a run's command under); only tests
  may import it. `internal/scriptrun/scriptruntest` holds the runner the
  hooks, scripts, and release suites share, built on it: a real `scriptrun.Engine` and its
  Service over a fresh database, the next start over the same history (`Restart`, `Next`),
  a fake terminal app that runs each run meant for the user's terminal under `/bin/sh`
  (`Terminal`, through `WithTerminal`; `Close` hangs it up mid-run), and `Ended` and `Await`,
  which look again only when the engine's `Changed` says the runs changed, never on a timer
  (`AwaitShell`, for the shell the engine notes without telling, alone polls); only tests
  may import it either. The repoguard guards on SQL and
  on the git verbs the app runs treat both test-support packages as they treat `_test.go`
  files.
- `internal/event/` holds the app's events, one Go type each with its name; services emit them
  into the one `event.Sink` they are given. `internal/desktop/` is the shell's logic: it hears
  those events, keeps the menu bar current with what `internal/menubar` works out, runs the
  scheduler, the checks follower (`inbox.ChecksFollower`: the listed pull requests whose
  checks were running, but a muted repository's, and those a routine's search follows, 50 a
  round at most, the listed first, read 25 to a request side by side through
  `(*github.Client).FetchItems`, and nothing while none runs; a pull request whose workflow
  runs GitHub holds for approval, `item_details.checks_awaiting`, which its card marks held
  in place of the rollup's state, isn't running, and waits for the whole refresh), the
  Release page's checks follower (`release.ChecksFollower`, below), the sweeper (these four
  as often as Settings → Refreshing says, through `periodic.RunEvery`, which core wakes on the Go-only
  `event.IntervalsChanged` the settings setter emits, so a change applies at once), the GitHub
  notifications' `ghthreads.Syncer` (woken the same way), the budget's `Saver`, the sessions watcher, the Release page's
  refresher, and the Hooks page's scans as tracked work, starts the deferred work
  (`core.Deferred`: the login-shell read first; the tool finder, the runs, the release
  `Follower`, and the service status `Checker` beside it) once after the first paint, never on the cold
  start, and shuts down in order (timers, then the work, then the database). It declares small interfaces that the Wails glue
  in `cmd/nilbox` implements, and imports no Wails. A window event is registered in
  `main_desktop.go` with `application.RegisterEvent`, which generates its TypeScript type and
  name in `frontend/bindings`; a repository test checks that every window event is registered.
- When a service needs another, prefer a narrow interface or value type declared where it is
  used (the launcher's `ItemStore`, `LaunchSettings`, `WorktreeMaker`, and per-agent `Truster`
  map, the worktree service's `SweepSettings` and `GitHub`) over importing the other service;
  `internal/core/adapters_<consumer>.go` (one file per consumer, the shared `settingsPart` in
  `adapters.go`) adapts the real services to them. The repoguard boundary test
  (`internal/repoguard/deps_test.go`) keeps the cuts it lists.
- `internal/budget/` keeps the GitHub API budget current. The one shared `github.Client` hands
  every response's rate-limit reading, and what the request spent, to its `Tracker`
  (`github.RateObserver`), whose state the `Saver` keeps across runs. Every GitHub request goes
  through `(*github.Client).do`, apart from three reads that aren't the API and carry no token:
  a profile picture (`FetchAvatar`, the user's own by the URL GitHub gives, and anyone's by login
  through `FetchLoginAvatar`, which builds the avatar host's URL itself and refuses a login no user
  could have, such as a bot's), a public release file (`DownloadAsset`, https only, its
  host and every redirect's on `DownloadHosts`), and a status page's summary (`FetchStatus`:
  GitHub's, Claude's, or OpenAI's, https on one of `StatusHosts` alone, a clean absolute path,
  every redirect on the request's own host, at most 1 MiB, conditional on its last ETag, so a
  304 carries no body). Never build an HTTP request anywhere else, nor run
  `gh api`;
  `internal/repoguard/githubrequests_test.go` fails the build if anything does. Every write goes
  through the `WriteGate`, a REST one through `edit` and a GraphQL mutation through `mutate`
  (`postGraphQL` refuses a mutation), but the chore lane's: marking a notification thread done
  or read, sent only by a rule the user turned on or by the user's click, goes through `chore`
  and the client's own `ChoreGate` (nil refuses every chore), whose `AllowChore` paces it apart
  from the edits, with counts of its own (50 a minute, 300 an hour, a second apart), the same
  reserve, and the same hold, so a wait GitHub asks of either lane stops both;
  `internal/repoguard/githubwrites_test.go` keeps it so. The notifications listing is
  conditional (page 1 sent with its last ETag as `If-None-Match`, through `do`'s `withETag`:
  measured in November 2026, github.com answered `If-Modified-Since` with a charged 200 and
  `If-None-Match` with a free 304), and a 304 spends nothing, but says only that page 1 is as it
  was; `internal/repoguard/githubnotifications_test.go` holds the client to `GET /notifications`
  and `DELETE` and `PATCH` on `/notifications/threads/{id}`. Every request carries what it was
  sent for (`github.WithPurpose`: the inbox's refresh, the routines' searches, the checks
  follower, or Autopilot's own reads), and the `Tracker` splits each budget's spend by it
  (`Reading.Purposes`, which add up to `Spent`); the budget menu says Autopilot's share in one
  line, only once it spent something.
- `internal/inbox/` also reads the routines' searches beside its own lists. A refresh hands its
  watch pass (`WatchPasses`, a desktop worker run before the scheduler) the searches
  `Config.Watches` gives, whose requests (`github.FetchWatches`, packed a few searches to a
  request) go out only once the refresh has stored and told the inbox, outside its errgroup,
  so the refresh stores and returns on the inbox's own; the pass then reads the details of the
  watched items that are new or changed with no lock held, and stores them, their
  `item_watches` rows, and each search's state (`inbox.watches`) through the order, after
  reading again under the lock what is stored of them (`recheck`), so a read stored since its
  request went out stands, and the rows of an item closed or unlisted since never come back;
  then it records in the write order every card and state it stored (`writeOrder.stated`), so
  an item's own read or a follower round that went out before it stores nothing over them,
  and a whole refresh still fetching applies its states again; then it tells
  `Config.Changed` (core's Go-only `event.InboxItemsChanged`). No write ever puts a merged
  item's state back (every query that writes `items.state`). A watched item is in no list or
  count; a failed search, a truncated one's tail, and a partial answer's missing items keep
  their rows, stale; an item whose details didn't come back keeps its card, to be read again;
  no pass, and no routine's test or its check names, goes out under a quarter of the GraphQL
  budget; the follower takes the watched pull requests a routine follows, muted or not, after
  the listed ones; `items.state` is written by every read that carries it, another service's
  through `Store.NoteStates` in the order its read went out (`Store.StartRead`, the Store
  core hands the Service as `Config.Store`, so they share one write order); items nothing
  refers to are pruned after 30 days (`Config.Keep`, `inbox.Keepers` over the agent sessions'
  and the notifications' items in core, a refresh that can't read them pruning nothing); and
  a guard test decides every query that reads the reason flags
  (`TestEveryReasonFlagQueryIsDecided`). Autopilot's own reads (`SearchNow`, `ReadChecks`) go
  through `inbox.Routines`, never the bound Service, whose `ItemsByID` serves a routine's
  Watching list.
  Full design: [docs/architecture.md](docs/architecture.md#internalinbox)
- `internal/launcher/` resolves a launch (the agent first, then that agent's profile, model,
  and flags), plans it, and writes its files; `internal/launcher/terminal/` renders the launch
  script, which only changes into the folder and runs the agent, and opens it in the user's
  terminal app, one driver per terminal and OS. `terminal.WriteScript` writes every script the
  app opens in a terminal into the launch folder, a session's and a followed run's alike (named
  `pr-`, `issue-`, or `run-<number>`, pruned after seven days by those prefixes alone); a
  followed run's script runs its command under `script(1)`, recording its output, its shell's
  PID, and its exit status in the run's files. `launcher.CommandOpener`, which core hands the
  runs and never binds, opens only such a script, in the terminal chosen in Settings. `internal/workspacetrust` marks the launch
  folder trusted in each agent's own config (Claude's `.claude.json`, Codex's `config.toml`,
  appended to and never rewritten).
- `internal/localrepos` finds the user's clones, in the clone root (`clones.root`, the primary,
  which new clones go in) and the other repository locations (`clones.locations`, which
  settings alone writes: each a folder looked into one level deep as the clone root is, or a
  single clone, its kind worked out once as it is added). `Roots` holds them, each expanded
  and its links resolved once; `Roots.Clones` lists the clones in all of them at once,
  reading no git, a clone two locations reach listed once, the clone root's first, and an
  extra location it can't read skipped and named (`Found.Skipped`), while a clone root that
  can't be read is an error; `Holds` says whether the locations hold a clone, which every
  service checks before it changes one; `Key` is the clone root alone while there are no
  others, so a cache keyed by it stays valid. `Survey` previews a folder before it is added,
  with what to add instead (the folders inside one with no clones of its own, at most 100
  subfolders and 5,000 entries looked at, or the folder of a clone that holds others), and
  `Check` says how each stored location reads now. `Candidates` suggests folders for the
  setup's Clones step: every folder directly in home that holds clones, whatever its name,
  or the folders one level down inside it that do, and the same in GOPATH's and ghq's
  layouts, 20,000 entries at most in all, the common names first. It never reads a hidden
  folder, a link in home, macOS's own folders (Desktop, Documents, Downloads, Library, and
  the rest, which macOS guards or which hold no clones), or a folder a disk is mounted on.
  It reads the mounts from the kernel's table with `MNT_NOWAIT` (`mountPoints`), so it
  never asks a share, a cloud drive, or a virtual machine's files, and never makes macOS
  ask the user. The Clones step looks in GitHub Desktop's `~/Documents/GitHub` only on the
  user's click (`PreviewCloneLocation`), and `Classify` words a folder macOS keeps the app
  out of (`EPERM`) with where to allow it. No location is searched recursively.
  One `Reader`, which core builds and hands every service that reads the locations, bounds
  each read of another location than the clone root, the resolution of its links included,
  to `DefaultReadLimit` (5 s) and keeps one of each going per location: a share that hangs
  reads as `StateNotAnswering` and is skipped, its hung read joined until it ends, never
  started again beside it. The clone root is always read directly.
- The app owns its worktrees: `internal/worktrees` makes them (one per item per clone, shared by
  every agent, under `<the clone's folder>/.<slug>/worktrees/<clone>/`, so in a location as in
  the clone root), records them in `internal/worklog`, checks every record against git on each
  listing and sweep (a record whose clone's folder is gone too, as on a drive unplugged, is
  kept), and removes
  them safely. A listing runs git only in the clones worth listing (`worthListing`): one whose
  common git folder registers a linked worktree or can't be read, and one a record names, read
  without git by `linkedWorktrees`, which the sweep's count shares. Only `internal/worktrees/create.go` fetches or runs `git worktree add` (Repo
  scripts' pull, below, fetches only as `git pull` itself does), and nothing the app runs
  changes a clone's own checkout; `TestNoGitCommandChangesAClone` in `internal/repoguard` fails
  the build otherwise. There are two exceptions, each on the user's click. A tool the user
  turned on: a release's magex switches to the branch, pulls, tags, and pushes the tag
  (`internal/release`), and the app only starts it and checks afterwards where the tag and the
  clone are. And Repo scripts' **Pull**: a fast-forward-only pull of the branch a clone is on
  from the branch it follows on origin (never a merge or a rebase), held to
  `internal/scripts/pull.go`, whose one pull argv the guard parses and holds to an exact list
  of literal words (`--ff-only`, `--refmap=`, and `--no-verify-signatures` among them),
  with no other option anywhere in the file and no other verb it bans. Every git command
  there runs with a hooks folder's hooks, the file system monitor, and submodule recursion
  off (`gitread.Hardening`, which the worktrees' `hardening` and the pull share), since a
  worktree holds a pull request's own files, and a pull request's worktree is made only at
  the head whose files the launch checked; a clone's own config-named hooks refuse every
  command that could fire one (below). With core's `Guard`, the worktree service's own
  commands on a clone or its worktrees are guarded too (`git.do`): the reads (`readOnly`:
  listing the worktrees and branches, resolving a ref, counting commits, an ancestor's
  check, and a commit's time, with `--no-show-signature` and `--no-mailmap`), which run no
  filter or hook but could read an object a partial clone lacks, run with gitread's
  `ReadEnv` (no transport, no lazy fetch, no optional lock, no replace ref), so a promisor
  remote the clone names is never asked; every other (a status, which runs a file's clean
  filter, a removal, a prune, an unlock, and a branch's deletion, which fires
  reference-transaction) runs with gitread's `LocalSetup` for its folder, worked out once
  per operation and one folder at a time (`localCache`, a `flight.Group` per folder):
  every filter driver of the clone's own off, Git LFS's too, no optional lock, no replace
  ref, no transport, and refused before git runs, with a `guardError` worded for the
  window, when the clone's own config holds a setting gitread refuses. A removal, whose
  own status runs inside the worktree, weighs the worktree's config too (its
  `config.worktree`), and runs with the filters both define off. Such a worktree shows the
  refusal as its `Error`, which also takes away its removal (`CanRemove`), so no sweep or
  cleanup removes it, and a removal fails with the same words. `create.go`'s one fetch argv (`fetchArgs`:
  `fetch --no-tags --refmap= --upload-pack=git-upload-pack <remote> <refspec>`, so it moves
  only the ref its refspec names, never one the clone's own `remote.<name>.fetch` maps,
  which the repoguard test holds to exactly those words), its `ls-remote --symref` (with
  `--upload-pack=git-upload-pack`), and its `git worktree add` also run under gitread's
  guards, through the worktrees `Guard` core gives every `Service` (the same `gitread.Git`
  as Release's): an Ensure that builds a worktree works out one `RemoteSetupFor` its
  item's remote, on its first guarded command, under the clone's lock (`git.session`), and
  `git.reach` runs a command that reaches the remote with it and `git.write` one that writes
  files with its `Checkout` (no transport, but a partial clone's checkout may fetch from its
  promisor remotes, which are weighed too). A checkout weighs only what it can reach: when
  the setup refused the remote itself (a `RefusedError`), `git.write` works out
  `CheckoutSetup` instead, so a worktree from the last fetched copy, or on a branch the
  clone has, is still made, while a partial clone's promisors, and rerouting settings Git
  LFS would follow, still refuse it. Each runs as `git -C <clone> <words>` with that
  setup's environment, which holds every setting (`GIT_CONFIG_COUNT`), `GIT_DIR`, and
  `GIT_WORK_TREE`, and the setup's own git, through `Config.GitWith`, core's runner with an
  environment of its own. Core gives the worktrees gitread's `GitFinder` as their `Finder`,
  so every other worktree command runs that same git: one clone is never handled by two
  git builds. A clone whose own config holds a setting gitread refuses (a hook, a filter,
  `core.worktree`, an include, ...), a remote helper, or a rerouted remote is refused
  before git runs (a `guardError` holding gitread's), in `gitread.Message`'s words as a
  sentence, naming the settings or the remote or saying why the setup couldn't be worked
  out (a config git can't read, no git), which the launch's error shows; a fetch that
  failed after the clone's own ssh command, password program, or credential helpers were
  set aside says which (`gitread.SetAsideNote`, a `gitread.SetAsideError` in the chain).
  The launcher
  reaches the `Maker` through its
  `WorktreeMaker` interface; no agent's own `--worktree` flag is used. A branch is deleted only
  with the conditional `git update-ref -d <ref> <oid>` (the repoguard tests ban `git branch
  -d/-D`): by `create.go`, of a branch it just fetched at a head the launch didn't check, and
  otherwise only through `internal/worktrees/branches.go`, when the user
  ticked it (or, for the automatic cleanup, the user's rules pick it), every commit on it is on
  GitHub, and neither the user's protected patterns (`internal/branchrules`) nor a remote's
  `HEAD` keeps it. The one exception is the cleanup of merged branches: a branch kept only
  for its commits not on GitHub (`MergedBranch.Unpushed`) can be deleted there anyway, but
  only once the user asks for such branches to be offered, ticks it (never ticked to start
  with), and confirms a second dialog naming each and what it holds; pinned to the commit
  shown, whose result names that commit. A branch checked out, protected, or a remote's
  default never is. Every job that changes a clone holds that clone's lock from
  `internal/clonelock`, one `Set` that core builds and hands to each, keyed by
  `paths.Resolved`. Lock `act` before a clone's lock, never after. Worktree changes wait only
  for one another. A run (a release, checks, a script) is never waited for: a launch or a
  click fails at once with a sentence naming the run, and the sweep and a listing's
  forgetting of a record skip the clone until next time.
- `internal/sessions/` follows the agent sessions launches start. It owns `agent_sessions`, the
  ledger the launcher records each launch in (through its `SessionLedger`) under an ID minted
  before the launch, which Claude takes as `--session-id`. A `Watcher` (a desktop worker)
  reconciles what each agent reports into it and emits `event.SessionsUpdated`; one `Observer`
  per agent turns the agent's own shapes into one `State`: `observe/claude` runs `claude agents
  --json` and reads the transcript tail, and `observe/codex` talks to Codex's app-server daemon
  over `codex app-server proxy` with a read-only JSON-RPC client (its `Allowed` methods only, over
  a WebSocket it speaks itself, since only `internal/github` speaks HTTP), falling back to the
  rollout files. Each observer degrades to `StateUnknown` rather than guess, and a session's
  process says whether it still runs. Following never sends a session anything, and nothing here
  reaches the network. A session that stops working (idle, waiting on the user, ended, or
  failed) hands its item to the inbox's `Rereads` (a desktop worker, through the `Rereader`
  interface core wires), which reads it again, one GraphQL point each, one item at a time; a
  single-item read (`RefreshItem`, the checks follower, the review sheet's `ReadPull`) takes a
  pull request out of Review requested once GitHub no longer asks the user, by name or
  through a team, stamped like the inbox's own edits (`review_left_at`), so a lagging search
  keeps it out until it moves. `LastWords` reads a session's whole last message from the
  agent's own files, on demand, for the review sheet. Each record keeps what its launch
  reported (the permissions it started with, what it did to the item's worktree, and what it
  worked around), which the card's sessions panel shows in place of a line on the row after
  the launch, and `ShowFolder` shows a session's folder in Finder through core's `open -R`
  Revealer. The bound `Forget` deletes an ended session's record on the user's click (the
  panel's ×), refusing one still running (`ErrLive`; failed and unknown count as running) and
  counting one already gone as done, and emits `event.SessionsUpdated`; the prune is
  otherwise the only thing that deletes a session.
- `internal/review/` is the review sheet's service. The pure `Decide` works out every action's
  offer and the one primary from a pull request's facts, and `Act` runs it again before any
  write, refusing an offer with warnings (`Offer.Confirm`) unless the request accepts them.
  Asking Dependabot to rebase and Update branch are the primary only while the branch is behind,
  never as a fallback. Every write goes through the inbox's `Actor` (the item lock, the account
  check, and the token retry; never bound) and is pinned to the head the user saw (`loadAct`
  turns another head into `pushed_since`); a merge reads `mergeStateStatus` fresh and merges
  only when GitHub says it can, so an admin's token never merges past branch protection on its
  own: only `merge_bypass` does, never the sheet's button, with each rule it skips accepted.
  Approving held workflow runs (`approve_runs`: one `ApproveWorkflowRun` each through `edit`,
  never one twice, asking first when the pull request changes workflow files) and a close
  (`close`: offered only when `viewerCanClose` says so, always confirmed, run only from the menu,
  `menuOnly`, never with a default reply) are never the button either, and every try of a close
  reads the head first (`FetchMergeState`). Line comments are checked against the cached diff
  (`hunks.go`) before anything is sent; text that goes with a merge or a close is posted as a
  COMMENT review (never an approval) only once GitHub would act, and never twice: anything that
  fails after it is `commented_not_merged` or `commented_not_closed` (`failedAfterComment`). It
  owns `review_diffs` and `review_diff_files`, a bounded cache pruned in the transaction that
  writes; one `FetchPullReview` (one point) brings the conversation too, cut to fit
  (`fitContext`); only `Open` moves `review_diffs.seen_at`; `Earlier` (`PeekPull`) stores
  nothing. Each step is an `event.ReviewProgress`; a merge or a close takes the pull request out
  of every list (`Acted.Merged`, `Acted.Closed`), and a merge tells its `Merges` (the Release
  `Refresher`). Nothing here lets an agent approve, merge, or close.
  Full design: [docs/architecture.md](docs/architecture.md#internalreview)
- `internal/issues/` is the issue sheet's service, beside the review sheet's: neither imports
  the other (the repoguard boundary test), and they share only the building blocks, the
  conversation's among them (`internal/timeline`). `Decide`
  is pure: from an issue's facts it works out where it stands (gone, closed as what and by
  whom, locked, a fix merged or in progress, the user replied last, assigned, open), the
  offers, and the one primary, and `Act` runs it again before any write. Comment is offered
  always, but held back on a locked conversation for a user without write access; the two
  closes (as completed, as not planned) wait for the sheet's first read ("Wait for it to
  load"), are left out when GitHub's `viewerCanClose` says no, always confirm, and post the
  user's text first (`Offer.CommentsFirst`, with `Offer.CommentBlocked` while a lock refuses
  it). The button is Comment, or Close as completed once a fix merged. `Open` serves the issue
  from its cache and the inbox card with no request, and starts a visit: `seen_at` holds
  GitHub's `updatedAt` of the read it shows, which only `Open` moves, so what a later read
  brings that is newer is new to the user (`Issue.LastSeenAt`, GitHub's own times, no clock
  skew). `Refresh` reads the issue (one GraphQL point, `FetchIssueView`) unless a read under 20
  seconds old matches the card's activity, `ReadAgain` whenever the user asks, one read per
  issue at a time (`flight.Group`), through the inbox's `Actor` (`ReadIssue`), which rewrites
  the card from the same answer and under whose lock the cache is written; a read that finds
  the issue closed or gone is returned once, built from the read, and the cache dropped.
  `Earlier` reads the timeline's page before a cursor (`FetchTimeline`, the review sheet's
  query too) through `PeekIssue`, and stores it nowhere. Every action goes through `ActIssue` and tells each step as an
  `event.IssueProgress`. A close is pinned to the comments the user saw (the earlier of the
  request's `Seen` and the read's): every try, a token retry included, first reads the state
  and the newest ten comments (`FetchIssueState`); a comment someone else wrote since, or ten
  newer with more before them, is `new_activity` with nothing sent, and an issue someone closed
  first is `already_closed` and leaves the inbox. Then the text is posted once, and the issue
  closed with one REST `PATCH` (`(*github.Client).CloseIssue`; an answer that names another
  account as the closer, or another reason than the one asked, means someone closed it first).
  Once GitHub answered a step, nothing after it is an error: `failedAfterStep` makes a close
  whose comment was posted `commented_not_closed`, and keeps any other outcome, logging the
  inbox's own failure. After any action, under
  the item's lock, the cache is dropped (a close) or marked out of date. It owns
  `issue_views`, a bounded cache (1 MiB per issue, whose long texts are cut to 16 KiB, then 4
  KiB, then emptied, each cut marked, by `timeline.Cut`; 200 issues, 32 MiB, and 14 days unused
  in all), pruned in
  the same transaction that writes. Unassign me is the inbox's own `Unassign`. `Replies` and
  `FillReply` offer and fill in the prompt library's comment replies for issues (a comment
  reply may apply to issues; a default reply, which only the review sheet starts from, must
  apply to pull requests), through the reply filler core shares with the review sheet,
  which refuses a reply for another kind of item. With triage access or more (`github.CanTriage`,
  `Issue.CanEdit`, on an open issue the sheet has read), `Labels` lists the repository's
  labels and `Assignable` searches who the issue can be assigned to (one GraphQL point each,
  one read of each at a time, kept in memory ten minutes), and `Edit` adds and removes labels
  and assignees through `ActIssue`, each answer checked (GitHub ignores a change it won't
  make), never sending a step twice, stopping at the first that fails, and saying how many
  GitHub made (`EditResult`), so the window reads the issue again only then; the user takes
  themselves off with Unassign me alone.
- `internal/timeline/` is the conversation both sheets show, an issue's and a pull request's:
  its entries (`Entry`: comments, reviews with their line comments, and events), a page of
  them (`Page`), and the items, labels, and reactions they name. `Entries` converts what the
  GitHub client read, the viewer's own marked; `HunkTail` keeps a line comment's last four
  lines of its diff hunk; `Cut` shortens the description and every text of the entries, each
  cut marked, for a sheet's cache, whose own cap and steps stay its own; `NonNil` fills the
  lists an older build's cache lacks. Its JSON is what the issue sheet's cache already held,
  so no name or tag changes. It is a leaf: it imports only `github`, `item`, `clock`, and
  `textutil`, never a service, the store, or either sheet (the repoguard boundary test).
- `internal/avatars/` keeps the profile pictures of the people the sheets show (who opened the
  issue or pull request, an issue's assignees and participants, a pull request's reviewers, and
  who wrote each comment and review shown in full, never a bot), which the window asks for in
  one batch through the issue sheet's bound `Avatars`, for both sheets, once a sheet renders, so
  the conversation never waits on one. It owns `avatars`, one row per
  lower-cased login, downloaded by login (`FetchLoginAvatar`), at most four at a time, each once
  until it is a week old and shown again, a failed one not tried again for 30 minutes, and
  bounded (30 days unused, 2,000 rows, 16 MiB), pruned in the transaction that writes. The window
  gets them as data: URLs (`PersonAvatar`, `frontend/src/lib/pictures.ts`), so it loads nothing
  remote itself.
- `internal/reporules/` holds the pinned and muted repositories and orgs (`repos.pinned` and
  `repos.muted`, each a list of `owner` or `owner/name`, a target in one list at most), which
  settings alone writes (`SetRepoRule`, emitting the Go-only `event.RepoRulesChanged`, which
  core turns into an `event.InboxUpdated` and the Release `Refresher`'s `RulesChanged`) and
  the inbox and Release read; `Rules.For` gives a repository's own rule before its org's. In
  the inbox, each snapshot item carries its `RepoRule` (the rules are in the snapshot cache's
  `settingsKey`, so a change rebuilds it), a pinned repository's items come after the items
  pinned themselves, and a muted one's are in each list's Muted tab, never `NeedsYou` (so
  out of the menu bar's count), told in no arrival, followed by no checks follower, and
  given no file check by a refresh, though their details still come with it. It is a leaf:
  it imports only `internal/repo` and `prefval`.
- `internal/threadrules/` holds the rules for the user's GitHub notifications (`threadrules.*`:
  whether they are read, whether they are cleared on their own, marked done or read, by which
  triggers, and whether a watched repository's or a bot's activity counts), which settings
  alone writes (each rule's setter emitting the Go-only `event.ThreadRulesChanged` once it
  is stored, which core's relay turns into the ghthreads `Syncer`'s `RulesChanged`; the
  cleanup card's flag is the window's alone, and tells nothing), and the
  pure `Decide`, which says whether one thread is cleared, skipped, or held at the moment its
  item was finished; it is a leaf that imports only `internal/item` and `prefval`.
- `internal/ghthreads/` mirrors the user's GitHub notifications (threads) as the web inbox lists
  them, in `gh_threads`, which it owns with `gh_thread_moments` and `gh_thread_clears`, and keeps
  its passes' state in `ghthreads.sync`, a JSON pref only it writes; a thread's
  `latest_comment_url` is never shown or opened. A pass runs one at a time (the `Syncer`, a
  desktop worker, and the bound `CheckNow` share one), and with reading off it sends nothing and
  borrows no token. It lists conditionally (page 1 with its last ETag, a 304 free; a full walk
  up to `github.MaxThreadPages` every `FullWalkEvery`), never uses another account's ETag or
  mark, stores a listing in one `store.InTx` at its end (never an ETag or a mark its pages don't
  back), sends no listing for `RefusedFor` after `github.NotificationsRefused` unless `CheckNow`
  asks, and checks its items' states in one `FetchItemStates` call. The bound `Snapshot` and
  `List` read the database alone, and `event.GHThreadsUpdated` fires only when what they show
  changed. Its only writes to GitHub mark a thread done or read: clearing by the user's rules
  (`clear.go`: `threadrules.Decide` against the moments the inbox's `Settler` records), which
  sends nothing until the user turns clearing on or clears by hand, and the panel's `MarkDone`
  and `MarkRead` (`mark.go`). Every clear is queued in the ledger before it is sent, and a user's
  own waits the Undo hold; a pass queues at most `FirstCap` or `Cap` (else the count is a
  `Backlog`), checks at most `MaxCommentChecks` latest comments (`FetchLatestComment`), and
  sends one at a time through the client's chore lane, each checked again first (`sendOne`).
  Clearing turned off skips every queued clear but the user's own, and reading turned off skips
  them all. `Clear` and `ClearItem` clear by hand, never a thread that isn't a pull request's or
  an issue's; `Preview` and `Keep` are bound too. It never imports the inbox, settings, core,
  notify, the sheets, or the desktop shell, and the inbox never imports it (the repoguard
  boundary test).
  Full design: [docs/architecture.md](docs/architecture.md#internalghthreads)
- `internal/statusprefs/` is the service status check's catalog and the user's choices of
  what it watches. Each `Provider` (GitHub, Claude, OpenAI) has its page, where its summary is
  read (`Host`, `Path`, and `Format`: Statuspage's `summary.json`, or incident.io's documented
  widget API, `/api/v1/summary`, since OpenAI's Statuspage-shaped summary leaves Codex out),
  and the `Part`s a user may watch, each named by the app's own stable `PartID`
  (`github.pulls`, `claude.api`, `openai.codex-cli`, ...) and matched to the page by its
  component ids (`PageIDs`), so a provider that issues new ids needs only a catalog edit and
  the stored choices survive it. The defaults are what the app depends on: GitHub's Git
  Operations, API Requests, Issues, Pull Requests, and Actions; Claude's API and Claude Code;
  and OpenAI's Codex API, Codex CLI, and Responses. The choices are stored as changes from the
  defaults, as notifyprefs stores the banners (`status.providersOff`, `status.partsOff` for
  the default parts unticked, `status.partsOn` for the others ticked), so a part a later build
  adds with `Default` is watched without the user doing anything; settings alone writes them
  (`SetStatusProvider`, `SetStatusPart`, each in one `prefs.Update`, emitting the Go-only
  `event.StatusRulesChanged`). Adding a provider is one catalog entry and its host in
  `github.DefaultStatusHosts`, and a parser only for a new format. It is a leaf: it imports
  only `prefval`.
- `internal/svcstatus/` follows those status pages, so the window can say when a part the
  user watches has an incident. A check is one GET of a provider's summary through
  `(*github.Client).FetchStatus`, outside any API, so it sends no token and costs no budget,
  and is conditional on the page's last ETag (GitHub's and Claude's give one; a 304 keeps the
  reading). The pure parsers (`statuspage.go`, `incidentio.go`) turn each format into one
  reading of components and incidents on one severity scale (`State`: operational <
  maintenance < minor < major < critical; `full_outage` and `major_outage` are critical, an
  impact of none is minor, anything unknown minor), and the pure `judge` (`relevant.go`)
  decides by component, never by a page's overall state, which also turns for a Copilot or
  Codespaces incident: a component matches a picked part by its page id, or else by name and,
  where the part names one, group (OpenAI lists two components named Login); the parts watched
  that aren't operational are `Affected`; an incident counts when it names a part watched, or
  names none and is major or critical (`Unscoped`), and every other is counted in `Others`; a
  Statuspage page's parts watched that it no longer lists are `Unlisted`, logged once. Each
  provider's last reading is kept raw, in memory alone, and judged against the choices as they
  are on every `Status`, so ticking a part costs no request. `Config.Rules` reads the interval
  and the choices in one read of the settings (core's `statusRules`, the defaults, logged, when
  the settings can't be read). The bound `Status` serves every provider in catalog order, off
  while not watched (switched off, no part ticked) or the check off, and `CheckNow` (the
  popover's Check again) reads, in parallel, each watched provider whose last check ended
  `FreshFor` (10 s) ago or more, one `flight` key per provider, so a timed pass and a click
  share a request; a check that fails keeps the reading before it, with the reason in its
  `Error`, and is logged, and each pass emits one `event.ServiceStatusUpdated`
  (`servicestatus:updated`). The `Checker`, a deferred desktop worker in its own select loop
  (recovering and logging a panic; its tests run in a `testing/synctest` bubble), checks every
  watched provider after the first paint, then each once its last check ended as long ago as
  Settings → Service status says (`refresh.serviceStatus`, 30 minutes by default, 5 minutes to
  an hour, 0 off; woken on `event.IntervalsChanged`), taking along any watched provider
  checked half an interval ago or more, so the providers share one timer; on
  `event.StatusRulesChanged` (core's relay calls `RulesChanged`) it drops what it keeps of a
  provider no longer watched, its ETag included, sets the alerts' baselines quietly, tells the
  window, and checks a provider turned on at once. Alerts (`alerts.go`, in memory): the first
  reading after a start, or after a change of the choices, only sets the baseline, so a
  restart during an incident repeats no banner; a state that rises into minor, major, or
  critical above the level already known tells its `Notifier` ("Claude: Partial outage", the
  parts affected and the first incident, opening the incident), one that falls back to
  operational or maintenance after it tells "Claude: Operational again", and nothing else
  does; core's `statusAlerts` records each as a `service_status` notification about no item,
  with its banner (on by default). It never imports notify, settings, core, the inbox, or the
  desktop shell (the repoguard boundary test). The window shows nothing while every watched
  provider is operational, unchecked, or the check is off; otherwise one amber (minor,
  maintenance) or red (major, critical) warning sits before the budget gauge, opening a card
  per provider in trouble (its parts affected, its incidents with their stages, how many
  others don't touch what is watched, and its last error), and the account menu's Service
  status submenu names each watched provider's state.
- `internal/update/` is how the app updates itself. It holds the manifest each release attaches
  (`manifest-darwin.json`: `Manifest`, `Parse`, `SignedMessage`, Ed25519 `Sign` and `Verify`,
  `CheckFile`, `ParseKey`, which never takes a private key or a point of small order, and
  `AssetNames`) and `ReleaseKey`, the public key the app pins: a test pins its SHA-256, so
  changing the trust root takes two deliberate edits, and while it is empty `magex app:package`
  and `app:verify` (`magefiles/release.go`; `app:keygen` makes the key) refuse. The `Engine`
  (never bound; `Service` is the window's face, `Worker` its timed checks) checks, downloads,
  and installs by its `Flavor`: a source build never checks, a release trusts `ReleaseKey` alone
  through core's `githubFeed`, and a rehearsal reads a `FolderFeed`. It takes a manifest only
  once `Verify` passes, its platform is the build's, and its version a newer release
  (`internal/semver`), drops an update the releases no longer offer, keeps only a zip that
  matches its manifest (`Cache`, `<data>/updates/`), runs one operation at a time, and words
  every failure as `LastError`, with no raw error, path, or URL. It updates only from
  `Location.OK` (`/Applications` or `~/Applications`, else `ErrLocation`); `MoveToApplications`
  runs one at a time (`ErrMoving`), never during a restart (`ErrUpdating`), never replaces what
  is in the target (`MoveExists`, `ErrTargetTaken`) or touches the bundle that runs (`isSelf`),
  and holds the quit while it moves (`Mover.HoldQuit`). `Restart(confirmed)` refuses while runs
  go (`RunsGoingError`), backs the database up first (`store.Backup`) and stops when that fails,
  records `update.pendingInstall` only once the update is staged, and emits
  `event.QuitConfirmed{Restarting: true}` before `Installer.Restart`; the `Worker` settles the
  outcome at the next start (`JustUpdated`, `InstallFailed`, `update.uninstallable`). Bound too:
  `Status`, `CheckNow`, `Download`, `OpenInApplications`, `ShowAppInFinder`, `HideBanner`,
  `ShowBanner`, `AckUpdated`, `ShowInstallLog`. It writes only the `update.*` state keys, never
  `update.mode`, and uses stdlib crypto alone, importing no service, store, prefs, or HTTP (the
  repoguard boundary test); core adapts the feed, the prefs (`Memory`), the backup, the runs,
  and the notifications. The same section covers how `store.RestoreBackup` copies a backup in.
  Full design: [docs/architecture.md](docs/architecture.md#internalupdate)
- `internal/updateprefs/` declares the update setting, `update.mode` (`Mode`: `download`, the
  default, "Download it and tell me"; `notify`, "Just tell me"; `off`, "Don't check"), which
  settings alone writes (`SetUpdateMode`, emitting the Go-only `event.UpdatesChanged`) and
  backups carry (`updateMode`), and the update package reads. It is a leaf that imports nothing
  of the app's.
- `internal/semver/` reads semantic versions and compares them by precedence (`Parse`,
  `Compare`, `Version.Prerelease`), held to semver 2.0 (no leading zero in a number, a
  prerelease's numeric identifiers included), for the integrations' tool versions and the
  app's updates alike. It is a leaf.
- `internal/bump/` reads a dependency update from a bot's pull request title into an `Update`,
  for the review sheet and the `.Dependency` prompt variables, and which bot's branch a
  head branch is and the ecosystem a Dependabot branch names (`ParseBranch`, the
  `dependabot.yml` words, `Ecosystems`), for Autopilot's conditions. It is pure and imports
  no service.
- `internal/autopilot/` is Autopilot, which runs the user's routines. Its root package keeps
  them, in `autopilot_routines`, which it owns: the bound `Service` (`List`, `Get`, `Save`,
  `Delete`, `Duplicate`, `SetEnabled`, `SetMode`, `Reorder`, `Catalog`, and the builder's
  `Suggest`, `Watching`, and `Test`) checks every spec on
  save and load, carries one that won't load (a newer build's, say) as broken with why and
  never rewrites it, stores a routine with a step this build doesn't offer in Dry run alone,
  starts a new one off in the mode Settings says (Ask first to begin with) and of the account
  signed in (`Accounts`, the inbox's viewer; `Routine.OtherAccount` marks another's), keeps
  names unique ignoring case in any alphabet (`nameKey`, Unicode's simple folding, stored as
  `name_key` under the unique index, since NOCASE folds ASCII alone), and deletes softly;
  mode, switch, and account are columns, never spec. It writes its copy,
  `autopilot.json` (`File`, the export format `Bundle`, `"<slug>-autopilot/1"`), after every
  change, which `Restore` brings back, off, into a table that is new, and holds a backup's
  Autopilot section through `NewBackupPart`, apart from the Service. After every change it
  builds again the snapshot of the enabled routines' searches for the account signed in, each
  key once, one build at a time so the last to read stores last, which the inbox reads as
  each refresh starts (`NewSearches`, never bound); a draft's `Test` sends nothing while the
  GraphQL budget is low and says so (`LowBudget`). The engine (`NewEngine`, a deferred desktop
  worker, never bound) runs them: one loop, the only writer of the ledger it owns
  (`autopilot_matches`, one row per routine, item, fire key, and Dry run, open or closed,
  every change a compare-and-set; `autopilot_runs`, with the one-active-run index;
  `autopilot_tries`, `autopilot_baselines`, `autopilot_hands_off`; its state in
  `autopilot.engine`), woken by the inbox, the settings, the outcomes, a 1-minute tick, and
  the window's commands, doing everything slow on workers; it holds a `flock` on
  `<data>/autopilot.lock` (only the holder sends the routines' searches, `inbox.WatchHolder`),
  waits for a refresh that carried its searches, checks every limit in
  Go before a start, never sends a GitHub action again on its own once a quit cut it off,
  and starts steps only through the dispatchers each part registers (none
  yet, so every routine runs in Dry run). The bound Service's ledger actions (`Approve`, `Skip`,
  `RunNow`, `TryAgain`, `LetActAgain`, `ClearHistory`) are commands it applies; `StopRun` and
  `StopAll` go straight to the runner; `Activity`, `ItemHistory`, `Explain`, `Engine`, and
  `Matching` read. `internal/autopilot/sim` (test support only) is its simulator. It
  reaches the rest only through the interfaces it declares (`Prefs`, `Accounts`, `Names`,
  `Items`, `Costs`, `EngineItems`, the dispatchers, `Holds`, `ProblemSink`), which
  `internal/core/adapters_autopilot.go` adapts, and never imports the inbox, the sheets, the
  launcher, the worktrees, the sessions, the runs, notify, settings, the desktop, or core (the
  repoguard boundary test). Beneath it, the routines core, five pure leaves that read nothing
  and keep no clock: `catalog` (the
  vocabulary every spec name comes from: the triggers, the fields and their operators, the
  steps with the dispatcher that runs each, not `Offered` until it is built, the presets,
  the limits, and the builder's words; no entry for a close, approving workflow runs, or a
  merge past GitHub's rules, which a guard test holds), `watch` (a search into its query:
  scoped, its labels quoted, at most 256 characters, the query its own dedup key), `spec`
  (the routine's JSON, format version 1 holding every step kind; strict `Decode`,
  `Normalize`, `Validate` naming the part and the field and holding the steps to their
  order, `Load` broken rather than fatal, and `Sentence`), `facts` (an item's values from
  the plain `Row` the inbox fills, each known, unknown with what to read, or not applying),
  and `eval` (`Evaluate`: three-valued, cheapest first and order-free, with a fire key per
  trigger and reasons). They import only `item`, `repo`, `trust`, `sensitive`, `bump`,
  `clock`, `textutil`, and one another (`TestLeafImports`).
  Full design: [docs/architecture.md](docs/architecture.md#internalautopilot)
- `internal/autopilotprefs/` declares the `autopilot.*` settings (Pause Autopilot, the mode
  new routines start in, agents at once, runs and GitHub actions a day, the daily cap in
  dollars or tokens, keeping the Mac awake, holding starts during an outage, catching up, and
  how long the history and the run logs are kept), with their ranges, and a routine's `Mode`
  (`dryRun`, `ask`, `auto`); never `autopilot.engine`, the engine's own state, which the
  engine alone writes. Settings alone writes them (`SetAutopilot…`, each emitting
  the Go-only `event.AutopilotChanged`, writing only the keys that change), backups carry
  them (`Portable.Autopilot`), and Autopilot reads them. It is a leaf that imports only
  `prefval`.
- `internal/integrations/` finds the tools the app drives, mage-x's magex and go-pre-commit,
  each one a `Definition` (its binary, releases, version parser, quiet environment, minimum
  version, and the release that brought each upstream `Feature`). Detection looks on the login
  shell's `PATH`, then in the well-known tool folders and go install's, runs every copy's
  `--version` (four at a time), and works out where each came from; the `Worker` runs it as
  deferred work after the shell read, again when woken (settings emits the Go-only
  `event.IntegrationsChanged`, which core relays: a chosen copy wakes the finder, a flipped
  switch only tells the window), and checks GitHub for newer releases once a day
  (`(*github.Client).FetchLatestReleases`, adapted in core; **Check again** reuses an answer
  less than ten minutes old). `Service.List` never waits: it
  serves the last detection kept in `tools.<id>.seen`, marked as checking, until this run's
  ends, and `event.ToolsUpdated` tells the window; a `List` that finds this run's detection
  older than `staleAfter` (ten minutes) wakes the finder too. `Trash` moves a copy the last
  detection found (the one in use when the app installed it, or any other) to the Trash
  through Finder (`osascript`, the path passed as an argument, so Put Back works), never a
  link or a Homebrew copy, and fails with `ErrAutomationDenied` while the user hasn't let the
  app control Finder; `Reveal` shows such a copy in Finder (`open -R`). It writes the
  `tools.*` preferences and only reads `integrations.*`, which settings writes through the leaf `internal/integrationprefs`
  (the switches, automatic, on, or off, and the chosen copies; the leaf also declares the
  window's memory of the Scripts tab, `integrations.announced`, `scripts.lastPage`,
  `scripts.lastRepo`, and `scripts.sections` (the Release, Hooks, and Repo scripts sections the user
  opened or folded), and `scripts.showRuns`, whether a run Repo scripts starts opens beside
  it, off by default, which settings stores for the window alone), so settings never imports
  integrations. One rule says which folders are go install's (`goFolders`), for a copy's
  source and for its own updater alike: `$GOBIN`, each `$GOPATH` folder's bin, and
  `~/go/bin`, from the shell's environment and, while `go` is on its `PATH`, from `go env
  GOBIN GOPATH` too (run once per detection with `GOTOOLCHAIN=local`; one that fails leaves the
  folders unsure, and then no copy updates itself). Every run of a tool gets
  `Definition.RunEnv` (the shell's environment with the quiet variables and
  `GIT_TERMINAL_PROMPT=0`, or the quiet variables alone in the terminal), detection's and an
  install's `--version` runs included; release and hooks use it too. `Update` of a copy that can update itself runs the tool's own updater
  (`Definition.UpdateArgs`: `magex update:install`, `go-pre-commit update`), which downloads
  the latest release, checks it against the release's checksums, and renames it over the copy
  it runs as: a copy from a GitHub release or the app, no link, whose version has
  `FeatureSelfUpdate`, that it and its folder can be written, in no go install folder (magex's
  updater would run `go install` there). Detection works that out once per look
  (`detection.selfUpdates`, kept in `tools.<id>.seen`), so `List` reads no files for it. It is a
  `scriptrun.KindUpdate` run in the app, in the copy's folder, holding that folder's lock;
  its `Before` checks the copy again under the lock, the bound `Update` returns the run's ID for
  the window to show, and its `Finish` detects again, reading the copy's version from that look
  (the copy runs once), judges the update by the version alone (exit 0 with the updater saying
  the copy is already up to date succeeds, saying so), and, before the look matches its copies
  with the installs, records a copy the app installed as the app's (a new `tool_installs` row at
  the same path); `Tool.SelfUpdate` and `event.ToolsUpdated` bring that to the card. A tool's
  own update counts as updating it however it started (`scriptrun.Engine.Updating`, through
  `Runs`): one from before a restart shows on the card and refuses every install, and core
  registers `integrations.Verifier` to settle it as one that just ended. A self-update needs no
  sign-in and no build named by the app, since the tool finds its own release and archive. It
  asks nothing about replacing a copy the app didn't install: the tool replaces its own.
  `Install`, `Reinstall`, and every other `Update` take the lock of the folder they place the
  program in (`clonelock.KindInstall`, the app's one `Set`), so they never meet a tool's
  update there, and hand the latest release to
  `internal/toolinstall`, which downloads the archive and checksums file into a fresh private
  folder under `<data>/downloads` (every file in it made and read through an `os.Root` of it,
  so no name from GitHub reaches out), checks both against the checksums file and GitHub's digests,
  extracts only the program's own entry, tries it, and places it so the target is never missing
  (a copy beside it renamed over it, the old one put back if the new one won't run); it keeps no
  records and speaks no HTTP, and integrations records each install in `tool_installs`, which
  makes the copy read as installed by the app. Nothing replaces a Homebrew copy or a link, a
  copy the app didn't install is replaced only after the user confirms, and a go install copy
  gets a new copy in `~/.local/bin` instead; the app never runs `go install` or `sudo`. A run
  using the tool (`scriptrun.Engine.InUse`, which leaves out the tool's own update) holds both
  kinds of update back, nothing installs a tool while it updates itself, and nothing installs
  before this run's first detection ends (the kept one is only shown, never acted on).
- `internal/scriptrun/` runs the commands the integrations start (releases, hooks actions,
  checks, scripts, a clone's pull, and a tool's own update) and follows them, in the app or in the user's terminal, and owns
  `script_runs`. The `Engine` is never bound, since a `Spec` holds functions: `Start` takes the
  clone's lock first and fails at once when the clone is busy, runs the program through
  `execpath.Start` in its own process group, writing straight into its log in
  `<data>/script-runs` (four runs at a time in the app; a release never waits for a slot), and
  ends it with a per-kind timeout or the stop ladder. A spec's `Before`, run under the lock and
  a slot, may work the command out there, and its `Progress` says how far the run has got and
  whether Stop is let through now: the bound `Run` carries it, `Stop` refuses with the kind's
  words (`ErrStopRefused`), quitting signals a run only once its kind lets it stop (within
  `QuitKillAfter`; one still held back is left going for the next start), and
  `Config.Progressed` hears each change with the run's ID, which core sends the window as
  `event.ScriptRunProgress` (`scriptruns:progress`), never the whole list's
  `event.ScriptRunsUpdated`. A spec's `Recorded` is told the outcome only once the run's
  end is recorded, so a record that fails tells nothing twice, and its `Released` once the run
  has let go of its clone, so a page that reads the clone again finds it free. After
  `Released`, once the run has let go of its clone and its slot, the engine's own
  `Config.Failed` hears each run whose end is recorded as failed (one stopped at its time limit
  included), of every kind, in the app or the terminal, started now or settled after a restart
  (by a `Verifier` or by how it ended), and never one that couldn't start (its start's error
  says so), one the user or the quit stopped, one a hang-up, Control-C, or SIGTERM ended (exit
  129, 130, or 143, unless the engine sent it at the time limit), one interrupted, one that
  succeeded, or one left `Unsettled`: once, since the record that ends a run changes only a
  row still recorded as going. It gets a `scriptrun.Failure`, worded there ("Checks failed:
  Checks · all files", the run's folder in place of a repository it lacks, with the run's
  summary or "Timed out after 30 minutes."), which core (`runFailures`) hands the notify
  `Dispatcher`'s `Gather` as a `run_failed` notification about no item: recorded at once, its
  banner held `GatherWindow` (2 s) with those of the failures that follow, so a burst past
  `MaxBanners` (Install and update all, a restart's reconcile) shows one summary, under the
  same rules as every kind's banner; core's `Close` drops the banners still held (the history
  keeps their entries) and cuts short a batch already showing (each batch shows within the
  gathering's lifetime, which `Close` ends), waiting for it to end, before the database
  closes. The
  entry keeps the run's ID and its recorded start as RFC 3339 (`notifications.run_id` and
  `run_started`, `Entry.RunID`/`RunStarted`, `Notification.RunID`/`RunStarted`, and
  `event.RevealItem`'s for a banner's click), and the window opens it in Scripts → Runs only
  once the run with that ID says it started then, since Clear history lets an ID be handed out
  again; otherwise, or while the Scripts tab is gone, a toast says why. scriptrun never imports
  notify (the repoguard boundary test). A spec's `Keep`,
  which its `Before` may fill in, is written as JSON once `Before` has run into the run's own
  `<id>.kept` beside its log, never into its record or the window's view; `Engine.Kept` reads it
  back for a kind's `Verifier`, and it is removed once the run's end is recorded (it stays while
  a record fails, for the next start) and with the run's other files. Its `Run` is a deferred
  desktop worker: it reconciles the runs left from before a restart (a live group whose start
  time matches is orphaned and followed again; one that is gone was interrupted; a terminal
  run is told apart by its PID and exit files; every one is held under a placeholder lock and
  registered before any is settled, and a kind's `Verifier`, registered with
  `RegisterVerifier`, gives such a run its `Callbacks` (its output reader, progress, `Finish`,
  `Recorded`, and `Released`, which a `Spec` embeds) again from its record's intent and what
  it kept, built by the same helper as the kind's `Spec`, so it is read and verified as it
  ends, as tracked work) and, as the app quits, stops every run in the app and records it
  stopped, unless its `Finish` returns `Unsettled` (a release whose check of GitHub the quit
  cut off), which leaves it recorded as going for the next start. `Active` is the desktop
  shell's quit guard (`desktop.QuitGuard`): while a run goes in
  the app, `Shell.ShouldQuit` holds a quit and the window asks (`event.QuitRequested`), until the
  bound `Service.QuitAnyway` emits the Go-only `event.QuitConfirmed`. The Service's
  `OpenInTerminal` opens the user's terminal in a run's folder and runs nothing there, and
  `ShowLog` shows a run's log in Finder, both by the run's ID alone. `InUse` tells an install a
  tool is running, never counting a `KindUpdate` run, which is the tool updating itself (it
  holds its copy's folder, not a clone, for up to ten minutes), and `Updating` names such a run
  going now, one from before a restart included. `Amend` rewrites an ended run's summary and
  its kind's own result, in one transaction with the read its change is given, as a release's
  follow of its workflow does, and `Ended` lists a kind's ended runs, from which that follow
  resumes. The history keeps 200 runs and 512 MB of logs, and cuts a long log to its
  head and tail; Run again works the command out afresh from its source, never for a release
  or a tool's update.
- `internal/gitread/` is the read-only git that release, hooks, and Repo scripts share: an
  exact-argv allowlist plus validating predicates that refuses anything else with
  `ErrGitCommandNotAllowed` before git runs, and typed helpers over it (`OriginHead`,
  `HookPaths`, `IndexLocked`, ...). Every command runs with no fsmonitor, no submodule
  recursion, no optional lock, no prompt, no lazy fetch, and no transport, never with
  `GIT_CONFIG` and never touching `core.hooksPath`; a `status` turns off every filter driver the
  clone defines, Git LFS's too. An `ls-remote` (15 s, `RemoteTimeout`) refuses a remote helper
  (`ErrRemoteHelper`) or an http or https origin the clone reroutes (`ErrCloneRoutes`), runs only
  the transports the user allows, sets the clone's own ssh command, password program, and
  credential helpers aside for the user's (`SetAsideError`), saves no cookie, and asks for git's
  own upload-pack. Reading a clone runs no program the clone names, and only the system, global,
  and `command` scopes count as the user's. Its allowlist test asserts that no entry holds a verb
  `TestNoGitCommandChangesAClone` guards. For the commands that do change a clone (Repo scripts'
  pull, worktree creation) it hands over what they run with rather than running them
  (`RemoteSetup`, `RemoteSetupFor`, `CheckoutSetup`, `LocalSetup`): it weighs every setting of
  the clone's own by the three classes in `cloneconfig.go`, the one place they are listed
  (`inertKeys`, `neutralizedKeys`, and everything else refused with a `CloneConfigError`: a hook,
  a filter but Git LFS's own, `core.worktree`, an include, and any key a later git adds), and
  returns a `Setup` whose git (`GitFinder`), environment (`GIT_DIR`, `GIT_WORK_TREE`,
  `GIT_NO_REPLACE_OBJECTS=1`), and settings (`Hardening`, `fetchHardening`,
  `credentialHardening`, through `GIT_CONFIG_COUNT`) keep the command's argv to its own words.
  `CloneConfig` runs that check alone. `Message` words every gitread failure for the window, and
  `ValidBranch` is the branch rule a command's argument must pass.
  Full design: [docs/architecture.md](docs/architecture.md#internalgitread)
- `internal/release/` is the Release page's service. `Candidates` lists every clone in the
  repository locations whose origin is on GitHub, grouped (unreleased, up to date, no releases
  yet, can't release here, or muted: `GroupMuted`, whose repositories are never asked about),
  from two rounds of batched GraphQL (`FetchReleaseStatus`, `FetchReleaseCompare`), reading each
  unreleased clone with `gitread` and never asking origin. It serves the last list however old,
  reads the whole list again only on **Read GitHub again** (or with none yet, one from other
  locations, or one out of date), never on a timer, and keeps it in `release.lastCandidates`;
  the `Refresher` patches one repository's rows after a release or a merge
  (`event.ReleasesUpdated`), and the `ChecksFollower` (`refresh.releaseChecks`) reads again at
  most `followChecksMax` rows a round whose checks run, never a muted one's, asking nothing
  while none runs. `version.go` works the next version out exactly as magex does, tested against
  a table recorded from the real magex. A release follows the review sheet's template: the
  `Gatherer` reads its `Facts`, the pure `Decide` works out the `Offer` and magex's argv
  (`push=true`, never the bare word; `expect=`, `json=true`, and `ff-only` only when
  `Definition.Features` has them), the bound `Prepare` decides again within `FactsFor` without
  asking GitHub, and `Start` hands `scriptrun` a run whose `Before` decides again under the
  clone's lock, so a release that isn't ready never runs. magex tags and pushes; the app only
  starts it, turns Stop off from magex's switch to the branch until its pull ends, and
  shell-quotes the commands it offers to copy. `Finish` (and `release.Verifier` after a
  restart) verifies the release whatever magex's exit status, and only this run's tag succeeds
  (`Taken`). The `Follower` follows each release to *published* (at most `FollowPolls` over
  `FollowFor`, on the real clock, `clock.Sleep`), writing each step with
  `scriptrun.Engine.Amend` and telling a banner only once the record holds it. `ReleaseResult`
  and `OfferAfterMerge` (local state alone, never a scan of a location) are bound too.
  Full design: [docs/architecture.md](docs/architecture.md#internalrelease)
- `internal/hooks/` is the Hooks page's service. `Repos` scans, four at a time, every clone
  in the repository locations with a go-pre-commit config inside it (a `.github/env/*.env` file, or
  `.github/.env.base`): where git runs its hooks (gitread's `HookPaths`), its `pre-commit` hook
  (missing, go-pre-commit's by its marker, or another tool's, named husky, pre-commit, or
  lefthook from its text), and `ENABLE_GO_PRE_COMMIT` and the checks' switches from the config
  files themselves. Only a regular file, or a link to one, is read, a config file and a hook
  alike, and each is opened without waiting and checked again once open (`paths.OpenRegular`),
  so a named pipe swapped in never hangs a scan. A hooks folder outside both the clone's git folder and its work tree is
  shared, and the scan counts the clones that use it. Only go-pre-commit's `status` says whether
  its own hook is current, a byte comparison, so it runs in a scratch repository of the app's own
  that holds the hook's bytes, never in a clone, whose `.env` files go-pre-commit would load into
  its environment, `PATH` included, before it runs git; its answer is kept per copy and per
  hook's SHA-256, so status runs once for bytes no one judged. A scan runs apart from the
  window's call that started it, within `ScanTimeout`, and `Scans`, a desktop worker, holds
  every `Repos` call, its scan and its reads of the database included, to the app's lifetime:
  the quit ends every one under way and refuses later ones, and the worker returns once they
  have, so the shell waits for them before it closes the database. The scan is kept
  five minutes, a clone an action or a check ran in is scanned again on the next read, and
  `event.HooksUpdated` tells the window once the run has let go of the clone (the spec's
  `Released`), so the reread finds it free and the run ended. `Install`, `Update`, `Reinstall`, `Replace`,
  `Uninstall`, and `RunChecks` hand `scriptrun` a `KindHooks` or `KindChecks` run whose `Before`,
  under the clone's lock, reads the clone again and refuses a shared hooks folder the request
  doesn't confirm. An action's `Finish` reads what it did from the hook files, never from the
  exit status alone (1.13 exits 0 when another tool's hook stops it); a checks run's compares
  git's porcelain v2 status and each dirty file's SHA-256, taken before and after, so a fix to a
  file that was already dirty shows. The `Before` keeps what it read beside the run's record
  (`scriptrun`'s `Spec.Keep`, never its intent: an action's hooks and the copy of go-pre-commit
  that ran, with its version's features; a checks run's config switches and its snapshot,
  hashed or, past `maxHashed` dirty files, by size and time), so the `Verifier`, which core
  registers for both kinds, judges a run left from before a restart as its `Finish` would,
  once the shell's environment is read (`shellenv.Await`): the hook an action left unchanged is
  judged only while the copy that ran is still the one in use, and a run that kept nothing
  says it can't tell. A `Finish` whose step (git's status, go-pre-commit's) the quit cuts short
  leaves the run `Unsettled`, for the next start; one whose steps all ended stands.
  `InstallAll` installs or updates one clone at a time, each its own run in the app, and
  skips another tool's hook, a shared folder, and a busy clone. The readers know 1.13.1's
  text, recorded in `testdata/go-pre-commit-1.13.1`, and read `status --json` and `run
  --json` once `Definition.Features` says the copy has them. The page carries the PATH
  warning (the hook finds go-pre-commit only on the PATH or in `$(go env GOPATH)/bin`) and,
  until `FeatureVerifiedTools`, the note that the checks install their own tools, folded
  once a checks run ended (`hooks.checksRan`, which hooks alone writes). The bound
  `ActionResult` and `ChecksResult` read a run's outcome back typed, and its `Rerunner`, which
  core registers for both kinds, works Run again out afresh from the run's intent. A row's
  **Open in terminal** (`OpenTerminal`) opens the terminal the runs open in at the clone and
  runs nothing there, and its **Open the config** (`ShowConfig`) shows `.github/env/` (or
  `.github/.env.base`) in Finder, each only for a clone the repository locations hold.
- `internal/scripts/` is Repo scripts' service. While the Scripts integration is off, `List`
  returns the page marked off and `Run` and Run again refuse. `List` reads a clone's sources
  (the user's own scripts, `t3.json`, the runnable part of `.vscode/tasks.json` through
  `jsonc.go`, and magex's targets) and runs nothing in a clone: magex's built-ins come from
  `magex help:commands json=true` in a scratch folder of the app's own, the custom targets from
  parsing `magefiles/` with `go/parser`. An entry it can't run (an editor variable, a character
  that hides what runs, magex's own updaters, ...) is listed greyed with why, and a command that
  tags a version points to Release (`Anyway` runs it). A command from the repository's files
  asks before its first run and whenever it changes (D13): its trust is a SHA-256 recorded in
  `script_trust` under the clone's origin. `Run` hands `scriptrun` a `KindScript` run whose
  `Before`, under the clone's lock, reads the source again and runs only the command the user
  confirmed or trusted (a `QuestionError` asks); the `Rerunner` asks nothing, so it runs a
  repository command only while its trust record matches. A repository's shell command runs as
  `[$SHELL, -c, command]` only under a POSIX login shell, else under `/bin/sh`. The user's own
  scripts live in `user_scripts`, which it owns, renumbered in the `store.InTx` of every change
  (bound `Save`, `Delete`, `Duplicate`, `Move`, and `RepoNames`); they need no trust and run
  exactly as written in the login shell with `Variables()`, a run is refused when its script
  was saved again as it started, and Run again refuses one set to ask first (`ErrAsksFirst`).
  `BranchStatus` reads the clone's own refs and one `ls-remote` of the followed branch (never a
  fetch, and another host's origin only on the user's click). **Pull** (`pull.go`, `KindPull`)
  is offered only when behind and runs under the clone's lock: its one argv is held to an exact
  list of literal words (`--ff-only`, `--refmap=`, `--no-verify-signatures`, ...), which
  `TestNoGitCommandChangesAClone` parses, with `gitread.RemoteSetup`'s environment and
  settings; Stop and the quit are held for the whole run, and `scripts.NewVerifier` settles one
  left from before a restart.
  Full design: [docs/architecture.md](docs/architecture.md#internalscripts)
- `internal/backup/` is Settings → Back up and restore's service, which owns no table. It
  exports the user's setup to one `<slug>.backup` file (`File`, version 1, at most 16 MB,
  written user-only through `appdata.WriteFile`), one section per category ticked: settings,
  repositories, prompts (a `prompts.Bundle`, exactly as `prompts.json`), launch defaults (the
  `launch-defaults.json` format), the user's scripts, and the Autopilot routines (the
  `autopilot.json` format, each added or saved over arriving off, one a newer build wrote
  never touched; the window doesn't list this category yet); and imports one back. It reaches each
  category's owner only through a `Part` (`Export`, `Prepare` outside the transaction, `Apply`
  inside it, `Done` once it commits), which `internal/core/adapters_backup.go` adapts over each
  owner's `BackupPart` (`settings`, `prompts`, `launchdefaults`, `scripts`, `autopilot`), a type of its own
  built with `NewBackupPart`, never a method of the bound Service, so the window can't reach a
  method that takes a `*sql.Tx`; the owners import only backup's words (`Mode`, `Plan`, `Item`,
  `Count`), and the boundary test forbids the reverse. `Preview` works every section out under
  both modes (Merge, whose wire value is `combine`, and Replace), a section that can't be read
  shown with why; `Import` reads the file again and refuses it when its SHA-256 differs from the
  preview's (`ErrChanged`), saves the whole setup first to `<data>/backups/<slug> before import
  <stamp>.json` (the newest `KeepBefore`, 5, kept), works each section out again, then applies
  every one in one `store.InTx`, in `Sections` order, a `Clearer`'s rows (the launch defaults',
  under Replace) deleted first so the prompts part may delete what they named; the prompts
  part fills in the `Links` (each file nickname's ID here) the launch defaults part finds its
  prompts through, else the library by nickname. Each owner checks what it imports as its own
  setters or editors do (settings' `check…` helpers, shared with the setters), leaving out what
  it refuses with a note: a setting the setter refuses, a profile not on this Mac, a copy of a
  tool that isn't a program here, a location that doesn't exist, a launch default's prompt not
  found (it inherits). Under Replace, prompts are matched by nickname and purpose, the rest
  deleted but one a launch default the import keeps still names; scripts identical to the
  file's keep their IDs. A backup never holds the GitHub sign-in, caches, mirrors, history,
  per-item triage, agent profiles, script trust (a file from elsewhere could pre-approve a
  repository's command), the window's memory, or tool detection. Its `Saver` (`NewSaver`, a
  type of its own, never bound) saves the whole setup before a reset the user asked for, to
  `<data>/backups/<slug> before reset <stamp>.json`, the newest `KeepBefore` of those kept apart
  from the import's.
- `internal/setup/` is the first-run setup's service: where the user is in it and whether it
  is done, kept in `setup.done` and `setup.step` (a `Step`, a named string type), which it
  alone writes through its narrow `Memory` (core's `prefs.Store`), and which a backup never
  holds. `setup.Adopt`, never bound, decides once per database at startup (core's
  `adoptSetup`) whether the setup shows: while no `setup.*` key is stored, done when the inbox
  ever signed in (`inbox.Store.ViewerLogin`, an install from before the setup), else not; once
  decided it never decides again, and a state that was never stored reads as done, so a
  passing problem never traps the user. `SetStep` remembers the step, `Finish` marks it done
  and emits the Go-only `event.SetupFinished` (the shell then asks to allow banners, which it
  held while the setup showed at launch), and `Restart` opens it again from Welcome (Settings
  → About → Run setup again). Its checks: `GitHub` reads the GitHub CLI's sign-in through its
  `Account` (core: `github.WithToken` and `(*github.Client).FetchSignIn`, the one-point viewer
  query `FetchViewer` sends, widened to `login name url`, with the token's `X-OAuth-Scopes`)
  into one of six `GitHubState`s with the command that fixes it, never an error but a check
  the window gave up on; `Terminals` says which terminals are installed
  (`terminal.Registry.Installed`, over `/Applications` and `~/Applications`; Terminal always
  is); `Signals` counts the clones, every repository location's through the shared
  `localrepos.Reader`, that hold each sign the Tools step looks for (a `go.mod`, mage-x's and
  go-pre-commit's config, go-fortress's workflow), fixed paths with `os.Lstat` alone. `Reset`,
  once the user typed `ResetWord`, refuses while runs go in the app (its `Runs`), saves a
  backup first through its `Saver`, refusing the reset when that fails, writes the request
  (`appdata.RequestReset`, `<data>/reset-request.json`), and emits the Go-only
  `event.ResetRequested`, which the shell answers by letting the quit through and quitting to
  open again. It reaches nothing else, and the boundary test keeps it from the inbox, the
  settings, the store, the runs, and the backup, which core adapts.
- `internal/measure/` and `magefiles/measure.go` take the numbers in
  [`docs/performance.md`](docs/performance.md).
- `frontend/` is the React + TypeScript + Vite + Tailwind app. It reaches Go only through the
  generated bindings in `frontend/bindings/`, funneled through `frontend/src/platform/`.
  The React Compiler memoizes the build, so components hold no hand-written `useMemo` or
  `useCallback`, and the gate's render check (`pnpm test:renders`) profiles the compiled
  inbox. Tests stand in for Go only with the shared fake in `frontend/src/test/platform.tsx`.
  Both sheets show a conversation with the same parts, never a copy of them:
  `components/conversation/` (`Description`, `Timeline`, and `TimelineEntry`, which shows a
  review with its line comments), `lib/timeline.ts` (its rows, what is new, a quote, and the
  counts), and `lib/useTimeline.ts` (a visit's start and the earlier pages, joined). The review
  sheet shows it in its Comments tab, beside Files (the description and the diff), each kept
  mounted in an `Activity` while the other shows; the tab is the window's memory
  (`ui.reviewTab`, which a backup never holds), and so is the order both sheets list a
  conversation in (`ui.conversationOrder`, oldest or newest first, under the description),
  which the inbox shares between them.
  `components/notifications/` is the Notifications page (`NotificationsView`, loaded on its
  first visit), whose tabs are the app's own history (`AppNotificationsTab`) and the user's
  GitHub notifications (`GitHubNotificationsTab`, with the Cleared view, `ClearedList`), each
  listed by day (`DayGroups`); the toolbar's bell and GitHub button are quick views of it
  and share its rows (`EntryRow`, `ThreadRow`), so a row is never copied. Its tab is the
  window's memory (`ui.notificationsTab`); each tab's Show and Type live in App's `Nav`, so
  the toolbar's field (`lib/notificationFilter.ts`) counts what the list shows. Opening the
  bell marks nothing: an entry is marked read when opened, by its ✓, by a click on its
  banner (`event.RevealItem.NotificationID`, checked against the entry's item, page, or run,
  since Clear history hands ids out again), or when its item's sheet opens (`InboxView`'s
  `onItemOpened`, once per opening).
  Theme colors come from the `--color-*` tokens `internal/brandgen` writes to
  `frontend/src/generated/theme.css`. Checkboxes and radio buttons take the `checkbox` and
  `radio` classes (`frontend/src/index.css`), which draw them in the app's own style: macOS
  draws a native one grey whenever it counts the window as inactive. Text is sized in rem,
  never px (`text-[0.6875rem]`, not `text-[11px]`): the text size setting scales the root's
  font size, which only rem follows, and the shell passes the stored size in the page's URL
  (`internal/desktop.PageURL`) so the first frame is already at it. A repoguard test fails
  on px text. Only chrome lined up with the native window, such as the toolbar's height and
  its inset beside the window controls, and hairlines stay in px.
- The bound API's shapes: a closed set of string values is a named Go string type with
  constants (`item.Kind`, `settings.Theme`, `sensitive.Scope`, ...), which `magex
  app:bindings` writes as a TypeScript union of its values plus a const object of the same
  name, so a plain `'pr'` still type-checks. A bound result never holds a nil slice or map,
  so the window gets `[]`, never `null` (`internal/core/bound_test.go` checks every bound
  method). Each settings setter writes and reads the settings back in one transaction and
  returns them, so a setter that fails has stored nothing.
- `magefiles/` holds the custom `magex` targets: `brand:*`, `frontend:*`, `app:*`, `db:*`,
  and `measure:*`. Its tests build with the `mage` tag and run the real target code against fake
  tools on an isolated `PATH` (`fakeTool` in `magefiles/helpers_test.go`). Keep new targets
  testable on Linux the same way: a macOS-only target checks the OS, then hands the real work
  to a helper the tests can call, as `App.Build` does with `buildBundle`.

Building and running the desktop app, and the rules it follows on your machine, are in
[`docs/desktop.md`](docs/desktop.md).

Conventions for commits, branches, pull requests, testing, and security live in
[`.github/tech-conventions/`](.github/tech-conventions/README.md).

<br>

## Data layer

The app keeps its data in one local SQLite database. `internal/store` opens it with
`database/sql` over the pure-Go `modernc.org/sqlite` driver, so it builds without CGO. No other
package imports the driver, and nothing imports an ORM (`internal/repoguard` enforces both).

- **Migrations.** The schema lives in `internal/store/migrate/migrations/`: forward-only,
  checksummed SQL files, starting from the squashed `0001_init.sql`. Never edit an applied
  migration; change the schema with a new `NNNN_<name>.sql`. Nothing else creates, alters, or
  drops a table. Never squash or edit a migration a build has shipped: every existing database
  then reads as a history this build disagrees with and is set aside, and only the prompt
  library, the launch defaults, and the user's own scripts come back on their own.
- **Ownership.** Every table has one owner package (`tableOwners` in
  `internal/repoguard/datalayer_test.go`): profiles owns `agent_profiles`, keyed by `(agent,
  name)`, and worklog owns `worktree_records` and `worktree_cleanups`, for instance. The owner
  keeps its SQL in `internal/<owner>/internal/queries/*.sql`, and sqlc generates package
  `queries` beside it, so Go's `internal/` rule lets only the owner import it. Other packages call the owner's API,
  usually through a small interface the consumer declares and `internal/core` wires. SQL
  appears nowhere else but `internal/store`, and a query file never names another owner's table.
- **Adding a query.** Write it in the owner's query file under a `-- name: <Name> :one`
  (`:many`, `:exec`, `:execrows`) comment, run `magex db:generate`, and commit the generated code
  with it. A new owner also adds an entry to `sqlc.yaml`. Never edit a `queries` package by hand.
  `magex db:check` fails when the generated code is stale or a query does not prepare against
  the schema. sqlc on SQLite has sharp edges:
  - Put scalar parameters before a `sqlc.slice(...)`: one after it is numbered into the
    expanded list. Return early on an empty slice in Go.
  - Never mix `sqlc.arg` / `sqlc.narg` with `?` in one query; use all named or all `?`.
  - Never name a parameter after a Go builtin (`max`, `min`, `len`); use `row_limit`.
  - In `sqlc.yaml`, write merge keys in the formatter's `!!merge <<:` form.
  - sqlc mistypes `COALESCE`, so a LEFT JOIN selects the raw nullable columns and the owner maps
    the defaults in Go.
- **Transactions.** `store.InTx(ctx, db, fn)` commits when `fn` returns nil and rolls back on an
  error or a panic. Every transaction begins IMMEDIATE, so one that reads and then writes waits
  for the write lock instead of failing. Inside, run the owner's queries with `q.WithTx(tx)`, and
  join another owner's store with its `WithTx(tx)` (`prefs.WithTx` most often). Never call a
  pool-backed store from inside an open transaction: it waits on the lock the transaction holds.
- **Prefs.** `internal/prefs` owns the `prefs` table. Any package reads any key, but each key has
  exactly one writer: settings writes the user's settings, including the rule keys that the trust,
  snooze, sensitive, notifyprefs, branchrules, reporules, integrationprefs, threadrules,
  statusprefs, updateprefs, and autopilotprefs packages declare (`update.mode` and the
  `autopilot.*` settings among them); inbox
  writes `inbox.*`; prompts writes `prompts.*`; setup writes `setup.*`; update writes its state,
  the other `update.*` keys (the package doc has the full list); Autopilot's engine writes its
  own state, `autopilot.engine`, never a setting. Settings and
  those leaves write a switch and a list of strings one way, through the leaf
  `internal/prefs/prefval` (`Bool` and `EncodeBool`, `List` and `EncodeList`, which stores a nil
  list as `[]`).
- **Values.** Owners stamp `created_at`, `updated_at`, and every other time from their injected
  `clock.Clock`; nothing fills them in. The one exception is a release's follow, whose times in
  its run's result come from the real clock its waits run on (see `internal/release/`). The
  driver stores every `time.Time` as UTC text (`2006-01-02 15:04:05.999999999+00:00`), so
  stamps compare and sort as text in time order.
  JSON columns are TEXT, written with `store.EncodeJSON` and read with `store.DecodeJSON[T]`.
  An owner converts its query rows with `store.ConvertRows`, whose list is never nil, and reads
  a nullable or LEFT JOIN column with `store.Deref`.
- **Self-heal.** `store.Open` backs the database up before applying pending migrations. A file
  that is not a database or is damaged, a schema the migrations did not create, or a history this
  build disagrees with is renamed, with its `-wal` and `-shm` files, to
  `<path>.broken-<UTC stamp>` once no other process has it open; a fresh database starts in its
  place, and `Opened.Reset` reaches the window as a notice. A database from a newer build is
  refused with `store.ErrNewerSchema` and left untouched. Any other failure (a lock, a
  permission, a full disk, a failed migration) is a plain error, and nothing is moved. A reset
  the user asked for (Settings → About, or `magex app:reset`, each writing
  `appdata.RequestReset`'s request) is carried out by the next start, in core's `openData`,
  before anything opens the database: `store.OpenFresh` waits about five seconds for another
  copy of the app to let go, then sets the database aside as `<path>.reset-<UTC stamp>`
  (`ResetRequested`, no error) for a fresh one, putting it back when the fresh one fails;
  `store.PruneResetCopies` keeps the newest three, and the request is cleared. A database still
  held is a busy error, the startup dialog's, with the request kept for the next start; with
  no database file there is nothing to set aside, and a request that can't be read is logged
  and dropped, never carried out. `store.Backup` takes a backup of the open database whenever
  asked, as an update's restart does before it installs, named and pruned exactly as the
  migrations' (`migrate.Backup`: `<path>.backup-<UTC stamp>-v<schema>`, the newest three of
  both kept; `store.Snapshots` reads the schema only from a name ending `-v<digits>` or
  `-v<digits>-<n>`, so a copy renamed `-v4-copy` names none), and `store.RestoreBackup` puts one of those backups back in the database's place
  for an older build that finds a newer schema: it refuses a file that isn't one of the
  database's own backups, waits for another copy to let go, sets the database aside as
  `<path>.broken-<UTC stamp>` with its `-wal` and `-shm`, and copies the backup in, putting the
  database back when the copy fails.
- **Prompts, launch defaults, and the user's scripts outlive the database.** They are what the user made, which
  GitHub can't give back. `prompts.LibraryFile` rewrites `prompts.json` beside the database
  after every change to the library, and `launchdefaults.File` rewrites
  `launch-defaults.json` after every change to the launch defaults or the library, since it
  names prompts by nickname. At startup, when the library is new, `prompts.Restore` brings it
  back before `Seed` runs, from the database set aside or else from `prompts.json`, and core
  (`internal/core/kept.go`) restores the launch defaults from the same place, matched to the
  restored prompts. Core writes each copy at startup only when it is missing or the data was
  just restored, so a loss in the database never overwrites a copy. The Autopilot routines
  keep a copy too: `autopilot.File` rewrites `autopilot.json` (the export format) after every
  change to a routine, and when `autopilot_routines` holds no row at all, deleted ones
  included, `autopilot.Restore` brings them back after the prompts and the launch defaults,
  from the database set aside or else from the copy, each switched off, its spec stored as it
  was (a newer build's stays broken, never rewritten); a routine names its prompts by
  nickname, which the restored library keeps. The user's own scripts
  have no copy, but come back from a database set aside, whatever the reason (`scripts.Restore`,
  beside the restore of the prompts, only into an empty list, each checked as `Save` checks
  it), so a reset keeps them. Agent profiles need no
  copy: discovery finds them on every start, and nothing in the app edits one. `store.OpenSnapshot` reads a kept database of any
  schema, read-only and tolerant of missing columns, for the restore and for **Restore from a
  backup**. Import and restore go through one planner (`internal/prompts/importer.go`), which
  never changes a prompt the user wrote or edited, and checks every imported setting the way
  the editor does. The file format is `prompts.Bundle`, specified in
  [`docs/prompt-files.md`](docs/prompt-files.md); keep the two in step, and raise
  `BundleVersion` only for a change an older build would misread. An import offers the file's
  defaults (`ImportPreview.Defaults`, each a `DefaultOffer` where its prompt fits the slot and it
  would change something) and applies the chosen ones in the transaction of the prompts,
  through the nickname links `importInto` returns. The user's whole setup, these three
  included, also leaves the app as a backup (`internal/backup`, its format in
  [`docs/backup-files.md`](docs/backup-files.md)), whose prompts, launch defaults, and
  Autopilot sections are exactly these three copies' formats; keep the backup's in step with
  them.
- **Tests** open a fresh, migrated database with `testutil.DB(t)`, a given file with
  `testutil.DBAt(t, path)`, and a closed one with `testutil.ClosedDB(t)`. To make a write fail,
  add a trigger that calls `RAISE(ABORT, ...)`, or drop the table.
