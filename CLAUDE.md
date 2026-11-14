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
   that file: no token, and nothing of the user's.
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
build). CI has no macOS runner, so this check is local.

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

Files the shared source doesn't have, such as `.github/workflows/frontend.yml` and
`.github/workflows/sqlc.yml`, are left alone.

Settings → About's **Report a bug** opens `.github/ISSUE_TEMPLATE/bug_report.yml` and fills in its
`environment` field: the versions, then the settings and integrations, which core words
(`internal/core/bugreport.go`, `appinfo.StateReader`) with nothing personal in them, a name or
path of the user's own told only as the default or the user's own, or counted; its test fails on
any. The form is synced, so if a sync renames that field, the button stops filling
it in. The `internal/appinfo` tests read the form and fail when that happens. The `magefiles`
tests likewise fail if the synced `10-mage-x.env` stops pinning golangci-lint, which `magex
app:lint` reads.

<br>

## Layout

- `cmd/nilbox/` is the application entry point: a headless build by default, and the Wails
  desktop app with the `desktop` build tag (`main_desktop.go`). Only the desktop bootstrap
  imports Wails.
- `internal/brand/` holds product identity, design tokens, and the logo mark. The palette tests
  enforce WCAG AA contrast for every text color in both themes. `internal/brandgen/` generates
  the frontend theme tokens, `build/darwin/Info.plist`, and the icon artwork (the app icon
  `build/appicon.svg` and the menu bar icon `build/trayicon.svg`) from it (`magex
  brand:generate`); `magex brand:icons` renders the artwork into the committed icons (macOS,
  needs `rsvg-convert`).
- `build/` holds that generated artwork and `build/appicon.svg.sha256`, the stamp of which
  artwork the icons were rendered from. `build/darwin/` holds the bundle's generated
  `Info.plist` and the rendered icon set, `icons.icns`.
- `internal/core/` assembles the app and builds every service the window binds: settings,
  profiles, prompts, launch defaults, inbox, review, issues, launcher, worktrees,
  notifications, app info, the API budget, sessions, integrations, script runs, release, hooks,
  repository scripts, and the mirror of the user's GitHub notifications.
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
  runs of real tools, a runner, finder, clock, event sink, log, and fake tools); only tests
  may import it. `internal/scriptrun/scriptruntest` holds the runner the
  hooks, scripts, and release suites share, built on it: a real `scriptrun.Engine` and its
  Service over a fresh database, the next start over the same history (`Restart`, `Next`),
  and `Ended` and `Await`, which look again only when the engine's `Changed` says the runs
  changed, never on a timer; only tests may import it either. The repoguard guards on SQL and
  on the git verbs the app runs treat both test-support packages as they treat `_test.go`
  files.
