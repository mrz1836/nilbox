# internal/clonelock

[Docs](../README.md) › [Architecture](../README.md#architecture) · Code: [`internal/clonelock`](../../internal/clonelock) · See also: [`internal/worktrees`](worktrees.md), [`internal/scriptrun`](scriptrun.md)

`internal/clonelock/` keeps one clone-changing job at a time on each clone: a worktree made or
removed, a release, a hooks install, a checks run, a script, or a pull. A tool updating itself, or
being installed, holds its folder the same way, and a routine's agent in the background holds the
item's worktree. A job that finds its clone busy learns what holds it, which the window shows: "A
release is using acme-org/sdk right now." The locks are in-process: they keep the app's own jobs
apart, not another program's. The package knows no service (`internal/repoguard`'s boundary test,
`deps_test.go`) and imports only `brand` and `paths`. The rule every change keeps is in
`CLAUDE.md`'s [Rules every change keeps](../../CLAUDE.md#rules-every-change-keeps).

## The one Set

`Set` holds a lock per clone: a mutex over a map from each clone's key to its `hold`, the job's
`Holder` and a `done` channel closed when the job lets go. The zero `Set` is ready to use (`New`
returns an empty one), and it is safe for concurrent use. One `Set` serves the whole app, so every
job that changes a clone sees every other: core builds it once (`clonelock.New()` in `core.go`) and
hands it to the worktree service (`worktrees.Config.Clones`), the run engine
(`scriptrun.Config.Locks`), and the integrations (`integrations.Config.Locks`), and, to read which
job holds a clone, to Release and Hooks (their `Locks`, each a `LockHolders` interface declared
where it is used). The worktree service, the engine, and the integrations each keep a `Set` of
their own when given none.

A clone is keyed by `paths.Resolved`: the path cleaned, its symbolic links resolved, and a path that
doesn't exist resolved through its nearest existing parent. A path through a link, one with a
trailing separator, and one through `sub/..` share the lock of the folder they name
(`TestSymlinkedPathSharesLock`). Keys are compared whole, never as prefixes, so a folder inside a
clone has a lock of its own. An agent's run holds its worktree's own key: `worktrees.ManagedRoot`
puts the app's worktrees beside the clone (`<parent>/.<slug>/worktrees/<clone>/`), so the run never
holds the clone.

## Taking a lock

- **`TryLock(path, holder)`** takes the clone for the holder when it is free and returns its
  release, or returns the job that holds it and `false`, without waiting. The release may be
  called more than once (`sync.OnceFunc`): it drops the hold and closes `done`.
- **`LockWaitingFor(ctx, path, holder, waitFor)`** waits while a job that `waitFor` accepts holds
  the clone, until `ctx` ends, and fails at once with a `*BusyError` naming a holder `waitFor`
  refuses. It fails at once, too, when `ctx` has already ended, even on a free clone, and a `ctx`
  that ends while it waits fails it with `ctx.Err()` itself, as a semaphore does, so callers word
  it as they word any other. Woken by a release, it tries again, since another job may have taken
  the clone first.
- **`Lock(ctx, path, holder)`** is `LockWaitingFor` waiting for every holder. Only the tests call
  it.
- **`Holder(path)`** returns the job that holds a clone, and `true`, without taking it.

`TestLockKeepsJobsApart` runs 16 jobs that each take one clone 50 times and checks that one is
inside at a time. The waiting tests run in a `testing/synctest` bubble, and `TestMain` fails the
run on a goroutine left behind (`goleak`).

## What holds a clone, and the sentence

A `Holder` says what holds a clone, for the user: its `Kind`; `Repo`, what the window calls the
clone, its repository on GitHub, as in `acme-org/sdk`, or else its folder; and the run's `Title`
and `RunID`. The kinds are `KindRelease` (`release`), `KindHooks` (`hooks`), `KindChecks`
(`checks`), `KindScript` (`script`), `KindWorktree` (`worktree`), `KindPull` (`gitPull`, a clone's
branch pulled from origin, fast-forward only), `KindUpdate` (`update`, a tool updating itself,
which holds the folder its copy is in), `KindInstall` (`install`, the app installing a tool, which
holds the folder it places it in), `KindOrphan` (`orphan`, a run from before the app restarted,
still running, which the app follows under a placeholder lock), and `KindAgent` (`agent`, a coding
agent's run in the background, which holds the item's worktree, never the clone, so two items of
one clone run side by side).

`Holder.Sentence` names the job as the subject of "… is using <Repo> right now.": "A release", "A
hooks install", "A checks run", "A script", "A worktree change", "A pull", "A tool update", "A tool
install", "A run from before " + `brand.Name` + " restarted", or "An agent in the background"
(`doers`). A kind it doesn't know is "Another job", and an empty `Repo` is "this clone"
(`TestSentence`). `BusyError` carries the `Holder`, its `Error` is the sentence, and it matches
`ErrBusy` ("the clone is busy") through `errors.Is`, so a caller tells a busy clone from any other
failure and still shows the sentence.

## A run is never waited for

