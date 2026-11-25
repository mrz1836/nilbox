# internal/notify

[Docs](../README.md) › [Architecture](../README.md#architecture) · Code: [`internal/notify`](../../internal/notify) · Guide: [Menu bar and notifications](../guide/menu-bar.md), [The Notifications page](../guide/notifications-page.md)

`internal/notify/` is the app's own notifications: the history of what it told the user (the
`notifications` table, which it alone owns), the `Dispatcher` that records each one and shows its
native banner, the agent sessions' banners, and the bound `Service` the bell and the Notifications
page read. Which kinds show a banner is [`internal/notifyprefs`](notifyprefs.md), a leaf the
settings write through, so settings never imports this package (the repoguard boundary test
forbids it), nor through it the inbox. Banners are posted by the desktop shell's `Poster`, and
nothing leaves the Mac: the history is a local table, and a banner goes to the system's
notification center. What the user sees is [Menu bar and notifications](../guide/menu-bar.md) and
[The Notifications page](../guide/notifications-page.md); Autopilot's side is in
[Notices](autopilot/notices.md#the-notifications-side-internalnotify-internalnotifyprefs).

## The kinds

The kinds are `notifyprefs.Kind` constants, which this package names again: the inbox's three
kinds of arrival (`KindReviewRequested`, `KindAssigned`, and `KindWoke`, the strings of
`inbox.ArrivalReviewRequested`, `ArrivalAssigned`, and `ArrivalWoke`), the kinds about no item
(`KindReleaseDone`, `KindReleasePublished`, `KindReleaseFailed`, `KindRunFailed`,
`KindServiceStatus`, `KindBudgetDraining`, and `KindUpdateReady`), Autopilot's five
(`KindAutopilotAlert`, `KindAutopilotNeedsYou`, `KindAutopilotFinished`, `KindAutopilotChecks`,
and `KindAutopilotProblem`), and, in `sessions.go`, the sessions' two (`KindSessionNeedsYou` and
`KindSessionFinished`), which the history never keeps. `TestKindsAreTheInboxArrivals` holds them
to `notifyprefs.Kinds`, in its order. Every kind but the sessions' has a banner title
(`kindTitles`: *Review requested*, *Assigned to you*, *Back from snooze*, *Released*,
*Published*, *Release workflow failed*, *Run failed*, *Service status*, *API budget draining
fast*, *New version*, *Autopilot alert*, *Needs your OK*, *Agent finished*, *Checks after a fix*,
and *Autopilot problem*) and a summary noun, singular and plural (`summaryNouns`), which
`TestEveryKindHasATitleAndASummaryNoun` checks, the two tables holding no other kind.

## The history (`notify.go`, migration `0101_inbox`)

`Store` (`NewStore`, over the pool) reads and writes `notifications` through the sqlc queries in
`internal/notify/internal/queries/notifications.sql`. A row holds its `kind` (never `''`),
`created_at`, `read_at` (NULL while unread), `item_id` (a reference to `items`, `ON DELETE SET
NULL`), `item_kind` (`''`, `pr`, or `issue`), `repo_owner` and `repo_name`, `number`, `title`,
`url`, `author`, `detail`, a failed run's `run_id` and `run_started` (no reference: the run
history forgets its oldest runs, and hands an id out again once cleared), `heading`, the Autopilot
target (`autopilot_routine`, `autopilot_match`, `autopilot_run`, and `autopilot_created`), and
`once_key`. The repository, number, title, and URL are copies, so an entry still reads once its
item has left the inbox (`TestAnEntryOutlivesItsItem`). Times come from the injected
`clock.Clock`, stored in UTC and read back as RFC 3339 (`clock.Format`).

Two calls record:

- **`Record(ctx, now, arrivals)`** stores one row per `inbox.Arrival`, in order, its `detail`
  from `ArrivalDetail`, and trims the log, in one transaction, and returns what it stored with the
  ids. An arrival whose item isn't stored fails the foreign key, and none of them is stored
  (`TestRecordIsAllOrNothing`).
