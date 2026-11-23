# Agents in the terminal, readiness, and Needs your OK

[Docs](../../README.md) › [Architecture](../../README.md#architecture) › [`internal/autopilot`](README.md) · Code: [`internal/autopilot`](../../../internal/autopilot) · Guide: [Agents in your terminal](../../guide/autopilot/agents-in-your-terminal.md), [Needs you](../../guide/autopilot/needs-you.md), [This Mac](../../guide/autopilot/this-mac.md)

## The start path

Every agent start goes through the engine's one path, never another: the gate (`advance_gates.go`),
in Go, cheapest first, then on a worker what holds it back on this Mac, then the fresh read, then
the runner:

1. `gate`: the user's OK, a sign-out, Pause, the active hours, the tries and the day's counts, a
   free slot and the daily cap, the item's own rules (`itemRules`: an outside contributor's item
   is skipped, `skipped: outside_contributor`, unless `EngineConfig.Outside` allows its
   repository, a repository's own choice over its org's, core's `outsideRules` over
   `launchdefaults.Index.OutsideAllowed`; allowed, it still plans only, since nothing a routine
   builds sets `FullAccessOverride`), a fork's branch for a run that pushes, one active run per
   item, and this Mac's readiness (`readyFor`, from the last checks, never waiting on one: a need
   not checked yet waits `queued: starting` and asks for a check; one not met waits `blocked:
   not_ready`).
