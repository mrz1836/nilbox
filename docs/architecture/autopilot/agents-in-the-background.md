# Agents in the background

[Docs](../../README.md) › [Architecture](../../README.md#architecture) › [`internal/autopilot`](README.md) · Code: [`internal/agentrun`](../../../internal/agentrun), [`internal/autopilot`](../../../internal/autopilot) · Guide: [Agents in the background](../../guide/autopilot/agents-in-the-background.md), [Budgets and caps](../../guide/autopilot/budgets-and-caps.md)

A step `In the background` goes through the [start path](agents-in-the-terminal.md#the-start-path)
exactly as one in the terminal does, to the same `Runner`: core's `backgroundAgents`
(`internal/core/adapters_agentrun.go`) takes a step in the background itself and hands one in the
terminal to `agentRuns`. Its `Dispatch` is a launch's, with three differences:

1. **The plan.** `launcher.Backgrounds.Plan` (`launcher.NewBackgrounds`, a type of its own, so
   the bound launcher never plans one) plans the same `routineRequest` a terminal run would
   launch, with no terminal: the item's worktree, always, made at the head the match was
   decided on; the fence's access in place of the profile's (`Request.Fence`, the step's tools
   preset and the run's folder, which `resolvePermission` turns into `agent.AccessFence`: an
   outside contributor's item Read only whatever the fence says, a preset its agent can't
   keep `ErrFenceNotKept`, a start that waits `blocked: not_ready`); the profile's extra
   directories left out; and the agent's headless flags around the fence's own
   (`agent.HeadlessArgs`: `-p --output-format stream-json --verbose --permission-prompts none
   --session-id=<id>`, the model and effort, the fence's access flags, `--max-budget-usd`,
   `--json-schema` for the report, `--append-system-prompt` with the unattended note, and
   `--system-prompt-snapshot off`), a profile's extra arguments refused where they set what
   the fence sets or would loosen it (`agent.CheckFenced`). Its holds are a launch's own (`heldBack`): a
   session or a run in the worktree, the pull request's files under the unattended scope, and
   a dirty worktree, each confirmed only by the routine's user; after `EnsureWorktree`,
   `Backgrounds.Made` holds the worktree made as `heldBack` held the one planned, and a
   worktree off the head the plan checked waits (`waiting: dirty_worktree`). Then
   `Backgrounds.Fenced` works the run's fence out for the worktree made (`agent.PrepareFence`
   over `fence.Spec`: the preset, the worktree and its tools, `fence.RepoTools`, the item, its
   head branch and the clone's remote for a push, the step's own `Allow` and `Deny`, the run's
   folder, home, the data folder, and the profile's config folder, whose hook is the app's own
   executable in its hook mode), rebuilds the argv with the fence's line added to the
   unattended note (`FencedRun.Note`), and core writes its files into the run's folder
   (`FencedRun.Write`: the policy, the agent's settings, and a sandboxed preset's check
   folder); a push preset on a fork's pull request waits `blocked: cross_repository`. The
   prompt is filled in with `.Unattended` true and passed on stdin from the same folder
   (`<data>/agent-runs/<session>/prompt.md`).
2. **The environment** is the login shell's (or the app's with the finder's folders while it
   can't be read), with the plan's changes on top (`launcher.AgentEnv`: the profile's config
   folder or, for the default profile, none; no API key unless the user's setting lets one
   through), the config folder never named as the agent's default one, and the variables an
   agent session sets for itself dropped (`agent.HeadlessEnv`: `CLAUDECODE`, `CLAUDE_CODE_*`,
   `CLAUDE_EFFORT`, `CLAUDE_PID`, `CLAUDE_TMPDIR`).
3. **The run** is `internal/agentrun`'s. The `Runner` starts it as a `scriptrun.KindAgent` run
   (its own retention, `KeepAgentRuns` and `KeepAgentLogBytes`, and the age Settings' **Run
   logs kept** gives, apart from the scripts' runs, never listed on the Runs page: `Filter`
   leaves the kind out unless `WithAgents`), holding the worktree's lock (`clonelock.KindAgent`,
   keyed by the worktree, so two items of one clone run side by side), whose `Before` checks
   under that lock that the worktree holds no more changes than the user confirmed and that no
   session the app follows works there. It records the run as a session in the sessions
   ledger (`agent_sessions.run_id`, which the `Watcher` leaves to the run: `Ledger.RunState`
   moves its state from the stream, and hands the item to `Rereads` once it stops working), so
   the item's pill and panel follow it; the worktrees listing and the launcher's running hold
   read its folder (`RunFolders`), since the process list leaves out the app's own children.
   `Dispatch` returns `Running` with what it started (`Launched`, `ScriptRunID` included), and
   the run's tokens reach the ledger every 30 seconds (`Engine.RunUsage`).

