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
   nothing of the user's, and spends no API budget. The one exception is Describe it
   (`internal/describeit`), when the user uses it: Claude Code reads their words, their
   Autopilot setup, and their inbox's names, never an item's text or code, and sends them only
   to Anthropic.
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
magex frontend:gate   # format, lint, typecheck, tests, compiled render check, build, first paint; CI runs it too
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

**Look before guessing.** A build from source (Nilbox Dev, `magex app:dev`) carries an
inspector: when the owner reports something wrong in the window, `magex inspect:trail` shows
what they clicked, and `inspect:shot`, `inspect:cards`, `inspect:dom`, `inspect:js`,
`inspect:console`, `inspect:logs`, and `inspect:sql` show the window, the page, the log, and the
live database as they are now ([the inspector](docs/desktop.md#the-inspector)). The owner can
also point at an element (⌥⌘P) and leave a note: `magex inspect:notes` has the open ones (the
words, the element and where, its styles, components, and source file, and a boxed picture).
While working on the window with them, keep `inspect:notes follow=true` running in the
background so each new note reaches you, and mark one fixed with `inspect:notes done=<n>`. Run
them outside the sandbox, which refuses the socket.

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

The `internal/appinfo` tests read the synced `.github/ISSUE_TEMPLATE/bug_report.yml` (its
`environment` field, which **Report a bug** fills in) and the `magefiles` tests the synced
`10-mage-x.env` (its golangci-lint pin), so a sync that changes either fails them; a synced
`config.yml` with `blank_issues_enabled: false` would break **Send feedback**. See
[`internal/appinfo`](docs/architecture.md#internalappinfo).

<br>

## Rules every change keeps

These hold across packages, and most have a guard test in `internal/repoguard` that fails the
build when broken.

- **GitHub requests.** Every GitHub request goes through `(*github.Client).do`, apart from three
  tokenless reads that aren't the API (`FetchAvatar`/`FetchLoginAvatar`, `DownloadAsset`,
  `FetchStatus`). Never build an HTTP request anywhere else, nor run `gh api`
  (`githubrequests_test.go`). Every write goes through the `WriteGate` (`edit` for REST,
  `mutate` for a GraphQL mutation), but marking a notification thread done or read goes
  through `chore` (`githubwrites_test.go`). Every request carries its purpose
  (`github.WithPurpose`).
- **Processes.** Only `internal/execpath` starts a process (`TestOnlyExecpathStartsProcesses`,
  with named exceptions), and nothing runs `go install` or `sudo` (`TestNoGoInstall`,
  `TestNoSudo`).
- **Git.** Nothing the app runs changes a clone's own checkout (`TestNoGitCommandChangesAClone`):
  only `internal/worktrees/create.go` fetches or runs `git worktree add`, and the only
  exceptions, each on the user's click, are a release's magex and Repo scripts' **Pull**
  (`internal/scripts/pull.go`). A branch is deleted only with the conditional `git update-ref
  -d <ref> <oid>`, never `git branch -d/-D`. Release, hooks, Repo scripts, and Deps read git
  only through `internal/gitread`'s allowlist, and the commands that change a clone (a worktree's
  creation, Pull) run under its guarded setups.
- **Clone locks.** Every job that changes a clone holds that clone's lock from
  `internal/clonelock`, one `Set` that core builds and hands to each, keyed by
  `paths.Resolved`. Lock `act` before a clone's lock, never after. A run is never waited for:
  a launch or a click fails at once with a sentence naming the run.
- **Untrusted files.** A file a repository or an agent controls is read only through
  `paths.OpenRegular` (`ReadHead`, `ReadTail`), so a named pipe swapped in is refused rather
  than hanging the read.
- **Agents.** `internal/agent` is the one registry of the coding agents, one spec each; an
  agent's flags and program name are spelled only there and in its parsers and observers
  (`TestOnlyTheAgentsHomesSpellTheirFlags`). Every agent start's environment comes from
  `launcher.AgentEnv`.
- **Shell quoting.** `textutil.ShellQuote` (always quotes) for every word of a script the app
  writes, and `textutil.ShellWord` for commands the window shows or copies; no other quoting.
- **Dependencies.** When a service needs another, prefer a narrow interface or value type
  declared where it is used (the launcher's `ItemStore`, `LaunchSettings`, `WorktreeMaker`, and
  per-agent `Truster` map, the worktree service's `SweepSettings` and `GitHub`) over importing
  the other service;
  `internal/core/adapters_<consumer>.go` (one file per consumer, the shared `settingsPart` in
  `adapters.go`) adapts the real services to them. The repoguard boundary test
  (`internal/repoguard/deps_test.go`) keeps the cuts it lists. Every `internal/*` package is
  plain Go, built and tested on Linux without CGO; only the desktop bootstrap imports Wails.
- **Events.** Services emit `internal/event` types into the one `event.Sink` they are given. A
  window event is registered in `main_desktop.go` with `application.RegisterEvent`, which
  generates its TypeScript type and name in `frontend/bindings`; a repository test checks that
  every window event is registered.
- **The bound API's shapes.** A closed set of string values is a named Go string type with
  constants (`item.Kind`, `settings.Theme`, `sensitive.Scope`, ...), which `magex
  app:bindings` writes as a TypeScript union of its values plus a const object of the same
  name, so a plain `'pr'` still type-checks. Declare all of such a type's constants in one
  file: the generator orders constants from several files differently from run to run, so
  `magex app:bindings` refuses them (`checkEnumFiles` in `magefiles/bindings.go`). A bound
  result never holds a nil slice or map, so the window gets `[]`, never `null`
  (`internal/core/bound_test.go` checks every bound method). Each settings setter writes and
  reads the settings back in one transaction and returns them, so a setter that fails has
  stored nothing.
- **The window.** `frontend/` reaches Go only through the generated bindings in
  `frontend/bindings/`, funneled through `frontend/src/platform/`, and its tests stand in for Go
  only with the shared fake in `frontend/src/test/platform.tsx`. The React Compiler memoizes
  the build, so components hold no hand-written `useMemo` or `useCallback`. Colors, text sizes,
  and shadows come from the theme's tokens (`--color-*`, `index.css`'s `@theme`), which a
  repoguard test enforces; checkboxes and radio buttons take the `checkbox` and `radio` classes;
  text is sized in rem, never px (a repoguard test fails on px text).
- **magefiles.** `magefiles/` holds the custom `magex` targets: `brand:*`, `frontend:*`,
  `app:*`, `db:*`, `measure:*`, `inspect:*`, and `autopilot:*`. Its tests build with the `mage` tag and run in parallel.
  A target checks the OS and reads its arguments, then hands the work to methods of a `runner`
  (`magefiles/runner.go`): the checkout, output, environment, and tool lookup they use. A
  target passes `hostRunner()`, this process's own; a test passes a `rig`'s runner
  (`magefiles/helpers_test.go`), a temporary checkout and `testutil.FakeTool` fakes answered
  per call. Keep new targets testable on Linux the same way, as `App.Build` does with `buildApp`.

<br>

## Layout

One line per package. Its full design is its section in
[`docs/architecture.md`](docs/architecture.md): read that before changing the package, and put
new design detail there, never here (`TestProjectGuideStaysSmall` caps this file at 30,000
bytes).

**The app and its shell**

- [`cmd/nilbox`](docs/architecture.md#cmdnilbox): the entry point; headless by default, the
  desktop app with the `desktop` tag; `--data-dir`, the update helper, and the fence hook and
  tool modes.
- [`internal/brand`](docs/architecture.md#internalbrand): product identity, design tokens, and the
  logo; `brandgen` generates the theme, `Info.plist`, and the icon artwork in `build/`.
- [`internal/core`](docs/architecture.md#internalcore): assembles the app and builds every
  service the window binds.
- [`internal/event`, `internal/desktop`](docs/architecture.md#internalevent-and-internaldesktop):
  the app's events; the shell's logic (menu bar, window close, quit guard, workers, shutdown).
- [`internal/appinfo`](docs/architecture.md#internalappinfo): app info, Report a bug, and Send
  feedback.
- [`internal/devtools`](docs/architecture.md#internaldevtools): the inspector a build from
  source carries (the `devtools` tag, never a release's), which `magex inspect:*` talks to.
- [`internal/store`](docs/architecture.md#internalstore): opens the database, self-heals,
  backs up and restores; what outlives the database.

**Shared building blocks** ([notes](docs/architecture.md#shared-building-blocks))

- `internal/repo`, `internal/item`: names for repositories, pull requests, issues, and worktrees.
- `internal/agent`: the coding agents' registry (Claude Code, Codex): flags, models, fence,
  headless and conversation halves, version, and sign-in.
- `internal/procs`, `internal/execpath`, `internal/shellenv`: running processes; finding and
  starting programs; the login shell's environment.
- `internal/clock`, `periodic`, `flight`, `paths`, `progress`, `textutil`, `toolsigns`: the
  clock, the interval runner, one call per key, paths, progress, text and shell quoting, and
  tools' marker files.
- `internal/testutil`, `internal/scriptrun/scriptruntest`,
  `internal/localrepos/localrepostest`, `internal/assignees/assigneestest`: the shared test
  fakes and fixtures; only tests import them.
- `internal/github/githubtest`: the shared fakes of `internal/github` (a token source, a fake
  GitHub's request counts, a refused token); only tests import it.

**GitHub, the inbox, and notifications**

- [`internal/budget`, `internal/github`](docs/architecture.md#internalbudget-and-internalgithub):
  the one GitHub client, its request and write rules, and the API budget.
- [`internal/inbox`](docs/architecture.md#internalinbox): the searches, Review requested and
  Yours, triage, the checks and merge followers, and the routines' watched searches.
- [`internal/reporules`](docs/architecture.md#internalreporules): pinned and muted repositories
  and orgs.
- [`internal/heldruns`](docs/architecture.md#internalheldruns): which held workflow runs count
  toward where a pull request stands, and the Ignored workflows setting.
- [`internal/threadrules`](docs/architecture.md#internalthreadrules): the rules for clearing
  GitHub notifications.
- [`internal/ghthreads`](docs/architecture.md#internalghthreads): mirrors the user's GitHub
  notifications, marks threads done or read, and marks them done in bulk.
- [`internal/statusprefs`](docs/architecture.md#internalstatusprefs): the service status
  catalog and what the user watches.
- [`internal/svcstatus`](docs/architecture.md#internalsvcstatus): follows GitHub's, Claude's,
  and OpenAI's status pages.
- [`internal/milestones`](docs/architecture.md#internalmilestones): the log of the inbox at
  nil: the streak, the achievements, and confetti at most once a day.

**The sheets**

- [`internal/review`](docs/architecture.md#internalreview): the review sheet's service: offers,
  merges, line comments, and the diff cache.
- [`internal/issues`](docs/architecture.md#internalissues): the issue sheet's service: comment,
  close, labels and assignees, and the issue cache.
- [`internal/timeline`](docs/architecture.md#internaltimeline): the conversation both sheets
  show.
- [`internal/avatars`](docs/architecture.md#internalavatars): the profile pictures the sheets
  and the inbox's cards show, people's and bots'.
- [`internal/people`](docs/architecture.md#internalpeople): the local directory of who works on
  each repository and who can be assigned there, which the pickers suggest from.
- [`internal/assignees`](docs/architecture.md#internalassignees): whom both sheets' assignee
  pickers offer, and when a read of GitHub is due.
- [`internal/bump`](docs/architecture.md#internalbump): reads a dependency update from a bot's
  pull request.

**Agents and launches**

- [`internal/launcher`](docs/architecture.md#internallauncher): resolves, plans, and opens a
  launch in the user's terminal; holds, trust, and `workspacetrust`.
- [`internal/sessions`](docs/architecture.md#internalsessions): follows the agent sessions
  launches start.

**Clones and worktrees**

- [`internal/localrepos`](docs/architecture.md#internallocalrepos): finds the user's clones in
  the clone root and the other repository locations.
- [`internal/pushwatch`](docs/architecture.md#internalpushwatch): notices a push made on this
  Mac to an inbox pull request, from its branch's ref in the clones, and has it read again.
- [`internal/worktrees`](docs/architecture.md#internalworktrees): makes the app's worktrees (one
  per item per clone), records them in `worklog`, and removes them and their branches safely.
- [`internal/gitread`](docs/architecture.md#internalgitread): the allowlisted read-only git, and
  the guarded setups for the git commands that change a clone.

**Integrations and runs**

- [`internal/integrations`](docs/architecture.md#internalintegrations): finds, installs, and
  updates magex and go-pre-commit (`toolinstall` downloads and checks a release).
- [`internal/scriptrun`](docs/architecture.md#internalscriptrun): runs and follows the
  integrations' commands and a routine's background agent, never Describe it's turns.
- [`internal/release`](docs/architecture.md#internalrelease): the Release page.
- [`internal/hooks`](docs/architecture.md#internalhooks): the Hooks page.
- [`internal/scripts`](docs/architecture.md#internalscripts): Repo scripts, and Pull.
- [`internal/manifests`](docs/architecture.md#internalmanifests): the Deps page: each clone's
  go.mod and package.json files and their ages.

**Updates**

- [`internal/update`](docs/architecture.md#internalupdate): how the app updates itself.
- [`internal/updateprefs`](docs/architecture.md#internalupdateprefs): the `update.mode` setting.
- [`internal/semver`](docs/architecture.md#internalsemver): semantic versions.

**Autopilot**

- [`internal/autopilot`](docs/architecture.md#internalautopilot): runs the user's routines; its
  settings are `internal/autopilotprefs`.
- [`internal/autopilot/draft`](docs/architecture.md#internalautopilotdraft): the one check of a
  routine's draft, for the builder, Save, and kit.
- [`internal/agentrun`](docs/architecture.md#agents-in-the-background): runs a routine's agent
  step in the background.
- [`internal/agentrun/stream`](docs/architecture.md#internalagentrunstream): reads an agent's
  stream, for background runs and Describe it's turns.
- [`internal/fence`](docs/architecture.md#internalfence): keeps an unattended agent run inside
  its tools preset.

**Describe it**

- [`internal/autopilot/kit`](docs/architecture.md#internalautopilotkit): what Claude Code reads
  and proposes: the snapshot, a proposal, its checks and risks, and the words.
- [`internal/describeit`](docs/architecture.md#internaldescribeit): the conversation, its cards,
  and the plugin for the user's own Claude Code.
- [`internal/mcpserver`](docs/architecture.md#internalmcpserver): the tool mode, the app's
  binary serving Describe it's tools; it writes only proposals.

**Backup and setup**

- [`internal/backup`](docs/architecture.md#internalbackup): exports and imports the user's setup.
- [`internal/setup`](docs/architecture.md#internalsetup): the first-run setup, and a reset.

**The window and the tools**

- [`frontend/`](docs/architecture.md#frontend): the React + TypeScript + Vite + Tailwind window.
- `magefiles/`: the custom `magex` targets (above); `internal/measure` and
  `magefiles/measure.go` take the numbers in [`docs/performance.md`](docs/performance.md).

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
  checksummed SQL files, starting from the base the last squash wrote (`0100_settings.sql` to
  `0103_autopilot.sql`). Never edit an applied migration; change the schema with a new
  `NNNN_<name>.sql`. Nothing else creates, alters, or drops a table. Never edit, renumber, or
  delete a migration a build has shipped: every database that ran it then reads as a history
  this build disagrees with and is set aside, and only the prompt library, the launch defaults,
  the routines, and the user's own scripts come back on their own. Combine shipped migrations
  only by a squash, which carries every database over untouched: freeze each history
  (`-freeze`), write the base at the next hundred, and prove it builds the frozen schema; the
  steps are in [`internal/store`](docs/architecture.md#internalstore).
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
  statusprefs, updateprefs, autopilotprefs, and heldruns packages declare (`update.mode`,
  the `autopilot.*` settings, and `checks.ignoredWorkflows` among them); inbox
  writes `inbox.*`; prompts writes `prompts.*`; setup writes `setup.*`; milestones writes
  `milestones.*`; update writes its state,
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
- **Self-heal.** `store.Open` backs the database up before applying pending migrations, sets a
  damaged database, a schema the migrations did not create, or a history this build disagrees
  with aside as `<path>.broken-<UTC stamp>` for a fresh one, and refuses a database from a newer
  build (`store.ErrNewerSchema`), leaving it untouched. A reset the user asked for, `store.Backup`,
  and `store.RestoreBackup` are in [`internal/store`](docs/architecture.md#internalstore).
- **Prompts, launch defaults, the Autopilot routines, and the user's scripts outlive the
  database.** They keep copies beside it (`prompts.json`, `launch-defaults.json`,
  `autopilot.json`) or come back from a database set aside, and their file formats
  ([`docs/prompt-files.md`](docs/prompt-files.md), [`docs/backup-files.md`](docs/backup-files.md))
  stay in step; see [`internal/store`](docs/architecture.md#internalstore).
- **Tests** open a fresh, migrated database with `testutil.DB(t)`, a given file with
  `testutil.DBAt(t, path)`, and a closed one with `testutil.ClosedDB(t)`. To make a write fail,
  add a trigger that calls `RAISE(ABORT, ...)`, or drop the table.
