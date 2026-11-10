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
   or caches anywhere except GitHub's own API.
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
`magex app:check` (bindings drift check, desktop lint, and the app bundle build). CI has no
macOS runner, so this check is local.

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
`testutil.RunGit`. A test that runs an external tool runs a fake one: `testutil.FakeTool`
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
would not help the user.

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
`environment` field. The form is synced, so if a sync renames that field, the button stops filling
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
  profiles, prompts, launch defaults, inbox, review, launcher, worktrees, notifications, app
  info, and sessions.
  Every `internal/*` package is plain Go, built and tested on Linux without CGO.
- Services share their building blocks rather than keep copies: `internal/repo` and
  `internal/item` name repositories, pull requests and issues, and their worktrees;
  `internal/agent` is the backend registry of the coding agents, one spec each (Claude Code,
  Codex) with its command, config variable and markers, effort levels, models, permission
  modes, reserved flags, and how a launch's options become its flags, so adding an agent adds
  a spec and the launcher, profiles, and editors read the rest from it; `internal/procs` lists
  the running processes' working directories (`lsof` on macOS, `/proc` on Linux), which tell
  a worktree some session is in, and reads a process's start time (`StartedAt`), which tells a
  process from a stranger that reused its PID; `internal/execpath` finds and runs external
  programs (`Finder`, `Runner`; `Search.Path` searches the login shell's PATH), and starts a
  long-lived one in its own process group (`Start`, `Proc`), writing straight into a file the
  app tails and stopping the whole group with a signal ladder; nothing else starts a process
  (`TestOnlyExecpathStartsProcesses`, with named exceptions), and nothing runs `go install` or
  `sudo` (`TestNoGoInstall`, `TestNoSudo`); `internal/shellenv` reads the user's login shell's
  environment, after the first paint, and never logs a variable's name or value; and
  `internal/clock`, `internal/periodic`, `internal/flight`, `internal/paths`, and
  `internal/textutil` hold the clock, the interval runner, one call per key at a time, `~`
  paths, and text helpers. `internal/testutil` holds the fakes and fixtures the test suites
  share (a fresh database, an isolated git, a runner, finder, clock, event sink, log, and fake
  tools); only tests may import it.
- `internal/event/` holds the app's events, one Go type each with its name; services emit them
  into the one `event.Sink` they are given. `internal/desktop/` is the shell's logic: it hears
  those events, keeps the menu bar current with what `internal/menubar` works out, runs the
  scheduler, the sweeper, and the sessions watcher as tracked work, starts the deferred work
  (`core.Deferred`: the login-shell read first) once after the first paint, never on the cold
  start, and shuts down in order (timers, then the work, then the database). It declares small interfaces that the Wails glue
  in `cmd/nilbox` implements, and imports no Wails. A window event is registered in
  `main_desktop.go` with `application.RegisterEvent`, which generates its TypeScript type and
  name in `frontend/bindings`; a repository test checks that every window event is registered.
- When a service needs another, prefer a narrow interface or value type declared where it is
  used (the launcher's `ItemStore`, `LaunchSettings`, `WorktreeMaker`, and per-agent `Truster`
  map, the worktree service's `SweepSettings` and `GitHub`) over importing the other service;
  `internal/core/adapters.go` adapts the real services to them. The repoguard boundary test
  (`internal/repoguard/deps_test.go`) keeps the cuts it lists.
- `internal/budget/` keeps the GitHub API budget current. The one shared `github.Client` hands
  every response's rate-limit reading, and what the request spent, to its `Tracker`
  (`github.RateObserver`), whose state the `Saver` keeps across runs. Every GitHub request goes
  through `(*github.Client).do`: never build an HTTP request anywhere else, nor run `gh api`;
  `internal/repoguard/githubrequests_test.go` fails the build if anything does. Every write goes
  through the `WriteGate`, a REST one through `edit` and a GraphQL mutation through `mutate`
  (`postGraphQL` refuses a mutation); `internal/repoguard/githubwrites_test.go` keeps it so.
- `internal/launcher/` resolves a launch (the agent first, then that agent's profile, model,
  and flags), plans it, and writes its files; `internal/launcher/terminal/` renders the launch
  script, which only changes into the folder and runs the agent, and opens it in the user's
  terminal app, one driver per terminal and OS. `internal/workspacetrust` marks the launch
  folder trusted in each agent's own config (Claude's `.claude.json`, Codex's `config.toml`,
  appended to and never rewritten).
- The app owns its worktrees: `internal/worktrees` makes them (one per item per clone, shared by
  every agent, under `<clone root>/.<slug>/worktrees/<clone>/`), records them in
  `internal/worklog`, checks every record against git on each listing and sweep, and removes
  them safely. Only `internal/worktrees/create.go` fetches or runs `git worktree add`, and
  nothing changes a clone's own checkout; `TestNoGitCommandChangesAClone` in
  `internal/repoguard` fails the build otherwise. Every git command there runs with hooks, the
  file system monitor, and submodule recursion off (`hardening` in `internal/worktrees/git.go`),
  since a worktree holds a pull request's own files, and a pull request's worktree is made only
  at the head whose files the launch checked. The launcher reaches the `Maker` through its
  `WorktreeMaker` interface; no agent's own `--worktree` flag is used. A branch is deleted only
  with the conditional `git update-ref -d <ref> <oid>` (the repoguard tests ban `git branch
  -d/-D`): by `create.go`, of a branch it just fetched at a head the launch didn't check, and
  otherwise only through `deleteBranchAt` in `internal/worktrees/branches.go`, when the user
  ticked it (or, for the automatic cleanup, the user's rules pick it), every commit on it is on
  GitHub, and neither the user's protected patterns (`internal/branchrules`) nor a remote's
  `HEAD` keeps it. Every job that changes a clone holds that clone's lock from
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
  files, on demand, for the review sheet.
- `internal/review/` is the review sheet's service. `Decide` is pure: from a pull request's
  facts it works out every action's offer and the one primary, and `Act` runs it again before
  any write. Every write goes through the inbox's `Actor` (the item lock, the account check,
  and the token retry, never bound, since its methods take funcs), is pinned to the head the
  user saw (an approval reads the head first, since GitHub takes a review of an older commit),
  and a merge first reads `mergeStateStatus` fresh and merges only when GitHub says it can, so
  an admin's token never merges past branch protection on its own: only `merge_bypass`
  ("merge past the rules") does, offered when GitHub sets `viewerCanMergeAsAdmin`, never the
  sheet's button, and with each rule it skips to accept. Each step is reported as an
  `event.ReviewProgress`, which the sheet shows while the user waits. Line comments are checked
  against the cached diff (`hunks.go`) before anything is sent. It owns `review_diffs` and
  `review_diff_files`, a bounded cache of the diffs it read (per pull request, in all, and by
  age), pruned in the same transaction that writes. Nothing here lets an agent approve or merge.
- `internal/bump/` reads a dependency update from a bot's pull request title (and Dependabot's
  commit trailer) into an `Update`, for the review sheet and the `.Dependency` prompt variables.
  It is pure and imports no service.
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
  snooze, sensitive, notifyprefs, and branchrules packages declare; inbox writes `inbox.*`; prompts writes
  `prompts.*` (the package doc has the full list).
- **Values.** Owners stamp `created_at`, `updated_at`, and every other time from their injected
  `clock.Clock`; nothing fills them in. The driver stores every `time.Time` as UTC text
  (`2006-01-02 15:04:05.999999999+00:00`), so stamps compare and sort as text in time order.
  JSON columns are TEXT, written with `store.EncodeJSON` and read with `store.DecodeJSON[T]`.
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