Only a worktree change ever waits for a clone, and only for another worktree change. Every other job
only tries the lock and fails at once with the sentence that names the job holding the clone, so a
launch or a click fails with it, and nothing waits on a clone's lock behind a run that could take
half an hour (a run in the app may wait for a slot, holding no lock).

- **Worktrees** (`internal/worktrees/clones.go`). `holdClone` takes the lock with `LockWaitingFor`
  and `isWorktreeChange`, as a `KindWorktree` holder named as the window names the clone: another
  worktree change is waited for, since those are short, and any other job refuses it with
  `uierr.New` and the holder's sentence. A wait that runs out of time is "Another worktree change
  in <clone> took too long. Try again in a moment." A launch's `Maker` holds the lock while it
  finds, makes, and records an item's worktree (within `ensureTimeout`, three minutes), and every
  removal while it changes the clone (`removeRow`, `cleanUpRow`, `deleteInClone` for branches,
  `Unlock`, and `Prune`). A click's `Remove`, `ForceRemove`, or `CleanUp` checks first that no run
  holds the clone (`checkNoRun`, through `Holder`), before assessing it, which costs git and GitHub
  requests, and `holdClone` checks again before anything changes. The sweep leaves a clone a run
  is using as it is this time (`skippedClones`, logged once), and a sweep's removal that meets a
  run meanwhile skips the worktree rather than fail it. A listing forgets a record only under
  `tryClone` (`TryLock`), so a busy clone keeps it for the next listing. See
  [`internal/worktrees`](worktrees.md).
- **Runs** (`internal/scriptrun`). `Engine.hold`, which `Start` calls, takes a slot first for a run
  in the app, then tries the lock of the run's folder (`Spec.Dir`), so a run that waits for a slot
  never holds its clone: a busy clone fails the start at once with the sentence (`uierr.New` over
  the `*BusyError`) and frees the slot. The holder's kind is the run's (`Kind.lockKind`: release,
  hooks, checks, update, pull, script, and agent, any other a script), its `Repo` the run's
  repository or else its folder with `~` (`Engine.where`), and its title the run's. The run's
  `Before` runs under the lock, so a release, a hooks action, a script, and a pull read the clone
  again there before the command starts. A run from before a restart that is still running is
  followed under a `KindOrphan` placeholder (`Engine.placeholder`), the only holder that names a
  `RunID`; a clone already held then is only logged. See [`internal/scriptrun`](scriptrun.md).
- **Release, Hooks, Repo scripts, and Pull** start their runs through the engine (`KindRelease`,
  `KindHooks` and `KindChecks`, `KindScript`, and `KindPull`). Release reads the holder into its
  facts (`Facts.Clone.Busy`, `Clone.Holder`), blocks the sheet with `CheckBusy` and the sentence,
  and offers `FixWatch` when the holder names a run; a gather under the release's own lock
  (`GatherRequest.Locked`) skips the read. The Hooks page shows a row's holder as `Repo.Busy`, and
  **Install and update all** leaves a busy clone's line `OutcomeSkipped` with the sentence. See
  [`internal/release`](release.md), [`internal/hooks`](hooks.md), and
  [`internal/scripts`](scripts.md).
- **Integrations** (`internal/integrations`). `admit` tries the lock of the folder an install
  places the program in (`KindInstall`, `Repo` the target folder as shown, `Title` "Install " and
  the program's name) and refuses at once with an `ErrInstallRefused` around the `*BusyError`. A
  tool's own update runs as a `KindUpdate` run in its copy's folder, so the two never meet there,
  an update from before a restart included. See [`internal/integrations`](integrations.md).
- **Autopilot's agents.** A background agent's run is a `KindAgent` run in the item's worktree
  (`agentrun`'s plan `Dir`), whose `Before` checks under that lock that the worktree holds exactly
  as many changed files as the user confirmed and that no session the app follows works there (see
  [Agents in the background](autopilot/agents-in-the-background.md)). A routine is never left
  waiting on a lock either: core turns an `ErrBusy` from making the worktree (`worktreeError`), from
  starting the run (`startError`), or from starting a routine's script (`startRefused`) into the
  match's queued wait, `busy_worktree` (`autopilot.BlockBusyWorktree`, "Queued: " and the sentence),
  which the engine tries again later ([A start held back on a
  session](autopilot/agents-in-the-terminal.md#a-start-held-back-on-a-session)).

Describe it's turns take no clone lock ([`internal/describeit`](describeit.md)).

## The order with `act`

The worktree service also holds `act`, a `sync.RWMutex` that serializes every removal, so a sweep
and a click never work on the same worktree at once; a `Maker` holds it shared while it makes or
resumes a worktree, so neither runs then either. Whoever needs `act` takes it before a clone's
lock, never after: `Maker.locked` takes `act.RLock`, then `holdClone`, and every removal and the
sweep take `act.Lock` first. Since a worktree change waits only for another worktree change, and
none of those waits on `act` while it holds a clone, the two locks never deadlock; and since a run
is never waited for, a removal holding `act`, which every launch then queues behind, never sits
behind a run.