**The stream** (the leaf [`internal/agentrun/stream`](../agentrun/stream.md): `stream.go`,
`claude.go`, and Codex's `codex.go`, the runner picking one by the run's agent, `parserFor`)
splits the log into lines itself (one is read up to 64 MB, a longer one counted and skipped),
keeps the last 2,000 events (each text cut
to 8 KB, `runevent.MaxText`) and the progress (the latest step, the todo list, tokens counted
once per message), and counts a message it doesn't know (`Progress.Unknown`). Each event
carries where its line starts in the log and which of the line's events it is (`Offset`,
`Part`, together its key) and the latest time a line gave (`At`, from the assistant's and the
user's messages' `timestamp`). `runevent` is the leaf both sides share; the window asks for a
run's events after the last it has (`Service.RunEvents`) on each `event.AutopilotRun`
(`autopilot:run`, at most four a second a run).

## The run view (`agentrun/view.go`, `autopilot/runview.go`)

`RunView` is the ledger's run and its match; `RunEvents` follows a working run from the runner's
memory, whose first page says what its log holds before the events kept (`Page.Earlier`, `Before`,
the first event's line); a run that ended, or one the runner doesn't follow, is its log's last page
(`Runner.EventsBefore` at the end), and `RunEventsBefore` reads the page before an offset:
`readBack` reads the log back a 256 KB chunk at a time (`scriptrun.Service.Output`) until it holds
about 200 whole lines, or 1 MB and one (a line longer still is read whole, to its start), drops the
line the chunk started inside, keeps the last 200 lines, and parses them with a parser started at
that offset (`stream.Parser.From`), so a long log is read a page at a time, never whole. A page of
an ended run carries its `Ending` (`runevent.Ending`: the record's summary, the agent's report or
last message, whether it needs a person, its turns, time, models, commit, changed files, and its
last todo list, `Outcome.Todos`), and its progress the record's tokens. `RunDiff` is the worktree's
changes since the head the run started on, committed or not, with the files git doesn't track
(`gitread.WorktreeDiff`: `git diff --no-ext-diff --no-textconv --no-color --no-renames
--ignore-submodules=dirty <head> --`, run with the clone's own filter drivers off as a status is,
cut to 1 MB at a line's end, beside the porcelain v2 status's untracked files), from the worktree
the run's intent names (`Intent.Dir`). A run that isn't an agent's in the background (one in the
terminal, a script's, a GitHub action's) has no timeline here: its events are an empty page
(`followedElsewhere`), and the window says where it is followed. `RunBySession` is the ledger's run
a session is (`autopilot_runs.session_id`), which a session's Watch opens.

## Take over (`launcher.Service.TakeOver`, `internal/core/adapters_takeover.go`)

