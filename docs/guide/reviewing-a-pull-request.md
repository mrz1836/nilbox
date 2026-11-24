# Reviewing a pull request

[Docs](../README.md) › [User guide](../README.md#user-guide) · Design: [`internal/review`](../architecture/review.md), [`internal/timeline`](../architecture/timeline.md), [`internal/people`](../architecture/people.md), [`internal/assignees`](../architecture/assignees.md), [`internal/heldruns`](../architecture/heldruns.md), [`internal/bump`](../architecture/bump.md), [`internal/avatars`](../architecture/avatars.md)

Clicking a pull request's title opens the **review sheet**, a large panel over the inbox, so you
can read a pull request and act on it without leaving Nilbox: approve a Dependabot bump you can
judge in five seconds, merge, ask for changes with comments on its lines, or ask Claude first.
Every card gets it, in any list.

## What it shows

- **The header:** the title, `owner/repo#n`, who opened it with the same trust tag the card has,
  which branch merges into which, and where it sits in its list, as in **3 of 8 in Yours**.
  **←** and **→** (or the arrow keys, while you aren't typing) step to the previous or next card
  in the same list and tab, in the order the list shows them: only the cards the
  [filter](filtering.md) matches, sorted by project status when the list is. Pull requests and
  issues come in turn, and the sheet becomes the [issue sheet](reading-an-issue.md) for an issue;
  filter `is:pr` to step through pull requests alone. Each arrow names the card it steps to in
  its tooltip. A card a refresh stops matching keeps its place while the sheet is open on it.
  Before the arrows sit the card's [triage](snooze-dismiss-pin.md) buttons: snooze, with its
  length menu, dismiss, and pin, or **Unsnooze**, **Restore**, or **Unmute** in those tabs. One
  that takes the card out of its tab moves the sheet on to the next card, and the toast, with
  **Undo**, shows over the sheet. **↗** opens it on GitHub, and **✕** or **Escape** closes the
  sheet; with a menu open, **Escape** closes the menu alone.
- **The files** on the left: each path with its additions and deletions, a dot for a
  [sensitive](sensitive-files.md) category, and a tick once you mark it viewed. Clicking one scrolls
  to its diff.
- **The description**, rendered from markdown. Images show as links labelled with their alt
  text, and HTML is stripped, so an outside contributor's description can never make the app
  load anything from another host.
- **The diff:** unified, with added and removed lines tinted. Lock and generated files (such as
  `go.sum`, lockfiles, `vendor/`, `dist/`, and `*.min.js`) and files with more than 400 changed
  lines start folded. A binary file, or one GitHub won't show, says so and offers ↗.
- **Three tabs** hold the larger column: **Files** (the description and the diff), **Comments**
  (the conversation), and **Commits**, each with its count. The sheet opens on the tab you last
  chose, and each tab keeps its place while another shows.
- **The commits,** in **Commits**: the newest 100, oldest first, each with its short SHA, its
  headline, its author, and how long ago it was committed; click one to open it on GitHub. A
  pull request with more says **N earlier commits on GitHub ↗** above them. The commit your own
  standing review was left on is tagged **You requested changes** or **You approved**, and a
  **Since your review** divider marks the commits pushed after it, which the tab scrolls to.
- **Since your review:** while the pull request is open and its author pushed after you
  approved or requested changes, a line above the tabs says so, as in **3 commits since you
  requested changes**, with **Show commits**. When the branch was rewritten since, as by a
  rebase, it says **The branch was rewritten since you requested changes**, in amber, and the
  Commits tab links to the force push in the conversation. It goes by where your reviewed
  commit is in the branch, never by dates, which a rebase keeps; commits you pushed yourself never
  count, so a merge from **Update branch** counts only when someone else clicked it.
- **The summary** on the right: a one-line readiness sentence (**Ready to merge**, **An approval
  · checks running · auto-merge available**, **Behind main**, **Conflicts with main**), which
  stays in view at the top of the column while the rest scrolls, with the pull request's
  worktree and agent session under it, as its card shows them; a chip for the kind of change
  and the folders it touches (as in **in wallet/ and the top level**); the dependency it bumps
  (as in `golang.org/x/net 0.30.0 → 0.31.0 · minor`, a major in amber); the card's sensitive-file
  chip; **Closes**, the issues it closes once it merges (one already closed says how, as in
  **closed as not planned**), each opening in the [issue sheet](reading-an-issue.md) when it is in
  your inbox; each reviewer's latest review, and who it still asks (see [Reviewers](#reviewers)
  below); who it is assigned to (see [Assignees](#assignees) below); and the checks, listed open
  with the failing ones first, then those running, then those
  passed, the required ones marked, and the skipped ones folded away below them under their count
  (click a count to open or fold it). A check that is queued
  or in progress has a ring swelling slowly out of its amber dot, as a working session's dot
  does on its card, and so does the count of those running (**43 checks · 5 failing · 5
  running**), so a folded list still shows that CI is at work. With reduced motion, the dots
  hold still.
- **The latest agent session** on the pull request, with what it last said. **Show all** reads
  the whole message from the agent's own files on this Mac, and **Use as review comment** puts it
  into your review's text, to edit before you send it.
- **Your review**, at the foot of the right column and always in view: the text, a **Replies**
  menu of your [review replies](prompts.md#review-replies), the line comments waiting to go, the action
  button, and **Open in Claude** (or **Open in Codex**: the item's launch button, with its
  menu).

## Opening is instant

The sheet opens from what it read last time, with no request, then reads the pull request from
GitHub in the background (**Checking GitHub…**): one GraphQL request, and its files only when it was
pushed to since. Opening one you read in the last 20 seconds asks nothing, unless its card's checks
or held runs changed since. The refresh button in the header reads it again whenever you click it,
however recent the last read (a check re-run on GitHub shows up), and a light glides along the
header while it reads. When the inbox has seen a push the sheet hasn't, it says so and offers
**Reload**. Right after a push, GitHub can still be working out whether the pull request can merge
(**Checking whether it can merge**); the sheet then asks again, up to twice, two seconds apart.

## It keeps up with its card

The sheet never shows an old copy as current. Whenever the inbox stores something new of the pull
request (its whole refresh, the checks follower while checks run, a push you made from this Mac),
the open sheet builds itself again from what it read and what the card says now, with no request:
the title, labels, draft, review requests, and review decision follow the card, and so do the
summary's check count, the readiness line, and auto-merge whenever the card's read is newer than the
sheet's. The same goes for a sheet you open: its count and readiness are never older than the
card's. The list of checks, the held runs, and the conversation stay as the sheet last read them, so
while the count has moved past them they say when, as in **Checks as read 14m ago**. While the sheet
reads them again, those it read more than five minutes ago, or before the card's newer read, fade
under **Reading checks… · read 14m ago** until GitHub answers; a read that fails leaves them marked
with their age beside the error. A pull request with more than 100 checks lists the first 100 and
says how many more GitHub has, with **Open on GitHub**.

## One read when it matters

When the card shows the checks' state, a check count, the held runs, or the head moved since the
sheet's last read, the open sheet reads the pull request from GitHub once more on its own, for the
same one point as opening it, so the list, the held runs, and the conversation catch up. It never
reads within 20 seconds of its last read (it waits out the rest), never on a timer, and one read at
a time: however often the card changes while one runs, at most one more follows it. A card that
changed nothing about the checks or the head (a new label, a review request) costs nothing, a read
that fails isn't made again until the card moves on, and a closed sheet reads nothing: the inbox's
refresh never reads a sheet's checks list for you. Coming back to Nilbox's window with a sheet open
reads it once more too, for a point, when its last read is more than five minutes old; within five
minutes, or while a read is under way, it asks nothing.

## Merged or closed since the last refresh

A card can stay in the inbox until the next refresh after its pull request was merged or closed on
GitHub. When the sheet's read finds that, the sheet doesn't show it as if it were open: opened on
it, the sheet closes; stepped to it with **←** or **→**, it steps past it the same way, or back to
the card you came from when it was the last; reached by a link, it goes back to the sheet the link
was in. A toast says what became of it, as in **acme-org/api#12 was merged**, with **Open on
GitHub**, and the card leaves the inbox at once: that read already stored it, so finding out costs
no request more. The same happens when a routine merges the pull request you have open, or a refresh
finds it merged or closed: its card leaves the inbox, the sheet builds itself again from what Nilbox
stored, sees it ended, and moves on with that toast, again with no request. While an action you
started runs, or the sheet shows what one left (as **pushed to since you opened it**), it stays
open, saying **No longer open**.

## One button

The action button runs the action that fits the pull request, and says exactly what it does, as in
**Approve & squash-merge**:

| When the pull request… | The button |
|---|---|
| can merge now (checks green, no review needed, or approved) | **Approve & merge**, or **Merge** once you approved this commit |
| can merge now, with only optional checks failing (**Ready to merge · 1 optional check failing**) | **Approve & merge**, whose confirmation asks you to accept the failing checks |
| can merge now, with workflow runs that never ran a check (**Ready to merge · 3 workflow runs awaiting approval**) | **Approve & merge**, whose confirmation says what those runs are and that every check that ran passed |
| only waits for your approval | **Approve & merge** |
| waits for required checks still running | **Approve & auto-merge** |
| has a required check failing | **Comment**, with the launch button (**Open in Claude**) beside it |
| is behind its base, and the rules want it up to date | **Update branch**; on a Dependabot pull request, **Ask Dependabot to rebase**, since Dependabot stops maintaining a branch someone else pushed to |
| has conflicts, from Dependabot | **Ask Dependabot to rebase** |
| is your own | Merge or auto-merge, never Approve or Request changes. When the rules want an approval, the sheet says it needs one **from someone else**, since GitHub never lets you approve your own, and **Merge when ready** warns that it waits for one |

The menu beside it lists every action, with why one can't run now (a draft, a merge queue, no
write access, a pending review of yours on GitHub, and so on). **⌘↩** runs the button. **Request
changes** and **Comment** need text or a line comment.

## Bringing a branch up to date

The menu offers **Ask Dependabot to rebase** on every open Dependabot pull request, whatever its
merge state, so one held back by a failing check can pick up the fix from its base. It runs once the
sheet has read the pull request from GitHub, for an account with push access (Dependabot takes
commands from no one else), while the branch is behind its base or has conflicts with it; otherwise
the menu says why not, as in **It's up to date with main, so there's nothing to rebase.** The menu
offers **Update branch** whenever GitHub says you can update the branch (it is behind its base and
you may push to it), even when the rules don't need it up to date and the pull request reads as
blocked or ready, and, with why not (**Your account can't update this branch.**), whenever the
branch is behind and you can't, but on Dependabot's. Both write to the branch, so outside the rows
above neither is ever the button: it keeps its own action, and when nothing else can run it reads
**Choose an action** and only opens the menu, and **⌘↩** does nothing until you pick one. **Nothing
to do here now.** shows only when no action can run.

## Behind its base

While an open pull request's branch is behind its base, a strip under the sheet's header says so, as
in **12 commits behind main**, or **Behind main** when GitHub couldn't count them, beside **Update
branch** (on Dependabot's, **Ask Dependabot to rebase**), or why you can't. The count comes with the
sheet's own read, at no extra cost: GitHub compares the base branch with the pull request's head, a
fork's included. The strip is amber while the rules hold the merge back until the branch is up to
date; otherwise it only says so, since GitHub merges a branch behind its base all the same there.
**Update branch** merges the base into the branch, as GitHub's own button does; it never rebases it.
Clicking the strip's button runs the action as picking it from the menu does: one that asks first,
as on a Dependabot branch, opens its confirmation.

## Every merge asks first

Merge, Merge when ready (auto-merge), Approve & merge, and Approve & auto-merge open a confirmation
with the merge methods the repository allows, the commit it merges, and any warnings to accept
first: optional checks failing, or sensitive files of the danger level. **Cancel** is where the
focus starts. Approve, Comment, Request changes, Update branch, Ask Dependabot to rebase, and
Re-request my review are one click, but any action with a warning asks first, in a confirmation of
its own with no merge methods. **Update branch** on a Dependabot pull request does: its confirmation
warns that Dependabot stops rebasing a pull request once someone else changes its branch, and that
asking Dependabot to rebase keeps it managed, and it updates only once you tick **I've looked,
update anyway**.

## When nothing listed is failing

GitHub calls a pull request it would merge, but whose head commit has something not passing,
*unstable*. That something isn't always a check: a workflow run that never ran a job, such as a
fork's run waiting for a maintainer to approve it, or one that failed to start, counts too, though
no check shows it. The sheet names those runs beneath its checks list (**3 workflow runs awaiting
approval: Labeler, Deploy preview**), the readiness line counts them, and the merge's confirmation
says that every check that ran passed and that GitHub merges it as it is, so accepting it is no
bypass: merging past the rules is the separate **Merge past the rules**. They're read in the same
request as the checks.

## Approving held workflow runs

GitHub holds a fork's workflow runs until someone with write access approves them, and its overall
check state leaves them out, so it can read as passed while nothing of the pull request's own CI has
run. The inbox card marks them instead (**4 held**, in the warning color, and `checks:awaiting`
finds them), and the sheet names them in a bar under its header, each a link to its run on GitHub,
with **Approve workflows to run**. It approves each run Nilbox read on the commit you saw, one REST
request each, and asks first when the pull request changes workflow files, since the runs use its
versions of them. The sheet then stays open, with your draft, and reads the pull request again until
the runs' checks start (at most five reads, a few seconds apart), which also moves the card to
running checks.

## Runs only GitHub can approve

GitHub takes an approval from an app only for a fork's own runs (those a push to the pull request
started) and for runs the Actions bot queued; any other run it holds, such as one a review started
in the base repository, waits for approval on GitHub. Nilbox tells them apart from what it already
reads, with no request of its own, and such a run doesn't count toward where the pull request
stands: often it never runs at all, as a workflow a review starts on every fork's pull request. Its
card reads its checks as they are (23 passed, a tick), so an approved, mergeable pull request reads
**Approved · ready to merge** with **Merge**; the sheet's readiness says **Ready to merge** without
**· 1 workflow run awaiting approval**, the bar leaves it out, and the checks list names it quietly,
a line a run: **Auto-merge Trigger · held on GitHub · Open on GitHub**. The merge's confirmation
still says it, since GitHub still counts it. **Unless GitHub reports the merge blocked**: then the
run may be what blocks it, so it counts again, the card shows the clock on its track and **Checks: 1
workflow run waiting for approval on GitHub** rather than ready, and the bar lists it, as waiting
for approval on GitHub, with **Open on GitHub** (the run, or the pull request's checks when there
are several), no button, and **GitHub reports the merge blocked, so it may be what holds it back**.
Should GitHub still refuse an approval for what the run is, the bar says so in a short note
(**Auto-merge Trigger can only be approved on GitHub**), counts the run as held on GitHub until
Nilbox quits, and never sends it again. Any other refusal shows what GitHub said, and the run's link
is where to approve it by hand.

## Ignored workflows

Each held run's line, in the bar or the checks list, offers **Ignore this workflow** (**Ignore
{name}** for each of several), which adds its workflow's name to **Settings → Reviewing → Ignored
workflows**, with a toast that can undo it. A run of an ignored workflow never counts, whichever
kind it is, even when GitHub reports the merge blocked: the card counts without it at once, from
what Nilbox already stored, with no request, the sheet names it quietly with **· ignored**, and it
is never offered to approve (approve it on GitHub). Settings lists the names, each with a button
that counts it again, and takes one typed in; a name matches a workflow's whole name, case and all.

## Your text goes with a merge

Merge, Merge past the rules, and Merge when ready post the text in **Your review**, and any line
comments, as a comment before they merge, so the pull request shows it was reviewed, your own
included, where GitHub takes a comment but never your approval. A note under the button, and the
confirmation, say so (**Your text is posted as a comment first.**); clear the text to merge without
one. The comment goes to the commit you saw, the way **Comment** sends one, and only once the merge
state Nilbox reads first says the merge can go ahead: for **Merge**, that GitHub would merge it now;
for **Merge past the rules**, that GitHub still lets you; for **Merge when ready**, that it is open,
not a draft, and at the commit you saw. So a pull request that was pushed to gets no comment, and
neither does one a plain Merge finds in conflict or blocked. The merge then runs exactly as it would
without it. If the comment fails, nothing merges. If GitHub then won't merge, or anything else
fails, the toast says the comment is posted and why it didn't merge, as in **Commented on
acme-org/api#212, but it wasn't merged: Required status check is expected**, and the text leaves
your draft, so a second try doesn't post it again. While you have a review of the pull request
started on GitHub, which holds back every comment, the button waits and says so: submit or discard
that review there, or clear your text.

## Closing without merging

The menu offers **Close pull request**, set apart at its foot, on an open pull request GitHub says
you may close (your own, or anyone's where you have triage access or more). Until the sheet has read
the pull request from GitHub, it waits, greyed. It is never the button: picking it from the menu
opens its confirmation at once, it never becomes the action the button runs, **⌘↩** never closes,
and when nothing else can run, the sheet still says **Nothing to do here now.**, with the close in a
menu beside it. Text you wrote in **Your review**, or a reply you picked, goes with it, along with
any line comments, as a comment posted before it closes, the way a merge posts one: to the commit
you saw, and never as an approval. A default reply you never touched doesn't go. The confirmation
names the pull request, says when your text is posted as a comment first, and notes that you can
reopen it on GitHub. **Cancel** and Escape close nothing. GitHub takes no commit to pin a close to,
so Nilbox reads the pull request first (1 GraphQL point), and again before a second try with a fresh
token: one closed, merged, or pushed to since you opened it gets neither the comment nor the close.
If the comment fails, nothing closes. If the close then fails, or a push lands before a second try,
the toast says the comment is posted and why it didn't close, as in **Commented on acme-org/api#212,
but it wasn't closed: It was pushed to since you opened it**, and the text leaves your draft.
GitHub's refusal reads as one of a close, as in **GitHub wouldn't close acme-org/api#212: …**.
Otherwise the toast says **Closed acme-org/api#212 with your comment** (or **Closed
acme-org/api#212**).

## Merge method

**Settings → Reviewing** chooses the method the confirmation starts on: **As each repository offers
first** (the default, GitHub's own first choice for you there), **Squash and merge**, **Create a
merge commit**, or **Rebase and merge**. A pull request whose auto-merge is on starts on
auto-merge's method instead, so merging it now, past the rules or not, merges as auto-merge would. A
repository that doesn't allow the one you chose starts on the one it offers first. Every label names
its method, **Squash-merge**, **Rebase-merge**, or **Merge-commit** (as in **Approve &
merge-commit**), and the confirmation's button follows the method you pick there, as in
**Merge-commit past the rules**. Beneath it, **Merge your own pull requests past the rules** (off by
default) makes the button **Squash-merge past the rules** (or your method) on a pull request of
yours that only an approval from someone else holds back, where GitHub lets you merge past the
rules; see **Merge past the rules, only on purpose** below.

## Reviewers

The summary lists the pull request's **Reviewers**: who reviewed it, each with their latest review
(as in **approved · asked again** for someone it asks again), then the people and teams it asks who
haven't reviewed (**awaiting review**). With triage access or more to an open pull request the sheet
has read, **Edit** opens the reviewer picker: who it asks now under **Requested**, then **Suggested
for this pull request** (whom GitHub suggests, as the sheet last read them, with **Changed these
files** or **Reviewed these files**), the people who work on the repository (**Works on
acme-org/api**, each with a word on why, as in **Reviewed 12 here · 3d ago**), and its **Teams** (as
**acme-org/platform**). Past who it asks now, it never offers the author, you, or a bot, and lists
at most 100 people. Typing in **Filter people and teams** filters them at once on this Mac, by
login, name, or slug, those that start with what you typed first. A note under the list says where
the people come from, as in **Contributors to acme-org/api · from 50 pull requests, 2d ago**, or **·
from the pull requests you've viewed** before Nilbox has read the repository.

The picker opens on what Nilbox kept, with no request. When the repository is due a read (it was
never read, or last read a week or more ago, and no read of it failed in the last 30 minutes), the
picker reads it once as it opens (**Learning who works on acme-org/api…**): 1 GraphQL point for its
50 most recently updated pull requests, and, for an organization's repository, 1 REST point for its
teams. The list then fills in place, keeping what you checked. **Apply** (named for what it does, as
in **Request 2 · Remove 1**), or **⌘↩**, asks everyone you added in one REST request, people and
teams together, then takes everyone you unchecked off in one more, and the card and the sheet show
GitHub's answer at once, without reading the pull request again. What GitHub made stands when the
second request fails, and the picker stays open and says why, as it does when GitHub refuses a
request (for someone without access to the repository, say). In the summary, hover a request for
**×** to take it off, or someone who reviewed and isn't asked now for **↻** to ask them again
(never you, the author, a bot, or a team); while no one has reviewed it or is asked to, it says
**No one is asked to review yet.** and offers up to three of the picker's people, each asked in one
click, and **More…** for the picker. Your own request stays checked in the picker: ask for your
review again with [Re-request my review](#re-request-my-review), and take yourself off with
[Remove me as reviewer](#remove-me-as-reviewer). The people you ask rank higher among those who
work on the repository from then on ([People](#people)).

## Assignees

The summary lists who the pull request is assigned to, under its reviewers, each with their picture,
yours marked **(you)**. With triage access or more to an open pull request the sheet has read,
**Edit** opens a picker of the same kind as the reviewers': who it is assigned to first, then
**Suggested** (you, its author, the people who took part in it, and whoever was assigned in the
repository before), the people who **work on** the repository, the others GitHub lists as **can be
assigned** there, the people who work on the owner's **other repositories**, and those a search
**found on GitHub**, each with a word on why. Typing filters them at once on this Mac. While
Nilbox's list of who can be assigned is cut short (a repository with more than 100), a pause in
typing searches GitHub too, but only when nothing Nilbox kept answers the words: a search that named
everyone it matched answers every longer one for a week, and the same words again for ten minutes.
The picker reads who can be assigned in the repository once a week at most, as it opens. **Apply**
(named for what it does, as in **Assign 2 · Unassign 1**) takes everyone off in one request, then
assigns everyone added in one more, so a swap works at GitHub's cap of ten, and the sheet shows
GitHub's answer at once, without reading the pull request again. Hover a line for **×** to unassign
that person; with no one assigned, the summary offers **Assign yourself**, the author, and one more
in one click, and **More…** for the picker. You take yourself off with **Unassign me**, in the
card's menu, never here; your own row in the picker stays checked. A pull request read by an older
Nilbox shows its assignees once it is read again. Once GitHub takes it, the card shows at
once that you assigned yourself.

## People

**Settings → Reviewing** → **People** says how much Nilbox learned of who works on each repository
and who can be assigned there (as in **12 people across 3 repositories**), which the reviewer and
assignee pickers suggest from without asking GitHub. It learns from the pull requests and issues you
open, and from the pickers' own reads, and keeps it on this Mac. **Forget…**, after you confirm,
empties it: the pickers start again from what Nilbox reads next, and read who can be assigned in
each repository again as you open them. Nothing on GitHub changes.

## Nudges

**Settings → Reviewing** → **Nudges** decides when a pull request you requested changes on, waiting
on its author, offers **Nudge** on its card. **Offer a nudge after** is how long it has to be quiet
first: **Off**, **12 hours**, **1 day** (the default), **2 days**, **3 days**, or **1 week**. Any
push, comment, or review starts the wait again, and so does your own nudge; a bot's pull request
never offers one. **Snooze after a nudge** (off to start with) starts each nudge with **Then
snooze** ticked, for your snooze length. A note under them links to **Prompts → Review replies**,
where the **Nudge** reply the comment starts from is kept.

## Safe by construction

- **Pinned to the commit you saw.** Every merge names the head commit as the SHA the head must
  still be, and auto-merge and Update branch name it as the head they expect, so GitHub refuses
  them after a push. GitHub would take a review of an older commit, and an approval of one would
  count toward merging commits you never saw, so an approval first reads the head (1 GraphQL
  point) and isn't sent once someone pushed. Either way nothing changes, and the sheet stays
  open, with your draft, to reload. A comment or a request for changes is about the code you
  saw, so it goes to the commit you saw, its line comments with it.
- **No silent admin bypass.** For an admin, GitHub's REST merge quietly merges a pull request
  that branch protection blocks. So every merge first reads the merge state fresh (1 GraphQL
  point, up to three reads a second apart while GitHub is still working it out) and merges only
  when GitHub says it can (**CLEAN** or **HAS_HOOKS**, or **UNSTABLE** once you accept the failing
  checks). Otherwise it turns on auto-merge where the repository allows it, after an approval, or
  says why not.
- **Merge past the rules, only on purpose.** When GitHub lets you merge past a branch's rules,
  as it lets a repository admin, and the rules are what hold the pull request back, the action
  menu offers **Squash-merge past the rules** (or your method). It is never the main button,
  unless you turn on **Merge your own pull requests past the rules** in **Settings → Reviewing**:
  then it is the button on a pull request of yours that waits only on an approval from someone
  else (its required checks passed, no request for changes stands, and GitHub reads it as
  blocked, not behind or in conflict), where it would otherwise read **Merge when ready** and
  wait for a review that never comes. Clicking it, or pressing ⌘↩, still opens the same
  confirmation. Its confirmation lists each rule it skips (**It needs an approval from someone else.**, **1
  required check is failing.**, **It's behind main, and the rules want it up to date.**), and
  merges only once you tick **I'm merging past the rules**. It still reads the merge state first:
  it refuses after a push, on conflicts, or once GitHub no longer lets you, and when GitHub would
  merge anyway by then, it's a plain merge. The toast says **Merged acme-org/api#212 past the
  rules (squash)**. Nothing else in Nilbox merges past the rules but a routine you allow, on your
  own signed pull request that lacks only an approval ([how](autopilot/merge-and-post-review.md)),
  and no agent can.
- **A step GitHub made stands.** An approval GitHub took and a merge it won't make yet comes back
  as **Approved, but GitHub won't merge yet: 1 required check failing**, never as an error. A
  token GitHub refuses midway is borrowed again, and the approval is not sent twice.
- **The same pipeline as every edit:** one action per item at a time, the [account
  check](item-menu.md), the pace limits, and the API budget.

**While it works** the sheet dims and takes no input, and a panel ticks off each step as GitHub
finishes it (**Checking your account**, **Checking nothing was pushed since you opened it**,
**Approving**, **Checking that GitHub can merge it**, **Squash-merging**), with the one under way
marked. The sheet can't be closed or stepped away from until the action ends, so its outcome is
never lost.

**After acting** the sheet closes and the inbox's toast reports what happened, as in **Merged
acme-org/api#212 (squash)**, **Commented and merged acme-org/api#212 (squash)**, or **Approved
acme-org/api#212 · auto-merge is on**. A review takes the
pull request out of **Review requested** (into **Yours**, when you opened it or are assigned to
it), and a merge or a close out of the inbox, at once, with no extra request. That includes a
pull request there only because its author pushed since you requested changes: approving it,
or requesting changes again, ends its **Re-review** too.

## Re-request my review

Once a pull request's author pushed since you requested changes, the strip under the sheet's header
that says how many commits came since offers **Re-request my review** beside **Show commits**, and
so does the action menu, at its foot. It asks GitHub to request your review again, once, as GitHub's
own re-request does, and puts the pull request back in **Review requested** at once, as an ordinary
request, its card's story **Review re-requested** in place of **Re-review**. It dismisses nothing:
your request for changes stands until your next review replaces it. It is never the button and sends
none of your text, so the draft stays for that review. It isn't offered after an approval, with
nothing pushed since, or on your own pull request; it can't run while GitHub already asks you
(**GitHub already asks you to review it again.**), or for an account without write access, since
GitHub asks only collaborators to review.

## Remove me as reviewer

While a pull request asks for your review by name, hover your own **awaiting review** line under
**Reviewers** for **×**, or pick **Remove me as reviewer** at the foot of the action menu; the
card's menu offers the same. It needs no triage access, since anyone asked may take themselves off,
and costs one REST request. While it runs, the sheet shows the step and takes no input; once GitHub
takes you off, the sheet closes and a toast confirms it, naming the team the pull request still
asks when it asked one too. When GitHub refuses, the sheet stays open and says why. A request made
only to a team you are in offers neither, since GitHub takes a team's request off for every member
or for none ([the item menu](item-menu.md)).

## Line comments

Click a line number in the diff to comment on that line; shift-click another in the same part of the
diff for a range. **Suggest a change** (on the new version only) starts a ` ```suggestion ` block
with the lines as they are. A comment waits in your draft, shown under its line with **Edit** and
**Delete**, and goes with the review. Nilbox checks each comment against the diff before GitHub sees
it, so GitHub never refuses one for a line it doesn't show.

**Drafts** are kept per pull request for the session: closing the sheet, or stepping to another
pull request and back, keeps your text, chosen reply, line comments, and viewed files. A draft
is cleared once its action succeeds, except **Update branch**, **Ask Dependabot to rebase**, and
**Re-request my review**, which leave it for the review to come.

## What it costs

Opening costs nothing; a refresh with nothing pushed, 1 GraphQL point; the first open of a pull
request with up to 100 files, 1 GraphQL point and 1 REST point; each write, what the [edits
table](item-menu.md) says. Nothing is fetched ahead, since the sheet closes after acting.

## What it keeps

The sheet keeps what it read of each pull request in the app's database on this Mac, diffs included:
at most 8 MB of diffs per pull request, 64 MB and 100 pull requests in all, the least recently
opened dropped first, anything unopened for 14 days dropped, and a pull request's own dropped once
it merges. Nothing leaves your Mac but the requests to GitHub's API.
