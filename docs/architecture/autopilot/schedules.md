# Schedules, the digest, the script step, and a routine's week

What the user sees is in `docs/desktop.md`'s [Building a routine](../../guide/autopilot/building-a-routine.md)
and [Testing a routine](../../guide/autopilot/testing-a-routine.md).

**Schedules.** On a schedule (`catalog.EventSchedule`, `spec.Schedule`) is a state the
evaluator decides from the moment it is given, as every trigger is: it holds from the
schedule's time of day, in that moment's own location, to the end of a day it is due (every
day, a weekday, or the week's day), under the fire key `schedule:<local date>`. So it needs
nothing of its own in the engine: the 1-minute tick runs a cycle, which reaches the time; a
time the Mac slept through holds when it wakes (the sim's `Sleep` jumps with no tick, then the
first cycle fires it), and only that day; daylight saving and a new time zone keep the time of
day (`time.Date` in the clock's location; the sim's `Clock.Travel`), and a clock set back after
it fired finds the day's key recorded. The scenarios in `sim/schedule_test.go` hold each case.
`Schedule.Style` (`catalog.ScheduleStyles`: `each`, written as none, or `digest`) says how
it tells of what it matched; `spec.Validate` holds a digest's Notify me to the first step, so
the matches a time makes, which run their first step in the cycle that records them, tell
together.

**The digest** (`digest.go`). As a cycle ends, `tellNotices` takes out the Notify me jobs of a
digest routine's matches (`pass.digests`; a manual run's tells on its own) and hands each
routine's as one job carrying a `Digest` to the notices dispatcher (`dispatchDigest`: one
`Dispatch`, its answer applied to each match, so each step is done once the notice is told).
`digestOf` is pure: it puts each item in the first group that holds of it (a pull request
failing, in conflict, with changes requested, behind, with checks running, a draft, approved,
else waiting for review; an issue quiet a week or more, else open; a value not known yet passes
on), counts the groups in that order, lists the items so, and names them by kind and, when one
bot opened all, by it. The notice (`digestNotice`) is an alert about no item, headed by the
routine, *3 Dependabot pull requests need you*, *2 failing, 1 waiting for review: acme-org/web
#88, …* (five named, the rest counted), keyed by the routine, when it was made, and a hash of
its matches, so a digest told again after a quit is told once and a later one is its own.

