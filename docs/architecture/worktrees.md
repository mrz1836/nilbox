# internal/worktrees

[Docs](../README.md) › [Architecture](../README.md#architecture) · Code: [`internal/worktrees`](../../internal/worktrees) · Guide: [Launching a session](../guide/launching-a-session.md), [The Worktrees view](../guide/worktrees-view.md)

The app owns its worktrees: `internal/worktrees` makes them (one per item per clone, shared by every
agent, under `<the clone's folder>/.<slug>/worktrees/<clone>/`, so in a location as in the clone
root), records them in `internal/worklog`, checks every record against git on each listing and sweep
(a record whose clone's folder is gone too, as on a drive unplugged, is kept), and removes them
safely. A listing runs git only in the clones worth listing (`worthListing`): one whose common git
folder registers a linked worktree or can't be read, and one a record names, read without git by
`linkedWorktrees`, which the sweep's count shares. A pull request's worktree fetches its head, and,
on every Ensure, made or found, its base into the remote-tracking ref (`fetchBase`, the default
branch's own fetch: best-effort, a notice when it fails, never a checkout or a local branch moved),
so an agent has a fresh base to rebase onto or compare with.

Only `internal/worktrees/create.go` fetches or runs `git worktree add` (Repo scripts' pull, in
[internal/scripts](scripts.md), fetches only as `git pull` itself does), and nothing the app runs
changes a clone's own checkout; `TestNoGitCommandChangesAClone` in `internal/repoguard` fails the
build otherwise. There are two exceptions, each on the user's click. A tool the user turned on: a
release's magex switches to the branch, pulls, tags, and pushes the tag (`internal/release`), and
the app only starts it and checks afterwards where the tag and the clone are. And Repo scripts'
**Pull**: a fast-forward-only pull of the branch a clone is on from the branch it follows on origin
(never a merge or a rebase), held to `internal/scripts/pull.go`, whose one pull argv the guard
parses and holds to an exact list of literal words (`--ff-only`, `--refmap=`, and
`--no-verify-signatures` among them), with no other option anywhere in the file and no other verb it
bans. Every git command there runs with a hooks folder's hooks, the file system monitor, and
submodule recursion off (`gitread.Hardening`, which the worktrees' `hardening` and the pull share),
since a worktree holds a pull request's own files, and a pull request's worktree is made only at the
head whose files the launch checked; a clone's own config-named hooks refuse every command that
could fire one (below).

With core's `Guard`, the worktree service's own commands on a clone or its worktrees are guarded too
(`git.do`): the reads (`readOnly`: listing the worktrees and branches, resolving a ref, counting
commits or listing those not on GitHub, an ancestor's check, and a commit's time, the logs with
`--no-show-signature` and `--no-mailmap`), which run no filter or hook but could read an object a
partial clone lacks, run with gitread's `ReadEnv` (no transport, no lazy fetch, no optional lock, no
replace ref), so a promisor remote the clone names is never asked; every other (a status, which runs
a file's clean filter, a removal, a prune, an unlock, and a branch's deletion, which fires
reference-transaction) runs with gitread's `LocalSetup` for its folder, worked out once per
operation and one folder at a time (`localCache`, a `flight.Group` per folder): every filter driver
of the clone's own off, Git LFS's too, no optional lock, no replace ref, no transport, and refused
before git runs, with a `guardError` worded for the window, when the clone's own config holds a
setting gitread refuses. A removal, whose own status runs inside the worktree, weighs the worktree's
config too (its `config.worktree`), and runs with the filters both define off. Such a worktree shows
the refusal as its `Error`, which also takes away its removal (`CanRemove`), so no sweep or cleanup
removes it, and a removal fails with the same words.

`create.go`'s one fetch argv (`fetchArgs`:
`fetch --no-tags --refmap= --upload-pack=git-upload-pack <remote> <refspec>`, so it moves
only the ref its refspec names, never one the clone's own `remote.<name>.fetch` maps,
which the repoguard test holds to exactly those words), its `ls-remote --symref` (with
`--upload-pack=git-upload-pack`), and its `git worktree add` also run under gitread's
guards, through the worktrees `Guard` core gives every `Service` (the same `gitread.Git`
as Release's): an Ensure that builds a worktree works out one `RemoteSetupFor` its
item's remote, on its first guarded command, under the clone's lock (`git.session`), and
`git.reach` runs a command that reaches the remote with it and `git.write` one that writes
files with its `Checkout` (no transport, but a partial clone's checkout may fetch from its
promisor remotes, which are weighed too). A checkout weighs only what it can reach: when
the setup refused the remote itself (a `RefusedError`), `git.write` works out
`CheckoutSetup` instead, so a worktree from the last fetched copy, or on a branch the
clone has, is still made, while a partial clone's promisors, and rerouting settings Git
LFS would follow, still refuse it. Each runs as `git -C <clone> <words>` with that
setup's environment, which holds every setting (`GIT_CONFIG_COUNT`), `GIT_DIR`, and
`GIT_WORK_TREE`, and the setup's own git, through `Config.GitWith`, core's runner with an
environment of its own. Core gives the worktrees gitread's `GitFinder` as their `Finder`,
so every other worktree command runs that same git: one clone is never handled by two
git builds.

A clone whose own config holds a setting gitread refuses (a hook, a filter, `core.worktree`, an
include, ...), a remote helper, or a rerouted remote is refused before git runs (a `guardError`
holding gitread's), in `gitread.Message`'s words as a sentence, naming the settings or the remote or
saying why the setup couldn't be worked out (a config git can't read, no git), which the launch's
error shows; a fetch that failed after the clone's own ssh command, password program, or credential
helpers were set aside says which (`gitread.SetAsideNote`, a `gitread.SetAsideError` in the chain).
The launcher reaches the `Maker` through its `WorktreeMaker` interface; no agent's own `--worktree`
flag is used.

A branch is deleted only with the conditional `git update-ref -d <ref> <oid>` (the repoguard tests
ban `git branch -d/-D`): by `create.go`, of a branch it just fetched at a head the launch didn't
check, and otherwise only through `internal/worktrees/branches.go`, when the user ticked it (or, for
the automatic cleanup, the user's rules pick it), every commit on it is on GitHub, and neither the
user's protected patterns (`internal/branchrules`) nor a remote's `HEAD` keeps it. The one exception
is the cleanup of merged branches: a branch kept only for its commits not on GitHub
(`MergedBranch.Unpushed`) can be deleted there anyway, but only once the user asks for such branches
to be offered, ticks it (never ticked to start with), and confirms a second dialog naming each and
what it holds; pinned to the commit shown, whose result names that commit. A branch checked out,
protected, or a remote's default never is.

Every job that changes a clone holds that clone's lock from `internal/clonelock`, one `Set` that
core builds and hands to each, keyed by `paths.Resolved`. Lock `act` before a clone's lock, never
after. Worktree changes wait only for one another. A run (a release, checks, a script) is never
waited for: a launch or a click fails at once with a sentence naming the run, and the sweep and a
listing's forgetting of a record skip the clone until next time.

A listing reads each worktree's status once (`status --porcelain=v1 -z
--untracked-files=all`, read by `readStatus`): every file it names counts toward the row's
`DirtyCount`, each one ` D` (a tracked file gone from the folder that the index has as HEAD
has it, as a removal that stopped partway leaves it) toward its `MissingCount` too, and the
first 12 by path are its `Files`, each with git's two status letters
and its path (a rename or a copy by its new one, its source skipped), which the view shows
before it reads the rest. Only those 12 are kept as the status is read, each file past them
only counted, so a worktree with tens of thousands of untracked files costs little more
than the count. A clean worktree's `Files` is `[]`, and so is one whose status couldn't be
read. Keeping them runs no git of its own. `Details` counts its `MissingCount` from its own
status the same way.

A worktree whose every change is a missing file (`onlyMissing`: `MissingCount` equals a
`DirtyCount` over 0) loses nothing with its folder, since HEAD has each file as it was, so it
counts as clean: `assess` makes it `CanRemove` (while the processes are listed, as its removal
needs `--force`) and never `CanForceRemove`, and `stageOf` lets it be `StageFinished`, so
`CleanUp`, `CleanUpFinished`, and the sweep take it as they take a clean one. A missing file
beside any other change (staged, modified, added, untracked) leaves the worktree dirty.

## The launch records (`internal/worklog`, `reconcile.go`)

`internal/worklog` owns `worktree_records` (migration `0102_runs_and_caches`), one row per clone
and item worktree name (`pr-<n>`, `issue-<n>`), read as a `worklog.Record`: its folder (`Path`,
or `""` for a record kept before the app made worktrees itself, whose `Dir` is
`<clone>/.claude/worktrees/<name>`, `worklog.LegacyDir`), branch, item, the latest session's agent
and start (`Agent`, `LastAgentAt`), `Launches`, `CreatedAt`, and `FetchedHead`. `Store.Record`
refuses an incomplete or malformed record (`ErrInvalidRecord`) and upserts on the clone and the
name, keeping `created_at`, the agent, and the launches, and the stored `fetched_head` unless the
new record names one. `Touch` counts a launch with its agent and time (`ErrNotRecorded` without a
row); the launcher calls it for each session it starts in an item's worktree, and core's
`backgroundAgents` for a routine's run in the background.

The service writes a record in `recordWorktree`, once Ensure has made or found an item's worktree
(never a task's); in `adopt`, for an unrecorded worktree linked by `name` that the app or Claude
Code made or that is on an item's own `worktree-pr-<n>` or `worktree-issue-<n>` branch, unless the
clone already has a recorded worktree of that name; and in `rerecord`, for one that moved. A
record links its worktree to the item exactly (`LinkedItem.Launched` when `Launches` is over 0),
finds it for the item's next launch (`Maker.record`), and brings its clone into the sweep; whether
a worktree may be removed is still decided from git and GitHub each time.

`reconcile` checks the records, read before git lists anything, against each listing's rows. A
record matches the row at its `Dir`, else the one on its branch, never the main worktree or one
whose folder is gone, so a worktree moved with `git worktree move` is rerecorded, and marks it
(`markRecorded`: `Managed`, `Agent`, `Launches`, `LastLaunchedAt`, `OwnBranch`). One whose clone
wasn't listed is forgotten only when the clone's folder is gone and its parent folder is there
(`cloneGone`); one whose clone git couldn't read is kept. One no row matches is forgotten
(`forgetStale`) only once git, asked again under the clone's lock, still lists nothing for it: a
launch holds that lock from finding the item's worktree to recording it, and the lock is only
tried (`tryClone`), so a busy clone keeps the record for the next listing. A failed write is only
logged.

## Linking a worktree to its item

A worktree is linked to its pull request or issue by its record, by the name of the folder the app
or Claude Code made it in (`pr-<n>`, `issue-<n>`), or by an item's own branch (`worktree-pr-<n>`):
`Worktree.LinkedBy` is `name` then. A linked worktree with none of these, on a local branch pushed
to one of the clone's GitHub remotes, is linked by that branch (`branch`, `branchlink.go`): through
the remote-tracking branch of the same name, its upstream's remote's first when it follows one by
that name (an upstream of another name is the branch it started from, its base, not its head), then
origin's, then the other remotes' by name. A remote's default branch, a detached HEAD, and a remote
that isn't on GitHub never link. Of the pull requests whose head branch has that name in that same
repository (`headRepository`), so a fork's branch of the same name never does, it links to the open
one, when there is one, else to the one updated last, merged or closed. Each clone's refs are read
once a listing (`refsArgs`: `for-each-ref` over `refs/heads/` and `refs/remotes/`, each with its
commit, upstream, symbolic target, and worktree, a read with `ReadEnv`), and the branches a removal
offers are judged from the same read (`markRepo`), so linking by branch runs no git of its own; a
clone whose refs can't be read links nothing by branch and offers no branch. A remote's name may
hold a slash, so a remote-tracking branch is the branch of the clone's remote, of every one it names
with a URL (`localrepos.LocalRepo.Names`, from the same config read as its GitHub remotes), whose
name is the longest that begins it (`withOwners`): `refs/remotes/origin/mirror/main` is a remote
called `origin/mirror`'s `main`, never origin's `mirror/main`, and a branch of a remote the clone
doesn't name is no one's. So is a remote's HEAD: `refs/remotes/origin/mirror/HEAD` names the default
branch of the remote called `origin/mirror`, once the clone names that remote, which nothing links
through nor looks for copies on. GitHub is asked in the listing's request for the items' states:
`github.FetchLinkStates` adds one aliased `repository { pullRequests(headRefName: ..., states:
[OPEN, MERGED, CLOSED], first: 20, orderBy: UPDATED_AT DESC) }` per branch to the states query, at
most fifty of each a request, so branches beside items cost no request, and branches alone one
(twenty pull requests a branch, so the repository's own isn't lost behind forks' of a branch of the
same name updated since, still one point a request). The answer is kept in memory by repository,
branch, and the commit the clone saw it at (`headCache`): the next listing asks about that pull
request by number with the items, and about the branch only once it moves, or, at most every
`headRecheck` (10 minutes), while no pull request heads it or the one that does is done (one opened
since, or a branch reused for another, with or without a push). A pull request GitHub no longer
answers as that branch's is forgotten, and the branch asked about again. A worktree linked by its
branch is never recorded as its item's (`adopt`), so a launch on the item never resumes it; it
offers no pull request's branch with its removal (`BranchPullRequest` stays `name`'s); and the sweep
removes it only when the app or Claude Code made it (`Managed`) and, as always, its HEAD is the
merged pull request's head.

## A task's worktree (`task.go`)

A repository's launch asks for a worktree for a task on the repository itself rather than an item's
(`EnsureRequest.Task`: its base name, `deps-<YYYYMMDD>`, with no kind, number, base, or head,
`checkSubject`). It is made new every time, as an issue's is, from the repository's default branch
as just fetched, through the default branch's fetch and `git worktree add --no-track -b
worktree-<name>`, so no new argv (`Ensured.Task` is the name taken, and `DefaultBranch` the branch
it starts from, or, for `Plan`, which asks no remote, the request's or the one the clone last
learned from its remote's HEAD), and it is never resumed, adopted, attached, or recorded: one reused
could build on commits a merged pull request already took, and a record names an item, so it would
link the worktree by name and keep its branch from relating it to its pull request. Its name is the
first free one of the base, then `<base>-2` up to `<base>-99` (`freeTaskName`; past those,
`ErrNoTaskName`, worded): a name is taken while anything is at its managed path, or may be, as when
the path can't be looked at; while git has a worktree at that path or on its branch, its folder
there or gone; while the clone has that local branch; and while it has a remote-tracking branch of
that name on any remote, so one left from an earlier task, pushed and since merged, never relates
the new worktree to the old pull request. `Plan` says the name it would take now, asking no remote,
and `Ensure` takes the first free one as it makes the worktree. Calls for a task never share a run
(no `calls` grouping, which would hand two quick launches one worktree): the clone's lock runs them
one after the other, so a second the same day takes the next name. It is made only from the clone's
own folder, the main worktree git lists first (`ErrNotMainWorktree`), so it sits beside the clone's
other managed worktrees, where a listing and the sweep find it as the app's (`Managed`,
`OwnBranch`). One its launch made but couldn't start a session in is taken away again by
`Maker.Discard`, as `Remove` takes one away (refused while a session works there or it holds
changes, its branch deleted only while nothing on it would be lost), so the next launch takes its
name back. Once its session pushes the branch under its own name (`git push -u <remote> HEAD`) and
opens a pull request, a listing links it by that branch (`branch`, above), and the sweep removes it
once that pull request merges with its HEAD as the head, as for any worktree the app made and a
pushed branch links. The limit: a fetch of the user's own that prunes the remote-tracking branch, as
after GitHub deletes the merged branch, before a listing sees the merge leaves the worktree linked
to nothing, and it is then removed by hand.

## Copies of a worktree's commits (`copies.go`)

A coding agent's sub-session works in a worktree of its own, on a branch no one pushes, and the
session that started it copies its commits (a cherry-pick or a rebase) onto its own branch and
pushes that. A copy keeps the commit's change (git's patch ID), author, author time, and subject,
but not its commit ID, so the original counts as not on GitHub (`Unpushed` counts by ID, and keeps
doing so).

Each listing compares the commits not on GitHub (U) of every linked worktree that has any with the
commits on the clone's pushed branches: the remote-tracking branches of its GitHub remotes, each
remote's default branch aside. Per clone, one log reads U of every worktree with at most 200, with
each commit's parents, author time, author email, and subject (`copyUnpushedArgs`: `log
--no-show-signature --no-mailmap --max-count=10000 --format=%H%x00%P%x00%at%x00%ae%x00%s <HEAD>...
--not --remotes`, with `--ignore-missing` and the commit they were made at from GitHub when they
have one, one log per such commit), each worktree's own U then walked by its parents within it; and
one more log reads the pushed commits not in the default branch whose commit time is at most a day
before the oldest of those author times (`copyCandidatesArgs`: `log ... --no-merges --source
--max-count=20000 --format=%H%x00%S%x00%at%x00%ae%x00%s --since=@<seconds> --remotes --not
refs/remotes/<remote>/<default>`; a copy keeps the author time and is made later). When that log
reaches its cap, only the worktrees that need its window (the oldest) can't be compared, and one
more log, from the next oldest worktree's window, serves the rest, so one worktree left for months
never keeps its clone's others from being compared.

A pushed commit with a
commit's author time, email, and subject is its likely copy. The log credits each pushed
commit to one ref only (`--source`), often the lowest of a stack of branches, or a remote's
HEAD, so the pushed branches it credits with likely copies, most first and at most three,
only order the comparisons: for each, one `rev-list --left-right --no-merges
<tip>...<HEAD>` (`copySidesArgs`, commit IDs only) lists the commits the branch has that
HEAD hasn't (`<`) and back (`>`), and every likely copy on the branch's side is one there,
whatever ref the log credited it to, so a branch built on another counts the copies the two
share, even those of a default branch no copy is looked for on.

No patch ID decides anything: git's leave out every space inside a changed line, a hunk's position,
and the mark of a missing last newline, so a copy that was only re-indented, or lost a space from a
string, would pass, though in Python, YAML, a Makefile, or a string literal that is a change. So
each pair is compared byte for byte (`copypatch.go`): the changes of the commit and its likely
copies are read in one more log for the comparison (`copyPatchesArgs`: `log --no-show-signature
--no-mailmap --no-walk=unsorted --root -p --no-ext-diff --no-textconv --no-color --no-renames
--full-index --unified=3 --format=%x00%H <commit>...`, a read) and compared whole but for a text
file's blob IDs (its `index` line keeps only its mode) and the line numbers of each hunk's `@@`
header (and the text git adds after it): spaces, context lines, `\ No newline at end of file`,
modes, and file headers all count. A binary file's change is the same only when its blobs before and
after are, its `index` line whole. An empty change, one that doesn't read as git's per-file diffs,
or one git left out is no copy's, and a log that fails leaves the worktree not copied, with why. A
verdict on a pair of commits is kept in memory for good (`copyCache.pairs`), as commits never
change. A commit with a likely copy there whose change is the same has an identical copy; one with
likely copies there whose changes differ an edited copy, which never counts (an edited change isn't
the commit's); any other none. A copy made on a moved base whose context lines differ is edited too.

A worktree each of whose commits has an identical copy on one branch is fully copied onto it, and
links to that branch's pull request, asked about as a branch link is, when nothing else links it
(`LinkCopy`); the pull request of the branch a worktree copied only in part is asked about the same
way, without linking it. `Worktree.Copies` says what was found: the branch (`ref`, as in
`origin/feature-x`, and `branch`, its name on its remote as `withOwners` works it out, `main` for
`origin/mirror/main` of a remote called `origin/mirror`, or `''` when no remote the clone names is
its), how many commits (`count`), and how many have an identical copy, an edited one, or none, with
`note` saying why they couldn't be compared, or why identical copies on several branches don't
count; `pr` is the number of the pull request the copies went into (the one `ref`'s branch heads,
or, when `recorded`, the one the record names; 0 when none is known), and `gone` says `ref`'s
remote-tracking branch is gone, as it can be only when `recorded`. Its zero value means there was
nothing to compare with.

Each read is a read (`readOnly`, with `ReadEnv`, so an object a partial clone lacks fails it rather
than being fetched), and the allowlist holds each to its exact words: commit IDs, a remote-tracking
branch, and a whole number of seconds. Anything that goes wrong leaves the worktree not fully copied
and never fails the listing: git refusing a read, or warning on its way (its answer may then be
wrong), a log reaching its cap, more than 200 commits, a merge among U, a count that changed
meanwhile, or a comparison that leaves a commit out. What a comparison found is kept in memory
(`copyCache`) by the worktree's HEAD, the commit it was made at from GitHub, its clone's GitHub
remotes, and every remote-tracking branch and where it points (`cloneRefs.fingerprint`), so a
listing after which nothing of those changed runs no git to compare, and a worktree that is gone is
forgotten, as is every worktree of a clone the window's listing, which lists every clone with a
linked worktree, didn't list (the cache starts over, too, past 4,096 worktrees); git that couldn't
be run at all is tried again next time.

`Details` marks each of its commits not on GitHub with its copy (`UnpushedCommit.Copy`, `identical`
or `edited`, and `CopyOID`): what the listing found, when one read of the clone's refs and one of
its remotes say nothing changed since (the listing's key: its HEAD, the commit it was made at, the
GitHub remotes and every remote's name, and the refs' fingerprint), or else a comparison of its own;
none when the refs or the remotes can't be read. `Details.Copies` (`CopyComparison`) says how that
went, so a blank mark is never mistaken for an unknown one: `compared` is true when the comparison
ran to completion (the pushed commits read, every branch holding likely copies compared and
confirmed byte for byte), or there was no pushed branch to compare with; `ref` is the branch
compared with, `""` when no pushed commit was a likely copy of any, and `branch` its name on its
remote, as `Copies.branch` says it; `identical` and `edited` count the commits; and `note` says why
it couldn't run. So `compared` with `identical` at 0 means no copy is on a pushed branch now (the
branch was deleted, or force-pushed without them), while `compared` false means only that this is
unknown. It reads no copy record, so a worktree its record vouches for (`Worktree.Copies.recorded`)
may compare with nothing; its zero value is for a worktree with nothing not on GitHub.

## The copy record (`copyrecords.go`, migration `0109_worktree_copies`)

A worktree fully copied onto a branch stays so only while the branch is there: a squash merge leaves
the copies out of the default branch, and deleting the branch leaves them on no remote-tracking
branch. The pull request keeps them (GitHub keeps every pull request's head commit), so when a
listing finds a worktree fully copied onto a branch whose pull request it knows, it records that
pull request in `worklog` (`worklog.Copy`, the `worktree_copies` table, one row per worktree by its
resolved directory): the worktree's HEAD, the branch, the pull request, the commits not on GitHub,
and `copy_tip`, the branch's commit, which moves on with the branch while it still holds every copy,
so it is most likely the pull request's last head. A record that already says all a new one would
isn't written again. Each listing checks the records against the worktrees it listed, as the launch
records are: one whose worktree is gone (or its folder), or whose HEAD isn't the record's, is
dropped, and so is one whose clone is gone from a folder that is still there; one whose clone wasn't
listed, or couldn't be, is kept. Removing a worktree drops its record too. The worklog store stamps
a record's `created_at` (kept while the HEAD is the same) and `updated_at` with its own clock.

## Copied work counts as on GitHub (`copyjudge.go`)

`OnGitHub` is `Unpushed == 0`, or HEAD being the linked pull request's head, or the worktree's
copies counting (`copyState`), while `Unpushed` keeps the count by commit ID. They count when this
listing found every commit not on GitHub with an identical copy on one pushed branch of a GitHub
remote: on GitHub as that branch. Or when the copy record holds, and either its branch is gone or
its pull request merged: the record describes the worktree (HEAD is the record's); it covers every
commit the worktree has that isn't on GitHub, each being one whose identical copy the record vouches
for or on the record's branch commit by ID (`judgeCoverage`: no git when this listing's comparison
read those commits and all are the record's, else one `log` per record branch commit,
`copyUnpushedArgs` with that commit after `--remotes` and `--ignore-missing`, kept in memory by the
worktree's HEAD, the remote-tracking branches, and the record; this is what keeps the commits a
worktree started from on a branch since squash-merged covered, while a commit of another branch
deleted unmerged is never vouched for); the record's pull request was read in this listing's request
(asked by number, with the items, only for a covering record); and that pull request's head is the
record's `copy_tip` or has it (`prHolds`: `merge-base --is-ancestor`, which a commit the clone lacks
fails; a yes kept for good, a no while the remote-tracking branches are the same). `Copies.Recorded`
says so then, with `Identical` at `Count`, and a worktree nothing else links is linked to that pull
request (`LinkCopy`). A merged pull request vouches whatever became of its branch, deleted or
force-pushed without the copies; while it is open and its branch is there, only the copies this
listing finds on the branch count, so a branch force-pushed without them makes them count for
nothing. A record that covers less than the worktree holds links nothing. `stageOf` is unchanged: a
merged or closed item, on GitHub, and clean is finished. The record's pull request merging with
every copy (`copyState.merged`: the record holds, and its merged pull request's head has `copy_tip`)
is what the cleanup waits for: `autoCleanable` takes a worktree whose work is on GitHub only as
copies only then, and one linked by its copies only when that is the pull request it is linked to,
and only when the app or Claude Code made it; never while the pull request is open, and never by a
branch's name. Its own branch, and a pull request's branch it has checked out, are deletable then
too (`headSafety`), still subject to the protected patterns, a remote's default branch, and the
conditional `update-ref -d`; while the pull request is open, their reason says the commits are on
GitHub only as copies in a pull request that hasn't merged with them.

The rest of what a worktree's drawer shows comes from `Details(repoPath, path)`, which the
window calls when the drawer first opens. It first checks the request as the removals do
(`checkedGit`, then `entryOf`, which lists the clone's worktrees, then `ErrPrunable` for a
folder that's gone), and fails when the request can't be checked or isn't about a linked
worktree of a clone the repository locations hold, or when its context ends. Past that,
every read it can't make is a sentence in `Errors`, not an error. It runs three reads side
by side:

- the status, every time, so the drawer shows the files as they are now: the first by
  path, at most 200, and `FileCount`, all of them;
- the count of the commits not on GitHub, every time, the listing's own, and only when
  it is over 0, their list (`log --no-show-signature --no-mailmap --max-count=200
  --format=%H %ct %s HEAD --not --remotes`, or with `--ignore-missing` and the commit the
  worktree was made at after `--remotes`, as `unpushedPastArgs` counts them;
  `allowedUnpushedLog` takes HEAD as the tip and a full commit ID as that commit, and
  nothing else);
- origin's default branch as the clone last fetched it and how many commits it has that
  HEAD doesn't (`Base`, `Behind`, -1 when unknown), through the `BaseReader` core gives,
  gitread's `OriginHead`, `TrackingBranch`, and `CommitsBetween`, in steps, each only once
  the one before found what it needs.

When the app made the worktree comes from its record (`CreatedAt`). Where several records
name one folder, `recordsByDir` picks the newest that holds the commit the worktree was
made at, or else the newest, for the listing's count (`fetchedHeads`) and `Details` alike.
`Details` never asks GitHub, takes no lock, and emits nothing.

`OpenTerminal(repoPath, path)` and `Reveal(repoPath, path)` check the request as `Details`
does, so a worktree whose folder is gone is refused with `ErrPrunable`, then open the
terminal chosen for launches in the worktree through the service's own `FolderOpener`, which
runs nothing there (without one, OpenTerminal fails with `ErrNoTerminal`, worded for the
window), or show it in Finder with `open -R` (off macOS there is no `Revealer`, and Reveal
fails with `ErrNoFinder`, likewise worded). The package never imports `internal/scriptrun`.
Core builds the worktree service before the terminal, which needs the launcher, which needs
the worktrees, so the service's `FolderOpener` is a core adapter (`worktreeTerminal`) filled
in once the terminal is built, before any work starts, which opens the folder as every page's
Open in terminal does: in Warp itself, with no script, and in any other terminal through a
script named `scriptrun.FolderScript` (see [internal/launcher](launcher.md)).

## In use: the running processes (`sessions.go`)

A worktree is in use while a process works in it (`Worktree.InUse`, each a `Session`: PID,
program, followed agent, and `By` for a run). A listing or sweep reads the processes once, before
git lists anything (`processes`, over core's `procs.System`; see
[Shared building blocks](shared-building-blocks.md)), and resolves their folders once
(`markSessions`, `procs.Locate`): one counts when its folder is the worktree or inside it,
symbolic links resolved. Left out are the app and its children (`procs.Others`), and an agent's
background service with everything under it (`procs.WithoutServices`, over each `agent.All()`
spec's `Command`): a process of an agent's command whose parent is PID 1 or a process named
`launchd`, `init`, or `systemd`, as Codex's app-server daemon is. A process another listed one
started is folded into its furthest listed ancestor (`sessionsOf`, `rootOf`), and a followed agent
(by PID, from `AgentProcesses`, the sessions ledger) folded into an unfollowed one stands for it.
The app's own runs, which the process list leaves out, count too (`RunFolders`, from core's
`backgroundAgents`).

Each session's process is then described (`describeSessions`, over `Config.Describe`, core's
`procs.DetailOf`), once per process whatever worktrees it is in, and never one of the app's own
runs: `Session.Args` is its whole command line, and `Session.Detached` reports one with no terminal
that the process manager adopted (`procs.Adopted`: its parent is PID 1, or `launchd`, `init`, or
`systemd`), so what started it has exited, as a test's child does when its test binary dies first.
The window marks it, since nothing the user can see holds the worktree. `DetailOf` reads the
kernel's process table on macOS (`kern.proc.pid` and `kern.procargs2`, the arguments alone, never
the environment after them) and `/proc` on Linux, starting nothing, and only for the few processes
in a worktree. A process it can't read keeps its program's name alone and is never called
detached.

When the processes can't be listed, the listing sets `Snapshot.SessionsUnknown`: no process is
found in any worktree (runs still are), no row is `CanForceRemove`, and the sweep removes nothing.
With no `Procs` lister, the processes count as listed, and none runs. A removal reads them again
for its folder (`checkSessions`) and refuses a worktree something works in with an `InUseError`
(`ErrInUse`, naming each, as in `codex (pid 4242)`). Called `strict`, as a force removal and the
automatic cleanup call it, it also refuses with `ErrSessionsUnknown` when they can't be listed,
while a plain removal or a cleanup the user asks for goes ahead, since git refuses a worktree with
changes and the locks still guard it.

## Agent locks (`lock.go`)

Claude Code locks a worktree for a session with a reason `parseAgentLock` reads
(`claudeLockPattern`, mirroring the agent's own): `claude session <name> (pid <pid>)` or
`claude agent` likewise, with an optional `start <time>` in `time.ANSIC` form, no zone. From each
lock's reason in `git worktree list --porcelain -z`, `judgeLock` sets `AgentLock`, `StaleLock`
(`endedAgentLock`), and `CanUnlock`. `sessionEnded` probes the PID (`procs.Probe`): gone, the
session ended; running, it ended only when that process started (`procs.StartedAt`) more than
`startSlack` (2 seconds) after the latest moment the recorded start can name (`latestStart`: the
start read as UTC and in the machine's zone, the later kept), so the PID was reused. Anything else
keeps it running: an unknown answer, a PID `procs.ValidPID` refuses, a missing or unreadable
start. Off macOS `processStart` reads no start, so a live PID always counts as the session.

A live agent lock makes its worktree `StageInUse`, and a lock set by hand leaves it no stage
(`lockedStage`); a stale lock leaves the stage to the rest, so a finished worktree is still
`CanCleanUp`, though never `CanRemove`, which needs it unlocked. `Unlock` holds `act` and the
clone's lock, lists the worktrees again, and runs `git worktree unlock <path>` only for an ended
agent lock, else refusing with `ErrNotLocked`, `ErrHandLock` (a reason the pattern doesn't match,
the user's to release), or `ErrSessionRunning`. A cleanup releases a stale lock the same way
(`unlockEntry`), after checking no process works there, and only while the reason is the one
assessed (`ErrChanged`).

## Removing a worktree (`service.go`, `cleanup.go`)

Each removal holds `act` for the whole call, and the clone's lock (`holdClone`) from its last
checks to its last change; `Remove`, `ForceRemove`, and `CleanUp` refuse a clone a run holds
before assessing it, which would cost git and GitHub requests (`checkNoRun`).

`Remove(repoPath, path, branches)` assesses the clone alone again, GitHub included (`assessClone`;
`ErrOutsideCloneRoot`, `ErrNotAWorktree`, `ErrMainWorktree`). Then `removable` lists the worktrees
again and refuses a locked one (`ErrLocked`), one whose HEAD moved since (`ErrChanged`), and, unless
its folder is gone, one a process works in, and `git worktree remove <path>` runs without `--force`
(`allowedRemove` takes a path and at most one `--force`); git's refusal is a `RefusedError`
(`ErrRemovalRefused`), its `Reason` `has uncommitted changes`, `is locked`, git's line, or `git
exited with an error`; a failure git didn't refuse (`gitRefused`) is a plain error. A plain
removal runs through `removeLossless`: when git refuses the worktree with `has uncommitted
changes` alone, which it says only after its own checks of the lock and the submodules (a
worktree with a submodule is refused for that first, and never forced), it reads the status
again, and when every change is still a missing file (`changeCount.onlyMissing`) and no process
works there, checked `strict`, it runs the removal again with `--force`; otherwise git's refusal
stands. `Remove` words `ErrSessionsUnknown` from that check for the window. `ForceRemove`
adds `--force` and `strict`, and `discardable` refuses a folder that's gone (`ErrPrunable`) or
nothing to discard (`ErrNotDirty`). `RemoveAllClean(picks)` takes one fresh listing and removes each
`Pick` still `CanRemove` as `Remove` does. `Prune` runs `git worktree prune` under the same locks.
`Maker.Discard` is `Remove` with the task's branch. After a removal, `forgetRecord` drops its launch
records and its copy record, by the path as `paths.Resolved` resolved it beforehand; the branches
named go next (`deleteChosen`), and the `RemoveResult` says why any was kept.

`CleanUp(repoPath, path, branches)` takes only a worktree assessed again as `CanCleanUp`
(`StageFinished`), refusing a path that isn't one of its linked worktrees (`ErrNotAWorktree`) and
the main worktree (`ErrMainWorktree`), and any other with `ErrGitHubUnavailable` or `ErrNotFinished`
and `notFinishedReason`, and wraps a later refused step, a busy clone aside, in `ErrCleanUpFailed`.
Each step checks git again (`cleanUpRow`, `removeFinished`): a locked worktree has its ended agent
lock released (above), then `removable` runs with the commit assessed, and `git worktree remove`
without `--force`, through `removeLossless`, as `Remove` runs it. Last it records the cleanup
(`recordCleanup`), which no other removal does.
`CleanUpFinished(picks)` takes one fresh listing, fails whole when that couldn't ask GitHub, and
cleans up each pick still `CanCleanUp`. The sweep runs `cleanUpRow` with `automatic`, which makes
its process checks `strict`.

## Deleting a branch (`branches.go`)

A removal offers the worktree's own `worktree-<name>` branch (`BranchOwn`), checked out there or
left beside it, and, for a worktree linked by `name` to a pull request, that pull request's head
branch when the worktree has it checked out (`BranchPullRequest`), both judged from the listing's
one read of the refs (`markRepo`), and for an own branch left beside the worktree, not at its pull
request's head, one `rev-list --count` (`branchOnGitHub`). `judge` gives the first `Reason` that
holds: a remote's default branch, a protected pattern (`branchrules.Matches`), checked out in the
clone or another worktree, or commits not on GitHub (`headSafety` at the worktree's HEAD,
`branchOnGitHub` elsewhere); otherwise the branch is `Deletable`. `deleteChosen` deletes only names
offered and `Deletable`, through `deleteBranchAt`, under the removal's clone lock: a full commit ID
(`ErrChanged`), no worktree on the branch now (`ErrBranchInUse`), then `git update-ref -d
refs/heads/<branch> <oid>`. The sweep picks the deletable branches whose kind `Rules.Own` or
`Rules.PullRequest` takes (`defaultBranches`).

`MergedBranches` changes nothing: per clone with a GitHub remote, `listConcurrency` (8) at a time,
it takes the local branches but the remotes' defaults, the protected ones, and invalid names
(`branchCandidates`), and asks GitHub `MergedPullRequests` by name (50 names a request,
`mergedBatch`) and `ItemStates` for a `worktree-pr-<n>` branch's pull request. A branch nothing has
checked out is `Deletable` when it is at or behind a merged head (`inMerged`, `merge-base
--is-ancestor`) or has no commit missing from the remote-tracking branches; otherwise it holds
`Unpushed` (-1 when the count failed; [the opening notes](#internalworktrees) give the exception). A
checked-out branch holds only its `Reason`. `DeleteBranches(picks)` holds `act` and each clone's
lock, reads each clone once (`readCloneBranches`, refusing a clone the locations don't hold with
`ErrOutsideCloneRoot`), and checks each `BranchPick` again: `ErrInvalidRequest`,
`ErrBranchProtected`, `ErrBranchGone`, `ErrBranchMoved`, `ErrBranchInUse`, then `update-ref -d`.

## Branch rules (`internal/branchrules`)

`branchrules.Rules` are four `prefs` keys settings writes together (`SetBranchRules`,
`Rules.Encode`) and the service reads (`SweepSettings.Branches`): `branches.own` (`Own`),
`branches.pullRequest` (`PullRequest`), `branches.merged` (`Merged`, which the worktrees package
never reads; the merged branches dialog starts from it), and `branches.protected` (`Protected`, a
JSON list). `Defaults` turns each switch on and protects `main` and `master` (`DefaultProtected`).
The switches only set which boxes start ticked and what the sweep takes; `Protected` refuses: no
removal, sweep, or merged-branch cleanup deletes a branch it matches (only `verifyHead` in
`create.go` deletes, unchecked, the branch its own fetch just made). `Normalize` trims, drops empty
patterns and repeats, and refuses over `maxPatterns` (100, `ErrTooManyPatterns`) and, with
`ErrInvalidPattern`, a pattern over `maxPatternLen` (200) bytes, holding a space or a control
character, or one `path.Match` refuses. `Decode` keeps a switch's default when it is unstored or
unreadable, keeps an empty list empty, and falls back to `DefaultProtected` for a list it can't read
or normalize. `Matches` runs `path.Match` on the whole name: `*` matches any run of characters but
`/` (`release/*` protects `release/1.2`, not `release/1.2/fix`), `?` any one but `/`, `[a-z]` one of
a set; a plain name protects that branch.

## The cleanup history (`history.go`, `worktree_cleanups`)

`recordCleanup` stores each worktree `cleanUpRow` removed, by hand or by the sweep, as a
`worklog.Cleanup` (in `worktree_cleanups`, migration `0102_runs_and_caches`): `CleanedAt`,
`Automatic`, the clone's and the worktree's folder names, `DisplayPath`, the branches deleted
joined with `, ` (`DeletedBranch`), `Head`, and the item. A failure is only logged.
`Store.RecordCleanup` refuses one without a repository or worktree name (`ErrInvalidCleanup`), and
in one transaction inserts it and keeps the newest `KeepCleanups` (100) by `cleaned_at`, then `id`
(`TrimCleanups`). The service's `RecentCleanups` returns the newest `recentLimit` (50) for the
window, and `ClearCleanups` deletes every row and nothing else.

## The sweep (`sweep.go`)

`Sweeper` runs `Sweep` on a `periodic.Job`: now, then from the end of each sweep as often as the
`refresh.worktrees` setting says (300 seconds by default; `DefaultSweepInterval`, 5 minutes, when
`NewSweeper` gets no `Every`); `Wake` reads the setting again. A sweep holds `act` throughout and
lists only `sweepClones`: the locations' clones with an entry in their managed root or
`.claude/worktrees`, found without git (`managedCloneDirs`, skipping one reached through a symlink
unless it is a clone location), and those of the records the locations hold. With none, it only
drops the records whose clone is gone; else it assesses them as a listing does (`assessClones`),
and its report's `Error` is the listing's `LinkError`, or why the sweep couldn't run.

With automatic cleanup on (`Enabled`, `worktrees.autoCleanup`) and the processes listed, it skips
the clones a run is using (`skippedClones`) and cleans up each worktree `autoCleanable` takes,
with the branches `defaultBranches` names, into `Cleaned` or `Failed`; a run that took the clone
meanwhile (`clonelock.BusyError`) leaves it for the next sweep. `autoCleanable` takes a finished
worktree with an item (`sweepMayTake`: one linked by its pushed branch or copies only when
`Managed`, one on GitHub only as copies only once `copyState.merged`, see
[Copied work counts as on GitHub](#copied-work-counts-as-on-github-copyjudgego)):
a merged pull request's when the app launched a session there (`LinkedItem.Launched`) or HEAD is
its head, a closed issue's when the app launched one there, and, linked by copies, the merged pull
request its copy record names. Each worktree left `CanCleanUp` or `StageNeedsReview` counts in
`Leftover`. `WorktreeCount`, counted last without git (`countWorktrees`: each common git folder's
`worktrees/` entries with a non-empty `gitdir`), leaves out what it removed. The report is kept
for `LastSweep`, and `event.WorktreesUpdated` goes out when the sweep removed a worktree or
`Leftover`, `WorktreeCount`, or `Error` changed.
