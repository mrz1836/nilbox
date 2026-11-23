# internal/ghthreads

`internal/ghthreads/` mirrors the user's GitHub notifications (threads, in the REST API's
words) as the web inbox lists them, read and unread but not done, in `gh_threads`, which it
owns, one row per account and thread (its reason, unread, times, subject type and title,
repository, the pull request or issue `Thread.Ref` names, '' and 0 for any other subject,
and `latest_comment_url`, kept for the next check of who acted after a moment and never
shown or opened), and keeps the state of its passes in `ghthreads.sync`, a JSON pref only it
writes. Its writes to GitHub mark a thread done or read: clearing one (below), which sends
none until the user turns clearing on or clears one by hand, and the panel's own clicks
(`MarkDone`, `MarkRead`, below). A pass, one at a time (the Syncer's
and `CheckNow`'s share one), reads the rules through its `RuleSource` (core's
`ghThreadRules`, one read of the prefs): with `Read` off it sends nothing and borrows no
token, and marks the mirror stale (`walkNext`). Otherwise it borrows a token through
`github.WithToken`, learns its login with `FetchViewer` (remembered per token in a
`github.ViewerCache`, the inbox's building block too: one GraphQL point per new token), and
lists through its `Client` (`*github.Client`): page 1 with the ETag its last answer
carried, a 304 ending the listing at no cost, and a 200 reading on (`ThreadPage.NextPage`)
until a page whose oldest thread is no newer than the high-water mark, the newest
`updated_at` the last stored listing saw. A 304 covers page 1 alone, so every page is
walked, with no ETag, up to `github.MaxThreadPages` (the newest 1,000, the mirror
`Truncated` when GitHub lists a page past them, `ThreadPage.Capped`): at the first pass
after the Service is built, every `FullWalkEvery` (6 h), after the account changes (another
account's ETag and mark are never used, and its rows, never shown, are dropped once unseen
for 30 days), once reading is back on, and after a refusal; a pass that reads to the last
page counts as one. A 200 drops the account's rows it didn't list within the span it read
(done on GitHub): every one for a walk, and, for a pass that stopped at the mark, those
updated strictly after the oldest thread on its last page, since GitHub lists the newest
updated first; a thread updated while the pages are read may be dropped once, and listed
again by the next pass. A pass stores its listing in one `store.InTx` at its end, rows and
`ghthreads.sync` together, so one that fails partway stores nothing, and never an ETag or a
mark the pages it meant to read don't back. GitHub refusing the listing to the token
(`github.NotificationsRefused`: 404, or 403 but a rate limit) marks access refused ("Your
GitHub token can't read notifications."), and no listing is sent for `RefusedFor` (6 h)
unless `CheckNow` asks; any other failure is only the status's last error
(`uierr.Message`), and the next pass tries again. Then, a 304 pass included, it checks the
state of the pull requests and issues its threads are about, each once, in one
`FetchItemStates` call: those never checked, those whose thread updated since, and those
not known merged or closed and checked `StateRecheck` (30 min) ago; releases, discussions,
check suites, and commits are counted, never checked. One GitHub can't find is stamped
checked, its state kept, and a check that fails stores only the last error, the listing
standing. The `Syncer`, a desktop worker after the scheduler and woken with it
(`followed.intervals`), runs a pass at once, then the longer of its own refresh interval
(Settings → Refreshing, `refresh.notifications`, a minute to an hour) and GitHub's
`X-Poll-Interval` after the last pass ended, its own or `CheckNow`'s (a timed
pass that finds one ended while it waited for the running one lists nothing), in its own
select loop, recovering and logging a panic; its tests run in a `testing/synctest` bubble.
The bound `Snapshot` reads the database alone, for the account the last pass read: the
status (reading, the account, `Access`, the last pass and walk, the last error, when a
refused listing is sent again, the count, the unread count, and `Truncated`), and the
newest 20 `Unread`, none with a URL GitHub gave; with reading off, its lists are empty.
The bound `List` reads every thread the mirror holds for that account, read and unread,
each with its item's state as the last state check read it (`Thread.State`), for the
Notifications page, with the same status: `snapshot` and `List` share one row walk
(`listed`), so both count alike. The `Snapshot` also holds an unexported fingerprint of
every listed thread (its id, unread, state, title, reason, and update time), which JSON and
the bindings skip but `tell`'s comparison sees, so a pass that changes a thread past the
newest unread still tells the window.
`CheckNow` runs a pass once any running has ended, lifting the back-off and ignoring the
poll interval, and returns the `Snapshot`, whose status tells a pass that failed. A pass
that changes what `Snapshot` shows, the passes' times aside, emits `event.GHThreadsUpdated`
(`ghthreads:updated`), but one whose caller gave up tells nothing.
It clears the threads by the user's rules (`clear.go`), and owns `gh_thread_moments` and
`gh_thread_clears` for it. The inbox's `Config.Outcomes` is its `Settler` (core's
`ghThreadOutcomes` adapts `inbox.Outcome` to its own `Outcome`, never bound), whose
`Settle` records, in one transaction and without waiting, the moments the user finished an
item in the app (a merge, a close, a review that approves or requests changes, a
dismissal, taking themselves off), a review of the user's made anywhere else that the inbox
saw (an agent's gh, a browser: `OutcomeReviewed` from a read of the pull request alone or a
whole refresh, once per review and within two hours of it, at GitHub's time for it, so a
thread that moved after it isn't cleared), a merge's or a close's state as GitHub answered it
(`MERGED` or `CLOSED`, read now), and a restore's taking back of the dismissal's moment and
its queued clears (kept), then nudges the `Syncer`, which runs a pass on those items at
once, listing first only when the mirror is older than the poll interval (a minute at
least, `MinNudgeListing`); an item the inbox saw close or leave its lists has its state
checked. An item merged or closed, or that left the lists (`OutcomeKind.finishes`), is also
followed up (`nudges.follow`): GitHub tells of the merge or close a little after it, and of
what follows it (a bot's comment), and each brings a thread already marked done back, so the
`Syncer` runs a nudged run deciding the threads about the items followed up every
`FollowUpEvery` (2 minutes) until `FollowUpFor` (10 minutes) after the inbox told of each
(`takeFollowUp`, `untilFollowUp`, a timer of its own in the `Syncer`'s sleep). Each lists,
since the mirror is older than the poll interval by then, conditionally, so a page that
didn't change costs nothing; a thread that came back has its state checked again and clears
at once. A pass reads the account's threads, moments, and clears once (`decideInputs`),
and again only what a step changed: the threads once a state check stored an answer, and
the clears inside the transaction that queues, since a click may have queued or kept one
meanwhile; a send reads its one item's moments. After its listing and state checks, a
pass decides each pull request and issue thread with `threadrules.Decide` against every
moment that could apply: the user's own, and GitHub's own merge or close
(`outsideMoment`, only on a state read within `StateFreshFor`, an hour; one older that
would clear is read again first, but not while a backlog counted under the same rules is
held back over its cap, `BacklogRules` in `ghthreads.sync`: such threads count toward it
as they are, so a held backlog costs no state reads). A thread held back, unread and
updated after the moment, has who acted after it checked once per update of the thread,
and only while a bot's activity counts: the latest comment's author (`FetchLatestComment`,
one REST point, `MaxCommentChecks`, 150, a pass at most, the rest the next pass, the checks
stopping at the first answer that says GitHub is limiting the app's requests,
`github.RateLimited`), stored as `after_moment`, a person's sticking until the thread is
read (a person's comment followed by a bot's before the next pass reads as a bot's). A
latest-comment URL that is the subject's own is an event, not a comment, and needs no
request: on an item merged or closed it is the merge or the close itself, a bot's; on an
open one (a push, a request for review, an approval with no text) it counts as a
person's. A thread that clears is queued in the ledger before anything is sent, with its
trigger and action (`threadrules.action`: done, a `DELETE`, or read, a `PATCH`): a clear
the user's own action chose waits the Undo hold (`Hold`: the toast setting plus 3 s, 15 s
at least, through core's `ghThreadHolds`), one GitHub's state chose goes at once, and the
latest hold among the moments that clear wins. One the user kept (`Keep`), and one whose
clear failed, is not queued again on its own until its thread moves (`marks`); the user
may still clear it by hand. The first pass after clearing turns on or the rules change
(`RulesSeen`, a fingerprint in `ghthreads.sync`) queues at most `FirstCap` (25), any later
one `Cap` (150); one over its cap queues none and records the count as the `Backlog`. The
sends go one at a time through the client's chore lane, from a pass or from a run the
`Syncer`'s timer starts at the next `send_after` or when the lane lets the next go, which
lists nothing and lets go of the pass slot at the lane's first refusal, and each is checked
again first (`sendOne`): clearing still on (but for one by hand), reading on, GitHub not
refusing the listing, the token's login the row's (else the queue waits for the next
pass), the thread still listed at the `updated_at` it was decided on (else decided again;
one not listed is skipped, never sent), queued less than a day ago (else decided again),
and `Decide` still clearing it. A 404 counts as done; a refusal of the lane costs no
attempt; any other failure is tried again a minute on, three times at most, then failed. A
thread marked done leaves the mirror, and one marked read stays, read. Clearing turned off
(settings' Go-only `event.ThreadRulesChanged`, which core's relay turns into the `Syncer`'s
`RulesChanged`) skips every queued clear but the user's own at once, and reading turned off
skips them all and sends nothing. `ClearItemThreads` clears an item's threads by hand (no
hold, with clearing on or off, never a thread that isn't a pull request's or an issue's) and
says how many it queued, which Autopilot's Mark the GitHub notification done runs; `Keep`
takes queued clears back; and
`MarkDone` and `MarkRead` (`mark.go`) are the panel's Done and its opening of a line on
GitHub: any thread the mirror lists for the account, whatever its subject and whatever the
rules' action, sent at once through the chore lane (waiting out its pace up to
`MaxPaceWait`, else worded), holding the pass slot so no pass stores a listing read before
GitHub took the mark, after the token's login is checked against the account, each landing
in the mirror as GitHub takes it (done or gone: dropped; read: kept, read), recorded in no
ledger, and told to the window. The
`Snapshot` also lists the `Queued` clears, the `Backlog`, and the `Recent` 50 that settled.
Moments and settled clears are dropped 30 days on. It never imports the inbox, settings,
core, notify, the sheets, or the desktop shell, and the inbox never imports it (the
repoguard boundary test).

**Marking a backlog done in bulk** (`bulkdone.go`, `bulkrun.go`). GitHub's API marks one thread
done at a time, a click waits `MaxPaceWait` at most for the chore lane, and the mirror holds
only the newest 1,000, so Mark all done is a job of its own, which the Syncer runs. The bound
`StartBulkDone(BulkDoneRequest{Age, Repos, All})` refuses, with a `uierr` sentence, while
reading is off, before a pass read an account, while access isn't `AccessOK`, for an age it
doesn't know (`BulkAge`: `any`, `day`, `week`, `month`, `quarter`) or a request that names no
repository, and while one runs; otherwise it stores the job in `ghthreads.bulkDone`, a JSON
pref only it writes (`bulkState`: the account, the cutoff `Before`, the click's time less the
age in whole seconds, the repositories or `All`, the `Cursor`, when it started and ended,
whether it was `Stopped`, the mirror's matching rows at the start as `Expected`, `Done`,
`Failed` and the `Skipped` IDs, the current thread's failed `Attempt`, and `NextAt`, `Pause`,
and `LastError`), counting what the mirror lists in its scope, in one transaction, and wakes
the Syncer. Every change of the pref is a read and a write in one `store.InTx`
(`changeBulk`), so a `StopBulkDone` that lands while a run marks a thread is never undone by
the run's count. `DismissBulkDone` deletes the pref of a job that ended, which outlasts a
restart. The Syncer runs a bulk run (`passRequest.bulk`) when `untilBulk` says one is due,
after the queued clears: `bulkAt`, on the Service's clock, set by the run that ended (with a
generation, as the sends have, so a job started meanwhile is never put off) and armed after
any other pass while a job runs and waits on no time (`armBulk`), which is how a job left
unfinished goes on after a restart, and how one paused for another account's token or
reading off goes on. A bulk run never counts as the last pass for the timer. With reading
off it pauses the job (`BulkPauseReadingOff`) and sends nothing. Otherwise it borrows a
token and checks its login is the job's account (`BulkPauseOtherAccount` until a pass, as
for no token), then reads page 1 of the listing before the cursor
(`ThreadQuery{Before: Cursor or Before}`), past the mirror's cap; one page stands for
`bulkPageFor` (a minute) in memory (`bulkPage`), so one GET covers up to 50 marks. Its
targets are the page's threads in scope (`strings.EqualFold` on the repository, every one
for `All`) and not `Skipped`; a target the mirror lists as updated at or after the cutoff
has new activity and is left alone. Each mark goes through `MarkThreadDoneInBulk`, the bulk
lane, holding `sending` as a queued clear's send does: taken or not found, the thread leaves
the mirror, a clear queued for it is settled (`settleQueuedFor`), and `Done` counts it, out of
any pause, in one transaction; a lane refusal stops the run until `budget.RetryIn` says,
pausing the job as `held` or `reserve` for GitHub's hold or the reserve (the lane's own pace
is no pause); any other failure pauses it as `failing` for `FailedRetry`, and after
`MaxAttempts` in a row the thread is given up (`Failed`, `Skipped`), the job stopping after
`maxBulkSkipped`. A full page with none in scope moves the cursor to its oldest update and a
second, which lists the threads updated in that second again, or a second back when that
doesn't move it, at most `maxBulkPages` pages a run; a page whose targets were all marked
moves it the same way; and a short page with none ends the job (`EndedAt`). The `Snapshot`'s
`BulkDone` (`BulkDoneStatus`, every slice non-nil) is how it stands, with `Left`, the mirror's
rows still in scope, `More` while the mirror is `Truncated`, and the bulk lane's
`PerMinute` and `PerHour`. After each mark the run emits `event.GHThreadsBulk`
(`ghthreads:bulk`) with the counts; `compared` leaves the counts out (and `NextAt` without a
pause), and a bulk run calls `tell` only every `bulkTellEvery` (10 s), or at once when the
job's state beyond its counts changed (a pause, the end, a stop), so the window's 1,000-row
`List` isn't read again every 1.5 seconds. Start, stop, and the end are logged with counts.
