<div align="center">

# 📭&nbsp;&nbsp;Nilbox

**Your GitHub inbox, set to nil.**

A local-first desktop command center for developers who belong to hundreds of repositories across
several organizations. It shows what needs *you*, hides the noise, and hands any pull request or
issue to a coding agent with one click.

</div>

<br/>

> **Status: pre-alpha, private.** Nilbox is under active development and stays private until the MVP
> proves itself. It runs on macOS only for now, and will be released as open source under the MIT
> license.

<br/>

## Why

GitHub's notifications don't scale past a few dozen repositories. With 200–400 repos across
personal accounts and organizations, review requests, mentions, assignments, and bot noise all land
in one unranked pile, and acting on any of it means a context switch: find the clone, check out the
branch, open a terminal, start an agent, retype the prompt.

Nilbox fixes both halves:

- **Signal.** Three lists of what needs you, drawn from every repository your account can see,
  with archived repositories and finished items left out. Snooze, dismiss, or pin any item; your
  triage state lives locally and never touches GitHub.
- **Action.** Open any pull request or issue in a terminal that is already in the right clone,
  running Claude Code with your chosen profile (and so your chosen account), model, and flags, and
  a prompt from your own template library.

<br/>

## What it does

The window has three tabs, **Inbox**, **Worktrees**, and **Prompts**, plus a Settings page. The
[desktop guide](docs/desktop.md) explains how each feature below behaves.

### The inbox

- **Three lists**, each one GitHub search across every repository your account can see:
  **Review requested** (open pull requests that ask for your review), **Authored** (your open pull
  requests), and **Assigned** (open issues and pull requests assigned to you). Each shows up to
  100 items, with **N of M** when GitHub matched more.
- **Compact cards**: the title with **Draft** and **Conflicts** tags; the repository, author, and
  age; then checks passed out of those that ran, reviews, comments, a size grade with lines,
  files, and commits, and the first labels in their GitHub colors.
- **Kept current**: a refresh on start and every five minutes, a manual refresh, and a per-card
  refresh that reads one item for one API point. Archived repositories, and items merged or
  closed since GitHub indexed them, never show.
- **Tab counts and collapsible lists**: the Inbox tab counts what needs you, each item once.
  Collapse any list and the app remembers it across launches.

### Triage, kept local

- **Snooze** an item for 1 day to 30 days (3 days by default), **dismiss** it until you restore
  it, or **pin** it to the top of its lists. A toast confirms each snooze or dismissal, with
  **Undo**.
- **Wake rules** end a snooze early when new commits, reviews, or comments reach your thresholds,
  and the card says why, as in **Back early: approved, 2 new commits**.
- **Inbox nil**: empty every list and the inbox throws you a party, confetti included. With Reduce
  motion on, it holds still.
- Snoozes, dismissals, and pins live in the local database. GitHub never hears about them.

### Launching a coding agent

- **Open in Claude** starts Claude Code in a new iTerm2 tab (the default) or a new Terminal.app
  window, in the item's local clone, found by matching git remotes one level under your clone root
  (`~/projects` by default).
- **Worktrees by default**: the session starts in a worktree that Claude Code creates with its own
  `--worktree` flag, named `pr-<n>` or `issue-<n>`, so launching on the same item again resumes
  it.
- **Profiles**: `~/.claude` is the `default` profile, and each `~/.claude-<name>` or
  `~/.claude_<name>` config directory becomes profile `<name>`, run with `CLAUDE_CONFIG_DIR`, so a
  repository can launch on its own account.
- **The launch menu** picks the prompt, profile, model, effort, worktree, and plan mode for one
  launch, shows where each default comes from, previews the exact prompt and command, and saves
  the choices as the repository's or the org's defaults.
