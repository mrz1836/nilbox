# Docs

Every doc of Nilbox, and what's in it. Nilbox is a macOS desktop app: a Go core, a
[Wails v3](https://wails.io) shell, and a React + TypeScript + Vite + Tailwind frontend in
`frontend/`. The project guide, [`CLAUDE.md`](../CLAUDE.md), holds only the rules every change
keeps; everything else is here, one topic to a file.

| If you want to…                                                  | Start at                        |
|------------------------------------------------------------------|---------------------------------|
| Use the app: what each feature does, and what it does on your Mac | [User guide](#user-guide)       |
| Build, debug, or release the app                                 | [Development](#development)     |
| Change a package: read its design first, and keep it in step     | [Architecture](#architecture)   |
| Read a file the app writes, or the numbers it was measured at    | [Reference](#reference)         |

<br>

## Finding things

- **One topic, one file.** Each file covers one topic and opens with its title. A topic too big
  for one file is a folder: its `README.md` holds the topic's own notes and lists the rest in
  reading order.
- **Architecture mirrors the tree.** The notes on `internal/<path>` are at
  `architecture/<path>.md`: `internal/inbox` is [architecture/inbox.md](architecture/inbox.md),
  and `cmd/nilbox` is [architecture/cmd/nilbox.md](architecture/cmd/nilbox.md). Packages that
  share notes, or have none of their own, are in
  [Packages covered elsewhere](#packages-covered-elsewhere).
- **Guide and development pages** are named for their subject (`guide/inbox-cards.md`,
  `development/build-targets.md`).
- **The nav line.** Under each title, one line leads back here and across: an architecture file
  names its code and its guide pages (`Guide:`), a guide page its design notes (`Design:`), and
  any page the pages beside it (`See also:`).
- **Searching.** `grep -rn '^#' docs/` lists every heading, and `grep -rln <name> docs/` finds
  where a type, a setting, or a file is described.
- **Keeping them current.** Read a package's notes before changing the package, and change them
  with it: a package's design belongs here, never in `CLAUDE.md`, whose size a guard test caps.
  Repository tests (`internal/repoguard`) check that every link and anchor resolves, that every
  file is listed here, that each opens with its title and nav line, and that none passes 48 KiB.

<br>

## User guide

How the app behaves on your Mac, feature by feature, grouped as the README's Features are.

### Getting started

| Page                                                 | What's in it                                                                                  |
|------------------------------------------------------|-----------------------------------------------------------------------------------------------|
| [First-run setup](guide/first-run-setup.md)          | The setup a fresh install opens on: from the first launch to a filled inbox and an agent launch |
| [Updates](guide/updates.md)                          | How the app checks for, downloads, and installs a new version; Moving to Applications         |
| [Your shell's environment](guide/shell-environment.md) | Why the app reads your login shell's environment, and what it finds tools with              |

### Your inbox

| Page                                                 | What's in it                                                                                  |
|------------------------------------------------------|-----------------------------------------------------------------------------------------------|
| [Inbox cards](guide/inbox-cards.md)                  | How to read a card, compact cards, unread dots, where the details come from, issue projects   |
| [Filtering](guide/filtering.md)                      | The filter's keys and suggestions, what the inbox searches, filtering the other pages         |
| [Snooze, dismiss, and pin](guide/snooze-dismiss-pin.md) | Triage that stays on your Mac, pinned and muted repositories and orgs, waking a snooze early |
| [The item menu and edits on GitHub](guide/item-menu.md) | Everything a card's **⋯** menu does, and its edits on GitHub, with your account and budget |
| [Inbox nil](guide/inbox-nil.md)                      | The party when every list is empty: milestones, the streak, and confetti                      |

### Review and merge

| Page                                                 | What's in it                                                                                  |
|------------------------------------------------------|-----------------------------------------------------------------------------------------------|
| [Reviewing a pull request](guide/reviewing-a-pull-request.md) | The review sheet: what it shows, its one button, merges, held runs, assignees, line comments |
| [Reading an issue](guide/reading-an-issue.md)        | The issue sheet: what it shows, commenting and closing, labels and assignees                  |

### Coding agents

| Page                                                 | What's in it                                                                                  |
|------------------------------------------------------|-----------------------------------------------------------------------------------------------|
| [Launching a session](guide/launching-a-session.md)  | What a launch does: repository locations, worktrees, a clone's own config, workspace trust     |
| [Coding agents and profiles](guide/agents-and-profiles.md) | How the app finds Claude Code, Codex, and their profiles, and picks one                 |
| [Launch defaults](guide/launch-defaults.md)          | What launches use for an org or a repository, and the order the defaults resolve in           |
| [Prompts](guide/prompts.md)                          | The prompt library: starters, writing and checking a prompt, review replies, keeping them     |
| [Following a session](guide/following-a-session.md)  | What the app reads of each session it starts, and where it shows it                           |
| [Outside contributors](guide/outside-contributors.md) | When an item is trusted, and what a launch may do on one that isn't                          |
| [Sensitive files](guide/sensitive-files.md)          | Which changed files count as sensitive, where they show, and what the prompt says             |

### Autopilot (beta)

| Page                                                 | What's in it                                                                                  |
|------------------------------------------------------|-----------------------------------------------------------------------------------------------|
| [Autopilot](guide/autopilot/README.md)               | Routines: start here, the tab, turning one on, and the ⚡ chip                                 |
| [Building a routine](guide/autopilot/building-a-routine.md) | The builder: a routine top to bottom, its problems and warnings, and saving            |
| [Testing a routine](guide/autopilot/testing-a-routine.md) | **Test**, **Run on the matches now…**, and **Preview ▸**                                 |
| [Templates and quick setup](guide/autopilot/templates-and-quick-setup.md) | Easy mode: a routine from a template or from an item, and quick setup      |
| [Describe it](guide/autopilot/describe-it.md)        | Say what you want; Claude Code proposes it as cards you review and apply                      |
| [Use Nilbox in your own Claude Code](guide/autopilot/your-own-claude-code.md) | Describe it from a Claude Code session of your own                     |
| [Export and import](guide/autopilot/export-and-import.md) | Moving routines between Macs in a file                                                   |
| [Agents in your terminal](guide/autopilot/agents-in-your-terminal.md) | A routine's agent step **In my terminal**                                    |
| [Agents in the background](guide/autopilot/agents-in-the-background.md) | A routine's agent step **In the background**: headless, fenced, and followed |
| [Closing the loop: Wait for the checks](guide/autopilot/wait-for-the-checks.md) | Following an agent's push to its checks, tries, and the loop guard   |
| [Merge it and Post the agent's review](guide/autopilot/merge-and-post-review.md) | The two steps that act on what a run came to                        |
| [Needs you](guide/autopilot/needs-you.md)            | What a routine can't do without you, and answering it                                         |
| [Snooze, dismiss, and pin: by a routine](guide/autopilot/snooze-dismiss-pin-by-a-routine.md) | A routine's triage steps, and Undo                    |
| [What Autopilot may change on GitHub](guide/autopilot/changes-on-github.md) | The writes a routine may make on GitHub, and their pace                |
| [Fences: what a background agent may do](guide/autopilot/fences.md) | Each tools preset, the hard lines under every one, and how each agent keeps them |
| [Autopilot's notifications and the menu bar](guide/autopilot/notifications-and-menu-bar.md) | Autopilot's notification kinds and its menu bar rows   |
| [Settings → Autopilot](guide/autopilot/settings.md)  | Autopilot's settings, their defaults, and their ranges                                        |
| [Budgets and caps](guide/autopilot/budgets-and-caps.md) | The limits on what the routines' agents spend                                              |
| [This Mac](guide/autopilot/this-mac.md)              | Which Mac runs the routines, and what each checks it has                                      |
| [What Autopilot never does](guide/autopilot/never-does.md) | The lines a routine never crosses                                                       |
| [Routine searches](guide/autopilot/routine-searches.md) | What a routine's own searches read, what they cost, and when they fail                     |
| [When a routine misbehaves](guide/autopilot/when-a-routine-misbehaves.md) | What to do when a routine does what you didn't want, or nothing          |

### Your clones

| Page                                                 | What's in it                                                                                  |
|------------------------------------------------------|-----------------------------------------------------------------------------------------------|
| [The Worktrees view](guide/worktrees-view.md)        | Every clone's worktrees: rows, chips, cleaning up after a merge, copied work, and branches    |
| [The Scripts tab](guide/scripts-tab.md)              | The tab for the tools the app drives, and when it shows                                       |
| [Release](guide/release.md)                          | Tagging and pushing a release with magex, and following it to published                       |
| [Hooks](guide/hooks.md)                              | go-pre-commit's git hook in every clone, installed and kept current                           |
| [Deps](guide/deps.md)                                | How old each clone's dependencies are, and an agent that upgrades them                        |
| [Repo scripts](guide/repo-scripts.md)                | A clone's own commands and your own scripts, one click each                                   |
| [Runs](guide/runs.md)                                | The one runner behind releases, hooks, checks, scripts, and pulls, and its record             |
| [Tools the app finds and installs](guide/tools.md)   | mage-x and go-pre-commit: which copy the app uses, and installing and updating them           |

### The app

| Page                                                 | What's in it                                                                                  |
|------------------------------------------------------|-----------------------------------------------------------------------------------------------|
| [The window](guide/window.md)                        | The toolbar and its tabs, the account menu, service status, the API budget gauge, Settings    |
| [Menu bar and notifications](guide/menu-bar.md)      | The menu bar icon, closing the window, what is notified, the bell, and banners                |
| [The Notifications page](guide/notifications-page.md) | The app's own notifications, your GitHub ones, and Mark all done                             |
| [GitHub notifications](guide/github-notifications.md) | The mirror of your GitHub notifications: opening, Done, and clearing by rules                |
| [Data location](guide/data-location.md)              | Where the app keeps its database, launch files, and logs                                      |
| [Backing up your setup](guide/backing-up.md)         | Saving your whole setup to one file, and restoring it                                         |
| [Starting over](guide/starting-over.md)              | **Reset Nilbox…**: what starts over and what stays                                            |

<br>

## Development

Building, debugging, and releasing the app. The README's Development section has the gate every
change passes.

| Page                                                 | What's in it                                                                                  |
|------------------------------------------------------|-----------------------------------------------------------------------------------------------|
| [Prerequisites](development/prerequisites.md)        | The one-time setup: macOS, the Xcode tools, Go, Node, pnpm, magex, go-pre-commit, the GitHub CLI |
| [Build targets](development/build-targets.md)        | Every `app:*` target, Nilbox Dev (a build from source), and a sandbox on fresh data           |
| [The inspector](development/inspector.md)            | Looking inside a running build from source: what was clicked, the window, the logs, notes     |
| [Rehearsing an update](development/rehearsing-an-update.md) | Trying an update end to end on one Mac with `magex app:rehearse`                       |
| [Packaging a release](development/packaging-a-release.md) | What `magex app:package` builds, and how it signs                                        |
| [Publishing a release](development/publishing-a-release.md) | Tagging a version, and what the release workflow does                                  |
| [How the desktop shell is kept out of CI](development/ci.md) | What CI builds and tests on Linux, and what only a Mac checks                         |

<br>

## Architecture

The design notes of every package, grouped as `CLAUDE.md`'s Layout is. Read a package's notes
before changing it.

### The app and its shell

| Notes                                                | What's in them                                                                                |
|------------------------------------------------------|-----------------------------------------------------------------------------------------------|
| [`cmd/nilbox`](architecture/cmd/nilbox.md)           | The entry point; headless by default, the desktop app with the `desktop` tag; `--data-dir`, the update helper, and the fence hook and tool modes |
| [`internal/brand`](architecture/brand.md)            | Product identity, design tokens, and the logo; `brandgen` generates the theme, `Info.plist`, and the icon artwork in `build/` |
| [`internal/core`](architecture/core.md)              | Assembles the app and builds every service the window binds                                   |
| [`internal/event` and `internal/desktop`](architecture/event-and-desktop.md) | The app's events; the shell's logic (menu bar, window close, quit guard, workers, shutdown) |
| [`internal/menubar`](architecture/menubar.md)        | What the menu bar icon and its menu show, worked out from the inbox, the runs, the update, and Autopilot |
| [`internal/appinfo`](architecture/appinfo.md)        | App info, Report a bug, and Send feedback                                                     |
| [`internal/devtools`](architecture/devtools.md)      | The inspector a build from source carries (the `devtools` tag, never a release's), which `magex inspect:*` talks to |
| [`internal/store`](architecture/store.md)            | Opens the database, self-heals, backs up and restores; what outlives the database            |
| [Shared building blocks](architecture/shared-building-blocks.md) | `repo`, `item`, `agent`, `procs`, `execpath`, `shellenv`, `clock`, `periodic`, `flight`, `paths`, `progress`, `textutil`, `toolsigns`, and the test support |

### GitHub, the inbox, and notifications

| Notes                                                | What's in them                                                                                |
|------------------------------------------------------|-----------------------------------------------------------------------------------------------|
| [`internal/budget` and `internal/github`](architecture/budget-and-github.md) | The one GitHub client, its request and write rules, and the API budget |
| [`internal/inbox`](architecture/inbox.md)            | The searches, Review requested and Yours, triage, the checks and merge followers, and the routines' watched searches |
| [`internal/reporules`](architecture/reporules.md)    | Pinned and muted repositories and orgs                                                        |
| [`internal/heldruns`](architecture/heldruns.md)      | Which held workflow runs count toward where a pull request stands, and the Ignored workflows setting |
| [`internal/threadrules`](architecture/threadrules.md) | The rules for clearing GitHub notifications                                                  |
| [`internal/ghthreads`](architecture/ghthreads.md)    | Mirrors the user's GitHub notifications, marks threads done or read, and marks them done in bulk |
| [`internal/statusprefs`](architecture/statusprefs.md) | The service status catalog and what the user watches                                         |
| [`internal/svcstatus`](architecture/svcstatus.md)    | Follows GitHub's, Claude's, and OpenAI's status pages                                         |
| [`internal/milestones`](architecture/milestones.md)  | The log of the inbox at nil: the streak, the achievements, and confetti at most once a day    |
| [`internal/notify`](architecture/notify.md)          | The app's own notifications: the history, gathering, and the native banners           |
| [`internal/notifyprefs`](architecture/notifyprefs.md) | The notification kinds, and their banner switches                                        |

### The sheets

| Notes                                                | What's in them                                                                                |
|------------------------------------------------------|-----------------------------------------------------------------------------------------------|
| [`internal/review`](architecture/review.md)          | The review sheet's service: offers, merges, line comments, and the diff cache                 |
| [`internal/issues`](architecture/issues.md)          | The issue sheet's service: comment, close, labels and assignees, and the issue cache          |
| [`internal/timeline`](architecture/timeline.md)      | The conversation both sheets show                                                             |
| [`internal/avatars`](architecture/avatars.md)        | The profile pictures the sheets and the inbox's cards show, people's and bots'                |
| [`internal/people`](architecture/people.md)          | The local directory of who works on each repository and who can be assigned there            |
| [`internal/assignees`](architecture/assignees.md)    | Whom both sheets' assignee pickers offer, and when a read of GitHub is due                    |
| [`internal/bump`](architecture/bump.md)              | Reads a dependency update from a bot's pull request                                           |

### Agents and launches

| Notes                                                | What's in them                                                                                |
|------------------------------------------------------|-----------------------------------------------------------------------------------------------|
| [`internal/launcher`](architecture/launcher.md)      | Resolves, plans, and opens a launch in the user's terminal; holds, trust, and `workspacetrust` |
| [`internal/sessions`](architecture/sessions.md)      | Follows the agent sessions launches start                                                     |
| [`internal/prompts`](architecture/prompts.md)        | The prompt library and review replies: starters, templates and their checks, defaults, and the copy beside the database |
| [`internal/launchdefaults`](architecture/launchdefaults.md) | The org and repository launch defaults, how they resolve, and the copy beside the database |
| [`internal/profiles`](architecture/profiles.md)      | The coding agents' profiles: discovery, pruning, and sign-in                             |
| [`internal/trust`](architecture/trust.md)            | Which items are trusted, the trusted bots, and the auto-merge bots                        |
| [`internal/sensitive`](architecture/sensitive.md)    | Which changed files are sensitive, and how a pull request's files are checked              |

### Clones and worktrees

| Notes                                                | What's in them                                                                                |
|------------------------------------------------------|-----------------------------------------------------------------------------------------------|
| [`internal/localrepos`](architecture/localrepos.md)  | Finds the user's clones in the clone root and the other repository locations                  |
| [`internal/pushwatch`](architecture/pushwatch.md)    | Notices a push made on this Mac to an inbox pull request, and has it read again               |
| [`internal/worktrees`](architecture/worktrees.md)    | Makes the app's worktrees, records them, and removes them and their branches safely           |
| [`internal/gitread`](architecture/gitread.md)        | The allowlisted read-only git, and the guarded setups for the git commands that change a clone |
| [`internal/clonelock`](architecture/clonelock.md)    | The one lock per clone that every job changing it holds, and the sentence that refuses another |

### Integrations and runs

| Notes                                                | What's in them                                                                                |
|------------------------------------------------------|-----------------------------------------------------------------------------------------------|
| [`internal/integrations`](architecture/integrations.md) | Finds, installs, and updates magex and go-pre-commit                                       |
| [`internal/scriptrun`](architecture/scriptrun.md)    | Runs and follows the integrations' commands and a routine's background agent                  |
| [`internal/release`](architecture/release.md)        | The Release page                                                                              |
| [`internal/hooks`](architecture/hooks.md)            | The Hooks page                                                                                |
| [`internal/scripts`](architecture/scripts.md)        | Repo scripts, and Pull                                                                        |
| [`internal/manifests`](architecture/manifests.md)    | The Deps page: each clone's `go.mod` and `package.json` files and their ages                  |

### Updates

| Notes                                                | What's in them                                                                                |
|------------------------------------------------------|-----------------------------------------------------------------------------------------------|
| [`internal/update`](architecture/update.md)          | How the app updates itself                                                                    |
| [`internal/updateprefs`](architecture/updateprefs.md) | The `update.mode` setting                                                                    |
| [`internal/semver`](architecture/semver.md)          | Semantic versions                                                                             |

### Autopilot

| Notes                                                | What's in them                                                                                |
|------------------------------------------------------|-----------------------------------------------------------------------------------------------|
| [`internal/autopilot`](architecture/autopilot/README.md) | The routines core's five pure leaves, and what each part of Autopilot does               |
| [The routines, kept](architecture/autopilot/routines-kept.md) | The routines' table, the bound Service, their copy beside the database, and the settings |
| [The searches, and the builder's reads of the inbox](architecture/autopilot/searches.md) | What the builder reads of the inbox, and the window's builder |
| [The engine and the ledger](architecture/autopilot/engine-and-ledger.md) | The ledger of what each routine did, and the engine's cycle, queue, and commands |
| [What runs next](architecture/autopilot/upcoming.md) | The ⚡ chip's *What runs next*, worked out as a cycle decides                                  |
| [Verify, tries, the loop guard, and profile fallback](architecture/autopilot/verify-and-tries.md) | How a run's push is judged, tried again, kept from looping, and moved to another profile |
| [Merge it and Post the agent's review](architecture/autopilot/merge-and-review.md) | The two GitHub writes that follow an agent's run |
| [Agents in the terminal, readiness, and Needs your OK](architecture/autopilot/agents-in-the-terminal.md) | The start path, holds, readiness, and the user's OK |
| [Agents in the background](architecture/autopilot/agents-in-the-background.md) | The background runner, `internal/agentrun`: the stream, the run view, Take over, and the end |
| [Codex in the background](architecture/autopilot/codex-in-the-background.md) | What Codex does differently as a background agent             |
| [Local actions: by a routine, Undo, and hands off](architecture/autopilot/local-actions.md) | A routine's pin, snooze, dismiss, and restore, and Undo |
| [The GitHub step](architecture/autopilot/github-step.md) | The GitHub dispatcher: one action, one run, its limits and replies                        |
| [Notices, the menu bar, and the window's close](architecture/autopilot/notices.md) | Notify me and Tell me, the glance, the menu bar, and the shell |
| [Easy mode](architecture/autopilot/easy-mode.md)     | The gallery, quick setup, from an item, why a routine didn't run, export and import, graduating |
| [Schedules, the digest, the script step, and a routine's week](architecture/autopilot/schedules.md) | Schedules, the digest, Run one of my scripts, Run on the matches now, and the week |
| [The Autopilot tab](architecture/autopilot/autopilot-tab.md) | The window's side of Autopilot                                                       |
| [`internal/autopilot/draft`](architecture/autopilot/draft.md) | The one check of a routine's draft, for the builder, Save, and kit                   |
| [`internal/agentrun/stream`](architecture/agentrun/stream.md) | Reads an agent's stream, for background runs and Describe it's turns                 |
| [`internal/fence`](architecture/fence.md)            | Keeps an unattended agent run inside its tools preset                                         |

### Describe it

| Notes                                                | What's in them                                                                                |
|------------------------------------------------------|-----------------------------------------------------------------------------------------------|
| [`internal/autopilot/kit`](architecture/autopilot/kit.md) | What Claude Code reads and proposes: the snapshot, a proposal, its checks and risks, and the words |
| [`internal/describeit`](architecture/describeit.md)  | The conversation, its cards, and the plugin for the user's own Claude Code                    |
| [`internal/mcpserver`](architecture/mcpserver.md)    | The tool mode, the app's binary serving Describe it's tools; it writes only proposals         |

### Backup and setup

| Notes                                                | What's in them                                                                                |
|------------------------------------------------------|-----------------------------------------------------------------------------------------------|
| [`internal/backup`](architecture/backup.md)          | Exports and imports the user's setup                                                          |
| [`internal/setup`](architecture/setup.md)            | The first-run setup, and a reset                                                              |

### The window

| Notes                                                | What's in them                                                                                |
|------------------------------------------------------|-----------------------------------------------------------------------------------------------|
| [`frontend`](architecture/frontend/README.md)        | The React + TypeScript + Vite + Tailwind window: the build and its checks, shared hooks, both sheets, and the pages |
| [The inbox cards](architecture/frontend/inbox-cards.md) | `ItemRow` and the card's parts, its settings, and the party at inbox nil                   |
| [The Scripts tab's pages](architecture/frontend/scripts-pages.md) | The Release page's rows and the Deps page                                         |
| [The Worktrees page](architecture/frontend/worktrees-page.md) | `WorktreesView`, its grid, and its rows                                              |

### Packages covered elsewhere

| Package                                              | Its notes                                                                                     |
|------------------------------------------------------|-----------------------------------------------------------------------------------------------|
| `internal/agent`, `clock`, `execpath`, `flight`, `item`, `paths`, `periodic`, `procs`, `progress`, `repo`, `shellenv`, `textutil`, `toolsigns`, `testutil`, and the test fakes (`githubtest`, `assigneestest`, `localrepostest`, `scriptruntest`) | [Shared building blocks](architecture/shared-building-blocks.md) |
| `internal/agentrun`, `internal/agentrun/runevent`    | [Agents in the background](architecture/autopilot/agents-in-the-background.md) and [Codex in the background](architecture/autopilot/codex-in-the-background.md) |
| `internal/autopilot/catalog`, `watch`, `spec`, `facts`, `eval` | [`internal/autopilot`](architecture/autopilot/README.md), the routines core's five leaves |
| `internal/autopilot/sim`                             | [Local actions](architecture/autopilot/local-actions.md) and [The GitHub step](architecture/autopilot/github-step.md) |
| `internal/autopilotprefs`                            | [The routines, kept](architecture/autopilot/routines-kept.md)                                 |
| `internal/brandgen`                                  | [`internal/brand`](architecture/brand.md)                                                     |
| `internal/branchrules`, `internal/worklog` | [`internal/worktrees`](architecture/worktrees.md)                                 |
| `internal/integrationprefs`, `internal/toolinstall`  | [`internal/integrations`](architecture/integrations.md)                                       |
| `internal/launcher/terminal`, `internal/workspacetrust` | [`internal/launcher`](architecture/launcher.md)                                            |
| `internal/sessions/observe/...`                      | [`internal/sessions`](architecture/sessions.md)                                               |
| `internal/store/migrate`                             | [`internal/store`](architecture/store.md)                                                     |
| `internal/measure`                                   | [Performance](performance.md)                                                                 |
| `internal/prefs`, `internal/prefs/prefval`, `internal/settings` | `CLAUDE.md`'s [Data layer](../CLAUDE.md#data-layer), and the package docs          |
| `internal/uierr`                                     | `CLAUDE.md`'s [Logging and errors](../CLAUDE.md#logging-and-errors), and the package doc      |
| `internal/repoguard`                                 | `CLAUDE.md`'s [Rules every change keeps](../CLAUDE.md#rules-every-change-keeps), and the package doc |
| `internal/snooze` | The package doc; what it does in [Snooze, dismiss, and pin](guide/snooze-dismiss-pin.md) |
| `internal/appdata`, `internal/logging`, `internal/timing` | The package docs; where the app keeps its files in [Data location](guide/data-location.md) |

<br>

## Reference

| Page                                                 | What's in it                                                                                  |
|------------------------------------------------------|-----------------------------------------------------------------------------------------------|
| [Performance](performance.md)                        | Measured startup, refresh, launch, size, and API-budget numbers, and how to take them again  |
| [Prompt files](prompt-files.md)                      | The format of a prompt file, the app's portable form of your prompts                         |
| [Backup files](backup-files.md)                      | The format of a backup file, your whole setup in one portable file                           |
