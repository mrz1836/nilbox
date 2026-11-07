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
```

On macOS, a change to the desktop shell, the bound services, or the frontend must also pass
`magex app:check` (bindings drift check, desktop lint, and the app bundle build). CI has no
macOS runner, so this check is local.

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
- `internal/measure/` and `magefiles/measure.go` take the numbers in
  [`docs/performance.md`](docs/performance.md).
- `frontend/` is the React + TypeScript + Vite + Tailwind app. It reaches Go only through the
  generated bindings in `frontend/bindings/`, funneled through `frontend/src/platform/`.
- `magefiles/` holds the custom `magex` targets: `brand:*`, `frontend:*`, `app:*`, and
  `measure:*`. Its tests build with the `mage` tag and run the real target code against fake
  tools on an isolated `PATH` (`fakeTool` in `magefiles/helpers_test.go`). Keep new targets
  testable on Linux the same way: a macOS-only target checks the OS, then hands the real work
  to a helper the tests can call, as `App.Build` does with `buildBundle`.

Building and running the desktop app, and the rules it follows on your machine, are in
[`docs/desktop.md`](docs/desktop.md).

Conventions for commits, branches, pull requests, testing, and security live in
[`.github/tech-conventions/`](.github/tech-conventions/README.md).
