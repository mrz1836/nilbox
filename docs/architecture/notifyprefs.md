# internal/notifyprefs

[Docs](../README.md) › [Architecture](../README.md#architecture) · Code: [`internal/notifyprefs`](../../internal/notifyprefs) · Guide: [Autopilot's notifications and the menu bar](../guide/autopilot/notifications-and-menu-bar.md), [Menu bar and notifications](../guide/menu-bar.md)

`internal/notifyprefs/` is the kinds of the app's own notifications and the switches of which of
them show a banner, in Settings → Notifications, which settings alone writes and
[`internal/notify`](notify.md) obeys. It is a leaf: it imports only `prefval`, so settings, which
the repoguard boundary test keeps off `internal/notify`, writes the switches without depending on
notify, nor through it on the inbox and the GitHub client. What the user sees is
[Banners](../guide/menu-bar.md#banners) and [Autopilot's
notifications](../guide/autopilot/notifications-and-menu-bar.md).

## The kinds

`Kind` is a named string type, all its constants in `notifyprefs.go`, so `magex app:bindings`
writes it as one TypeScript union. `Kinds` lists all 16 in the order the settings list them:
`review_requested`, `assigned`, and `woke` (the inbox's arrivals, `inbox.Arrival`);
`session_needs_you` and `session_finished` (an agent session waiting for the user's approval or
input, or finishing its turn); `release_done`, `release_published`, and `release_failed` (a
release's tag reaching GitHub, its workflow publishing it, or its workflow failing); `run_failed`
(a run from the Scripts tab that failed or ran past its time limit); `service_status` (a part of a
service the user watches going down, or coming back); `update_ready` (a new version ready to
restart into, or only available with Just tell me, told once per version); and Autopilot's five,
which `AutopilotKinds` lists in the same order: `autopilot_alert`, `autopilot_needs_you`,
`autopilot_finished`, `autopilot_checks`, and `autopilot_problem`. `Kind.Valid` reports a kind of
`Kinds`, `Kind.Autopilot` one of `AutopilotKinds`, and `ErrUnknownKind` is the error for any
other.

`Kind.DefaultOn` is true for every kind but the sessions' two, which start off, since the coding
agents and the terminal can already notify the user themselves: the other nine kinds and
Autopilot's five show banners until the user turns them off (`TestDefaultOn`).

## The switches

Two prefs keys, each a JSON list of kinds (`prefval.List`, `prefval.EncodeList`), store the
switches as changes from the defaults, as statusprefs stores what the status check watches:
`notify.bannersOff` (`KeyBannersOff`) lists the kinds that start on and were turned off, and
`notify.bannersOn` (`KeyBannersOn`) the kinds that start off and were turned on. A kind a later
build adds follows its `DefaultOn` until the user changes it.

- **`DecodeBannersOff(storedOff, storedOn)`** gives the kinds whose banners are off, in the order
  of `Kinds`, never nil. Each list counts only for the kinds that start its way (`woke` in the
  second list, or `session_finished` in the first, changes nothing); a value unset (`""`) or that
  can't be read lists none, and a kind this build doesn't know is dropped.
- **`SetBanner(off, kind, on)`** turns one kind's banners on or off in a decoded list and returns
  the new list, in the order of `Kinds`, or `ErrUnknownKind` for a kind that isn't one.
- **`EncodeBanners(off)`** writes a decoded list back as the two stored values.

`FuzzDecodeBannersOff` checks that any stored values decode to known kinds, each once and in the
order of `Kinds`, that a decoded list reads back unchanged once encoded, and that turning any kind
on or off changes that kind alone and reads back unchanged too.

## Who writes and reads them

Settings alone writes the two keys, always together. `SetNotificationBanner(ctx, kind, on)` reads
both, applies `SetBanner` to what `DecodeBannersOff` gives, and writes both with `EncodeBanners`,
in the one transaction its `save` runs, then returns the settings, their `BannersOff` decoded again.
It emits no event: the notifications read the switches afresh each time they would post one. A
backup carries the list as `Portable.BannersOff` (labeled *Notification banners*), whose import
goes through `checkBannersOff`, which refuses a kind this build doesn't know and writes both keys.
The history keeps every kind whatever the switches say; only the sessions' two are banners alone.

The readers:

- **The notifications.** Notify's `Dispatcher` and `SessionBanners` read both keys each time they
  would post (`prefs.Store.GetMany`, then `DecodeBannersOff`), and the Dispatcher's `notify.Banners`
  drops the kinds that are off, as `SessionBanners` does for its own kind. An entry's own choice
  (`notify.Entry.Banner`, set only for an Autopilot routine's) outranks its kind's switch, and the
  user's allowing the app's banners in macOS holds either way.
- **The window.** `Settings.BannersOff` gives Settings → Notifications and the first-run setup's
  Notifications step their switches, through the generated `Kind` union, and each switch calls
  `SetNotificationBanner`; Settings lists Autopilot's five only while Autopilot (beta) is on ([At
  a glance](autopilot/notices.md#at-a-glance-glancego)).
- **Core.** `noticeKinds` (`internal/core/adapters_notices.go`) maps each `autopilot.NoticeKind` to
  its kind, and `TestEveryNoticeIsAKindOfNotification` holds the map to `AutopilotKinds`, each once
  and in order.