**Run one of my scripts** (`scripts.go`, `catalog.StepScript`, `spec.ScriptStep`: the
script's ID, and the name it was picked under, for the sentence and an export). It is an
asks-first step of the scripts part (`catalog.DispatchScripts`), recorded as a run under
runner `script` (`autopilot_runs`' check takes that runner) and handed to `EngineConfig.Scripts`, a `Runner`, as
an agent's is: Dispatch starts it and returns Running, and its end comes through `RunEnded`;
the engine routes a run's `Alive`, a Stop, and Stop all by its runner (`runnerFor`). Its limits
are a GitHub action's: the tries, the routine's runs a day, Pause, the active hours, an outside
contributor's item waiting for the user, and one run per item; it takes no agent's slot and
counts toward neither the agents' caps nor the GitHub actions' (`isAgentRunner`). The user's
own scripts reach the engine and the builder through `MyScripts` (core's `myScripts`, over
`scripts.Routines.Mine`), which the bound `MyScripts` lists, and which `Save` and `Describe`
check a step's ID against (the draft leaf's `scriptProblems`). **Ask before running** is the
engine's to honor,
since Repo scripts' Run leaves it to its window: the gate's first check (`scriptAsks`) holds a
script step whose script asks until the step's `Confirmed` holds `ScriptKey(id, look)`, the
script as the user saw it (its look hashes its name, command, place, and the setting), whatever
the mode, a manual run's and an approved match's too, waiting `script_asks` with the hold's key
on the step, which `Approve` confirms (`confirmHold`); a script edited since asks again.
Core's runner (`adapters_scriptsteps.go`, `scriptSteps`) checks it once more as it starts
(`ScriptAsks`), finds the item's clone as a launch does (`launcher.CloneTarget`), and runs it
through `scripts.Routines.Run`, the same spec Repo scripts' Run works out, always in the app,
refusing a script saved otherwise since its check (`ErrChanged`), its end told back from the
run engine's `Recorded` with how it ended (`RoutineEnd`, from its `Finish`: the user's Stop, its
time limit, the app's quit, which interrupts it). A busy clone waits queued, Scripts turned off
or a script that can't run waits not ready, no clone waits for the user, and a script gone or
not showing in the clone fails the step. A step names its script by its ID and the name it was
picked under: a script saved under another name waits not ready, and `Save` and `Describe` say
so (`renamedWords`), since an ID a routine brought from another Mac (an export, a backup) may
name another script here; the builder offers it to be picked again. After a restart, `Alive` reads the run engine's record
of the run's `script_run_id` and tells the engine an end it didn't hear.

**Run on the matches now** (`runon.go`). The bound `RunOnMatches(id, items)` is a command on the
loop: for each item the saved routine matches now and would run on (`TestItem.RunsNow`: no
record of its occurrence, a record that closed before it ran, or a baseline), it records the
routine's own match for that occurrence, approved, or opens that record again approved, so the
routine never runs on the occurrence twice and an Ask first routine doesn't ask again; every
other limit still holds it. A routine that is off, broken, or another account's is refused,
and so is a list longer than a test's.

**A routine's week** (`stats.go`). The bound `Stats(id)` reads the routine's runs started in
the last seven days with their matches' steps (`ListRoutineWeekRuns`) and counts, in Go
(`weekStats`), its agents' runs and its scripts', the fixes (an agent's run whose verdict
passed, or whose match's Wait for the checks after its pushed step passed, once per match),
merges, reviews posted, and GitHub actions GitHub took (each by the kind of the step its run
ran), and what the runs reported spending: the dollars, how many runs reported one, and how
many of those an estimate (`cost_estimated`, which a subscription's sign-in reports as the
API's price; the window says *about* for every figure), and the tokens. A run that never
started counts nowhere. A run in the terminal reports neither, and its session's tokens are
the session panel's (`internal/sessions`' `Usage`, from the agent's own files). A background
run's session reads its run's instead: `Usage` asks its `RunTokens` (core's
`backgroundTokens`, over `Engine.BackgroundTokens`, by the run engine's run through the
index `0016` adds on `script_run_id`), which the runner keeps current (`Engine.RunUsage`),
never the transcript, and marks them `FromRun`.

**The cycle's reads.** A cycle takes each routine's matches by occurrence and what it first saw
of each item from the ledger's keys the engine keeps in memory, reads the items each leaves
alone and its resets of the tries in two reads (`readLedger`, which Run on the matches now
shares), indexes each routine's last closed match by item once (`lastClosed`, which the
evaluator's history reads), and keeps its own state only when it changed beyond the last cycle
(`keepCycle`). `BenchmarkEngineCycle` measures it
([performance.md](../../performance.md#the-engines-cycle)).

**The ledger's keys in memory** (`ledgerindex.go`, `ledgerIndex`, the engine's ledger's `index`,
which only the loop reads or changes) hold every match with an item, by routine and occurrence
(`keyRow`: its ID, state, reason, and whether it started a step, `startedAny`, which is all
`record`, `reopensRow`, `runsNow`, and `lastClosed` read), and every baseline, by routine and
item. The engine is the ledger's only writer, so each of its writes notes what it wrote where
it writes it: the ledger's `insert` and `update` the match's key, `saveBaseline` the baseline
once its `:execrows` query says it wrote one, and the prune's `clear` the matches it deletes. A
write through `ledger.inTx` notes into that transaction's list (`begin`, keyed by its
`*queries.Queries`), applied once it commits and dropped when it rolls back (`end`), and a
write through a transaction the ledger didn't open has the keys built again. They are built
from the database (`ListMatchKeys`, `ListAllBaselines`) on their first use, whenever the
engine takes the lock (`takeLock`, since another copy of the app may have written the ledger
while it held it), and whenever one count of the rows (`CountLedgerKeys`), the only read a
steady cycle makes of them, differs from theirs: what changes these rows besides the engine
only deletes (the inbox's prune of an old item takes its baselines with it and sets its
matches' `item_id` NULL; the hourly prune's forgetting of deleted routines' baselines), so a
count tells it. The items a routine leaves alone are read every cycle, since the user's
reversal records them in whichever copy of the app it happens. `TestOnlyTheLedgerWritesWhatItsKeysHold`
holds every query that writes these rows to the one function that notes it, and
`EngineConfig.CheckLedger` has the engine compare the keys with the database (`diff`) after
every cycle, command, tick, and worker's result while it holds the lock: the simulator sets it
for every scenario and fails on any difference (`sim.drift`).
