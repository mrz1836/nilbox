# internal/milestones

[Docs](../README.md) › [Architecture](../README.md#architecture) · Code: [`internal/milestones`](../../internal/milestones) · Guide: [Inbox nil](../guide/inbox-nil.md)

`internal/milestones/` keeps the local log of the inbox reaching nil and says what each time
is worth, so the party at nil can save its confetti for what means something. The log is one
preference, `milestones.log` (`KeyLog`), JSON through `store.EncodeJSON`/`DecodeJSON`, which
this package alone writes: `count` (the nils the user earned), `streak`, `best`, `lastDay`,
`confettiDay`, and `unlocked`, each achievement as `"kind:n"`. Days are local calendar days
(`YYYY-MM-DD`) from the injected `clock.Clock` in a `*time.Location` (`WithLocation`, default
`time.Local`); yesterday is computed at noon, so a day a clock change makes 23 or 25 hours
long still follows the one before. The one bound method, `Reached(Reach{Earned, Cleared,
Loud})`, reads and writes the log in one `prefs.Store.Update` (one IMMEDIATE transaction), so
two windows count both times and throw confetti once at most. Every call extends the streak:
the same day leaves it, the day after adds one, a gap starts it again, and a day before the
last (a clock or zone moved back) leaves it as it is; `best` follows. Only an earned call adds
to `count` and unlocks achievements (`Kind`, all its constants in `kind.go` so the bindings
write one union): every streak tier the streak has reached (3, 7, 14, 30, then every 30) and
count tier the count has (10, 25, 50, 100, then every 100) not unlocked before, so a tier
reached on a day the inbox was only found empty unlocks with the next earned nil; the first
nil; a sweep of `SweepAt` (10) or more cleared; and the badges for the hour and the day
(early 05:00–12:00, Friday, the weekend, owl 22:00–05:00). `Moment.Unlocked` is never nil and
lists them the most significant first (streak, count, first, sweep, badges, each kind's
highest tier first). `Confetti` is `Loud && len(Unlocked) > 0 && confettiDay != today`, and
stamps `confettiDay` when true, so the once-a-day cap holds across restarts and windows.
A stored log that can't be read starts afresh (logged), and a value no build writes (a
negative number, a day that isn't one) reads as never having happened. The log is history,
like the triage: a backup never holds it, and a reset clears it with the database. Core
builds the service beside the settings and binds it.
