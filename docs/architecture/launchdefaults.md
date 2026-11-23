# internal/launchdefaults

[Docs](../README.md) › [Architecture](../README.md#architecture) · Code: [`internal/launchdefaults`](../../internal/launchdefaults) · Guide: [Launch defaults](../guide/launch-defaults.md), [Outside contributors](../guide/outside-contributors.md)

`internal/launchdefaults/` is the owner of the launch defaults of orgs and repositories (the
`launch_defaults` table) and of their copy beside the database, `launch-defaults.json`. A row sets
what launches on an org's or a repository's items start from: the coding agent; each agent's
profile, model, and extra flags; the default prompt for pull requests, for issues, and for each of
the four situations a pull request can be in; the effort, plan mode, and worktree, which every
agent shares; and whether Autopilot may run agents on outside contributors' items there. The
package stores and checks the rows; the launcher resolves them ([internal/launcher](launcher.md)),
and **Settings → Repositories → Launch defaults** edits them
([Launch defaults](../guide/launch-defaults.md)). It uses `agent`, `prompts`, and `profiles` for
their types and checks, and imports `backup` only for the words of an import's plan. The window
binds its `Service`.

## A row and what it inherits

A row with `Name` `""` holds an owner's defaults, which every repository of that owner inherits; a
row with a name holds one repository's, which win over its owner's. Rows are keyed by owner and name
rather than by the inbox's repository rows, so an org's defaults need no repository the inbox has
seen and outlive the inbox's cache. `Defaults` is a row as the window reads it, and `Input` one as
the editor saves it. Every setting inherits when it is `""`, nil, or has no entry for the agent, so
a row holds only what differs:

- `Agent`, an `agent.ID`, where `agent.Inherit` (`""`) takes the app's default agent;
- `Profiles` and `Models`, maps of `agent.ID` to a profile's name and to a model;
- the extra flags: `Input.ExtraFlags`, each agent's arguments typed on one line and quoted as in a
  shell, stored as `ExtraArgs`, a map of `agent.ID` to a list of arguments, which `Defaults` also
  carries joined on one line (`ExtraFlags`, `agent.JoinArgs`);
- `PRPromptID` and `IssuePromptID`, the kinds' default prompts, and `ChecksPromptID`,
  `ConflictsPromptID`, `ChangesPromptID`, and `RereviewPromptID`, the situations' (below), each a
  `*int64`;
- `Effort`, and `PlanMode` and `Worktree` (each a `*bool`), shared by every agent: an agent that
  lacks the effort level launches with its own default;
- `AutopilotOutside`, an `Outside` (below).

`Defaults.UpdatedAt` is when the row was last saved, as RFC 3339, and `Target` names it as
`acme-org` or `acme-org/example-repo`. `Input.Empty` reports an input where every setting inherits,
the outside contributors' choice included, so that choice alone is a row worth keeping
(`TestTheOutsideContributorsChoiceIsALaunchDefaultOfItsOwn`).

## Saving, checking, and listing

`NewService(Config)` takes a `Store`, a `ProfileLookup` (`*profiles.Store`), and a `PromptLookup`
(`*prompts.Store`), which are required; a `RepoSource`, needed only by `Targets`; and a
`CloneSource`, a `File`, and a `Logger`, which are optional. The bound `Service` has five methods.
`List` returns every row, by owner and then repository ignoring case, each owner's own row first.
`Check` validates an `Input` as `Save` would, without saving it, and returns its
`agent.FieldError`s, never nil.

`Save` trims the owner, the name, the agent, the outside contributors' choice, and each agent's
entry, dropping the blank ones (`normalized`; the effort is checked as given), then checks the input
(`validate`), in this order: the owner (`repo.ValidOwner`); the name (`repo.ValidRepo`, or `""`);
the agent (`agent.Known`, or `agent.Inherit`); the outside contributors' choice; each agent's
profile, by agent ID, through `ProfileLookup`; the six prompts, through `PromptLookup`, where one
that is gone, a review reply, or one that doesn't cover the kind is refused, and a situation's must
cover pull requests; and last the models, the effort, and the flags (`agent.CheckSettings`, the
check the prompt editor uses too). Every problem comes back at once, in that order, as an
`*agent.ValidationError` wrapping `ErrInvalidDefaults`
(`TestValidationErrorListsEveryProblemInOrder`), each on its field's JSON name (`FieldOwner`,
`FieldProfiles`, `FieldChecksPromptID`, ...), a per-agent one through `agent.FieldFor`
(`profiles.claude`, `models.codex`). A lookup that fails for any other reason is a plain error. An
input that sets nothing removes the target's row instead, when it has one, and returns the empty
`Defaults` (`TestSaveWithNothingSetRemoves`); any other replaces the target's row whole. `Remove`
deletes an org's (name `""`) or a repository's row, and is `ErrNotFound` when none is stored. The
form saves through `Save`, and so does the launch menu's **Save these choices as the default for**,
whose undo is a `Remove`, or a `Save` of the row it replaced (`LaunchControls.tsx`).

