# Release

[Docs](../README.md) › [User guide](../README.md#user-guide) · Design: [`internal/release`](../architecture/release.md), [The Scripts tab's pages (frontend)](../architecture/frontend/scripts-pages.md)

**Release** lists every clone in the [repository locations](launching-a-session.md#repository-locations) whose `origin`
is on GitHub, which is where magex pushes, and releases a new version of any of them with magex's
`version:bump`. The app itself never tags or pushes: magex does, when you click a button that
names the version.

## The page

- **Groups:** **Unreleased changes** (the default branch has commits since its newest version
  tag), open; **Up to date**, **No releases yet** (no `vX.Y.Z` tag), **Can't release here**
  (archived, read-only to you, not found, or with no commits on GitHub yet), and **Muted** (the
  repositories you [muted](snooze-dismiss-pin.md#pinned-and-muted-repositories-and-orgs), each with **Unmute**, which
  GitHub isn't asked about), collapsed, each with its count. A group you open or fold stays that
  way, when you come back and after a restart.
- **Readiness at a glance:** the heading of a group with **Release…** (Unreleased changes and No
  releases yet) sums up its rows after the count, what needs a look first: *2 failing*, *3
  blocked*, *1 running*, *13 ready*, *1 without checks*, in that order, each after a dot in the
  color of those rows' edges (below). A count of zero is left out, and one that doesn't fit on
  the heading's line is dropped whole rather than cut short.
- **Sort:** **Newest** (the newest commit first, to start with), **A–Z** (by `owner/name`,
  ignoring case), or **Commits** (the most commits since the newest version first) orders
  every group, and the repositories you [pinned](snooze-dismiss-pin.md#pinned-and-muted-repositories-and-orgs) come
  first in each, whichever you pick. Commits breaks a tie by the newest commit, and Newest by
  name. Sorting asks GitHub nothing, and the order you pick stays after a restart, though a
  [backup](backing-up.md) doesn't hold it.
- **Search** filters every group, opening each while it searches. **Read GitHub again** reads the
  whole list again now. Nothing else does on a timer: the list stays as it was read, however
  old (the header says *Read 3h ago*), and after a release, or a merge in the review sheet, only
  that repository's row is read again. When GitHub can't be reached, the last list stays, marked
  *Couldn't reach GitHub · 14m old*, until you read it again. Clones without a GitHub origin are
  counted at the foot.
- **While it reads,** the page says how far it has got: *Finding your clones…*, *Asking GitHub:
  45 of 151 repositories…* (GitHub answers 15 at a time), *Asking GitHub what changed: 12 of 40
  repositories…*, then *Checking your clones: 3 of 12…*.
- **After a restart,** the page shows the list from the last time it was read at once, marked
  *From 2h ago · checking again…*, while it reads GitHub again behind it, and then replaces it.
  A list read from other repository locations isn't shown.
- **Recent** lists the last 10 releases started from the app, with how each came out (*on
  GitHub*, *on GitHub, following its workflow*, *published*, *release workflow failed*, *on
  GitHub, not published*, *not pushed*, *failed*, *not checked on GitHub*); each opens its run.

**Each row** is one repository on one line, its parts in columns its whole group shares, so
they line up down the list. When the list is narrow, as in a small window, the headline and
your clone's notes move to a second line under the name, and narrower still the version and
the age join them there. From the left:

- **The edge** says how ready the repository is to release, on a row with **Release…**: red
  while the checks on its newest commit fail, amber while your clone stops a release (below), a
  grey pulse while the checks run, green once they passed with nothing in the way, and muted
  when the commit has no checks, which never reads as ready. A row both failing and blocked
  shows red. Checks that are done but for a run GitHub queued and never started, two hours
  after the commit (as an app's check GitHub never runs, which the Actions page doesn't
  list), read as passed: green, without the pulse.
- **The checks** on the default branch's newest commit, as an icon (✓ passed, an amber ✓ when
  they passed but a run never started, *checks passed, 1 never started*, a pulsing dot while
  they run, ✗ failing, or a ring for none), its words in its tooltip.
- **The repository:** its owner, muted, and its name, cut short with the whole in its tooltip.
  A pinned repository has a pin after its name, so the names still line up, and its tooltip
  says which rule pins it and where to unpin it.
- **The version:** the newest version tag, or *no version yet*.
- **+N**, on a row with unreleased changes: how many commits came since that version, with a
  **feat** chip when one of the newest 10 of them starts with `feat:`, which usually means a
  minor release, or **breaking** when one is marked breaking, which usually means a major one;
  the chip's tooltip says so, as the sheet's hint does. **+?** means GitHub couldn't compare the
  version with the default branch, so how many came since isn't known.
- **The headline** of the newest commit, taking the room the rest leave, with **×N** when 3 or
  more repositories listed in the group have it as their newest commit, as a change synced to
  many repositories does; its tooltip names them. A search that leaves fewer than 3 drops it.
- **Your clone's notes,** from read-only git checks of the unreleased clones. Only what stops a
  release is amber: uncommitted files (*3 uncommitted*), commits not on GitHub (*2 not
  pushed*), a detached commit, a version tag only on this Mac (*v1.4.1 only on this Mac*), or a
  clone git couldn't read. A row with any of those shows only them. Otherwise it says, quietly,
  that origin has commits your clone lacks (*behind*), then the branch it's on when that isn't
  the default, neither of which stops a release: magex pulls first, and switches to the default
  branch and back. Each note's tooltip says what to do, as in *Commit or stash them first.* or
  *magex pulls them first.*
- **The age** of the newest commit.
- **Release…** opens the sheet, and so does a click anywhere else on the row; each part of
  the row still shows its tooltip. A click that ends selecting the row's words opens nothing,
  so you can still copy them. Release… stays quiet until you point at the row or move the
  focus into it, so a column of them stays calm.

An **Up to date** row has no edge and no **Release…**, a **Can't release here** row says why in
the headline's place, without checks, and neither opens anything when clicked. A **Muted** row
shows its clone's folder there and **Unmute** in Release…'s place, quiet the same way; only
Unmute itself unmutes, never a click on the row.

**The sheet** checks the clone, origin, and GitHub as it opens, then shows:

- **Branch:** the repository's default branch on GitHub, with `main` or `master` beside it when
  origin has the other one; a branch that isn't the default says so.
- **The checks,** each with ✓, ℹ, ⚠, or ✗ and its words: magex, the clone and your access,
  git in the middle of a rebase or merge, uncommitted changes, where your clone is and whether
  git can switch, the branch following origin, commits not on GitHub, a version only on this
  Mac, reaching origin with your git credentials, something to release, the checks on the newest
  commit, another run holding the clone, signed tags, your shell's environment, and your hooks.
  A ✗ blocks the release; a ⚠ makes the button read **Release v1.4.3 anyway**. Fixes sit beside
  each: **Open Settings**, **Update magex in Settings**,
  [**Open in terminal**](launching-a-session.md#open-in-terminal) (your terminal in the clone,
  running nothing), **View on GitHub**, **Watch the run**, and **Copy** for a command the app
  never runs itself. Under the checks, one **Check again** reads them all afresh, whatever they
  say, with *Checking again…* beside it while it reads.
- **The checks on the newest commit** are GitHub's own rollup of them, which counts commit
  statuses (a coverage service's, say) as well as Actions runs, so it can fail while every
  workflow passed. A failing line names what failed (*codecov/project is failing on 4ce994b*;
  past three, two and a count), read in the same query as the rest, and **View on GitHub** opens
  the commit's checks, or the commit itself when a commit status failed, since the checks page
  lists none. A check run GitHub still has only queued two hours after the commit, with every
  other check done and none failing, never started: the line warns and names it
  (*.github/dependabot.yml never started on 4ce994b; the other checks passed*), and **View on
  GitHub** opens that run's own page, or the commit's checks when more than one never started.
  A running line's **View on GitHub** opens the commit's checks. For a failing, stuck, or
  running line, the sheet's **Check again** reads GitHub afresh.
- **What's in it:** the newest commits since the newest version, from GitHub (up to 10, three
  shown until you ask for the rest, with GitHub's comparison for any past those), each linked
  to its commit.
- **The version:** **Patch**, **Minor**, or **Major**, each with the version it makes. Patch is
  chosen every time the sheet opens. Major needs a magex that can be told which version to make
  (1.30.0 or later, with `expect=`); before that its card says so, with **Update magex in
  Settings**. Choosing Major asks you to type the exact version, with an empty field every time,
  and, for a Go module whose `go.mod` doesn't name the new major, to tick **Release anyway: this
  module isn't meant to be imported**. The service checks both again. Under the cards, a hint
  reads those commits' conventional-commit prefixes: *2 commits start with feat:, which usually
  means a minor release.*, or, for a commit marked breaking (`feat!:`, `fix(api)!:`, or a
  headline starting `BREAKING CHANGE:`), that it usually means a major one. It never changes
  the choice.
- **Runs in:** the app or your terminal, as Settings → Integrations sets, or the other one for
  this release only.
- **The sentence** says exactly what will happen, and **Command** shows the exact command, to
  copy. The button names the version: **Release v1.4.3**, **Release v1.4.3 in iTerm2**, or
  **Release v2.0.0 (major)**.

Choices decide the sheet again on the facts it read. Only opening it, **Check again**, and
coming back to the window while a check blocks or warns, or the sheet couldn't check at all (at
most every 10 seconds, never while a release starts or runs), read the clone, origin, and GitHub
afresh: push a tag only on this Mac from your terminal, come back, and its ✗ clears by itself. A
sheet whose checks all pass or only inform never reads by itself. **Release** reads the clone and
origin once more under the clone's lock before magex starts. When that refuses (the clone
changed, or someone released that version meanwhile), the sheet says why, checks again, and
shows the version it would make now.

**While it runs,** the sheet ticks off magex's steps (switched to the branch, pulled, tagged,
pushed, then the app's own check that GitHub has the tag), with the run's panel and log below.
The panel shows magex's command with your home folder as `~`; the log leaves out magex's JSON
result line, which the result shows, and the log file and **Copy log** keep it. Closing the
sheet never stops a release: it goes on under Runs. **Stop** is off while magex switches
branches and pulls (*Stopping now could leave main partly updated*), and asks first once the
tag is made, or while magex has your clone on another branch.

**The result** comes from what magex said and what origin and your clone say once it ended,
never from its exit status alone:

- **On GitHub:** the tag and commit, the branch your clone is back on, and **View v1.4.3 on
  GitHub**, **Watch the release workflow**, and **Copy v1.4.3**, with a burst of confetti
  (none when the system asks for reduced motion, or with [Celebrations](inbox-nil.md) quiet or
  off). A version that moved meanwhile, or a newer
  commit than the one you saw, is said too.
- **Tagged on this Mac only:** the push magex skipped or GitHub refused, the commands to push or
  undo the tag, to copy, and **Open in terminal**.
- **Failed:** what happened in words, magex's line that says so, where your clone is now, and
  **Try again**, which reopens the sheet with fresh checks. A release is never run again from
  its stored command.
- **Couldn't check:** *Couldn't check whether v1.4.3 is on GitHub*, when origin couldn't be
  asked once magex ended, so it may be there; **Try again** reopens the sheet, which checks
  afresh.

magex's warnings show with the result, all but the one it prints on every release with
something to release, that the newest version tag isn't on HEAD; the log keeps that one.

## Following the release

Once a release is on GitHub, the app follows the tag's release workflow until it publishes the
release, and the result says how it goes:

- **While it follows:** *Following the release workflow for acme-org/sdk v1.4.3…*.
- **Published:** the heading reads *acme-org/sdk v1.4.3 is published*, the line *The release
  workflow published acme-org/sdk v1.4.3* · **View** opens the release, and the run's summary
  and Recent say *published*.
- **A workflow failed:** *The goreleaser workflow failed for acme-org/sdk v1.4.3*, naming the
  workflow, · **View** opens its run, or the tag's Actions runs.
- **No workflow ran:** *No workflow ran for the v1.4.3 tag, so there's nothing to follow* ·
  **View** opens the tag's Actions runs, when none ran for the tag by the third check, about
  seven minutes in.
- **Not published:** *The tag's workflows finished, but GitHub hasn't published a release for
  v1.4.3* · **View** opens the repository's releases, where a draft shows, when every one of
  the tag's runs ended well and a check later there's still no release.
- **Stopped:** *Stopped following acme-org/sdk v1.4.3 after 45 minutes* · **View** opens the
  tag's Actions runs. Nothing is assumed: the release is only on GitHub.

It checks GitHub a minute after the release ends, then every three minutes, at most 20 times
over 45 minutes, whichever comes first, through the same client as everything else, so the
API budget gauge counts every request. Each check is one GraphQL query for the tag's release
and, until it is published, one REST request for the tag's own workflow runs: those a push of
the tag started at its commit, never the branch's own runs, another tag's, or a schedule's.
*Published* means GitHub has the tag's release published, not a draft, and wins over anything
the runs say. *Failed* means one of the tag's runs failed, timed out, or never started, once
every one of them ended: while one still goes, the release may still be published. Each check
is written into the release's run, so a follow cut off by quitting resumes once the app starts
again, within the same 20 checks and 45 minutes. A published release and a failed workflow are
recorded in the [notification history](menu-bar.md), with their banners when
those kinds are on, even when you quit just then; any other end only says so in its result.

## After a merge

When you merge a pull request in the review sheet, the toast that says so offers **Release
acme-org/sdk…** when Release can release it: the setting **Offer a release after you merge a pull
request** (Settings → Integrations, on by default) is on, mage-x can be used, the repository has a
clone with that GitHub origin, and the pull request merged into a branch Release offers. Asking
costs no GitHub request. The action opens Scripts → Release at that repository's sheet, its checks
already running; it never releases by itself. A release that reaches GitHub is also recorded in the
[notification history](menu-bar.md) as **Released**, with its banner when that kind is on, and then
as **Published** or **Release workflow failed** once its workflow says so.
