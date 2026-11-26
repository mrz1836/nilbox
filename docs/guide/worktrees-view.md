# The Worktrees view

[Docs](../README.md) › [User guide](../README.md#user-guide) · Design: [`internal/worktrees`](../architecture/worktrees.md), [`internal/localrepos`](../architecture/localrepos.md), [The Worktrees page (frontend)](../architecture/frontend/worktrees-page.md)

The **Worktrees** tab lists every linked worktree of every git repository in the [repository
locations](launching-a-session.md#repository-locations), including worktrees from sessions started outside the app. It
loads when you open it or click **Check again**, and again after the automatic cleanup, not on
the inbox's five-minute timer. It runs git only in the clones whose git folder registers a
linked worktree or can't be read, and in those an app record names; any other clone has nothing
to list, and is only looked at.

## The head

The top of the tab stays in place while the clones scroll under it. Its first line says how many
worktrees there are, in how many clones, and when they were checked, as in **12 worktrees in 4
clones · checked 2m ago**. While a listing runs, its progress takes that place: finding the clones,
listing each clone's worktrees ("3 of 12 clones", with a bar), checking each worktree, and asking
GitHub about their pull requests and issues. While a change runs, the same place says what it is
doing, as in **Removing example-repo/pr-7…**. Neither moves the rows. Then come **Check again** and
the cleanups: **Clean up N finished** while any worktree is finished, with **⌄** beside it for
**Remove N clean worktrees…** and **Clean up merged branches…**, or **Clean up ⌄** alone while none
is, its menu saying why a cleanup has nothing to take.

## Chips

Under the title, a chip counts the worktrees in each state any of them is in: **running**,
**uncommitted**, **not on GitHub**, **needs a look**, and **ready to clean up**. A chip writes its
filter in the toolbar (`is:in-use`, `is:dirty`, `is:unpushed`, `is:review`, `is:finished`), and a
second click clears it. Under the chips are the notes a listing carries: a location it couldn't
read, such as a drive that isn't plugged in, a system whose running processes can't be listed, and
GitHub that couldn't be reached. Last comes a one-line card that says what the latest change did.

## Clones

Each clone has a heading that stays at the top while its rows scroll by: its owner and name, its
path, and its main checkout as a note, such as **on main · clean**, amber with the count while it
holds uncommitted files. On the right it counts its worktrees that run, are at risk, are ready, and
have a problem, and offers **Prune** when git still lists worktrees whose folders are gone. Clicking
the heading folds the clone's rows away. The main checkout isn't a row of its own. Every clone
shares one set of columns, so the rows line up down the whole page.

## Rows

Each linked worktree is one line, in the order of what needs you: something runs in
it, it needs a look, it holds work that isn't on GitHub, it's open, it's ready to clean up, then
a problem (its folder is gone, git couldn't read it, or it is locked by hand), each group with
the newest commit first. An edge in the group's color marks it. The line shows the worktree's
state, its branch, the pull request or issue it was made for (the number opens it on GitHub,
a **copied** tag when it is linked by its commits' copies, and while the inbox lists the item,
a dot for its checks and a draft tag), its work (the count of uncommitted files, and ↑ the
commits not on GitHub, or, muted, ⧉ the commits on GitHub only as copies, as in **⧉ 10 on
#42**: see [copied work](#copied-work), and **17 missing**: see [missing
files](#files-missing-from-the-folder)), what works in it (an agent and how its session
stands, a routine's agent in the background, another program by its pid, or, muted, the last
session the app launched there), and the age of its last commit. Then comes at most one verb:
**Clean up** for a finished worktree, **Unlock** for a lock an ended session left, or, on an
open item the inbox lists whose own worktree it is (linked by its record, its name, or the
branch made for it), **Open in Claude** or **Open in Codex**, which opens the launch options
and, from their **Open**, resumes it. Last come **›_**, which opens the worktree in the
terminal your launches open in ([Open in terminal](launching-a-session.md#open-in-terminal)),
**⋯**, and the chevron.

## The drawer

A click on a row, or on its chevron, unfolds its drawer; from the keyboard, → unfolds it, ← folds
it, and ↑ and ↓ move between the rows. Several drawers can be open at once, and they stay open
across listings. On top, one sentence says what removing the worktree would cost: green when nothing
would be lost, amber when work would be or is only here, blue while something runs in it, and red
for a real problem, with the way out (for a lock set by hand, the `git worktree unlock` command to
copy). Under it are three columns. **Changes** lists the uncommitted files by their status letters,
with **Copy list**. **Not on GitHub** lists the commits that aren't, each marked with its copy on a
pushed branch when it has one (**= copied as** and the copy's commit for an identical copy, **≈
edited copy** for an edited one), then the branch, its head, and how far it is behind `origin`'s
default branch; when every one of them is on GitHub as an identical copy, it is headed **Copied to
PR #42** (or the branch the copies are on) instead. **About** says what the worktree is for, the
session in it, its launches, who made it (Nilbox, a routine, Claude Code under a clone's
`.claude/worktrees/`, or someone outside the app) and when, its path, its lock, and which branches a
removal would delete. The first 12 files show at once, from the listing; the drawer reads the rest
from local git as it opens, with the commits and the base, once per listing (a read that failed is
tried again when the drawer opens again), and lists at most 200 files and 200 commits (the rest are
in your terminal, in `git status` and `git log`). The drawer's footer has **Open in Terminal**,
**Reveal in Finder**, and **Copy path**, then **Clean up** or **Unlock**, and **Remove…**, or
**Force remove…** for a worktree with uncommitted files, as the drawer last read them, each saying
why when it can't run.

**The ⋯ menu** holds everything a row offers: **Open in Terminal**, **Reveal in Finder**, **Copy
path**, **Copy branch**, the item on GitHub, **Unlock** for a lock an ended session left,
**Prune** for a folder that's gone, then **Clean up…**, **Remove…**, and **Force remove…**, in
red. An item that can't run now says why under it. For a folder that's gone, or one git
couldn't read, **Open in Terminal** and **Reveal in Finder** are disabled in ⋯, which says why,
and the row and the drawer leave them out.

## Keeping track

The app records each worktree it makes or adopts, and checks every record against `git worktree
list` each time it lists or sweeps:

- a record is matched to its worktree by path, else by its branch, so a worktree moved with `git
  worktree move` is followed;
- a record whose worktree is gone, removed outside the app or pruned, or whose clone is gone from a
  folder that is still there (a clone on a drive that isn't plugged in keeps its records), is
  forgotten, so neither the inbox nor a launch preview ever claims a worktree that isn't there;
- a worktree with no record that the app or Claude Code made for an item, or that is on an item's
  own `worktree-pr-<n>` or `worktree-issue-<n>` branch, is recorded once GitHub links it to its
  item.

## In use

A worktree is **in use** while a running process works inside it, whichever agent or tool it is, and
whether a terminal or an editor runs it, as in **codex · pid 4242**. A coding agent's process is
named for its agent, as in **Claude 2.1.328 · pid 63296**, where the program's own name would be
Claude Code's version; while the app [follows](following-a-session.md) its session there, the row
says how the session stands instead, as in **Claude 2.1.328 · needs approval · 5m**. A process a
session started there, such as the `caffeinate` that keeps the Mac awake for it, is folded into it.
Hover the row for the process's whole command line, which the drawer's **Session** shows too. One
no terminal runs, whose starter has exited so launchd (or init or systemd) adopted it, is
**detached**, as in **bash · pid 75083 · detached**: nothing you can see holds the worktree, as when
a test run dies and leaves a child it started behind. Stop it, and the worktree can go.
The app lists the running processes' working folders once per listing or sweep, with `lsof` on macOS
and `/proc` on Linux, leaving out its own git commands, and a coding agent's background service: the
agent's own program running under launchd (or init or systemd) rather than a terminal or an app,
with everything under it. Codex's app-server daemon is one: Codex starts it with its first session
and keeps it after you quit, and it keeps that session's folder as its working directory, so it
would otherwise hold that worktree in use until you restart. Where the processes can't be listed,
the view says so; the automatic cleanup then removes nothing, since it never guesses, and no
worktree is force-removed, while a plain removal you ask for still goes ahead, since git refuses one
with uncommitted changes. A lock Claude Code wrote for a session in one of its own worktrees counts
too.

## Linked pull requests and issues

A worktree made for an item is linked to it: exactly, from the app's record, or otherwise by its
name (`pr-<n>` or `issue-<n>` in the app's or Claude Code's worktree folder) or its own branch, and
the clone's GitHub remotes. A worktree with none of these, on a branch pushed to one of the clone's
GitHub remotes, is linked by that branch to the pull request it heads in that same repository, the
open one when there is one, else the one updated last, so a fork's branch of the same name never
links it. One whose commits were copied onto a pushed branch is linked to that branch's pull request
([copied work](#copied-work), below). GraphQL requests, a point for each fifty, ask GitHub whether
each is open, merged, or closed, and the row links to it; the branches ride in that same request,
and one is asked about again only once it moves, or now and then while no pull request heads it.
Each linked worktree gets a stage, which places its row (**Rows**, above):

- **in use**: a session is running in it.
- **ready to clean up**: its pull request merged or its issue closed, and removing it loses
  nothing. It has no uncommitted changes but [files missing from its
  folder](#files-missing-from-the-folder), every commit it has is on GitHub (on a
  remote-tracking branch, as the pull request's head, which GitHub keeps after the branch is
  deleted, in the commit the app made the worktree at, which it had just fetched from
  GitHub, so a pull request that moves on after the launch still counts, or as an identical
  [copy](#copied-work)), and no session runs in it.
- **needs a look**: its item is done, but it holds uncommitted changes or commits that are not
  on GitHub.
- An item still open gets no marker; the row just says it is open.

GitHub decides when a worktree is finished, and git alone decides whether removing it is safe,
so a name or branch that only looks like an item's can never cost work. If GitHub can't be
reached, the view says so and lists the worktrees without their items.

## Files missing from the folder

A removal that stops partway, as when the Mac shuts down or its terminal closes while `git worktree
remove` deletes the folder, leaves the worktree registered with half its folder gone. git counts
every tracked file that's gone as an uncommitted change (` D` in `git status`), but each is still in
the worktree's last commit as it was, so removing the worktree loses none of them. When those are a
worktree's only changes, the app treats it as clean: its row notes, muted, how many are missing (**17
missing**) instead of uncommitted files, it is never at risk, and it is ready to clean up once its
item is done, with every commit on GitHub, so **Clean up**, **Remove…**, and the automatic cleanup
take it. Its drawer says the missing files are in its last commit, and `is:missing` finds such
worktrees. One file changed, staged, or added beside them, and the worktree holds uncommitted work
again, which only **Force remove…** discards.

git itself refuses such a worktree without `--force`, so the app runs `git worktree remove` without
it first. Only when git refuses the worktree for its changes alone, after its own checks of the lock
and of any submodules (a worktree with a submodule is never forced), does the app read its status
again; if every change is still a missing file and no process works in the folder, with the running
processes listed, it runs `git worktree remove --force`.

## Actions

Each row offers only what is allowed, and every removal checks again first that no session runs in
the worktree. Each removal and cleanup asks first, in a dialog that starts with the drawer's
sentence on what removing the worktree would cost, says what it does to the folder, and lists a box
for each [branch](#branches) that could go with it, ticked as **Settings → Worktrees** says; untick
any to keep it. While a change runs, each worktree it takes dims and says what is happening; one a
bulk change left in place keeps its color, with why on a line under it. When a change finishes, a
toast says what it did (or why it failed), with **View** if another view is open by then, and a
one-line card at the top of the Worktrees view sums it up, with notes on anything left under it,
until you dismiss it, click **Check again**, or make the next change.

- **Clean up** removes a finished worktree. It checks the worktree again first, releases the
  lock of a Claude Code session that has ended, and runs `git worktree remove` without
  `--force`. **Clean up N finished** does the same for every finished worktree: its dialog lists
  each one, ticked, with what removing it would cost in a line and its branches under it, and
  an unticked worktree is left alone.
- **Unlock** appears when a Claude Code session's lock outlived the session (you closed the
  terminal, say). The lock records the session's process and start time. A lock counts as
  stale only when no process has that ID, or the process that has it now started after the
  session did (the ID was reused, after a restart for instance). Anything in doubt counts as a
  running session. A lock you set yourself with `git worktree lock` is never released: the
  drawer shows the `git worktree unlock` command to copy and run instead.
- **Remove…** checks the worktree again and runs `git worktree remove` without `--force`. The app
  refuses a locked worktree before git runs, and git refuses a dirty one; the row says why. One
  whose only changes are [files missing from its folder](#files-missing-from-the-folder) is
  removed anyway, since that loses nothing. This
  works for an item still open, such as a session that only answered a question; a branch kept with
  commits that aren't on GitHub is picked up again by the item's next launch. **Remove N clean
  worktrees…** removes every clean, unlocked worktree no session runs in, listed in its dialog the
  same way as **Clean up N finished**.
- **Force remove…** is offered only on dirty, unlocked worktrees no session runs in (never one
  whose only changes are missing files, which **Remove…** takes), and only
  while the running processes can be listed. It runs `git worktree remove --force` after a
  confirmation that lists the uncommitted files it discards (as the drawer lists them, read
  from local git once per listing, at most 200) and the commits not on GitHub (at most 200
  too), which stay in the clone on the worktree's branch, or, for a detached worktree, which no
  branch holds, are lost with it.
- **Prune** appears on the heading of a clone with entries whose folder is gone, and runs
  `git worktree prune`, which touches no worktree that exists and no branch.
- The main checkout shows only as its clone's note, and is never removable.

## Cleaning up after a merge

A worktree stays after its session ends, so the next launch on the item resumes it, and no agent
removes one whose pull request merges later. So every five minutes (or as set in **Settings →
Refreshing**) the app checks the clones that have worktrees it or Claude Code made, or records,
without listing every clone, and, with **Clean up after merging** on (**Settings → Worktrees**, on
by default), cleans up each finished worktree whose pull request merged or whose issue closed and
that no session runs in, with the branches **Settings → Worktrees** deletes. Which worktrees it
takes depends on how each is linked, so a name or a branch that only looks like an item's is never
enough:

- one linked by its name, or by the pushed branch it is on, only when the app launched a
  session in it, or its HEAD is the merged pull request's own head commit, and one linked by
  its branch only when the app or Claude Code made it, too;
- one linked by its commits' copies only when the app or Claude Code made it, and the pull
  request the copies went into merged with every copy;
- and any worktree whose work is on GitHub only as copies only once the pull request they went
  into merged with every copy, never while it is open.

A toast names what was cleaned up, with **View** to open the list below. The same check
updates the **Worktrees** tab's count, which it reads from each clone's git directory without
running git, and marks the tab while some worktrees are done on GitHub but still on disk.
Removing a worktree also removes the files git ignores in it, such as build output.

## Recently cleaned up

A toast is easy to miss, so every cleanup, by hand or automatic, is also recorded, and the bottom of
the Worktrees view lists the newest five (**Show N more** for the rest), one line each. Each line
names the repository and worktree, the pull request or issue it was for, whether you or the
automatic cleanup removed it, which branch went with it, the commit it was at, which is on GitHub,
and when. Pointing at a line shows its actions: **Copy restore command** copies the `git worktree
add` that brings the worktree back at that commit, on the branch that went with it when one did (for
a pull request whose head git has pruned since, `git fetch origin pull/<n>/head` fetches it first),
and the item opens on GitHub. The history survives restarts and keeps the newest 100, of which the
view lists the newest 50; **Clear** empties it and removes nothing else.

## Copied work

A coding agent's session often hands parts of its work to sub-agents, each in a worktree of its
own (Claude Code makes them under the clone's `.claude/worktrees/`) on a branch no one pushes,
then copies their commits onto its own branch, with a cherry-pick or a rebase, and pushes that.
A copy keeps the commit's change, author, author time, and subject, but gets a new commit ID,
so the original is on no remote-tracking branch, and by its ID it isn't on GitHub.

Each listing compares a worktree's commits that aren't on GitHub with the commits on the clone's
pushed branches, from local git alone: a pushed commit with the same author, author time, and
subject is a likely copy, and its change is identical only when it is the same byte for byte, so
a copy that only changed spaces or indentation is an edited one. When every
one of them has an identical copy on one pushed branch, the worktree's work is on GitHub as that
branch. Its row is calm, with no amber: its state says each commit's change is on GitHub, its
work says **⧉ 10 on #42**, the pull request that branch heads (or **⧉ 10 on feature-x**, the
branch, when no pull request is known to head it), and its tooltip says where. When nothing
else links it, it is linked to that branch's pull request, with a **copied** tag, and its
drawer says it was copied onto that pull request; a worktree linked otherwise, as by its name,
keeps its own item, and its copies are still named by the pull request they went into. The
drawer's sentence says every commit's change is on GitHub, copied onto the pull request's
branch, and its second column, headed **Copied to PR #42**, marks each commit **= copied as**
its copy.

A commit whose likely copy has another change, an edited copy, isn't on GitHub: the row stays
amber, with **↑**, and its tooltip and the drawer's sentence say how many have an identical
copy, how many only an edited one, and how many none, as in **9 have an identical copy on
origin/feature-x, 1 only an edited one**; the drawer marks that commit **≈ edited copy**.
Identical copies spread over several branches don't count either, and a comparison that can't
run (more than 200 commits, a merge among them, or git refusing a read) counts none: the tooltip
says why.

A squash merge leaves the copies out of the default branch, and deleting the branch, or
force-pushing it without them, takes them off every remote-tracking branch, but the pull
request keeps them. So when a listing finds a worktree fully copied onto a branch whose pull
request it knows, the app records it. While the worktree's HEAD stays where it was, and the
pull request's head still has the copies, what the app recorded vouches for them once the
branch is gone, or once the pull request merged, whatever became of its branch; while the pull
request is open and its branch is there, only the copies the branch holds count. The row and
the drawer then say the copies went into the pull request, the drawer that the branch is gone,
or no longer holds them all, and what the app recorded vouches for each copy. It marks no
commit then: it compares them with the branch as it is now, which says nothing of the pull
request.

What that means for removing it:

- While the pull request is open, **Remove…** loses nothing: removing the worktree keeps its
  branch, with the original commits, and that branch can't go yet, saying why. Nothing removes
  the worktree on its own.
- A worktree linked to that pull request by its copies is ready to clean up once it merged
  with every copy: its own branch can go with it as your branch rules say, and the automatic
  cleanup takes it when the app or Claude Code made it. A worktree linked to its own pull
  request or issue by its name follows that item: it is ready to clean up once that item is
  done, but the automatic cleanup takes it, and its own branch can go, only once the pull
  request its copies went into merged with every one too.
- A detached worktree's original commits, which no branch holds, go with it, but their changes
  are on GitHub.

A sub-agent's worktree isn't its pull request's own: the inbox's worktree chip never counts one
linked by its copies, and neither it nor one linked by its pushed branch offers **Open in
Claude** or **Open in Codex**, which would open the item's own worktree, not this one.
`is:unpushed` and the **not on GitHub** chip leave out commits on GitHub as identical copies.

The drawer compares the commits with the pushed branches again as it opens. Unless what the app
recorded vouches for them, they count as on GitHub there only while that comparison ran and
found every one with an identical copy on the same branch: once HEAD moved, the branch was
deleted or force-pushed without them, their copies are on another branch now, or the
comparison can't run (the drawer says why), they count as not on GitHub, and the drawer marks
each commit as it found it, until the next listing.

## Branches

Removing a worktree can delete local branches with it, and **Clean up merged branches…** deletes
the branches of pull requests that merged, so the branches in a clone you work in often don't
pile up. **Settings → Worktrees** sets what each starts with; every dialog shows a box per branch,
ticked as those settings say, and you can change any before you confirm.

- **Delete the worktree's own branch**: `worktree-pr-<n>` or `worktree-issue-<n>`, the branch the
  app made for it. On by default.
- **Delete the pull request's branch**: such as `feature-x`, when a session checked it out in the
  worktree (with `gh pr checkout`, say). On by default.
- **Merged branches.** **Clean up merged branches…** lists each clone's local branches whose
  pull request merged. It asks GitHub, in GraphQL requests of up to 50 branch names each across
  every clone, for the merged pull requests whose head branch has each name, in each of the
  clone's GitHub repositories, and only when you click it. A `worktree-pr-<n>` branch left after its worktree went is matched by its
  number. The dialog groups them by clone, each closed to start with: a clone's row shows
  how many branches it has and how many are ticked, a box that ticks or unticks them all,
  and a warning sign when some of them can't be deleted. Open it to see each branch with its
  pull request, when it merged, and its commit. **Tick merged branches** (on by default) sets
  whether they start ticked.

**Protected branches** are never deleted, by any cleanup: `main` and `master` by default, and any
you add, as a name or a pattern such as `release/*` (`*` matches any run of characters but `/`, and
`?` any one but `/`). Each repository's default branch (its remote's `HEAD`) and any branch checked
out in a worktree are always kept too.

A branch is deleted only when nothing is lost: every commit on it is on GitHub (on a
remote-tracking branch, as the pull request's head, which GitHub keeps after the branch is
deleted, or in the commit the app made its worktree at, fetched from GitHub), or, for a merged
branch, it points at the merged pull request's head or a commit before it. A worktree's branch
whose commits are on GitHub only as [copies](#copied-work) can go only once the pull request
they went into merged with every copy. A branch that can't go is shown unticked and held, with why, such as "2 commits on it
aren't on GitHub". Each is checked again as it is deleted, and deleted with `git update-ref -d`
only while it still points at the commit the dialog showed, so a branch that moved in between is
kept. The outcome lists why each branch kept stayed and, folded away, each deleted branch with
that commit, so `git branch <name> <commit>` brings one back; **Copy restore commands** copies
those commands for every branch deleted. Branches are deleted a few clones at a time, each
clone's branches read once. The automatic cleanup deletes only a worktree's own and pull request branches, as the
settings say; merged branches go only through the dialog. Nothing on GitHub is ever changed.

### Deleting a merged branch with work not on GitHub

A merged branch held only because some of its commits aren't in its pull request or on GitHub, such
as an old branch you know you don't want, can be deleted anyway. Tick **Also offer the N branches
with commits not on GitHub** above the list: each such branch then gets a box, unticked, and says
how many commits it holds. Delete asks once more, naming each of those branches and its commits, and
**Go back** returns to the list. Their commits are then left only in the clone, until git cleans
them away (`git gc`), and the outcome lists each branch's commit, so `git branch <name> <commit>`
brings one back while git still has it. A branch checked out, protected, or a repository's default
branch is never offered.

Every git command runs as an argument list with `git -C <repository>`, never through a shell.
