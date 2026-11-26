# internal/core

[Docs](../README.md) › [Architecture](../README.md#architecture) · Code: [`internal/core`](../../internal/core)

`internal/core/` assembles the app and builds every service the window binds: settings,
profiles, prompts, launch defaults, inbox, review, issues, the profile pictures, launcher,
worktrees, notifications, app info, the API budget, sessions, integrations, script runs,
release, hooks, repository scripts, the Deps page ([internal/manifests](manifests.md)),
the mirror of the user's GitHub notifications, the
backup of the user's setup, the service status check, the first-run setup, the app's updates,
Autopilot's routines, and [Describe it](describeit.md).
Some building blocks are built once and handed to every service that needs them, so what they
hold is shared: the clone locks (`clonelock.Set`), the profile pictures (`avatars.Service`), and
the assignee Finder both sheets offer people to assign through (`assignees.Finder`, over the
People directory, adapted by `assigneesDirectory` in `adapters_assignees.go`), so a
repository's assignable users are read at most once at a time, whichever sheet asks.
Every `internal/*` package is plain Go, built and tested on Linux without CGO.

The terminal reaches the services through two adapters over the launcher's one registry.
`terminalRuns` (`adapters_runs.go`) writes a followed run's script into the launch folder and
opens it through `launcher.CommandOpener`, and its `OpenFolder`, which every page's Open in
terminal goes through (the runs', Hooks', and Release's directly, the Worktrees view's and Deps'
through `worktreeTerminal`), tries `CommandOpener.OpenFolder` first, so a terminal that opens a
folder itself (Warp) gets no script, and writes and opens the script that changes into the
folder and starts the login shell only when it can't. `terminalFocuser` (`adapters_sessions.go`)
builds the `terminal.Tab` for the sessions' `Focuser`, focuses in the terminal the agent's
`TERM_PROGRAM` names (read with its `env`, `procs.EnvOf`, at the click), the session's recorded
one when it names none, and words its errors: no such tab, no Warp focus link (naming Warp
v0.2026.05.27), and a terminal that can't show a tab. See [internal/launcher](launcher.md).
