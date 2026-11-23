# internal/svcstatus

[Docs](../README.md) › [Architecture](../README.md#architecture) · Code: [`internal/svcstatus`](../../internal/svcstatus) · Guide: [The window](../guide/window.md)

`internal/svcstatus/` follows the status pages [`internal/statusprefs`](statusprefs.md) catalogs, so
the window can say when a part the user watches has an incident. A check is one GET of a provider's
summary through `(*github.Client).FetchStatus`, outside any API, so it sends no token and costs no
budget, and is conditional on the page's last ETag (GitHub's and Claude's give one; a 304 keeps the
reading). The pure parsers (`statuspage.go`, `incidentio.go`) turn each format into one reading of
components and incidents on one severity scale (`State`: operational < maintenance < minor < major <
critical; `full_outage` and `major_outage` are critical, an impact of none is minor, anything
unknown minor), and the pure `judge` (`relevant.go`) decides by component, never by a page's overall
state, which also turns for a Copilot or Codespaces incident: a component matches a picked part by
its page id, or else by name and, where the part names one, group (OpenAI lists two components named
Login); the parts watched that aren't operational are `Affected`; an incident counts when it names a
part watched, or names none and is major or critical (`Unscoped`), and every other is counted in
`Others`; a Statuspage page's parts watched that it no longer lists are `Unlisted`, logged once.
Each provider's last reading is kept raw, in memory alone, and judged against the choices as they
are on every `Status`, so ticking a part costs no request.

`Config.Rules` reads the interval and the choices in one read of the settings (core's `statusRules`,
the defaults, logged, when the settings can't be read). The bound `Status` serves every provider in
catalog order, off while not watched (switched off, no part ticked) or the check off, and `CheckNow`
(the popover's Check again) reads, in parallel, each watched provider whose last check ended
`FreshFor` (10 s) ago or more, one `flight` key per provider, so a timed pass and a click share a
request; a check that fails keeps the reading before it, with the reason in its `Error`, and is
logged, and each pass emits one `event.ServiceStatusUpdated` (`servicestatus:updated`).

The `Checker`, a deferred desktop worker in its own select loop (recovering and logging a panic; its
tests run in a `testing/synctest` bubble), checks every watched provider after the first paint, then
each once its last check ended as long ago as Settings → Service status says
(`refresh.serviceStatus`, 30 minutes by default, 5 minutes to an hour, 0 off; woken on
`event.IntervalsChanged`), taking along any watched provider checked half an interval ago or more,
so the providers share one timer; on `event.StatusRulesChanged` (core's relay calls `RulesChanged`)
it drops what it keeps of a provider no longer watched, its ETag included, sets the alerts'
baselines quietly, tells the window, and checks a provider turned on at once.

Alerts (`alerts.go`, in memory): the first reading after a start, or of a provider just turned on,
only sets the baseline, and a change of the choices sets it at once from the reading kept, so a
restart during an incident repeats no banner; a state that rises into
minor, major, or critical above the level already known tells its `Notifier` ("Claude: Partial
outage", the parts affected and the first incident, opening the incident), one that falls back to
operational or maintenance after it tells "Claude: Operational again", and nothing else does; core's
`statusAlerts` records each as a `service_status` notification about no item, with its banner (on by
default). It never imports notify, settings, core, the inbox, or the desktop shell (the repoguard
boundary test).

The window shows nothing while every watched provider is operational, unchecked, or the check is
off; otherwise one amber (minor, maintenance) or red (major, critical) warning sits before the
budget gauge, opening a card per provider in trouble (its parts affected, its incidents with their
stages, how many others don't touch what is watched, and its last error), and the account menu's
Service status submenu names each watched provider's state.
