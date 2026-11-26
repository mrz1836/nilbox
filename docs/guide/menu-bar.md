# Menu bar and notifications

[Docs](../README.md) › [User guide](../README.md#user-guide) · Design: [`internal/menubar`](../architecture/menubar.md), [`internal/notify`](../architecture/notify.md), [`internal/notifyprefs`](../architecture/notifyprefs.md), [`internal/event` and `internal/desktop`](../architecture/event-and-desktop.md)

## The menu bar icon

While the app runs, its mark sits in the menu bar with how many items need you beside it: the
**Inbox** tab's count, so snoozed and dismissed items stay out of it, and no number shows at zero.
The icon is a template image, so macOS tints it to suit a light or dark menu bar. Click it for its
menu:

- **Open Nilbox** brings the window forward.
- Agent sessions waiting for your approval or input, under **Waiting for you**, such as
  **example-repo #12 · Claude needs approval**, or, for an upgrade from [Deps](deps.md),
  **example-repo · Upgrade dependencies · Claude needs approval**. Click one to bring its
  terminal tab forward (see [Show in terminal](following-a-session.md#show-in-terminal)), or its
  item in the window when the tab can't be shown.
- Up to five items from each list that need you, under the list's name, **Review requested**
  or **Yours** (a native section heading on macOS 14 and later), and **N more…** for the rest. Hover an item for its full title. Click it to bring the window forward with the
  item in view: its list opens at the tab that holds it, and its row scrolls into view,
  highlighted for a moment.
- While Autopilot (beta) is on and you have a routine, an **Autopilot** group: the routines'
  agents working, what waits for your OK, and **Pause Autopilot** (see [Autopilot's
  notifications and the menu bar](autopilot/notifications-and-menu-bar.md)).
- In a build that updates itself (see [Updates](updates.md)): **Check for Updates…** (**Checking
  for Updates…** while a check runs), then **Restart to Update (0.3.0)** once a new version is
  ready, or **Update to 0.3.0…** while one is found but not downloaded, which bring the window
  forward and restart there, as its banner does, asking first when restarting would lose
  something; and, for a minute after the menu's own check, a line saying how it went.
- **Refresh Now**, and when the last refresh finished, or that it failed (hover for why).
- **Quit Nilbox** (⌘Q).

The menu is redrawn whenever the inbox changes, after a refresh or a snooze, dismissal, or pin
in the window, when a snooze ends, when a session changes what it is doing, when Autopilot's
group changes, and when the update's rows change, so the count is right without waiting for a
refresh.

## Closing the window

While the icon is in the menu bar, closing the window only hides it: the app keeps refreshing, keeps
the count current, and can still show notifications. The icon, its **Open Nilbox**, or the Dock icon
brings the window back; quit from the icon's menu or with ⌘Q. While an
[Autopilot](autopilot/README.md) routine is on (and Autopilot (beta) is) or an agent works, closing
the window only hides it even with the icon off. Otherwise (the icon off in Settings →
Notifications, and no routine on) closing the window quits the app, as ⌘Q does: while a run goes in
the app, the window stays and asks first ([Runs](runs.md)). The log says *the app quit* as it does,
so a quit can be told from a crash there.

## What is notified

After each refresh, the app compares the lists with the last refresh's and records what newly needs
you; a release or a run is recorded as it ends, and a change of a service you watch as the status
check finds it:

| Kind | When |
|---|---|
| Review requested | A pull request newly asks for your review, including when it asks again after you reviewed it, or its author pushed to it since you requested changes without asking again (the banner says **Pushed since you requested changes**) |
| Assigned to you | An issue or pull request someone else opened is newly assigned to you |
| Back from snooze | New activity ends a snooze early under your [wake rules](snooze-dismiss-pin.md#waking-a-snooze-early); it says what was new |
| Released | A release you started reaches GitHub; it names the version and the commit it tagged |
| Published | The release workflow on GitHub publishes a release you started (*acme-org/sdk v1.4.3 is published*) |
| Release workflow failed | A workflow the tag of a release you started ran fails on GitHub (*The goreleaser workflow failed for acme-org/sdk v1.4.3*) |
| Run failed | A run from the Scripts tab (a release, a hook change, checks, a script, a tool's update, or a pull), in the app or in your terminal, fails or runs past its time limit; it names the run and its repository (or its folder) and says how it ended. One that couldn't start, one you stopped (Control-C, or closing its terminal window, included), and one a quit or a restart interrupted never notify |
| Service status | A part you watch of [GitHub, Claude, or OpenAI](window.md) goes down (*Claude: Partial outage*, with the parts affected and the incident), or is back (*Claude: Operational again*). The first check after the app opens, or after you turn a service on, never notifies |
| API budget draining fast | Something else signed in as you, such as `gh` or a coding agent, spends your [GitHub API budget](window.md#when-a-budget-drains-fast) fast enough to run it out before it refills (*Something signed in as you, not Nilbox, used 1,490 GraphQL points in 10 minutes.*, and when it runs out at that pace); you hear of each budget once an hour at most, and clicking it opens the API budget panel |
| New version | A new version of Nilbox is downloaded and ready to restart into, or, with **Just tell me**, out to download; you hear of each version once |

Nothing is recorded for the first refresh of an account (everything in it would be new), for
an item you dismissed or that is still snoozed, or for a refresh that failed. A list that shows
**N of M** can gain an item only because another left it, so in one an item counts only if it
changed since the last refresh. Refreshing a single card never notifies.

## The bell

The bell in the toolbar is a quick view of what is unread: its badge counts the unread
notifications, and a click lists the newest 20 of them, with **15 more unread · View all** when
there are more. Opening it marks nothing. Click an entry to show its item in the inbox, or on GitHub
once the item has left the inbox, which marks the entry read. An entry about no item, such as a
release, names its repository alone, and a click opens its page on GitHub; a release workflow that
failed is marked with a red cross, and opens the failed run there. A failed run's entry, marked the
same way, opens Scripts → Runs with the run in its panel; a toast says why it can't when the run is
no longer in the history, or the Scripts tab is gone (*Turn on an integration in Settings to see
your runs.*). Point at an entry, or move the focus to it, and **✓** takes the place of its time: it
marks the entry done, which takes it off the bell and keeps it in the history. **Mark all done**
does that to them all. **View all** opens the [Notifications page](notifications-page.md), and
**Notification settings** their settings.

## Read without asking

An entry is also marked read when you click its banner, and when you open its item's review or issue
sheet, however you open it; it stays read until you mark it unread on the page. A banner left from
before the history was cleared marks nothing, since its entry's number may belong to another by
then.

## Banners

Each kind can also show a native macOS banner. All ten are on by default, and each has its own
switch in Settings → Notifications. Autopilot's five kinds have switches of their own there, under
**Autopilot**, while Autopilot (beta) is on, all on by default too (see [Autopilot's
notifications](autopilot/notifications-and-menu-bar.md)). Two more switches there show a banner when
an [agent session](following-a-session.md) waits for you or finishes its turn; those start off, and
the history doesn't keep them. A banner gives the kind, the repository and number, and the title; a
snooze that ended early adds what was new. When one refresh finds more than three, a single banner
sums them up, such as **5 new in your inbox: 3 review requests, 2 assignments**. Clicking a banner
brings the window forward with its item in view. A release is about no item: its banner gives the
repository and the version, and clicking it opens the tag on GitHub, the release once it is
published, or the failed run once its workflow failed. So is a failed run: its banner gives the
repository (none for a folder that isn't one), what failed, and how it ended, and clicking it opens
the run in Scripts → Runs. Failed runs that come within 2 seconds of the first show their banners
together, and more than three as one, such as **5 new notifications: 5 failed runs**. A service
status change is about no item too, and clicking its banner opens the incident, or the service's
status page; so is an API budget draining fast, and clicking its banner opens the API budget panel
under the toolbar's gauge. Clicking a banner marks its entry read; a banner that sums several up
marks none.

The first time the app starts, macOS asks whether to allow its notifications. **Send a test**
in Settings shows a sample banner, asking first if macOS never has. If notifications are
turned off for the app, Settings says so; turn them back on in System Settings →
Notifications → Nilbox (Nilbox Dev for a build from source), which also sets the banner style,
sounds, and Focus.

Banners need the app bundle, whose identifier macOS files notifications under, so the release
and [Nilbox Dev](../development/build-targets.md#nilbox-dev-a-build-from-source) each have their own setting. They show in a
built app (`magex app:build`, `magex app:run`) but not in `magex app:dev`, which runs a bare
binary: there the history still fills, and Settings explains why no banner shows. Nothing
about notifications leaves your Mac: they are made from what the inbox already reads and
posted to macOS.
