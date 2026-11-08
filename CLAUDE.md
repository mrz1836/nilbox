# CLAUDE.md — read this first

This is **Nilbox**, a local-first desktop app (Go + Wails v3 + React) that turns a GitHub account
spanning hundreds of repositories into one focused inbox and launches coding agents on pull requests
and issues. The shared engineering rulebook is [`.github/AGENTS.md`](.github/AGENTS.md). Read it
before changing anything. The rules in this file are Nilbox's own, and they win wherever the two
differ.

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

<br>

## The gate

Every change must pass, in order:

```bash
magex format:fix
magex lint
magex test:race
go-pre-commit run --all-files
magex frontend:gate   # the frontend gate; CI runs the same script
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

Tests call `t.Parallel()` wherever it is safe, in top-level tests and subtests alike. The
exception is a test that changes process-wide state: one that uses `t.Setenv` or `t.Chdir`
(Go refuses both in a parallel test) or swaps a package-level variable. Once the
`paralleltest` linter is enabled in `.golangci.json`, each such test carries a
`//nolint:paralleltest` comment that says why. This deliberately overrides the synced
[`testing-standards.md`](.github/tech-conventions/testing-standards.md), which says to avoid
`t.Parallel()` outside concurrency tests; for this project, this file wins.

<br>

## Synced files

Shared tooling is synced from [go-broadcast](https://github.com/mrz1836/go-broadcast), and every
sync overwrites these files with the shared version. Anything Nilbox-specific written into them is
lost, so keep project rules in this file and project CI and tooling settings in
`.github/env/90-project.env`. The sync covers at least:

- `.github/AGENTS.md`, `.github/SECURITY.md`, `.mage.yaml`, and `codecov.yml`
- everything under `.github/workflows/`, `.github/actions/`, `.github/scripts/`,
  `.github/tech-conventions/`, `.github/ISSUE_TEMPLATE/`, `.github/docs/`, and `.vscode/`
- `.github/env/`, except `20-guardian.env`, `90-project.env`, and `99-local.env`

Files the shared source doesn't have, such as `.github/workflows/frontend.yml`, are left alone.

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
- `internal/core/` assembles the app; the other `internal/*` packages are the services it binds
  (inbox, launcher, prompts, launch defaults, settings, profiles, worktrees, notifications) and
  their building blocks, `internal/menubar` among them. All of them are plain Go, built and
  tested on Linux without CGO.
- `internal/event/` holds the app's events, one Go type each with its name; services emit them
  into the one `event.Sink` they are given. `internal/desktop/` is the shell's logic: it hears
  those events, keeps the menu bar current, runs the scheduler and the sweeper as tracked
  work, and shuts down in order (timers, then the work, then the database). It declares small
  interfaces that the Wails glue in `cmd/nilbox` implements, and imports no Wails. A window
  event is registered in `main_desktop.go` with `application.RegisterEvent`, which generates
  its TypeScript type and name in `frontend/bindings`; a repository test checks that every
  window event is registered.
- When a service needs another, prefer a narrow interface or value type declared where it is
  used (the launcher's `ItemStore` and `LaunchSettings`, the worktree service's
  `SweepSettings` and `GitHub`) over importing the other service; `internal/core` adapts the
  real services to them. The repoguard boundary test (`internal/repoguard/deps_test.go`)
  keeps the cuts it lists.
- `internal/launcher/` plans a launch and writes its files; `internal/launcher/terminal/`
  renders the launch script and opens it in the user's terminal app, one driver per terminal
  and OS.
- `internal/measure/` and `magefiles/measure.go` take the numbers in
  [`docs/performance.md`](docs/performance.md).
- `frontend/` is the React + TypeScript + Vite + Tailwind app. It reaches Go only through the
  generated bindings in `frontend/bindings/`, funneled through `frontend/src/platform/`.
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
  drops a table.
- **Ownership.** Every table has one owner package (`tableOwners` in
  `internal/repoguard/datalayer_test.go`). The owner keeps its SQL in
  `internal/<owner>/internal/queries/*.sql`, and sqlc generates package `queries` beside it, so
  Go's `internal/` rule lets only the owner import it. Other packages call the owner's API,
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
  snooze, sensitive, and notifyprefs packages declare; inbox writes `inbox.*`; prompts writes
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
- **Tests** open a fresh, migrated database with `testutil.DB(t)`, a given file with
  `testutil.DBAt(t, path)`, and a closed one with `testutil.ClosedDB(t)`. To make a write fail,
  add a trigger that calls `RAISE(ABORT, ...)`, or drop the table.
