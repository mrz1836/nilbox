# CLAUDE.md — read this first

This is **Nilbox**, a local-first desktop app (Go + Wails v3 + React) that turns a GitHub account
spanning hundreds of repositories into one focused inbox and launches coding agents on pull requests
and issues. The full engineering rulebook is [`.github/AGENTS.md`](.github/AGENTS.md). Read it
before changing anything.

<br>

## Non-negotiable rules

1. **This is a public, open-source project.** Never write the names of employers, clients,
   customers, companies, private projects, or private repositories into this repo. Never write
   personal machine paths (absolute home directories) or private tracker or ticket IDs. That
   applies to code, comments, tests, fixtures, docs, commit messages, branch names, and pull
   request titles and bodies. Use neutral placeholders such as `acme-org`, `example-repo`, and
   `~/projects`.
2. **Never bypass git hooks.** No `--no-verify`, no `-n`, no `core.hooksPath` tricks. If a hook
   fails, fix the cause.
3. **The product name lives only in `internal/brand`.** Reference `brand.Name`, `brand.Slug`, and
   friends. Never hard-code the name elsewhere.
4. **User data stays local.** Nilbox never sends a user's GitHub data, preferences, or triage state
   anywhere except GitHub's own API.

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

## Layout

- `cmd/nilbox/` is the application entry point: a headless build by default, and the Wails
  desktop app with the `desktop` build tag (`main_desktop.go`). Only the desktop bootstrap
  imports Wails.
- `internal/brand/` holds product identity, design tokens, and the logo mark. The palette tests
  enforce WCAG AA contrast for every text color in both themes. `internal/brandgen/` generates
  the frontend theme tokens, `build/darwin/Info.plist`, and the app icon artwork
  `build/appicon.svg` from it (`magex brand:generate`); `magex brand:icons` renders the artwork
  into the committed icon set (macOS, needs `rsvg-convert`).
- `internal/core/` assembles the app; the other `internal/*` packages are the services it binds
  (inbox, launcher, prompts, launch defaults, settings, profiles, worktrees) and their building
  blocks. All of them are plain Go, built and tested on Linux without CGO.
- `internal/measure/` and `magefiles/measure.go` take the numbers in
  [`docs/performance.md`](docs/performance.md).
- `frontend/` is the React + TypeScript + Vite + Tailwind app. It reaches Go only through the
  generated bindings in `frontend/bindings/`, funneled through `frontend/src/platform/`.
- `magefiles/` holds the custom `magex` targets: `brand:*`, `frontend:*`, `app:*`, and
  `measure:*`.

Building and running the desktop app, and the rules it follows on your machine, are in
[`docs/desktop.md`](docs/desktop.md).

Conventions for commits, branches, pull requests, testing, and security live in
[`.github/tech-conventions/`](.github/tech-conventions/README.md).
