# Data location

The app keeps its database, launch files, and logs in one directory, created on first start
and readable only by you:

| OS | Directory |
|---|---|
| macOS | `~/Library/Application Support/Nilbox` |
| Linux (not yet supported) | `$XDG_DATA_HOME/nilbox`, else `~/.local/share/nilbox` |
| Windows (not yet supported) | `%AppData%\Nilbox` |

The database is `nilbox.db` (SQLite, WAL mode). Before a schema migration changes an existing
database, a copy of it is saved next to it, and the three newest copies are kept. A
[reset](starting-over.md) sets the database aside as `nilbox.db.reset-<UTC time>`, and keeps the
three newest of those too. Beside it,
`prompts.json` and `launch-defaults.json` hold copies of your prompt library and your launch
defaults in plain JSON, each rewritten after every change (see
[Keeping your prompts](prompts.md#keeping-your-prompts)), and `autopilot.json` the same kind of copy of
your [Autopilot](autopilot/README.md) routines ([its format](../backup-files.md#autopilot)). Agent
profiles need no copy: they are found in
your home directory on every start. `backups/` holds the copies of your setup an import of a
[backup](backing-up.md) saves before it changes anything, the five newest, and the
copies a reset saves first (`nilbox before reset <UTC time>.json`), the five newest of those.
A reset asked for and not yet carried out waits in `reset-request.json`, and `autopilot.lock`,
which holds nothing, lets one copy of the app on this data folder run Autopilot. `describe/`
holds [Describe it](autopilot/describe-it.md)'s conversations, the proposals waiting for you, the snapshot
of your setup its tools read, and the plugin for your own Claude Code. A build from
source also keeps [its inspector](../development/inspector.md)'s socket there, `devtools.sock`, and in
`devtools/` the notes you leave by pointing and whether pointing is on; a release writes
neither. GitHub tokens are never
stored there, nor anywhere else: the app borrows the token from `gh auth token` and keeps it in
memory only. It reuses the token for up to ten minutes, and asks `gh` again sooner when GitHub
refuses it or `gh`'s account changes (`gh auth switch`, `login`, or `logout`).

**Logs.** The app logs to `logs/nilbox.log` in the same directory, one JSON record per line,
and writes the same records as text to stderr, which you see when you start the binary from a
terminal (`"bin/Nilbox Dev.app/Contents/MacOS/nilbox"` for a build from source, quoted for its
space). When the file would pass 5 MB it is renamed to `nilbox.log.1`, the earlier `nilbox.log.1` becomes `nilbox.log.2`, and the oldest is
deleted, so at most three files (about 15 MB) are kept. The log keeps Info records and up;
start the binary with `--debug`, or with `NILBOX_DEBUG=1` in its environment, to keep Debug
records too. A log file that can't be written never stops the app: it logs to stderr alone and
says why there. The startup timing lines go to `logs/timing.log`. Neither file holds your
GitHub token.

If the database can't be read at startup, because the file is damaged, isn't a database, or
wasn't set up by this app's migrations, the app renames it (with its `-wal` and `-shm` files)
to `nilbox.db.broken-<UTC time>`, starts with an empty database, and says so in the window.
Your prompts and launch defaults come back on their own: they are read out of the database
set aside, or from `prompts.json` and `launch-defaults.json` when that fails, before the
starters are added. The set-aside file is kept
until you delete it, and **Restore from a backup** on the Prompts page can read it again. A
database saved by a newer version of the app is left as it is: a dialog offers to update (a
release in Applications downloads and installs the latest one, and opens it), or gives the
command that installs or builds a newer one ([Data a newer version saved](updates.md), under
Updates). A database another process has open, such as an older copy of the app
that is still running, or one that can't be written, is never moved. Like anything else that
stops the app from starting, it is shown in an error dialog, which says what to do where that
is known, and the app quits once you dismiss it.

**Another data folder.** Started with `--data-dir=<absolute path>`, the app keeps everything,
its logs included, in that folder instead, and never touches the directory above. It is how
the [sandbox](../development/build-targets.md#a-sandbox-on-fresh-data) runs; a relative path is refused.
