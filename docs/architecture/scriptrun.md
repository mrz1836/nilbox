# internal/scriptrun

[Docs](../README.md) › [Architecture](../README.md#architecture) · Code: [`internal/scriptrun`](../../internal/scriptrun) · Guide: [Runs](../guide/runs.md)

`internal/scriptrun/` runs the commands the integrations start (releases, hooks actions, checks,
scripts, a clone's pull, and a tool's own update), and a routine's agent in the background
(`KindAgent`: its own retention and log age, its stdin and folder, never on the Runs page or told as
a failure), and follows them, in the app or in the user's terminal, and owns `script_runs`. The
`Engine` is never bound, since a `Spec` holds functions: `Start` takes a slot for a run in the app,
then the clone's lock, failing at once when the clone is busy, runs the program through
`execpath.Start` in its own process group, writing straight into its log in `<data>/script-runs`
(four runs at a time in the app; a release never waits for a slot), and ends it with a per-kind
timeout or the stop ladder.

A spec's `Before`, run under the lock and a slot, may work the command out there, and its `Progress`
says how far the run has got and whether Stop is let through now: the bound `Run` carries it, `Stop`
refuses with the kind's words (`ErrStopRefused`), quitting signals a run only once its kind lets it
stop (within `QuitKillAfter`; one still held back is left going for the next start), and
`Config.Progressed` hears each change with the run's ID, which core sends the window as
`event.ScriptRunProgress` (`scriptruns:progress`), never the whole list's `event.ScriptRunsUpdated`.
A spec's `Recorded` is told the outcome only once the run's end is recorded, so a record that fails
tells nothing twice, and its `Released` once the run has let go of its clone, so a page that reads
the clone again finds it free.

After `Released`, once the run has let go of its clone and its slot, the engine's own
`Config.Failed` hears each run whose end is recorded as failed (one stopped at its time limit
included), of every kind but `KindAgent`, in the app or the terminal, started now or settled after a
restart (by a `Verifier` or by how it ended), and never one that couldn't start (its start's error
says so), one the user or the quit stopped, one a hang-up, Control-C, or SIGTERM ended (exit 129,
130, or 143, unless the engine sent it at the time limit), one interrupted, one that succeeded, or
one left `Unsettled`: once, since the record that ends a run changes only a row still recorded as
going. It gets a `scriptrun.Failure`, worded there ("Checks failed: Checks · all files", the run's
folder in place of a repository it lacks, with the run's summary or "Timed out after 30 minutes."),
which core (`runFailures`) hands the notify `Dispatcher`'s `Gather` as a `run_failed` notification
about no item: recorded at once, its banner held `GatherWindow` (2 s) with those of the failures
that follow, so a burst past `MaxBanners` (Install and update all, a restart's reconcile) shows one
summary, under the same rules as every kind's banner; core's `Close` drops the banners still held
(the history keeps their entries) and cuts short a batch already showing (each batch shows within
the gathering's lifetime, which `Close` ends), waiting for it to end, before the database closes.

The entry keeps the run's ID and its recorded start as RFC 3339 (`notifications.run_id` and
`run_started`, `Entry.RunID`/`RunStarted`, `Notification.RunID`/`RunStarted`, and
`event.RevealItem`'s for a banner's click), and the window opens it in Scripts → Runs only once the
run with that ID says it started then, since Clear history lets an ID be handed out again;
otherwise, or while the Scripts tab is gone, a toast says why. scriptrun never imports notify (the
repoguard boundary test).

A spec's `Keep`, which its `Before` may fill in, is written as JSON once `Before` has run into the
run's own `<id>.kept` beside its log, never into its record or the window's view; `Engine.Kept`
reads it back for a kind's `Verifier`, and it is removed once the run's end is recorded (it stays
while a record fails, for the next start) and with the run's other files. Its `Run` is a deferred
desktop worker: it reconciles the runs left from before a restart (a live group whose start time
matches is orphaned and followed again; one that is gone was interrupted; a terminal run is told
apart by its PID and exit files; every one is held under a placeholder lock and registered before
any is settled, and a kind's `Verifier`, registered with `RegisterVerifier`, gives such a run its
`Callbacks` (its output reader, progress, `Finish`, `Recorded`, and `Released`, which a `Spec`
embeds) again from its record's intent and what it kept, built by the same helper as the kind's
`Spec`, so it is read and verified as it ends, as tracked work) and, as the app quits, stops every
run in the app and records it stopped, unless its `Finish` returns `Unsettled` (a release whose
check of GitHub the quit cut off), which leaves it recorded as going for the next start.

`Active` is the desktop shell's quit guard (`desktop.QuitGuard`): while a run goes in the app,
`Shell.ShouldQuit` holds a quit and the window asks (`event.QuitRequested`), until the bound
`Service.QuitAnyway` emits the Go-only `event.QuitConfirmed`, or `LetThemFinish`, offered while
every agent's run going may outlive the quit (`Run.OutlivesQuit`), emits it with `Engine.LetFinish`
set, so `StopAll` lets those runs go, still recorded as going for the next start; `QuitAnyway` and
`KeepRunning` clear it.

The Service's `OpenInTerminal` opens the user's terminal in a run's folder and runs nothing there,
and `ShowLog` shows a run's log in Finder, both by the run's ID alone. `InUse` tells an install a
tool is running, never counting a `KindUpdate` run, which is the tool updating itself (it holds its
copy's folder, not a clone, for up to ten minutes), and `Updating` names such a run going now, one
from before a restart included. `Amend` rewrites an ended run's summary and its kind's own result,
in one transaction with the read its change is given, as a release's follow of its workflow does,
and `Ended` lists a kind's ended runs, from which that follow resumes. The history keeps 200 runs
and 512 MB of logs, and cuts a long log to its head and tail; Run again works the command out afresh
from its source, never for a release or a tool's update.

`OpenInTerminal` opens the folder through the engine's `TerminalOpener` (its
`OpenFolder(ctx, name, dir)`, core's `terminalRuns`), as the package's `OpenFolder` does for
Hooks' and Release's clone, under `FolderScript`: Warp opens the folder itself and no script is
written, and any other terminal gets the script `name` names ([internal/launcher](launcher.md)).
A run in the terminal is followed the same in every terminal, but a closed Warp tab keeps its
shell about 60 seconds, so such a run ends as "The terminal closed while this was running."
that much later; one whose script the terminal never ran (Warp before its welcome is finished)
ends after `pidWait` (2 minutes) as "The terminal didn't start the run."
