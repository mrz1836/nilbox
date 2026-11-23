# internal/store

[Docs](../README.md) › [Architecture](../README.md#architecture) · Code: [`internal/store`](../../internal/store) · Guide: [Prompts](../guide/prompts.md), [Data location](../guide/data-location.md), [Starting over](../guide/starting-over.md) · See also: [Prompt files](../prompt-files.md), [Backup files](../backup-files.md)

## Migrations and squashes

The schema is the migrations in `internal/store/migrate/migrations/`, each recorded with its file's
checksum once it runs. A migration a build has shipped is never edited or deleted: a database that
ran it would read as a history this build disagrees with and be set aside. To combine shipped
migrations into fewer files, squash them, which carries every user's database over without touching
its data:

1. Make sure the users run the newest release; only the histories a squash freezes carry over.
2. Freeze each history users' databases may have, before deleting anything:
   `go test ./internal/store/migrate -run TestFreezeASquashedHistory -freeze NNNN`, NNNN the
   newest version of the history (the newest release's, and the tip's when a migration came
   after it). It writes `squashed/NNNN.json` (each version, name, and checksum) and
   `squashed/NNNN.schema` (the schema those migrations build, `schemaOf`'s form: comments,
   whitespace, case, and quotes left out, a table's columns and constraints one per line).
3. Write the base: whole `CREATE`s at the next hundred above every version any build shipped
   (`0100`, then `0200`), the columns in each table's order, an added column after the last
   one and before the table's constraints, as SQLite's `ADD COLUMN` writes it, and no data
   steps (they fixed rows older builds stored). A migration after the newest release stays a
   file of its own after the base.
4. Set each history's `through` (the newest base migration that builds its schema) and its
   `about`, delete the old files and their per-migration tests, and run `magex db:generate`:
   the generated code must not change but for comments that name a migration.

`TestEverySquashedHistoryBuildsTheSchemaOfItsBase` builds the base through each `through` and
compares it with the frozen `.schema`, and a database whose history is exactly a frozen one is
carried over by `migrate.Apply`: backed up as `-v<its newest>`, its history rows swapped for the
base's in one transaction, nothing run against its tables, then the later migrations applied. Any
other history of versions below the base is `migrate.ErrSquashedHistory`, which `store.Open` sets
aside as `ResetHistory`. An older build that opens a database carried over finds versions above
its own and refuses it as a newer build's, offering the backup taken before the carry over
(`store.KnownSchemas`, which `desktop.RestoreOffer` reads, knows the frozen histories' versions
too). A later squash drops the histories it can't carry over (their `through` is gone), so a
database two squashes old is set aside.

## Rebuilding a table

SQLite can't change a CHECK, so a migration that widens one makes the table again inside its own
transaction: a new table with the wider checks, its columns in their order (so the generated code
doesn't change) and every other constraint and default as before, every row copied as it is under
explicit column lists, the old table dropped and the new one renamed, and its indexes made again.
`0112_repo_sessions` does that to `agent_sessions`, which no table references and no trigger or view
names, for the sessions a repository's launch starts (no item: kind `''`, number 0, item 0, and the
worktree it made, never `''`; [internal/sessions](sessions.md)). A table another references takes
one step more, which `0111_repo_prompts` takes for `prompts`, whose `applies_to` may now be `'repo'`
(with `CHECK (applies_to <> 'repo' OR purpose = 'agent')`): foreign keys stay on, and
`launch_defaults` names prompts in six `ON DELETE RESTRICT` columns, which refuse the old table's
drop at once, deferred or not. So the launch defaults that name a prompt set their six columns aside
in an explicitly typed scratch table (`launch_defaults_prompt_ids`) and clear them; the new table
takes every prompt under its own id, the old one is dropped and the new one renamed (a foreign key
names its table by name, so the six name the new one once it is called `prompts`); each column takes
its value back through a correlated subquery; and the scratch table goes. Each migration's test
fills the table first (for `0111`, every one of the six references) and checks that every row and
column comes through as it was, that the new checks hold, and, for `0111`, that
`pragma_foreign_key_check` finds nothing and a prompt a launch default names still can't be deleted.

## Self-heal

`store.Open` backs the database up before applying pending migrations. A file that is not a database
or is damaged, a schema the migrations did not create, or a history this build disagrees with is
renamed, with its `-wal` and `-shm` files, to `<path>.broken-<UTC stamp>` once no other process has
it open; a fresh database starts in its place, and `Opened.Reset` reaches the window as a notice. A
database from a newer build is refused with `store.ErrNewerSchema` and left untouched, and the
startup dialog offers this build's way forward without opening it again (an update through
`update.Bare`, or the command that installs or builds a newer build; see
[cmd/nilbox](cmd/nilbox.md)). Any other failure (a lock, a permission, a full disk, a failed
migration) is a plain error, and nothing is moved. A reset the user asked for (Settings → About, or
`magex app:reset`, each writing `appdata.RequestReset`'s request) is carried out by the next start,
in core's `openData`, before anything opens the database: `store.OpenFresh` waits about five seconds
for another copy of the app to let go, then sets the database aside as `<path>.reset-<UTC stamp>`
(`ResetRequested`, no error) for a fresh one, putting it back when the fresh one fails;
`store.PruneResetCopies` keeps the newest three, Describe it's conversations and proposals, which
name routines that are gone, are removed (`forgetConversations`; its plugin, the plugin's link, and
`agent.json` stay), and the request is cleared. A database still held is a busy error, the startup
dialog's, with the request kept for the next start; with no database file there is nothing to set
aside, and a request that can't be read is logged and dropped, never carried out. `store.Backup`
takes a backup of the open database whenever asked, as an update's restart does before it installs,
named and pruned exactly as the migrations' (`migrate.Backup`: `<path>.backup-<UTC
stamp>-v<schema>`, the newest three of both kept; `store.Snapshots` reads the schema only from a
name ending `-v<digits>` or `-v<digits>-<n>`, so a copy renamed `-v4-copy` names none), and
`store.RestoreBackup` puts one of those backups back in the database's place for an older build that
finds a newer schema: it refuses a file that isn't one of the database's own backups, waits for
another copy to let go, sets the database aside as `<path>.broken-<UTC stamp>` with its `-wal` and
`-shm`, and copies the backup in, putting the database back when the copy fails.

## Prompts, launch defaults, and the user's scripts outlive the database

They are what the user made, which GitHub can't give back. `prompts.LibraryFile` rewrites
`prompts.json` beside the database after every change to the library, and `launchdefaults.File`
rewrites `launch-defaults.json` after every change to the launch defaults or the library, since it
names prompts by nickname: each row's six default prompts, the kinds' and the situations'
(`launchdefaults.Kept`, the four situations' left out when they inherit, so an older copy inherits
them), which `promptsOf` lists once for the copy, a restore, and a backup's import. At startup, when
the library is new, `prompts.Restore` brings it back before `Seed` runs, from the database set aside
or else from `prompts.json`, and core (`internal/core/kept.go`) restores the launch defaults from
the same place, matched to the restored prompts. Core writes each copy at startup only when it is
missing or the data was just restored, so a loss in the database never overwrites a copy.

The Autopilot routines keep a copy too: `autopilot.File` rewrites `autopilot.json` (the export
format) after every change to a routine, and when `autopilot_routines` holds no row at all, deleted
ones included, `autopilot.Restore` brings them back after the prompts and the launch defaults, from
the database set aside or else from the copy, each switched off, its spec stored as it was (a newer
build's stays broken, never rewritten); a routine names its prompts by nickname, which the restored
library keeps. What neither copy nor a restore carries is the user's confirmation that a routine may
merge past the rules (`autopilot_routines.past_rules_hash`, added by migration
`0110_routine_past_rules`, which `Restore` never reads): a restored routine with a tick asks for it
again. The user's own scripts have no copy, but come back from a database set aside, whatever the
reason (`scripts.Restore`, beside the restore of the prompts, only into an empty list, each checked
as `Save` checks it), so a reset keeps them. Agent profiles need no copy: discovery finds them on
every start, and nothing in the app edits one. `store.OpenSnapshot` reads a kept database of any
schema, read-only and tolerant of missing columns, for the restore and for **Restore from a
backup**. Import and restore go through one planner (`internal/prompts/importer.go`), which never
changes a prompt the user wrote or edited, and checks every imported setting the way the editor
does.

A starter's copy whose text the user never changed gets a later build's text: a change to a
starter's text keeps the text it replaces, byte for byte, in the starter's `Earlier` (pinned by
SHA-256 in `TestEarlierStarterTextsAreAsShipped`), and `Seed` gives every copy still holding one the
new text, keeping its title, nickname, and settings, as an import does a file's; a copy whose text
the user changed, or a duplicate, is never touched. A starter reply becomes its slot's default reply
(`prompts.ReplySlot`: `approve`, `approve_bots`, `request_changes`, `comment`, and `nudge`, each in
the pref `prompts.default.reply.<slot>`, `approve_bots` as `approve.bots`) as `Seed` adds it, when
the slot has none, so a library from a build before a slot existed gets the slot's starter and its
default once: the **Nudge** reply (`reply-nudge`, a comment reply for pull requests) that a card's
Nudge posts, which the review sheet's comment replies carry as `review.ReplyList.Nudge`, so the
nudge dialog fills it in through the same `FillReply`.

After the starters, `Seed` fills the app's prompt for each situation a pull request can be in
(`prompts.DefaultSlot`'s `checks_failing`, `conflicts`, `changes_requested`, and `rereview`, the
prefs `prompts.default.<slot>`) once, from the library's copy of the slot's starter when it has one,
and records the slot in `prompts.seededSlots` either way, so a slot the user cleared, or whose
starter they deleted, stays as they left it; unlike a kind's, a situation's default never falls back
to another prompt. The record travels with the library like `prompts.seeded`: a whole library's
`prompts.Bundle` carries it as `slotsSeeded` (`prompts.json`, an export, a backup's prompts
section), `snapshotRecords` reads it and the four situations' prefs out of a database set aside, and
`prompts.Restore` stores it, so a reset or a restore never fills a cleared slot again, while a
library restored from a file without it, as an older build's, has each slot filled once.

A starter's command names a value a repository, a clone's config, or an author chose only when it is
plain (`.BranchIsPlain`, `.RemoteIsPlain`, worked out from the raw value in `Data.sanitized`, since
sanitizing can hide characters), and then through the `shellquote` template function
(`TestStarterCommandsAreInert`).

The file format is `prompts.Bundle`, specified in [`docs/prompt-files.md`](../prompt-files.md); keep
the two in step, and raise `BundleVersion` only for a change an older build would misread. An import
offers the file's defaults (`ImportPreview.Defaults`, each a `DefaultOffer` where its prompt fits
the slot and it would change something) and applies the chosen ones in the transaction of the
prompts, through the nickname links `importInto` returns. The user's whole setup, these three
included, also leaves the app as a backup (`internal/backup`, its format in
[`docs/backup-files.md`](../backup-files.md)), whose prompts, launch defaults, and Autopilot
sections are exactly these three copies' formats; keep the backup's in step with them.

## Prompts for repositories

A prompt may apply to repositories (`prompts.AppliesToRepo`, `applies_to = 'repo'`, which the window
calls Repositories; migration `0111_repo_prompts`, above): an agent prompt alone, never a reply (the
table's check, the editor's `checkReplyAppliesTo`, and an import, which reads a reply's `repo` as
`pr`), which a repository's launch fills in for a clone rather than for an item, as the Deps page's
Upgrade does ([internal/launcher](launcher.md)). It covers no kind of item (`AppliesTo.Covers`), so
no launch on a pull request or an issue fills one in, nor can one be a kind's or a situation's
default (`checkAppliesTo`), and `Prompt.IsRepo` tells one. Its text is checked with the sample
repository (`SampleRepo`: acme-org/example-repo, `main`, and `origin`, and again with no default
branch or remote, each in the clone and in a worktree named `deps-20260115`), and previewed with it,
an item the editor picked set aside with a note (`repoPreviewData`); a launch fills it in from
`RepoData` (the repository's names, its default branch, and the clone's remote) and `WithWorktree`
(the worktree the launch made, by the name it was made under), never `WithSession`, which names an
item's worktree. Every variable but the repository's and the session's (`.Repo`, `.Owner`,
`.RepoName`, `.DefaultBranch`, `.Remote`, `.RemoteIsPlain`, `.InWorktree`, `.Worktree`, `.Profile`,
and `.Me`, `repoVariables`) is `Variable.ItemOnly`, always empty there, and the checker warns where
a prompt for repositories names one, as it warns of a pull request's variable in a prompt for
issues. A prompt a routine runs can't be made one (`checkRepoSwitch`, at the applies-to field; an
import never saves one over a starter's copy a routine runs either, `takesPlace`), since a routine
runs its prompts on pull requests and issues; one already for repositories stays free to change. Two
starters are for repositories (`starters_repo.go`, added after the others): **Upgrade dependencies**
(`deps-minor`, nicknamed Upgrade deps), which keeps each dependency within its major version and
lists the newer majors it left alone in the pull request, and **Upgrade dependencies, majors too**
(`deps-major`, Upgrade majors too), which keeps only the majors whose checks pass after a reasonable
fix. Each starts in a worktree, out of plan mode, and has the agent learn the repository's rules,
upgrade its Go modules (`magex deps:update all-modules stable-only`, with `allow-major` for majors,
where magex is set up, else `go get -u ./...` and `go mod tidy`) and its JavaScript packages (with
the lockfile's manager, never another), run the repository's own checks, commit, push with `git push
-u <remote> HEAD`, and open the pull request with `gh pr create --repo <repo>` against the default
branch; the remote is named only under the `.RemoteIsPlain` guard (origin when none is known), and
`--base <default>` only when `shellquote` leaves the default branch as it is (`defaultBranchOK`),
each through `shellquote`, so a remote that isn't plain gets no push, and the session commits and
stops. Prompts for repositories raised `BundleVersion` to 4: an older build reads an `appliesTo` it
doesn't know as `any`, so it would import a prompt for repositories as one for both kinds of item,
and a file of an earlier version holds none. Describe it leaves the prompts for repositories and
their starters out of its snapshot (core's `itemPrompts` and `itemStarters`), as no routine runs
one, and the draft's check treats a prompt written for no kind of item as a problem, whatever the
routine acts on (`kindsProblem`).
