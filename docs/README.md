# Docs

## Building and running the desktop app

Nilbox is a macOS desktop app: a Go core, a [Wails v3](https://wails.io) shell, and a React +
TypeScript + Vite + Tailwind frontend in `frontend/`. This guide covers the one-time setup, the
build targets, packaging and publishing a release, how the desktop shell is kept out of the Linux
CI build, and what the app does on your machine: where it keeps its data, how the window and the
inbox cards are laid out, how it finds your coding agents (Claude Code and OpenAI Codex) and their
profiles, how prompts and launch defaults decide what a session starts with, how it follows each
session it starts, how it decides what an item may do, and how it makes, keeps track of, and
cleans up git worktrees.

Measured startup, refresh, launch, size, and API-budget numbers, and how they were taken, are
in [performance.md](performance.md).

## Architecture

These are the design notes of every package, one section each. The project guide,
[`CLAUDE.md`](../CLAUDE.md), holds only the rules that apply everywhere and lists each package in
one line of its Layout, linking here. Read a package's section before changing the package, and
keep it in step with the change: a package's design belongs here, never in the guide, whose size
a guard test caps (`TestProjectGuideStaysSmall` in `internal/repoguard`).

The sections follow the guide's Layout:

- **The app and its shell**: [`cmd/nilbox`](architecture/cmd/nilbox.md), [`internal/brand`](architecture/brand.md),
  [`internal/core`](architecture/core.md),
  [`internal/event` and `internal/desktop`](architecture/event-and-desktop.md),
  [`internal/appinfo`](architecture/appinfo.md), [`internal/store`](architecture/store.md) (self-heal, and what
  outlives the database)
- [**Shared building blocks**](architecture/shared-building-blocks.md): `agent`, `procs`, `execpath`,
  `shellenv`, `clock`, `periodic`, `flight`, `paths`, `progress`, `textutil`, and the test support
- **GitHub, the inbox, and notifications**:
  [`internal/budget` and `internal/github`](architecture/budget-and-github.md),
  [`internal/inbox`](architecture/inbox.md), [`internal/reporules`](architecture/reporules.md),
  [`internal/threadrules`](architecture/threadrules.md), [`internal/ghthreads`](architecture/ghthreads.md),
  [`internal/statusprefs`](architecture/statusprefs.md), [`internal/svcstatus`](architecture/svcstatus.md)
- **The sheets**: [`internal/review`](architecture/review.md), [`internal/issues`](architecture/issues.md),
  [`internal/timeline`](architecture/timeline.md), [`internal/avatars`](architecture/avatars.md),
  [`internal/people`](architecture/people.md), [`internal/assignees`](architecture/assignees.md),
  [`internal/bump`](architecture/bump.md)
- **Agents and launches**: [`internal/launcher`](architecture/launcher.md),
  [`internal/sessions`](architecture/sessions.md)
- **Clones and worktrees**: [`internal/localrepos`](architecture/localrepos.md),
  [`internal/worktrees`](architecture/worktrees.md), [`internal/gitread`](architecture/gitread.md)
- **Integrations and runs**: [`internal/integrations`](architecture/integrations.md),
  [`internal/scriptrun`](architecture/scriptrun.md), [`internal/release`](architecture/release.md),
  [`internal/hooks`](architecture/hooks.md), [`internal/scripts`](architecture/scripts.md)
- **Updates**: [`internal/update`](architecture/update.md), [`internal/updateprefs`](architecture/updateprefs.md),
  [`internal/semver`](architecture/semver.md)
- **Autopilot**: [`internal/autopilot`](architecture/autopilot/README.md) (its prefs, the background runner
  `internal/agentrun` in [Agents in the background](architecture/autopilot/agents-in-the-background.md), and
  [the Autopilot tab](architecture/autopilot/autopilot-tab.md)), [`internal/autopilot/draft`](architecture/autopilot/draft.md),
  [`internal/agentrun/stream`](architecture/agentrun/stream.md), [`internal/fence`](architecture/fence.md)
- **Describe it**: [`internal/autopilot/kit`](architecture/autopilot/kit.md),
  [`internal/describeit`](architecture/describeit.md) (with
  [what the conversation spike found](architecture/describeit.md#what-the-conversation-spike-found)),
  [`internal/mcpserver`](architecture/mcpserver.md)
- **Backup and setup**: [`internal/backup`](architecture/backup.md), [`internal/setup`](architecture/setup.md)
- **The window**: [`frontend`](architecture/frontend/README.md)
