# internal/autopilot

[Docs](../../README.md) › [Architecture](../../README.md#architecture) · Code: [`internal/autopilot`](../../../internal/autopilot) · Guide: [Autopilot](../../guide/autopilot/README.md), [What Autopilot never does](../../guide/autopilot/never-does.md)

**In this folder:**

- [The routines, kept](routines-kept.md)
- [The searches, and the builder's reads of the inbox](searches.md)
- [The engine and the ledger](engine-and-ledger.md)
- [What runs next (`upcoming.go`)](upcoming.md)
- [Verify, tries, the loop guard, and profile fallback](verify-and-tries.md)
- [Merge it and Post the agent's review](merge-and-review.md)
- [Agents in the terminal, readiness, and Needs your OK](agents-in-the-terminal.md)
- [Agents in the background](agents-in-the-background.md)
- [Codex in the background](codex-in-the-background.md)
- [Local actions: by a routine, Undo, and hands off](local-actions.md)
- [The GitHub step](github-step.md)
- [Notices, the menu bar, and the window's close](notices.md)
- [Easy mode: templates, from an item, why, export and import, graduating](easy-mode.md)
- [Schedules, the digest, the script step, and a routine's week](schedules.md)
- [The Autopilot tab](autopilot-tab.md)
- [`internal/autopilot/draft`](draft.md)
- [`internal/autopilot/kit`](kit.md)

`internal/autopilot/` is Autopilot, which watches the user's items and runs their routines. Its root
package keeps the routines and serves them to the window ([The routines, kept](routines-kept.md));
beneath it is the routines core, five pure leaves. They read nothing, start nothing, and keep no
clock: the moment a decision is for comes with it. They import only `item`, `repo`, `trust`,
`sensitive`, `bump`, `clock`, `textutil`, the standard library, and one another, in this order:
`catalog` and `watch` at the bottom, `spec` over both, `facts` over `catalog`, and `eval` over all
three. `TestLeafImports` in `internal/repoguard/deps_test.go` holds each leaf's direct imports to
that list, so a leaf can never reach the store, GitHub, a process, or a service.

**`catalog`** is the vocabulary: every name a spec stores is one of its constants, and the
builder renders what it lists. Its parts:

- the triggers (`Triggers`: each one's event, label, the kinds of item it fires for, what
  one occurrence is, and its options with their ranges and defaults);
- the fields (`Fields`: `<group>.<name>` IDs in seven groups, item, repo, pr, checks,
  files, dep, and history, each with its type, the kinds it applies to, its choices, how
  a list's patterns match, globs or `.gitignore`'s, its read cost, and its words for the
  sentence);
- the operators per type (`Ops`, with arity and negation; a list of choices takes only the
  set operators, `Takes`);
- the steps (`Steps`: each with the `Dispatcher` that runs it, triage, notices, github,
  agent, scripts, verify, merging, or review, whether it runs at once (`Local`) or follows
  the routine's mode (`AsksFirst`), and `Offered`, which every step and action is in this
  build: one a later catalog lists before its dispatcher is built stays unoffered);
- a schedule's styles (`ScheduleStyles`: each match on its own, or one digest) and how often
  it fires (`Everies`); the ways and methods Merge it merges (`MergeHows`, `MergeMethods`, the
  repository's own first), the ways Post the agent's review posts (`ReviewAses`), and what
  Tell me can say (`TellWhens`), each with its label and help, as `All()` serves them;
- the Only if outcomes, the tools presets (`MayPush`, `ReadOnly`), the agent step's
  settings (a budget in tokens among them, `budgetTokens`), the other steps' whole-number
  settings (`StepSettings`: `snooze.days`, `waitForChecks.settleMinutes`, and
  `waitForChecks.giveUpHours`), the limits, and their ranges, which the builder reads its
  bounds and defaults from rather than keeping its own.

`GitHubActions` lists the GitHub step's four actions (rebase, updateBranch, reply, labels),
each offered, with how far it can be taken back (`GitHubActionByKind` reads one); Mark the
GitHub notification done is a local step of its own. The catalog has no entry
for a close, approving workflow runs, `merge_bypass`, or the sheet's combined approve and
merge: `TestCatalogHasNoForbiddenAction` checks every ID it lists, and
`TestNoAutopilotCodeSpellsAForbiddenAction` every string literal the autopilot packages
spell. Merging and posting an agent's review are steps of their own, held in by their own
checks. Lookups by ID (`FieldByID`, `OpByID`, `TriggerByEvent`, ...) read one index built
once, since the evaluator makes them for every condition on every item. Three of the
catalog's names (Restore, Merge it and the merge method, the rebase action and method) are
spelled as git verbs; `TestNoGitCommandChangesAClone` lets their declarations in
`catalog/steps.go` through, and nothing else.

**`watch`** turns a search, as the builder writes it from fields (kind, authors, where,
involving, labels, notLabels, base, headPrefix, state, and qualifiers typed by hand), into
the GitHub query a refresh sends. Every query adds `is:open archived:false
sort:updated-desc` (without the sort, GitHub orders newest-created first, so "the 100 most
recently updated" would not hold); is scoped, by a `user:`, `org:`, or `repo:` qualifier
that isn't negated, or one whose value is `@me`; quotes its labels, as one comma list each
for any of them (`label:"a","b"`) and for none of them (`-label:"a","b"`), refusing a label
holding a comma; quotes any value holding a space of any kind, Unicode's included; passes
the head branch's prefix through exactly, quoted, since GitHub reads `head:` as a plain
prefix, refusing in a branch any space, since the base goes in unquoted; writes a bot as
`author:app/<slug>` (the bare slug matches nothing, so
`Normalize` writes `dependabot`, `renovate`, and `github-actions` as apps and `x[bot]` as
`app/x`); refuses a hand-typed qualifier it doesn't list, free text, and the
search's own (`is:` but public, private, locked, or unlocked, `sort:`, `archived:`, `in:`);
and is at
most 256 characters, refused over that with the hint to split it. GitHub doesn't error on
a longer query, it answers nothing, which is why the cap holds the whole query. "Your
account" (`@me` in Where) becomes `user:<login>` in `Build`, so a routine follows
whichever account is signed in, while `Check`, which `spec.Validate` calls, builds with the
longest login GitHub allows, so a search it passes fits any account. The query is written
in one canonical order from the normalized search, so it is its own dedup key
(`Query.Key`).

No value can smuggle in a qualifier. GitHub's search has no escape for a quote inside
quotes, so no value of any field (a label, an author, a place, a branch, a hand-typed
qualifier's value) may hold what `unsearchable` names: a quote, a backslash (which a
reader could take for an escape of the closing quote, letting what follows go live), the
fullwidth quote and backslash (which a reader folding Unicode's compatibility forms reads
as those two), a control character (a newline and a tab among them), or a line or
paragraph separator. An apostrophe is fine, since GitHub quotes only with double quotes.
Each field refuses such a value with its own problem (`labels[1]`, `base`, `more`, ...),
and so does the build itself: every token carries the field it came from, and `build`
checks each one again as it writes the query (`token.check`), so a check a field missed
fails the query rather than sending it. The writer (`token.String`) also takes the quotes
and backslashes out of every value it writes (`quoteless`), whoever calls it, so not even a
token that skipped both checks can end its quotes early. `FuzzTokenString` reads what it
writes as GitHub's documented syntax does, and as a reader that takes a backslash for an
escape would, and finds one qualifier, the token's own, between the query's neighbors;
`TestHostileValuesAddNoQualifier` does the same for each field through `Build`.

**`spec`** is a routine's definition, one JSON document with a `version`: the watch's
sources (the inbox, by the reasons items are in it, or a search), one trigger with its options,
the conditions (`all` or
`any`, with one level of groups), the steps in order, each optionally carrying `onlyIf`,
the limits, the routine's own banner switches, and the tools it requires. The mode, the
on or off switch, the name, and the account are its row's, never the spec's. An option or
a limit left out takes its default (a nil pointer), so the builder can tell a value set
from one inherited. Format version 2 (version 1, which an older build wrote, reads as version 2:
`migrate`, inside `Normalize`) holds every step kind (notify, pin, snooze, dismiss,
restore, markDone, github, agent, waitForChecks, merge, postReview, tellMe), so a later
build adds the code that runs a step, never a format change, but one: Run one of my scripts
(`script`, the user's script by its ID and the name it was picked under) came later, as a key
an older build refuses, so it reads such a routine as broken. Version 2 adds a `uuid` to a prompt
reference and a script step, the record's UUID, which is looked at before the name: a key the
strict decode of an older build would refuse, so the version went up, and that build reads a
version 2 routine as one a newer build made, left as it is, rather than unreadable. A UUID must be
canonical and never the Nil UUID, a prompt's only beside a starter or a nickname, never the item's
own default; `Normalize` writes it in lowercase. `PromptRefs`, `ScriptSteps`, `Clone`, and
`WithoutUUIDs` are the shared walks. `TestSchemaGolden` pins every key and every closed set of
values.

- `Decode` is strict: it walks the document beside the `Spec` type and holds every
  object's keys, byte for byte, to its struct's JSON names, since encoding/json would
  match a key ignoring case, Unicode's folding included (`Trigger` beside `trigger`, or
  `ſteps` for `steps`); and it refuses a key given twice (which encoding/json would read
  as the last), a value of the wrong type, anything after the document, nesting past 16
  levels, and more than 64 KiB. `DecodeExact[T]` is the same strict walk for any type, which
  Describe it's [kit](kit.md) reads its files and proposals with.
- `ClosedSets` gathers every closed set of values the format holds (the watch's, the
  trigger's, the conditions', the steps', and the limits'), each from the catalog's, watch's,
  or item's own lists, never a copy: `Validate` checks every value against them, kit's schema
  enumerates them, and `TestEveryNamedStringTypeHasAClosedSet` holds every field of the format
  whose type is a named string type to one, so no vocabulary enters the format without its
  values.
- `Normalize` trims every text, sorts and dedupes every list that is a set (text compared
  ignoring case, the first spelling kept), writes a choice as the catalog spells it, keeps
  a condition written the same way twice in one list once (its value compared exactly, so
  the regexes `\d` and `\D` stay two), and keeps the user's order of the sources, the
  conditions, the steps, a GitHub step's actions, and the fallbacks.
- `Validate` returns every problem, each naming its part and field (`Problem.Path`:
  `watch.sources[0].search.where`), and holds the steps to their order: one agent step at
  most; Wait for the checks only after an agent step in the background that may push, and
  never only if it didn't push; Merge it only after a Wait for the checks that runs only if
  the agent pushed, so the pass it follows is on the agent's own commit and never a flaky
  check run again, with `onlyIf: "passed"`, or, in a spec with no agent step and no wait
  (`Spec.Agentless`), on the trigger `checks_passed`, once per head commit, with no Only if
  and no rebase, update of the branch, or script step before it (`agentlessMerge`, from
  `validator.event`, `oncePer`, and `before.changesBranch`); `allowBypass` and
  `allowSensitive` only there, with `how: "whenMergeable"`, and `allowSensitive` only with
  `allowBypass` (`pastRules`; `TestOnlyMergeItNamesABypass` holds them the format's only keys
  naming a bypass); Post the agent's
  review only after a read-only agent step in the background; Allow approving only with
  `as: "verdict"`; only a notice after an agent step in the terminal, whose end the app
  can't see; and an Only if only on an outcome an earlier step ends with (the checks', the
  latest Wait for the checks'; the push and the verdict, the agent step's).
- `Load` reads the version first, so a newer build's spec says it is newer rather than
  unreadable, then decodes, normalizes, and validates. A spec it can't load returns a
  `*BrokenError` (`ReasonNewer`, `ReasonUnreadable`, `ReasonInvalid`), so its routine is
  broken, not fatal; an older version is migrated in `migrate`, which `Normalize` runs first, so
  `draft.Encode`, a template, and kit's proposals are brought forward in one place (version 1 to 2
  adds nothing but the version, its UUIDs left for a stamp). A routine stored before a rule tightened (a label holding a fullwidth
  quote, say, which `watch` refuses) loads this way, broken as invalid, its problem
  naming the field for the builder to show, its spec never rewritten, and no query built
  from it.
- `Sentence` writes the one sentence every place shows, from `Phrase` for each condition,
  with `PromptNames` naming a starter as the library does; `TestSentenceGolden` pins each
  template's. A value it shows (a label, a condition's value, a search's author, place, or
  branch) goes in bare when it is one plain word, and otherwise quoted and escaped as Go
  quotes a string (`strconv.Quote`), so a value's own quote or backslash can never
  unbalance the sentence's quotes, and a newline or a bidirectional mark shows as its
  escape. The hand-typed qualifiers go in as typed while their quotes pair up and they
  hold no backslash and nothing that doesn't print, and otherwise quoted whole.
  `FuzzSentenceQuotes` holds every quoted stretch to read back, as Go reads a quoted
  string, as the value it quotes.

**`facts`** holds one item's values for the evaluator, built by the pure `Build` from a
plain `Row`, which the inbox's query fills: the item, its repository, its triage, and, once
read, its details, its head commit's check names, and its changed files, each part nil
until it is. Each value is known, unknown (with why, and the `Need` the engine would read to
know it: details, checks, or files), or not applying to the item (a pull request's field on
an issue). A list read only in part is known and `Partial`. Check names and files count only
when read at the head commit the details name. A check's name is `workflow / job` as GitHub
shows it (an app's run, such as CodeQL's or OSV-Scanner's, and a status have no workflow),
each name once though GitHub lists one per event that ran it, failed when any of its runs on
the head commit failed; the checks are done, and so can settle, only once every one has
finished, at the newest completion time (a skipped run may say it completed before it
started, so start times never count). The dependency fields apply only to a pull request
Dependabot or Renovate opened (by its author's type and login), read from its title by
`bump.Parse` and its ecosystem from its head branch by `bump.ParseBranch`; a title that
doesn't read as an update leaves them unknown. The author's trust is unknown while their
association isn't read, since they may be a member, but for a fork's pull request, an
outside contributor's whoever opened it, and a bot's, which the trusted list decides.
Every text is made one line and capped (`textutil.OneLine`).

**`eval`** decides. `Evaluate(spec, facts, history, now)` returns a `Decision`: `Match`
(yes, no, or unknown), the `FireKey` while the trigger holds, the `Trace` (the trigger's
step, then each condition that ran, with what it compared and the actual value), and the
`Needs` of an unknown decision. `Prepare(spec)` works out once what deciding the spec takes
from the spec alone (the order each list of conditions runs in, each condition's path and
phrase, the trigger's words), and its `Plan.Evaluate` decides an item byte for byte as
`Evaluate` does, traces included; `Evaluate` prepares the spec for its one decision, and a
caller deciding a routine on many items, as the engine's cycle does, prepares it once.
Conditions are three-valued: one on a value not known yet is unknown whatever its operator,
its negation too; one on a field that doesn't apply is no;
a partial list's find is sure and its miss unknown; an "all" operator over an empty list
is no, never vacuously yes (a pull request that changes no file doesn't have every changed
path under docs/). A regex or a path pattern is compiled once, in a cache every evaluation
shares, not on every item of every cycle. `All` is no at its first no, `Any` yes
at its first yes, and a list stops there; the order conditions run in is their read cost,
then what each says, never where it sits, so shuffling them never changes a decision. The
`History` is the ledger's: the routine's tries and last outcome, the head it first saw, the
heads routines pushed, the comments it last counted, and a click's token. Fire keys name
the event and the occurrence (`checks_failed:commit:<oid>`, `labeled:label:<name>`,
`new_comments:comments:<n>`, `schedule:<date>`, `manual:<click>`, ...), and the routine's
own once-per (item, head commit, or day) replaces the trigger's. A key holds only what is
known: Falls behind waits for the base branch to be read, so one occurrence never fires
twice, without its base and then with it. Checks fail and Checks pass wait for the checks
to settle, the newest completion at least the settle time ago. A trigger that doesn't hold
yet but will by itself says so (`Decision.Pending`, its `When`, its `At` when the clock
alone decides it, and the `FireKey` it fires for then, with the routine's own once-per):
Checks pass while its checks run (none failed, none waiting on a maintainer) or settle;
Checks fail, waiting for every check, once one failed while the rest run or settle; a
schedule due later today; Goes quiet within a day of its spell (`soonQuiet`). A trigger
that waits on an event no one can foresee is never pending. `Plan.Conditions` decides the
conditions alone, as `Evaluate` runs them, and `Plan.ConditionsWhenDue` decides them as the
item will stand once a pending trigger holds, with the facts it makes so (`Pending.Assume`:
Checks pass waiting on running checks makes them all passed, `allPassed`; Checks fail waiting
for the rest makes them finished and failing, `allFinished`), which the engine asks of a
pending decision. The property tests (`FuzzShuffledConditionsDecideAlike`, `FuzzNegationOfAnUnknownIsUnknown`,
`FuzzNeverYesOnAnUnknownItNeeds`) run over generated specs and facts; the last checks that
reading what was missing never turns a yes or a no into anything else.
