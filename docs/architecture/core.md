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
