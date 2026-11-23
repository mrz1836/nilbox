# internal/sensitive

[Docs](../README.md) › [Architecture](../README.md#architecture) · Code: [`internal/sensitive`](../../internal/sensitive) · Guide: [Sensitive files](../guide/sensitive-files.md)

`internal/sensitive/` is the rule for which of a pull request's changed files need a closer look
before anyone reviews, merges, or opens an agent on it: files that run on CI with the repository's
token and secrets, files a coding agent loads or runs, files that run on the reviewer's machine,
files that change who must review, and files that bring in third-party code. A check reads each
changed file's path, and a renamed or copied file's previous path, never its content. The package
holds the categories, the pattern syntax and its matcher, whose pull requests are checked, the
`files.*` settings, and a check's outcome as the window shows it (`Check`). It reads nothing
itself: the inbox lists the files from GitHub and stores them, and the launcher, the review sheet,
the fence, and Autopilot act on what it finds. It imports only `trust`, `clock`, and `textutil`.
What the user sees is in [Sensitive files](../guide/sensitive-files.md).

## The categories

`Categories` returns the built-in categories in the order a path is matched against them, each a
`Category` of plain values: its `ID` (what the settings store), `Label`, `Short` (its name on a
card), `Description`, `Level`, `Locked`, and `Patterns`. A file is reported under the first
category that matches it.

- **`agent`** (`CategoryAgent`, "Agent configuration", `LevelDanger`, 15 patterns): what coding
  agents load or run, such as `.claude/`, `.mcp.json`, `CLAUDE.md`, `AGENTS.md`, `.codex/`,
  `.gemini/`, `.cursor/`, and `.github/copilot-instructions.md`. It is the one `Locked` category,
  always on, because launches are held back by it, and it comes first
  (`TestCategoriesAreWellFormed`).
- **`workflows`** (`CategoryWorkflows`, "Workflows and CI", `LevelDanger`, 13): what runs on CI
  with the repository's token and secrets, such as `.github/workflows/`, `.github/actions/`,
  `action.yml`, `.github/env/`, `.gitlab-ci.yml`, and `.goreleaser.yml`.
- **`tooling`** (`CategoryTooling`, "Build and dev tooling", `LevelDanger`, 35): what runs on the
  reviewer's machine when they build, test, install, or commit, or open the project in an editor
  or a container, such as `Makefile`, `magefiles/`, `scripts/`, `*.sh`, `.husky/`, `.envrc`,
  `.vscode/`, `.devcontainer/`, `Dockerfile`, and `package.json`.
- **`review`** (`CategoryReview`, "Review and merge rules", `LevelWarning`, 11): what decides who
  must review a change and what updates or merges on its own, such as `CODEOWNERS`,
  `.github/dependabot.yml`, `renovate.json`, and `.mergify.yml`.
- **`dependencies`** (`CategoryDependencies`, "Dependencies", `LevelWarning`, 29): what brings in
  third-party code or changes where it comes from, such as `go.mod`, `go.sum`, `vendor/`,
  `.gitmodules`, the lockfiles, `requirements*.txt`, `Cargo.toml`, and `*.gemspec`.

The user's own patterns are a sixth category after them, `custom` (`CategoryCustom`, "Your
patterns", `LevelDanger`), which `customCategory` builds only when there are some. `LevelDanger`
is for files that run, or steer an agent, without anyone choosing to run them, `LevelWarning` for
files that change what the project trusts or depends on, and `LevelNone` (`""`) is the level of a
path that matched nothing. Every built-in pattern is one a user could enter: `NormalizePatterns`
accepts each (`TestCategoriesAreWellFormed`).

## The pattern syntax

Patterns follow `.gitignore`, matched without regard to case, since a case-insensitive file
system, as macOS uses by default, opens `.Claude/Settings.json` for `.claude/settings.json`
(`TestMatchingIgnoresCase`). `compile` trims a pattern and refuses it, as an `ErrInvalidPattern`
that names it (cut to its first 60 characters by `shorten`) and says why, when it is empty, longer
than `MaxPatternLength` (200 bytes), holds a control character, is an exclusion (a leading `!`),
holds a `..` name, has more than `maxDoubleStars` (4) `**` parts, holds a name `path.Match` can't
read, names no file or folder, or matches every file: a lone `**`, or any pattern that matches all
three probe paths of `everyPath` (`a`, `b/c`, and `d/e/f.txt`), such as `*` or `**/*`. Compiled, a
pattern is its lowercased names (`segment`s, skipping empty and `.` names), each a literal compared
by equality when it holds none of `*?[\`, else a `path.Match` pattern, and two flags:

- **Anchored** (`anchored`): a slash at its start or in its middle matches it from the
  repository's root (`.github/workflows`, `/Makefile`, `deploy/**/*.tf`). A pattern without one
  is a single name, matched at any depth against each folder on the path and the file's own name
  (`CLAUDE.md`, `*.sh`).
- **A folder** (`dir`): a trailing slash matches only a folder, so `deploy/` matches
  `services/deploy/x.yml` but never a file named `deploy`: a folder pattern must leave at least
  one name inside the folder it matches.

A pattern that matches a folder matches everything inside it, so `secrets.txt` matches
`secrets.txt/inside` too. `**` matches any number of folders, none included
(`**/migrations/*.sql` matches `migrations/1.sql`), `*` any run of characters within one name, `?`
one character, and `[abc]` one of a set (`TestPatternSyntax`). A path is split the same way
(`splitPath`: lowercased, empty and `.` names dropped), and an empty path matches nothing.
`FuzzCompilePattern` holds every pattern that compiles to this: surrounding whitespace and ASCII
case never change it, it never matches every file or an empty path, a folder pattern never matches
a file at the root, and one that matches a folder matches what is inside it.

`CompilePattern` and `Pattern.Match` give the same syntax to matching outside any category, a
routine's condition on the paths a pull request changes, and refuse what the settings refuse
(`TestCompilePatternRefusesWhatTheSettingsRefuse`).

## The settings (`files.*`)

The settings are three keys of the prefs table, which the settings service alone writes, and which
the inbox (and through it the launcher and Autopilot) and the review sheet read. `DecodeConfig`
reads them into a `Config` (`Scope`, `Off`, `Patterns`), each missing or unreadable one falling
back to its default (`DefaultConfig`: collaborators checked too, every category on, no patterns of
the user's):

- **`files.scope`** (`PrefScope`): `outside` or `collaborators` (`DefaultScope`). Any other value,
  one in another case included, reads as the default.
- **`files.off`** (`PrefOff`): the built-in categories turned off, a JSON array of IDs, read sorted
  without unknown or locked IDs or repeats (`decodeOff`). Unlike `prefval.List`, it keeps the IDs
  of an array that holds something else too.
- **`files.patterns`** (`PrefPatterns`): the user's patterns, a JSON array of strings in the order
  they were added. A stored pattern that no longer compiles is dropped, and at most `MaxPatterns`
  (100) are kept (`decodePatterns`).

`FuzzDecodeConfig` checks that any stored values decode to a valid `Config`, and that it decodes to
itself once written back as settings writes it. Settings writes the scope with `SetSensitiveScope`,
refusing any but the two (`ErrInvalidScope`); a category with `SetSensitiveCategory`, one
`prefs.Update` of `files.off` through `SetCategory`, which refuses an ID that isn't built in
(`ErrUnknownCategory`) and turning off the locked one (`ErrLockedCategory`); and the patterns with
`SetSensitivePatterns` through `NormalizePatterns`: each trimmed, empty ones and repeats (ignoring
case) dropped, the order kept, one that can't be used refused, and more than 100 an
`ErrTooManyPatterns`. Both lists are written through `prefval.EncodeList`. A backup carries the
three as `Portable`'s `sensitiveScope`, `sensitiveOff`, and `sensitivePatterns`, checked by the
same rules on import. `Settings.SensitiveFiles` hands the window the `Config`,
`Settings.SensitiveCategories` the categories with their patterns, and `MatchSensitivePath` answers
**Try a path** with `Lookup`: a `PathMatch` of the category, label, level, and pattern a path
matches, all empty for none.

## Whose pull requests are checked (`Scope`)

`Checked(scope, class, association, mine)` decides from the item's `trust.Class` (see
[`internal/trust`](trust.md)). It never checks a pull request the signed-in user opened (`mine`);
always checks one whose class is `trust.ClassOutside`, which is every pull request from a fork,
every outside contributor's, and every bot's not on the trusted list; and, under
`ScopeCollaborators` alone, checks a `trust.ClassMember`'s whose association is `COLLABORATOR`
(trimmed, in any case): people with access to the repository who are not its owner or members of its
org. Neither scope checks the owner, the org's members, or the trusted bots, so Dependabot's
`go.mod` updates stay quiet on the cards, and an issue never is: the inbox's `fileScope.checks` asks
only for pull requests.

`CheckedUnattended(mine)` is the wider scope a start a routine makes, rather than the user's own
click, is checked under: every pull request the user didn't open, members' and trusted bots'
included, whatever the scope says, since a trusted bot's pull request can still carry agent
configuration, a sync from another repository for one. `ErrNotChecked` is what the readers return
for an item the scope that applies leaves out.

## Matching and a check's outcome

`NewMatcher(cfg)` compiles the patterns of every category `cfg` enables into ordered rules: the
built-in ones in matching order (the locked one whatever `Off` holds), then the user's, skipping a
pattern that no longer compiles. `Matcher.Match(path)` returns the category and the pattern of the
first rule a path matches. A changed file (`File`: `Path`, `PreviousPath`, and `Change`, GitHub's
word for how it changed) is matched by its path and, when it was renamed or copied, by the path it
came from, whichever's category comes first in matching order, the new path on a tie
(`matchFile`): a file moved out of a sensitive folder is as sensitive as one moved into it, since
moving a workflow away disables it (`TestRenamesMatchEitherPath`).

A stored check is a `Result`: the head commit the files were listed at (`Head`), how many files
GitHub says the pull request changes (`Total`), the files it listed, whether those are all of them
(`Complete`), the `Error` that stopped it, and when it ran (`CheckedAt`).
`Matcher.Evaluate(result, head, prURL)` turns one into a `Check` for a pull request whose head is
now `head` (`""` when unknown, which takes the stored head as current):

- **`pending`** (`StatusPending`): no result, or one for another head (compared ignoring case):
  new to the inbox, newly in scope, or pushed since its last check.
- **`failed`** (`StatusFailed`): a result with an `Error`, whatever its head, so the failure shows
  until a check succeeds.
- **`flagged`** (`StatusFlagged`): at least one file is sensitive.
- **`partial`** (`StatusPartial`): GitHub listed only some of the files, none of them sensitive.
- **`clear`** (`StatusClear`): every file was checked, none of them sensitive.

A `Check` carries its `Status`, `Head`, `Checked` (the files listed) of `Total` (never less than
`Checked`), `Complete`, `CheckedAt` (RFC 3339 through `clock.Format`, or `""`), `Error`, and
`Groups`, which is empty unless it is flagged: one `Group` per category in matching order, its files
sorted by path, each a `Finding` with its path, previous path, change, the pattern it matched, and
`URL`, its diff in the pull request on GitHub (`diffURL`: the files page at the anchor GitHub names
`diff-` and the SHA-256 of the path, or `""` for a page not on `https://github.com/`).
`Check.Category` returns one category's group. `Check.Summary` names the sensitive files for a
prompt, each path with its category's `Short` and where a moved file came from, as in
`.github/workflows/ci.yml (Workflows)` or `docs/old.yml (Workflows, moved from
.github/workflows/ci.yml)`: at most `maxSummaryFiles` (20), then "and N more", and `""` when none
is sensitive. `BenchmarkEvaluateLargePullRequest` evaluates a pull request of 3,000 files.

## How a pull request's files are read

The inbox owns the reads and the table, `item_files` (`internal/inbox/files.go`; see
[`internal/inbox`](inbox.md)). A refresh's `checkFiles` picks its targets in inbox order
(`fileTargets`): listed pull requests outside a muted repository or org whose files are checked
under the scope, judged for the account this refresh read, that have a node ID and a head, and
whose last check failed, is for another head, or is missing (`fileScope.due`), at most
`maxFileChecks` (20) of them. `runFileChecks` checks them `fileChecksParallel` (4) at a time within
`fileCheckTimeout` (30 seconds), each with `github.FetchChangedFiles`: GitHub's REST API, since
GraphQL doesn't say where a renamed file came from, 100 files a page and at most 3,000, read between
two readings of the head commit so the list belongs to one commit (read once more when the head
moved, then `github.ErrHeadMoved`), at one request per 100 files plus one or two for the head. A
check that fails never fails the refresh: it is stored with its reason (`fileErrorMessage`, at most
300 runes), and the next refresh tries again. An item refreshed on its own is checked the same way
when a check is due (`checkUpdatedFiles`), and a caller's read of a pull request that listed its
files at the head it answered for, as the review sheet's does, serves as the check with no request
more (`PullRead.Files`, `readFiles`).

A row of `item_files` holds the head, the total, the files (a JSON array of each one's path,
previous path, and change), whether they are complete, the error, and when it was checked: paths
only. `PruneFiles` removes the checks of the items that left every list, but those a routine's
search watches. The snapshot evaluates each item's stored check as `Item.Files`, nil for an item
whose files aren't checked (`itemRow.checkedFiles`). The scope and its compiled matcher are kept
until the settings change (`scopeCache`, keyed by the `settingsKey`, which holds the three
`files.*` keys and `trust.bots` among others), so a change in Settings shows on every card at once,
without asking GitHub.

Three reads serve the rest. `StoredFiles` evaluates the last check without asking GitHub: pending
when the item was pushed since. `CheckFiles`, bound, which the card's **Check now** calls, holds
the item's lock, reads the pull request's head (`FetchPullHead`, one request), lists the files
again unless a successful check of that head is stored, and stores the outcome unless a refresh
that started later checked them too (`storeItemWrite`). `Routines.CheckFilesUnattended`, which the
window never reaches, does the same under the unattended scope. A check that can't finish comes
back as `failed`, never as an error, so the caller can hold back on it, and each returns
`ErrNotChecked` for an item its scope leaves out.

## Who uses it

- **The cards and Settings.** The window words `Item.Files` as the card's chip and its panel of
  files (`frontend/src/lib/sensitive.ts`'s `filesChip`, `components/card/RiskChip.tsx`,
  `components/SensitiveFiles.tsx`), and Settings lists the categories, takes the user's patterns,
  and tries a path (`components/settings/SensitiveFilesSection.tsx`, `PathTester.tsx`).
- **Launches.** The launcher's `FileChecker` is core's `agentFiles`, the inbox's checks
  (`launchFiles`) with what agent runs changed merged in. A click's launch checks as of now
  (`CheckFiles`), a routine's under the unattended scope (`CheckFilesUnattended`), and a preview or
  a resume reads the stored check (`StoredFiles`; `checkFor`, and `backgroundHolds` for a run in
  the background). `holdFor` holds a launch whose check has an agent group as `HoldAgentFiles`,
  keyed by the head, and one whose check is incomplete, failed, or pending as `HoldUnchecked`,
  keyed by the status and the head, so a confirmation never carries over to a later push; the
  other categories never hold a launch back. See [`internal/launcher`](launcher.md).
- **The prompt.** `promptData` fills `prompts.Data.SensitiveFiles` with `Check.Summary`, and
  `internal/prompts`' `sensitiveNotice` ends the prompt with the fixed line that names them, after
  the outside-contributor line, clipped to `maxSensitiveRunes` (1,000).
- **Autopilot's fence.** `fence.DeniedCategories` (agent, workflows, and tooling) are the files no
  fenced agent edits, matched by `deniedMatcher` from this package's own patterns whatever the user
  turned off, without the user's patterns, and listed by `fence.DeniedPatterns` for each agent's
  own rules. `fence.CheckChanged` checks the files a run's commits changed under the user's
  categories with the denied ones always on (at most `fence.MaxChanged`, 3,000), the inbox keeps
  them per item and head (`NoteAgentChanges`, `AgentChanged`), and `fence.MergeChanged` adds them
  to the item's own check, so an agent file a run wrote holds the next launch on the item, whoever
  opened it. See [`internal/fence`](fence.md).
- **Autopilot's routines.** A routine's facts read the stored check at the details' head as
  `files.paths` (`FieldPaths`: each path and previous path) and `files.sensitive`
  (`FieldSensitive`: the categories they touch), both partial when the check isn't complete and
  unknown until a check at that head is stored (`internal/autopilot/facts`, `build_pr.go`), with
  the settings from the inbox's `FactsEnv`. The catalog offers the categories by their `Short` in
  lower case, then "your patterns" (`sensitiveChoices`), and a condition on the changed paths
  compiles through `CompilePattern` (`spec`'s `checkMany`, `eval`'s `pathPatterns`). Post the
  agent's review approves only when `files.sensitive` is known, not partial, and empty
  (`guardOf`), and Merge it merges nothing when what the agent's commits changed at that head holds
  a sensitive file or wasn't all checked (core's `agentChangedAt`). See [Merge it and Post the
  agent's review](autopilot/merge-and-review.md) and [Agents in the terminal, readiness, and Needs
  your OK](autopilot/agents-in-the-terminal.md).
- **The review sheet.** `review`'s `fileOf` marks each file the sheet shows with its category's
  `Short` and `Level`, by its path, else its previous path, whoever opened the pull request. The
  danger-level ones become the merge warning `SensitiveWarning` ("It changes sensitive files:
  Workflows."), which a merge asks the user to accept, and which a routine's merge past the rules
  accepts only with `RoutineMerge.Sensitive`. See [`internal/review`](review.md).
