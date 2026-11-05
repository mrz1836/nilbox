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
- Local triage: mark items done, pin and mute repositories inline
- **Open in agent**: launch a terminal (iTerm2, Terminal.app, and more) in the right clone or a fresh
  worktree, with a saved agent profile and a rendered prompt
- An always-visible GitHub API rate-limit gauge, because every tool on your machine shares one budget
- Light and dark themes

Everything else, including notifications, a menu bar extra, security alert sweeps, integrations,
and Windows support, comes after the MVP proves the desktop app is worth it.

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

### Common commands

```bash
magex build          # build the binary into bin/
magex test           # run the test suite
magex test:race      # run tests with the race detector
magex lint           # run golangci-lint
magex format:fix     # format the code
magex help           # list every available command
```

Before opening a pull request, run the full gate:

```bash
magex format:fix && magex lint && magex test:race && go-pre-commit run --all-files
```

### Project layout

| Path               | Purpose                                                        |
|--------------------|----------------------------------------------------------------|
| `cmd/nilbox/`      | Application entry point                                        |
| `internal/brand/`  | Product identity and design tokens (the only place the name lives) |
| `.github/`         | CI workflows, community docs, and engineering conventions      |

<br/>

## Contributing

Read [AGENTS.md](.github/AGENTS.md) before contributing. It links to the conventions for code,
tests, commits, branches, and pull requests that both humans and AI agents follow here.

<br/>

## License

[MIT](LICENSE) © [MrZ](https://github.com/mrz1836)
