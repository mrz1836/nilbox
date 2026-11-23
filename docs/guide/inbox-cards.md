# Inbox cards

[Docs](../README.md) › [User guide](../README.md#user-guide) · Design: [`internal/inbox`](../architecture/inbox.md), [The inbox cards (frontend)](../architecture/frontend/inbox-cards.md), [`internal/avatars`](../architecture/avatars.md)

Each pull request or issue in the inbox is a card two lines high, built to answer three
questions at a glance: where it stands, what changed, and what to do next. **Settings →
Cards** can make every card one line instead (**Compact cards**, below).

- **The edge.** A bar down the card's left side says whose move it is: amber when something
  waits on you right now (an agent asking for approval, a routine asking for your OK, workflow
  runs held for you), red when it is stuck (conflicts, failing checks, an agent that failed),
  your accent when it is yours to do, green when it is ready to merge, blue while an agent or a
  routine works on it, and grey while it waits on others. In the Snoozed, Dismissed, and Muted
  tabs it has none.
- **Who and what.** The author's GitHub picture, in a circle for a person and a rounded square
  for a bot (an accent ring for your own), or until it arrives, and should it never, their
  initials on a color their login picks, or a bot's mark; with the pull request or issue mark
  at its corner, grey for a draft.
- **Line 1:** the title, after a blue dot when something happened on the item since you last
  opened it (**Unread dots**, below) and a pin when the item is pinned (hover the pin for who
  pinned it: you, a routine, or a rule for its repository or org) and **Draft** before a draft's
  title,
  then up to two labels in their GitHub colors and **+N** for the rest (hover it for their
  names). A label shows whole or folds into the **+N**, never cut off: the labels take the room
  the title leaves, or up to a third of the line beside a long title, and the first always
  shows. A long title is cut short; hover it for all of it. Clicking a pull request's
  title opens it in the [review sheet](reviewing-a-pull-request.md), and an issue's in the
  [issue sheet](reading-an-issue.md); **⌘-click** it, or click the small **↗** beside it, to open
  it on GitHub instead.
- **Line 2:** the story, one sentence in the edge's color that says why the card is here, such
  as **Your review · checks failing**, **Re-review · back early: 1 new commit**, **2 of 14
  checks failing**, **Approved · ready to merge**, **Claude needs approval**, **Waiting on the
  author · you requested changes**, or **Merges when green · squash**; in the Snoozed and
  Dismissed tabs, when it comes back or when it was dismissed, then, muted, where it stands, as
  its story would begin in its list (**Back tomorrow, 6:45 PM · Waiting on the author**).
  Then the byline: the repository and number, the author (**you** for your own; hover it for your login, and it still
  filters by the login), why it is yours (**via @team** for a review asked of a team,
  **assigned** when it is assigned to you, in either list), and how long ago it last changed. Then, only when there is something to say, one
  risk chip for who opened it and the [sensitive files](sensitive-files.md) it changes (such as
  **Outside · Workflows**, or **Untrusted bot · Not all files checked**; click it for the
  files), a link to the item GitHub ties it to (see **An issue and its fix**, below), the
  routine's ⚡ chip, and a worktree chip when the item's worktree, as the
  [Worktrees view](worktrees-view.md) last listed it, has uncommitted changes or a session
  running in it (hover it for the worktree's path). The card says nothing of your
  [notifications on GitHub](github-notifications.md): the toolbar's GitHub button lists them.
- **Where it stands.** A pull request's track: its checks, its review, and whether it can
  merge, each a circle with a tick, a cross, a clock for runs held for approval, a moving dot
  while checks run, ↻ once its author pushed since you asked for changes, a warning for
  conflicts, a bolt for auto-merge, or a ring while there is nothing yet to say. Under it, the
  comments when there are any, the size grade, and a five-cell bar of the lines added
  against those removed. An issue shows its status in its first project, that project's
  priority or size, and its sub-issues done out of all of them; nothing when it has none of
  these. A screen reader reads each step's sentence.
- **What to do.** One button for your next move, colored like the edge (and filled while the
  pointer is on the card) when it is yours to make, quiet when it isn't: **Review**,
  **Re-review**, **Read**, **Open**, **Merge** (which opens the review sheet, whose own
  button then merges), **Decide** (a routine's Needs you, in the Autopilot tab), **Show
  terminal** or **Watch** (an agent waiting on you), **What happened** (an agent that failed
  today), or **Unsnooze**, **Restore**, or **Unmute** in the tabs an item is set aside in.
  Beside it, the agent's: **✦ Claude** to [open a session](launch-defaults.md), named for its
  prompt where the card names one: **✦ Fix checks**, **✦ Resolve conflicts**, or **✦ Address
  review** on your pull request whose checks fail, that conflicts with its base, or that a
  reviewer asked to change, **✦ Re-review** on a pull request pushed to since your review
  (each the prompt your [launch defaults](launch-defaults.md) choose for that situation), and
  **✦ Work on it** on an issue. It carries its lock when the item
  [launches read-only](outside-contributors.md), and the arrow for the launch options; or, once
  a session runs, its live state (see [Following a session](following-a-session.md)).
- **Waiting on the author.** A pull request someone else opened that you requested changes on
  is the author's move until they push: its story says **Waiting on the author · you requested
  changes**, then its worst trouble (**you requested changes, conflicts with its base**), in
  grey, with a quiet **Open** and the agent's plain button. Once the author pushes, it asks for
  your **Re-review**, assigned to you or not.
- **Nudge.** Once such a pull request has been quiet for a day (**Offer a nudge after** in
  Settings → Reviewing, see [Nudges](reviewing-a-pull-request.md)), its story says so (**you requested changes, quiet for
  2d**) and its primary becomes **Nudge**, outlined in the accent color while its edge stays
  grey: it still waits on the author, but asking them is yours to do. **Nudge** opens a short
  dialog, **Nudge @author**, with the pull request, how long it has been quiet, and a comment
  already filled in from your **Nudge** reply in [Prompts → Review replies](prompts.md#review-replies);
  **Replies** picks another of your comment replies, and **Manage replies…** opens them. **Post
  comment** (or ⌘↩) posts it on the pull request's conversation, and **Then snooze for 3 days**
  (your snooze length; ticked to start with when **Snooze after a nudge** is on) snoozes it
  after, until it comes back early when the author pushes. A toast says **Nudged @author on
  example-repo #434**, with **View comment**, or, snoozed, **Nudged @author and snoozed until
  Sat**, with **Undo**. The card then says **you nudged them just now** with a quiet **Open**,
  and offers **Nudge** again only once the pull request has been quiet that long since. Any
  push, comment, or review restarts the wait; a nudge from before your latest request for
  changes counts for nothing, so a new round of review starts clean; and a bot's pull request
  never offers one. The card's **⋯** menu offers **Nudge @author…** whenever a pull request
  waits on its author, in every tab and however long it has been quiet, and so does its
  details' action bar.
- **Approved by you.** A pull request someone else opened that you approved has nothing left
  for you to review: it says **Approved by you**, then its worst trouble (**Approved by you · 1
  check still running**), or **Merges when green · squash** while auto-merge is on, in grey,
  with a quiet **Open**, until it is ready to merge, when one assigned to you reads **Approved ·
  ready to merge** with **Merge**. Workflow runs held for your approval on it come first, in
  amber: **1 run awaits your approval · you approved**, with **Open**.
- **Asked again.** Either way, once your review is requested again it is an ordinary review
  request: **Your review** (or **Review re-requested** after a push).
- **An issue and its fix.** GitHub ties a pull request to the issue it closes once it merges
  when its description says so (**Closes #12**, **Fixes #12**) or someone links them in its
  Development panel. An issue's card then links to the pull request: **#14** after the
  byline, with the pull request's mark in its state's color (green while open, grey for a
  draft, blue once merged), **+1** for each more, and **↗** when your inbox doesn't hold it.
  A pull request's card links the same way to an issue in your inbox it closes. Hover the
  link for what it is (**Fix in progress: #14 Retry the upload**); click it to show that card
  in the inbox, highlighted, or open it on GitHub. While a pull request from you or from
  someone trusted with its repository is on its way to closing the issue, the issue's story
  says so, in grey with a quiet **Read**, since the work is the pull request's now: **Fix in
  progress** (**· draft** for a draft), or **Fix merged** once one merged and the issue is
  still open. Anyone can say their pull request closes an issue, from a fork too, so one from
  an outside contributor or an untrusted bot only links, marked **from outside the project**,
  and the issue's story stays its own (**Assigned to you**). The link costs nothing: the
  issue's details ask GitHub for it, at no extra point. Once the pull request merges, in
  Nilbox or anywhere a read sees it, the issues in your inbox it closes are read again five
  seconds later, and every five seconds while GitHub still has one open, three times at most,
  so a closed issue leaves your inbox with its fix rather than at the next refresh. An issue
  GitHub keeps open, as one whose fix merged into a branch other than the default, stays and
  says **Fix merged**.

**Hover a card** for its triage over the track: **Show details**, refresh it, snooze it,
dismiss it, pin or unpin it, and the menu of everything else. They stay while the card is busy
with GitHub.

## Show details

Click a card's empty space, or **Show details**, to open it in place, under its two lines, with
everything the card leaves out, read from what the inbox already has, so it costs no request: where
it stands (the checks as a bar with their counts, the review, the merge, or an issue's projects,
each with its status and every field it has a value in, such as Size or Estimate, and its
sub-issues), the change (lines added and removed, files, commits, the head branch and whether it
comes from a fork, and the sensitive files by category) or, for an issue, how it is planned, as
GitHub's sidebar shows it (its type, such as **Task**, its values in its organization's issue
fields, such as **Priority** and **Effort**, its milestone with its due day, in amber once it has
passed, and how far along it is, the issue it is a sub-issue of, and how many open issues block it
and it blocks), and what is around it (the agent session and how it started, what the routines did
with it, an issue's **Development**, the pull requests that close it with where each stands, or the
issues in your inbox a pull request **Closes**, why it is here, and every label). An area with
nothing to say is left out. Its buttons repeat your next move, offer **Review** or **Read** when
that move doesn't open the item, open the agent's launch options, and snooze, dismiss, pin, or open
it on GitHub, and, with **Unread dots** on, **Mark as read** or **Mark as unread**. Click again to
close it.

## A narrow window

The cards follow their list's width rather than the window's: below about 1,000 pixels the action
column narrows, then the labels and the size go, then the agent button shows its spark alone and a
live session its dot, then the track goes too, with the repository's org.

## How to read a card

The **?** beside **Last refreshed**, at the top of the inbox, opens **How to read a card**: a sample
card, the real one, with its parts numbered 1 to 8 and a legend of what each says (whose move it is,
who opened it and what it is, the title and labels, the story, the byline, the risk, where it
stands, and your move and the agent's), then a key to the edge's colors, the track's symbols, a
person's picture and a bot's, and, with **Unread dots** on, the blue dot. **Show the guide**, in
**Settings → Cards** → **How to read a card**, opens it too, and setup's **Finish** step shows the
sample card and the key. The sample is the same pull request every time, made up for the guide: it
takes no clicks, showing it asks nothing of GitHub, and it is always the two-line card, whatever
**Density** says. Escape or **Close** puts it away.

## Compact cards

**Settings → Cards** → **Density** chooses how much room each card takes, in both lists and a
routine's Watching list: **Comfortable** (to start with) is the two-line card above, and **Compact**
fits about three times as many, one line each, a column per question so you can read down each one.
From the left: the edge; the pull request or issue mark (grey for a draft); the repository, its org
dimmed; the title and number, the labels as up to six dots in their colors (hover them for their
names; hidden labels stay hidden), the link to an issue's fix or a pull request's issue, the pin,
and the unread dot; the story as a chip tinted with the edge's color (in the Snoozed and Dismissed
tabs, when it comes back or was dismissed, then where it stands); the risk as its shield alone
(hover it for who opened it and the sensitive files); the track as three dots, filled for a tick, a
cross, or ↻, half-filled for runs held, pinging while checks run, dashed while a step waits and
dotted with nothing yet to say (hover them for the three sentences), or for an issue a ring of its
sub-issues done, else a dot in its project status's color; the size grade, or an issue's priority or
size; how long ago it last changed; and your next move as words in the edge's color, filled while
the pointer is on the card. Hover a compact card for its triage, the agent's button, **Show
details**, and the menu, just left of your next move. Click its empty space, or **Show details**,
for the same details as a comfortable card. In a list narrower than about 900 pixels the repository
and the size go, and below about 770 the story and the age.

## Author pictures

**Settings → Cards** turns the authors' pictures off and on (on to start with). The inbox asks for
the pictures of its authors as it loads, 25 to a request, all at once, and each card's shows as soon
as its request comes back, so a slow download holds back only its own. A person's picture is
downloaded by their login and a bot's from the address GitHub gives for it with the item, since the
picture host answers a bot's login with a generated pattern; both without your token and kept in the
database, each downloaded again at most once a week, and a failed one not tried again for 30
minutes. Turned off, the cards show initials and the bot mark, and nothing is downloaded for them;
with the cards compact, which show no picture, nothing is downloaded either. A routine's Watching
list shows initials and the bot mark alone.

## Unread dots

A blue dot before a card's title (after it, on a compact card) says something happened on the item
since you last opened it, such as a new commit, a comment, or a review, as GitHub's last activity on
the item says. Opening its review or issue sheet clears it, from the inbox or a routine's Watching
list, and so does opening it on GitHub (**⌘-click** on the title, the **↗** beside it, or **Open on
GitHub** in the card's menu or details). What you do in a sheet counts as seen too: the card is
marked again as the sheet steps on to the next card (← →) or closes, so your own review, comment, or
merge never brings the dot back, and so is a card you took yourself off on GitHub (**Unassign me**,
**Remove me as reviewer**). **Mark as read** and **Mark as unread**, in the card's **⋯** menu under
**Inbox actions** and in its details, set it by hand: an unread card keeps its dot until you open it
again. Something you do on github.com itself still lights the dot, since GitHub's last activity
doesn't say who acted. Your first sign-in marks everything it brings read; an item that arrives
later starts unread, unless only a routine's search found it. **Settings → Cards** → **Unread dots**
turns the dots off and on (on to start with); off, the cards show no dot and their menus no **Mark
as** item, but what you open is still marked, so the dots are right when you turn them back on. All
of it stays on this Mac: nothing is sent to GitHub, it doesn't mark your [GitHub
notifications](github-notifications.md) read, and theirs don't drive it.

## Hidden labels

**Settings → Cards** lists the labels the cards leave out, in every repository: `size/*` to start
with, since each card already shows its own size grade, so `size/XL` would only repeat it. Add a
label, or a pattern: `*` matches anything, `/` included, as in `size/*`, `?` any one character, and
case doesn't matter. **Restore the defaults** puts the list back. A hidden label is only left off
the cards, before the first two are taken, so it never takes a visible label's place or counts in
the **+N**; a card's details, the [filter](filtering.md) (`label:size/XL`), the issue sheet's
labels, Autopilot, and the prompts' variables still see it.

## Archived repositories are left out

Each of the inbox's four searches asks GitHub for `archived:false`, so an archived repository's pull
requests and issues never appear, never count toward a list's total, and never cost a details
request. The inbox query also reports each item's repository, so an item whose repository was
archived after GitHub indexed it is dropped before anything is stored, and the repository is
recorded as archived. Clones of archived repositories still show in the Worktrees view, where their
worktrees can be cleaned up.

## Merged and closed items are left out

The searches ask GitHub for open items only, but its search index can trail a merge or a close for a
while. The inbox query also reads each item's state as of now, at no extra cost, so an item merged
or closed since GitHub indexed it is dropped before anything is stored.

## Repository details

The same query reads what GitHub says about each item's repository, at no extra cost, since GitHub
prices lists and a repository is a single object: its description, visibility, whether it is
archived, disabled, or locked (and why), the repository a fork was made from, its default branch,
your permission and the merge method GitHub offers you first, which merge methods it allows, whether
it allows auto-merge and branch updates and deletes merged branches, its primary language, and its
last push. Every refresh rewrites them with the repository; your own settings for it, and its
matched clone, are left alone.

## Where the details come from

Each refresh reads them with a second GraphQL query that asks for the listed items by node ID.
Asking for these fields inside the inbox query's searches (three when it was measured) would cost
about 12 points a refresh, because GitHub prices nested lists per result slot; asking for the listed
items alone costs about one point per request. GitHub answers these requests at roughly 150 ms per
item, so they go out in batches of at least 10 items, up to 4 at a time, and the details of the
items already listed are read alongside the inbox query rather than after it. Only items new to the
lists wait for a request of their own, so a refresh takes no longer than before once its items are
known. The details are stored with each item and read again on every refresh, since checks finish
without the item itself changing. A card whose details no refresh has read yet, right after updating
the app for instance, says so.

## Watching a refresh

Every whole refresh shows the same way, whoever started it: the app's start, the timer, **Refresh**,
or the menu bar. The **Refresh** button turns its arrows and reads **Refreshing…** (it can't be
clicked again until the refresh ends), and in place of **Last refreshed** the toolbar says what the
refresh is doing: **Asking GitHub…**, then **Reading 3 of 8 new items…** while it reads the details
of the items new since the last refresh, then **Checking files: 2 of 5…** while it checks the
changed files of the pull requests whose files are checked, the last two with a thin bar of how far
along they are. A hairline glides along the top of the lists meanwhile, and the **Inbox** tab turns
a small spinner beside its count, so a refresh shows from any page. The lists stay as they were
until the refresh lands. The inbox's very first refresh, when nothing is stored yet, shows **Filling
your inbox from GitHub…** with what it brings (review requests, your pull requests, assignments,
checks and reviews) above the two lists' outlines, which the real lists replace once it lands; a
first refresh that fails shows its error and **Not refreshed yet** instead. An empty inbox throws
its [party](inbox-nil.md) only once the refresh the window opened on has ended, so a stored empty
inbox doesn't celebrate while the app's start still reads GitHub. The menu bar shows no refresh
under way.

## Refreshing one card

Hover a card and click the circular arrow, left of the snooze clock, to read that item alone from
GitHub, for one point, rather than refreshing the whole inbox. A pull request you just merged, or an
issue you just closed, leaves the inbox at once, and a toast says so. An item still open gets its
card and details rewritten in place, and new activity can end its snooze. Which lists it is in waits
for the next whole refresh, since only the searches can tell. The last-refreshed time and the error
line above the lists belong to whole refreshes; a card refresh that fails says why in a toast.

## Opening a session

**✦ Claude** beside a card's next step (**Open in Claude** to a screen reader, and **✦ Codex** when
the item's settings choose Codex) opens the launch menu on the item's [resolved launch
settings](launch-defaults.md), so you see what the session would start with, and can change any of
it, before anything starts; the menu's **Open in Claude** starts it. Every launch button does the
same: the review and issue sheets' **Open in Claude**, and a worktree row's in the Worktrees view,
which has no arrow. The arrow beside the others, named for the prompt and profile those settings
choose (**Launch options: Review · default**), opens the same launch menu, as does **✦ Claude…** in
the card's details. On a card that names a pull request's situation, the button opens the launch
menu on the prompt your launch defaults choose for it (**Open in Claude: Fix checks** to a screen
reader), with the settings that prompt brings; the card asks for nothing more until its launch menu
opens, which then works out that prompt's settings once and opens on it, and **Save these choices as
the default for** saves a prompt chosen there as the prompt for that situation, not for every pull
request:

- **Agent:** a switch between Claude and Codex at the top, the one the settings choose marked
  with where it comes from. An agent whose program isn't installed is shown disabled, with why.
  Switching agents clears the profile, model, and extra-flag choices, since each agent has its
  own.
- **Prompt:** a select of every prompt for the item's kind, by nickname, so the dialog stays one
  size however many there are; the one the settings choose is marked with where it comes from
  (*Review · repo default*), and the chosen prompt's full title shows under it. **Manage** opens
  the Prompts tab.
- **Profile:** every profile of the chosen agent, the chosen one marked. A Codex profile that
  isn't signed in is marked **not signed in**.
- **Model** and **Effort:** each offers its default first, with its value and source, as in
  "Default · Sonnet (prompt)", then the chosen agent's models and effort levels. Codex's models
  come from its own local model cache, so the list needs no request.
- **Start in a worktree** and **Start in plan mode** (**Start read-only** for Codex), each showing
  where its default comes from. On an outside contributor's item, plan mode is checked and
  locked until **Use profile permissions for this item** is ticked.
- **Preview:** exactly what the launch runs, planned without writing anything: the filled-in
  prompt, the directory, what the launch does with the item's worktree (makes it from the pull
  request's head or the default branch, or resumes it, with its uncommitted files), the
  permissions, and the command line.
- **Save these choices as the default for** the repository, or for all of its owner's
  repositories, then **Open in Claude** or **Open in Codex**. The two save buttons wait until a
  choice differs from the defaults. A save takes one click, since it changes only the defaults
  on this Mac, and its line offers **Undo**, which puts back what was stored before (or removes
  the new defaults) and brings the choices back, so they can be saved for the other scope.

A choice applies to the next launch only; afterwards every choice returns to the item's
settings. The arrow shows a dot while any choice differs from them, and picking the default
again clears the choice. Choosing another agent, prompt, or profile resolves the settings again,
so each default shown is the one that would really apply, such as a prompt's own model. A
launch that starts adds nothing below the card: the card's agent button follows the session,
and its panel says what it started with. Only a failure, or what a launch worked around (a
worktree started from the last fetched copy of the default branch, say), shows below the
card. An item no prompt applies to marks the arrow with a warning, and its launch options say
**No prompt** and why. A card whose item's worktree has uncommitted changes, or a session
running in it, says so.

## Issue projects

An issue's card shows its **Status** in the first GitHub project it is in, in GitHub's order,
as a pill in the color the project gives it, such as **In progress** in yellow, which is also
the card's story. Under it, the issue's priority shows as an outlined pill in its option color,
such as **High**: the first field named like *Priority* (or *Prio*), else the first named like
*Size* (or *Effort*), whatever their order in the project. The card's details name every
project the issue is in, such as **Roadmap**, with its status, priority, and size; hover one
for every field it has a value in, such as **Estimate**, **Phase**, a date, or an iteration,
and click it to open the project on GitHub. A closed project, and an item archived from its
project, are left out, and the first five projects an issue is in are read. A project whose
Status field was renamed or deleted shows its name alone. Pull requests show no projects.

### Sorting by status

A list whose issues are in projects, **Yours** in practice, has a **Recent | Status** toggle at the
right of its header. **Recent** is the usual order: pinned items first, then newest activity.
**Status** keeps the pinned items first, then lists the issues by their project status, an issue in
several projects by its best one; issues of one status stay newest first, and issues without a
status come last. Click the arrows icon beside the toggle for **Status order**: the statuses found
in the list, top ranked first, each moved with its up and down arrows. Every project names its own
statuses, so the order is yours. A status you haven't placed, as a new project brings, is placed by
its name: work under way (**In progress**), then in review, ready to start (**Ready**, **Todo**),
anything else, new work (**Backlog**), work set aside (**Blocked**, **Deferred**, **Waiting**), and
done last. It goes after the last status you placed at the same point or an earlier one. **Reset**
puts every status back where its name places it. The toggle and the order are kept across launches;
statuses you placed that are not in the list now keep their places.

### Showing one kind

A list that holds both pull requests and issues, **Yours** in practice, has an **All | PRs |
Issues** switch to the left of that toggle. **PRs** or **Issues** shows only that kind on the list's
own tab, in the same order, and its count then reads *N of M*, so nothing is hidden without saying
so; the sheets step through the same cards. A kind with nothing left in the list says so, with
**Show all**. Snoozed, Dismissed, and Muted always show both kinds. The choice is kept for each list
across launches, and a notification that opens a card of the other kind shows the whole list again.

### The scope

GitHub only shows projects to a token with the `read:project` scope (or `project`, which includes
it), and `gh auth login` doesn't ask for it. Add it to the token the app borrows, then refresh:

```bash
gh auth refresh -s read:project
```

`gh` adds it to the scopes the token already has. The app borrows the new token as soon as
`gh` rewrites its hosts file, as it does when it signs in again, and within ten minutes in any
case, when the token it holds expires.

### Without it, nothing breaks

The app never asks GitHub for projects while the token lacks the scope, so a refresh goes on as
before and issue cards just show none. When the inbox holds an issue, a tip above the lists offers
to show them, with the command and a **Copy** button: **Check again** refreshes to find out, and
says so when there is still no access. Once a refresh finds access, the tip says the projects are
on. Close the tip with **×** and it stays away; **Settings → Account → Projects on issues** keeps
the command, and says **On** once the projects show.

### How they are read

The projects and their fields come with each issue's other details, in the same requests, so they
never add one. GitHub prices the field values per project an issue is in, so with them a batch of
issues costs a few points of the GraphQL budget instead of one; only a token with the scope pays it.
Sub-issue counts cost nothing extra. A token that reports its scopes is asked for projects only when
it has the scope. The details of the items already listed are asked for before this refresh knows
the token's scopes, the way the last refresh read them. When the scope was just granted, every
item's details are read again once the scopes are known, so the first refresh after `gh auth
refresh` shows every issue's projects. When it was taken away, GitHub refuses the projects, and
those requests are sent again without them, so the refresh still succeeds and the cards show none. A
token that reports no scopes, such as a fine-grained token, is asked too; once GitHub refuses it the
projects, the app stops asking it. Refreshing one card reads its projects when the last whole
refresh could.
