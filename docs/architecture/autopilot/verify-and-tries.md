# Verify, tries, the loop guard, and profile fallback

[Docs](../../README.md) › [Architecture](../../README.md#architecture) › [`internal/autopilot`](README.md) · Code: [`internal/autopilot`](../../../internal/autopilot) · Guide: [Closing the loop: Wait for the checks](../../guide/autopilot/wait-for-the-checks.md), [When a routine misbehaves](../../guide/autopilot/when-a-routine-misbehaves.md)

- **Verify** (`verify.go`) is the engine's own (`catalog.DispatchVerify` names no registered
  dispatcher): each cycle judges every verifying match from the facts the cycle read (the
  open matches' items are read beside the watched ones, so one that left every watch is
  judged too) through the pure `judgeVerify`: the run's commit (`run_commit`, or its worktree's
  `head_after` once that is the pull request's head, for a report that didn't say), the head
  it started from, the pull request's head, its check counts, its checks' names and when the
  last finished (`ChecksDone`), and when its details were read (`facts.Facts.DetailsAt`, from
  `item_details.fetched_at`). The verdict (`Verdict`: fixed, still failing, unknown,
  overtaken, push rejected, no push) is the wait step's (`stepRun.Verdict`, with its Only if
  outcome, `passed` or `stillFails`) and the run's (`autopilot_runs.verdict` and
  `verified_at`, `SetRunVerdict`), written in one transaction with the match moving to its
  next step; the step's settle and give-up times are its own (`WaitStep.Settle`, `GiveUp`).
  While it waits, it asks only for what it lacks: the check names once none runs
  (`ReadChecks`, at most a minute apart), and the item itself (`ReadItem`, one point, at most
  every `verifyReadAfter`, two minutes, and only a minute after the run ended, so the read the
  run's end asks for comes first) while its push isn't seen, its details are that old, or no
  check has appeared. A routine with a Wait for the checks follows the checks of what its
  searches return (`WatchSearch.Follow`), so the checks follower reads it every round and
  verify reads nothing itself. A verdict on a fix's checks is told once as **Checks after a
  fix** (`EngineConfig.Checks`, a `NoticeChecks` keyed by the match and its run); a still
  failing verdict on the routine's last try says it gave up (`gaveUpWords`).
- **Tries** are a routine's counted runs on an item since its last Try again, across fire
  keys, so a Dependabot rebase, a new key, resets nothing (for agent runs as for GitHub
  actions); an interrupted run, one stopped for its item, one that sent nothing, and one out of
  usage count as none (`endCounts`). The occurrence after the last try closes `skipped:
  tries`, *Gave up after 2 tries.*, which `TryAgain` reopens.
- **The loop guard:** `eval.History.Working` reports an agent of any routine's working on the
  item (a run starting, running, or stopping), and New commits is unknown while it does,
  unless the trigger lets a routine's own pushes through, so a commit the agent pushes
  mid-run fires nothing before its run's end records it among `OwnHeads`.
- **Profile fallback** (`fallback.go`, core's `backgroundAgents.dispatch`): an end out of
  usage carries its limit (`RunEnd.Limit`: its type and when it resets, from
  `agentrun.Outcome.Limit`), and the engine rests the run's agent and profile (as its launch
  recorded them) until then, an hour when it doesn't say, in its own state
  (`engineState.Resting`, so a rest outlives a restart). Every agent step's `Job.Resting`, and
  so its `RunRequest`, holds the profiles resting now; the runner plans the step's chain in
  order (the step itself, then each fallback, `fallbackStep`: that agent's profile for every
  repository, the step's model and effort only for a fallback of the agent the step resolved
  to) and starts the first that isn't resting. A fallback whose plan says its agent can't run
  in the background here (`cantRunHere`: a preset it can't keep, as Codex can't keep GitHub
  tasks only, no background half, or no program) is passed over; the step's own profile never
  is. With nothing left it answers `Wait` (`out_of_usage`, queued, or `not_ready` when it
  passed a fallback over) with `RetryAt`, the soonest rest's end, and the engine holds the
  match until then (`heldBack.until`) without reading the item. Readiness marks what only a
  fallback needs (`Need.Fallback`), which is only ever a warning.
