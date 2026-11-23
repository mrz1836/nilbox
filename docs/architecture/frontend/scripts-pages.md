# The Scripts tab's pages

## The Release page's rows

`components/scripts/ReleaseRow.tsx` draws each group of the
Release page as a table, one line per repository: `releaseListClass` makes the group's `<ul>`
an `@container` and a grid of nine columns (the readiness edge, the checks, the repository,
the version, the count, the headline, the clone's notes, the age, and the action), and each
row, `CandidateRow` or `MutedRow` (its folder in the headline's place, Unmute in Release…'s),
is a `grid-cols-subgrid` of it across every column, each cell placed by its own column and
row classes, so the columns line up down the group and the headline takes the room the rest
leave. The rows follow the list's width, never the window's: under 56rem (`@max-4xl`) the
headline and the notes move to a second line under the repository, the edge spanning both,
and under 36rem (`@max-xl`) a row is a five-column grid of its own whose second line holds
the version, the headline, the notes, and the age (`metaClass`, `display: contents` while
they sit in the row's columns). `rowButtonClass` keeps Release… and Unmute quiet, muted words,
until the row is hovered or holds the focus (`group-hover/row`, `group-focus-within/row`).
A `CandidateRow` with Release… opens its sheet from a click anywhere on it: the `<li>` takes
the click itself, a pointer's shortcut for the button, which the keyboard and a screen reader
reach, rather than the inbox card's stretched button under raised cells, since nearly every
cell here has a tooltip that would otherwise have to sit above it and stop opening. Release…
stops its own click there, so the sheet opens once, and a click that ends a text selection
(`window.getSelection()`) opens nothing. A muted row's click does nothing: Unmute changes the
settings, so only its button does.
The pure half is `lib/release.ts`. `cloneNotes` turns a clone's state into `CloneNote`s,
each its `kind`, the few words the row shows (`text`), whether it `blocks` a release, and
its tooltip's `title`: uncommitted files, commits not pushed, a detached commit, a tag only
on this Mac, and a clone git can't read block; behind and a branch other than the default
don't, since magex pulls first and switches branches and back. `shownNotes` keeps only the
blockers while there are any, else behind, then the branch. `readiness` reads the edge of a
row with Release… (`releasable`), the first of failing checks, a blocking note, running
checks, passed checks, and none, and
`readinessCounts` counts a group's rows in that order for `ReadinessSummary`, the heading's
note, whose counts wrap onto a hidden second line so one that doesn't fit drops whole.
`sortCandidates` orders a group, whatever order the service sent, stably: the pinned
repositories first, then `newest` (the newest commit, then the name), `name` (`owner/name`
lowered, compared as the release service compares it), or `commits` (the most since the
newest version, then as `newest`). `sharedHeadlines` maps each headline to the repositories
of the group, as the search leaves it, whose newest commit it is, and a row shows the ×N
badge once `waveAt` (3) share it; `bumpKind` picks the count's feat or breaking chip from the
commits' headlines, as `bumpHint` words the sheet's hint. `ReleasePage` holds the sort the
user picks in local state over the stored one (`picked ?? releaseSort`), so the groups
reorder at once while App stores it with `settings.SetReleaseSort` (`scripts.releaseSort`)
and takes the settings it returns. `components/scripts/GroupHeading`, which Release, Hooks,
Repo scripts, and Deps share, is sticky at the top of the page's scroll container, over the page's
background, so a group's heading stays in view while its rows scroll under it, and its
`note` may be a node, given the rest of the heading's width, as the readiness summary is.

## The Deps page

`components/scripts/DepsPage.tsx` shows the manifests service's page the way
the Release page shows its own. Its head counts the clones and modules and says when they were
read, or, in that place, how far a read has got (`readStep`, from `manifests:progress`), so no
row moves; then Read again, the clones left out (`leftOutText`), and, while a failed read keeps
the last page, how old that is. Above the groups sit a search over `owner/name` that opens every
group (`matchClones`), a chip for each kind of module that keeps only the clones with one
(`moduleKinds`, Go and JS, each counting the clones the search leaves), and the sort (Oldest,
A–Z, or Modules, the pinned repositories first in each, `sortClones`), held in local state over
the stored one while App stores it with `settings.SetDepsSort` (`scripts.depsSort`). The pure
half is `lib/manifests.ts`, which groups, sorts, and words what the service read and decides
nothing else. A clone's group is when its age counts from as the clock stands (`cloneGroup` over
`ageGroup` of its `ageFrom`, which the service works out from its stalest counted module and the
user's mark, a month being 30 days, as `lib/time` counts one: Stale over three months, Aging from
one, Fresh under one); a clone none of whose modules counts (`noneCounted`) goes in Fresh with a
green edge and no age; and a clone whose age isn't known (`ageFrom` '', as when its read failed)
goes in Stale, last in Oldest, with a muted edge, since nothing vouches for it and a folded Fresh
would hide it; the page renders again for the clock only once a clone changes group (`groupsAt`
through `useNowAs`). Oldest sorts by `ageFrom` too. Each group sits under the shared `GroupHeading`, Stale alone starting open
(`depsGroups`; the folds are kept in `scripts.sections` as `deps.stale`, `deps.aging`, and
`deps.fresh`), Stale shown even when empty, and its note is the legend of its rows' edges
(`DepsLegend` over `legendOf`: caution, warning, and success, and a muted "age unknown").
`DepsRow.tsx` draws a clone on one line of its group's grid (`depsListClass`, nine columns, each
row a subgrid of them, following the list's width, `@container`, as a Release row does): the
edge (`staleTone`), the repository, its modules and tools, its stalest manifest or its error,
its notes, the age (its stalest counted manifest's, a mark notwithstanding), then in one cell
the mark up to date (a quiet `CircleCheckIcon`, `data-checked` and green while the repository is
marked, offered by `canMark` to a clone read with a module that counts, and to any clone marked,
to unmark it) and Upgrade…, quiet until the row is hovered or holds the focus, and the chevron,
which, like a click on the row's empty space, unfolds `DepsDrawer.tsx`. The notes are the upgrade
an agent works on there (`upgradeSessions`, the sessions of number 0 and kind '' in
`useSessions`' `byItem`, the one shown picked as a card's is, `cardSession`), then when the user
marked it (`checkedAgo`, *checked 3d ago*, as of the clock, `checkedTitle` in its tooltip), then
`cloneNotes` (none counted, or age unknown, read at HEAD, shallow, not all read). The drawer lists
the modules a line each, those that count first, a module that doesn't marked *not counted* after
its path (`uncountedTitle` says why), each with its own Reveal in Finder, so Finder shows the file
its line names, then the workspace files, with Open in Terminal, Mark up to date (or Unmark up to
date), and Upgrade dependencies… at its foot; it reads nothing more. A mark calls
`MarkChecked` or `ClearChecked` through `useManifests`' `mark`, which shows the page the service
returns in place of the one shown, so the clone moves group at once without a read of the clones;
one under way holds every mark back (`marking`), and one that fails says why in the head
(`markError`). `ScriptsView`
reads the page through `lib/useManifests.ts` (`listManifests`, with refresh for Read again,
following `manifests:progress`) each time the tab shows while Scripts is on, whichever page is
open, for the rail's count of the clones in Stale (`staleCount`), and keeps one sheet or dialog
open at a time: a release request or a run it opens closes the Upgrade dialog. **Upgrade…** opens
`DepsUpgradeSheet.tsx`, a dialog in the launch options' own frame (`LaunchFrame`, which
`LaunchDialog` holds an item's launch options in too), as tall as what it holds, which reads the
prompt library, the agents, and the profiles; offers the prompts for repositories
(`promptApplies` with kind '', which offers nothing else), in its first section, headed as the
launch options' are: the two starters, `deps-minor` as Minor and patch, chosen, and `deps-major`
as Majors too, while the library keeps them, and the user's others under Another prompt, each
with an Edit that closes the dialog and opens that prompt on the Prompts tab (App's
`openPromptsAt` hands `PromptsView` a `PromptRequest`, which it opens as a library click would,
asking first over unsaved changes, and `PromptLibrary` scrolls the open prompt into sight);
resolves and plans the launch with `ResolveRepo` and `PreviewRepo` (`repoLaunchRequest`) as the
prompt, agent, or profile changes, the plan naming the worktree and its base in the dialog's
sentence and showing a refusal, such as every name for today's worktree being taken, before
Start, which waits for both and while either fails; shows the launch options' own settings
(`LaunchFields`, from `LaunchMenu.tsx`,
the worktree's sentence in place of its choice) and their preview (`LaunchPreviewSection`);
starts with `LaunchRepo`, a hold opening the launch hold dialog, whose Start another starts
beside the session (a session with no item named by its repository alone, never `#0`); and
closes once the upgrade starts, unless its launch worked something around, which it stays open
to say. The launch options take a `LaunchSubject` rather than an item: an item by its kind, or a
repository by kind '', its trust, and how its launch is planned (`preview`, and `key`, what
else the plan is made with), so a repository's launch reuses them, and for a repository they
offer only its prompts, show the worktree always on, and offer no situation, override, or save
as a default. Elsewhere, a prompt for repositories stays out of every item's list
(`promptApplies` with an item's kind), and the routine builder's agent and GitHub steps offer
only prompts written for items (`fitsKinds` in `builder/parts.tsx`), even before the routine
acts on any kind. The Prompts tab offers Repositories among what an agent prompt is for, never a
reply's (`PromptEditor`), lists such prompts under a group of their own (`PromptLibrary`),
previews them with the sample repository alone (`PromptPreview`'s `PreviewKind` `repo`), and
dims every variable a repository's launch leaves empty, marked Item only, or PR only for a pull
request's (`VariablesPanel`, by `PromptVariable.itemOnly`).