- `internal/event/` holds the app's events, one Go type each with its name; services emit them
  into the one `event.Sink` they are given. `internal/desktop/` is the shell's logic: it hears
  those events, keeps the menu bar current with what `internal/menubar` works out, runs the
  scheduler, the checks follower (`inbox.ChecksFollower`: the listed pull requests whose
  checks were running, but a muted repository's, read in one request through
  `(*github.Client).FetchItems`, and nothing while none runs; a pull request whose workflow
  runs GitHub holds for approval, `item_details.checks_awaiting`, which its card marks held
  in place of the rollup's state, isn't running, and waits for the whole refresh), the sweeper (these three as often as Settings → Refreshing
  says, through `periodic.RunEvery`, which core wakes on the Go-only
  `event.IntervalsChanged` the settings setter emits, so a change applies at once), the GitHub
  notifications' `ghthreads.Syncer` (woken the same way), the budget's `Saver`, the sessions watcher, the Release page's
  refresher, and the Hooks page's scans as tracked work, starts the deferred work
  (`core.Deferred`: the login-shell read first; the tool finder, the runs, and the release
  `Follower` beside it) once after the first paint, never on the cold
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
  through `(*github.Client).do`, apart from two downloads that aren't the API and carry no token:
  a profile picture (`FetchAvatar`, the user's own by the URL GitHub gives, and anyone's by login
  through `FetchLoginAvatar`, which builds the avatar host's URL itself and refuses a login no user
  could have, such as a bot's) and a public release file (`DownloadAsset`, https only, its
  host and every redirect's on `DownloadHosts`). Never build an HTTP request anywhere else, nor run
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
  and `DELETE` and `PATCH` on `/notifications/threads/{id}`.
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
  `Check` says how each stored location reads now. No location is searched recursively.
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
  reaches the network. `LastWords` reads a session's whole last message from the agent's own
  files, on demand, for the review sheet. Each record keeps what its launch reported (the
  permissions it started with, what it did to the item's worktree, and what it worked around),
  which the card's sessions panel shows in place of a line on the row after the launch, and
  `ShowFolder` shows a session's folder in Finder through core's `open -R` Revealer. The
  bound `Forget` deletes an ended session's record on the user's click (the panel's ×),
  refusing one still running (`ErrLive`; failed and unknown count as running) and counting
  one already gone as done, and emits `event.SessionsUpdated`; the prune is otherwise the
  only thing that deletes a session.
- `internal/review/` is the review sheet's service. `Decide` is pure: from a pull request's
  facts it works out every action's offer and the one primary, and `Act` runs it again before
  any write. An offer with warnings asks first (`offers` sets `Offer.Confirm` for it), since
  `Act` refuses it unless the request accepts them; the window confirms a merge with its
  methods and any other action with none. Asking Dependabot to rebase is offered on every open
  Dependabot pull request, enabled once the sheet read it, for a user with push access, while
  the branch is behind or has conflicts; Update branch whenever GitHub's `viewerCanUpdateBranch`
  lets the user (with a warning on Dependabot's), and, disabled with why, on any other pull
  request GitHub reports as behind. Each is the primary only while the pull
  request is behind (the rebase also with conflicts), never as a fallback: with no primary and
  offers enabled, the sheet's button only opens the menu. Every write goes through the inbox's
  `Actor` (the item lock, the account check, and the token retry, never bound, since its
  methods take funcs), is pinned to the head the user saw (an approval reads the head first,
  since GitHub takes a review of an older commit), and a merge first reads `mergeStateStatus`
  fresh and merges only when GitHub says it can, so an admin's token never merges past branch
  protection on its own: only `merge_bypass`
  ("merge past the rules") does, offered when GitHub sets `viewerCanMergeAsAdmin`, never the
  sheet's button, and with each rule it skips to accept. An UNSTABLE pull request's readiness
  line and merge warning say what GitHub counts as not passing: optional checks failing or
  running, and the head commit's workflow runs that ended without a check (`Unrun`, read in
  the review query from the head commit's own check suites, `headSuites: commits(last: 1)`,
  which the details fragment asks for under the same alias and arguments, so GraphQL merges
  the two and a fork whose rollup is null still shows them: a fork's runs awaiting approval,
  each with its run's ID and page, or runs that failed to start), which the sheet names
  beneath its checks, each a link to its run. Runs awaiting approval are approved with
  `approve_runs` ("Approve workflows to run"), offered in a bar under the sheet's header and
  in the menu, never the button: held back until the sheet read every one's run ID (a cache
  an older build kept has none), for a user with write access, and asking first, with a
  warning, when the pull request changes workflow files. `Act` approves the runs the cache
  read at the head the user saw (`loadAct` refuses another head as `pushed_since`), one REST
  `POST .../actions/runs/{id}/approve` each (`(*github.Client).ApproveWorkflowRun`, through
  `edit`), never one twice across the token retry; once GitHub approved any, a refusal of the
  rest is the result's `Reason` (`runs_approved`, "Approved 2 of 4 ..."), not an error. The
  sheet stays open after it, with the draft, and reads again (`ReadAgain`) at once, then
  three seconds apart up to four more times, until fewer runs are held and a check is
  running; each read rewrites the card, which the checks follower then keeps current. Each step is reported as an
  `event.ReviewProgress`, which the sheet shows while the user waits. Line comments are checked
  against the cached diff (`hunks.go`) before anything is sent. `Decide` marks every offer with
  what its text goes with (`Offer.Reviews`, `Offer.CommentsFirst`, and `Offer.CommentBlocked`,
  why GitHub can't take the comment now), and `Act` and the window both go by those marks. A
  merge with no review of its own (`merge_now`, `merge_bypass`, `auto_merge`) posts the
  composer's text and line comments, when there are any (by `textutil.HasText`, which trims as
  the window's `trim()` does), as a COMMENT review at the head the user saw, the way `comment` does
  (never an approval, so it works on the user's own pull request), but only once the fresh merge
  state says GitHub would merge (auto-merge: that it is open, not a draft, at that head), so a
  refused merge strands no comment; then it merges exactly as it would without them: no
  auto-merge fallback, and `merge_bypass` still needs its rules accepted. A comment that fails
  merges nothing, and once one is posted nothing after it is an error: a merge GitHub won't make,
  or any failure, is `commented_not_merged`, a result whose words, like `Result.Commented`, say
  the comment was posted, so the sheet drops the text and never posts it twice. A close
  (`close`) is offered on an open pull request, held back ("Wait for it to load") until the
  sheet read it, then only when GitHub's own `viewerCanClose` says so (`Facts.CanClose`, read
  by the review query and kept in the cache), and always confirmed. It is never the button:
  the window runs it only when picked from the menu (`menuOnly`), never keeps it as the chosen
  action or runs it with ⌘↩, counts it as nothing to do, and sends with it only text the user
  wrote or picked, never a default reply. GitHub takes no head to pin a close to, so every try
  (a retry with a fresh token included) reads the head first (`FetchMergeState`): one no
  longer open is refused, and one pushed to is `pushed_since` with nothing sent, or, after the
  comment, `commented_not_closed`. Then it posts the text and line comments as a COMMENT
  review the same way, and closes with one REST `PATCH` (`(*github.Client).ClosePull`, whose
  refusals `closeError` words as a close, never as a merge or a push). A comment that fails
  closes nothing, and anything that fails after it is `commented_not_closed`, decided in one
  place (`failedAfterComment`). A merge or a close takes the pull request out of every list
  (`Acted.Merged`, `Acted.Closed`) and drops its cache. It owns `review_diffs` and
  `review_diff_files`, a bounded cache of the diffs it read (per pull request, in all, and by
  age), pruned in the same transaction that writes. A merge that went through tells its
  `Merges` the repository, without waiting (core: the Release page's `Refresher`, which reads
  that repository's row again); a close tells it nothing. Nothing here lets an agent approve,
  merge, or close.
- `internal/issues/` is the issue sheet's service, beside the review sheet's: neither imports
  the other (the repoguard boundary test), and they share only the building blocks. `Decide`
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
  `Earlier` reads the timeline's page before a cursor through `PeekIssue`, and stores it
  nowhere. Every action goes through `ActIssue` and tells each step as an
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
  KiB, then emptied, each cut marked; 200 issues, 32 MiB, and 14 days unused in all), pruned in
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
- `internal/avatars/` keeps the profile pictures of the people the issue sheet shows (who opened
  the issue, its assignees and participants, and who wrote each comment shown in full, never a
  bot), which the window asks for in one batch through the issue sheet's bound `Avatars` once the
  sheet renders, so the conversation never waits on one. It owns `avatars`, one row per
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
- `internal/ghthreads/` mirrors the user's GitHub notifications (threads, in the REST API's
  words) as the web inbox lists them, read and unread but not done, in `gh_threads`, which it
  owns, one row per account and thread (its reason, unread, times, subject type and title,
  repository, the pull request or issue `Thread.Ref` names, '' and 0 for any other subject,
  and `latest_comment_url`, kept for the next check of who acted after a moment and never
  shown or opened), and keeps the state of its passes in `ghthreads.sync`, a JSON pref only it
  writes. Its one write to GitHub is clearing a thread (below), and it sends none until the
  user turns clearing on or clears one by hand. A pass, one at a time (the Syncer's
  and `CheckNow`'s share one), reads the rules through its `RuleSource` (core's
  `ghThreadRules`, one read of the prefs): with `Read` off it sends nothing and borrows no
  token, and marks the mirror stale (`walkNext`). Otherwise it borrows a token through
  `github.WithToken`, learns its login with `FetchViewer` (remembered per token in a
  `github.ViewerCache`, the inbox's building block too: one GraphQL point per new token), and
  lists through its `Client` (`*github.Client`): page 1 with the ETag its last answer
  carried, a 304 ending the listing at no cost, and a 200 reading on (`ThreadPage.NextPage`)
  until a page whose oldest thread is no newer than the high-water mark, the newest
  `updated_at` the last stored listing saw. A 304 covers page 1 alone, so every page is
  walked, with no ETag, up to `github.MaxThreadPages` (the newest 1,000, the mirror
  `Truncated` when GitHub lists a page past them, `ThreadPage.Capped`): at the first pass
  after the Service is built, every `FullWalkEvery` (6 h), after the account changes (another
  account's ETag and mark are never used, and its rows, never shown, are dropped once unseen
  for 30 days), once reading is back on, and after a refusal; a pass that reads to the last
  page counts as one. A 200 drops the account's rows it didn't list within the span it read
  (done on GitHub): every one for a walk, and, for a pass that stopped at the mark, those
  updated strictly after the oldest thread on its last page, since GitHub lists the newest
  updated first; a thread updated while the pages are read may be dropped once, and listed
  again by the next pass. A pass stores its listing in one `store.InTx` at its end, rows and
  `ghthreads.sync` together, so one that fails partway stores nothing, and never an ETag or a
  mark the pages it meant to read don't back. GitHub refusing the listing to the token
  (`github.NotificationsRefused`: 404, or 403 but a rate limit) marks access refused ("Your
  GitHub token can't read notifications."), and no listing is sent for `RefusedFor` (6 h)
  unless `CheckNow` asks; any other failure is only the status's last error
  (`uierr.Message`), and the next pass tries again. Then, a 304 pass included, it checks the
  state of the pull requests and issues its threads are about, each once, in one
  `FetchItemStates` call: those never checked, those whose thread updated since, and those
  not known merged or closed and checked `StateRecheck` (30 min) ago; releases, discussions,
  check suites, and commits are counted, never checked. One GitHub can't find is stamped
  checked, its state kept, and a check that fails stores only the last error, the listing
  standing. The `Syncer`, a desktop worker after the scheduler and woken with it
  (`followed.intervals`), runs a pass at once, then the longer of the inbox's refresh interval
  and GitHub's `X-Poll-Interval` after the last pass ended, its own or `CheckNow`'s (a timed
  pass that finds one ended while it waited for the running one lists nothing), in its own
  select loop, recovering and logging a panic; its tests run in a `testing/synctest` bubble.
  The bound `Snapshot` reads the database alone, for the account the last pass read: the
  status (reading, the account, `Access`, the last pass and walk, the last error, when a
  refused listing is sent again, the count, the unread count, and `Truncated`), `ByItem` (each
  pull request or issue with threads: unread, total, and reasons) for the cards' tags, and
  the newest 20 `Unread`, none with a URL GitHub gave; with reading off, its lists are empty.
  `CheckNow` runs a pass once any running has ended, lifting the back-off and ignoring the
  poll interval, and returns the `Snapshot`, whose status tells a pass that failed. A pass
  that changes what `Snapshot` shows, the passes' times aside, emits `event.GHThreadsUpdated`
  (`ghthreads:updated`), but one whose caller gave up tells nothing.
  It clears the threads by the user's rules (`clear.go`), and owns `gh_thread_moments` and
  `gh_thread_clears` for it. The inbox's `Config.Outcomes` is its `Settler` (core's
  `ghThreadOutcomes` adapts `inbox.Outcome` to its own `Outcome`, never bound), whose
  `Settle` records, in one transaction and without waiting, the moments the user finished an
  item in the app (a merge, a close, a review that approves or requests changes, a
  dismissal, taking themselves off), a merge's or a close's state as GitHub answered it
  (`MERGED` or `CLOSED`, read now), and a restore's taking back of the dismissal's moment and
  its queued clears (kept), then nudges the `Syncer`, which runs a pass on those items at
  once, listing first only when the mirror is older than the poll interval (a minute at
  least, `MinNudgeListing`); an item the inbox saw close or leave its lists has its state
  checked. A pass reads the account's threads, moments, and clears once (`decideInputs`),
  and again only what a step changed: the threads once a state check stored an answer, and
  the clears inside the transaction that queues, since a click may have queued or kept one
  meanwhile; a send reads its one item's moments. After its listing and state checks, a
  pass decides each pull request and issue thread with `threadrules.Decide` against every
  moment that could apply: the user's own, and GitHub's own merge or close
  (`outsideMoment`, only on a state read within `StateFreshFor`, an hour; one older that
  would clear is read again first, but not while a backlog counted under the same rules is
  held for review, `BacklogRules` in `ghthreads.sync`: such threads count toward the cap
  as they are, so a held backlog costs no state reads). A thread held back, unread and
  updated after the moment, has who acted after it checked once per update of the thread,
  and only while a bot's activity counts: the latest comment's author (`FetchLatestComment`,
  one REST point, `MaxCommentChecks`, 150, a pass at most, the rest the next pass, the checks
  stopping at the first answer that says GitHub is limiting the app's requests,
  `github.RateLimited`), stored as `after_moment`, a person's sticking until the thread is
  read (a person's comment followed by a bot's before the next pass reads as a bot's). A
  latest-comment URL that is the subject's own is an event, not a comment, and needs no
  request: on an item merged or closed it is the merge or the close itself, a bot's; on an
  open one (a push, a request for review, an approval with no text) it counts as a
  person's. A thread that clears is queued in the ledger before anything is sent, with its
  trigger and action (`threadrules.action`: done, a `DELETE`, or read, a `PATCH`): a clear
  the user's own action chose waits the Undo hold (`Hold`: the toast setting plus 3 s, 15 s
  at least, through core's `ghThreadHolds`), one GitHub's state chose goes at once, and the
  latest hold among the moments that clear wins. One the user kept (`Keep`), and one whose
  clear failed, is not queued again on its own until its thread moves (`marks`); the user
  may still clear it by hand. The first pass after clearing turns on or the rules change
  (`RulesSeen`, a fingerprint in `ghthreads.sync`) queues at most `FirstCap` (25), any later
  one `Cap` (150); one over its cap queues none and records the count as the `Backlog`. The
  sends go one at a time through the client's chore lane, from a pass or from a run the
  `Syncer`'s timer starts at the next `send_after` or when the lane lets the next go, which
  lists nothing and lets go of the pass slot at the lane's first refusal, and each is checked
  again first (`sendOne`): clearing still on (but for one by hand), reading on, GitHub not
  refusing the listing, the token's login the row's (else the queue waits for the next
  pass), the thread still listed at the `updated_at` it was decided on (else decided again;
  one not listed is skipped, never sent), queued less than a day ago (else decided again),
  and `Decide` still clearing it. A 404 counts as done; a refusal of the lane costs no
  attempt; any other failure is tried again a minute on, three times at most, then failed. A
  thread marked done leaves the mirror, and one marked read stays, read. Clearing turned off
  (settings' Go-only `event.ThreadRulesChanged`, which core's relay turns into the `Syncer`'s
  `RulesChanged`) skips every queued clear but the user's own at once, and reading turned off
  skips them all and sends nothing. The bound `Preview` lists, from the database alone and as
  if clearing were on, what would clear, by repository, and what is held back, by reason;
  `Clear` and `ClearItem` clear threads by hand (no hold, with clearing on or off, never a
  thread that isn't a pull request's or an issue's); and `Keep` takes queued clears back. The
  `Snapshot` also lists the `Queued` clears, the `Backlog`, and the `Recent` 50 that settled.
  Moments and settled clears are dropped 30 days on. It never imports the inbox, settings,
  core, notify, the sheets, or the desktop shell, and the inbox never imports it (the
  repoguard boundary test).
- `internal/bump/` reads a dependency update from a bot's pull request title into an `Update`,
  for the review sheet and the `.Dependency` prompt variables. It is pure and imports no
  service.
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
- `internal/gitread/` is the read-only git that release, hooks, and Repo scripts share: an exact-argv allowlist
  plus validating predicates (a branch, a tag, a commit, a `v*` pattern, an absolute clean folder)
  that refuses anything else with `ErrGitCommandNotAllowed` before git runs, and typed helpers over
  it (the current branch, origin's default branch as the clone last learned it from its own
  `refs/remotes/origin/HEAD`, an operation in progress, uncommitted changes, each dirty file with what
  the index holds for it, unpushed commits, the commits between two, tags here and on origin, signing,
  the hooks folder).
  Every command runs as `git -C <dir> -c core.fsmonitor=false -c submodule.recurse=false` with
  `GIT_OPTIONAL_LOCKS=0`, `GIT_TERMINAL_PROMPT=0`, `GIT_NO_LAZY_FETCH=1`, and an empty
  `GIT_ALLOW_PROTOCOL` (no read reaches a transport, so a partial clone's missing object is never
  fetched through a URL the clone names), without `GIT_CONFIG` (which would hide the clone's
  settings from the guards' reads), never touches `core.hooksPath`, and an `ls-remote` gets a
  15-second limit and a batch-mode ssh unless the user's environment or own config picks one. A
  `status`, which reads files and so would run their filter drivers, first reads the drivers
  with `config --show-scope` and runs with every one the clone's own config defines turned off,
  Git LFS's too, since git-lfs's clean writes into the clone (its clean, smudge, and process
  emptied and required off),
  and with `--ignore-submodules=dirty`, so it runs no status inside a submodule, whose own config
  could name one. An `ls-remote` likewise first reads, with `config --show-scope`, the settings
  it weighs, and origin's URL with `ls-remote --get-url` (which asks origin nothing): a remote
  helper (a `<name>::` URL after `insteadOf`, or `remote.origin.vcs`) is refused
  (`ErrRemoteHelper`), since git falls back to an alias the clone defines; only the file, http,
  https, and ssh transports run, as the user's own config and environment allow them, through
  `GIT_ALLOW_PROTOCOL`, which beats every config; the clone's own ssh command, `core.askPass`
  (git runs it even with `GIT_TERMINAL_PROMPT=0`), and credential helpers give way to the
  user's own; an http or https origin the clone's own config reroutes (its TLS checks,
  certificates, proxy, `remote.origin.proxy`, or resolved address) is refused
  (`ErrCloneRoutes`), since the user's credentials, cookies, and headers for the real host
  could go elsewhere; no cookie is saved, and a cookie file the clone names is dropped; and
  origin is asked for git's own upload-pack with `--upload-pack` (git keeps the first
  `uploadpack` it reads, so a `-c` can't replace the clone's). An `ls-remote` that fails after setting anything aside returns a `SetAsideError`
  naming it, which `Message` (the words the Release sheet and Repo scripts' branch status
  share for every gitread failure) gives as the likely cause and where to set the clone's own
  settings instead: the user's global config, with an `includeIf` for the folder, which counts
  as the user's own. Only the system, global, and environment (`command`) scopes are the
  user's. Reading a clone runs no program the clone names (the user's own, such as Git LFS's,
  still run). Its allowlist test asserts that no entry holds a verb `TestNoGitCommandChangesAClone`
  guards. For the git commands that do change a clone, run elsewhere (Repo scripts' pull,
  and worktree creation's fetches, `ls-remote --symref`, and `git worktree add`), it hands
  over what they run with rather than running them: `RemoteSetup` (origin) and
  `RemoteSetupFor` (a named remote, whose own URL and remote helper it weighs), for one that
  reaches a remote, and `CheckoutSetup`, for one that writes files and reaches no remote.
  Each reads the clone's whole config once, includes followed, with `config --show-scope
  --show-origin -z --list`, and weighs every setting of the clone's own (the local and
  worktree scopes, and whatever they include) by the three classes in `cloneconfig.go`, the
  one place they are listed. Inert (`inertKeys`): what git init, clone, remote, branch,
  submodule, sparse-checkout, and Git LFS write, and the notes editors and git clients keep,
  which none of these commands reads as a program, a place to write, or a way to reach a
  host: `core.{repositoryformatversion, filemode, bare, logallrefupdates, ignorecase,
  precomposeunicode, symlinks, autocrlf, eol, sparseCheckout, sparseCheckoutCone}`,
  `index.sparse`; `remote.<n>.{url, fetch, pushurl, tagopt, prune, promisor,
  partialclonefilter, gh-resolved, gtserviceaccountidentifier}`; `branch.<n>.<any>`;
  `extensions.{partialclone, worktreeconfig, objectformat, refstorage}`; `user.{name, email,
  signingkey}`, `commit.gpgsign`, `tag.gpgsign`, `gpg.format`; `pull.{rebase, ff}`,
  `fetch.prune`; `submodule.<n>.{url, active}`, `submodule.active`;
  `lfs.repositoryformatversion`, `lfs.<url>.{access, locksverify}`; `rerere.{enabled,
  autoupdate}`; `log.excludeDecoration`, `color.*`, `gui.*`; and `mergetool.*`,
  `difftool.*`, `merge.tool`, `diff.tool` (and their gui ones), which only `git mergetool`
  and `git difftool` read, and which the app never runs (`TestNoGitCommandRunsAMergeOrDiffTool`);
  `remote.<n>.github-pr-remote` among a remote's notes; and what users and their tools write
  for commands the app never runs, `push.*`, `rebase.*`, and `status.*` (no subsection; the
  worktrees' own status is `--porcelain` with its own `--untracked-files`), `gitflow.*`,
  `init.defaultBranch`, `merge.ff` (the pull's `--ff-only` beats it), and
  `merge.conflictStyle`, none of which names a program, a path to write, or a host
  (`core.editor`, `sequence.editor`, and `diff.*` stay refused). Neutralized (`neutralizedKeys`), as
  the setup never lets the clone's own value reach git: `core.sshCommand`, `core.askPass`,
  `credential.helper` (for every URL or one), and the cookie settings, which the remote
  guard sets aside for the user's own in every setup (a checkout's Git LFS asks for
  credentials too), a failure then saying which (`Setup.SetAside`, `SetAsideNote`), and of
  the rest of `credential.*` only `username` and `useHttpPath` (`protectProtocol` and
  `sanitizePrompt`, which a command-line setting can't beat for one URL, refuse, and every
  setup sets both on again, `credentialHardening`); `url.*.insteadOf` and `pushInsteadOf`, as each remote
  reached is weighed by the URL git will use; `http.*` and `remote.<n>.proxy`, whose
  rerouting keys (`routeKeys`, a remote's proxy) refuse a command that reaches an http or
  https remote, or one Git LFS may run in, whatever origin's scheme (`cloneRoutes`:
  git-lfs reaches its server over https with the user's credentials), with
  `ErrCloneRoutes`; `protocol.*`, which `GIT_ALLOW_PROTOCOL` beats; `core.hooksPath`,
  `core.fsmonitor`, and `submodule.recurse`, which every setup sets again (husky writes the
  first); and `maintenance.*` and `gc.auto`, as every setup turns automatic maintenance off.
  Refused, before anything runs, with a `CloneConfigError` naming the settings and where
  each is set, which `Message` words with where to set them instead (the user's global
  config, with an `includeIf` for the folder): everything else, `refusedKeys`
  (`branch.<n>.mergeOptions`) among them, so a hook (`hook.<n>.*` runs whatever it says), a
  filter but Git LFS's own with exactly its own commands (`lfsSettings`, `--skip` ones
  too), `core.worktree`, `include.*` and `includeIf.*` (an `includeIf` of
  `gitdir:**/.git/worktrees/**` or `onbranch:` reaches only a worktree being made),
  `core.alternateRefsCommand`, `gc.*` but `gc.auto` (`gc.recentObjectsHook` runs a program),
  `lfs.customtransfer.*`, `lfs.standalonetransferagent`, `lfs.url`, `remote.<n>.{uploadpack,
  vcs}`, a merge or diff driver, and any key a later git adds. `CloneConfig` runs that check
  alone, for Repo scripts' branch status. The user's own scopes (system, global, and the
  environment's) are never weighed, so an `includeIf` in the user's global config still
  counts as theirs. Past the allowlist, the setup checks that git takes the folder as its
  clone's top folder, comparing the two as files (`os.SameFile`, so a clone root whose case
  differs from the disk's still matches; `ErrWorkTree`), weighs the remotes the command
  reaches exactly as an ls-remote's guard does (a `RefusedError` naming the remote), and
  returns a `Setup`: git as `GitFinder` finds it, the finder core hands the worktrees too
  (`ErrNoGit` when it can't); an ls-remote's environment (for `CheckoutSetup`, and a
  setup's `Checkout`, the same allowing no transport) with `GIT_DIR` and `GIT_WORK_TREE`
  set to the clone's folders, which beat its config, and `GIT_NO_REPLACE_OBJECTS=1`, so a
  replace ref can't have a checkout of the commit the launch checked write another's
  files; and every setting, `Hardening` (`core.hooksPath` at an empty folder, so no hooks
  folder's hook runs, and `hook.<event>.enabled=false` for every event git fires,
  `hookEvents`, which git 2.56 documents as turning off every hook of the event, the
  user's own config hooks too, but which a git whose config hooks predate that switch may
  read as one hook's switch: the clone's own `hook.*` settings are refused regardless; no
  fsmonitor or submodule recursion), `fetchHardening` (`core.alternateRefsCommand=true`,
  so a fetch from a clone with alternates runs nothing to learn their refs, an empty one
  making git try to run "" on every fetch; `maintenance.auto=false` and `gc.auto=0`, so no
  fetch or merge starts maintenance), `credentialHardening` (`credential.protectProtocol`
  and `sanitizePrompt` on), the remote guard's (the clone's own password program
  and credential helpers set aside, no cookie saved), and any filter driver of the clone's
  own but Git LFS's own turned off, passed through `GIT_CONFIG_COUNT` after any the
  environment passes (`Setup.Settings` lists them), so the command's argv holds only its
  own words. `LocalSetup` works out a `Local` for a command in a clone or a worktree that
  reaches no remote, the worktree service's own status, removal, prune, unlock, and branch
  deletion: a read's environment (no transport, no lazy fetch, no optional lock, no
  replace ref), `Hardening`, `fetchHardening`, and every filter driver of the clone's own
  off, Git LFS's too, with `Local.Refused` saying when the clone's own config holds a
  setting a setup would refuse. In a partial
  clone (`extensions.partialClone`, `remote.<r>.promisor`) either may fetch a missing file's
  contents: its promisor remotes are weighed as a remote is, and the setup leaves
  `GIT_NO_LAZY_FETCH` out and allows the user's transports (`Setup.Partial`). Only an
  ls-remote has `RemoteTimeout`, whose end is `ErrRemoteTimeout`, which `Message` words as
  origin not answering in time. `ValidBranch` is the branch rule a command's argument must
  pass.
- `internal/release/` is the Release page's service. `Candidates` lists every clone in the
  repository locations whose origin is on GitHub, grouped as unreleased, up to date, no releases yet,
  can't release here, or muted (`GroupMuted`: a clone of a repository muted as the list is read
  is listed with its name and folder alone and never asked about, a row muted later keeps what
  GitHub said and is served regrouped, through `arranged`, which also puts pinned ones first in
  their group, and unmuting a row never read reads that repository alone, as a release's patch
  does, `liftMuted`; the rules are read through its `RuleSource` on every call), from two rounds of batched GraphQL (`(*github.Client).FetchReleaseStatus`,
  then `FetchReleaseCompare` for the repositories with a version, 15 to a query, adapted in core),
  and reads each unreleased clone's state with `gitread`, never asking origin. It serves the
  last list however old it is: the whole list is read again only on the page's **Read GitHub
  again**, for a list from other locations, for one out of date, or with none yet (and
  behind a list kept from a past start, below), never on a timer. A refresh that fails keeps
  the last list, with its failure, until the user reads it again. A refresh tells the window
  how far it has got, at most four times a second (`event.ReleasesProgress`: finding the
  clones, each round's repositories as GitHub answers each batch, then the unreleased
  clones), and each list that lands is kept in the `release.lastCandidates` pref with the
  locations' `Key`. After a restart, the first `Candidates` serves that list at once, marked
  `Kept`, while the `Refresher` (a desktop worker) reads GitHub again behind the page and
  `event.ReleasesUpdated` says when it lands; a list from other locations is never served,
  and a refresh that fails or is cut short keeps nothing. `version.go` works the next version
  out exactly as magex does, tested against a table recorded from the real magex. A release
  follows the review sheet's template: the `Gatherer` reads its `Facts` at once (magex through
  a `ToolSource`, GitHub's status of the one repository, origin's refs with one `ls-remote`,
  and the clone with `gitread`), and the pure `Decide` works out the sheet's `Offer`: every
  check, every bump's version over the clone's and origin's tags, the Major gate, and the
  argv, `push=true` (never the bare word) plus `expect=`, `json=true`, and `ff-only` only for a
  magex whose `Definition.Features` has them. The bound `Prepare` takes the sheet's choice and,
  within `FactsFor`, decides again on the facts the sheet was shown, so a choice asks GitHub
  nothing; the bound `Start` hands `scriptrun` a release whose `Before`, under the clone's lock,
  gathers the facts once (origin and the clone afresh, GitHub's from the sheet while under
  `FactsFor` old) and decides there what magex runs, so a release that isn't ready, whose
  version moved, or that the sheet should never have sent (a major without its typed version,
  its go.mod box, or a magex with `expect=`, logged as a bug) never runs. The run's
  `OutputReader` turns magex's lines into steps, warnings, and unrecognized lines (the run's
  result leaves out the warning every release has, that the newest version tag isn't on HEAD;
  the log keeps it, and the window's log view leaves out magex's JSON result line), and its
  `Finish` verifies the release whatever magex's exit status: `ls-remote` for the tag, peeled to
  its commit, then the clone's own tag, its branch, and any rebase in progress, into a `Result`
  (released, not pushed, failed with `ExplainFailure`'s heading, or unverified). Only this
  run's tag succeeds: origin's must point at the commit magex's JSON result names, or the
  clone's tag that magex said it made; any other is someone else's (`Taken`). The commands it
  offers to copy are shell-quoted. `release.Verifier`, which core registers for releases,
  verifies one left from before a restart the same way. The reader's `Progress` turns Stop off
  from magex's switch to the branch until its pull ends, and asks first once the tag exists; a
  release that reaches GitHub is told once its outcome is recorded (the run's `Recorded`),
  through its `Notifier` (core: the notify `Dispatcher`'s `Notify`, a `release_done`
  notification about no item, and its banner). A release that ends doesn't put the page's
  whole list out of date: its `Finish` hands the repository to the `Refresher` and emits
  `event.ReleasesUpdated` at once, and the list reads as `Refreshing` until the patch lands.
  The `Refresher` reads the repository alone again (its status, a comparison only when its
  newest version isn't on its newest commit, and its clones' state, telling the window how
  far it has got with `event.ReleasesProgress`), once any refresh under way has landed (one
  read of the list at a time), patches those rows into the list, however old, which stays as
  current as it was and is kept for the next start, and emits `event.ReleasesUpdated` again.
  A refresh that began after the release ended has read the repository already, so the patch
  asks GitHub nothing. A list it can't patch (none yet, other locations', out of date,
  kept from a past start, one whose last refresh failed, without that repository, or GitHub
  failing) goes out of date instead, as it does when the `Refresher` isn't running or stops
  with a patch still queued, and a refresh under way when a list goes out of date lands out
  of date too. A merge in the review sheet is patched in the same way: the review service
  tells its `Merges` (core wires the `Refresher`'s `Merged`) the repository a pull request
  merged into, and the `Refresher` reads it again, but only while the list could be patched
  then, so a merge never puts the list out of date. The bound
  `ReleaseResult` reads a run's outcome back as a typed `Result`, and the bound
  `OfferAfterMerge` answers the review sheet's offer after a merge from local state alone: the
  `integrations.offerAfterMerge` setting, magex, the default branch the Release page or the
  inbox already holds, and a clone with that origin, from the page's list or else only the
  folders inside the clone root and each folder location named as the repository is, then
  each clone location (one git command each), never a scan of a location.
  The `Follower`, a deferred desktop worker, follows each release on GitHub to *published*: a
  release's `Finish` (its `Verifier`'s too) starts a `Follow` in its `Result` (its state,
  `until`, and `polls`) when the Service has `Workflows` and `Records`, and its `Recorded` hands
  it to the `Follower` once the outcome is kept. It polls a minute after a follow starts or
  resumes, then every three minutes, at most `FollowPolls` (20) over `FollowFor` (45 minutes),
  on the real clock for its limits and its waits alike (`clock.Sleep`; tests run it in a
  `testing/synctest` bubble). Each poll is `(*github.Client).FetchReleaseProgress` (core's
  `ReleaseWorkflow`): the tag's release, one GraphQL query through `postGraphQL`, and, until
  it is published, the tag's own workflow runs, one REST request through `do` (`GET
  .../actions/runs` by the push event, the tag as the run's branch, and the tag's commit, each
  checked again on the answer), so the budget counts both. *Published* (`publishedAt` set, not
  a draft) wins; while one of the tag's runs goes on, the follow goes on; once they all ended,
  one that failed, timed out, or never started ends it as failed, named by its workflow; all
  of them ending well two polls in a row with no release published ends it as
  `unpublished`, and no run of the tag's by the third poll as `noWorkflow`. Each poll that
  leaves the follow going is written into the run's record with `scriptrun.Engine.Amend` (an
  ended run's summary and outcome, in one transaction with its read, and only while the
  record still says this follow goes on). An end is settled the same way, written again a
  few times a moment apart without asking GitHub again, and, if it still can't be, logged
  and let go of for the next start's resume. *Published* (`Notifier.Published`,
  `release_published`, opening the release) and *failed* (`Notifier.WorkflowFailed`,
  `release_failed`, opening the run, or else the tag's Actions runs) are told only once the
  record holds them, with a context the quit doesn't end; `unpublished`, `noWorkflow`, and
  the limits get the result's line alone, no banner. As it starts, it resumes every release
  whose record says it follows (`Engine.Ended`, which reads only a kind's ended runs), within
  the same limits; a quit leaves the follow recorded for the next start.
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
- `internal/scripts/` is Repo scripts' service. While the Scripts integration is off (its own
  switch, whose automatic is on while mage-x or go-pre-commit is), `List` returns the page
  marked off and `Run` and Run again refuse. Otherwise `List` reads a clone's sources in the
  page's order (the user's own scripts, then `t3.json`'s `scripts`, the
  subset of `.vscode/tasks.json` the app can run, read through `jsonc.go`, and magex's
  targets), as the `scripts.repoFiles` and `scripts.magexTargets` switches in
  `integrationprefs` turn them on, and runs nothing in a clone: the files are read, magex's
  built-ins come from `magex help:commands json=true` run in a scratch folder of the app's
  own (a clone's `.github/env` files would override magex's environment, `PATH` included),
  kept per copy, a list that failed too, until another copy or `List`'s refresh, and the
  repository's own targets from parsing `magefiles/` with `go/parser` by magex's own rules,
  kept until a file there changes. An entry the app can't run is listed greyed with the
  reason in words: an editor variable such as `${file}`, `${command:…}` or `${config:…}`, a
  password input, another task type, a character that hides what runs (a control character
  but tab, DEL, or a bidirectional text control, in any command, argument, folder,
  variable, shell, or input value), a task shell outside the POSIX family (sh, bash, zsh,
  dash, ksh, mksh) given arguments to quote, a process task setting variables for a program
  whose path holds an `=`, magex's own updaters (`update`, `update:*`, `version:update`, and
  their aliases: Settings → Integrations updates magex), and the custom targets while
  `magefiles/` holds a link to a folder. A pickString option shows its value beside a label
  that differs (`Option.Shown`). A command that tags a version (`version:bump`,
  `version:tag`, `git:tag`, `git:tagupdate`, `git:tagremove`, or a magex alias of one) points
  to Release (`Anyway` runs it), checked again on the command as it will run; goreleaser's
  `release:*` targets are plain built-ins. A command from the repository's files asks before
  its first run and whenever it changes (D13): its trust is the SHA-256 of the canonical
  JSON of its effective form, `${workspaceFolder}` and `${env:…}` as written, a custom magex
  target's holding every file under `magefiles/` by its bytes (only those: not `go.mod` or the
  packages it imports, the known limit), recorded in `script_trust` under the clone's origin
  with the display text trusted; magex's built-ins need none. `Run` hands `scriptrun` a
  `KindScript` run whose `Before`, under the clone's lock, reads the source again from its
  bytes, refuses a command whose hash isn't the one worked out as the run was asked for,
  works the command out, refuses a version bump, then one that isn't the one the user
  confirmed (or, with no confirmation, the one they trusted), and only then remembers a
  confirmation they asked to keep, so what runs is what they saw. A refusal that asks is a
  `QuestionError` whose `Question` holds the command with the chosen inputs filled in;
  `desktop.MarshalError` sends any error's `Detail` to the window beside its message. A
  repository's shell command (a `t3.json` script or a shell task, which assume POSIX quoting,
  and which the app quotes for one) runs as `[$SHELL, -c, command]` when the login shell is
  of the POSIX family (sh, bash, zsh, dash, ksh, mksh), else under `/bin/sh` (a task's
  `options.shell` as written, with `-c` after its arguments as VS Code adds it), a process as
  its argv, a magex target as `[magex, target, key=value…]`
  with magex's `RunEnv`, and a `dependsOn` chain, or a part in another folder, as one `/bin/sh
  -c` line of shell-quoted parts joined by `&&`. Its `Rerunner` works Run again out from the
  run's intent and asks nothing, so it runs a repository command only while a trust record
  under the clone's current origin matches it; anything else is run from the page, which asks.
  The user's own scripts live in `user_scripts`, which it owns, in their order (positions
  0, 1, 2, numbered again in the `store.InTx` of every create, delete, duplicate, or move).
  The bound `Save` creates one (ID 0, at the end) or edits one, checking that the name and
  command are there, on one line, with no character that hides what runs, that the scope
  (`all`, `go`, or `repos`), the place (an `integrationprefs.RunsIn`, `''` for "As set in
  Settings"; `validPlace` checks it and a run request's alike), and the icon (t3.json's
  five) are known, and that a `repos` scope names at least one owner/name, kept once in any
  case; `Delete`, `Duplicate` ("<name> copy", right after the original), and `Move` (up or
  down past the nearest script shown in the clone named, so the move shows on that page)
  change the order, and `RepoNames` lists the clones' GitHub repositories for the editor's
  Chosen list, reading origins four at a time and keeping each until the clone's
  `.git/config` changes, forgetting a clone no longer listed, and starting no more reads
  once the window cancels the call. `List` shows the ones whose scope includes the clone:
  every clone, one with a `go.mod` at its root, or its origin among the chosen
  repositories, in any case. They need no trust (D13), keep the hidden character guard,
  point a version bump to Release, and run exactly as written, no `${…}` filled in, as
  `[$SHELL, -c, command]` in the user's login shell whatever its family (fish and tcsh
  included, since the line is theirs and the app quotes none of it; `/bin/sh` only for a
  login shell that isn't an absolute path), in the clone, with the run env plus
  `Variables()`, which the page also carries (`Page.Variables`) for the editor:
  `<SLUG>_REPO`, `<SLUG>_REPO_DIR`, and `<SLUG>_DEFAULT_BRANCH`, named from `brand.Slug`,
  set alone in the terminal. The default branch, and so the page's *From branch* note,
  is the one the app stored, else the one the clone's `refs/remotes/origin/HEAD` names
  (gitread's `OriginHead`, which never asks origin), else `main` or `master` when the clone
  is on one, else ''. Their own place beats the setting, and a request's place beats both.
  A run's `Before` compares the SHA-256 of what the saved script runs (its name, command,
  place, and Ask before running) with the one worked out as the run was asked for, and
  refuses one saved again in between ("changed as it was starting"). Run again works the
  command out afresh from the saved script by its ID and refuses one deleted, no longer
  shown there, or set to ask before running (`ErrAsksFirst`), which only the page asks:
  Ask before running is the window's confirmation, which `Run` leaves to the user.
  The bound `BranchStatus` tells the header beside the branch where the clone's branch
  stands against the branch it follows on origin (`BranchState`: up to date, behind, ahead,
  diverged, detached, no upstream on origin, git in the middle of something, unknown,
  refused, not asked, or off while the Scripts integration is, which reads nothing, with a
  worded note), read through its `BranchGit` (gitread) from the clone's own refs and one
  `RemoteBranch` ls-remote of the followed branch alone (`ls-remote --heads origin
  refs/heads/<branch>`, never every branch and tag), never a fetch, and only for an origin
  on GitHub unless refresh is set (the user's click): another host's origin is
  `BranchNotAsked` until the user checks, then served from the kept answer; a clone the pull would
  refuse for its own config (gitread's `CloneConfig`, read locally, before origin is asked)
  is `BranchRefused`; every note built from a gitread failure goes through `gitNote`
  (`uierr.New` of gitread's words, read with `uierr.Message`); origin's answer is kept per
  clone and branch for `OriginKept` (five minutes) unless refreshed, the clone itself is
  read on every call, and an origin commit the clone lacks counts as behind. Only behind is
  offered **Pull**, and a refused pull's error is the whole sentence the window shows (it
  names the branch), a run whose record can't be read letting go of the button. The bound `Pull`
  (`pull.go`) returns a `scriptrun.KindPull` run's ID (thirty minutes' limit), in the app,
  under the clone's lock, of the branch the last status was worked out on, an upstream
  `gitread.ValidBranch` passes: its `Before` refuses a clone now on another branch, one
  whose branch follows none on origin, and one git is in the middle of something in, and
  works out with `gitread.RemoteSetup`, asking origin nothing (the pull's own fetch is the
  one round trip), what git runs with: a clone whose own config holds a setting gitread
  refuses is refused, naming the settings; otherwise gitread's own git, an ls-remote's
  environment (`GIT_TERMINAL_PROMPT=0`, the user's transports in `GIT_ALLOW_PROTOCOL`, the
  user's ssh command or a batch-mode one, and `GIT_NO_LAZY_FETCH=1` but in a partial clone,
  whose promisor remotes are weighed too), `GIT_DIR` and `GIT_WORK_TREE`, and every setting
  (`gitread.Hardening`, no alternate refs command, no automatic maintenance, the clone's own
  password program and credential helpers set aside, no cookie saved) passed through
  `GIT_CONFIG_COUNT`. Its intent (`PullIntent`) is recorded once `Before` ran, with the
  upstream and the branch's commit it read. It
  runs `git pull --ff-only --no-rebase --refmap= --no-tags --no-autostash
  --no-recurse-submodules --no-verify-signatures --no-stat --upload-pack=git-upload-pack
  origin +refs/heads/<b>:refs/remotes/origin/<b>` as argv, nothing else in it, with
  `LC_ALL=C` so git speaks the window's language, the environment handed to the runner
  through the spec's `BeforeEnv`: the fetch updates that one remote-tracking ref, never one the
  clone's own `remote.origin.fetch` maps. Stop is held back (`StopRefused`) for the whole
  run, since git writes its output only as it exits, and so is a quit, which leaves it going
  for the next start; only the kind's limit cuts it off, and a pull it cuts off is read
  again (`cutOff`: the branch it is on, its commit against the one before, whether
  `index.lock` is left, through gitread's `IndexLocked`, and what git is in the middle of)
  and said to be cut off partway, the clone perhaps partly updated, with `git status` in a
  terminal to see where it stands. Its `Finish` checks the clone is still on the
  branch (a switch while git ran fails the run, saying git may have pulled into the other),
  reads the branch back and words the summary (updated from one commit to another, how many
  commits, already up to date, or git's last error line, with any of the clone's own
  settings set aside, which may be why), and
  keeps the commit git fetched as origin's for the next status, leaving when origin was
  asked as it was, or forgets origin's answer when that can't be told. A pull whose end wasn't seen is settled from the branch alone:
  moved on from the commit `Before` read is pulled. Core registers the `Verifier`
  (`scripts.NewVerifier`) for pulls, so one left going at a quit comes back after a
  restart with Stop held again and that same `Finish`. Run again pulls the same branch, its
  `Before` checking everything afresh.
- `internal/measure/` and `magefiles/measure.go` take the numbers in
  [`docs/performance.md`](docs/performance.md).
- `frontend/` is the React + TypeScript + Vite + Tailwind app. It reaches Go only through the
  generated bindings in `frontend/bindings/`, funneled through `frontend/src/platform/`.
  The React Compiler memoizes the build, so components hold no hand-written `useMemo` or
  `useCallback`, and the gate's render check (`pnpm test:renders`) profiles the compiled
  inbox. Tests stand in for Go only with the shared fake in `frontend/src/test/platform.tsx`.
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
  library comes back on its own.
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
  snooze, sensitive, notifyprefs, branchrules, reporules, integrationprefs, and threadrules packages
  declare; inbox
  writes `inbox.*`; prompts writes `prompts.*` (the package doc has the full list). Settings and
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
  permission, a full disk, a failed migration) is a plain error, and nothing is moved.
- **Prompts and launch defaults outlive the database.** They are what the user made, which
  GitHub can't give back. `prompts.LibraryFile` rewrites `prompts.json` beside the database
  after every change to the library, and `launchdefaults.File` rewrites
  `launch-defaults.json` after every change to the launch defaults or the library, since it
  names prompts by nickname. At startup, when the library is new, `prompts.Restore` brings it
  back before `Seed` runs, from the database set aside or else from `prompts.json`, and core
  (`internal/core/kept.go`) restores the launch defaults from the same place, matched to the
  restored prompts. Core writes each copy at startup only when it is missing or the data was
  just restored, so a loss in the database never overwrites a copy. Agent profiles need no
  copy: discovery finds them on every start, and nothing in the app edits one. `store.OpenSnapshot` reads a kept database of any
  schema, read-only and tolerant of missing columns, for the restore and for **Restore from a
  backup**. Import and restore go through one planner (`internal/prompts/importer.go`), which
  never changes a prompt the user wrote or edited, and checks every imported setting the way
  the editor does. The file format is `prompts.Bundle`, specified in
  [`docs/prompt-files.md`](docs/prompt-files.md); keep the two in step, and raise
  `BundleVersion` only for a change an older build would misread.
- **Tests** open a fresh, migrated database with `testutil.DB(t)`, a given file with
  `testutil.DBAt(t, path)`, and a closed one with `testutil.ClosedDB(t)`. To make a write fail,
  add a trigger that calls `RAISE(ABORT, ...)`, or drop the table.
