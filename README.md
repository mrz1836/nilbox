<div align="center">

# 📭&nbsp;&nbsp;Nilbox

**Your GitHub inbox, set to nil.**

A local-first desktop command center for developers who belong to hundreds of repositories across
several organizations. It shows what needs *you*, hides the noise, and hands any pull request or
issue to a coding agent with one click.

</div>

<br/>

> **Status: pre-alpha, private.** Nilbox is under active development and stays private until the MVP
> proves itself. It will be released as open source under the MIT license.

<br/>

## Why

GitHub's notifications don't scale past a few dozen repositories. With 200–400 repos across
personal accounts and organizations, review requests, mentions, assignments, and bot noise all land
in one unranked pile, and acting on any of it means a context switch: find the clone, check out the
branch, open a terminal, start an agent, retype the prompt.

Nilbox fixes both halves:

- **Signal.** One inbox for what needs you, with per-repo and per-organization controls to pin
  what matters and mute what doesn't. Your triage state lives locally and never touches GitHub.
- **Action.** Open any pull request or issue in a terminal that is already in the right directory,
  running your coding agent with your chosen account, model, and flags, and a prompt from your own
  template library.

<br/>

## MVP scope

The first release is a deliberately thin steel thread on macOS:

- A three-step setup: GitHub CLI check, repository mute/pin, agent and terminal preferences
- Three views: **Needs Review**, **My Pull Requests**, and **Assigned**
- Local triage: snooze, dismiss, or pin items to the top; pin and mute repositories inline
- **Inbox nil**: clear every list and the inbox throws you a party
- **Open in agent**: launch a terminal (iTerm2, Terminal.app, and more) in the right clone or a fresh
  worktree, with a saved agent profile and a prompt from your library
- **Worktree cleanup**: a session's worktree is linked to its pull request or issue, and cleaned up
  with its branch once that merges or closes, only when nothing in it would be lost
- **Sensitive files**: pull requests from forks and outside contributors (and collaborators, unless
  you narrow it) point out the files that run on CI, run on your machine, or steer a coding agent,
  and a session on one that changes agent configuration asks you first
- **Prompt library**: write prompts as Go templates with `{{.Title}}`-style variables, checked as you
  type and previewed against real items; set defaults per org and per repository
- An always-visible GitHub API rate-limit gauge, because every tool on your machine shares one budget
- Light and dark themes: follow the system, or pin one in Settings
- **Menu bar and notifications**: a menu bar icon with how many items need you and a mini-inbox,
  native banners for new review requests, assignments, and snoozes that end early, and a
  notification history behind the toolbar's bell

Everything else, including security alert sweeps, integrations, and Windows support, comes after
the MVP proves the desktop app is worth it.

<br/>

## How it works

- **Desktop shell:** [Wails v3](https://wails.io) with a Go backend and a native webview
- **Frontend:** React, TypeScript, Vite, and Tailwind
- **GitHub access:** borrows your existing [GitHub CLI](https://cli.github.com) login, then talks to
  the GraphQL and REST APIs directly with batching and conditional requests to stay well inside rate
  limits
- **Storage:** SQLite (pure Go, no CGO) for your preferences, triage state, and a disposable cache
- **Agents:** launches the coding agent CLIs you already use; Nilbox never stores model credentials

<br/>

## Development

### Prerequisites

- [Go](https://go.dev/dl/) 1.26 or newer
- [MAGE-X](https://github.com/mrz1836/mage-x) build tool: `go install github.com/mrz1836/mage-x/cmd/magex@latest`
- [go-pre-commit](https://github.com/mrz1836/go-pre-commit) hooks: `go-pre-commit install`
- For the desktop app: macOS 13 or newer, the Xcode Command Line Tools, Node 26, and pnpm at the
  version pinned in `frontend/package.json` (`npm install -g pnpm@12.16.1`, once)

### Quick start

```bash
cd ~/projects/nilbox
magex app:run          # build the macOS app into bin/Nilbox.app and open it
```

The app borrows your [GitHub CLI](https://cli.github.com) login, so run `gh auth login` first.
[docs/desktop.md](docs/desktop.md) covers the full setup, every build target, and how the app
treats your clones and worktrees; [docs/performance.md](docs/performance.md) has the measured
startup, refresh, and size numbers.

### Common commands

```bash
magex build          # build the headless binary into bin/
magex test           # run the test suite
magex test:race      # run tests with the race detector
magex lint           # run golangci-lint
magex format:fix     # format the code
magex frontend:gate  # format check, lint, typecheck, and test the frontend
magex app:dev        # run the desktop app with a hot-reloading frontend
magex app:build      # build the macOS app bundle
magex app:open       # open the built app (builds it first if it is missing)
magex app:run        # build the app, quit the running copy, and open the new build
magex help           # list every available command
```

Before opening a pull request, run the full gate:

```bash
magex format:fix && magex lint && magex test:race && go-pre-commit run --all-files
magex frontend:gate
magex app:check      # on macOS: bindings drift check, desktop lint, and bundle build
```

### Project layout

| Path               | Purpose                                                        |
|--------------------|----------------------------------------------------------------|
| `cmd/nilbox/`      | Application entry point (the desktop app builds with the `desktop` tag) |
| `internal/brand/`  | Product identity and design tokens (the only place the name lives) |
| `internal/`        | The core services: inbox, launcher, prompts, launch defaults, settings, profiles, worktrees, and the data layer |
| `frontend/`        | The React + TypeScript frontend and its generated Go bindings  |
| `magefiles/`       | Custom `magex` targets for the frontend, the app, and measurements |
| `docs/`            | Desktop build guide and measured performance                   |
| `.github/`         | CI workflows, community docs, and engineering conventions      |

<br/>

## Contributing

Read [AGENTS.md](.github/AGENTS.md) before contributing. It links to the conventions for code,
tests, commits, branches, and pull requests that both humans and AI agents follow here.

<br/>

## License

[MIT](LICENSE) © [MrZ](https://github.com/mrz1836)