- **Launch defaults** per org and per repository set the profile, the default prompts, the model,
  effort, plan mode, worktree, and extra flags, and resolve in a
  [fixed order](docs/desktop.md#launch-defaults).
- **No clone yet?** The card offers to run `gh repo clone` into the clone root, then continues the
  launch. That is the only time the app creates a clone, and a launch never changes an existing
  one: no checkout, fetch, pull, or reset.
- **Workspace trust**: before each launch, the clone is marked trusted in the profile's Claude Code
  config, so a `--worktree` session doesn't stop at the trust dialog.
- The prompt is written to a file that the launch script reads, so it never appears on a command
  line.

### Prompt library

- Prompts are Go `text/template`s with `{{.Title}}`-style variables. Each has a title, a short
  nickname for the launch button, the kinds of item it applies to, and optional launch settings
  (model, effort, plan mode, worktree, and extra flags).
- Four starters, **Review**, **Work on it**, **Quick look**, and **Fix checks**, are yours to edit,
  duplicate, delete, or reset.
- The editor colors the template and completes variables as you type. It checks the template with
  the same Go template package a launch uses, reporting the line and column with "did you mean"
  suggestions, and previews the prompt filled in for a real inbox item beside a cheat sheet of
  variables.
- Set the default prompt for pull requests and for issues app-wide, per org, or per repository.

### Trust and sensitive files

- **Outside contributors**: items from forks, people outside the project, and bots not on your
  trusted list (`dependabot`, `github-actions`, and `renovate` by default) launch in plan mode,
  whatever the profile says, unless you tick **Use profile permissions for this item** for that
  launch. Their prompts end with a fixed line telling the agent to treat the item's text as
  information, not instructions.
- **Sensitive files**: pull requests from forks, outside contributors, and untrusted bots (and from
  collaborators, unless you narrow the check) have their changed files checked against five
  categories (agent configuration, workflows and CI, build and dev tooling, review and merge rules,
  and dependencies) and your own `.gitignore`-style patterns. The card names the categories a pull
  request touches and lists each file with a link to its diff.
- A launch on a pull request that changes agent configuration, or whose files couldn't all be
  checked, waits until you confirm its exact head commit: a project hook added on that branch runs
  as soon as a session starts, even in plan mode.

### Worktrees

- The **Worktrees** tab lists every linked worktree of the clones under the clone root, including
  ones from sessions started outside the app, with the branch, uncommitted files, commits not on
  GitHub, locks, and the age of the last commit.
- Agent worktrees are linked to their pull request or issue, and GitHub is asked whether it is
  done, so each one shows as **in use**, **ready to clean up**, or **needs a look**.
- **Clean up** removes a finished worktree, and its own `worktree-<name>` branch, only when nothing
  would be lost: no uncommitted changes, every commit on GitHub, and no session running in it. A
  sweep every five minutes does the same on its own once a pull request merges or an issue closes
  (on by default).
- **Remove**, **Remove all clean**, **Force remove** (for dirty, unlocked worktrees, after a
  confirmation), **Prune**, and **Unlock** for an agent lock whose session has ended. The main
  worktree is never removable, and a lock you set by hand is never released.
- **Recently cleaned up** lists each cleanup, by hand or automatic, with the branch deleted and the
  commit the worktree was at.
- The Worktrees tab counts the linked worktrees on disk, with an amber dot while some are done on
  GitHub but still there.

### Menu bar and notifications

- A **menu bar icon** shows how many items need you. Its menu lists up to five items per list,
  refreshes the inbox, and opens any item in the window. While the icon is shown, closing the
  window keeps the app running in the menu bar.
- **Native banners** announce new review requests, new assignments, and snoozes that end early,
  each kind with its own switch. More than three at once are summed up in one banner.
- A **notification history** behind the toolbar's bell keeps the newest 200.

### Everything else

- **API budget gauge**: an always-visible dial that follows whichever GitHub budget has the least
  left (GraphQL, REST core, or search), since every tool on your machine shares them. Click it for
  each budget's remaining points, its reset time, and the last refresh's cost.
- **Themes**: follow the system, or pin Light or Dark in Settings. The native window frame and
  menus follow the choice.
- **Settings** covers your account, appearance (the theme and the window's opening size),
  notifications, snoozing, launching, profiles, repositories, trust, and sensitive files. **About**
  shows the version, build, commit, and system, with a **Report a bug** button that opens a GitHub
  issue with those details filled in.

<br/>

## Not built yet

The MVP plan also includes a first-run setup (a GitHub CLI check, repository mute and pin, and
agent and terminal preferences) and muting or pinning whole repositories and orgs. Neither exists
yet. Security alert sweeps, integrations, more coding agents and terminals, and Linux and Windows
support come after the MVP proves the desktop app is worth it.

<br/>

## How it works

- **Desktop shell:** [Wails v3](https://wails.io) with a Go backend and the native macOS webview.
  Everything but the window is plain Go, built and tested on Linux without CGO.
- **Frontend:** React, TypeScript, Vite, and Tailwind, which reach Go only through generated
  bindings.
- **GitHub access:** borrows your [GitHub CLI](https://cli.github.com) token (`gh auth token`) for
  each refresh and keeps it in memory only. One GraphQL request carries all three searches; card
  details are read by node ID in concurrent batches; REST serves the rate limits and a pull
  request's changed files. A 20-item inbox refreshed every five minutes spends under 1% of the
  hourly GraphQL budget.
- **Network:** the app itself talks only to GitHub: `api.github.com`, and
  `avatars.githubusercontent.com` for your profile picture, which is then cached. Your GitHub data,
  preferences, and triage state go nowhere else.
- **Storage:** SQLite through a pure-Go driver (no CGO) in `~/Library/Application Support/Nilbox`,
  with forward-only, checksummed migrations and a backup before each one. It holds your
  preferences, triage state, prompts, launch defaults, notification and cleanup history, and a
  cache of what GitHub returned.
- **Agents:** launches the Claude Code CLI you already use, with your own profiles. Nilbox never
  stores model credentials.

<br/>

## Development

### Prerequisites

- [Go](https://go.dev/dl/) 1.26 or newer
- [MAGE-X](https://github.com/mrz1836/mage-x) build tool: `go install github.com/mrz1836/mage-x/cmd/magex@latest`
- [go-pre-commit](https://github.com/mrz1836/go-pre-commit) hooks: `go-pre-commit install`
- For the desktop app: macOS 13 or newer, the Xcode Command Line Tools, Node 26, and pnpm at the
  version pinned in `frontend/package.json` (`npm install -g pnpm@12.16.1`, once)
- To use the app: the [GitHub CLI](https://cli.github.com), signed in with the `repo` and
  `read:org` scopes, and to launch sessions, the `claude` CLI and iTerm2 or Terminal.app

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
| `cmd/nilbox/`      | Application entry point: a headless build by default, and the desktop app (window, menu bar icon, and notification banners) with the `desktop` tag |
| `internal/brand/`  | Product identity and design tokens (the only place the name lives); `internal/brandgen/` generates the theme, `Info.plist`, and icon artwork from it |
| `internal/`        | The services the app binds (inbox, launcher, prompts, launch defaults, settings, profiles, worktrees, and notifications) and their building blocks: the GitHub client, the trust and sensitive-file rules, snoozing, the menu bar, and the SQLite store |
| `frontend/`        | The React + TypeScript frontend and its generated Go bindings  |
| `build/`           | The app and menu bar icon artwork, and the macOS bundle's `Info.plist` and icon set, generated from `internal/brand` |
| `magefiles/`       | Custom `magex` targets for the brand, the frontend, the app, and measurements |
| `docs/`            | The desktop guide (setup, build targets, and how each feature behaves) and measured performance |
| `.github/`         | CI workflows, community docs, and engineering conventions      |

<br/>

## Contributing

Read [AGENTS.md](.github/AGENTS.md) before contributing. It links to the conventions for code,
tests, commits, branches, and pull requests that both humans and AI agents follow here.

<br/>

## License

[MIT](LICENSE) © [MrZ](https://github.com/mrz1836)
