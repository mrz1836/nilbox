# internal/event and internal/desktop

[Docs](../README.md) › [Architecture](../README.md#architecture) · Code: [`internal/event`](../../internal/event), [`internal/desktop`](../../internal/desktop) · Guide: [The window](../guide/window.md), [Menu bar and notifications](../guide/menu-bar.md), [The Notifications page](../guide/notifications-page.md)

`internal/event/` holds the app's events, one Go type each with its name; services emit them into
the one `event.Sink` they are given. `internal/desktop/` is the shell's logic: it hears those
events, keeps the menu bar current with what `internal/menubar` works out (its Autopilot group,
while Autopilot (beta) is on and a routine exists, with Pause Autopilot as the tray's one checkbox
row, `Shell.AutopilotAction`), decides the window's close (`Shell.WindowClosing`, the pure
`CloseOf`): it goes through while the shell stops, hides the window while the icon shows or while
Autopilot has a routine on (none counts while Autopilot (beta) is off) or an agent working
(`HidesOnClose`, from Autopilot's state as the shell last read it), and otherwise keeps the window
and quits the normal way, so `ShouldQuit` asks in it while runs go (the last window's close never
quits by itself: `ApplicationShouldTerminateAfterLastWindowClosed` stays off, since AppKit asks it
as the window hides too), runs the routines' watch passes (`inbox.WatchPasses`, started before the
scheduler, without which no refresh reads the routines' searches), the scheduler, the checks
follower (`inbox.ChecksFollower`: the listed pull requests whose checks were running, but a muted
repository's, and those a routine's search follows, 50 a round at most, the listed first, read 25 to
a request side by side through `(*github.Client).FetchItems`, and nothing while none runs; a pull
request whose workflow runs GitHub holds for approval, `item_details.checks_awaiting` and
`checks_awaiting_on_github`, which its card marks held in place of the rollup's state, isn't
running, and waits for the whole refresh), the merge follower (`inbox.MergeFollower`: a few seconds
after each merge the inbox learns of, its own through the Actor or one a read finds, the listed
issues the merged pull request closes, read again every five seconds while GitHub still has one
open, and the repository's other open pull requests on the branch merged into, read again as the
checks follower reads its own, and again while GitHub answers `UNKNOWN`, three rounds each at most,
and nothing while no merge happened), the Release page's checks follower (`release.ChecksFollower`,
in [internal/release](release.md)), the sweeper (these four as often as Settings → Refreshing says,
each through its own `periodic.Job`, which runs `periodic.RunEvery` and which core wakes on the
Go-only `event.IntervalsChanged` the settings setter emits, so a change applies at once), the GitHub
notifications' `ghthreads.Syncer` (woken the same way), the budget's `Saver` and `Warner`, the
sessions watcher, the inbox's `Rereads` (one item at a time, one GraphQL point each: the item the
sessions watcher hands it, as an agent session stops working or its agent finishes a command that
changes GitHub, and a pull request whose branch a push on this Mac moved, from the push watch), the
Release page's refresher, and the Hooks and Deps pages' scans (`hooks.Scans`, `manifests.Scans`) as
tracked work, starts the deferred work (`core.Deferred`: the login-shell read first; the tool
finder, the runs,
the release `Follower`, the service status `Checker`, the update worker (`update.Worker`, in
[internal/update](update.md)), Autopilot's engine (`autopilot.Engine`), the
[push watch](pushwatch.md), woken by `event.IntervalsChanged` too, and Describe it's worker
(`describeit.Worker`, in [internal/describeit](describeit.md)), beside it) once after the first
paint, never on the cold start, and shuts down in order (timers, then the work, then the database).
It declares small interfaces that the Wails glue in `cmd/nilbox` implements, and imports no Wails. A
window event is registered in `main_desktop.go` with `application.RegisterEvent`, which generates
its TypeScript type and name in `frontend/bindings`; a repository test checks that every window
event is registered.

A window that never reports its first paint still gets the deferred work: `Shell.Launched`, which
the desktop runtime calls once the app has launched, arms a timer (`armDeferred`) that starts it
`desktop.DeferredFallback` (10 seconds) later, and the first paint, when it comes, starts the work
and stops the timer. Either way the deferred work starts once (`startDeferred`), and never in a
startup measurement, whose `Launched` arms no timer, nor once the shell is stopping.
