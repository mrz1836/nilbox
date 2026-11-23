# Notices, the menu bar, and the window's close

[Docs](../../README.md) › [Architecture](../../README.md#architecture) › [`internal/autopilot`](README.md) · Code: [`internal/autopilot`](../../../internal/autopilot), [`internal/notify`](../../../internal/notify), [`internal/menubar`](../../../internal/menubar) · Guide: [Autopilot's notifications and the menu bar](../../guide/autopilot/notifications-and-menu-bar.md)

## The notices (`notices.go`)

The notices dispatcher (`NewNotices`, `catalog.DispatchNotices`) runs Notify me and Tell me as local
steps: each becomes a `Notice` handed to a `NoticeSink`, which core adapts over the notifications
(`internal/core/adapters_notices.go`, `autopilotNotices`), so the engine never imports notify.
Notify me tells why the match matched (the trigger's words and each condition that held, with what
it compared, on one line, at most 280 characters) and the user's own line; Tell me says it reached
the step and how each step before it ended, an outcome named as Only if names it (the richer words
of what happened come with the steps that make outcomes). Either is an alert (`NoticeAlert`) about
the match's item, headed by the routine's name, that opens the match's card, with when the match was
made. Its banner is the step's own choice (`NotifyStep.Banner`), else the routine's
(`spec.Notices.Alert`), else the settings'. A notice that can't be told fails its step, and the
match says a step failed. Each notice is told once whatever happens to the engine: its `Key` names
it (a step's by its match, when the match was made, the step, and the try; a problem's by the
problem and when it began, a start the engine saves before it tells it), and the notifications keep
one entry per key, so a step that runs again because a quit came before the loop heard it was told,
or a problem told again after a crash, adds no second entry or banner. A cycle hands its notices
over together as it ends (`pass.tellNotices`), each on a worker: with more than one, or when any
comes from a routine catching up, each is a `Burst`, which core gathers (`notify.Dispatcher.Tell`
with gather, `GatherWindow`), so more than three at once show as one banner (*4 new from Autopilot*,
*4 routines need you*); a lone one shows at once. A Dry run tells nothing. Problems go through
`NewProblemNotices` over the same sink: a `NoticeProblem` about no item, its line the routine's name
("A routine" when it can't be read) and its body the problem, opening the routine, with when the
routine was made; several untold at once are a burst. The other kinds (`NoticeNeedsYou`,
`NoticeFinished`, `NoticeChecks`) are declared for the parts that make their moments. Core maps each
kind to the notifications' own (`noticeKinds`; a test holds the map to
`notifyprefs.AutopilotKinds`).

## The notifications' side (`internal/notify`, `internal/notifyprefs`)

The five kinds, `autopilot_alert`, `autopilot_needs_you`, `autopilot_finished`, `autopilot_checks`,
and `autopilot_problem`, are all on by default (`notifyprefs.AutopilotKinds`, `Kind.Autopilot`),
titled *Autopilot alert* (an alert's banner is headed by its routine, `Entry.Heading`), *Needs your
OK*, *Agent finished*, *Checks after a fix*, and *Autopilot problem*. `notifications` (migration
`0101_inbox`) has a `heading` and the Autopilot target (`autopilot_routine`, `autopilot_match`,
`autopilot_run`, `autopilot_created`, RFC 3339 of the most specific one's creation, since Clear
history hands IDs out again), and a `once_key` under a partial unique index. An `Entry` now names
its item too (`ItemID`, `ItemKind`, `Number`; an item no longer stored is recorded without its ID,
as the log forgets an item that goes), so an Autopilot entry about an item is kept by the inbox's
prune (`KeptItems`), opens its item, and is marked read when that item's sheet opens, as arrivals
are. `Entry.Banner` is an entry's own choice of banner over its kind's switch (the user's allowing
banners holds either way). `Entry.Key` (the `once_key` column, unique while set) keeps an entry once
while the log holds it: a repeat is refused as told (`ErrAlreadyTold` from `RecordEvent`), banner
and all, and an entry with no key is never refused. `Dispatcher.Tell(ctx, entry, gather)` records
and shows (now, or gathered) and returns the error recording failed with, nil for a repeat; `Notify`
and `Gather` are it, logged. The banner (`Banner.Autopilot`) and the click (`desktop.BannerData`,
`BannerClicked`, `event.RevealItem.Autopilot`, an `event.AutopilotTarget`, whose `Activity` asks for
Activity rather than one match, as the menu bar's row of what needs the user does) carry the target.
A summary of Autopilot's alone is titled *N new from Autopilot*.

## At a glance (`glance.go`)

The bound `Glance` reads the database alone: whether Autopilot (beta) is on (`On`), the routines not
deleted and those the engine runs (on, of the account signed in, and loading; none while no one is
signed in, the engine's last cycle found the sign-in gone, or Autopilot is off), Pause Autopilot,
the open matches waiting for the user (`StateWaiting`), and the agent runs going (starting, running,
or stopping, in the background or the terminal, never a GitHub action's), oldest first.
`KeepsRunning` is a routine the engine runs or an agent working, so after a switch to another
account, whose routines don't run, or with Autopilot off, a close quits again as before, but while
an agent it started before still works. The window reads it as it opens and again on each
`autopilot:updated` (`useAutopilot`): the Autopilot tab counts its agents working and marks its
matches waiting with an amber dot. While Autopilot (beta) is on, the window names Autopilot whether
or not a routine exists: Settings → Notifications lists the five switches, and the Notifications
page offers the Autopilot type; off, neither does.

## The menu bar (`internal/menubar/autopilot.go`, pure)

`Summarize(snap, waiting, autopilot, now)` works out `Summary.Autopilot` from an `AutopilotState`
(core's `ShellAutopilot` adapts the glance, and hands an empty one while Autopilot (beta) is off,
`shellAutopilotState`): shown only while a routine exists, the first `PerSection` agent runs
("example-repo #88 · Claude · 6m", each opening its run) and how many more, the matches waiting ("1
needs your OK", opening Activity), and whether Pause Autopilot is ticked; `NextChange` is when a
run's minutes next change. The count beside the icon stays the inbox's. The tray
(`cmd/nilbox/tray_desktop.go`) draws it after the lists, under its heading, with the app's first
checkbox row (`AddCheckbox`), and every click goes through `Shell.AutopilotAction`: a run or
Activity reveals its target, and Pause flips what the menu showed through
`desktop.Autopilot.SetPaused`, core's call to the settings' own `SetAutopilotPaused`, the one writer
of `autopilot.paused`, then reads Autopilot again and draws the menu in full, so a write that failed
takes back the tick the menu put on the click itself.

## The shell (`internal/desktop/autopilot.go`)

The shell reads Autopilot's state at its start and on every `autopilot:updated`, in its one redraw
loop, whether or not the icon shows. Core's relay sends one on each `AutopilotChanged`, so a Pause
made in Settings reaches the tick of the copy it was made in at once, whether or not that copy holds
the engine; a Pause made in another copy reaches a copy that doesn't hold the engine on its next
engine tick (`followElsewhere`, within a minute), and the copy that holds it on its next cycle. It
keeps what it last read of keeping the app running and of Pause. `HidesOnClose` answers at once from
that alone: yes while the icon shows, and yes while Autopilot has a routine on or an agent working
(no routine counts while Autopilot (beta) is off, `Glance.KeepsRunning`), whatever the icon setting,
since the close would otherwise quit the app and quietly stop them; the Dock icon brings the window
back (`ApplicationShouldHandleReopen`). The window's close hook asks `Shell.WindowClosing`, whose
pure rule is `CloseOf` (`internal/desktop/close.go`): the close goes through while the shell is
stopping (the runtime's own shutdown closes the window once the services have stopped, so a quit's
own close never quits again), the window only hides while `HidesOnClose` says so, and otherwise the
window stays and the shell quits the normal way (`Quit`), so `ShouldQuit` asks about runs going, or
holds the quit for a move, in a window that still exists. The app never quits on the last window's
close by itself: `ApplicationShouldTerminateAfterLastWindowClosed` stays off (`macAppOptions`),
since AppKit asks it as the last visible window hides too, and with it on a close that only hid the
window ended the app a moment later. The window comes back with nothing said about the hide (an
earlier build's `ui.keptRunningTold` is left stored, and nothing reads it). A click on an Autopilot
notification or banner about an item opens that item, as an inbox arrival's does (`bannerOpens`
falls back to the item, `opensAutopilot` is an entry about no item alone), and one about no item, as
a problem is, or on a menu row of the group, opens the Autopilot tab at what its target names ([The
Autopilot tab](autopilot-tab.md)).
