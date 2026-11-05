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
```

<br>

## Layout

- `cmd/nilbox/` is the application entry point.
- `internal/brand/` holds product identity and design tokens. The palette tests enforce WCAG AA
  contrast for every text color in both themes.

Conventions for commits, branches, pull requests, testing, and security live in
[`.github/tech-conventions/`](.github/tech-conventions/README.md).