- **`RecordEvent(ctx, now, entry)`** stores one `Entry`, a notification recorded on its own (one
  about no item, or one of Autopilot's, which may name an item), and trims the log, in one
  transaction. A kind that isn't one of `notifyprefs.Kinds` (`notifyprefs.ErrUnknownKind`), or an
  item kind that is neither a pull request nor an issue (`ErrUnknownItemKind`), is refused before
  anything is written. An `ItemID` no longer stored fails the foreign key, and the entry is
  recorded again without it, its kind and number kept for its line; a negative `Number` is stored
  as 0. An entry whose `Key` the log holds already breaks the partial unique index
  `notifications_once_key` (`WHERE once_key <> ''`) and is refused with `ErrAlreadyTold`; an entry
  with no key is never refused.

Each write ends with `TrimNotifications`, which deletes every row but the newest `Keep` (1,000),
by `created_at` and then `id`, so of entries recorded at the same time the last recorded stay
(`TestTheLogKeepsTheLastRecordedOfTheSameTime`); a trim that fails rolls back the rows it
followed. `Log` reads at most `Keep` rows, newest first (`ListNotifications`), into a `Log`:
`Items`, never nil, and `Unread`. A kind this build doesn't know, as another build may have
written, is left out, and not counted. `MarkAllRead` stamps `read_at` on the unread alone;
`MarkRead` marks read those of the ids given that are unread, and `MarkUnread` unread those that
are read, each returning how many it changed (an empty list does nothing); `Clear` deletes every
row. The table also has an index on `created_at` (`notifications_created_at`).

`KeptItems` lists each `item_id` the history names, once (`ListNotifiedItems`). Core hands a
`notify.NewStore` to the inbox among its `inbox.Keepers` (`itemKeepers`,
`internal/core/adapters_inbox.go`), so the prune of old items keeps an item while an entry names
it, and the entry still opens its sheet ([State, arrivals, and the
prune](inbox.md#state-arrivals-and-the-prune)).

`Notification` is a row as the window reads it: `ID`, `Kind`, `CreatedAt`, `Read`, `ItemID` (0 once
the item is gone, or for none), `ItemKind` (`""` for an entry about no item, whose `Number` and
`Author` are then 0 and `""`), `Repo` (`owner/name`, or `""`, never a lone slash), `Number`,
`Title`, `URL`, `Author`, `Detail`, `RunID`, `RunStarted`, `Heading`, and `Autopilot`, an
`event.AutopilotTarget`. The window opens a run only while the run with `RunID` started at
`RunStarted`, and an Autopilot target only while the one with its ID was created at its `CreatedAt`,
since clearing the history (the runs', or Autopilot's **Clear history**) hands ids out again. The
entry's own choice of banner (`Entry.Banner`) rides along unexported from `RecordEvent` to the
banners, and the row doesn't keep it.

## The Dispatcher (`service.go`)

`NewDispatcher(Config)` takes the `Pool` (required: the history, and the prefs it reads the
banner switches from, through a `prefs.Store` of its own), a `Poster` (nil shows no banner, and
the history is kept all the same), `Events`, `Clock`, and `Logger`, the last three filled in by
`event.Or`, `clock.Or`, and `logging.Or`. It is the inbox's `inbox.ArrivalSink` and is never bound
to the window. A call that records something emits `event.NotificationsUpdated`
(`notifications:updated`), and the window reads the history again.

- **`Arrived(ctx, arrivals)`** runs on the refresh's goroutine, once the refresh that found them
  is stored. It records them (`Record`), tells the window, and shows their banners. A failure is
  logged and goes no further: the refresh is stored already.
- **`Tell(ctx, entry, gather)`** records the entry (`RecordEvent`), tells the window, then shows
  its banner at once or, with `gather`, holds it (below). A repeat (`ErrAlreadyTold`) does nothing,
  banner included, and returns nil; any other failure is returned unlogged, with nothing held, for
  a caller that tells again what wasn't told, as Autopilot does with a problem.
- **`Notify(ctx, entry)`** is `Tell` without gather and **`Gather(ctx, entry)`** with it; each logs
  a failure (`could not record the notification`) in place of returning it.

Every banner goes the same way (`post`): none unless the `Poster` reports `StatusOn`; then the
switches are read (`notifyprefs.DecodeBannersOff` over `notify.bannersOff` and
`notify.bannersOn`), `Banners` picks what to show, and each is posted in turn, a post that fails
logged and the rest still posted. The switches are read within the caller's context, and a read
that fails is logged; once they are read, every banner of `Arrived` or `Notify` is posted whatever
becomes of that context (`TestOnlyTheQuitCutsShortTheBannersShowing`).

## Gathering (`gather.go`)

`Gather`, and `Tell` with gather, hold a banner in the Dispatcher's `held`, a `gathering`, for
`GatherWindow` (2 seconds) from the first banner it held, then show every banner held meanwhile as
one batch through `Banners`, so a burst (Install all's failed runs, the runs a restart settles, the
notices a cycle of Autopilot hands over) sums up as one refresh's arrivals do; the entries
themselves are recorded, and the window told, at once. The 2 seconds don't stretch: a banner held
after a batch showed begins the next one (`TestAGatheringStartsAnotherBatchOnceItShowed`). A batch
shows on its timer's goroutine within the gathering's own lifetime, never a caller's context.
`Close`, which `core.Core.Close` calls before it closes the database, drops what is held, holds
nothing after (an entry recorded later is kept, with no banner), ends that lifetime so a batch
already showing posts no more, and returns once that batch has ended, so nothing reads the database
or posts a banner after it; a read the close cut short isn't logged as a failure
(`TestTheQuitCutsShortABatchShowing`). The package's `TestMain` fails a run that leaves a goroutine
behind (`goleak.VerifyTestMain`).

## Banners (`banners.go`)

`Banner` is one native notification: `ID` (posting the same ID again replaces it), `Title`,
`Subtitle`, `Body`, `Thread` (which groups a kind's banners in the system's list), and what a
click carries back: `ItemID`, `URL`, `RunID`, `RunStarted`, `Autopilot`, and `NotificationID`, the
entry a click marks read, or 0 for a banner that stands for none or many (a summary, a session's,
the test banner). The window marks it only while the entry with that id is about the banner's
item, page, or run, since the log hands an id out again once it is cleared.

`Banners(rows, off)` drops each row whose own choice is false, and each row with no choice (every
arrival, and every entry but an Autopilot routine's) whose kind is in `off`; a choice of true
shows one whatever its kind's switch says. Of what is left, up to `MaxBanners` (3) show one each,
and more show as a single summary.

- **One entry's banner** (`banner`): its `Heading`, cut to 160 runes (`maxTitleRunes`), or else
  its kind's title, as the title; `Repo` and `#number` as the subtitle, or the repository alone for
  an entry about no item, so a problem about no item has no `#number` line; and its `Title`, cut to
  160 runes, as the body, with its `Detail`, capitalized, on a line under it. Its `ID` is
  `brand.Slug` and the entry's id, and its `Thread` its kind.
- **The summary** (`summary`): *N new from Autopilot* when every row is Autopilot's, *N new in your
  inbox* when one is about an item, and otherwise *N new notifications*, as for a burst of failed
  runs; its body counts each kind, in the order of `notifyprefs.Kinds` (*3 review requests, 2
  assignments*). It names no item, run, or entry, so a click on it only brings the window forward,
  and marks nothing.

`ArrivalDetail` is an arrival's `detail`: `PushedDetail` (*Pushed since you requested changes*)
for a pull request its author pushed to since the user requested changes (`Arrival.Pushed`), and
otherwise `WakeDetail` of what ended a snooze early, most decisive first, as the card says it: the
review (*changes requested*, *approved*, or *new review*), the commits (*2 new commits*) or else
*force-pushed*, the comments (*3 new comments*), and *marked ready for review*. `TestBanner` is the
sample `Service.SendTest` shows (*Notifications are on*).

## The Poster and a click

`Poster` is `Post(Banner)`, which never waits on the user; `Status()`, which reports `StatusOn`,
`StatusOff` (the user hasn't allowed the app's banners, or turned them off in the system's
settings), or `StatusUnavailable` (the build can't show them); and `Authorize()`, which asks the
user the first time and may wait for the answer. The shell's is `banners` in `cmd/nilbox`. On
macOS (`banners_darwin_desktop.go`) it is the Wails notifications service, which needs the app
bundle, so `newBanners` returns nil in a bare binary (`magex app:dev`), logged, and core gets no
`Poster` (`banners.poster()` gives a nil interface, never a nil pointer); it must run before the
app finishes launching, so a click on a banner that launched the app still arrives. `Post` checks
the authorization first and returns `ErrBannersOff` without it. Off macOS
(`banners_other_desktop.go`) there is none yet.

A banner carries its click data as `desktop.BannerData`. A click on the banner itself (the default
action) comes back through `desktop.BannerClicked` as an `event.RevealItem` with the same fields,
which the shell turns into the window coming forward and, when it names anything
(`RevealItem.Named`), the window's own reveal ([`internal/event` and
`internal/desktop`](event-and-desktop.md)). The shell asks `Authorize` once a run at launch, or,
while the first-run setup shows, once it ends (`Shell.askForBannersOnce`), never in a startup
measurement.

## The Service (`service.go`)

`Service` (`NewService`, over the same `Config`) is what the window binds, through
`frontend/src/platform/`: `List` (the `Log`), `MarkAllRead` (**Mark all done**), `MarkRead`,
`MarkUnread`, and `Clear` (**Clear history**), `MarkAllRead` and `Clear` emitting
`event.NotificationsUpdated` after every call that succeeds, and `MarkRead` and `MarkUnread` only
when one changed; `BannerStatus`, which is `StatusUnavailable` with no `Poster`; and `SendTest`,
which refuses with `ErrBannersUnavailable` when the build can't show banners, asks `Authorize`,
refuses with `ErrBannersOff` when the answer is no, and posts `TestBanner`. Opening the history
marks nothing: an entry is marked read when the user opens it, clicks its banner, opens its item's
sheet, or marks it done, and unread again only by the user (**Mark unread**, which puts it back on
the bell; [Read without asking](../guide/menu-bar.md#read-without-asking)).

## The session banners (`sessions.go`)

`SessionBanners` (`NewSessionBanners`) is the sessions `Watcher`'s `AttentionSink`
(`sessions.WatcherConfig.Attention`), which core builds with no `Events`: `NeedsYou` and `Finished`
post a banner while banners are allowed and that kind is on (both start off,
`notifyprefs.Kind.DefaultOn`), and record nothing, since the session's card already says what it is
doing. `SessionBanner` titles it with the agent's name and what it does (*Claude needs approval*, or
*Claude finished its turn*), gives `sessions.Session.Label` as its subtitle, and as its body what
the agent last said, or else the session's title, on one line and cut to 160 runes. Its `ID` is
`brand.Slug`, `-session-`, and the session's ID, so a later banner for the session replaces the
earlier, its `Thread` the kind, and a click shows the item, or, for a repository's launch's session,
opens the repository's page.

## Who sends to it

Core builds one `Dispatcher` (`internal/core/core.go`) and hands it to each source through an
adapter, so none imports this package (the repoguard boundary test keeps settings, the sessions,
svcstatus, update, scriptrun, and Autopilot, among others, off it):

- **The inbox** (`inbox.Config.Arrivals`): `Arrived` after each whole refresh with the review
  requests, assignments, and snoozes ended early that it found (`inbox.Arrival`: none on an
  account's first refresh, for an item dismissed or still snoozed, or for a muted repository or
  org; [`internal/inbox`](inbox.md)).
- **A release** (`releaseNotices`, `internal/core/adapters_release.go`, the release `Notifier`):
  `Notify` with `release_done`, `release_published`, or `release_failed`, its title, body, page,
  and repository ([`internal/release`](release.md)).
- **A failed run** (`runFailures`, `adapters_runs.go`, `scriptrun.Config.Failed`): `Gather` with
  `run_failed`, the run's title, body, and repository, its `RunID`, and its start as `RunStarted`
  ([`internal/scriptrun`](scriptrun.md)).
- **A service status change** (`statusAlerts`, `adapters_status.go`, the svcstatus `Notifier`):
  `Notify` with `service_status`, its title, body, and the incident's or status page's URL
  ([`internal/svcstatus`](svcstatus.md)).
- **An API budget draining fast** (`budgetAlerts`, `adapters_budget.go`, the budget `Notifier`,
  which the budget's `Warner` calls once an hour at most for each budget): `Notify` with
  `budget_draining`, its sentence and body, no URL, and a `Key` of the budget and its window, so
  the same window is told once; a click brings the window forward, which opens the API budget's
  panel ([`internal/budget` and `internal/github`](budget-and-github.md#a-budget-draining-fast)).
- **A new version** (`updateNotices`, `adapters_update.go`, the update `Notifier`): `Notify` with
  `update_ready`, the app's name and version with *is ready* (*Restart to update when you're
  ready.*) or *is available* (*Update when you're ready.*), and no URL, so a click only brings the
  window forward, where the update's banner shows ([`internal/update`](update.md)).
- **Autopilot** (`autopilotNotices`, `adapters_notices.go`, the engine's `NoticeSink` for Notify me
  and Tell me, the problems, Needs your OK, the agents finished, and the checks after a fix):
  `Tell`, gathering a notice marked `Burst`, its error returned to the engine, with the kind
  `noticeKinds` maps it to, the routine's name as an alert's `Heading`, the notice's `Banner` and
  `Key`, its item, and its `event.AutopilotTarget` ([Notices](autopilot/notices.md)).
- **The agent sessions** go to `SessionBanners` instead, never the Dispatcher.
