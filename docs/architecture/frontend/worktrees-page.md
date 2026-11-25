# The Worktrees page

[Docs](../../README.md) › [Architecture](../../README.md#architecture) › [`frontend`](README.md) · Code: [`frontend/src`](../../../frontend/src) · Guide: [The Worktrees view](../../guide/worktrees-view.md)

`components/WorktreesView.tsx` is `worktrees/PageHead` over a body that scrolls on its own
(`overflow-y-auto`, as `ReleasePage`'s does), so the clone headings stick in it, and the head's
progress, which takes the place of its line of counts, never moves a row. The pure half is
`lib/worktrees.ts` and `lib/worktreeParts.ts`, which word what the backend decided and decide
nothing themselves (the `can*` flags say what may be removed). `laneOf` puts a worktree in one lane
from the `Worktree` alone, so the toolbar's filter (`lib/worktreeFilter`, which leaves the main
checkout out and keeps a row showing an error) counts what the page shows, and `attentionSort`
orders a clone's rows by lane, then the newest commit. `stateOf`, `workNotes`, and `agentLine` word
a row's cells (a live process's command line, and whether it is detached, come with it: the agent
cell's tooltip and the drawer's Session line show the one, and the cell, that line, and `safety`'s
band mark the other, `detachedWords`), `verbOf` its one verb, `removeWhy` why Remove… or Force remove… can't run, and
`safety` what removing it would cost, as `Words` data the drawer's band, the confirmations, and the
bulk confirmations' `SafetyLine` render. A worktree whose only changes are files missing from its
folder (`onlyMissing`, from the backend's `missingCount`) has nothing to discard (`uncommitted` is
0): it is never at risk, its row notes the files muted (**17 missing**) with a muted ghost for its
state, its drawer offers Remove…, never Force remove…, and `safety` says its last commit has them.
`cloneSummary` is a clone's heading (its main checkout as a
`MainNote`, its lanes counted), `facetsOf` and `facetQuery` the head's chips, and `itemFacts` and
`liveSessions` join what the window already has, the inbox's items by `itemKey` and the followed
sessions by their folder, which add words and move no row to another lane.

`lib/useWorktreeDetails` keeps the `Details` read for one listing, which `useWorktrees` numbers
(`listing`): a new number drops them (state set while rendering), a ref dedupes the asks, so
StrictMode's doubled effects make one call, and an answer for an older listing is dropped. A failed
ask leaves the ref, so the next read asks again, but nothing retries on its own. An open drawer
reads as it opens and after each listing (`useEffectEvent` and an effect), and Force remove's
confirmation as it mounts and whenever a listing drops what it read while it's open, both through
the page's one instance, and `readable` keeps the main checkout, a folder that's gone, and one git
couldn't read from asking at all.

`asReadNow` counts a worktree's files (those missing from its folder among them) and commits as its
details last did, and the drawer's band and
removal, the ⋯ menu's removals, and the confirmations all go by it, so a read can hold a removal
back but never allow one the `can*` flags refused. Details doesn't say whether HEAD is the pull
request's head (on GitHub for a fork's, though no remote-tracking branch has it), so a count of
commits equal to the listing's keeps the listing's `onGitHub`, and one that changed counts as not on
GitHub; `notOnGitHub` is that count, which the drawer's column and Force remove's list go by, each
saying where the rest are past the first 200 (`git log`).

Every clone shares one grid, `worktreeGridClass` in `worktrees/layout.ts`, of nine columns, the
verb's apart from the tools' so a launch's control never widens them; each `CloneSection`, its
`<ul>`, and each `WorktreeLine` row is a `grid-cols-subgrid` of it, so the columns line up down the
page, and the rows follow the grid's width (`@container`), as the Release rows do. Nothing in the
grid clips, for the sticky headings and the menus' panels.

A row's disclosure is its chevron, a `<button>` with `aria-expanded` and `aria-controls` whose keys
unfold (→) and fold (←) it and move between rows (↑ ↓); the `<li>` takes a pointer's click as a
shortcut for it, as a Release row does, and leaves alone a click that ends a text selection or lands
on a control, a menu, or a dialog a control renders elsewhere, whose clicks still bubble through
React. The page keeps the open drawers as a set of paths, which outlives a listing. App hands it
`onQuery`, which a chip writes the toolbar's filter with, the sessions snapshot, the inbox's items,
and `launchFor(item, place)`, `LaunchControls`' `row` variant placed in the row's cells
(`LaunchPlace`).
