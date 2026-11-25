# internal/menubar

[Docs](../README.md) › [Architecture](../README.md#architecture) · Code: [`internal/menubar`](../../internal/menubar) · Guide: [Autopilot's notifications and the menu bar](../guide/autopilot/notifications-and-menu-bar.md), [Menu bar and notifications](../guide/menu-bar.md)

`internal/menubar/` is what decides the menu bar icon's words: it works out the count beside it
and every row of its menu, from the stored inbox, the agent sessions waiting for the user,
Autopilot's glance, and the app's update. It is pure: it keeps no state, reads nothing itself,
and imports no Wails; of the app it imports only `agent`, `brand`, `inbox`, `sessions`,
`textutil`, and `update`, for the values it is handed (`inbox.Snapshot`, `sessions.Session`,
`update.Status`) and their helpers, so it builds and is tested on any OS. The desktop shell
([`internal/event` and `internal/desktop`](event-and-desktop.md)) decides when to draw and calls
it, and `cmd/nilbox`'s `tray` (`tray_desktop.go`) draws the result with native menu items. What
the user sees is [The menu bar icon](../guide/menu-bar.md#the-menu-bar-icon).

## Summarize (`menubar.go`)

`Summarize(snap, waiting, autopilot, now)` returns a `Summary`, its times in `now`'s location:

- **The count.** `Count` is `snap.NeedsYou(now)`: the items in either list that aren't muted with
  their repository or org, dismissed, or snoozed, each once, which is the Inbox tab's count, so
  the menu bar and the window agree. `Label`, beside the icon, is the count, `99+` past `MaxCount`
  (99), or `""` at zero. Autopilot adds nothing to it.
- **The lists.** `Sections` are the lists in the inbox's order (`snap.Lists`: Review requested,
  then Yours, named from `sectionTitles`), each with its items that need the user in the list's
  order (`List.Waiting`), the first `PerSection` (5) as its `Entries` and the rest counted in
  `More`; a list with none is left out. No item is in both lists, so each is listed once, as the
  count counts it. An `Entry` is the repository's short name, the number, and the title with its
  whitespace collapsed and cut to 48 runes (`maxTitleRunes`): "example-repo #123 · Fix the login
  redirect"; its `Tooltip` has the whole name and title ("acme-org/example-repo #123: Fix the
  login redirect"). `MoreLabel` words what is left out ("3 more…").
- **The sessions waiting.** `Waiting` is `WaitingEntries(waiting)`, in the order given (the shell's
  `Sessions.Waiting`: the longest waiting first), cut to `PerSection` with `WaitingMore` for the
  rest, shown under `WaitingTitle` (*Waiting for you*). A row is the session's
  `sessions.Session.Label`, then the agent's name (`agent.Lookup`, else its id) and
  `State.Phrase`: "example-repo #12 · Claude needs approval", or, for a repository's launch's
  session, whose label holds its title, "example-repo · Upgrade dependencies · Claude needs
  approval". Its tooltip uses `FullLabel` and adds an item's title. A row keeps `SessionID`,
  `ItemID`, and `URL` for its click.
- **Nothing.** `Nothing` is the line in place of the lists when no item and no session needs the
  user: *Nothing needs you*, or *The inbox has not loaded yet* while `snap.LastRefreshedAt` is
  empty.
- **The status line.** `Status` is *Updated 3:42 PM* after a refresh today, *Updated Mar 4, 3:42
  PM* after an earlier one, *Last refresh failed* while `snap.LastError` is set (`Failed`, with
  `Error` as its tooltip), with *· retrying at 3:43 PM* after it while the inbox plans another try
  (`snap.Retry.At`, see [internal/inbox](inbox.md#a-refresh-github-failed)), or *Not refreshed
  yet* when `LastRefreshedAt` isn't RFC 3339.
- **The next change.** `NextChange` is `snap.NextSnoozeEnd(now)`: when the soonest running snooze
  of an item that isn't muted ends, and the count goes up with nothing stored; zero for none.
- **Autopilot.** `Autopilot` is `AutopilotGroup(autopilot, now)` (below).

`Summary.Updates` is left empty: the shell fills it in from `UpdateRows`.

## Autopilot's group (`autopilot.go`)

`AutopilotGroup(st, now)` works out the group from an `AutopilotState`: `Routines`, `Paused`,
`NeedsYou`, and the `Working` agent runs, oldest first, each an `AgentRun`. Core's
`ShellAutopilot` adapts it from `autopilot.Glance`, and hands an empty one while Autopilot (beta)
is off (`shellAutopilotState`). With no routine (`Routines` 0) the group is the zero `Autopilot`,
so a user without one never sees Autopilot named; otherwise it is `Shown`, under `AutopilotTitle`
(*Autopilot*), with:

- **The runs.** The first `PerSection` runs as `Working` rows, the rest counted in `WorkingMore`.
  A row is "example-repo #88 · Claude · 6m", and its tooltip the whole name, the title cut to 96
  runes, and "Claude working for 6m"; it keeps `RunID`, `RoutineID`, `MatchID`, and `Created` (the
  run's `CreatedAt` as RFC 3339 in UTC, or `""`), which tells the run from a later one given its
  ID. `Elapsed` words the time: `<1m`, `6m`, `1h`, or `2h 5m`.
- **What waits.** `NeedsYou` is `NeedsYouLabel` of the matches waiting for the user: "1 needs your
  OK", "3 need your OK", or `""` for none, which leaves the row out.
- **Pause.** `Paused` ticks `PauseLabel` (*Pause Autopilot*), the menu's one checkbox row.
- **The next change.** `NextChange` is the soonest moment any run's minutes tick over
  (`nextMinute`), shown or not, so the times are drawn again; a run whose start lies after `now`
  (a clock ahead) ticks a minute after its start. Zero while none runs.

`SameAutopilot` reports whether two groups draw the same rows (`NextChange` aside), so a change that
leaves them as they were redraws nothing. A click on a row of the group is an `AutopilotAction`:
`AutopilotWatch` (a run's row), `AutopilotActivity` (the row of what waits), or `AutopilotPause`;
its *N more…* row (`WorkingMore`) only brings the window forward.

## The update's rows (`updates.go`)

`UpdateRows(st, note, now)` works out the update's part of the menu, `Updates` (its `Rows`, in
order, and `Until`), from the `update.Status` and an `UpdateNote`, what the menu itself last asked
of the update: `At`, when its check ended, `Checked`, and `CheckError`. A build that doesn't update
(`Status.Enabled` false) gets none. An `UpdateRow` has a `Label`, a `Tooltip`, and an
`UpdateAction`; a row with `UpdateNone` is a line that can't be clicked.

- **Check for Updates…** (`UpdateCheck`): *Checking for Updates…*, disabled, while a check runs,
  and disabled while the app restarts into a version (`StateInstalling`, `StateFinishing`).
- **The version found** (`installRow`): *Restart to Update (X)* once it is ready
  (`UpdateRestart`), *Update to X…* once it is found and not downloaded, as Just tell me leaves it
  (`UpdateDownload`), or the disabled *Downloading X…*, *Restarting into X…*, or *Finishing the
  update…*, whose tooltip says to quit the app and open it again if it doesn't quit.
- **An install that failed** (`Status.InstallFailed`): when the last restart didn't install the
  version found, a disabled *X didn't install* with its reason as the tooltip, then *Download X
  Manually…* (`UpdateManual`) when X fails the same way every time (`Retryable` false) and is
  available or ready, or else the version's own row, worded *Try Updating to X Again* when it can
  be clicked. With no version found, *X didn't install* still shows, with *Download X Manually…*
  when it isn't retryable, and Check for Updates… is the way to try again. `ManualURL` is the page
  that row opens: the version found's `ReleaseURL`, or, with none found, `update.ReleaseURL` of
  the one that didn't install.
- **The status line**, last, for `NoteFor` (one minute) after the menu's own check ended: *Check
  failed* (the error as the tooltip), *Up to date (X)* with the running version (*Checked just
  now.*), or *Download failed* when the check found a version whose download then failed. A check
  that found a version adds no line, since its row shows it, and a check running again shows none.
  `Until` is when the line ends, `At` plus `NoteFor`, so the menu is drawn again without it; zero
  when none shows.

## How the shell draws it

The shell (`internal/desktop/tray.go`, `updates.go`, and `autopilot.go`) keeps one redraw loop
(`redrawTray`), which runs one redraw at a time and folds an ask that comes while one is pending
into it:

- **In full** (`askRedraw`) on `event.InboxUpdated`, when the icon is switched on
  (`event.MenuBarChanged`, from the `ui.menuBar` setting, on by default), after Pause is flipped
  from the menu, when the menu's `UpdateNote` is cleared or set (`setUpdateNote`: a click on an
  update's row, a check ending), and when its timer fires.
- **Only what changed** (`askChangedRedraw`, `redrawChanged`) on `event.SessionsUpdated` and
  `event.UpdatesUpdated`: the menu is drawn again only when `WaitingEntries`, the update's rows
  (`sameUpdates`), or Autopilot's group (`SameAutopilot`) read otherwise than when it was last
  drawn, since a session goes through many changes the menu doesn't show, and a download reports
  how far it got a few times a second.
- **Autopilot read again** (`askAutopilot`, `readAutopilot`) on `event.AutopilotUpdated` and at the
  shell's start, whether or not the icon shows, since the window's close asks what it last read;
  then only what changed is drawn.

A draw (`drawTray`, or `redrawChanged` once it sees a change) reads the sessions waiting and the
update's rows (`updateRows`: `UpdateRows` over the update's status and the shell's `UpdateNote`);
`drawTrayWith` then reads the stored inbox (`Inbox.Snapshot`), calls `Summarize`, sets its
`Updates`, hands the `Summary` to the `Tray`, keeps what it drew to compare the next change with,
and sets its one timer (`wakeAt`) for the soonest of `NextChange`, `Updates.Until`, and
`Autopilot.NextChange`, plus `snoozeSlack` (1 second). A hidden icon draws nothing and keeps no
timer; a snapshot that can't be read is logged, and the next change draws in full.

The `tray` draws the menu in this order: *Open* and the app's name; the sessions waiting, each
bringing its terminal tab forward (`Shell.FocusSession`, which shows its item instead when the
terminal can't be); `Nothing` when no list and no session shows; each list under its heading (a
native section heading where macOS has one, `addSectionHeader`), each entry revealing its item
(`event.RevealItem`) and *N more…* bringing the window forward; Autopilot's group; the update's
rows; **Refresh Now**; the status line, disabled, with the error as its tooltip when the refresh
failed; the inspector's row in a build from source (`devKit.menu`); and *Quit* (⌘Q). It sets
`Summary.Label` beside the template icon. Every change runs on the main thread, in the order it
was asked for, and the shell never waits for it.

A click goes back to the shell. A row of Autopilot's group goes through `Shell.AutopilotAction`: a
run reveals its `event.AutopilotTarget`, the row of what waits reveals Activity (`Activity: true`),
and Pause flips what the menu showed through `desktop.Autopilot.SetPaused` (core's call to the
settings' `SetAutopilotPaused`), then reads Autopilot again and draws in full, so a write that
failed takes back the tick the menu put on the click ([The menu
bar](autopilot/notices.md#the-menu-bar-internalmenubarautopilotgo-pure)). An update's row goes
through `Shell.UpdateAction`, which first clears the note: a check runs as tracked work and leaves
its `UpdateNote` (`CheckError` from the status, or the error's `uierr.Message`); Restart to Update,
Update to X…, and Try Updating to X Again bring the window forward and emit
`event.UpdateRestartRequested`, so every restart runs the window's one flow; Download X Manually…
opens `ManualURL` in the browser ([`cmd/nilbox`](cmd/nilbox.md)).
