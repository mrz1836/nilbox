# internal/backup

[Docs](../README.md) › [Architecture](../README.md#architecture) · Code: [`internal/backup`](../../internal/backup) · Guide: [Backing up your setup](../guide/backing-up.md) · See also: [Backup files](../backup-files.md)

`internal/backup/` is Settings → Back up and restore's service, which owns no table. It
exports the user's setup to one `<slug>.backup` file (`File`, version 1, at most 16 MB,
written user-only through `appdata.WriteFile`), one section per category ticked: settings,
repositories, prompts (a `prompts.Bundle`, exactly as `prompts.json`), launch defaults (the
`launch-defaults.json` format), the user's scripts, and the Autopilot routines (the
`autopilot.json` format, each added or saved over arriving off, one a newer build wrote
never touched); and imports one back. It reaches each
category's owner only through a `Part` (`Export`, `Prepare` outside the transaction, `Apply`
inside it, `Done` once it commits), which `internal/core/adapters_backup.go` adapts over each
owner's `BackupPart` (`settings`, `prompts`, `launchdefaults`, `scripts`, `autopilot`), a type of its own
built with `NewBackupPart`, never a method of the bound Service, so the window can't reach a
method that takes a `*sql.Tx`; the owners import only backup's words (`Mode`, `Plan`, `Item`,
`Count`), and the boundary test forbids the reverse. `Preview` works every section out under
both modes (Merge, whose wire value is `combine`, and Replace), a section that can't be read
shown with why; `Import` reads the file again and refuses it when its SHA-256 differs from the
preview's (`ErrChanged`), saves the whole setup first to `<data>/backups/<slug> before import
<stamp>.json` (the newest `KeepBefore`, 5, kept), works each section out again, then applies
every one in one `store.InTx`, in `Sections` order, a `Clearer`'s rows (the launch defaults',
under Replace) deleted first so the prompts part may delete what they named; the prompts
part fills in the `Links` (each file nickname's ID here) the launch defaults part finds its
prompts through, else the library by nickname. Each owner checks what it imports as its own
setters or editors do (settings' `check…` helpers, shared with the setters), leaving out what
it refuses with a note: a setting the setter refuses, a profile not on this Mac, a copy of a
tool that isn't a program here, a location that doesn't exist, a launch default's prompt not
found (it inherits). Under Replace, prompts are matched by nickname and purpose, the rest
deleted but one a launch default the import keeps still names; scripts identical to the
file's keep their IDs. A backup never holds the GitHub sign-in, caches, mirrors, history,
per-item triage, agent profiles, script trust (a file from elsewhere could pre-approve a
repository's command), the window's memory, or tool detection. Its `Saver` (`NewSaver`, a
type of its own, never bound) saves the whole setup before a reset the user asked for, to
`<data>/backups/<slug> before reset <stamp>.json`, the newest `KeepBefore` of those kept apart
from the import's.
