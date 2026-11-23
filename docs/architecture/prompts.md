# internal/prompts

[Docs](../README.md) › [Architecture](../README.md#architecture) · Code: [`internal/prompts`](../../internal/prompts) · Guide: [Prompts](../guide/prompts.md)

`internal/prompts/` is the prompt library: the agent prompts a launch fills in for a pull request,
an issue, or a repository, each with the launch settings it sets; the review replies the sheets
fill in as the text of a review or a comment, which share its table (`Purpose`); the app's defaults
among them; the starters the app ships; and the copies of the library it keeps and moves:
`prompts.json`, export and import, and a backup's prompts section. It owns the `prompts` table and
every `prompts.*` preference, and emits no event. The window binds its `Service`; the launcher, the
launch defaults, the sheets' replies, and Autopilot read its `Store` through interfaces and
adapters of their own; core hands the backup a `BackupPart` and Autopilot's export and import a
`RoutinePrompts`, types of their own the window never reaches. What the user sees is in
[Prompts](../guide/prompts.md), the file format in [Prompt files](../prompt-files.md), and what
outlives the database in
[`internal/store`](store.md#prompts-launch-defaults-and-the-users-scripts-outlive-the-database),
whose [Prompts for repositories](store.md#prompts-for-repositories) these notes don't repeat.

## The table and the store (`store.go`, `internal/queries`)

`prompts` (migration `0100_settings`, made again by `0111_repo_prompts` so `applies_to` may be
`'repo'`) holds one row per prompt or reply: `name` (the title), `nickname`
(`COLLATE NOCASE UNIQUE`, so prompts and replies share one set of nicknames), `applies_to` (`pr`,
`issue`, `any`, or `repo`), `body`, the launch settings (`models` and `agent_args`, JSON objects
keyed by agent; `effort`; `plan_mode` and `worktree`, NULL to inherit), `starter_key`,
`created_at`, `updated_at`, `purpose` (`agent` or `reply`), and `reply_action` (`''` for an agent
prompt, else `approve`, `request_changes`, or `comment`), and `uuid` (`0116_record_uuids`: the
prompt's UUID, which never changes; see
[Records named for good](store.md#records-named-for-good-uuids)). Its checks tie `purpose` and
`reply_action` together and keep a reply off `repo`. The queries in `internal/queries/prompts.sql`
are `ListPrompts` (by nickname ignoring case, then by id), `GetPrompt`, `CountNickname`,
`InsertPrompt`, `UpdatePrompt`, `DeletePrompt`, and, for the UUIDs, `CountPromptUUID`,
`PromptsWithoutUUID`, and `SetPromptUUID`; `UpdatePrompt` leaves `purpose`, `starter_key`,
`created_at`, and `uuid` as they are. `insert` keeps a prompt's `UUID` when it is canonical and no
prompt has it (`uuidFor`), as one restored or imported is, and otherwise mints one; `Duplicate`'s
copy gets its own.

`Store` (`NewStore(db, c)`) runs them beside a `prefs.Store`; `WithTx(tx)` joins a
transaction, and `inTx` runs a step on the one a `Store` joined or else on its own, so a check and
the write it allows commit together. Writes stamp from the injected clock in UTC. `fromRow` makes a
`Prompt` whose maps are never nil, with `ExtraFlags` (each agent's arguments on one line), RFC 3339
times, and `Modified` for a starter's copy that no longer `matchesStarter` (its title, applies-to,
text, reply action, and settings; never its nickname, which `Reset` keeps when another prompt has
taken the starter's). `freeNickname` returns `base` when free, else `base 2`, `base 3`, and so on,
cut to 20 runes.

## Defaults (`defaults.go`, `store.go`)

The app's defaults are preferences holding a prompt's ID: `prompts.default.pr` and
`prompts.default.issue`, the kinds'; `prompts.default.checks_failing`, `.conflicts`,
`.changes_requested`, and `.rereview`, the situations'; and `prompts.default.reply.approve`,
`.approve.bots`, `.request_changes`, `.comment`, and `.nudge`, the default replies.
`prompts.seeded` and `prompts.seededSlots` (JSON lists) record the starters the library was given
and the situations it filled once. `DefaultSlot` names the eleven defaults (`DefaultSlots`: the
kinds', the situations', then the replies' under their `ReplySlot` names), a situation's under its
launcher `Situation`'s name.

`Store.Library` reads every prompt once with the defaults as they apply, which `Defaults` returns.
A kind's stored default that is gone, or no longer an agent prompt covering the kind, falls back to
the first agent prompt, by nickname, that covers it (`pickDefault`), so a prompt for repositories,
which covers no kind (`AppliesTo.Covers`), is never one. A situation's that is gone, or no longer
an agent prompt for pull requests, leaves the situation with none (`pickSituation`), so a pull
request in it launches with the prompt it would have anyway; a reply that no longer fits its slot
(`ReplySlot.fits`: a reply for the slot's review that applies to pull requests) leaves that review
empty (`pickReplies`). The setters check first: `SetDefault(kind, id)` takes an agent prompt that
covers the kind (`ErrNotAgentPrompt`, `ErrWrongKind`), `SetSlotDefault` a situation's slot alone
(`ErrNotSituation`) and an agent prompt for pull requests, and `SetReplyDefault` a known slot and a
reply that fits it (`ErrNotReply`, `ErrWrongKind`); the last two take 0 for none.

## Where a prompt is used, and deleting (`usage.go`)

`Usage` says where a prompt is a default and who runs it: `Defaults` (the kinds whose stored
default it is), `Slots` (the situations'), `Orgs` and `Repos` (the launch defaults naming it for a
kind), `SlotPlaces` (those naming it for a situation), `Kinds` (every kind it is a default for
anywhere, a situation's counting as a pull request's), `Replies`, and `Routines`. `loadUsage`
builds it from the stored IDs (`storedDefaults`, never `Library`'s fallbacks), the launch defaults'
`UsageSource.PromptUses` (each a `Use`), and the routines' `RoutineSource.RoutineUses` (each a
`RoutineUse`, naming a prompt by a starter's key, which names the user's copy of that starter
whatever it is called, or else by a nickname, ignoring case). `normalizeUsage` sorts the lists and
leaves none nil, and `describeUsage` words them ("a default for acme-org and 2 repositories").

`Usage.InUse` counts all but `Replies` and the derived `Kinds`. `Delete` first refuses a prompt a
routine runs (`refuseRoutineUse`; routines that can't be read refuse it with their error).
`Store.remove` then, in one transaction, clears each reply slot whose default the prompt is, refuses
it while it is a kind's or a situation's stored default, and deletes the row, which the launch
defaults' six `ON DELETE RESTRICT` foreign keys refuse while one names it. Each refusal is an
`InUseError` (matching `ErrPromptInUse`) whose `Usage` the service fills in, so its sentence says
where: `%q can't be deleted while it's %s; choose other defaults there first`, or `choose other
prompts there first` when a routine runs it, `%s` from `describeUsage`.

## Saving a prompt (`prompts.go`, `validate.go`)

`Create` and `Update` take an `Input`, the editor's fields with each agent's extra flags typed on
one line (`ExtraFlags`). `normalized` trims the names and turns `\r\n` into `\n`; an input that
isn't `PurposeReply` is an agent prompt, and a reply loses every launch setting and applies to pull
requests unless it is a comment reply that says otherwise (`replyAppliesTo`). `validate` checks it
inside the transaction that saves it:

- a title of 1 to 80 runes and a nickname of 1 to 20 that no other prompt has, ignoring case, each
  on one line (`checkName`, `checkNickname`);
- for a reply, one of the three actions, the one each default reply it is needs
  (`checkReplyAction`), and `pr`, `issue`, or `any`, covering pull requests while it is a default
  reply (`checkReplyAppliesTo`);
- for an agent prompt, one of the four applies-to values, covering every kind in `Usage.Kinds`
  (`checkAppliesTo`), and never switched to `repo` while a routine runs it (`checkRepoSwitch`); and
  its models, effort, and extra flags as `agent.CheckSettings` takes them, which returns what the
  row stores;
- a text that parses for its purpose (`parseFor`).

The problems come back together as an `agent.ValidationError` wrapping `ErrInvalidPrompt`, each an
`agent.FieldError` on `name`, `nickname`, `appliesTo`, `replyAction`, `body`, `effort`, `models` or
`extraFlags` for an agent the build doesn't know, or a per-agent field such as `models.codex`.
`Update` keeps the stored purpose, and reads the usage it checks against before its transaction
opens.

`Duplicate` copies a prompt as the user's own (no starter key), titled `<title> (copy)`, under
`freeNickname("<nickname> copy")`. `Reset` gives a starter's copy its shipped title, text, and
settings (`ErrNotStarter` for any other), and its nickname unless another prompt has it, checked as
`Update` checks. Every change to the library ends in `Service.saved` (see
[The copy beside the database](#the-copy-beside-the-database-libraryfilego)).

## Templates (`data.go`, `template.go`)

A prompt is a Go `text/template` over `Data`, whose 42 fields are its variables. `Variables` lists
them in field order (`TestVariablesMatchData`), each with its `Group` (Item, Repository, Pull
request, Session, or Autopilot), `Type`, `PROnly` (empty for an issue), and `ItemOnly` (empty for a
repository's launch: all but the ten `repoVariables`). `Data` holds an item's address and short
facts, never its description or comments. `.Routine`, `.Trigger`, `.Attempt`, and `.Unattended`
are filled in by a routine's launch alone (the launcher's `withRoutine`, and `.Unattended` in the
background). `Sample(kind)` and `SampleRepo` (acme-org/example-repo) are made-up data for checks
and previews, and `RepoData` a repository's launch's, with no item.
`WithSession(inWorktree, atPRHead)` sets an item's session (`.Worktree` as `item.WorktreeName`
names it, such as `pr-7`), `WithWorktree(name)` a repository's launch's own, and `WithUpdate` a
bot's pull request's dependency fields (`bump.Parse`).

Every fill sanitizes first (`Data.sanitized`): each text becomes one plain line
(`textutil.OneLine`); `.Title`, `.Dependency`, `.FromVersion`, and `.ToVersion` are cut to 200
runes, `.Labels` and `.FailedChecks` to 300, `.SensitiveFiles` to 1,000, `.Remote` and
`.HeadCommit` to 100, and `.Routine` and `.Trigger` to 80; `.Attempt` is held between 0 and 100;
and `.ReviewedCommit` survives only as a commit's ID (`CommitID`: 7 to 64 lower-case hex digits).
`.BranchIsPlain` and `.RemoteIsPlain` are worked out from the values as given, and hold only for a
`plainName` (`textutil.ShellSafe`, not starting with `-`) that sanitizing left unchanged, so a name
with a hidden character never passes for the one it shows as. `TestEveryDataTextIsSanitized` fails
on any text of `Data` that `sanitized` leaves dirty.

`Template.Execute(data)` fills a checked template into a buffer that refuses more than 64 KiB
(`ErrPromptTooLong`), then `tidy`s it: trailing spaces trimmed, runs of blank lines collapsed to
one, blank lines at either end dropped, and one closing newline. An agent prompt that comes out
empty (`ErrEmptyPrompt`) or starts with `-` (`ErrLeadingDash`: it is the agent's first argument) is
refused; a reply may be either. `Render` adds `Notice` to an agent prompt: a line for an outside
contributor's item, or else a bot's, whose text can quote other projects (`trustNotice`), then one
naming a pull request's sensitive files (`sensitiveNotice`), so no prompt can leave them out. The
template has `missingkey=error` and its own `printf`, `print`, and `println`, each refusing a
result over the 64 KiB cap (and `printf` a width or precision over 999 or taken with `*`,
`ErrPrintfWidth`), and `shellquote` (`textutil.ShellWord`, capped alike), so no nesting of them
grows without limit.

## Checking a template (`check.go`, `walk.go`)

`Parse(body, appliesTo)` and `ParseReply` (the same checker in reply mode) return the `*Template`,
nil when any `Diagnostic` is an error, and every `Diagnostic`: a `SeverityError` or
`SeverityWarning`, its 1-based `Line`, its `Column` and `Length` in UTF-16 code units as the editor
indexes text (all 0 for the whole prompt), and a sentence saying how to fix it. `checker.check`
takes, in order:

1. the raw text: something there, valid UTF-8 with no NUL, at most 20,000 runes (`checkText`);
2. Go's parse, its errors reworded and placed (`parseError`, `explainParseError`), an unclosed
   block at the innermost one left open;
3. a walk of the tree (`walkNode`), refusing `{{range}}` and `{{template}}`, a variable `Data`
   lacks (suggesting the nearest, `suggest`), a dotted name past a variable, `{{.}}` where the dot
   is the data, a variable inside `{{with}}` without `$.`, and every function but `and`, `or`,
   `not`, `eq`, `ne`, `lt`, `le`, `gt`, `ge`, `len`, `printf`, `print`, `println`, and `shellquote`
   (`allowedFuncs`);
4. `{{define}}` and `{{block}}`, refused (`checkDefinitions`), which leaves nothing that loops;
5. for an agent prompt, its start (`checkStart`): text starting with `-` is an error, and a value
   printed first a warning;
6. `trySamples`, which fills it in for all it applies to (`samplesFor`): each kind it covers, as
   `Sample(kind)` and with every yes/no flipped and the optional text emptied, in the clone and in a
   worktree (a pull request's also at its head); or `SampleRepo`, with and without a default branch
   and remote, in the clone and in `deps-20260115`. A failure is placed where Go reports it
   (`execError`), so an error that shows only when a prompt runs, in the branches the samples take,
   is caught.

Warnings never block saving: a pull-request-only variable in a prompt for issues, an item's
variable in a prompt for repositories, a session variable (`.InWorktree`, `.WorktreeOnPRHead`,
`.Worktree`, `.Profile`) in a reply, and a value that starts the prompt. `Prompt.Template()` parses
a stored prompt for its purpose, failing with `ErrInvalidTemplate` and the first error, which is
how a launch and a sheet's reply refuse a prompt imported with problems.

## The editor's check and preview (`preview.go`)

`Check(CheckRequest)` checks the editor's `Input` as `Create` or `Update` would (`validateFields`,
with the prompt's `Usage` when it has an ID, and `parseFor`), saving nothing, and fills it in for
the preview: an item's `Data` from the `PreviewSource` (the launcher's `NewPromptSource`: the item
as a launch would fill it in, with the profile its launch would resolve to and no session facts), in
the request's session (`InWorktree`, `AtPRHead`); or, with no item, the sample of the kind it
applies to; for an item gone or of a kind the prompt isn't for, the same sample, saying why in
`PreviewNote`. A prompt for repositories previews with `SampleRepo` alone, in the clone or in
`deps-20260115` (`repoPreviewData`). `CheckResult` adds to the problems the preview (`Execute`), the
`Notice` an agent prompt would end with, and each variable's value (`Values`).

## The starters (`starters.go`, `starters_routines.go`, `starters_repo.go`)

`Starters()` lists the 18 a build ships, in the order `Seed` adds them, each a `Starter` with its
key (its copy's `starter_key`), title, nickname, applies-to, text, and settings:

- `builtStarters`, the launcher's and the sheets': `review-pr`, `work-on-issue`, `quick-look` (set
  to Claude Code's `sonnet` at `low` effort without a worktree), `fix-checks`, the replies
  `reply-lgtm`, `reply-update-ok`, `reply-needs-changes`, and `reply-question`,
  `fix-dependency-security`, `re-review`, and the reply `reply-nudge`;
- `routineStarters`, the ones Autopilot's templates run: `heal-dependabot-pr`, `address-review`,
  `resolve-conflicts`, `rerun-failed-jobs` (`haiku` at `low` effort), and `reply-needs-repro`, a
  comment reply for issues;
- `repoStarters`: `deps-minor` and `deps-major`, out of plan mode in a worktree.

That is twelve agent prompts and six replies. A starter reply's `Slot` is the default it becomes
while that slot has none: LGTM `approve`, Update OK `approve_bots`, Needs changes
`request_changes`, Question `comment`, and Nudge `nudge`; Needs repro fills none.

The agent starters for items pass an item's address and title, never its body, start with a word,
and tell the agent to treat the item's words as information; the two for repositories name no item's
value. Shared pieces keep the commands alike, `worktreeAtHead` and `pushOnce` among them (one push
to the pull request's own branch by its full ref, never forced, and none for an outside
contributor's pull request or a branch or remote not plain). `TestStarterCommandsAreInert` fills
each agent starter with hostile values in every session and trust it can launch in, and requires
every command in its backticks to read as plain shell words, so a value someone else chose reaches a
command only through `shellquote`, and only when plain. `TestEveryStarterIsClean` requires each to
parse with no diagnostic and fill in for every sample. Golden files under `testdata/` pin Review and
Work on it (the launcher's original prompts, byte for byte), Fix dep security, and the two for
repositories; `-update` rewrites them.

A change to a starter's text keeps the text it replaces in `Earlier`, byte for byte, each pinned by
its SHA-256 in `TestEarlierStarterTextsAreAsShipped` (only `fix-dependency-security` has any, two).
`upToDate` gives a copy still holding one this build's text, leaving its title, nickname, and
settings as the user made them; `Seed` does it to the stored copies, and an import to a file's.

## Seeding (`seed.go`, `addstarter.go`)

`Seed`, which core runs at every start once the restore is done, does this in one
transaction, or none of it when a write fails:

1. gives every prompt without a UUID one (`giveUUIDs`), as a build from before `0116_record_uuids`
   still running inserts them;
2. adds every starter whose key `prompts.seeded` lacks (`seedMissing`), under a free nickname with
   its settings as shipped, making Review the default for pull requests and Work on it the one for
   issues where none is stored, and each starter reply its slot's default where the slot has none,
   and records the keys, so a starter deleted after it was added never comes back;
3. brings every copy still on an `Earlier` text up to date (`refreshStarters`);
4. fills each situation's slot not in `prompts.seededSlots` once (`seedSlots`), with the library's
   copy of the slot's starter (`DefaultSlot.starter`: Fix checks, Resolve conflicts, Address
   review, Re-review) when it holds one that fits and the slot has none, recording the slot either
   way, so one the user cleared, or whose starter they deleted, stays as they left it.

`AddStarter(key)`, bound, adds one starter back on the user's word, the way an Autopilot template
whose prompt was deleted brings it back: it returns the library's copy when there is one, else adds
the starter as `seedStarter` does (a kind's or a reply slot's default only where none is stored,
never a situation's), records its key, and returns the copy. A key this build doesn't ship is
`ErrUnknownStarter`, worded.

## The file format (`bundle.go`)

`Bundle` is the one portable form of prompts, specified in [Prompt files](../prompt-files.md): an
export, an import's file, `prompts.json`, a backup's prompts section, and the prompts an Autopilot
export carries. It names prompts and defaults by nickname, never by ID (`BundleDefaults`), and
writes every field; each prompt carries its `uuid` too (`omitempty`, so a file an older build
wrote has none, which adding the field didn't need a new version for). `BundleFormat` is
`brand.Slug + ".prompts"`; `BundleVersion` is 4, raised only
for a change an older build would misread (2: each agent's settings; 3: replies; 4: prompts for
repositories), and `BundlePrompt.UnmarshalJSON` reads version 1's single `model` and `extraArgs`
list as Claude Code's. `DecodeBundle` refuses more than 8 MiB or 1,000 prompts
(`ErrBundleTooLarge`), a file that isn't one (`ErrNotBundle`), and a newer version
(`ErrNewerBundle`), ignores unknown fields, and never returns a nil list. `newBundle` writes the
whole library with its `records` (`startersAdded`, `slotsSeeded`), or the chosen prompts without
them, each with the defaults it is.

## The copy beside the database (`libraryfile.go`)

`LibraryFile` is `prompts.json` (`appdata.Paths.Prompts`). `Save` writes the whole library through
`appdata.WriteFile` (a temporary file renamed over it, so a reader sees the old file or the new one
whole), one `Save` at a time under its mutex, so the file ends up holding the latest library.
`Service.saved` runs it after every change (each edit, setter, and `Delete` that went through,
`AddStarter`, an `Import` that changed something, and a backup's import), on a context that
outlives the caller's, logging a failure rather than returning it (the next change writes the file
again), then tells `Config.Changed`, which core points at `kept.saveDefaults`, the rewrite of
`launch-defaults.json`, which names prompts by nickname. `Seed` writes no copy: core's `kept.keep`
writes `prompts.json` at startup only when it is missing or the library was just restored, so a
database that came back without its prompts never overwrites the copy that has them.

`Restore(ctx, st, setAside, file)` runs only on a library that doesn't exist yet (`libraryExists`:
no `prompts.seeded` and no prompt), before `Seed`. It reads the database set aside at startup when
there is one (`readSnapshot`), and otherwise, or when that can't be read, the library file (one that
isn't there is no error). In one transaction (`restoreBundle`) it stores every prompt, cleaned as an
import cleans it (`cleanImport`) and renamed where two share a nickname, through the import's
planner in its `faithful` mode (nothing skipped or saved over; a second copy of a starter stored as
the user's own), the source's `startersAdded` with each starter it stored, its `slotsSeeded` (none
from a file without them, so `Seed` fills each situation once), and its defaults where their prompts
fit (`restoreDefaults`). `Restored` says how many came back and from where, with the ID each
nickname got (`IDs`) and, from a database, each old ID's nickname (`Nicknames`), which core's
`kept.restoreDefaults` matches the launch defaults to.

## Export, import, and restore from a backup (`transfer.go`, `importer.go`)

`Export(path, ids)` writes the prompts with `ids`, or all of them, to an absolute path, counting
the agent prompts and the replies (`ExportResult`). `Backups()` lists, newest first,
`prompts.json` (`BackupLibrary`) and the databases `store.Snapshots` finds beside the database
(`BackupSetAside`, `BackupReset`, and `BackupUpdate`, taken before migrations), each with how many
prompts it holds and how many an import would add or save over (`New`), or why it can't be read.
`PreviewImport(ImportSource)` reads a file the user chose (`SourceFile`: a prompt file or,
by `store.IsDatabase`, a database; one that isn't a regular file, such as a named pipe, is refused
unread) or one of those copies (`SourceBackup`, else `ErrUnknownBackup`), and returns each
prompt's `ImportEntry` and the `DefaultOffer`s, changing nothing. `Import(prompts, defaults)`
stores the prompts it is handed and the defaults it names, all or none (`importInto`, then
`restoreDefaults`, in one transaction), and counts them (`ImportResult`). **Restore from a
backup** is this import, never the startup's `Restore`.

A database is read through `store.OpenSnapshot`, read-only, whichever build wrote it
(`readSnapshot`): its `prompts` rows by `promptColumns`, a column it lacks taking its default and
the older single `model` and `extra_args` read as Claude Code's (`SnapshotModels`, `SnapshotArgs`,
which the launch defaults' snapshot read shares), and its preferences (`snapshotRecords`: the
defaults by the nicknames of the IDs they hold, and the two records), none when they can't be read.

The planner (`planner.plan`) decides from the library it was given, one prompt at a time, each as
though those before it were in, so a preview and the import after it agree. `cleanImport` first
makes the prompt one this build would store, noting each change on the entry: names on one line,
cut to 80 and 20 runes, one missing filled from the other (else "Imported prompt" and "Imported");
a starter key not matching `^[a-z0-9][a-z0-9-]{0,39}$` dropped; a starter's `Earlier` text read as
its text now; a `uuid` that isn't a canonical, non-Nil UUID dropped; for an agent prompt, an
unknown applies-to read as `any`, and each model or flag for
an agent this build doesn't know, a model that isn't one word, flags `agent.CheckOverrides`
refuses, and an effort `agent.ValidateEffortAny` refuses left out; for a reply, every setting
dropped, an unknown action read as `comment`, and `repo` or an unknown applies-to read as `pr`. A
text with problems is kept, with a note. Then the `ImportOutcome`:

- `skip` when the library holds the prompt already: the same title, text, and settings under any
  nickname (`sameContent`), or, for a starter exactly as this build ships it, its copy
  (`untouchedImport`);
- `replace` when the library's copy of its starter is still as added (`untouchedStored`), not yet
  saved over in this import, and the prompt can take its place (`takesPlace`: it covers every kind
  the copy is a default for, fits every reply slot it is, and doesn't make a copy a routine runs a
  prompt for repositories); the copy keeps its ID and starter key, so it stays a default wherever it
  was and `Reset` still restores the shipped text;
- `add`, or `rename` under a free nickname such as "Review 2".

An added prompt keeps its starter key only while the library has no copy of that starter, and each
key stored joins `prompts.seeded` (`recordStarters`). It keeps the file's UUID only while no prompt
in the library, the ones planned before it included, has it (`freeUUID`), else the store mints one;
a faithful restore so brings each prompt back under its UUID. A prompt saved over a starter's copy
keeps the copy's UUID, so what names the copy still finds it. `importInto` returns the ID each of
the
source's nicknames ended up under (the first of two that share one, the holder's for a skip), which
`restoreDefaults` and a backup's launch defaults find their prompts by. `offers` lists each of the
source's defaults whose prompt, as stored, fits the slot and isn't its default already.

## A backup's prompts (`backup.go`)

`BackupPart` (`NewBackupPart`) is the library's part of a backup ([`internal/backup`](backup.md)),
which core's `backupPrompts` adapts to `backup.Part`: `Bundle` exports the whole library;
`PlanBackup(b, mode, clears)` works the section out, changing nothing; `ApplyBackup` works it out
again inside the import's transaction and fills in the `Links`; `BackupDone` runs `Service.saved`
once it commits; and `BackupPlan.Prompts` tells the launch defaults' part what each of the file's
nicknames will be (`backup.PromptKind`). Under Merge (`planMerge`, `applyMerge`) the section is an
`Import`, the file's defaults included. Under Replace (`planReplace`, `applyReplace`) each of the
file's prompts is saved over the first stored prompt not yet matched with its nickname, ignoring
case, and its purpose, keeping the stored prompt's UUID, or else added under a free nickname and
the file's UUID where none of the prompts it keeps has it (`nameAdded`). Every other stored prompt
is deleted
first, freeing its nickname, except one a launch default names for a kind, which is kept with a
note unless the import replaces the launch defaults too (`clears`), and any other a foreign key
refuses (`removeAll`). Every default becomes the file's where its prompt fits, else none, a kind's
falling back as `Library` does (`replaceDefaults`).

## Renames, and Autopilot (`routinebundle.go`)

A routine names each prompt it runs by the prompt's UUID, beside a starter's key or a nickname,
its fallback where no prompt has the UUID (as on another Mac). The library learns of them through
`Config.Routines` (`RoutineSource`): each `RoutineUse` resolves to one prompt (`usedPrompt`: its UUID
first, then the user's copy of its starter, then its nickname, ignoring case), so its `Usage` names
the routines that run it whatever it is called now, `Delete` refuses a prompt one runs,
`checkRepoSwitch` refuses to make it one for repositories, and an import never makes a starter's
copy one runs a prompt for repositories. Every change to the library, whatever made it (an edit,
`Reset`, an import or a backup saving over a starter's copy, a delete), tells `Config.Changed`
after the library file is written, which core points at the launch defaults' copy and at
Autopilot's `Follower.Follow` (`internal/autopilot/follow.go`): it stamps every routine with the
UUID and the nickname now of each prompt it names, so a reset or an import that renames a prompt
never breaks a routine. `Update` also tells `Config.Renamed(from, to)` when the nickname changed
(compared exactly, so a change of case counts), once the change is stored and before the library
file and `Changed`, which `Follow` replays on a routine that names the prompt by its old nickname
alone, as one stored before UUIDs; a starter's key follows its prompt on its own. Core logs a
failure; the routine still finds its prompt by UUID, and the next change or start writes its
name (see [The routines, kept](autopilot/routines-kept.md)).

`RoutinePrompts` (`NewRoutinePrompts`), never bound, carries the prompts an Autopilot export names
(core's `routinePromptTransfer`; see [Easy mode](autopilot/easy-mode.md)): `Bundled(nicknames)`
returns those prompts, ignoring case, without the defaults or records; `Arrivals` says whether each
is new (no prompt here has its UUID or its nickname, the UUID looked at first: `arrivalMatch`), the
same (`sameContent`), or different, the copy here kept; and `AddNew` hands the new ones to
`Import`, with no defaults, so a prompt here under its UUID is never added again, whatever either
is called now. Core's other Autopilot
adapters (`promptLibrary` for a routine's checks, `starterNames`, `promptIDOf` for an agent step,
and `githubSteps.reply`) read the `Store` directly, and find a routine's prompt as the draft does,
its UUID first (`draft.PromptIndex`).

## How other packages reach it

- **The launcher** reads the library through its own `PromptReader` (`Library`), drops the
  replies, picks a launch's prompt as [`internal/launcher`](launcher.md) describes, parses it again
  at every launch (`Prompt.Template`), and fills it in with `Render` (`fillIn`, and `fillRepo` for
  a repository's). `launchItemRow.promptData` builds an item's `Data`, and `NewPromptSource` is the
  editor's `PreviewSource`.
- **The sheets' replies** go through core's `replyFiller`, which refuses a prompt that isn't a
  reply or doesn't apply to the item's kind, fills it in with `Execute` (no notice) from the same
  `PreviewSource`, and trims the closing newline. `reviewReplies` offers the review sheet the
  replies for an action that apply to pull requests, with the defaults (a bot's approval falling
  back to the person's), and `issuesReplies` the issue sheet the comment replies for issues, none a
  default; a GitHub step's comment uses the same filler. Neither sheet imports this package
  (`internal/repoguard/deps_test.go`).
- **The launch defaults** implement `UsageSource` (`Store.PromptUses`), look prompts up through
  their own `PromptLookup` (`Store.Lookup`), and keep `launch-defaults.json`, which `Changed`
  rewrites.
- **The backup** reaches it only through `BackupPart`; `internal/backup` may not import it, and it
  imports backup's words (`backup.Plan`, `backup.Mode`, `backup.Count`).
- **Describe it** gets names, never text (core's `kitPrompt`, `kitStarter`), without the prompts
  for repositories and their starters (`itemPrompts`, `itemStarters`), and the `Variables`.
- **`cmd/nilbox`** binds the `Service` (`binders`): `List`, `Create`, `Update`, `Delete`,
  `Duplicate`, `Reset`, `Defaults`, `SetDefault`, `SetSlotDefault`, `SetReplyDefault`, `Variables`,
  `Check`, `Export`, `Backups`, `PreviewImport`, `Import`, and `AddStarter`.