2. On a worker (`readFresh`): the step's prompt, checked against the library again
   (`promptHold`); then `EngineConfig.Holds` (core's `agentHolds`: the item's own launch
   resolved, its agent's program and its profile checked cheaply, the last sign-in check kept;
   while Settings' **Hold agent starts while their provider reports an outage** is on, the
   agent's `StatusPart` major or critical in `svcstatus.Status`, `queued: provider_outage`; a
   repository with no clone, `waiting: no_clone`; a tool the clone's marker files say it needs
   (`go.mod`, `pnpm-lock.yaml`, `magefiles`, `.mage.yaml`, `Makefile`, `Cargo.toml`,
   `pyproject.toml`, by `os.Lstat` alone) missing from the login shell's PATH, `blocked:
   missing_tool`); and only once nothing held it, the item read fresh (`ReadItem`, one point).
3. The cycle after the read records the run and hands the step to the `Runner`, whose `Result`
   may say it didn't start (`Wait`, `Requeue`): the run ends `not_started` (no run, no try), and
   the match waits on what held it (`heldAt`), an OK given before a hold taken back.

## Agents in the terminal

Core's `agentRuns` (`internal/core/adapters_agents.go`) is the `Runner`: a step `In my terminal` is
the launcher's own `Launch` of `routineRequest`, the request a click would make with the routine's
choices in the launch menu's place (the step's agent, profile, model, and effort, a per-org or
per-repository row of the routine's over them, a repository's over its org's; the prompt by its
starter's key or its nickname; plan mode and the worktree), and `launcher.RoutineLaunch` naming the
routine. `RoutineLaunch` only ever holds a launch back more than a click: its pull request's files
are checked under the unattended scope (`FileChecker.CheckFilesUnattended`, the inbox's
`Routines.CheckFilesUnattended`: every pull request the user didn't open, trusted bots' and members'
included, `sensitive.CheckedUnattended`), a file hold is confirmed only by `RoutineLaunch.Confirmed`
(never the request's own `Confirmed`), a session running in the folder always holds it (`Alongside`
is ignored), a worktree with uncommitted changes holds it (`HoldDirty`, keyed by the worktree, its
head, and the count), `FullAccessOverride` is ignored (`personsChoices`), and its prompt names the
routine (`.Routine`, `.Trigger`, `.Attempt`; `.Unattended` stays false in the terminal). A hold
comes back as the match's wait (`heldResult`: agent files or unchecked files `waiting: hold` with
the hold, a dirty worktree `waiting: dirty_worktree`, a session `queued: busy_worktree`);
`worktrees.ErrHeadMoved` requeues it. A launch that opened ends the run done at once, recording its
session, agent, profile, clone, and worktree (`Launched`): the session is the user's from there,
which the sessions ledger follows as any, and a later start finds it in the folder (the launcher's
running hold). `Alive` answers gone for a terminal run still recorded as going, so one a crash cut
off mid-launch is never launched again on its own (the engine's `alive` takes a terminal run its
runner never started as gone). A step in the background goes to the background runner ([Agents in
the background](agents-in-the-background.md)).

## A start held back on a session

A start its runner held back on something working where it would start (a queued wait,
`busy_worktree`) isn't tried again until a tick has passed (`holdBack`, `backOff`): each try reads
the item from GitHub first, so a session in the worktree would otherwise cost a point every cycle
until it ends. The user's OK tries it again at once.

## The user's OK of a hold

A held step keeps its `Hold` in the match's steps (`stepRun.Hold`), which `Match.Hold` shows the
window while the match waits on it; `Approve` adds exactly that hold's `Key` to the step's
`Confirmed` (`confirmHold`), which the next `Job` carries (`Job.Confirmed`, `RunRequest.Confirmed`)
into `RoutineLaunch.Confirmed`. A routine never confirms a hold on its own, and a key names what the
user saw (the head commit for files, the file count for a dirty worktree), so a push or a new file
since holds it again.

## Readiness (`ready.go`)

`Needs(spec, LaunchDefaults)` is pure: GitHub for every routine; banners for a notice; for an agent
step its place (`place:`), the terminal for a terminal run, each agent and profile it may run (its
own, its per-org rows', and its fallbacks', Inherit as the app's default agent and that agent's
default profile), `gh` and `git`; and what it `Requires`. A `ReadinessChecker` (core's
`readyChecks`) checks each need: GitHub from the inbox's last refresh, or `setup.GitHub` (one point)
on Check again; a program on the login shell's PATH; an agent by its spec's `VersionArgs`
(`agent.ParseVersion`); a profile by its folder and its spec's `SignInArgs` (`agent.ParseSignIn`:
Claude Code's `auth status` JSON, keeping only `loggedIn`, the method, `subscriptionType`, and the
expiry, never the email; Codex's `login status`), run with the profile's `ConfigEnv` and without the
agent's `KeyEnv` unless the user's API key setting is on (then a key in the shell's environment is
ready by itself); the terminal chosen; and the banners (a warning only). An agent's version and a
profile's sign-in are kept ten minutes unless fresh. A routine is ready when each need is ok or a
warning. The Service and the engine share the results (`readiness`): the engine checks on a worker
as it starts, when a routine is saved or switched on (`Service.changed` marks it due), and every ten
minutes; the bound `Readiness` serves them (checking once when none ran) and `CheckReadiness` runs
every check again, fresh, then wakes the engine. A need not met is a problem (`ProblemNotReady`,
keyed by the routine and the need), told once.

## Needs your OK

Every save of a match that comes to wait for the user (`saveIn` → `noteWait`), a Dry run's never, is
told at the cycle's end (`tellWaits`, on a worker) as a `NoticeNeedsYou` through
`EngineConfig.NeedsYou` (core's notices sink), keyed by the match, its creation, the wait, and the
try, so a cycle that sees it again, or a restart, tells nothing twice; read again first, so one no
longer waiting is told nothing; several at once are a burst. Its target is the match, which the
window's Needs you card is. The bound `NeedsYou` lists the open matches that haven't started
(waiting, blocked, queued), real ones alone, newest first, for that section.

**Run a routine** (`RunNow(id, itemID, anyway)`): a manual match, once per click, counting
toward no cap or try, only on an item of a kind it acts on (a click holds whatever the trigger,
so the kinds are checked first); with `anyway`, on such an item its conditions don't match too,
under a manual fire key. Every limit of a start still holds it. Without `anyway`, an item its
conditions don't match is refused with a `RefusalError` (`refusal.go`) whose code is
`RefusalNoMatch`, its words naming the first condition that fails and its chain holding
`ErrNoMatch`; `desktop.MarshalError` sends the window `{"code": "noMatch"}` beside them, as
`update.RefusalError`'s codes go, so the window offers Run anyway from the code, never the
words, and never asks `Explain` for it. Every other refusal, the same `ErrNoMatch` for a match
that is gone and a run, anyway or not, on a kind it doesn't act on among them, carries no code.

**Preview** (`PreviewRun(draft, itemID)`) is `Previewer` (core's `runPreviews`): the launcher's
own `Preview` of the same `routineRequest` a start would launch, with where each setting came
from (`routine` for the routine's own), so what it shows is what runs.
