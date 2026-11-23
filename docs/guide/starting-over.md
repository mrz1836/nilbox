# Starting over

[Docs](../README.md) › [User guide](../README.md#user-guide) · Design: [`internal/store`](../architecture/store.md), [`internal/setup`](../architecture/setup.md) · See also: [A sandbox on fresh data](../development/build-targets.md#a-sandbox-on-fresh-data)

**Settings → About → Reset Nilbox…** starts the app over on fresh data, once you type `reset`
to confirm. It saves a backup of your whole setup first, as an import does (to
`backups/nilbox before reset <UTC time>.json`), refusing the reset when that fails, and it
waits while runs are going in the app. Then it asks for the reset, quits, and opens again; the
start carries the reset out before anything opens the database, and opens on the
[first-run setup](first-run-setup.md), whose Welcome says what came back. From a terminal,
`magex app:reset` does the same for [Nilbox Dev](../development/build-targets.md#nilbox-dev-a-build-from-source): it quits the
running Nilbox Dev, asks for the reset, and opens it. The release and Nilbox Dev share one data
folder, so a reset in either starts both over.

| Starts over | Stays |
|---|---|
| Settings and repository locations, pinned and muted repositories and orgs, triage, the cached inbox, the notification history, the mirror of your GitHub notifications, worktree records, session history, run history, Autopilot's history, script trust, tool records, the cached diffs, conversations, and profile pictures, and Describe it's conversations and proposals | Your prompts, launch defaults, and your own scripts, which come back from the database set aside; your Autopilot routines, switched off; Describe it's agent and its plugin for your own Claude Code; `backups/` and `logs/`; your clones and worktrees on disk (the next listing records them again); your agents' own config; and GitHub itself |

The database is set aside, with its `-wal` and `-shm` files, as `nilbox.db.reset-<UTC time>`,
and the newest three such copies are kept. To undo a reset, quit the app and rename the newest
copy back to `nilbox.db`. Should another copy of the app still have the database open, the
start stops with a dialog that says so, and the reset waits for the next start: it never
starts on the old data unannounced.