A run's session is resumable as any is (`ResumableSession.RunID`, and `Fence`, the run's preset and
folder from its record's intent, `agentrun.Runner.TakenOf`). `TakeOver` asks core's `TakeOvers`
first: it tells Autopilot's engine (`Engine.TakeOver`, a command applied on the loop before the
run's end can be: a run still working is marked, so its end closes its match done as `taken_over`,
counting as no try and telling no Agent finished; an ended one closes its match so when the match
still waits on its interruption or the user stopped it), stops the run while it works
(`Runner.Stop`), and waits for its end to be told (`Runner.Wait`, at most 30 seconds), so the
session is recorded ended and its conversation whole. Then it resumes the session as `Resume` does,
with `ResumeRequest.Access`: `fence` (the default for a run's session) resumes under the run's own
fence, taken over (`launcher.Fence.TakenOver`, `agent.Access.TakenOver`): the same tools, rules,
hook, and sandbox, in `manual` rather than `dontAsk` for a fixed preset, so anything the fence
doesn't allow asks the user (`fenced: <preset>; asks you about the rest`); `profile` resumes with
the profile's own access, under the trust rule. A fence whose settings are gone from the run's
folder is `ErrNoFence`. Background runs pass `--system-prompt-snapshot off`, so a resume replays no
unattended note, and the resumed session is a terminal session like any (`ResumedFrom` the run's),
which the launcher's running hold and the engine's busy check follow from there.

## The end

A run is judged by the agent's `result` line, never its exit status (a Control-C ends with
`error_during_execution` and exit 0; `judge`, `outcome.go`): its time limit, the quit (interrupted,
so its match comes back under Needs you), the user's Stop, and a token budget reached say so first;
then `error_max_budget_usd` is out of budget, a result in error after a `rate_limit_event`
`rejected` is out of usage (with the limit's type and its reset), any other error is the agent's own
words, and success is *Finished:* with the report's summary (`structured_output`, `ReportSchema`) or
the agent's last message. The cost and the models are the result's (`total_cost_usd`, `modelUsage`);
a run on the user's plan (`apiKeySource: none`) has an estimate (`Result.Estimated`,
`autopilot_runs.cost_estimated`). The run's sandbox check is read from its folder (`fence.Sandboxed`
over its policy, `Outcome.Sandbox`): a sandbox that leaked fails a run that ended on its own,
whatever its result. A result line ends the run whatever its program does next: the runner stops the
group (`Callbacks.Over`), and after a SIGKILL ends the processes still working in the worktree
(`ProcSweeper`). At the end it reads the worktree's commit and the files its commits changed
(`gitread.Head`, `ChangedFiles`, `git diff --name-only --no-renames`), which core notes on the item
at the head the run started from and its own last commit (`inbox.Store.NoteAgentChanges`), so a
launch at either head holds for an agent file it wrote. The record holds the outcome; the runner
tells `Engine.RunEnded` only once it does, and again from the record after a restart (`Verify`, the
kind's `Verifier`), so an end is never lost or told twice. Each end done or failed is told once as
**Agent finished** (`NoticeFinished`, through `EngineConfig.Finished`), with its cost.

## Stopping and the quit

`Stop` and `StopAll` reach the run engine at once (`scriptrun.Engine.StopKind`, the stop ladder).
The quit stops every run in the app unless the user lets the agents finish: the quit dialog offers
**Let them finish** while every agent's run going may outlive the quit
(`scriptrun.Run.OutlivesQuit`, a spec's `OutlivesQuit`: an agent that keeps to a dollar budget of
its own), and `scriptrun.Service.LetThemFinish` sets `Engine.LetFinish` and quits; `QuitAnyway` and
`KeepRunning` clear it. `StopAll` then lets such runs go: each keeps its record as going, and the
next start follows it again as an orphan, stopping it at once when its time limit is past. A
confirmed restart to update lets them go too (`update.Runs.LetFinish`, set just before the quit is
let through and cleared again when the restart fails after it), and its question says which carry on
(`update.Run.Outlives`). With **Keep the Mac awake while agents work** on, `caffeinate -i -w <pid>`
holds the Mac awake for each run's program.

## Seams

The outcome keeps the limit's type and reset (`Outcome.Limit`), which the profile fallback rests a
profile by. Another agent claims Take over with its spec's `Resume` capability, its headless resume
in `agent.Args`, and a fence it keeps in an interactive session (`fenceKeeper`, `Fence.TakenOver`);
the run view reads it through its stream parser (`parserFor`), which every page and the memory
share. Codex has all of them ([Codex in the background](codex-in-the-background.md)).
