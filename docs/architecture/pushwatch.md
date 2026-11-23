# internal/pushwatch

[Docs](../README.md) › [Architecture](../README.md#architecture) · Code: [`internal/pushwatch`](../../internal/pushwatch) · Guide: [The window](../guide/window.md)

`internal/pushwatch` notices a push made on this Mac to a pull request the inbox lists, so
its card catches up at once rather than at the next refresh. Git runs no hook after a push,
but a push that lands moves the clone's own `refs/remotes/origin/<branch>` to the commit it
pushed, whatever ran it. The `Watcher`, a deferred desktop worker (`Core.PushWatcher`),
follows that ref and hands a pull request whose branch moved to the inbox's `Rereads`
(`RefreshItem`, one point), as the sessions' Watcher does. It reads its pull requests, the
clones, each clone's origin, and its switch through its own `Targets`, `Clones`, `Origins`,
and `Switch`, which `internal/core/adapters_pushwatch.go` adapts; the repoguard boundary
test keeps it off the inbox, settings, the store, and GitHub.

- **What it watches.** `inbox.Store.PushTargets` (`ListPushTargets`; the Store's, so it is
  not bound): the signed-in account's listed open pull requests, neither dismissed nor
  snoozed, whose head branch is in their own repository (`is_cross_repository = 0`), a muted
  repository's or org's left out, the ones the user opened first, then by activity, each with
  its head commit as the details last read it. The Watcher keeps the first `MaxTargets`
  (200) whose branch `gitread.ValidBranch` takes, so a ref's path never leaves
  `refs/remotes/origin`. It reads them, and the switch, once a minute (`TargetsEvery`), and
  at once when woken: `pushesOnInbox` wakes it on every `event.InboxUpdated`, so a pull
  request newly listed is watched before it can be pushed to, and `event.IntervalsChanged`
  (which `SetWatchPushes` and a backup's import emit too) through core's `followSwitch`.
- **The clones.** Every `IndexEvery` (5 minutes) while there is a pull request to watch, it
  lists the clones in the clone root and the other locations (`Roots.Clones`, no git), each
  by its common git folder (`localrepos.CommonDir`), so a linked worktree listed beside its
  clone counts once, and a push from any worktree moves the one ref. A clone's origin is
  read with the Release page's `releaseOrigins` (one `git config --get-regexp`) the first
  time, and again only once its `config` file's stat changed; only a clone whose origin is
  the pull request's repository is watched, every one of them.
- **A look.** Every `Tick` (3 s) while some watched pull request has a clone, it `Lstat`s each
  watched ref file (anything but a regular file reads as missing), and reads one
  (`paths.ReadHead`, 128 bytes, a full commit ID alone or nothing) only once its stat changed:
  inode, size, and time, since git writes a moved ref as a new file, a lock renamed over it.
  The first look at a ref only learns where it is. A ref packed away (`git pack-refs`) or
  deleted keeps the commit last read, and only a ref read at another commit moved. A clone
  that keeps its refs in reftable (`reftable/tables.list`, which git replaces whenever any
  ref moves) is stat'ed by its table list, once a look, and its watched branches are read,
  once it changed, with gitread's allowlisted `rev-parse --verify --quiet
  refs/remotes/origin/<branch>`; without a `Git`, such a clone is not watched. Nothing is
  written to a clone or installed in it.
- **The read.** A ref that moved to a commit other than the head GitHub reported has every
  pull request on that branch read again `Settle` (2 s) after the look, so GitHub has taken
  the push in. Before each read the pull requests are read again, and one whose new commit
  GitHub reports now, or that is no longer watched, waits no more. While GitHub still
  reports another head, it is read again after 10 and then 30 seconds (`retryAfter`), three
  reads at most, then left to the refresh. A launched session's push reaches `Rereads`
  through the sessions' Watcher too, and the inbox's change after either read stops the
  watch's retries.
- **Off.** While `refresh.watchPushes` (Settings → Refreshing → **Notice pushes from this
  Mac**, on by default, carried by a backup) is off, it reads nothing, not even the list,
  until woken. Turned on again, it starts afresh, so a branch moved while it was off is the
  refresh's, and it keeps only the origins it read, which hold while their config files do.
