# The engine and the ledger

[Docs](../../README.md) › [Architecture](../../README.md#architecture) › [`internal/autopilot`](README.md) · Code: [`internal/autopilot`](../../../internal/autopilot) · Guide: [Budgets and caps](../../guide/autopilot/budgets-and-caps.md), [When a routine misbehaves](../../guide/autopilot/when-a-routine-misbehaves.md)

**The ledger** (migration `0103_autopilot`, all of it Autopilot's) records what each
routine did with each item it matched. `autopilot_matches` holds one row per routine, item,
fire key, and Dry run (unique), so an existing row blocks a repeat: once per occurrence holds
across refreshes, check re-runs, and restarts, and a Dry run's record never blocks a real
run. Each row keeps a copy of its item (repository, kind, number, title, URL; `item_id` is set
NULL once the item is gone), the decision's trace as it was, the steps it started with and
how each went (`steps`, JSON, with `step` where it is: a routine saved with other steps never
changes what a match has done), the spec's fingerprint (`spec_hash`), the head it was decided
on, whether auto-merge was on, the user's OK (`approved`), and how many runs it started
(`attempt`). A row is open (`waiting`, `blocked`, `queued`, `running`, `verifying`, with
`blocked_by`) or closed (`done`, `skipped`, `dismissed`, `closed`, `baseline`, with `reason`),
and every change is a compare-and-set on the state it was read in (`UpdateMatch ... WHERE
state = was`). Only the routine's own outcomes close a row: it ran (`done`), the user skipped
it (`dismissed`), it ran out of tries or met the outside contributor rule (`skipped`), or the
item moved on (`closed`: merged, closed, gone, left the watch, its fire key moved, no longer
matches, the routine changed or was switched off, the item's sheet no longer offers the
action). Everything that only delays a start keeps it open, retried every cycle. A row that
closed without ever running (no longer matches, left the watch, moved on, the routine changed
or was switched off, `Reason.reopens`) opens again when the same occurrence matches again, but
only if none of its steps ever started (`reopensRow`: no run, no step moved past, none running,
done, or failed): a row that started a step, a local one included, is the occurrence's record,
which only Try again opens again, and `Matching` counts it as recorded.
`autopilot_runs` holds one row per run a match starts (an agent's, in the background or the
terminal, or a GitHub action the app sends), with every column the run view, the caps, and the
stats read, and a partial unique index on `item_id` for the active statuses (starting,
running, stopping), so a second active run on an item fails in the database whatever the
engine's own checks say. `autopilot_tries` keeps when Try again last reset a routine's count
on an item, `autopilot_baselines` what a routine first saw of an item (its head, its comment
count, for New commits' first commit and New comments), `autopilot_hands_off` the items a
routine leaves alone, and `autopilot_effects` what its
local steps did to their items' triage ([Local actions](local-actions.md)).
`autopilot_matches` is indexed by `item_id`, which every read by
item (`LatestMatches` and `ItemHistory`) searches. The engine's own state,
`autopilot.engine` (a JSON pref only the engine writes, `enginestate.go`), holds when Pause
began, the last refresh it saw, when the last catch-up began and the routines still in it, the
last cycle, each GitHub routine's first-pass fingerprint, the routines whose current matches
become their baseline, the problems, and when the retention last ran; a deleted or
switched-off routine's entries are dropped.

**The engine** (`NewEngine`, never bound; the Service is its face) is a deferred desktop worker
with one loop of its own (`engine.go`), the only writer of matches and the only thing that
starts anything. It wakes on what the inbox stores (core's `engineOnInbox` for a whole
refresh's `event.InboxUpdated`, and the `Changed` seam), on a change of Autopilot's settings
(core's relay turns `event.AutopilotChanged` into `Wake`) or of a routine, on what the inbox
saw happen to items (core's `outcomesFanOut` tells it beside the mirror of the GitHub
notifications, without waiting), on its 1-minute tick, on a window's command, and on a
worker's result; what comes during a cycle folds into one more. A cycle (`cycle.go`) reads
SQLite and evaluates, milliseconds: the enabled routines of the account signed in (one that
won't load is a problem, and skipped), the items each watches, their facts, the open matches'
items' states, the runs on them, and the day's counts; then it decides each routine on each
item (with the `eval.Plan` it prepares once per routine each cycle, `routineRun.decide`, and
the ledger's `History`: tries since the last reset, the last
outcome, the head it first saw, the comments it last counted, and the commits routines' runs
pushed), records each new match, closes what moved on, and moves each open match through its
steps (`advance.go`). Everything slow runs on a worker that hands its result back to the loop
(a mailbox that never blocks a worker): check names for the candidates whose decision waits
on them, the states of items that left a watch (read before anything closes), the read of an
item right before a start, a dispatch, a stop. So a command is applied within a fraction of a
second, mid-cycle or not. Each cycle is logged (its length, the routines, the items, the
matches, the starts, the closes, the reads), kept as `Engine().LastCycle`, and told to the
window as `autopilot:updated`, at most four times a second. A cycle that panics is recovered
and logged, and the loop holds off cycles for a minute; commands are still answered.

- **The first cycle after a start** waits for a refresh that stored after the engine was built
  and, when the routines have searches, for a watch pass after it (after Autopilot (beta) was
  last turned on, `onSince`), so the engine never acts on what the database held when the app
  was last open, or while it was off; when a refresh came but no pass (the very first one,
  before an account was stored, or Autopilot just turned on), it asks the inbox for the
  searches at once (`Routines.WatchNow`, a pass outside a whole refresh). Commands don't wait
  for it.
- **The lock** (`lock.go`) is a `flock` on `<data>/autopilot.lock` (`appdata.Paths.AutopilotLock`),
  tried as the engine is built (in `assemble`, so the copy that will hold it sends the searches
  from its first refresh) and on every tick, both only while Autopilot (beta) is on ([Autopilot
  (beta)](routines-kept.md#autopilot-beta)): only the holder runs cycles and applies commands
  (another copy refuses them, `ErrElsewhere`, and `Engine().Elsewhere` says so), and only the
  holder sends the routines' searches at all (`Searches.HoldsWatches`, which the inbox's
  `WatchHolder` reads before a watch pass), so two copies on one data folder never replace each
  other's rows; the holder builds its snapshot of the searches again from the database on every
  tick, which catches a routine changed in the other copy. A copy that doesn't hold it reads
  `Glance` on every tick and sends `autopilot:updated` when it reads otherwise than on the tick
  before (`followElsewhere`), since it runs no cycle that would: a Pause or a routine switched
  on in the holder reaches its menu bar and its window's close within a minute. The system
  lets go of the lock when the process ends.
- **The runs going** are checked when the lock is taken and on every tick (`reconcile`), but
  those a worker of this copy is starting or dispatching, and those whose `RunEnded` is on its
  way. A GitHub action still recorded as going was cut off by a quit or a crash, maybe after
  GitHub took it, so it is never sent again on its own: it ends interrupted, and its match waits
  for the user, worded so they check the item on GitHub first. An agent's run is asked of its
  runner (`Runner.Alive`, a `Liveness`), and taken as gone or never started only once two checks
  a tick apart found it so, as a runner may let go of a run a moment before its end is delivered
  (the `Runner` contract keeps a run `LiveAlive` until then). One both checks found the runner
  positively never started (`LiveNever`, still `starting`, as a start the quit cut off before it
  reached the runner) goes back to the queue, its match with it; any other found gone ends
  interrupted. `LiveNever` must hold across restarts and never come from a history that forgets
  runs (a runner that prunes its records answers `LiveGone` or `LiveUnknown` for a run it no
  longer knows), so a run that started and crashed is never run again on its own. A runner's
  end that comes after the engine recorded the run interrupted is kept over that record
  (`ReviseRunEnd`), its cost and commit with it, and moves the match on while it still waits on
  that interruption. One the runner can't say of (`LiveUnknown`, or an error) is asked again on
  the next tick, and ends interrupted once it has been unknown past the longest time limit a run
  may have and a grace (`unknownFor`), so no run holds its item's slot, an agent's slot, or a
  reservation for good. A start the runner refuses as it closes (`ErrClosed`) stays queued, and
  no agent starts in this run of the engine after it. A start that never happened, either way,
  ends `not_started`: it counts as no try and no run (`Attempt` is taken back, so the match is
  decided again like any queued one), and toward none of the day's counts or the daily cap.
  A run's end and its match's move are written in one transaction, a dispatch's answer too; an
  end that can't be written is tried again on the next tick, and each tick also moves on any
  match still `running` on a run the ledger records as ended (`unstrand`), by the end it keeps,
  or as interrupted when that end can't say (a stop, whoever asked for it). A stop that fails is
  tried again a tick later, never at once, and its reason is kept: a run that ends stopped
  anyway closes its match for what the stop was for (merged, closed, gone).
- **A catch-up** begins after a gap longer than Settings' catch-up hours (with the app closed or
  open, the Mac asleep): at once, on a cycle that finds the last successful refresh that long
  ago, before any refresh came; at the first successful refresh that long after the one before;
  or at a Resume after a pause as long. One gap begins one catch-up. Every routine on then is in
  it, and each leaves it on its own once a cycle has decided on what it watches read after the
  catch-up began (each of its searches answered with no error, and a refresh, when it watches
  the inbox's lists or has no search): until then its agent and GitHub steps wait for the user
  (`catch_up`), and a routine whose own search keeps failing says so, while the others go on.
- **A start** goes through, in Go, in this order (`gate`): the user's OK for a routine that asks
  first, and in a catch-up an Autopilot routine's too; no one signed in to GitHub (the last
  refresh found no token, or GitHub refused it: `signed_out`), Pause, and the routine's active
  hours, all three of which hold a local step too; the tries (`skipped: tries`), the routine's runs a day, the agent runs
  a day, the GitHub actions a day, and the wait between tries, none of which a manual run counts
  toward; a free slot and the daily cap and the routine's own budget, reserved in their unit,
  for an agent; the outside contributor rule (an agent closes `skipped`, a GitHub action waits);
  a fork's branch for a run that pushes; and one active run per item. Then the item is read
  fresh on a worker (`ReadItem`, the inbox's `RefreshItem`) and the `Holds` the later parts
  register are asked; the cycle after that read, which evaluated the item on what it stored
  (a moved head closes the match as moved on), records the run and the reservation in one
  transaction and hands the step to its dispatcher. A local step runs at once, through its
  dispatcher, held back only by a sign-out, Pause, and the active hours. A day runs from local
  midnight to local midnight, as the clock tells local time: a run's cost belongs to the day it
  started, and every run still working counts what it reserved against today's cap whatever
  day it started.
- **The dispatch seam** (`dispatch.go`): a narrow `Dispatcher` per part, keyed by the catalog's
  dispatcher names (`EngineConfig.Dispatchers`: triage, notices, github, verify, merging, review),
  and the agent part's `Runner` (`Dispatch` starts a run, `Stop` and `StopAll` stop at once,
  `Alive`), whose ends come back through `Engine.RunEnded`. A `Job` carries the match, the run, the
  routine, the item's copy, the head it is pinned to, the step, and the budget; a `Result` says it
  runs on, how it ended (an Only if outcome), whether GitHub took a write, and when the item's sheet
  no longer offers the action or offers it with warnings. A step with no dispatcher registered can't
  run, and the Service keeps a routine with such a step in Dry run (`Offers`, the catalog's own by
  default, which a test hands in with fakes). The triage steps are offered: the engine registers its
  own triage dispatcher over `EngineConfig.Triage` (core's `inboxTriage`, [Local
  actions](local-actions.md)) unless `Dispatchers` names one; so are the notices (Notify me and Tell
  me, [Notices](notices.md)), whose dispatcher core registers, the agent step, whose `Runner` core
  registers ([Agents in the terminal](agents-in-the-terminal.md)), and the GitHub step and Mark the
  GitHub notification done ([The GitHub step](github-step.md)), whose dispatcher the engine
  registers over the Service's `GitHubActions`; a routine with any other step still runs in Dry run,
  and the engine records what it would run. A `Result` may also say the step didn't start and what
  the match waits on instead (`Wait`, `WaitWords`, and a `Hold` the user confirms), or that it goes
  back to the queue (`Requeue`), and what a launch started (`Launched`, recorded on the run,
  `RecordRunLaunch`). For the notices, a job also carries when its match was made, the routine's own
  banner switches (`Notices`), why it matched (`Reason`, from the trace), and how the steps before
  it went (`Before`). `dispatch` hands a job over only while `Engine.Enabled` says Autopilot (beta)
  is on, asked on the worker right before `Dispatch`, so no part (the agent runner's starts
  included) is handed a step after it was turned off; the freshness read before an agent's start or
  a GitHub action asks it too, so nothing is read from GitHub for a start Autopilot turned off
  since.
- **A run's end** moves its match on: an interrupted one waits for the user (`interrupted`, counting
  no try), one the user stopped closes `dismissed`, one the engine stopped because its item merged
  or closed closes `closed` (counting no try), one out of usage (`RunOutOfUsage`) rests its profile
  and goes back to the queue at the same step (counting no try, [Profile
  fallback](verify-and-tries.md)), and one that ended goes to its next step, straight to `verifying`
  when it pushed and Wait for the checks is next, so the push, which moves the item's head, never
  closes it as having moved on. A match that started a run is closed only by its item being done
  with; its fire key moves by design. While one of a routine's matches on an item is running or
  verifying (`pass.active`), another of its matches there waits (`settling`).
- **Settle what moved on:** an open match whose item merged, closed, or is gone closes; one
  whose item left every watch of its routine has its state read (`ReadStates`, one request, one
  at a time per item, again every half hour), so it closes as merged or closed when it did, and
  closes as having left the watch only while every search of the routine last answered in full:
  a search that failed, was truncated, or was read in part keeps its rows, stale, and even once
  the inbox drops a truncated tail, its items are stale, not gone; one whose fire key moved
  closes as moved on, one the routine no longer matches as no longer matches (or, after an edit,
  as the routine changed), and the rest take the routine's new steps. A running agent whose item
  merged or closed is stopped through the runner; a verifying match closes, and one whose item
  is still there is judged ([Verify](verify-and-tries.md)), its routine switched off or not.
- **The first pass** of a routine with a GitHub action, after it is switched on, leaves Dry run,
  changes mode, or its spec changes (a fingerprint of the spec, when it was last switched on,
  and its mode, so switching it off and on in the other copy between two cycles counts too;
  recorded only for a live, enabled routine), waits until its searches were read since, then
  queues at most `FirstCap` (25) matches; over that, none, and they wait as a `backlog` for
  Approve.
- **Apply to current matches:** `SetEnabled`, `SetMode`, and `Save` (`Draft.Current`) take what
  to do with what the routine matches now and has no record of (`Matching` counts them, as
  `Test` reads them, and says when the count may be short, `Incomplete`): `run` them too, or
  `fromNow`, applied as one command with the change, so no cycle runs between; the engine then
  records those matches as `baseline` once the routine's searches were read since, and nothing
  runs on them. `fromNow` is refused, with why, in a copy that doesn't run the engine (it hasn't
  started yet, or another copy holds it), rather than dropped, but only for a change the
  question applies to (switching on, leaving Dry run, a save that changes the Watch, When, If,
  or Once per of a routine that is on, `watchChanged`, since Once per decides each match's
  fire key, each told before the change is made); any other change, and `run`, is made there at
  once.
- **Problems** (`upkeep.go`): a search GitHub fails, and an enabled routine that won't load, are
  recorded once each, keyed by routine and problem, with when they began, in the engine's state and
  `Engine().Problems`, and told through a `ProblemSink` once (core registers `NewProblemNotices`, in
  [Notices](notices.md)), with the routine's name, when it was made, and its own banner switch for
  problems; one that couldn't be told stays untold and is told on a later cycle, and one that ends
  is forgotten, so it is told again when it comes back.
- **Retention:** once an hour, a closed match closed longer ago than the history's days goes,
  with its runs, but only once its item is known to be done with (gone, merged or closed, or out
  of every list and watch since then): deleting the record of an item still open would let it
  fire again. Open matches never go. `ClearHistory` does the same whatever a row's age, but
  "out of every list and watch" still means for the history's days, since the app reads no more
  of an item once it left them; it keeps a closed match that may still block its occurrence
  without its trace and summaries. What deleted routines
  kept of their items goes, and a deleted routine nothing refers to goes for good. The inbox's
  prune keeps every item a match or a run refers to (`KeptItems`, one of core's `itemKeepers`).

**The window's actions** are commands the loop applies, then answers (`commands.go`), so a
double click, a banner's click racing a cycle, or an Approve during a refresh changes a match
once: `Approve` (an Ask first match's OK, a catch-up's, a backlog's, an interrupted run's),
`Skip` (refused for a running one), `RunNow` (once per click, on an item its conditions match,
counting toward no cap or try), `TryAgain` (resets the tries and opens again what gave up on
the occurrence that still holds), `LetActAgain`, `Undo` ([Local actions](local-actions.md)), and
`ClearHistory`. A command before the loop runs is refused (`ErrNotRunning`), and one while
another copy holds the engine
(`ErrElsewhere`). `StopRun` and `StopAll` go straight to the runner, never queued behind the
loop, so they work before the first refresh, offline, or signed out. The reads (`activity.go`)
read the database alone: `Activity` (a page of matches before an ID, by local day, with their
runs and effects, each day's grouped lines of local steps, and, on the first page, the items
the routines leave alone), `ActivityFrom` (the same, of the matches from an ID on and before
another, 0 for up to the newest, at most 500, `ListMatchesFrom`: a stretch of the pages the
window has loaded read again in one call, by its own bounds, so their grouped lines join by
`Key` and nothing falls between them; `More` and `Next` say where the rest of the stretch
begins, and only a stretch up to the newest says what the routines leave alone),
`ItemHistory`,
`LatestMatches` (for each item among the ids it is
given, at most 500, its newest match that did something there or waits to, in one query,
`ListLatestItemMatches`: open, or closed as done, skipped, or dismissed, never a baseline or
one that closed as the item or the routine moved on before it acted, with its runs and
effects, for the chip on every row; the window asks in chunks of 500, `inChunks`),
`Upcoming` (for the items among the ids, the routines that will run there by themselves,
from the engine's memory, none while Autopilot is off or paused, signed out, or run by
another copy; see [What runs next](upcoming.md)), `Explain`
(the decision now, and what the ledger knows:
the record for the occurrence, the tries used, not in the watch, left alone), and `Engine`. No
result holds a nil list.