`Targets` lists what the form offers to add defaults for, and what the pinned and muted lists offer
(`RepoRules.tsx`): the repositories the inbox has seen that aren't archived (`RepoSource`, core's
`inboxRepos` over `inbox.Store.RepoCounts`, each with how many of its items the inbox lists now)
and the repositories the clones in the repository locations have as their origin (`CloneSource`,
`*scripts.Service`'s `RepoNames`), each owner before its repositories, by owner and then name
ignoring case. It asks GitHub nothing. A repository both have is listed once, as the inbox spells
it, and marked `Cloned`; a clone's is listed whatever GitHub says of it. An owner's `Items` adds up
its repositories', it is `Cloned` when one of them is, and it is spelled as the first of its
repositories the inbox has, else as its first. A failure to list the inbox's repositories is an
error, while clones that can't be listed are logged and left out, so a location that can't be read
never empties the list; only a caller that has gone gets that error.

## The store and its queries

`Store` reads and writes `launch_defaults`, which no other package touches (`tableOwners`). A
`Store` from `NewStore(db, clock)` runs each call on the pool, and `WithTx(tx)` returns one that
runs every call inside an open transaction, as a restore and a backup's import do. `All` returns
every row, and `PromptUses` every place a row names a prompt, each a `prompts.Use` (the prompt, the
kind, the target, and its `prompts.DefaultSlot`: `DefaultSlotPR`, `DefaultSlotIssue`, or a
situation's), which makes the `Store` a `prompts.UsageSource`. Saving and removing are unexported,
so only the package writes: `Service`, `Restore`, and `BackupPart`. The queries, in
`internal/launchdefaults/internal/queries/launchdefaults.sql`:

- `ListLaunchDefaults`, every row by `owner COLLATE NOCASE` and then `name COLLATE NOCASE`, so an
  owner's own row (name `''`) comes before its repositories';
- `ListPromptUses`, the owner, name, and six prompt columns of every row that names a prompt, in
  the same order;
- `SaveLaunchDefaults`, an upsert `ON CONFLICT (owner, name)`: a row stored for the same target,
  matched ignoring case since both columns are `COLLATE NOCASE`, has every setting replaced and
  `updated_at` stamped, and keeps the owner, name, and `created_at` it was first saved with
  (`TestSaveReplacesTheRowIgnoringCase`); the stored row comes back through `RETURNING`;
- `DeleteLaunchDefaults` (`:execrows`), one target's row, ignoring case;
- `DeleteAllLaunchDefaults`, every row, for an import that replaces them.

The table is in the base migration `0100_settings.sql`: `owner` and `name` (`COLLATE NOCASE`,
`UNIQUE (owner, name)`, `name` `''` by default); `agent` and `effort` (`''` inherits); `plan_mode`
and `worktree` (`BOOLEAN`, NULL inherits); `profiles`, `models`, and `agent_args` (TEXT, each
checked to be a JSON object, `{}` by default); `autopilot_outside` (NULL, `'off'`, or `'on'`); the
six prompt columns, `pr_prompt_id`, `issue_prompt_id`, `checks_prompt_id`, `conflicts_prompt_id`,
`changes_prompt_id`, and `rereview_prompt_id`; and `created_at` and `updated_at`, which a save
stamps from the injected `clock.Clock` in UTC. Each prompt column references `prompts (id)` `ON
DELETE RESTRICT` and has an index of its own, so a prompt that is a default anywhere can't be
deleted: the prompts service's `Delete` fails with `prompts.ErrPromptInUse`, a
`*prompts.InUseError` naming the orgs and repositories
(`TestDefaultPromptsCannotBeDeletedWhileUsed`, `TestASituationsPromptCannotBeDeletedWhileUsed`).
Those six references are why `0111_repo_prompts` sets the columns aside while it rebuilds `prompts`
([internal/store](store.md#rebuilding-a-table)). `fromRow` decodes the three JSON columns into maps
that are never nil, joins each agent's arguments into `ExtraFlags`, and reads an
`autopilot_outside` this build doesn't know as inherit (`outsideOf`); a column that doesn't decode
is an error naming the target.

## How a launch resolves them

The launcher reads every row on each resolution (`DefaultsReader`, the `Store`'s `All`, in
`loadWorld`) and indexes them with `NewIndex`, keyed by `repo.Name.Key` (`owner/name` in lower
case, an owner alone as `owner/`). `Index.For` gives a repository's org row and its own row, each
nil when none is stored, and the launcher's `layers` lists them most specific first. The order is
fixed in `internal/launcher/resolve.go`. The agent comes first (`resolveAgent`): the launch menu's,
else the repository's row, the org's, the app's default agent (`LaunchSettings.DefaultAgent`), and
last `agent.Default()`, Claude Code. Every per-agent setting is then that agent's entry:

- the profile (`profile`, and below the launch menu's `inheritedProfile`): the launch menu's, the
  repository's, the org's, the app's default profile for the agent
  (`LaunchSettings.DefaultProfiles`), the agent's `default`, else its first stored by name;
- the prompt (`prompt`, and below the launch menu's `inheritedPrompt`): the launch menu's; else, for
  a pull request's situation, that situation's (below); else the repository's, the org's, or the
  app's prompt for the item's kind;
- the model, the effort, and plan mode (`text`, `toggle`): the launch menu's, the chosen prompt's,
  the repository's, the org's, then the profile's, and for a model or an effort the profile leaves
  blank, the agent's own default. A resolved effort level the agent lacks is dropped with a
  warning, and an agent that takes the effort only as a config override
  (`agent.Agent.ConfigOverrides`, Codex) is passed none unless `LaunchSettings.EffortOverrides` is
  on;
- the worktree: the launch menu's, the prompt's, the repository's, the org's, else the app's
  `WorktreeDefault`;
- the extra flags (`extraArgs`) add up instead: the profile's, the org's, the repository's, then the
  prompt's. The org's, the repository's, and the prompt's lists are each checked again with
  `agent.CheckExtra`, in case the database was edited by hand, and then the whole list with the
  profile's (a list that fails stops the resolution with an error, rather than being skipped), since
  an option that ends one layer would take the next layer's first word as its value.

A stored value that no longer makes sense (an agent this build doesn't know, a profile or prompt
that is gone, a prompt that doesn't cover the kind) is skipped with a warning in
`Resolution.Warnings`, and the next layer answers. A repository's launch (`ResolveRepo`) reads the
repository's and its org's rows the same way, but takes only the prompt it names and always makes
a worktree. The guide gives the same order in the user's words.

## A situation's prompt

The four situation columns hold the default prompt for a pull request whose checks fail, that
conflicts with its base, that a reviewer asked to change, and that the user is asked to review
again. The launcher's `Situation` (`internal/launcher/situation.go`: `SituationChecksFailing`,
`SituationConflicts`, `SituationChangesRequested`, and `SituationRereview`, each named as its
`prompts.DefaultSlot`) picks the column (`promptID`). A launch for one (`LaunchRequest.Situation`,
ignored on an issue) takes the repository's prompt for it, else the org's, else the app's
(`prompts.DefaultPrompts.Slot`, the pref `prompts.default.<slot>`), and with none of those set the
pull request's own chain above (`inheritedPrompt`, which asks `situationPrompt` first), so an org's
prompt for pull requests never beats the app's for a situation. A situation's prompt is a pull
request's: `validate` refuses one that doesn't cover pull requests on `checksPromptId`,
`conflictsPromptId`, `changesPromptId`, or `rereviewPromptId` (`TestValidationOfASituationsPrompt`),
and the launcher skips a stored one that no longer does, with a warning. `Resolution.Situations`
resolves all four for a pull request, only to label the card's agent button. The copy and a backup
leave a situation's prompt out when it inherits, so an older build's file, which has none, inherits
all four (`TestAnOlderCopyWithoutTheSituationsPromptsInherits`).

## Autopilot on outside contributors' items

`Outside` is a row's choice of whether an Autopilot routine may run a coding agent on an outside
contributor's item there: `OutsideInherit` (`""`, stored as NULL), `OutsideOff` (`off`), or
`OutsideOn` (`on`). `Outside.Valid` accepts those three, `validate` refuses any other on
`autopilotOutside`, and a stored value this build doesn't know reads as inherit.
`Index.OutsideAllowed` is the one rule: a repository's own choice wins over its org's, a row whose
choice inherits takes its org's, and with neither it is off, since there is no app-wide switch. It
also says where the choice was made, `"repo"`, `"org"`, or `""` for nowhere
(`TestARepositorysOutsideContributorsChoiceWinsOverItsOrgs`). On still means plan mode: a routine's
launch ignores `FullAccessOverride` ([internal/launcher](launcher.md)), so the outside
contributor's rule stays in force.

Autopilot reaches the rule through core's `outsideRules` (`internal/core/adapters_agents.go`, an
`autopilot.OutsideRules`), which reads every row with `Store.All`, indexes them, and answers through
`outsideIndex.Allowed`. The engine reads it once a cycle (`Engine.outsideIndex`; one that can't be
read allows agents nowhere, logged), and `outsideRule` (`advance_gates.go`) closes an agent step's
match on such an item where it is off as skipped, saying *Autopilot can act on these in acme-org's
launch defaults.* (`outsideWords`, naming the item's owner). See
[The start path](autopilot/agents-in-the-terminal.md#the-start-path) and [Autopilot and outside
contributors](../guide/outside-contributors.md#autopilot-and-outside-contributors).

## The copy beside the database

`File` is the copy of every row in a plain file beside the database (`appdata.Paths.LaunchDefaults`,
`launch-defaults.json`), which core builds with `NewFile(path, version, clock)`. The database stays
the rows' home. `File.Save` reads every row and writes the file whole through `appdata.WriteFile`
(a user-only temporary file beside it, synced, then renamed over it), one `Save` at a time (`mu`),
so the file always ends up holding the latest rows. It is written:

- after every `Service.Save` and `Remove` (`saved`, under `context.WithoutCancel`, so a caller that
  has gone doesn't stop it); a failure is logged, never returned, since the change is stored and
  the next one writes the copy again (`TestACopyThatCannotBeWrittenIsLoggedNotFailed`), and a save
  the checks refuse writes nothing (`TestARefusedChangeLeavesTheCopy`);
- after every change to the prompt library, since the copy names prompts by nickname and one may
  have been renamed (`prompts.Config.Changed`, core's `kept.saveDefaults`);
- once a backup's launch defaults are committed (`BackupPart.BackupDone`);
- at startup, only when the file is missing or the prompt library was just brought back
  (`kept.keep`), so a database that came back without the user's rows never overwrites a copy that
  still holds them.

The format is a document of `Kept` rows (`EncodeKept`: `format`, `version`, `savedAt` as RFC 3339,
`app`, and `defaults`, as indented JSON ending in a newline, HTML left unescaped), named by
`KeptFormat` (`<slug>.launch-defaults`) and `KeptVersion`, 2. A `Kept` names its six default
prompts by nickname (`prPrompt`, `issuePrompt`, `checksPrompt`, `conflictsPrompt`, `changesPrompt`,
`rereviewPrompt`) rather than by ID, so it can be matched to a library brought back under new IDs;
a prompt that is gone is written as `""`, which inherits. It keeps `profiles`, `models`, and
`extraArgs` per agent (lists, not a line), and leaves `autopilotOutside` and the four situations'
prompts out when they inherit. `DecodeKept` reads any version up to `KeptVersion`: another `format`
or a version below 1 is `ErrNotKept`, a newer version `ErrNewerKept`, and a version 1 copy's single
`profile`, `model`, and `extraArgs` are Claude Code's (`keptV1.claudeOnly`,
`TestLoadReadsAVersionOneCopy`). Its lists and maps are never nil. `File.Load` refuses a file over
8 MiB (`maxKeptBytes`, `appdata.ErrTooLarge`), and `DecodeKept` refuses more data than that as
`ErrNotKept`; a missing file is an error matching `fs.ErrNotExist`. The same format is a backup's
launch defaults section, described in [Backup files](../backup-files.md); keep the two in step.

## Recovery

When the database is set aside, damaged, or deleted, core (`internal/core/kept.go`) brings the rows
back during `Bootstrap`, after the prompt library and before the starters are seeded, and only when
the library came back (`prompts.Restore`, which runs only on a new library, found a source).
`kept.readDefaults` reads them from where the prompts came from: the database set aside, through
`ReadSnapshot`, when `prompts.Restored.Nicknames` is set; else, or when that database can't be read,
the copy, through `File.Load`, where a copy that isn't there holds none. `ReadSnapshot` opens a
database file of any schema read-only (`store.OpenSnapshot`) and reads `launch_defaults` by the
columns of every build (`defaultsColumns`): the per-agent `profiles`, `models`, and `agent_args`,
and the single `profile`, `model`, and `extra_args` of the builds before them, which were Claude
Code's (`prompts.SnapshotModels`, `prompts.SnapshotArgs`). A column it lacks inherits, and each
prompt ID is named by its nickname in that database (`TestReadSnapshotReadsAnOlderSchema`).

`Restore(ctx, store, kept, promptID)` then stores the rows in one transaction, all or none
(`TestRestoreIsAllOrNothing`), and only into a table that holds no row
(`TestRestoreLeavesLaunchDefaultsThatExist`), and returns how many it stored, which the window's
notice counts (`appinfo.Restored.LaunchDefaults`). Each prompt is found by its nickname among the
restored prompts (`prompts.Restored.IDs`), and one not found inherits. A profile is kept by name
even when no profile has it now, as on a Mac without that config directory yet: the launch menu
warns that it is missing, and it works again once the profile is back. What this build would
refuse is left out (`restorable`): an agent it doesn't know inherits, an effort no agent takes is
cleared, and an entry for an agent it doesn't know, a model `agent.ValidateModel` refuses, and flags
`agent.CheckExtra` refuses are dropped; a row for no valid org or repository, or one that then sets
nothing, is skipped (`TestRestoreMatchesPromptsByNickname`). A failure is logged and never stops the
start. The whole flow, with the prompts and the routines, is in
[internal/store](store.md#prompts-launch-defaults-and-the-users-scripts-outlive-the-database).

## Backup

`BackupPart` (`NewBackupPart(service)`) is the launch defaults' part of a backup of the user's
setup. It is a type of its own, apart from the `Service`, so the window, which binds the `Service`,
never reaches a method that takes a `*sql.Tx`; core's `backupDefaults`
(`internal/core/adapters_backup.go`) adapts it to a `backup.Part` ([internal/backup](backup.md)).
The section is the copy's format: `BackupPart.Kept` returns every row as the copy keeps it, which
`EncodeKept` writes.

`PlanBackup(ctx, kept, mode, known)` works out an import, changing nothing. Under `backup.ModeMerge`
each of the file's rows replaces this Mac's row for the same org or repository, matched ignoring
case and keeping this Mac's spelling, and the other rows stay; under `backup.ModeReplace` the rows
are exactly the file's, and each row here that the file lacks is planned as a removal. Each of the
file's rows is cleaned as a restore cleans one (`cleanKept`), with a note for what it leaves out: a
target that isn't an org or a repository (the whole row), an agent or an effort this build doesn't
know, each agent's profile, model, or flags it drops, and an outside contributors' choice it
doesn't know. A prompt `known` doesn't find for its kind inherits, with a note (`resolvePrompt`).
Core's `known` looks first among the prompts the library will hold for the file once the same
import's prompts section is applied (`backup.How.Prompts`: an agent prompt that covers the kind),
then, unless a Replace takes the prompts too, in the library by nickname ignoring case. A target
the file holds twice keeps its first row, and a row that sets nothing is left out, each with a
note. Each row kept is a `backup.ChangeAdd`, `ChangeUpdate`, or `ChangeSame` (`sameKept`), so an
import of the rows already here changes nothing (`TestImportOfTheSameRowsChangesNothing`).

`ClearTx` deletes every row inside the import's transaction, which a Replace does first (core's
`backupDefaults.Clear`, a `backup.Clearer`), so the prompts part may then delete what the rows
named. `ApplyBackup` stores each planned row inside the same transaction, its prompts found through
`find` (core's: the IDs the prompts part linked to the file's nicknames, else the library by
nickname), and a row whose prompts aren't found and that then sets nothing removes this Mac's row
for its target, which then inherits every setting. `BackupDone` rewrites the copy once the import
commits.

## Who reads them

- **The launcher** reads every row through `DefaultsReader` on each resolution, an item's launch's
  and a repository's alike (above).
- **Autopilot** reaches the rows two ways, both through core's adapters. An agent step is the
  launcher's own launch with the routine's choices in the launch menu's place (`routineRequest`),
  so a routine's Inherit means exactly what a click uses, both rows included; and `outsideRules`
  answers where agents may run on outside contributors' items. Readiness resolves Inherit only to
  the app's default agent and each agent's default profile (`autopilot.LaunchDefaults`, a type of
  Autopilot's own); the rows a click would follow are checked right before each start, for the
  item's own ([Agents in the terminal](autopilot/agents-in-the-terminal.md)).
- **The prompt library** learns where each prompt is a default through `Store.PromptUses`
  (`prompts.Config.Usage`), which a prompt's `Usage` lists as `Orgs`, `Repos`, and `SlotPlaces`.
- **Core** builds the `Store` and the `File` (`newKept`), hands the `Store` to the launcher, the
  prompts service, and the Autopilot adapters, restores the rows and keeps the copy (`kept.go`),
  adapts the backup's part, and supplies `Targets`' sources (`inboxRepos` in
  `internal/core/repos.go`, and the Repo scripts service). A bug report counts the orgs and the
  repositories that have rows, never naming one (`launchDefaultsWords`).
- **The window** binds the `Service` (`List`, `Check`, `Save`, `Remove`, `Targets`), reached through
  `frontend/src/platform/`: the Launch defaults form (`LaunchDefaultsSection.tsx`), the launch menu
  (`LaunchControls.tsx`, `useLaunchConfig.ts`), and the pinned and muted lists (`RepoRules.tsx`).
