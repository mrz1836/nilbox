# Architecture

These are the full design notes, one section per package, that the project guide,
[`CLAUDE.md`](../CLAUDE.md), summarizes. The guide's Layout keeps a short entry for each of
these packages, with what it is for, what it owns, and the rules it keeps, and links here. Each
section below holds the package's whole write-up, word for word as the guide carried it. Read
the section for a package before changing it, and keep the section and the guide's summary in
step with the change.

- [`internal/review`](#internalreview): the review sheet's service
- [`internal/ghthreads`](#internalghthreads): the mirror of the user's GitHub notifications,
  and clearing them
- [`internal/update`](#internalupdate): how the app updates itself
- [`internal/gitread`](#internalgitread): the read-only git, and the guarded setups for the git
  commands that change a clone
- [`internal/release`](#internalrelease): the Release page's service
- [`internal/scripts`](#internalscripts): Repo scripts' service
- [`internal/inbox`](#internalinbox): the routines' searches, read after the inbox's store, and the
  inbox's reads for Autopilot
- [`internal/autopilot`](#internalautopilot): the routines core, Autopilot's pure leaves

## internal/review

`internal/review/` is the review sheet's service. `Decide` is pure: from a pull request's
facts it works out every action's offer and the one primary, and `Act` runs it again before
any write. An offer with warnings asks first (`offers` sets `Offer.Confirm` for it), since
`Act` refuses it unless the request accepts them; the window confirms a merge with its
methods and any other action with none. Asking Dependabot to rebase is offered on every open
Dependabot pull request, enabled once the sheet read it, for a user with push access, while
the branch is behind or has conflicts; Update branch whenever GitHub's `viewerCanUpdateBranch`
lets the user (with a warning on Dependabot's), and, disabled with why, on any other pull
request GitHub reports as behind. Each is the primary only while the pull
request is behind (the rebase also with conflicts), never as a fallback: with no primary and
offers enabled, the sheet's button only opens the menu. Every write goes through the inbox's
`Actor` (the item lock, the account check, and the token retry, never bound, since its
methods take funcs), is pinned to the head the user saw (an approval reads the head first,
since GitHub takes a review of an older commit), and a merge first reads `mergeStateStatus`
fresh and merges only when GitHub says it can, so an admin's token never merges past branch
protection on its own: only `merge_bypass`
("merge past the rules") does, offered when GitHub sets `viewerCanMergeAsAdmin`, never the
sheet's button, and with each rule it skips to accept. An UNSTABLE pull request's readiness
line and merge warning say what GitHub counts as not passing: optional checks failing or
running, and the head commit's workflow runs that ended without a check (`Unrun`, read in
the review query from the head commit's own check suites, `headSuites: commits(last: 1)`,
which the details fragment asks for under the same alias and arguments, so GraphQL merges
the two and a fork whose rollup is null still shows them: a fork's runs awaiting approval,
each with its run's ID and page, or runs that failed to start), which the sheet names
beneath its checks, each a link to its run. Runs awaiting approval are approved with
`approve_runs` ("Approve workflows to run"), offered in a bar under the sheet's header and
in the menu, never the button: held back until the sheet read every one's run ID (a cache
an older build kept has none), for a user with write access, and asking first, with a
warning, when the pull request changes workflow files. `Act` approves the runs the cache
read at the head the user saw (`loadAct` refuses another head as `pushed_since`), one REST
`POST .../actions/runs/{id}/approve` each (`(*github.Client).ApproveWorkflowRun`, through
`edit`), never one twice across the token retry; once GitHub approved any, a refusal of the
rest is the result's `Reason` (`runs_approved`, "Approved 2 of 4 ..."), not an error. The
sheet stays open after it, with the draft, and reads again (`ReadAgain`) at once, then
three seconds apart up to four more times, until fewer runs are held and a check is
running; each read rewrites the card, which the checks follower then keeps current. Each step is reported as an
`event.ReviewProgress`, which the sheet shows while the user waits. Line comments are checked
against the cached diff (`hunks.go`) before anything is sent. `Decide` marks every offer with
what its text goes with (`Offer.Reviews`, `Offer.CommentsFirst`, and `Offer.CommentBlocked`,
why GitHub can't take the comment now), and `Act` and the window both go by those marks. A
merge with no review of its own (`merge_now`, `merge_bypass`, `auto_merge`) posts the
composer's text and line comments, when there are any (by `textutil.HasText`, which trims as
the window's `trim()` does), as a COMMENT review at the head the user saw, the way `comment` does
(never an approval, so it works on the user's own pull request), but only once the fresh merge
state says GitHub would merge (auto-merge: that it is open, not a draft, at that head), so a
refused merge strands no comment; then it merges exactly as it would without them: no
auto-merge fallback, and `merge_bypass` still needs its rules accepted. A comment that fails
merges nothing, and once one is posted nothing after it is an error: a merge GitHub won't make,
or any failure, is `commented_not_merged`, a result whose words, like `Result.Commented`, say
the comment was posted, so the sheet drops the text and never posts it twice. A close
(`close`) is offered on an open pull request, held back ("Wait for it to load") until the
sheet read it, then only when GitHub's own `viewerCanClose` says so (`Facts.CanClose`, read
by the review query and kept in the cache), and always confirmed. It is never the button:
the window runs it only when picked from the menu (`menuOnly`), never keeps it as the chosen
action or runs it with ⌘↩, counts it as nothing to do, and sends with it only text the user
wrote or picked, never a default reply. GitHub takes no head to pin a close to, so every try
(a retry with a fresh token included) reads the head first (`FetchMergeState`): one no
longer open is refused, and one pushed to is `pushed_since` with nothing sent, or, after the
comment, `commented_not_closed`. Then it posts the text and line comments as a COMMENT
review the same way, and closes with one REST `PATCH` (`(*github.Client).ClosePull`, whose
refusals `closeError` words as a close, never as a merge or a push). A comment that fails
closes nothing, and anything that fails after it is `commented_not_closed`, decided in one
place (`failedAfterComment`). A merge or a close takes the pull request out of every list
(`Acted.Merged`, `Acted.Closed`) and drops its cache. It owns `review_diffs` and
`review_diff_files`, a bounded cache of the diffs it read (per pull request, in all, and by
age), pruned in the same transaction that writes. The one read of the pull request
(`FetchPullReview`, one point, measured) brings its conversation too: the newest hundred
entries of its timeline (comments, reviews with their first twenty line comments, each with
the last lines of its diff hunk, and events; its commits left out), counted by GitHub's
`filteredCount`, since a pull request's `totalCount` counts every type, and twenty when the
answer is too large, with the description's edited time and reactions. Reviews still
pending, and ones that only comment and hold nothing, are dropped; a review of another
commit than the head is marked `Outdated`. The context the cache keeps holds the
conversation (`timeline.Entries`), cut to fit 1 MiB (`fitContext`, `timeline.Cut` at 16
KiB, then 4 KiB, then nothing, each cut marked) on every write, and a read under 20 seconds
old is served again only while the card shows no activity since it (`recent`), so a new
comment always shows. `Open` starts a visit as the issue sheet's does: `review_diffs.seen_at`
holds GitHub's `updatedAt` of the read it shows, which only `Open` moves
(`Review.LastSeenAt`). `Earlier` reads the conversation's page before a cursor
(`FetchTimeline`) through the inbox's `PeekPull`, and stores it nowhere. The summary lists
each reviewer's latest review without its text, which is in the conversation, then who the
card says the pull request still asks to review (`withRequests`, from `review_users` and
`review_teams`, no request), above the checks, which show every failing one and fold the
rest under their counts once they pass `openChecks` rows. A merge that
went through tells its `Merges` the repository, without waiting (core: the Release page's
`Refresher`, which reads that repository's row again); a close tells it nothing. `EditLabels`
adds and takes off a pull request's labels as the issue sheet's edit changes an issue's, at
the same issues endpoint (`AddLabels`, `RemoveLabel`, which the client takes for a pull request
too): pinned to the head the request names (another is `PushedSince`, with nothing sent), for
triage access or more (`ErrCannotEditLabels`), through the Actor, each step sent once across the
token retry, and a step GitHub made standing (`LabelsResult`); Autopilot's labels action is its
caller. Nothing here lets an agent approve, merge, or close.

## internal/ghthreads

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
dismissal, taking themselves off), a merge's or a close's state as GitHub answered it
(`MERGED` or `CLOSED`, read now), and a restore's taking back of the dismissal's moment and
its queued clears (kept), then nudges the `Syncer`, which runs a pass on those items at
once, listing first only when the mirror is older than the poll interval (a minute at
least, `MinNudgeListing`); an item the inbox saw close or leave its lists has its state
checked. A pass reads the account's threads, moments, and clears once (`decideInputs`),
and again only what a step changed: the threads once a state check stored an answer, and
the clears inside the transaction that queues, since a click may have queued or kept one
meanwhile; a send reads its one item's moments. After its listing and state checks, a
pass decides each pull request and issue thread with `threadrules.Decide` against every
moment that could apply: the user's own, and GitHub's own merge or close
(`outsideMoment`, only on a state read within `StateFreshFor`, an hour; one older that
would clear is read again first, but not while a backlog counted under the same rules is
held for review, `BacklogRules` in `ghthreads.sync`: such threads count toward the cap
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
skips them all and sends nothing. The bound `Preview` lists, from the database alone and as
if clearing were on, what would clear, by repository, and what is held back, by reason;
`Clear` and `ClearItem` clear threads by hand (no hold, with clearing on or off, never a
thread that isn't a pull request's or an issue's), and `ClearItemThreads` is `ClearItem` saying
how many it queued, which Autopilot's Mark the GitHub notification done runs; `Keep` takes
queued clears back; and
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

## internal/update

`internal/update/` is how the app updates itself. It holds the manifest each
release attaches (`manifest-darwin.json`, `ManifestName`): its JSON (`Manifest`, `Encode`, and
`Parse`, which refuses one over 16 KiB, a field the format lacks, or anything after it), the
fixed lines its Ed25519 signature covers (`SignedMessage`: the format, the platform, the
version, the zip's name, its size, and its SHA-512, so none can change without the key),
`Sign` and `Verify` (every field held to its form, so none can pass for another: the version
a release's, as semver allows it, the zip's name exactly `FileName`, its size at most 200
MiB), `Digest` and `CheckFile` (a zip's size and SHA-512 against a manifest `Verify`
accepted), the keys (`ParseKey`: raw base64 or a PKIX PEM block, never a private key and
never an Ed25519 point of small order, under which a signature could be forged, which
`Verify` refuses too; `ParsePrivateKey` and `EncodePrivateKey`, PKCS#8), the release's asset
names (`AssetNames`, from `brand.Name`), and `ReleaseKey`, the public key the app pins (a
new one strands every copy that pins the old). Stdlib crypto only, no service, no HTTP (the repoguard
boundary test). `magefiles/release.go` builds and checks releases with it (`magex
app:package`, `app:verify`), and `magefiles/keygen.go` makes the release key (`magex
app:keygen`), so the packaging and the app agree on what a valid release is; while
`ReleaseKey` is empty, `app:package` and `app:verify` refuse, and a test pins the SHA-256 of
the constant, so changing the trust root takes two deliberate edits.
The `Engine` (never bound; `Service` is the window's face, `Worker` its timed checks) checks,
downloads, and installs the app's updates. What a build does is its `Flavor`'s, which core
works out from `core.Options` (`updateSource`): a source build is off ("Updates are off in
builds made from source."), its `Status` still telling the mode the user chose; a release
reads GitHub's latest release through core's `githubFeed` over
`(*github.Client).DownloadAsset` (the manifest from the latest release's download link, at
most 16 KiB, a 404, `github.ErrDownloadMissing`, reading as no release; the zip from its
tag's, `ZipURL`, at most 200 MiB) and trusts `ReleaseKey` alone, whatever `UpdateFeed` and
`UpdateKey` say, and is off while it is empty; a rehearsal reads a local folder
(`FolderFeed`, its failures worded with no path) and trusts its own raw base64 key (never a
PEM block, never a point of small order). The running version must pass `ValidVersion`, or
updates are off. A check reads the manifest within `ManifestLimit` (a minute) and takes it
only once `Verify` passes, its platform is the build's, its version a release's (no
prerelease) and newer than the running one (`internal/semver`); no release, or none newer,
is up to date, and an update waiting that the releases no longer offer is dropped, so
deleting a release's manifest withdraws it from every install (the lever for pulling a bad
release). Where the app can update itself is `Location` (`OK` only in `/Applications` or
`~/Applications`, in a folder it can write); elsewhere checks still run and tell, nothing
downloads on its own, and `Download` and `Restart` refuse with `ErrLocation` ("Move
Nilbox to Applications to update it."). `Location.Movable` says a move helps (a build that
updates, its bundle outside every Applications folder), and the bound
`MoveToApplications` moves it: the target worked out again (`/Applications` when writable,
else `~/Applications`, made when missing), a copy of the app already there, by its
`Info.plist`'s bundle identifier (`bundleID`, never a link followed), never replaced
(`MoveExists`, for `OpenInApplications`, which checks that copy's signature against the
running app's requirement and opens it), anything else there refused (`ErrTargetTaken`), and
otherwise the copy made and checked in a hidden folder of the move's own beside the target
(`stage`, `stagePrefix`, its marker and an empty bundle in it), through the `Mover` the shell
attaches (`AttachMover`), and only then renamed into the target, never over anything that
appeared there (`place`, `renameNew`: RENAME_EXCL on macOS), so a copy cut off or never
checked is never where the app goes, and a failed copy's removal (`discard`, that folder
alone) never touches what was there; a later move removes such a folder a cut-off move left
(`sweepLeftovers`: named so, marked before the copy began by `stageMarker`, the bundle ID and
the move's process, holding nothing else but the copy however far it got, and its maker this
run or gone by `procs.Probe`, or, when that can't be told, older than `leftoverAfter`); then
the Mover quits the normal way and opens the copy once the app has quit (`MoveMoved`).
`OpenInApplications` refuses a copy macOS would open from a hidden folder
(`Mover.Translocates`, `ErrQuarantined`), leaving its mark, before the check, and a check that
couldn't read the app's own signature (`ErrOwnSignature`) blames no copy. The bundle that runs
is never its own copy, by the folder it started from too (`isSelf`), so one dragged into
Applications while it runs still reads as itself. Every move holds the app's quit from its
start to its end (`Mover.HoldQuit`): its copy and check within `MoveLimit`, the steps around
them (the sweep, the unmarking, the rename, a failed copy's removal) moments more. One move
runs at a time (`ErrMoving`), its copy bounded by `MoveLimit` and never cut short by the
window's call, and none starts
while a restart into an update installs or finishes after a held quit, which goes first (`ErrUpdating`,
with `ErrFinishing` for the latter, each worded for the window); the bundle that runs is never
touched, and `ShowAppInFinder` shows it, but for a translocated one. With downloads on, the zip goes into
`<data>/updates/<version>/` (`Cache`, `appdata.Paths.Updates`) through a temporary file, hashed
as it arrives, stopped at the manifest's size, within `DownloadLimit` (10 minutes), its
progress told as `event.UpdatesUpdated` a few times a second (`internal/progress`); only a
zip matching its manifest is kept, with its manifest beside it, and every start checks each
kept update again, so one waiting is ready at once. A background download that failed is
remembered by its version and SHA-512 and tried again only at the next due check, never on
the ticks between (a click on Update tries, and so does Check for Updates, which forgets
the failure and has the `Worker` download at once). One operation (a check, a download, a
restart) runs at a time. A failure changes no state: it is the status's `LastError`, worded
with no raw error, path, or URL in it (`reachWords`, `downloadWords`, and the restart's own
sentences), and a check that reached the releases is the last check, a manifest it refused
included. The `Worker` (deferred work) settles the start first: it reads the outcome of the
last restart from `update.pendingInstall` (`JustUpdated` when the running build is the
version it installed, newer than the one it ran from, or, with no record, newer than
`update.lastRunRelease`, as after an install by hand; `InstallFailed` when it still runs the
version it ran from, with the runtime's helper log, `<TMPDIR>/wails-update-<pid>.log`,
copied to `logs/update-<version>.log` and worded from the messages it ends on, which tell
the helper's exit code, never repeating a path, and `Retryable` unless the update itself
was at fault: a payload failure, the new version failing its signature check or a staged
update not installable, fails the same way every time, so its cache entry is dropped and
its version and SHA-512 (`pendingInstall.sha512`) go into `update.uninstallable` (the
last five), which no check downloads, tells, or offers again, and `Download` and
`Restart` refuse (`ErrUninstallable`), while a newer version, or the same one with
another SHA-512, goes on as usual; any other ending (the app not quitting in time, the
swap's attempts or the copy aside failing, `open -n` of the new version failing to start,
which says nothing of the update, a missing or unknown log) keeps the cache for a
retry), then cleans up by exact name only (beside
the bundle, `<bundle>.bak` and the `wails-update-*` folders holding this app's update or
nothing; in the temporary folder, the last install's helper `.log`, any other process's
once `HelperWindow` old and only when its first line names this bundle as the target, and
the `.ready` files of those processes alone), then loads the cache. While the last install
is under `HelperWindow` (3 minutes) old and its helper's log hasn't ended on a final
message (one ending on the old app being put back counts once `<bundle>.bak` is gone),
the helper may still be restoring the old app, so the start settles nothing (no
outcome, no cleanup, the record kept) and the `Worker` settles again once it may have
ended; meanwhile the bound `Download` and `Restart` refuse with `ErrSettling` ("The last
update is still finishing. Try again in a moment."), and `CheckNow` checks but downloads
nothing. It checks a minute later (`FirstCheckAfter`), or at once on a Wake within that
minute, then, through `periodic.RunEvery` on a
15-minute tick, whenever six hours passed on the wall clock (`CheckEvery`), since timers
stand still while a Mac sleeps; a Wake (the settings' Go-only `event.UpdatesChanged`, or a
manual check that found a version to download) has it look at once. A timed check tells
each version once (`update.notified`, through its `Notifier`: core's `updateNotices`, an
`update_ready` notification about no item, which opens nothing, so a click brings the window
forward), once it is ready, or available where it doesn't download; Don't check makes none;
`CheckNow` works in every mode, downloads nothing in Don't check or Just tell me, and marks
what it finds told without a notification once it is available or ready, so a download it
started that fails still lets a later check tell of it. `Restart(confirmed)` refuses while
runs go in the app, unless confirmed, with a `RunsGoingError` whose `Detail` (`{"runsGoing":
[...]}`, through `desktop.MarshalError`) names them; it checks the zip against its manifest
again (`CheckFile`; a damaged or missing one is dropped, back to available, downloaded
again when downloads are on, and the refusal says so), backs the database up first
(`store.Backup`, through its `DataBackup`, reusing one it took under `BackupReuse`, 10
minutes, ago) and stops when that fails, stages the update through the `Installer` the
shell attaches (`Attach`), checks again, unless confirmed, that no run started meanwhile,
records `update.pendingInstall` (`{from, to, at, pid, sha512}`) only then, so a quit while
the update is staged leaves no record of an install that never began, and emits
`event.QuitConfirmed{Restarting: true}` before `Installer.Restart`, inside which the app
exits: the shell then lets every quit through, whatever runs go, since a held quit would
leave the runtime's helper to reopen this copy beside the new one. A failure before the
quit, or a `Restart` that returns an error, goes back to ready with `LastError`, clears
the record even once the call's context ended, removes the staging folder, and, once the
quit was let through, emits the Go-only `event.UpdateRestartFailed`, which has the shell
hold a quit while runs go again. A `Restart` that returns nil with the app still running
(a quit something held) left the helper standing by, which can't be called back, so the
restart is finishing (`StateFinishing`, "Finishing the update…"): the staging folder and
the record stay, a later restart is refused (`ErrFinishing`), and the Go-only
`event.UpdateQuitHeld` has the shell see the app out (`Shell.finishRestart`): every quit
stays let through, it asks to quit again every `desktop.FinishQuitEvery` (5 s),
`FinishQuitTries` (4) times, off the main thread, and, the app still running
`FinishWithin` (25 s) after `UpdateQuitHeld.HelperStarted` (stamped by the engine just
before `Installer.Restart`, since the helper's wait starts inside it, and at once when
that is already past), stops the shell in order (closing the database, waited for at
most `FinishStopWithin`, 3 s) and exits (`desktop.Config.Exit`, `os.Exit` but in tests),
before the helper's 30-second wait ends, logging each step. `HideBanner` (Later) hides the banner of the version found for a day, `ShowBanner`
lifts it, `AckUpdated` forgets the toast, and `ShowInstallLog` shows the kept log in
Finder. It writes only the `update.*` state keys (`update.lastCheck`, `update.notified`,
`update.bannerHiddenUntil`, `update.lastRunRelease`, `update.pendingInstall`,
`update.uninstallable`), never
`update.mode`, and imports no service, no store or prefs, and no HTTP: core adapts the
feed, the preferences (`Memory`), the backup, the runs (`Runs`, over the run engine), and
the notifications (the repoguard boundary test). `store.RestoreBackup` copies a backup into
a temporary file beside the database before it sets the database aside and renames the
copy into place, putting the database back when the rename fails; with no database there,
it removes a `-wal` or `-shm` left behind first. Every open removes the copies a restore
cut short left (`.<db>.restore-<digits>.tmp`, regular files five minutes old or more).

## internal/gitread

`internal/gitread/` is the read-only git that release, hooks, and Repo scripts share: an exact-argv allowlist
plus validating predicates (a branch, a tag, a commit, a `v*` pattern, an absolute clean folder)
that refuses anything else with `ErrGitCommandNotAllowed` before git runs, and typed helpers over
it (the current branch, origin's default branch as the clone last learned it from its own
`refs/remotes/origin/HEAD`, an operation in progress, uncommitted changes, each dirty file with what
the index holds for it, unpushed commits, the commits between two, tags here and on origin, signing,
the hooks folder).
Every command runs as `git -C <dir> -c core.fsmonitor=false -c submodule.recurse=false` with
`GIT_OPTIONAL_LOCKS=0`, `GIT_TERMINAL_PROMPT=0`, `GIT_NO_LAZY_FETCH=1`, and an empty
`GIT_ALLOW_PROTOCOL` (no read reaches a transport, so a partial clone's missing object is never
fetched through a URL the clone names), without `GIT_CONFIG` (which would hide the clone's
settings from the guards' reads), never touches `core.hooksPath`, and an `ls-remote` gets a
15-second limit and a batch-mode ssh unless the user's environment or own config picks one. A
`status`, which reads files and so would run their filter drivers, first reads the drivers
with `config --show-scope` and runs with every one the clone's own config defines turned off,
Git LFS's too, since git-lfs's clean writes into the clone (its clean, smudge, and process
emptied and required off),
and with `--ignore-submodules=dirty`, so it runs no status inside a submodule, whose own config
could name one. An `ls-remote` likewise first reads, with `config --show-scope`, the settings
it weighs, and origin's URL with `ls-remote --get-url` (which asks origin nothing): a remote
helper (a `<name>::` URL after `insteadOf`, or `remote.origin.vcs`) is refused
(`ErrRemoteHelper`), since git falls back to an alias the clone defines; only the file, http,
https, and ssh transports run, as the user's own config and environment allow them, through
`GIT_ALLOW_PROTOCOL`, which beats every config; the clone's own ssh command, `core.askPass`
(git runs it even with `GIT_TERMINAL_PROMPT=0`), and credential helpers give way to the
user's own; an http or https origin the clone's own config reroutes (its TLS checks,
certificates, proxy, `remote.origin.proxy`, or resolved address) is refused
(`ErrCloneRoutes`), since the user's credentials, cookies, and headers for the real host
could go elsewhere; no cookie is saved, and a cookie file the clone names is dropped; and
origin is asked for git's own upload-pack with `--upload-pack` (git keeps the first
`uploadpack` it reads, so a `-c` can't replace the clone's). An `ls-remote` that fails after setting anything aside returns a `SetAsideError`
naming it, which `Message` (the words the Release sheet and Repo scripts' branch status
share for every gitread failure) gives as the likely cause and where to set the clone's own
settings instead: the user's global config, with an `includeIf` for the folder, which counts
as the user's own. Only the system, global, and environment (`command`) scopes are the
user's. Reading a clone runs no program the clone names (the user's own, such as Git LFS's,
still run). Its allowlist test asserts that no entry holds a verb `TestNoGitCommandChangesAClone`
guards. For the git commands that do change a clone, run elsewhere (Repo scripts' pull,
and worktree creation's fetches, `ls-remote --symref`, and `git worktree add`), it hands
over what they run with rather than running them: `RemoteSetup` (origin) and
`RemoteSetupFor` (a named remote, whose own URL and remote helper it weighs), for one that
reaches a remote, and `CheckoutSetup`, for one that writes files and reaches no remote.
Each reads the clone's whole config once, includes followed, with `config --show-scope
--show-origin -z --list`, and weighs every setting of the clone's own (the local and
worktree scopes, and whatever they include) by the three classes in `cloneconfig.go`, the
one place they are listed. Inert (`inertKeys`): what git init, clone, remote, branch,
submodule, sparse-checkout, and Git LFS write, and the notes editors and git clients keep,
which none of these commands reads as a program, a place to write, or a way to reach a
host: `core.{repositoryformatversion, filemode, bare, logallrefupdates, ignorecase,
precomposeunicode, symlinks, autocrlf, eol, sparseCheckout, sparseCheckoutCone}`,
`index.sparse`; `remote.<n>.{url, fetch, pushurl, tagopt, prune, promisor,
partialclonefilter, gh-resolved, gtserviceaccountidentifier}`; `branch.<n>.<any>`;
`extensions.{partialclone, worktreeconfig, objectformat, refstorage}`; `user.{name, email,
signingkey}`, `commit.gpgsign`, `tag.gpgsign`, `gpg.format`; `pull.{rebase, ff}`,
`fetch.prune`; `submodule.<n>.{url, active}`, `submodule.active`;
`lfs.repositoryformatversion`, `lfs.<url>.{access, locksverify}`; `rerere.{enabled,
autoupdate}`; `log.excludeDecoration`, `color.*`, `gui.*`; and `mergetool.*`,
`difftool.*`, `merge.tool`, `diff.tool` (and their gui ones), which only `git mergetool`
and `git difftool` read, and which the app never runs (`TestNoGitCommandRunsAMergeOrDiffTool`);
`remote.<n>.github-pr-remote` among a remote's notes; and what users and their tools write
for commands the app never runs, `push.*`, `rebase.*`, and `status.*` (no subsection; the
worktrees' own status is `--porcelain` with its own `--untracked-files`), `gitflow.*`,
`init.defaultBranch`, `merge.ff` (the pull's `--ff-only` beats it), and
`merge.conflictStyle`, none of which names a program, a path to write, or a host
(`core.editor`, `sequence.editor`, and `diff.*` stay refused). Neutralized (`neutralizedKeys`), as
the setup never lets the clone's own value reach git: `core.sshCommand`, `core.askPass`,
`credential.helper` (for every URL or one), and the cookie settings, which the remote
guard sets aside for the user's own in every setup (a checkout's Git LFS asks for
credentials too), a failure then saying which (`Setup.SetAside`, `SetAsideNote`), and of
the rest of `credential.*` only `username` and `useHttpPath` (`protectProtocol` and
`sanitizePrompt`, which a command-line setting can't beat for one URL, refuse, and every
setup sets both on again, `credentialHardening`); `url.*.insteadOf` and `pushInsteadOf`, as each remote
reached is weighed by the URL git will use; `http.*` and `remote.<n>.proxy`, whose
rerouting keys (`routeKeys`, a remote's proxy) refuse a command that reaches an http or
https remote, or one Git LFS may run in, whatever origin's scheme (`cloneRoutes`:
git-lfs reaches its server over https with the user's credentials), with
`ErrCloneRoutes`; `protocol.*`, which `GIT_ALLOW_PROTOCOL` beats; `core.hooksPath`,
`core.fsmonitor`, and `submodule.recurse`, which every setup sets again (husky writes the
first); and `maintenance.*` and `gc.auto`, as every setup turns automatic maintenance off.
Refused, before anything runs, with a `CloneConfigError` naming the settings and where
each is set, which `Message` words with where to set them instead (the user's global
config, with an `includeIf` for the folder): everything else, `refusedKeys`
(`branch.<n>.mergeOptions`) among them, so a hook (`hook.<n>.*` runs whatever it says), a
filter but Git LFS's own with exactly its own commands (`lfsSettings`, `--skip` ones
too), `core.worktree`, `include.*` and `includeIf.*` (an `includeIf` of
`gitdir:**/.git/worktrees/**` or `onbranch:` reaches only a worktree being made),
`core.alternateRefsCommand`, `gc.*` but `gc.auto` (`gc.recentObjectsHook` runs a program),
`lfs.customtransfer.*`, `lfs.standalonetransferagent`, `lfs.url`, `remote.<n>.{uploadpack,
vcs}`, a merge or diff driver, and any key a later git adds. `CloneConfig` runs that check
alone, for Repo scripts' branch status. The user's own scopes (system, global, and the
environment's) are never weighed, so an `includeIf` in the user's global config still
counts as theirs. Past the allowlist, the setup checks that git takes the folder as its
clone's top folder, comparing the two as files (`os.SameFile`, so a clone root whose case
differs from the disk's still matches; `ErrWorkTree`), weighs the remotes the command
reaches exactly as an ls-remote's guard does (a `RefusedError` naming the remote), and
returns a `Setup`: git as `GitFinder` finds it, the finder core hands the worktrees too
(`ErrNoGit` when it can't); an ls-remote's environment (for `CheckoutSetup`, and a
setup's `Checkout`, the same allowing no transport) with `GIT_DIR` and `GIT_WORK_TREE`
set to the clone's folders, which beat its config, and `GIT_NO_REPLACE_OBJECTS=1`, so a
replace ref can't have a checkout of the commit the launch checked write another's
files; and every setting, `Hardening` (`core.hooksPath` at an empty folder, so no hooks
folder's hook runs, and `hook.<event>.enabled=false` for every event git fires,
`hookEvents`, which git 2.56 documents as turning off every hook of the event, the
user's own config hooks too, but which a git whose config hooks predate that switch may
read as one hook's switch: the clone's own `hook.*` settings are refused regardless; no
fsmonitor or submodule recursion), `fetchHardening` (`core.alternateRefsCommand=true`,
so a fetch from a clone with alternates runs nothing to learn their refs, an empty one
making git try to run "" on every fetch; `maintenance.auto=false` and `gc.auto=0`, so no
fetch or merge starts maintenance), `credentialHardening` (`credential.protectProtocol`
and `sanitizePrompt` on), the remote guard's (the clone's own password program
and credential helpers set aside, no cookie saved), and any filter driver of the clone's
own but Git LFS's own turned off, passed through `GIT_CONFIG_COUNT` after any the
environment passes (`Setup.Settings` lists them), so the command's argv holds only its
own words. `LocalSetup` works out a `Local` for a command in a clone or a worktree that
reaches no remote, the worktree service's own status, removal, prune, unlock, and branch
deletion: a read's environment (no transport, no lazy fetch, no optional lock, no
replace ref), `Hardening`, `fetchHardening`, and every filter driver of the clone's own
off, Git LFS's too, with `Local.Refused` saying when the clone's own config holds a
setting a setup would refuse. In a partial
clone (`extensions.partialClone`, `remote.<r>.promisor`) either may fetch a missing file's
contents: its promisor remotes are weighed as a remote is, and the setup leaves
`GIT_NO_LAZY_FETCH` out and allows the user's transports (`Setup.Partial`). Only an
ls-remote has `RemoteTimeout`, whose end is `ErrRemoteTimeout`, which `Message` words as
origin not answering in time. `ValidBranch` is the branch rule a command's argument must
pass.

## internal/release

`internal/release/` is the Release page's service. `Candidates` lists every clone in the
repository locations whose origin is on GitHub, grouped as unreleased, up to date, no releases yet,
can't release here, or muted (`GroupMuted`: a clone of a repository muted as the list is read
is listed with its name and folder alone and never asked about, a row muted later keeps what
GitHub said and is served regrouped, through `arranged`, which also puts pinned ones first in
their group, and unmuting a row never read reads that repository alone, as a release's patch
does, `liftMuted`; the rules are read through its `RuleSource` on every call), from two rounds of batched GraphQL (`(*github.Client).FetchReleaseStatus`,
then `FetchReleaseCompare` for the repositories with a version, 15 to a query, adapted in core),
and reads each unreleased clone's state with `gitread`, never asking origin. It serves the
last list however old it is: the whole list is read again only on the page's **Read GitHub
again**, for a list from other locations, for one out of date, or with none yet (and
behind a list kept from a past start, below), never on a timer. A refresh that fails keeps
the last list, with its failure, until the user reads it again. A refresh tells the window
how far it has got, at most four times a second (`event.ReleasesProgress`: finding the
clones, each round's repositories as GitHub answers each batch, then the unreleased
clones), and each list that lands is kept in the `release.lastCandidates` pref with the
locations' `Key`. After a restart, the first `Candidates` serves that list at once, marked
`Kept`, while the `Refresher` (a desktop worker) reads GitHub again behind the page and
`event.ReleasesUpdated` says when it lands; a list from other locations is never served,
and a refresh that fails or is cut short keeps nothing. `version.go` works the next version
out exactly as magex does, tested against a table recorded from the real magex. A release
follows the review sheet's template: the `Gatherer` reads its `Facts` at once (magex through
a `ToolSource`, GitHub's status of the one repository with its newest commit's checks and
statuses, which the checks line names when they fail, in `GitHub.RepoStatus`'s one query
(the list's batches read the rollup's state alone), origin's refs with one `ls-remote`,
and the clone with `gitread`), and the pure `Decide` works out the sheet's `Offer`: every
check, every bump's version over the clone's and origin's tags, the Major gate, and the
argv, `push=true` (never the bare word) plus `expect=`, `json=true`, and `ff-only` only for a
magex whose `Definition.Features` has them. The bound `Prepare` takes the sheet's choice and,
within `FactsFor`, decides again on the facts the sheet was shown, so a choice asks GitHub
nothing; the bound `Start` hands `scriptrun` a release whose `Before`, under the clone's lock,
gathers the facts once (origin and the clone afresh, GitHub's from the sheet while under
`FactsFor` old) and decides there what magex runs, so a release that isn't ready, whose
version moved, or that the sheet should never have sent (a major without its typed version,
its go.mod box, or a magex with `expect=`, logged as a bug) never runs. The run's
`OutputReader` turns magex's lines into steps, warnings, and unrecognized lines (the run's
result leaves out the warning every release has, that the newest version tag isn't on HEAD;
the log keeps it, and the window's log view leaves out magex's JSON result line), and its
`Finish` verifies the release whatever magex's exit status: `ls-remote` for the tag, peeled to
its commit, then the clone's own tag, its branch, and any rebase in progress, into a `Result`
(released, not pushed, failed with `ExplainFailure`'s heading, or unverified). Only this
run's tag succeeds: origin's must point at the commit magex's JSON result names, or the
clone's tag that magex said it made; any other is someone else's (`Taken`). The commands it
offers to copy are shell-quoted. `release.Verifier`, which core registers for releases,
verifies one left from before a restart the same way. The reader's `Progress` turns Stop off
from magex's switch to the branch until its pull ends, and asks first once the tag exists; a
release that reaches GitHub is told once its outcome is recorded (the run's `Recorded`),
through its `Notifier` (core: the notify `Dispatcher`'s `Notify`, a `release_done`
notification about no item, and its banner). A release that ends doesn't put the page's
whole list out of date: its `Finish` hands the repository to the `Refresher` and emits
`event.ReleasesUpdated` at once, and the list reads as `Refreshing` until the patch lands.
The `Refresher` reads the repository alone again (its status, a comparison only when its
newest version isn't on its newest commit, and its clones' state, telling the window how
far it has got with `event.ReleasesProgress`), once any refresh under way has landed (one
read of the list at a time), patches those rows into the list, however old, which stays as
current as it was and is kept for the next start, and emits `event.ReleasesUpdated` again.
A refresh that began after the release ended has read the repository already, so the patch
asks GitHub nothing. A list it can't patch (none yet, other locations', out of date,
kept from a past start, one whose last refresh failed, without that repository, or GitHub
failing) goes out of date instead, as it does when the `Refresher` isn't running or stops
with a patch still queued, and a refresh under way when a list goes out of date lands out
of date too. A merge in the review sheet is patched in the same way: the review service
tells its `Merges` (core wires the `Refresher`'s `Merged`) the repository a pull request
merged into, and the `Refresher` reads it again, but only while the list could be patched
then, so a merge never puts the list out of date. The `ChecksFollower`, a desktop
worker as often as Settings → Refreshing → **Release checks** says (`refresh.releaseChecks`,
45 seconds by default, 0 off, woken on `event.IntervalsChanged`), reads again the rows
whose default branch's newest commit's checks run (`PENDING` or `EXPECTED`), of no
repository muted now: at most `followChecksMax` (15, one status batch and at most one
comparison batch, about two points) a round, the longest unread first, each left alone
while read within `followChecksFresh` (25 s, by a refresh, a patch, or the round before,
`Service.readAt`), and none whose newest commit is older than `followChecksFor` (2 h), so
a check never reported can't spend the budget for good. It asks GitHub nothing while none
runs, and follows only a list it could patch (`patchable`: read from GitHub in this run,
from the locations as set now, current, its last read not failed), after the read of the
list under way (`startReading`). It reads each row as a patch does (`readRows`: status,
comparison, and clone state, so a push the checks run for shows too), but quietly: no
`event.ReleasesProgress`, never `Refreshing`, and only a row that changed keeps the list
and emits `event.ReleasesUpdated`. A round GitHub fails is logged and leaves the list
current, never out of date; the next round tries again. The bound
`ReleaseResult` reads a run's outcome back as a typed `Result`, and the bound
`OfferAfterMerge` answers the review sheet's offer after a merge from local state alone: the
`integrations.offerAfterMerge` setting, magex, the default branch the Release page or the
inbox already holds, and a clone with that origin, from the page's list or else only the
folders inside the clone root and each folder location named as the repository is, then
each clone location (one git command each), never a scan of a location.
The `Follower`, a deferred desktop worker, follows each release on GitHub to *published*: a
release's `Finish` (its `Verifier`'s too) starts a `Follow` in its `Result` (its state,
`until`, and `polls`) when the Service has `Workflows` and `Records`, and its `Recorded` hands
it to the `Follower` once the outcome is kept. It polls a minute after a follow starts or
resumes, then every three minutes, at most `FollowPolls` (20) over `FollowFor` (45 minutes),
on the real clock for its limits and its waits alike (`clock.Sleep`; tests run it in a
`testing/synctest` bubble). Each poll is `(*github.Client).FetchReleaseProgress` (core's
`ReleaseWorkflow`): the tag's release, one GraphQL query through `postGraphQL`, and, until
it is published, the tag's own workflow runs, one REST request through `do` (`GET
.../actions/runs` by the push event, the tag as the run's branch, and the tag's commit, each
checked again on the answer), so the budget counts both. *Published* (`publishedAt` set, not
a draft) wins; while one of the tag's runs goes on, the follow goes on; once they all ended,
one that failed, timed out, or never started ends it as failed, named by its workflow; all
of them ending well two polls in a row with no release published ends it as
`unpublished`, and no run of the tag's by the third poll as `noWorkflow`. Each poll that
leaves the follow going is written into the run's record with `scriptrun.Engine.Amend` (an
ended run's summary and outcome, in one transaction with its read, and only while the
record still says this follow goes on). An end is settled the same way, written again a
few times a moment apart without asking GitHub again, and, if it still can't be, logged
and let go of for the next start's resume. *Published* (`Notifier.Published`,
`release_published`, opening the release) and *failed* (`Notifier.WorkflowFailed`,
`release_failed`, opening the run, or else the tag's Actions runs) are told only once the
record holds them, with a context the quit doesn't end; `unpublished`, `noWorkflow`, and
the limits get the result's line alone, no banner. As it starts, it resumes every release
whose record says it follows (`Engine.Ended`, which reads only a kind's ended runs), within
the same limits; a quit leaves the follow recorded for the next start.

## internal/scripts

`internal/scripts/` is Repo scripts' service. While the Scripts integration is off (its own
switch, whose automatic is on while mage-x or go-pre-commit is), `List` returns the page
marked off and `Run` and Run again refuse. Otherwise `List` reads a clone's sources in the
page's order (the user's own scripts, then `t3.json`'s `scripts`, the
subset of `.vscode/tasks.json` the app can run, read through `jsonc.go`, and magex's
targets), as the `scripts.repoFiles` and `scripts.magexTargets` switches in
`integrationprefs` turn them on, and runs nothing in a clone: the files are read, magex's
built-ins come from `magex help:commands json=true` run in a scratch folder of the app's
own (a clone's `.github/env` files would override magex's environment, `PATH` included),
kept per copy, a list that failed too, until another copy or `List`'s refresh, and the
repository's own targets from parsing `magefiles/` with `go/parser` by magex's own rules,
kept until a file there changes. An entry the app can't run is listed greyed with the
reason in words: an editor variable such as `${file}`, `${command:…}` or `${config:…}`, a
password input, another task type, a character that hides what runs (a control character
but tab, DEL, or a bidirectional text control, in any command, argument, folder,
variable, shell, or input value), a task shell outside the POSIX family (sh, bash, zsh,
dash, ksh, mksh) given arguments to quote, a process task setting variables for a program
whose path holds an `=`, magex's own updaters (`update`, `update:*`, `version:update`, and
their aliases: Settings → Integrations updates magex), and the custom targets while
`magefiles/` holds a link to a folder. A pickString option shows its value beside a label
that differs (`Option.Shown`). A command that tags a version (`version:bump`,
`version:tag`, `git:tag`, `git:tagupdate`, `git:tagremove`, or a magex alias of one) points
to Release (`Anyway` runs it), checked again on the command as it will run; goreleaser's
`release:*` targets are plain built-ins. A command from the repository's files asks before
its first run and whenever it changes (D13): its trust is the SHA-256 of the canonical
JSON of its effective form, `${workspaceFolder}` and `${env:…}` as written, a custom magex
target's holding every file under `magefiles/` by its bytes (only those: not `go.mod` or the
packages it imports, the known limit), recorded in `script_trust` under the clone's origin
with the display text trusted; magex's built-ins need none. `Run` hands `scriptrun` a
`KindScript` run whose `Before`, under the clone's lock, reads the source again from its
bytes, refuses a command whose hash isn't the one worked out as the run was asked for,
works the command out, refuses a version bump, then one that isn't the one the user
confirmed (or, with no confirmation, the one they trusted), and only then remembers a
confirmation they asked to keep, so what runs is what they saw. A refusal that asks is a
`QuestionError` whose `Question` holds the command with the chosen inputs filled in;
`desktop.MarshalError` sends any error's `Detail` to the window beside its message. A
repository's shell command (a `t3.json` script or a shell task, which assume POSIX quoting,
and which the app quotes for one) runs as `[$SHELL, -c, command]` when the login shell is
of the POSIX family (sh, bash, zsh, dash, ksh, mksh), else under `/bin/sh` (a task's
`options.shell` as written, with `-c` after its arguments as VS Code adds it), a process as
its argv, a magex target as `[magex, target, key=value…]`
with magex's `RunEnv`, and a `dependsOn` chain, or a part in another folder, as one `/bin/sh
-c` line of shell-quoted parts joined by `&&`. Its `Rerunner` works Run again out from the
run's intent and asks nothing, so it runs a repository command only while a trust record
under the clone's current origin matches it; anything else is run from the page, which asks.
The user's own scripts live in `user_scripts`, which it owns, in their order (positions
0, 1, 2, numbered again in the `store.InTx` of every create, delete, duplicate, or move).
The bound `Save` creates one (ID 0, at the end) or edits one, checking that the name and
command are there, on one line, with no character that hides what runs, that the scope
(`all`, `go`, or `repos`), the place (an `integrationprefs.RunsIn`, `''` for "As set in
Settings"; `validPlace` checks it and a run request's alike), and the icon (t3.json's
five) are known, and that a `repos` scope names at least one owner/name, kept once in any
case; `Delete`, `Duplicate` ("<name> copy", right after the original), and `Move` (up or
down past the nearest script shown in the clone named, so the move shows on that page)
change the order, and `RepoNames` lists the clones' GitHub repositories for the editor's
Chosen list and, through core, the launch defaults' targets (the Pinned and muted field
and Add defaults), reading origins four at a time and keeping each until the clone's
`.git/config` changes, forgetting a clone no longer listed, and starting no more reads
once the window cancels the call. `List` shows the ones whose scope includes the clone:
every clone, one with a `go.mod` at its root, or its origin among the chosen
repositories, in any case. They need no trust (D13), keep the hidden character guard,
point a version bump to Release, and run exactly as written, no `${…}` filled in, as
`[$SHELL, -c, command]` in the user's login shell whatever its family (fish and tcsh
included, since the line is theirs and the app quotes none of it; `/bin/sh` only for a
login shell that isn't an absolute path), in the clone, with the run env plus
`Variables()`, which the page also carries (`Page.Variables`) for the editor:
`<SLUG>_REPO`, `<SLUG>_REPO_DIR`, and `<SLUG>_DEFAULT_BRANCH`, named from `brand.Slug`,
set alone in the terminal. The default branch, and so the page's *From branch* note,
is the one the app stored, else the one the clone's `refs/remotes/origin/HEAD` names
(gitread's `OriginHead`, which never asks origin), else `main` or `master` when the clone
is on one, else ''. Their own place beats the setting, and a request's place beats both.
A run's `Before` compares the SHA-256 of what the saved script runs (its name, command,
place, and Ask before running) with the one worked out as the run was asked for, and
refuses one saved again in between ("changed as it was starting"). Run again works the
command out afresh from the saved script by its ID and refuses one deleted, no longer
shown there, or set to ask before running (`ErrAsksFirst`), which only the page asks:
Ask before running is the window's confirmation, which `Run` leaves to the user.
The bound `BranchStatus` tells the header beside the branch where the clone's branch
stands against the branch it follows on origin (`BranchState`: up to date, behind, ahead,
diverged, detached, no upstream on origin, git in the middle of something, unknown,
refused, not asked, or off while the Scripts integration is, which reads nothing, with a
worded note), read through its `BranchGit` (gitread) from the clone's own refs and one
`RemoteBranch` ls-remote of the followed branch alone (`ls-remote --heads origin
refs/heads/<branch>`, never every branch and tag), never a fetch, and only for an origin
on GitHub unless refresh is set (the user's click): another host's origin is
`BranchNotAsked` until the user checks, then served from the kept answer; a clone the pull would
refuse for its own config (gitread's `CloneConfig`, read locally, before origin is asked)
is `BranchRefused`; every note built from a gitread failure goes through `gitNote`
(`uierr.New` of gitread's words, read with `uierr.Message`); origin's answer is kept per
clone and branch for `OriginKept` (five minutes) unless refreshed, the clone itself is
read on every call, and an origin commit the clone lacks counts as behind. Only behind is
offered **Pull**, and a refused pull's error is the whole sentence the window shows (it
names the branch), a run whose record can't be read letting go of the button. The bound `Pull`
(`pull.go`) returns a `scriptrun.KindPull` run's ID (thirty minutes' limit), in the app,
under the clone's lock, of the branch the last status was worked out on, an upstream
`gitread.ValidBranch` passes: its `Before` refuses a clone now on another branch, one
whose branch follows none on origin, and one git is in the middle of something in, and
works out with `gitread.RemoteSetup`, asking origin nothing (the pull's own fetch is the
one round trip), what git runs with: a clone whose own config holds a setting gitread
refuses is refused, naming the settings; otherwise gitread's own git, an ls-remote's
environment (`GIT_TERMINAL_PROMPT=0`, the user's transports in `GIT_ALLOW_PROTOCOL`, the
user's ssh command or a batch-mode one, and `GIT_NO_LAZY_FETCH=1` but in a partial clone,
whose promisor remotes are weighed too), `GIT_DIR` and `GIT_WORK_TREE`, and every setting
(`gitread.Hardening`, no alternate refs command, no automatic maintenance, the clone's own
password program and credential helpers set aside, no cookie saved) passed through
`GIT_CONFIG_COUNT`. Its intent (`PullIntent`) is recorded once `Before` ran, with the
upstream and the branch's commit it read. It
runs `git pull --ff-only --no-rebase --refmap= --no-tags --no-autostash
--no-recurse-submodules --no-verify-signatures --no-stat --upload-pack=git-upload-pack
origin +refs/heads/<b>:refs/remotes/origin/<b>` as argv, nothing else in it, with
`LC_ALL=C` so git speaks the window's language, the environment handed to the runner
through the spec's `BeforeEnv`: the fetch updates that one remote-tracking ref, never one the
clone's own `remote.origin.fetch` maps. Stop is held back (`StopRefused`) for the whole
run, since git writes its output only as it exits, and so is a quit, which leaves it going
for the next start; only the kind's limit cuts it off, and a pull it cuts off is read
again (`cutOff`: the branch it is on, its commit against the one before, whether
`index.lock` is left, through gitread's `IndexLocked`, and what git is in the middle of)
and said to be cut off partway, the clone perhaps partly updated, with `git status` in a
terminal to see where it stands. Its `Finish` checks the clone is still on the
branch (a switch while git ran fails the run, saying git may have pulled into the other),
reads the branch back and words the summary (updated from one commit to another, how many
commits, already up to date, or git's last error line, with any of the clone's own
settings set aside, which may be why), and
keeps the commit git fetched as origin's for the next status, leaving when origin was
asked as it was, or forgets origin's answer when that can't be told. A pull whose end wasn't seen is settled from the branch alone:
moved on from the commit `Before` read is pulled. Core registers the `Verifier`
(`scripts.NewVerifier`) for pulls, so one left going at a quit comes back after a
restart with Stop held again and that same `Finish`. Run again pulls the same branch, its
`Before` checking everything afresh.

## internal/inbox

These notes cover what the inbox does for Autopilot: the routines' searches, which each
refresh reads after the inbox's own three lists, and the reads Autopilot makes through it.
The inbox's own refresh, triage, edits, and the review sheet's Actor are described in the
package's doc comments.

**The searches, after the refresh's store.** `Config.Watches` (`WatchSource`) gives the
searches of the enabled routines of the account signed in, each key once (none while Autopilot
(beta) is off, when no copy holds the watches either: `WatchHolder`), from the snapshot
Autopilot's service keeps (core's `autopilotWatches`, which builds it again first when the
account changed). `runRefresh` hands the watch pass's worker (`WatchPasses`, a desktop worker
the shell starts before the scheduler) a job as the refresh starts, and the pass waits, on the
worker's goroutine and outside the refresh's `errgroup`, until the refresh has stored the
inbox and told the window (the job's release); only then do its requests go out, so they
never share GitHub's time with the inbox query (beside it, the heavier searches cost the
inbox's store about 0.3 s, measured in November 2026), and a search that fails, or a request
that does, never fails the refresh: the refresh stores and returns on the inbox's own
requests, and never waits for the pass. One pass runs at a time: a refresh that finds one
running hands over no job (the scheduler never skips a tick; `periodic.RunEvery` waits after
each refresh). No job is handed over before an account is known, nor, once the searches' rows
are gone, while no routine has a search. While the GraphQL budget the inbox query just left is
under a quarter of its size (and won't refill first), the pass sends no request and only
records `PausedLowBudget`, keeping every row. A pass that fails is logged, and one that
panics is recovered; either way its `WatchPass` is kept and `AwaitPass` told (`endPass`, in a
`defer`).

**The request.** `github.FetchWatches` packs the searches into requests by how many results
each returned last time (`inbox.watches` keeps each search's last count; a new one counts as a
full page), up to about 120 results and ten searches a request, at most four requests side by
side, a point each. GitHub resolves a request's aliases one after another and answers one that
runs past about 10 seconds with a 502 and an HTML body: measured in November 2026, five searches
of 295 results in one request failed that way every time. A request that fails, as with that
502 or with errors and no data (`EXCESSIVE_PAGINATION`), fails each of its searches; an error
whose path stops at one alias or its `nodes` fails that search alone; one whose path goes on
to a result (`["w0", "nodes", 3, ...]`) leaves that result out and marks the search `Partial`;
one with no path marks every search of its request `Partial`, keeping the data. A query that
can't be written into a request (not UTF-8) fails its search before anything is sent, and the
others go on. Only a refused token fails the call, so `WithToken` borrows a fresh one. The searches read a lean fragment (`WatchFields`): what the
inbox stores of an item (its own fields, its state, when it closed), its head commit, base
branch, and the checks' overall state (finished checks don't move `updatedAt`, so the state
must be in the search), and its repository's node ID, owner, name, and archived flag.
`mergeable`, `mergeStateStatus`, `reviewDecision`, and the repository's other fields cost
GitHub time per result, so they come with the details, read by ID.

**The pass.** Once the inbox is stored and the requests answered, `planPass` gathers the open
results (a closed or merged one, which the index can trail, only records its state, and takes
the item out of the open results another search returned; an archived repository's is left
out), each node once with the searches that returned it and whether any follows the checks,
and compares each with what is stored (`ListWatchBaseline`, a read, no lock). A listed item
has its details from the refresh, and only its rows are stored; one a later read stored (its
`fetched_at` or its details' after the pass's request went out) is left as that read stored
it, and keeps no row when that read left it closed; one stored merged keeps no row, since
nothing comes back from a merge and the search's open is older; any other is stored, and its
details read when `needsDetails` says: no details; another update time, draft flag, or state;
a pull request's other head commit, base branch, or checks' state, checks still running at the
last read, or a merge state GitHub hadn't worked out; or, new to the watches, details missing
or older than `detailsStaleAfter`. Details at least `detailsStaleAfter` (30 minutes) old, and
nothing else, are read as room allows (`fillStale`, oldest first): in the room the moved items
leave in the last request, or one request when none moved, so they never add more than one,
and with some 300 watched items each comes round about every 20 passes. The details are read
with the repository whole (`DetailOptions.Repository`, the `DetailRepo` fragment), with no
lock, `watchDetailsBatch` (15) to a request (`DetailOptions.Batch`), the requests that
succeeded kept when one fails (`DetailOptions.Partial`): GitHub reads an item it hasn't lately
more slowly, and four requests of 25 newly watched pull requests side by side ran past its
10-second limit (measured in November 2026), while of 15 none did.

`storePass` then writes, in one transaction under `order.mu`. It first reads again what is
stored of every result (`recheck`, `ListWatchBaseline` inside the transaction), since an
item's own read, the follower, a whole refresh, or another service's state (`NoteStates`) may
have stored it while the pass read GitHub: an item whose card, details, state, or lists
changed since the pass compared it, or that was stored after its request went out, is newer,
and keeps what that read stored, only its rows written while it is open; one stored merged,
or closed by such a read, or, for a pass, whose rows went since (an unlist takes a closed or
gone item out of every search; a test's results have no rows, which a pass may drop for any
reason), is gone: its rows are deleted, never written, and it isn't told as changed; one
deleted since is stored again as new. Then each item (`UpsertWatchedItem`, which never
touches the reason flags or the times an item arrived in a list, and never writes a state over
`MERGED`; its repository through `EnsureRepo`, which keeps what is stored, or whole from the
details), its details, its rows (`UpsertWatch`), and each search's state. A stored item whose
details the pass meant to read, or whose card moved, but whose details didn't come back,
keeps its card, so its old update time makes the next pass read it again, and isn't told as
changed; a new one is stored without them, told once a pass reads them. A search answered in
full replaces its rows (`DeleteUnseenWatches`), a truncated or `Partial` one keeps the rows it
didn't return, stale, for `staleTailFor` (a week), and one that failed marks every row stale
(`MarkSearchStale`); the rows of a search no routine has are deleted (`DeleteWatchesNotIn`).
The closed results' state is recorded through `SetRefStateBefore`, read before the pass's
request went out, so an item stored since keeps the newer read. Under the same lock, the pass
records in the write order the number its fetch took for every item whose card or state it
wrote (`writeOrder.stated`, through `orderStored`): an item's own read or a follower round
that went out before the pass and lands after it is superseded, as by a whole refresh, and
stores nothing over them. A refresh still fetching that started before the pass's request
applies again the items whose card and details the pass stored, and the states it wrote (a
`stateWrite` each, through `SetItemState`), on top of its own (the replays of `order.go`).
`stated` drops what is no later than the whole refresh before the last, so it stays as small
as what was stored lately. Then `Config.Changed` hears the items stored as changed,
outside every lock, which core turns into the Go-only `event.InboxItemsChanged`; the
follower's rounds, an item's own read, and an action on it tell it too. Each pass's
`WatchPass` (its time in all and after the inbox's store, its requests and points, its results
and details read) is logged at Debug and kept for `Routines.LastWatchPass` and
`Routines.AwaitPass`, which the refresh measurement waits on.

**What keeps watched items out of the inbox.** Every query that reads the three reason flags
is decided in `reasonFlagDecisions` (`flags_test.go`), which fails on a query it doesn't list:
the lists, the snapshot, and the arrivals read listed items alone, so a watched item is in no
list, no count, and not the menu bar's; `PruneFiles` keeps a watched item's file check;
`ListRepoCounts` counts watched items, so a repository the user only watches can have launch
defaults; `ListRunningChecks` adds the watched pull requests a search follows
(`item_watches.follow`, set for routines whose trigger reads checks), but only while this copy
reads the routines' searches (`watched`, `holdsWatches`: so in none while Autopilot (beta) is
off, when what the searches stored is stale), and `followable` leaves
out a muted repository's only for a listed one no search follows, since a routine watches a
muted repository on purpose, and takes the listed ones before those only a search follows
when a round holds more than `followMax` (50); a round reads them `followBatch` (25) to a
request, side by side, storing what the answered requests read when another fails, since
GitHub works out each one's merge state as it answers; `GetEditItem` says whether an item is
watched, so the Actor acts on a watched item's sheet as on a listed one's, while the review
and issue sheets' reads take any stored item.

**State, arrivals, and the prune.** `items.state` and `closed_at` are written by every read
that carries them: the inbox's searches (`UpsertItem`), an item's own read and the follower's
(`UpdateItem`, `UnlistItem`, which also drops a closed or gone item's watch rows), the closed
items a refresh's searches returned (`SetRefState`), the routines' searches and a test's
(`SetRefStateBefore` for their closed results), and the states the worktree sweep and the
notifications mirror read (`Store.NoteStates`, through core's `itemStates` and `stateNoter`).
Every one of them stamps `fetched_at`, and none writes over a `MERGED`, which is final: not
`UpsertItem`, `UpdateItem`, `UnlistItem`, `UpsertWatchedItem`, `SetRefState`,
`SetRefStateBefore`, or `SetItemState`. A close can open again, so it is ordered rather than
guarded: another service numbers its read in the inbox's write order as it goes out
(`Store.StartRead`, a `StateRead`; core hands the Service its Store as `Config.Store`, so the
two share one order), and `NoteStates` stores by it as a pass does: an item stored at or after
the read went out keeps that newer read (`SetRefStateBefore`), an item's own read that went
out before it stores nothing over it (`stated`), and a whole refresh still fetching that went
out before it applies the state again on top of its own. `review_requested_at` and `assigned_at`
keep when the inbox first saw an item in Review requested and in Assigned while it stays
there, cleared once it leaves (`ClearArrivalStamps`, and every query that takes it out), for
the routines' fire keys. Each whole refresh prunes, in its own transaction, the items in no
list, watched by no search, with no triage, that no read brought for 30 days (`PruneItems`);
what else refers to an item comes from `Config.Keep` (`ItemKeeper`), read before the
transaction, so a later owner of a reference, as a routine's ledger, extends the rule there.
Core hands it `inbox.Keepers` (one failing fails the whole, and then the refresh prunes
nothing, keeping everything, logs why, and goes on) over the agent sessions' items (`sessions.Store.KeptItems`: `agent_sessions.item_id` has no
foreign key, so a pruned item would leave a dangling id, or a reused one) and the
notifications' (`notify.Store.KeptItems`: `notifications.item_id` would be cleared, and the
entry would no longer open its item). A worktree record and a GitHub notification thread name
their item by repository and number, so a pruned item comes back for them with the next read.
The rows that hang off an item by its id go with it, by their foreign keys' cascade: its
details, file check, and check names, and the review sheet's `review_diffs` (with
`review_diff_files`) and the issue sheet's `issue_views`, which are only caches and are read
again on the next visit. (An item with watch rows or local state is never pruned.)

**The reads for Autopilot.** The bound Service adds only `ItemsByID`, the rows of a routine's
Watching list (listed and watched items alike, as a snapshot shows them). Everything else is
off the window: `Store.FactsRows` (one query, `ListFactsRows`, into `facts.Row`, with the
repository rules applied), `Store.FactsEnv`, `Store.WatchedItems` (a routine's lists and its
searches' rows, stale ones included), `Store.Suggest` and `KnownOwners` (the builder's
suggestions, labels and check names counted in Go from their JSON columns), `Store.WatchStatus`,
and, through `inbox.Routines`, `SearchNow` (a draft's searches read now and stored as a pass
stores them, `recheck` and the write order included, with no rows, so the prune takes them in
time; the details of an item no search watches are read only when missing, older than
`detailsStaleAfter`, or its state moved, since the passes keep the watched ones current and a
test only looks, and its card is stored all the same; nothing is sent while the budget is
under a quarter, `ErrLowBudget`) and `ReadChecks` (`github.FetchChecks` into `item_checks`,
returning what it cost, which a test adds to its points, and `ErrLowBudget` too).

**A routine's triage** (`triage_routine.go`, migration `0008_autopilot_effects`). Autopilot's
local steps run the inbox's own triage through `inbox.Routines`, never the bound Service:
`Triage` runs a Pin, a Snooze (for the routine's length, recording the item's activity as
the user's Snooze does, so the user's wake rules end it early), a Dismiss, or a Restore on
one item, marked with the routine (`item_local_state.set_aside_by` for a dismissal, a
snooze, or a routine's Restore, and `pinned_by` for a pin: two columns, since a pin is
independent of a snooze or a dismissal on the same row, each `REFERENCES
autopilot_routines ON DELETE SET NULL`). In one transaction it reads the item's local state
(`LocalState`), works out the change, hands it to the request's `Record` before writing
it, so Autopilot's effect and the change are stored together or not at all, then writes it.
Each step is a compare-and-set: `RoutineTriage.Expect` is the state the routine decided on
(`Routines.LocalStates`, which the engine reads before an item's facts), and a step whose
part of the triage (the pin, or the dismissal and the snooze) differs now changes nothing
(`KeptChanged`), nor does one whose routine's own mark, on either part, was there when it
decided and is gone now (`lostMark`: the user unpinned what the routine pinned before its
next step landed), so the user's own change in between always wins. A routine's Snooze and
Dismiss never replace what the user set aside: they leave an item the user dismissed or
snoozed (still set aside, with no routine's mark) as the user left it (`KeptYours`); its
Pin leaves a pinned item (`KeptPinned`), and its Dismiss a dismissed one (`KeptDismissed`).
Its Restore is for bringing an item back to the user's attention when the routine's
conditions hold, the user's own snooze or dismissal included, and has nothing to do on an
item not set aside (`KeptNotSetAside`); each that changes nothing records nothing. A
routine's Restore keeps its mark on the row with nothing set aside, which `PruneState` then
keeps, so its Undo can tell the item is still as it left it, until the user's own triage of
the item clears it, or a whole refresh drops it once it is older than Autopilot's history
(`PruneRestoreMarks`, `autopilot.historyDays`, read through `autopilotprefs`), unless
Autopilot still offers that Restore's Undo (`Config.Marks`, a `MarkKeeper`: core's
`engineWaker`, answered by `Engine.KeptMarks` from the effects neither undone nor taken
back, read before the refresh's transaction; one that can't be read drops no mark); it tells the
outcome sink of the restore, as the user's does, so the mirror of the GitHub notifications
takes a dismissal's clears back, and no other routine step tells anything. `UndoTriage`
puts back, in one transaction and newest first, each change's state before, only where the
item is still as the change left it (`asLeftBy`: the pin and who pinned it, or the
dismissal, the snooze's end, and who set them, so the user's own triage, a snooze the wake
rules ended, or a later step leaves it as it is), and hands its `record` which it put back;
a routine named in a state put back that is gone for good is dropped from it. Undoing a
routine's Restore of the user's own dismissal tells the outcome sink of that dismissal again,
at its own time, so the mirror records it as the user's and clears by the user's own rules,
as before the Restore; no other Undo tells anything. Two edges of that, accepted: a clear the
mirror marked kept while the routine's Restore stood stays kept after the Undo, so it sends
less, never more; and a dismissal whose routine was since deleted for good reads as the
user's (its mark is gone), so undoing a later Restore of it lets the user's own rules clear
that thread, only ever after the user's own Undo. `AsLeft` asks the same without writing,
for Activity's Undo. The snapshot carries the marks (`Item.SetAsideBy`, `Item.PinnedBy`).
Every triage path of the user's own clears the mark it changes (`SetTriage`, `Pin`, `Unpin`,
and `Restore` all write it), so "Snoozed by a routine" never outlives the user's own
re-snooze, takes its time once its transaction holds the write lock, so a reversal's time
comes after every routine's step that committed before it, and tells the outcome sink before
the window and the engine hear the inbox changed; one that takes back what a routine did
tells the outcome sink with
`Outcome.Reversed` (the routine, the item, and what it did): a Restore of what a routine
dismissed or snoozed, still in effect; a Snooze over that (`OutcomeSnoozed`, told only then);
and an Unpin of what a routine pinned (`OutcomeUnpinned`, only then). Setting aside again
what a routine's Restore brought back is the user handling the item, never a reversal, and
going further than a routine (a dismissal over its snooze, the user's pin over its pin)
takes nothing back either, nor does snoozing again what a routine snoozed once that snooze
has ended.

**Check names.** `github.FetchChecks` reads each pull request's checks on its head commit, a
fragment of its own (`CheckFields`) modeled on the review sheet's `checkList` selection, with
each run's `startedAt` and `completedAt` and each status's `createdAt`, under one alias per
pull request (`node(id:)`) in batches of 25, four side by side, a point each: whether a check
is required takes the pull request's own ID (`isRequired(pullRequestId:)`), which a fragment
can't take, so each alias asks for it beside the fragment, over the same contexts in the same
order. A name repeats, once per event that ran it; `item_checks` stores every run, and
`facts.Build` dedupes them. `HeadChecks.Settled` is the newest completion once every check has
finished, stored as `settled_at`, by the facts' own rule (`HeadCheck.Running`): a check runs
while it is pending, or has no completion time and wasn't skipped. A head with more than a
page (100) of checks is read in part (`Complete` false): its names are partial, so a negation
over them is unknown, and it settles once every check read has finished and the rollup over
all of them (`HeadChecks.Rollup`, and the details' checks state for the facts) says the rest
have too, at the newest time read.

**The budget's purposes.** Each request's context names what it was sent for
(`github.WithPurpose`): the refresh is `PurposeInbox`, the watch pass `PurposeWatches`, the
checks follower `PurposeFollower`, and `SearchNow` and `ReadChecks` `PurposeAutopilot`;
anything else is `PurposeOther`. The client hands the purpose to the `Tracker` with each
reading (`RateReading.Purpose`, which the tracker strips before it keeps the reading), and the
tracker splits each budget's spend by it (`Reading.Purposes`, kept across runs, a state saved
before the split, and a purpose this build doesn't know, counting as the rest), so the shares
add up to `Spent`. The budget menu's one line for Autopilot is `watches` and `autopilot`
together, shown only while they are above zero; the follower's rounds, which read the watched
pull requests a routine follows in the listed ones' requests, stay `follower`.

## internal/autopilot

`internal/autopilot/` is Autopilot, which watches the user's items and runs their routines.
Its root package keeps the routines and serves them to the window (**The routines, kept**,
below); beneath it is the routines core, five pure leaves. They read nothing,
start nothing, and keep no clock: the moment a decision is for comes with it. They import
only `item`, `repo`, `trust`, `sensitive`, `bump`, `clock`, `textutil`, the standard
library, and one another, in this order: `catalog` and `watch` at the bottom, `spec` over
both, `facts` over `catalog`, and `eval` over all three. `TestLeafImports` in
`internal/repoguard/deps_test.go` holds each leaf's direct imports to that list, so a leaf
can never reach the store, GitHub, a process, or a service.

**`catalog`** is the vocabulary: every name a spec stores is one of its constants, and the
builder renders what it lists. Its parts:

- the triggers (`Triggers`: each one's event, label, the kinds of item it fires for, what
  one occurrence is, and its options with their ranges and defaults);
- the fields (`Fields`: `<group>.<name>` IDs in seven groups, item, repo, pr, checks,
  files, dep, and history, each with its type, the kinds it applies to, its choices, how
  a list's patterns match, globs or `.gitignore`'s, its read cost, and its words for the
  sentence);
- the operators per type (`Ops`, with arity and negation; a list of choices takes only the
  set operators, `Takes`);
- the steps (`Steps`: each with the `Dispatcher` that runs it, triage, notices, github,
  agent, verify, merging, or review, whether it runs at once (`Local`) or follows the
  routine's mode (`AsksFirst`), and `Offered`, false until its dispatcher is built);
- the Only if outcomes, the tools presets (`MayPush`, `ReadOnly`), the agent step's
  settings (a budget in tokens among them, `budgetTokens`), the other steps' whole-number
  settings (`StepSettings`: `snooze.days`, `waitForChecks.settleMinutes`, and
  `waitForChecks.giveUpHours`), the limits, and their ranges, which the builder reads its
  bounds and defaults from rather than keeping its own.

`GitHubActions` lists the GitHub step's four actions (rebase, updateBranch, reply, labels),
each offered, with how far it can be taken back (`GitHubActionByKind` reads one); Mark the
GitHub notification done is a local step of its own. The catalog has no entry
for a close, approving workflow runs, `merge_bypass`, or the sheet's combined approve and
merge: `TestCatalogHasNoForbiddenAction` checks every ID it lists, and
`TestNoAutopilotCodeSpellsAForbiddenAction` every string literal the autopilot packages
spell. Merging and posting an agent's review are steps of their own, held in by their own
checks. Lookups by ID (`FieldByID`, `OpByID`, `TriggerByEvent`, ...) read one index built
once, since the evaluator makes them for every condition on every item. Three of the
catalog's names (Restore, Merge it and the merge method, the rebase action and method) are
spelled as git verbs; `TestNoGitCommandChangesAClone` lets their declarations in
`catalog/steps.go` through, and nothing else.

**`watch`** turns a search, as the builder writes it from fields (kind, authors, where,
involving, labels, notLabels, base, headPrefix, state, and qualifiers typed by hand), into
the GitHub query a refresh sends. Every query adds `is:open archived:false
sort:updated-desc` (without the sort, GitHub orders newest-created first, so "the 100 most
recently updated" would not hold); is scoped, by a `user:`, `org:`, or `repo:` qualifier
that isn't negated, or one whose value is `@me`; quotes its labels, as one comma list each
for any of them (`label:"a","b"`) and for none of them (`-label:"a","b"`), refusing a label
holding a comma; quotes any value holding a space of any kind, Unicode's included; passes
the head branch's prefix through exactly, quoted, since GitHub reads `head:` as a plain
prefix, refusing in a branch any space, since the base goes in unquoted; writes a bot as
`author:app/<slug>` (the bare slug matches nothing, so
`Normalize` writes `dependabot`, `renovate`, and `github-actions` as apps and `x[bot]` as
`app/x`); refuses a hand-typed qualifier it doesn't list, free text, and the
search's own (`is:` but public, private, locked, or unlocked, `sort:`, `archived:`, `in:`);
and is at
most 256 characters, refused over that with the hint to split it. GitHub doesn't error on
a longer query, it answers nothing, which is why the cap holds the whole query. "Your
account" (`@me` in Where) becomes `user:<login>` in `Build`, so a routine follows
whichever account is signed in, while `Check`, which `spec.Validate` calls, builds with the
longest login GitHub allows, so a search it passes fits any account. The query is written
in one canonical order from the normalized search, so it is its own dedup key
(`Query.Key`).

No value can smuggle in a qualifier. GitHub's search has no escape for a quote inside
quotes, so no value of any field (a label, an author, a place, a branch, a hand-typed
qualifier's value) may hold what `unsearchable` names: a quote, a backslash (which a
reader could take for an escape of the closing quote, letting what follows go live), the
fullwidth quote and backslash (which a reader folding Unicode's compatibility forms reads
as those two), a control character (a newline and a tab among them), or a line or
paragraph separator. An apostrophe is fine, since GitHub quotes only with double quotes.
Each field refuses such a value with its own problem (`labels[1]`, `base`, `more`, ...),
and so does the build itself: every token carries the field it came from, and `build`
checks each one again as it writes the query (`token.check`), so a check a field missed
fails the query rather than sending it. The writer (`token.String`) also takes the quotes
and backslashes out of every value it writes (`quoteless`), whoever calls it, so not even a
token that skipped both checks can end its quotes early. `FuzzTokenString` reads what it
writes as GitHub's documented syntax does, and as a reader that takes a backslash for an
escape would, and finds one qualifier, the token's own, between the query's neighbors;
`TestHostileValuesAddNoQualifier` does the same for each field through `Build`.

**`spec`** is a routine's definition, one JSON document with a `version`: the watch's
sources (inbox lists, or a search), one trigger with its options, the conditions (`all` or
`any`, with one level of groups), the steps in order, each optionally carrying `onlyIf`,
the limits, the routine's own banner switches, and the tools it requires. The mode, the
on or off switch, the name, and the account are its row's, never the spec's. An option or
a limit left out takes its default (a nil pointer), so the builder can tell a value set
from one inherited. Format version 1 holds every step kind (notify, pin, snooze, dismiss,
restore, markDone, github, agent, waitForChecks, merge, postReview, tellMe), so a later
build adds the code that runs a step, never a format change; `TestSchemaGolden` pins every
key and every closed set of values.

- `Decode` is strict: it walks the document beside the `Spec` type and holds every
  object's keys, byte for byte, to its struct's JSON names, since encoding/json would
  match a key ignoring case, Unicode's folding included (`Trigger` beside `trigger`, or
  `ſteps` for `steps`); and it refuses a key given twice (which encoding/json would read
  as the last), a value of the wrong type, anything after the document, nesting past 16
  levels, and more than 64 KiB.
- `Normalize` trims every text, sorts and dedupes every list that is a set (text compared
  ignoring case, the first spelling kept), writes a choice as the catalog spells it, keeps
  a condition written the same way twice in one list once (its value compared exactly, so
  the regexes `\d` and `\D` stay two), and keeps the user's order of the sources, the
  conditions, the steps, a GitHub step's actions, and the fallbacks.
- `Validate` returns every problem, each naming its part and field (`Problem.Path`:
  `watch.sources[0].search.where`), and holds the steps to their order: one agent step at
  most; Wait for the checks only after an agent step in the background that may push, and
  never only if it didn't push; Merge it only after a Wait for the checks that runs only if
  the agent pushed, so the pass it follows is on the agent's own commit and never a flaky
  check run again, with `onlyIf: "passed"`; Post the agent's
  review only after a read-only agent step in the background; Allow approving only with
  `as: "verdict"`; only a notice after an agent step in the terminal, whose end the app
  can't see; and an Only if only on an outcome an earlier step ends with (the checks', the
  latest Wait for the checks'; the push and the verdict, the agent step's).
- `Load` reads the version first, so a newer build's spec says it is newer rather than
  unreadable, then decodes, normalizes, and validates. A spec it can't load returns a
  `*BrokenError` (`ReasonNewer`, `ReasonUnreadable`, `ReasonInvalid`), so its routine is
  broken, not fatal; an older version is migrated in `migrate`, which has nothing to bring
  forward yet. A routine stored before a rule tightened (a label holding a fullwidth
  quote, say, which `watch` refuses) loads this way, broken as invalid, its problem
  naming the field for the builder to show, its spec never rewritten, and no query built
  from it.
- `Sentence` writes the one sentence every place shows, from `Phrase` for each condition,
  with `PromptNames` naming a starter as the library does; `TestSentenceGolden` pins each
  template's. A value it shows (a label, a condition's value, a search's author, place, or
  branch) goes in bare when it is one plain word, and otherwise quoted and escaped as Go
  quotes a string (`strconv.Quote`), so a value's own quote or backslash can never
  unbalance the sentence's quotes, and a newline or a bidirectional mark shows as its
  escape. The hand-typed qualifiers go in as typed while their quotes pair up and they
  hold no backslash and nothing that doesn't print, and otherwise quoted whole.
  `FuzzSentenceQuotes` holds every quoted stretch to read back, as Go reads a quoted
  string, as the value it quotes.

**`facts`** holds one item's values for the evaluator, built by the pure `Build` from a
plain `Row`, which the inbox's query fills: the item, its repository, its triage, and, once
read, its details, its head commit's check names, and its changed files, each part nil
until it is. Each value is known, unknown (with why, and the `Need` the engine would read to
know it: details, checks, or files), or not applying to the item (a pull request's field on
an issue). A list read only in part is known and `Partial`. Check names and files count only
when read at the head commit the details name. A check's name is `workflow / job` as GitHub
shows it (an app's run, such as CodeQL's or OSV-Scanner's, and a status have no workflow),
each name once though GitHub lists one per event that ran it, failed when any of its runs on
the head commit failed; the checks are done, and so can settle, only once every one has
finished, at the newest completion time (a skipped run may say it completed before it
started, so start times never count). The dependency fields apply only to a pull request
Dependabot or Renovate opened (by its author's type and login), read from its title by
`bump.Parse` and its ecosystem from its head branch by `bump.ParseBranch`; a title that
doesn't read as an update leaves them unknown. The author's trust is unknown while their
association isn't read, since they may be a member, but for a fork's pull request, an
outside contributor's whoever opened it, and a bot's, which the trusted list decides.
Every text is made one line and capped (`textutil.OneLine`).

**`eval`** decides. `Evaluate(spec, facts, history, now)` returns a `Decision`: `Match`
(yes, no, or unknown), the `FireKey` while the trigger holds, the `Trace` (the trigger's
step, then each condition that ran, with what it compared and the actual value), and the
`Needs` of an unknown decision. Conditions are three-valued: one on a value not known yet is
unknown whatever its operator, its negation too; one on a field that doesn't apply is no;
a partial list's find is sure and its miss unknown; an "all" operator over an empty list
is no, never vacuously yes (a pull request that changes no file doesn't have every changed
path under docs/). A regex or a path pattern is compiled once, in a cache every evaluation
shares, not on every item of every cycle. `All` is no at its first no, `Any` yes
at its first yes, and a list stops there; the order conditions run in is their read cost,
then what each says, never where it sits, so shuffling them never changes a decision. The
`History` is the ledger's: the routine's tries and last outcome, the head it first saw, the
heads routines pushed, the comments it last counted, and a click's token. Fire keys name
the event and the occurrence (`checks_failed:commit:<oid>`, `labeled:label:<name>`,
`new_comments:comments:<n>`, `schedule:<date>`, `manual:<click>`, ...), and the routine's
own once-per (item, head commit, or day) replaces the trigger's. A key holds only what is
known: Falls behind waits for the base branch to be read, so one occurrence never fires
twice, without its base and then with it. Checks fail and Checks pass wait for the checks
to settle, the newest completion at least the settle time ago. The
property tests (`FuzzShuffledConditionsDecideAlike`, `FuzzNegationOfAnUnknownIsUnknown`,
`FuzzNeverYesOnAnUnknownItNeeds`) run over generated specs and facts; the last checks that
reading what was missing never turns a yes or a no into anything else.

### The routines, kept

The root package owns `autopilot_routines` (migration `0005_autopilot_routines`): one row
per routine, its spec one JSON document (`spec.Encode`, compact) beside its `spec_version`,
and, as columns, never spec, its name, description, switch (`enabled`, with `enabled_at`
while on), mode (`dryRun`, `ask`, or `auto`, checked in Go, so a later mode needs no
rebuild; one this build doesn't know reads as Dry run), the account that made it
(`account_login`, NOCASE), the template it came from, its place (`sort_order`, 0, 1, 2, and
so on, numbered again in the transaction of every change that adds, deletes, or moves one),
its last error, and the broken reason it was last written with. A delete is soft
(`deleted_at`, and a check keeps a deleted routine off), so the history that names a routine
keeps its name and sentence. Names are unique, ignoring case in any alphabet, among the
routines not deleted: `nameKey` folds a name in Go (each rune to the smallest of its
`unicode.SimpleFold` orbit, so two names share a key exactly when `strings.EqualFold` says
they match), every comparison of names uses it, and the row stores it as `name_key` under a
partial unique index, since SQLite's NOCASE folds ASCII alone; a worded check runs before the
index would refuse. Its queries are in
`internal/autopilot/internal/queries`, generated by sqlc.

**The bound Service** (`List`, `Get`, `Save`, `Delete`, `Duplicate`, `SetEnabled`, `SetMode`,
`Reorder`, `Catalog`, `RoutineNames`) never returns a nil list, and words every refusal with
`uierr`.

- Every read loads each spec with `spec.Load`: one that won't load (`spec.BrokenError`:
  newer, unreadable, or invalid) is listed as broken, with `Broken` in the window's words
  (*Made by a newer Nilbox: update to edit it*) and an invalid one's `Problems`, normalized
  so its lists are empty rather than null, and the list carries on with the rest. No read
  writes anything, and nothing rewrites a broken spec: such a routine can't be switched on
  or change its mode, a newer build's can't be saved over, and only switching it off,
  deleting, duplicating, and reordering touch its row.
- `Save` normalizes the draft's spec, encodes it, and loads it back, so the spec stored is
  checked as every load checks it; problems come back as a `ProblemsError`, whose `Detail`
  (`{"problems": [...]}`, through `desktop.MarshalError`) lets the builder show each where it
  is. A new routine starts off, at the end, in the draft's mode or else the one Settings says
  new routines start in (`Prefs`, core's adapter over `autopilotprefs`; Ask first to begin
  with), and belongs to the account signed in (`Accounts`, the inbox's viewer login). One
  saved before keeps its switch, place, and template, and its mode unless the draft names one;
  a routine of no account (made before any account signed in, or brought back from a copy or
  a backup, which name none) becomes the editor's. It also refuses, as a `ProblemsError`, a
  step naming a prompt the library doesn't have (by its starter's key, or its nickname, ignoring
  case), a review reply where an agent's prompt goes or the other way about, or a prompt that
  doesn't apply to every kind of item the routine acts on (its watch's kinds that its trigger
  fires for), each at the step's prompt field (`Library`, core's `promptLibrary` over the
  prompt library); with no library, or one that can't be read (logged), it refuses none.
  `SetEnabled`, `Duplicate`, a restore, and an import don't check them again; the engine does,
  right before each start (`promptHold`, the same `stepPromptProblems`), so a prompt deleted,
  made a reply, or narrowed to the other kind after the save keeps the run from starting
  (`blocked: not_ready`, *Its prompt can't run: ...*). A prompt a routine names can't be deleted
  (the prompt library's `RoutineSource`, core's `routinePrompts` over `PromptFollower.Uses`),
  and renaming one rewrites its nickname in every routine that names it by that nickname
  (`PromptFollower.Renamed`, told by `prompts.Config.Renamed`, in one transaction, each spec
  compared-and-set on the one read); a starter's key follows its prompt on its own.
- A step's **Where** must be a place this build runs an agent in (`catalog.Places`,
  `Offers.Places`): one it can't (the background, in this build) is a problem at
  `steps[i].agent.where` (`placeProblems`), in `Describe` and `Save` alike, never a Dry run
  hold, so a routine is never saved to wait for a later build; a routine stored before with
  such a step isn't ready on this Mac (its `place:` need, **Readiness**, below).
- `Describe(draft)` reads a draft as the builder edits it, saving nothing and asking GitHub
  nothing: the spec normalized, encoded, and loaded as `Save` does, its problems (the spec's,
  then the prompts'), each with its part and field, the steps this build runs only in Dry run,
  its warnings (a Pin, Snooze, Dismiss, or Restore over a watch that is only searches; on
  Autopilot, an agent in the terminal or with the profile's own permissions, unless the routine
  is held in Dry run), and, only while it has no problem, its sentence, so the builder keeps
  the last one that read.
- A routine whose steps include a kind the catalog doesn't offer yet (`Step.Offered`, or a
  GitHub step with an action `GitHubActions` doesn't offer) runs in Dry run alone:
  `Routine.DryRunOnly` names the kinds, `Save` stores it as `dryRun` whatever the mode, and
  `SetMode` refuses Ask first and Autopilot for it with a sentence naming the steps. As each
  dispatcher is built and its steps offered, the same routines take any mode.
- `List` marks a routine of another account than the one signed in (`OtherAccount`; logins
  compare ignoring case, and with no one signed in none is another's). It doesn't run.
  `List` also gives each routine its `Figures`, for its status line, from three reads of the
  ledger (the grouped `ListRoutineFigures`, then `ListRoutineLastRuns` and `ListMatchHeads`
  for the match each routine ran last): how many matches it recorded today (since local
  midnight in the clock's zone), real ones and a Dry run's apart, how many wait for the user,
  and when it last ran: when its match that ran (done; in Dry run, would have) that closed
  last closed, which is when it ran, however long it waited (for the user, or under Pause)
  after it was recorded, and whether that was a Dry run's, so a newer one that waits or was
  skipped never reads as *ran*. The counts take the matches
  `ListLatestItemMatches` counts: open, or closed as done, skipped, or dismissed; a baseline,
  and a match that closed before it acted, count in none of them.
- `RoutineNames` gives every routine's name by its ID, the deleted ones included
  (`RoutineName.Deleted`), so the window still names a routine the history, a mark, or a chip
  names after it is gone.
- `Duplicate` makes an off copy right after the routine, named "<name> copy" (then "copy 2",
  and so on), the account signed in's. `SetEnabled` keeps `enabled_at` when switching on a
  routine that is on. `Reorder` puts the routines it names first, in that order, and the
  rest after them as they were, refusing one that is gone. `Catalog` is `catalog.All()`.
- `Sentence` names a starter prompt by the nickname of the user's copy of it (`Names`, core's
  adapter over the prompt library).

**The copy and the restore.** After every change to a routine, the Service writes its copy,
`autopilot.json` beside the database (`File`, `appdata.Paths.Autopilot`, user-only through
`appdata.WriteFile`), every routine not deleted in the export format:

```text
{ "format": "<slug>-autopilot/1", "exportedAt", "app",
  "routines": [ { "name", "description", "mode", "enabled", "template", "spec": {…} } ] }
```

`DecodeBundle` refuses another format and a newer version (`ErrNewerBundle`), and takes a
field a later build adds. A bundle never holds an account, or a routine's history.
`Restore` runs only while `autopilot_routines` holds no row at all, the deleted ones
included, so a routine the user deleted is never brought back: from the database set aside
when it has the table (`store.OpenSnapshot`, tolerant of missing columns, keeping each
routine's account), and otherwise from the copy, every routine not deleted, in its order,
in one transaction, each switched off, its mode as the source says (a step this build can't
run keeps it in Dry run), its spec stored as `Save` stores one (normalized, then encoded)
when it loads, and otherwise compacted but as it was, so a newer build's stays broken and
unrewritten; a spec that isn't JSON at all is left out, and two of one name are told
apart by a number. Core (`internal/core/kept.go`) runs it after the prompts and the launch
defaults, so the prompts a routine names by nickname are back first, and writes the copy at
startup only when it is missing or routines were just restored, so a loss in the database
never overwrites it. The window's restore notice counts them (`appinfo.Restored.Routines`).

**Backups.** The Autopilot section (`backup.SectionAutopilot`) is the export format,
through `NewBackupPart`, a type of its own that core adapts in `adapters_backup.go`, never a
method of the bound Service. `PlanBackup` leaves out, with a note, a routine with no name, a
second of a name, and one whose spec won't load (as a newer build's); a mode this build
doesn't know takes the mode new routines start in. Routines are matched by name
(`nameKey`): one whose definition, description, and mode are the file's is left as it is, on
or off, under either mode; any other is saved over and switched off, and one this Mac lacks
is added, off, at the end (Merge) or in the file's order (Replace, which also deletes,
softly, every routine the file doesn't name). A routine here that a newer build wrote is
never saved over or deleted by an import, under either mode: it is left as it is, with a note
(*made by a newer Nilbox: left as it is*), as `Save` refuses to save over it. A spec that
loads is stored normalized, as on a restore. `BackupDone` writes the copy again. The window
lists the category as **Autopilot routines**, beside the others.

**The settings** are `internal/autopilotprefs`'s, a leaf that imports only `prefval`:
`autopilot.enabled` (Autopilot (beta), off: see **Autopilot (beta)**, below),
`autopilot.paused` (off), `autopilot.defaultMode` (`ask`), `autopilot.agentsAtOnce` (2, 1 to
3, so one of the run engine's four slots stays free for the user's scripts),
`autopilot.runsPerDay` (20, 1 to 200), `autopilot.actionsPerDay` (50, 1 to 200),
`autopilot.dailyCap` and `autopilot.dailyCapUnit` (0 for none, else 1 to 500 dollars or
100,000 to 50,000,000 tokens; `usd` or `tokens`), `autopilot.keepAwake` (off),
`autopilot.holdOnOutage` (on), `autopilot.catchUpHours` (24, 1 to 168),
`autopilot.historyDays` (90, 7 to 365), and `autopilot.logDays` (30, 1 to 90, never more than
the history). `Decode` reads a value out of range as its default, a cap that doesn't fit its
unit as none, and logs longer than the history as long as it. Settings alone writes them:
each `SetAutopilot…` setter reads them and writes the change in the transaction that reads
the settings back, refuses what `Config.Check` refuses with a worded sentence, writes only
the keys that change (`Writes`: a setting never touched keeps following its default), and
emits the Go-only `event.AutopilotChanged`; a shorter history shortens the logs with it, and
logs past the history are refused. Backups carry them as `Portable.Autopilot`, the switch
(`enabled`) among them, the history and the logs checked together against this Mac's. While
the user has a routine (not deleted), the bug report states them, the switch first (*Beta:
off*), with the routines counted by mode, by switch, broken, and another account's, none
named; with none, it says nothing of Autopilot.

**Autopilot (beta).** `autopilot.enabled` (`SetAutopilotEnabled`, Settings → Labs) is off on
every install until the user turns it on, an install from before it included, since a key
never stored reads as its default; Prefs that can't be read count as off. Off, Autopilot is
inert and invisible, its routines, ledger, and copy left as they are:

- the snapshot of the searches is empty (`rebuildWatches` leaves every routine out), and
  `Searches.HoldsWatches` is false (`Engine.Enabled` and the lock), so the inbox's watch pass
  sends nothing and the checks follower follows none of the routines' items (its
  `ListRunningChecks` takes `watched`, `holdsWatches`, so the rows a search stored before stay,
  stale, and unfollowed);
- the engine (**The engine and the ledger**, below) takes no lock, runs no cycle, applies no
  command (`ErrOff`, *Autopilot is off. Turn it on in Settings → Labs.*), and makes no GitHub
  request; `Test`, and `Matching` through it, are refused the same way before they read
  anything;
- `Glance.On` is false and `Glance.Enabled` 0, so `KeepsRunning` counts only an agent run
  still going, and core's `ShellAutopilot` hands the menu bar no Autopilot group;
- the window (**The Autopilot tab**, below) shows none of it.

The engine reads the switch on every wake (`follow`, on the loop; core's relay wakes it on
`event.AutopilotChanged`), and a command reads it again before it is applied. Turned on, it
starts as a start does (`turnOn`: `takeLock`, which reads its state and checks the runs
going); its first cycle waits for a watch pass from then on (`onSince`), since what the
searches stored before is stale, and asks the inbox for one at once (`WatchNow`); and the
state's last refresh is the one it saw before it was off, so being off longer than the
catch-up setting begins a catch-up (`watchGaps`), as a quit that long would. Turned off
(`turnOff`), it starts nothing new from then on and stops nothing: it drains (`drain`), still
applying what its workers bring and settling the runs going on its ticks, and lets go of the
lock once no worker's result, no check of the runs, no run's end left to write, and no run
the ledger records as going (one whose end is on its way still is) remains. `Engine.Enabled` is the one check every start
goes through: `dispatch` asks it on the worker right before a step is handed to its
dispatcher (the agent `Runner` included), and a step that comes just after Autopilot was
turned off isn't handed over, and waits as one that never started (`ErrOff` in
`dispatchFailed`: a run ends `not_started`, counting as no try, and the match is queued).

### The searches, and the builder's reads of the inbox

**The snapshot.** After every change to a routine (`changed`, a backup's import included) and
once at startup (core's `assemble`, before the scheduler starts), the Service builds again
the searches of the enabled routines of the account signed in that load: each search built
for that account (`watch.Build`, "your account" written as its login), each key once with the
routines that share it, and `Follow` set when one of their triggers reads the checks (Checks
fail, Checks pass). It keeps them in an atomic value, which `Searches.Current` reads at once,
never waiting on anything; `Searches` (`NewSearches`) is the inbox's face of it, never bound.
Builds run one at a time (`watchSet.build`, a one-slot channel held from the read of the
routines to the store), so the last to read stores last: two side by side, a change's and a
new account's, could otherwise store the older read over the newer. A routine that is off,
broken, another account's, or whose search won't build is left out, and every routine while
Autopilot (beta) is off; the engine builds the snapshot again as it is turned on or off.

**`Suggest`** returns what the stored items hold for the builder (`Items.Suggestions`): the
people and the bots who opened them, the owners of their repositories, their labels, and
their check names, each most common first, no list nil.

**`Watching(id)`** returns the IDs of the items the routine watches, newest activity first:
those in the inbox lists it names, and those its searches' rows hold, stale ones included, so a
search that failed still shows its items (`Items.WatchedItems`); the inbox's bound `ItemsByID`
turns them into rows.

**`Test(draft)`** tries a draft on its items now, changing no routine and running nothing: its
spec is checked as `Save` checks it (a `ProblemsError` with each problem); its searches are
read from GitHub (`Items.SearchNow`) unless a test read them within a minute (`testCache`,
by query, answers that failed never kept), their items stored as the watch pass stores them,
with no rows; each search is checked for an owner the inbox has never seen (`KnownOwners`),
since GitHub answers an unknown user, org, or repository with nothing and no error; the
routine is evaluated on each item (`eval.Evaluate`, with no history yet) from its facts
(`Items.Facts`), and the check names of the pull requests whose decision waits on them are
read, one batch of 25 (`Items.ReadChecks`), before they are decided again; the matches come
first, then the unknowns. The result says what the test spent (`Points`: the searches, the
details they read, and the check names, the searches costing nothing again when `Cached`) and
what the draft's searches would add to each refresh (`Costs.Estimate`, core's `watchCosts`:
the requests the refresh packs them into, by what each matched, at most a page, and a full
page for one that failed, at what the last watch pass cost a request, at the refresh's pace,
as a share of the GraphQL budget). While the GraphQL budget is under a quarter of its size the
inbox sends nothing (`ErrLowBudget`, from `SearchNow` or, when every search came from the
cache, `ReadChecks`): the result says so (`LowBudget`), each search it had no answer for says
why, no check name is read, and the lists' items are still decided. A search that matched
more than a page warns that only the newest 100 are checked; each item says whether an inbox
list holds it (`Listed`), and when most don't, each triage step gets a warning (`Warnings`, at
the step), since triage changes only what the inbox shows; a watch of searches alone gets none,
since `Describe` warns each such step of it already. For a saved routine (`d.ID`), each item
says what the ledger holds of the occurrence it fires for now (`Record`, in the window's words:
*It already ran on this.*, *Waiting for your OK.*, that a Dry run recorded it, or why it closed
in Activity's own words, *Closed: it merged.*, from `reasonWords`, which a test holds to the
window's `reasonChips`), read from `ListRoutineMatchKeys`; a match with no record is one it
would run on.

**The window's builder** (`frontend/src/components/autopilot/builder`) renders from
`Catalog`, its pickers fed by `Suggest`, and holds the draft in the Autopilot tab, which stays
mounted while another tab shows, so a draft survives switching tabs. It calls `Describe` once
editing rests (300 ms), and `Test` only on the user's click, once the draft has no problem,
never as a routine opens or as it is edited, since a test's searches cost points; a test of an
earlier watch, trigger, conditions, or Once per stays shown, marked as such, its warnings
shown only while the steps are as tested (they are by step), and its cost line, once the
searches changed, said to be the last test's. `Test`'s minute-long cache keeps a second test
of the same searches from costing points again.
Saving a routine that is on goes through the same question as its switch (`useRoutineChange`'s
`save`): `Matching` with the draft, nothing asked only when nothing matches, nothing waits to
be told, and everything was read (`Matching.Incomplete`: a search with no answer or past a
page, a budget too low, or more items than a test tries), and `Save` with the answer as its
`Current`. The window's catalog fixture (`frontend/src/test/autopilotCatalog.json`) is the
catalog itself, written and checked by `TestTheWindowsCatalogFixtureIsTheCatalog` (`-update`
rewrites it).

### The engine and the ledger

**The ledger** (migration `0007_autopilot_ledger`, all of it Autopilot's) records what each
routine did with each item it matched. `autopilot_matches` holds one row per routine, item,
fire key, and Dry run (unique), so an existing row blocks a repeat: once per occurrence holds
across refreshes, check re-runs, and restarts, and a Dry run's record never blocks a real
run. Each row keeps a copy of its item (repository, kind, number, title, URL; `item_id` is set
NULL once the item is gone), the decision's trace as it was, the steps it started with and
how each went (`steps`, JSON, with `step` where it is: a routine saved with other steps never
changes what a match has done), the spec's fingerprint (`spec_hash`), the head it was decided
on, whether auto-merge was on, the user's OK (`approved`), and how many runs it started
(`attempt`). A row is open (`waiting`, `blocked`, `queued`, `running`, `verifying`, with
`blocked_by`) or closed (`done`, `skipped`, `dismissed`, `closed`, `baseline`, with `reason`),
and every change is a compare-and-set on the state it was read in (`UpdateMatch ... WHERE
state = was`). Only the routine's own outcomes close a row: it ran (`done`), the user skipped
it (`dismissed`), it ran out of tries or met the outside contributor rule (`skipped`), or the
item moved on (`closed`: merged, closed, gone, left the watch, its fire key moved, no longer
matches, the routine changed or was switched off, the item's sheet no longer offers the
action). Everything that only delays a start keeps it open, retried every cycle. A row that
closed without ever running (no longer matches, left the watch, moved on, the routine changed
or was switched off, `Reason.reopens`) opens again when the same occurrence matches again, but
only if none of its steps ever started (`reopensRow`: no run, no step moved past, none running,
done, or failed): a row that started a step, a local one included, is the occurrence's record,
which only Try again opens again, and `Matching` counts it as recorded.
`autopilot_runs` holds one row per run a match starts (an agent's, in the background or the
terminal, or a GitHub action the app sends), with every column the run view, the caps, and the
stats read, and a partial unique index on `item_id` for the active statuses (starting,
running, stopping), so a second active run on an item fails in the database whatever the
engine's own checks say. `autopilot_tries` keeps when Try again last reset a routine's count
on an item, `autopilot_baselines` what a routine first saw of an item (its head, its comment
count, for New commits' first commit and New comments), `autopilot_hands_off` the items a
routine leaves alone, and `autopilot_effects` (migration `0008_autopilot_effects`) what its
local steps did to their items' triage (**Local actions**, below). Migration
`0011_autopilot_matches_item` indexes `autopilot_matches` by `item_id`, which every read by
item (`LatestMatches` and `ItemHistory`) searches. The engine's own state,
`autopilot.engine` (a JSON pref only the engine writes, `enginestate.go`), holds when Pause
began, the last refresh it saw, when the last catch-up began and the routines still in it, the
last cycle, each GitHub routine's first-pass fingerprint, the routines whose current matches
become their baseline, the problems, and when the retention last ran; a deleted or
switched-off routine's entries are dropped.

**The engine** (`NewEngine`, never bound; the Service is its face) is a deferred desktop worker
with one loop of its own (`engine.go`), the only writer of matches and the only thing that
starts anything. It wakes on what the inbox stores (core's `engineOnInbox` for a whole
refresh's `event.InboxUpdated`, and the `Changed` seam), on a change of Autopilot's settings
(core's relay turns `event.AutopilotChanged` into `Wake`) or of a routine, on what the inbox
saw happen to items (core's `outcomesFanOut` tells it beside the mirror of the GitHub
notifications, without waiting), on its 1-minute tick, on a window's command, and on a
worker's result; what comes during a cycle folds into one more. A cycle (`cycle.go`) reads
SQLite and evaluates, milliseconds: the enabled routines of the account signed in (one that
won't load is a problem, and skipped), the items each watches, their facts, the open matches'
items' states, the runs on them, and the day's counts; then it decides each routine on each
item (`eval.Evaluate` with the ledger's `History`: tries since the last reset, the last
outcome, the head it first saw, the comments it last counted, and the commits routines' runs
pushed), records each new match, closes what moved on, and moves each open match through its
steps (`advance.go`). Everything slow runs on a worker that hands its result back to the loop
(a mailbox that never blocks a worker): check names for the candidates whose decision waits
on them, the states of items that left a watch (read before anything closes), the read of an
item right before a start, a dispatch, a stop. So a command is applied within a fraction of a
second, mid-cycle or not. Each cycle is logged (its length, the routines, the items, the
matches, the starts, the closes, the reads), kept as `Engine().LastCycle`, and told to the
window as `autopilot:updated`, at most four times a second. A cycle that panics is recovered
and logged, and the loop holds off cycles for a minute; commands are still answered.

- **The first cycle after a start** waits for a refresh that stored after the engine was built
  and, when the routines have searches, for a watch pass after it (after Autopilot (beta) was
  last turned on, `onSince`), so the engine never acts on what the database held when the app
  was last open, or while it was off; when a refresh came but no pass (the very first one,
  before an account was stored, or Autopilot just turned on), it asks the inbox for the
  searches at once (`Routines.WatchNow`, a pass outside a whole refresh). Commands don't wait
  for it.
- **The lock** (`lock.go`) is a `flock` on `<data>/autopilot.lock` (`appdata.Paths.AutopilotLock`),
  tried as the engine is built (in `assemble`, so the copy that will hold it sends the searches
  from its first refresh) and on every tick, both only while Autopilot (beta) is on (**Autopilot
  (beta)**, above): only the holder runs cycles and applies commands
  (another copy refuses them, `ErrElsewhere`, and `Engine().Elsewhere` says so), and only the
  holder sends the routines' searches at all (`Searches.HoldsWatches`, which the inbox's
  `WatchHolder` reads before a watch pass), so two copies on one data folder never replace each
  other's rows; the holder builds its snapshot of the searches again from the database on every
  tick, which catches a routine changed in the other copy. A copy that doesn't hold it reads
  `Glance` on every tick and sends `autopilot:updated` when it reads otherwise than on the tick
  before (`followElsewhere`), since it runs no cycle that would: a Pause or a routine switched
  on in the holder reaches its menu bar and its window's close within a minute. The system
  lets go of the lock when the process ends.
- **The runs going** are checked when the lock is taken and on every tick (`reconcile`), but
  those a worker of this copy is starting or dispatching, and those whose `RunEnded` is on its
  way. A GitHub action still recorded as going was cut off by a quit or a crash, maybe after
  GitHub took it, so it is never sent again on its own: it ends interrupted, and its match waits
  for the user, worded so they check the item on GitHub first. An agent's run is asked of its
  runner (`Runner.Alive`, a `Liveness`), and taken as gone or never started only once two checks
  a tick apart found it so, as a runner may let go of a run a moment before its end is delivered
  (the `Runner` contract keeps a run `LiveAlive` until then). One both checks found the runner
  positively never started (`LiveNever`, still `starting`, as a start the quit cut off before it
  reached the runner) goes back to the queue, its match with it; any other found gone ends
  interrupted. `LiveNever` must hold across restarts and never come from a history that forgets
  runs (a runner that prunes its records answers `LiveGone` or `LiveUnknown` for a run it no
  longer knows), so a run that started and crashed is never run again on its own. A runner's
  end that comes after the engine recorded the run interrupted is kept over that record
  (`ReviseRunEnd`), its cost and commit with it, and moves the match on while it still waits on
  that interruption. One the runner can't say of (`LiveUnknown`, or an error) is asked again on
  the next tick, and ends interrupted once it has been unknown past the longest time limit a run
  may have and a grace (`unknownFor`), so no run holds its item's slot, an agent's slot, or a
  reservation for good. A start the runner refuses as it closes (`ErrClosed`) stays queued, and
  no agent starts in this run of the engine after it. A start that never happened, either way,
  ends `not_started`: it counts as no try and no run (`Attempt` is taken back, so the match is
  decided again like any queued one), and toward none of the day's counts or the daily cap.
  A run's end and its match's move are written in one transaction, a dispatch's answer too; an
  end that can't be written is tried again on the next tick, and each tick also moves on any
  match still `running` on a run the ledger records as ended (`unstrand`), by the end it keeps,
  or as interrupted when that end can't say (a stop, whoever asked for it). A stop that fails is
  tried again a tick later, never at once, and its reason is kept: a run that ends stopped
  anyway closes its match for what the stop was for (merged, closed, gone).
- **A catch-up** begins after a gap longer than Settings' catch-up hours (with the app closed or
  open, the Mac asleep): at once, on a cycle that finds the last successful refresh that long
  ago, before any refresh came; at the first successful refresh that long after the one before;
  or at a Resume after a pause as long. One gap begins one catch-up. Every routine on then is in
  it, and each leaves it on its own once a cycle has decided on what it watches read after the
  catch-up began (each of its searches answered with no error, and a refresh, when it watches
  the inbox's lists or has no search): until then its agent and GitHub steps wait for the user
  (`catch_up`), and a routine whose own search keeps failing says so, while the others go on.
- **A start** goes through, in Go, in this order (`gate`): the user's OK for a routine that asks
  first, and in a catch-up an Autopilot routine's too; no one signed in to GitHub (the last
  refresh found no token, or GitHub refused it: `signed_out`), Pause, and the routine's active
  hours, all three of which hold a local step too; the tries (`skipped: tries`), the routine's runs a day, the agent runs
  a day, the GitHub actions a day, and the wait between tries, none of which a manual run counts
  toward; a free slot and the daily cap and the routine's own budget, reserved in their unit,
  for an agent; the outside contributor rule (an agent closes `skipped`, a GitHub action waits);
  a fork's branch for a run that pushes; and one active run per item. Then the item is read
  fresh on a worker (`ReadItem`, the inbox's `RefreshItem`) and the `Holds` the later parts
  register are asked; the cycle after that read, which evaluated the item on what it stored
  (a moved head closes the match as moved on), records the run and the reservation in one
  transaction and hands the step to its dispatcher. A local step runs at once, through its
  dispatcher, held back only by a sign-out, Pause, and the active hours. A day runs from local
  midnight to local midnight, as the clock tells local time: a run's cost belongs to the day it
  started, and every run still working counts what it reserved against today's cap whatever
  day it started.
- **The dispatch seam** (`dispatch.go`): a narrow `Dispatcher` per part, keyed by the catalog's
  dispatcher names (`EngineConfig.Dispatchers`: triage, notices, github, verify, merging,
  review), and the agent part's `Runner` (`Dispatch` starts a run, `Stop` and `StopAll` stop at
  once, `Alive`), whose ends come back through `Engine.RunEnded`. A `Job` carries the match,
  the run, the routine, the item's copy, the head it is pinned to, the step, and the budget;
  a `Result` says it runs on, how it ended (an Only if outcome), whether GitHub took a write,
  and when the item's sheet no longer offers the action or offers it with warnings. A step with
  no dispatcher registered can't run, and the Service keeps a routine with such a step in Dry
  run (`Offers`, the catalog's own by default, which a test hands in with fakes). The triage
  steps are offered: the engine registers its own triage dispatcher over `EngineConfig.Triage`
  (core's `inboxTriage`, **Local actions**, below) unless `Dispatchers` names one; so are the
  notices (Notify me and Tell me, **Notices**, below), whose dispatcher core registers, the
  agent step, whose `Runner` core registers (**Agents in the terminal**, below), and the
  GitHub step and Mark the GitHub notification done (**The GitHub step**, below), whose
  dispatcher the engine registers over the Service's `GitHubActions`; a routine with any other
  step still runs in Dry run, and the engine records what it would run. A `Result` may also say
  the step didn't start and what the match waits on instead (`Wait`, `WaitWords`, and a `Hold`
  the user confirms), or that it goes back to the queue (`Requeue`), and what a launch started
  (`Launched`, recorded on the run, `RecordRunLaunch`). For the notices, a job also carries
  when its match was made, the routine's own banner switches (`Notices`), why it matched
  (`Reason`, from the trace), and how the steps before it went (`Before`). `dispatch` hands a
  job over only while `Engine.Enabled` says Autopilot (beta) is on, asked on the worker right
  before `Dispatch`, so no part (the agent runner's starts included) is handed a step after it
  was turned off; the freshness read before an agent's start or a GitHub action asks it too, so
  nothing is read from GitHub for a start Autopilot turned off since.
- **A run's end** moves its match on: an interrupted one waits for the user (`interrupted`,
  counting no try), one the user stopped closes `dismissed`, one the engine stopped because its
  item merged or closed closes `closed` (counting no try), and one that ended goes to its next
  step, straight to `verifying` when it pushed and Wait for the checks is next, so the push,
  which moves the item's head, never closes it as having moved on. A match that started a run
  is closed only by its item being done with; its fire key moves by design.
- **Settle what moved on:** an open match whose item merged, closed, or is gone closes; one
  whose item left every watch of its routine has its state read (`ReadStates`, one request, one
  at a time per item, again every half hour), so it closes as merged or closed when it did, and
  closes as having left the watch only while every search of the routine last answered in full:
  a search that failed, was truncated, or was read in part keeps its rows, stale, and even once
  the inbox drops a truncated tail, its items are stale, not gone; one whose fire key moved
  closes as moved on, one the routine no longer matches as no longer matches (or, after an edit,
  as the routine changed), and the rest take the routine's new steps. A running agent whose item
  merged or closed is stopped through the runner; a verifying match closes.
- **The first pass** of a routine with a GitHub action, after it is switched on, leaves Dry run,
  changes mode, or its spec changes (a fingerprint of the spec, when it was last switched on,
  and its mode, so switching it off and on in the other copy between two cycles counts too;
  recorded only for a live, enabled routine), waits until its searches were read since, then
  queues at most `FirstCap` (25) matches; over that, none, and they wait as a `backlog` for
  Approve.
- **Apply to current matches:** `SetEnabled`, `SetMode`, and `Save` (`Draft.Current`) take what
  to do with what the routine matches now and has no record of (`Matching` counts them, as
  `Test` reads them, and says when the count may be short, `Incomplete`): `run` them too, or
  `fromNow`, applied as one command with the change, so no cycle runs between; the engine then
  records those matches as `baseline` once the routine's searches were read since, and nothing
  runs on them. `fromNow` is refused, with why, in a copy that doesn't run the engine (it hasn't
  started yet, or another copy holds it), rather than dropped, but only for a change the
  question applies to (switching on, leaving Dry run, a save that changes the Watch, When, If,
  or Once per of a routine that is on, `watchChanged`, since Once per decides each match's
  fire key, each told before the change is made); any other change, and `run`, is made there at
  once.
- **Problems** (`upkeep.go`): a search GitHub fails, and an enabled routine that won't load, are
  recorded once each, keyed by routine and problem, with when they began, in the engine's state
  and `Engine().Problems`, and told through a `ProblemSink` once (core registers
  `NewProblemNotices`, below), with the routine's name, when it was made, and its own banner
  switch for problems; one that couldn't be told stays untold and is told on a later cycle,
  and one that ends is forgotten, so it is told again when it comes back.
- **Retention:** once an hour, a closed match closed longer ago than the history's days goes,
  with its runs, but only once its item is known to be done with (gone, merged or closed, or out
  of every list and watch since then): deleting the record of an item still open would let it
  fire again. Open matches never go. `ClearHistory` does the same whatever a row's age, but
  "out of every list and watch" still means for the history's days, since the app reads no more
  of an item once it left them; it keeps a closed match that may still block its occurrence
  without its trace and summaries. What deleted routines
  kept of their items goes, and a deleted routine nothing refers to goes for good. The inbox's
  prune keeps every item a match or a run refers to (`KeptItems`, one of core's `itemKeepers`).

**The window's actions** are commands the loop applies, then answers (`commands.go`), so a
double click, a banner's click racing a cycle, or an Approve during a refresh changes a match
once: `Approve` (an Ask first match's OK, a catch-up's, a backlog's, an interrupted run's),
`Skip` (refused for a running one), `RunNow` (once per click, on an item its conditions match,
counting toward no cap or try), `TryAgain` (resets the tries and opens again what gave up on
the occurrence that still holds), `LetActAgain`, `Undo` (**Local actions**, below), and
`ClearHistory`. A command before the loop runs is refused (`ErrNotRunning`), and one while
another copy holds the engine
(`ErrElsewhere`). `StopRun` and `StopAll` go straight to the runner, never queued behind the
loop, so they work before the first refresh, offline, or signed out. The reads (`activity.go`)
read the database alone: `Activity` (a page of matches before an ID, by local day, with their
runs and effects, each day's grouped lines of local steps, and, on the first page, the items
the routines leave alone), `ActivityFrom` (the same, of the matches from an ID on and before
another, 0 for up to the newest, at most 500, `ListMatchesFrom`: a stretch of the pages the
window has loaded read again in one call, by its own bounds, so their grouped lines join by
`Key` and nothing falls between them; `More` and `Next` say where the rest of the stretch
begins, and only a stretch up to the newest says what the routines leave alone),
`ItemHistory`,
`LatestMatches` (for each item among the ids it is
given, at most 500, its newest match that did something there or waits to, in one query,
`ListLatestItemMatches`: open, or closed as done, skipped, or dismissed, never a baseline or
one that closed as the item or the routine moved on before it acted, with its runs and
effects, for the chip on every row; the window asks in chunks of 500, `inChunks`), `Explain`
(the decision now, and what the ledger knows:
the record for the occurrence, the tries used, not in the watch, left alone), and `Engine`. No
result holds a nil list.

### Agents in the terminal, readiness, and Needs your OK

What the user sees is in `docs/desktop.md`'s
[Agents in your terminal](desktop.md#agents-in-your-terminal), [Needs you](desktop.md#needs-you),
and [This Mac](desktop.md#this-mac).

**The start path.** Every agent start goes through the engine's one path, never another: the
gate (`advance.go`), in Go, cheapest first, then on a worker what holds it back on this Mac, then
the fresh read, then the runner:

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

**Agents in the terminal.** Core's `agentRuns` is the `Runner`: a step `In my terminal` is the
launcher's own `Launch` of `routineRequest`, the request a click would make with the routine's
choices in the launch menu's place (the step's agent, profile, model, and effort, a per-org or
per-repository row of the routine's over them, a repository's over its org's; the prompt by its
starter's key or its nickname; plan mode and the worktree), and `launcher.RoutineLaunch` naming
the routine. `RoutineLaunch` only ever holds a launch back more than a click: its pull request's
files are checked under the unattended scope (`FileChecker.CheckFilesUnattended`, the inbox's
`Routines.CheckFilesUnattended`: every pull request the user didn't open, trusted bots' and
members' included, `sensitive.CheckedUnattended`), a file hold is confirmed only by
`RoutineLaunch.Confirmed` (never the request's own `Confirmed`), a session running in the
folder always holds it (`Alongside` is ignored), a worktree with uncommitted changes holds it
(`HoldDirty`, keyed by the worktree, its head, and the count), `FullAccessOverride` is ignored
(`personsChoices`), and its prompt names the routine (`.Routine`, `.Trigger`, `.Attempt`;
`.Unattended` stays false in the terminal). A hold comes back as the match's wait (`heldResult`:
agent files or unchecked files `waiting: hold` with the hold, a dirty worktree `waiting:
dirty_worktree`, a session `queued: busy_worktree`); `worktrees.ErrHeadMoved` requeues it. A
launch that opened ends the run done at once, recording its session, agent, profile, clone,
and worktree (`Launched`): the session is the user's from there, which the sessions ledger
follows as any, and a later start finds it in the folder (the launcher's running hold).
`Alive` answers gone for a terminal run still recorded as going, so one a crash cut off
mid-launch is never launched again on its own. The background is P11's runner; until then a
background step can't be saved (`placeProblems`), and one stored before isn't ready.

**The user's OK of a hold.** A held step keeps its `Hold` in the match's steps (`stepRun.Hold`),
which `Match.Hold` shows the window while the match waits on it; `Approve` adds exactly that
hold's `Key` to the step's `Confirmed` (`confirmHold`), which the next `Job` carries
(`Job.Confirmed`, `RunRequest.Confirmed`) into `RoutineLaunch.Confirmed`. A routine never
confirms a hold on its own, and a key names what the user saw (the head commit for files, the
file count for a dirty worktree), so a push or a new file since holds it again.

**Readiness** (`ready.go`). `Needs(spec, LaunchDefaults)` is pure: GitHub for every routine;
banners for a notice; for an agent step its place (`place:`), the terminal for a terminal run,
each agent and profile it may run (its own, its per-org rows', and its fallbacks', Inherit as
the app's default agent and that agent's default profile), `gh` and `git`; and what it
`Requires`. A `ReadinessChecker` (core's `readyChecks`) checks each need: GitHub from the inbox's
last refresh, or `setup.GitHub` (one point) on Check again; a program on the login shell's PATH;
an agent by its spec's `VersionArgs` (`agent.ParseVersion`); a profile by its folder and its
spec's `SignInArgs` (`agent.ParseSignIn`: Claude Code's `auth status` JSON, keeping only
`loggedIn`, the method, `subscriptionType`, and the expiry, never the email; Codex's `login
status`), run with the profile's `ConfigEnv` and without the agent's `KeyEnv` unless the user's
API key setting is on (then a key in the shell's environment is ready by itself); the terminal
chosen; and the banners (a warning only). An agent's version and a profile's sign-in are kept
ten minutes unless fresh. A routine is ready when each need is ok or a warning. The Service and
the engine share the results (`readiness`): the engine checks on a worker as it starts, when a
routine is saved or switched on (`Service.changed` marks it due), and every ten minutes; the
bound `Readiness` serves them (checking once when none ran) and `CheckReadiness` runs every check
again, fresh, then wakes the engine. A need not met is a problem (`ProblemNotReady`, keyed by the
routine and the need), told once.

**Needs your OK.** Every save of a match that comes to wait for the user (`saveIn` →
`noteWait`), a Dry run's never, is told at the cycle's end (`tellWaits`, on a worker) as a
`NoticeNeedsYou` through `EngineConfig.NeedsYou` (core's notices sink), keyed by the match, its
creation, the wait, and the try, so a cycle that sees it again, or a restart, tells nothing
twice; read again first, so one no longer waiting is told nothing; several at once are a burst.
Its target is the match, which the window's Needs you card is. The bound `NeedsYou` lists the
open matches that haven't started (waiting, blocked, queued), real ones alone, newest first,
for that section.

**Run a routine** (`RunNow(id, itemID, anyway)`): a manual match, once per click, counting
toward no cap or try; with `anyway`, on an item its conditions don't match but of a kind it acts
on, under a manual fire key. Every limit of a start still holds it.

**Preview** (`PreviewRun(draft, itemID)`) is `Previewer` (core's `runPreviews`): the launcher's
own `Preview` of the same `routineRequest` a start would launch, with where each setting came
from (`routine` for the routine's own), so what it shows is what runs.

### Local actions: by a routine, Undo, and hands off

What the user sees is in `docs/desktop.md`'s
[Snooze, dismiss, and pin: by a routine](desktop.md#snooze-dismiss-and-pin-by-a-routine).

**The effects** (`effects.go`). The triage dispatcher (`triageDispatcher`) runs a match's Pin,
Snooze (its `days`, else three), Dismiss, or Restore through `Triage.Apply`, the inbox's own
triage for the routine (`inbox.Routines.Triage`, see `internal/inbox`'s **A routine's
triage**), whose record writes the `autopilot_effects` row in the triage's own transaction,
before its write: the match and step (unique), the routine, the item, the action, and the
item's local state before and after, as the inbox writes them (JSON, opaque here), with
`applied_at`. A step is a compare-and-set on the item's local state as its cycle read it,
before the item's facts (`EngineItems.LocalStates`, passed as `Job.Local`), and its record
checks, in the same transaction, that the routine isn't hands off the item, so the user's
own change, or reversal, while the step was on its way wins: `KeptChanged` ("Not done: the
item changed since this matched.", which also follows a snooze the wake rules ended or
another routine's step that landed first), or the routine left alone ("Left alone: you took
back what it did here.", the words that go with Let it act again). A step that changes
nothing records nothing, and its step's note says why ("You set it aside yourself, so it's
left as you left it."). A step a crash cut off after its change
committed runs again on the next start (`stepUnderWay`), finds its effect, and is done, so
nothing changes twice. Its limits are the local steps' (`runLocal`): a sign-out, Pause, and the
routine's active hours hold it, the match staying queued (`signed_out`, `paused`,
`active_hours`); the caps, the tries, the agents at once, the daily cap, readiness, the
outside contributor rule, and a catch-up never do; its own limit is the routine's Once per.
`Match.Effects` lists each match's, with `CanUndo` (neither undone nor taken back, and the
item still as it left it, `Triage.AsLeft`; false in a copy without the engine).

**Undo** (`Undo(ctx, ids []int64) (Undone, error)`, by effect: a match's `Effects`, or a
grouped line's `ActivityGroup.Undo`) is a command the loop applies. It reads the effects newest
first, skips those undone, taken back, or of an item gone (`Skipped`), and hands the rest to
`Triage.Undo` in one transaction, whose record marks each effect it put back `undone_at` and
leaves its routine hands off the item (`autopilot_hands_off`, "You undid what it did."). An
item that changed since stays as it is (`Left`). `Words` says it for a toast ("Put back 5 items.
1 changed since, so it's left as it is."). A GitHub action has no Undo.

**Hands off.** The user's own triage that takes back what a routine did reaches the engine
through the outcomes fan-out (`Outcome.Reversed`: the routine, the item, and what it did,
which core's `toEngineOutcomes` passes on), which tells the engine before the mirror of the
GitHub notifications, whose record can wait on a clear on its way to GitHub;
`Engine.Outcomes` records it at once, in whichever copy of the app the user did it, in one
transaction: that part's effects of the routine on the item (the pin, or the dismissal and
the snooze) applied by the outcome's time are `reversed_at`, so a step that landed after it
keeps its Undo, and the routine leaves the item alone (`InsertHandsOff`, with the words: "You
brought back what it snoozed.", "You snoozed it again yourself.", "You unpinned what it
pinned.", ...). Setting aside again what a routine's Restore brought back is no reversal:
only an Undo leaves such a routine hands off the item. A routine evaluates no item
it leaves alone, so it records no match there; an open match of it on the item that hasn't
started a run closes as `hands_off` ("Left alone: you took back what it did here."), which
opens again, if it never started a step, once the user lets it act again (`LetActAgain`).
`Explain` says so (`LeftAlone`), `Test` marks such items (`TestItem.LeftAlone`), and `Matching`
counts none of them.

**Activity's lines.** Each `ActivityDay` holds, beside its matches, a line per routine and
kind of local step among the page's matches, on the local day each step ran
(`ActivityGroup`: `Words` "Snoozed 6", the count, how many were undone and taken back, the
effects Undo would still put back, the matches it stands for, their items, and when the last
ran; `Key` names it across pages, whose matches may split a day). The first page also says
which items the routines leave alone (`ActivityPage.LeftAlone`, newest first, each with the
routine, the item's copy from its latest match, why, and since), for **Let it act again**.

**The simulator** runs the triage steps through the inbox's real triage (`sim.Triage`, the
adapter core's `inboxTriage` is, over the app the scenario built), and its scenarios include
the *Snooze passing bot PRs* template as `spec/testdata/templates` writes it (the inbox lists,
Checks pass, a trusted bot's patch or minor update: snooze three days, once per head commit,
waking by the user's rules, at once in Ask first, a record alone in Dry run), Undo after the
user changed some of the items, and hands off and **Let it act again**.

**The simulator** (`internal/autopilot/sim`, test support only tests import) runs the real
engine and ledger over a real database, inside a `testing/synctest` bubble: a scripted GitHub
(`World`: items whose heads, checks, and states a scenario changes) that the inbox's own
service reads, so every item goes through the inbox's store; a clock with sleep gaps (`Sleep`
jumps with no tick, `Advance` lets the ticks run); the fake `Runner` and fake dispatchers, but
the real notices dispatcher and problem notices over a fake sink (`Notices`, which records each
notice and can fail); and crash-and-restart points over the same data (`Crash`, `Begin`,
`Restart`), and a second copy on the same data folder (`Copy`). A scripted pull request's
failed checks can be named (`FailNamed`), as *Tell me about security failures* needs, whose
template the simulator loads exactly as the gallery holds it. Its pool keeps one connection,
so a wait for the database is one the bubble sees. Each scenario ends with the ledger it
expects.

### The GitHub step

What the user sees is in `docs/desktop.md`'s
[What Autopilot may change on GitHub](desktop.md#what-autopilot-may-change-on-github).

**The seam** (`github.go`). The GitHub dispatcher (`NewGitHub`, which the engine registers
over the Service's `Config.GitHub` unless `EngineConfig.Dispatchers` names one) reaches the
app only through `GitHubActions`: `Read` (the item's sheet reads it again, one point, which
rewrites the inbox's card from the same answer), `Send` (one action, as its sheet sends it),
`MarkDone`, and `Preview`. Core's `githubSteps` (`internal/core/adapters_githubsteps.go`)
adapts the review sheet (`ReadAgain`, `Refresh`, `Act`, and `EditLabels`, its label edit of a
pull request at the issues endpoint), the issue sheet (`ReadAgain`, `Refresh`, `Act`, `Edit`),
the mirror of the GitHub notifications (`ClearItemThreads`, what the card's menu clears), and
the reply filler the sheets share, each call marked `github.PurposeAutopilot`. A pull request's
actions are exactly `reviewActionOf`'s (`rebase_bot`, `update_branch`, `comment`), and
`TestTheGitHubStepReachesNoForbiddenAction` parses the adapter and fails on any other sheet
action it names, so no merge, approval, request for changes, close, or approval of workflow
runs is reachable from a GitHub step.

**One action, one run.** A GitHub step's actions go one at a time, each its own
`autopilot_runs` row (`runner = github`, `github_action` its kind), recorded before it is sent:
the step's stored state counts those that went (`stepRun.Action`), so a quit or a crash
interrupts only the one under way, which is never sent again on its own (`settleRun`), and the
user's OK sends that one alone. The read before each start is the sheet's (`itemReader`: the
dispatcher reads in place of the inbox's single-item read, so an action costs one point, not
two); the cycle after it decides the match again on what it stored, and the action is pinned
to the head that read found (`Job.Head`, `headNow`), which the cycle just decided on. The
adapter then decides from the sheet's own offers on that read (its `Refresh` serves it with no
request): absent or disabled is `NotOffered` (the match closes `not_offered`, with the sheet's
reason); offered with warnings the user hasn't accepted is `Warning` (the match waits
`warning`, its words the warnings); a head other than the one pinned is `PushedSince`, and a
sheet that couldn't read the item just before is `GitHubUnread`, which goes the same way. Only
then does it call the sheet's `Act` (`Accept` only for warnings the user accepted), whose own
`Decide` runs again, and whose refusals map the same way; the write gate's refusal before
anything was sent (`budget.RetryIn`, its pace, the reserve, or GitHub's hold) is `Held`. A
labels action sends only what changes the labels its read found, and says when there is
nothing (`GitHubNothing`).

**What each answer does** (`dispatchAnswered`): a sent action moves to the step's next, or
ends the step (`stepEnded`); `Warning`, `PushedSince`, and `Held` end the run `not_sent`,
counted as no try, and take back the attempt (`notSent`), so a match that sent nothing is
decided again like one that started nothing; `PushedSince` reads again before its next start,
and `Held` holds every GitHub action until the gate said the next may go (`holdWrites`,
`writes_held`). `Approve` of a match waiting on warnings records them on its step
(`stepRun.Accepted`), which the next send hands the sheet (`Job.Accept`), so a different
warning waits again; the routine's own Ask first OK accepts none. `ApproveBacklog` is Run on
them: the OK for every match of a routine waiting as a backlog.

**Limits.** A match is one try and one of its routine's runs a day however many runs it starts
(`history` and `dayCounts` count distinct matches; `limits` holds only a match's first run to
the tries, the routine's runs, and the wait), while the GitHub actions a day count each one
sent (a `not_sent` run counts toward nothing). Autopilot keeps to a third of the write gate's
edit lane, `ActionsPerMinute` (10) and `ActionsPerHour` (60), from the runs started in the last
minute and hour, manual ones too (`pace`, `writes_held`); the gate's own pace still holds
under it. An outside contributor's item waits for the user's OK in every mode
(`outside_contributor`), and the first pass's cap (`FirstCap`) holds a routine with a GitHub
action, as above. Mark the GitHub notification done is a local step: it runs at once, through
the chore lane, recorded as no run.

**Replies.** A reply action posts a comment's reply alone: `LibraryPrompt.ReplyFor` names the
review a library reply is written for, and `promptProblem` refuses an approval's (LGTM,
Update OK) or a request for changes' in `Describe` and `Save`; the engine checks the step's
replies again on its worker right before each action (`promptHold`, before the read, so a reply
changed since holds the action back as `blocked: not_ready` and spends no point), and the
adapter refuses one that isn't a comment's (`ErrNotACommentReply`) before filling it in.

**Preview** (`PreviewActions`, bound) builds what a draft's GitHub actions would send one item,
through `GitHubActions.Preview`: the rebase's comment, the branch's update, the reply filled in
for the item, or the labels it adds and takes off, with why one can't be built; it sends
nothing.

**The simulator** runs the GitHub step through the real dispatcher over a fake of the sheets
(`sim.GitHub`, over the scripted World: its read is the inbox's own single-item read, a send is
pinned to the World's head, and a scenario can withdraw an offer, add warnings, have the gate
refuse, or change the World as a send arrives), with a fake prompt library (`sim.Library`);
its scenarios include *Ask Dependabot to rebase what conflicts* as the template gallery holds
it.

### Notices, the menu bar, and the window's close

**The notices** (`notices.go`). The notices dispatcher (`NewNotices`, `catalog.DispatchNotices`)
runs Notify me and Tell me as local steps: each becomes a `Notice` handed to a `NoticeSink`,
which core adapts over the notifications (`internal/core/adapters_notices.go`,
`autopilotNotices`), so the engine never imports notify. Notify me tells why the match matched
(the trigger's words and each condition that held, with what it compared, on one line, at most
280 characters) and the user's own line; Tell me says it reached the step and how each step
before it ended, an outcome named as Only if names it (the richer words of what happened come
with the steps that make outcomes). Either is an alert (`NoticeAlert`) about the match's item,
headed by the routine's name, that opens the match's card, with when the match was made. Its
banner is the step's own choice (`NotifyStep.Banner`), else the routine's (`spec.Notices.Alert`),
else the settings'. A notice that can't be told fails its step, and the match says a step
failed. Each notice is told once whatever happens to the engine: its `Key` names it (a
step's by its match, when the match was made, the step, and the try; a problem's by the
problem and when it began, a start the engine saves before it tells it), and the
notifications keep one entry per key, so a step that runs again because a quit came before
the loop heard it was told, or a problem told again after a crash, adds no second entry or
banner. A cycle hands its notices over together as it ends (`pass.tellNotices`), each on a
worker: with more than one, or when any comes from a routine catching up, each is a `Burst`,
which core gathers
(`notify.Dispatcher.Tell` with gather, `GatherWindow`), so more than three at once show as one
banner (*4 new from Autopilot*, *4 routines need you*); a lone one shows at once. A Dry run
tells nothing. Problems go through `NewProblemNotices` over the same sink: a `NoticeProblem`
about no item, its line the routine's name ("A routine" when it can't be read) and its body the
problem, opening the routine, with when the routine was made; several untold at once are a
burst. The other kinds (`NoticeNeedsYou`, `NoticeFinished`, `NoticeChecks`) are declared for
the parts that make their moments. Core maps each kind to the notifications' own
(`noticeKinds`; a test holds the map to `notifyprefs.AutopilotKinds`).

**The notifications' side** (`internal/notify`, `internal/notifyprefs`). The five kinds,
`autopilot_alert`, `autopilot_needs_you`, `autopilot_finished`, `autopilot_checks`, and
`autopilot_problem`, are all on by default (`notifyprefs.AutopilotKinds`, `Kind.Autopilot`),
titled *Autopilot alert* (an alert's banner is headed by its routine, `Entry.Heading`), *Needs
your OK*, *Agent finished*, *Checks after a fix*, and *Autopilot problem*. Migration
`0009_notification_targets` gives `notifications` a `heading` and the Autopilot target
(`autopilot_routine`, `autopilot_match`, `autopilot_run`, `autopilot_created`, RFC 3339 of the
most specific one's creation, since Clear history hands IDs out again), and a `once_key`
under a partial unique index. An `Entry` now names
its item too (`ItemID`, `ItemKind`, `Number`; an item no longer stored is recorded without its
ID, as the log forgets an item that goes), so an Autopilot entry about an item is kept by the
inbox's prune (`KeptItems`), opens its item, and is marked read when that item's sheet opens,
as arrivals are. `Entry.Banner` is an entry's own choice of banner over its kind's switch (the
user's allowing banners holds either way). `Entry.Key` (the `once_key` column, unique while
set) keeps an entry once while the log holds it: a repeat is refused as told
(`ErrAlreadyTold` from `RecordEvent`), banner and all, and an entry with no key is never
refused. `Dispatcher.Tell(ctx, entry, gather)` records and shows (now, or gathered) and
returns the error recording failed with, nil for a repeat; `Notify` and `Gather` are it,
logged. The banner (`Banner.Autopilot`) and the click (`desktop.BannerData`,
`BannerClicked`, `event.RevealItem.Autopilot`, an `event.AutopilotTarget`, whose `Activity`
asks for Activity rather than one match, as the menu bar's row of what needs the user does)
carry the target. A summary of Autopilot's alone is titled *N new from Autopilot*.

**At a glance** (`glance.go`). The bound `Glance` reads the database alone: whether Autopilot
(beta) is on (`On`), the routines not deleted and those the engine runs (on, of the account
signed in, and loading; none while no one is signed in, the engine's last cycle found the
sign-in gone, or Autopilot is off), Pause Autopilot, the
open matches waiting for the user (`StateWaiting`), and the agent runs going (starting,
running, or stopping, in the background or the terminal, never a GitHub action's), oldest
first. `KeepsRunning` is a routine the engine runs or an agent working, so after a switch to
another account, whose routines don't run, or with Autopilot off, a close quits again as
before, but while an agent it started before still works.
The window reads it as it opens and again on each `autopilot:updated` (`useAutopilot`): the
Autopilot tab counts its agents working and marks its matches waiting with an amber dot. While
Autopilot (beta) is on, the window names Autopilot whether or not a routine exists: Settings →
Notifications lists the five switches, and the Notifications page offers the Autopilot type;
off, neither does.

**The menu bar** (`internal/menubar/autopilot.go`, pure). `Summarize(snap, waiting, autopilot,
now)` works out `Summary.Autopilot` from an `AutopilotState` (core's `ShellAutopilot` adapts
the glance, and hands an empty one while Autopilot (beta) is off, `shellAutopilotState`):
shown only while a routine exists, the first `PerSection` agent runs
("example-repo #88 · Claude · 6m", each opening its run) and how many more, the matches
waiting ("1 needs your OK", opening Activity), and whether Pause Autopilot is ticked;
`NextChange` is when a run's minutes next change. The count beside the icon stays the
inbox's. The tray (`cmd/nilbox/tray_desktop.go`) draws it after the lists, under its heading,
with the app's first checkbox row (`AddCheckbox`), and every click goes through
`Shell.AutopilotAction`: a run or Activity reveals its target, and Pause flips what the menu
showed through `desktop.Autopilot.SetPaused`, core's call to the settings' own
`SetAutopilotPaused`, the one writer of `autopilot.paused`, then reads Autopilot again and
draws the menu in full, so a write that failed takes back the tick the menu put on the
click itself.

**The shell** (`internal/desktop/autopilot.go`). The shell reads Autopilot's state at its
start and on every `autopilot:updated`, in its one redraw loop, whether or not the icon
shows. Core's relay sends one on each `AutopilotChanged`, so a Pause made in Settings reaches
the tick of the copy it was made in at once, whether or not that copy holds the engine; a
Pause made in another copy reaches a copy that doesn't hold the engine on its next engine
tick (`followElsewhere`, within a minute), and the copy that holds it on its next cycle. It
keeps what it last read of keeping the app running and of Pause. `HidesOnClose` answers at
once from that alone: yes while the icon shows, and yes while Autopilot has a routine on or an
agent working (no routine counts while Autopilot (beta) is off, `Glance.KeepsRunning`),
whatever the icon setting, since the close would otherwise quit the app and
quietly stop them; the Dock icon brings the window back (`ApplicationShouldHandleReopen`).
The window's close hook asks `Shell.WindowClosing`, whose pure rule is `CloseOf`
(`internal/desktop/close.go`): the close goes through while the shell is stopping (the
runtime's own shutdown closes the window once the services have stopped, so a quit's own close
never quits again), the window only hides while `HidesOnClose` says so, and otherwise the
window stays and the shell quits the normal way (`Quit`), so `ShouldQuit` asks about runs going,
or holds the quit for a move, in a window that still exists. The app never quits on the last
window's close by itself: `ApplicationShouldTerminateAfterLastWindowClosed` stays off
(`macAppOptions`), since AppKit asks it as the last visible window hides too, and with it on a
close that only hid the window ended the app a moment later. A close hidden for Autopilot
alone is noted, and the next time the window shows (`showWindow`, every way it comes forward)
the page hears `event.KeptRunning` (`app:keptRunning`) once, and says why the app kept running,
once ever (`ui.keptRunningTold`, window memory a backup never holds). A click on an Autopilot
notification or banner about an item opens that item, as an inbox arrival's does
(`bannerOpens` falls back to the item, `opensAutopilot` is an entry about no item alone), and
one about no item, as a problem is, or on a menu row of the group, opens the Autopilot tab at
what its target names (**The Autopilot tab**, below).

### The Autopilot tab

The window side lives in `frontend/src/components/autopilot/` (the tab itself,
`AutopilotView`, loaded on its first visit and kept mounted in an `Activity`) and
`frontend/src/lib/`; it reaches Go only through the platform module.

- **Autopilot (beta).** App reads the switch from the settings (`autopilotOn`, off until they
  load). Off, the window shows none of Autopilot: `AppHeader` gets `autopilot={null}` and
  draws no tab (a window on it shows the inbox, as one on a Scripts tab gone does), the rows
  get no `AutopilotRowsContext`, so no chip and no *by ⚡* mark (`useMarkedBy`), and no
  `LatestMatches` is read; the settings page leaves out Autopilot's section and group, and
  Settings → Notifications its banner switches (`hiddenOf`, `shownSection`, and the index's
  entries marked `autopilot`, which **Find a setting** leaves out too, `matchSettings(query,
  hidden)`), opening **Labs** where Autopilot's section was asked for; the Notifications page's
  Type offers no Autopilot (`AppNotificationsTab`'s `autopilot`), and a choice of it reads as
  every type; and `openAutopilot` says *Autopilot is off. Turn it on in Settings → Labs to see
  this.* rather than show the tab. **Labs** (`LabsSection`) holds the switch, **Autopilot
  (beta)**. On, everything is as below, and the tab's label and the section's heading carry a
  `BetaBadge` (rem text, the accent's tokens), beside the name, never in it, so the tab and
  the region keep their names.

- **`lib/autopilot.ts`** holds the words, all pure: the modes, the tab's badge
  (`autopilotBadge`: the glance's agents working, an amber dot for its matches waiting, a red
  one for the user's routines that are broken or have an engine problem, none for another
  account's), each routine's status line (`routineStatus`, from the routine's own `Figures`:
  what waits, what it did today, what it would have done today in Dry run, else when it last
  ran or would have run), the chip (`chipOf`, from the newest match: a Dry run's *Would run*
  while it is open or done, an open state's words, a baseline's (a Dry run's too), a closed
  or skipped match's reason (a Dry run's too), each in words of its own, `reasonChips`, then
  a local step's verb or *Undone* and *Taken back*, a notice's *Told you*, else *Ran*),
  which matches Activity lists (`inActivity`: done, skipped, or
  dismissed; `notListedText` says what became of one it doesn't), the question
  (`matchingWords`), and Activity's lines (`activityLines`: each day's grouped lines of local
  steps in place of the matches they stand for, the matches that did something by
  themselves, and the items left alone, newest first, filtered by routine and by result,
  `filters` comparing the filter by value; open matches wait for the sections that show
  them). `mergeDays` joins a day, and a grouped line, two pages split, by the group's `Key`;
  `sameSecond` compares a target's time to a match's, since a target is written to the
  second. `inChunks` splits a list of ids into the backend's chunks of 500 (`maxIDs`), for
  every read by id the window sends.
- **`lib/useAutopilot.ts`** reads the glance, the routines, the engine, and every routine's
  name (`RoutineNames`, the deleted ones' too, so a mark or a line still names its routine)
  as the window opens and on every `autopilot:updated`, for App to share, and the routines
  again as the window's clock enters a new local day, so *today* turns over at midnight
  while nothing changes; `useAutopilotChips` reads `LatestMatches` for the ids a list
  shows, a chunk at a time (`all` and `andThen` in `lib/call.ts`, which keep the bound
  calls' cancel). **`lib/useAutopilotActivity.ts`** holds one pager per tab, outside React
  (`useSyncExternalStore`), which keeps Activity as stretches, newest first, each ending
  where the next begins: Activity's newest page (100 matches) as the tab first shows, and a
  page more by `next` as the user asks for more, each a stretch of its own, never merged.
  On `autopilot:updated` it reads the newest stretch alone again: the first page itself
  (`Activity(0, 100)`, 100 rows) while nothing past it is loaded or on its way, else
  `ActivityFrom` its own first match on (`ActivityFrom(lower, 0, 500)`: the first page and
  what was recorded since, one call); past 500, that page's newest 100 become the newest
  stretch and the rest is read again in slices of 500, so nothing loaded goes and the
  newest stretch starts a page long again. Every stretch is read again, each by its own
  bounds (`ActivityFrom(lower, upper, 500)`, one call a page loaded, in slices past 500),
  once a line's own action (Undo, Try again, Let it act again) goes through and each time
  the tab shows again, since its effects stop while it is hidden; until that read lands,
  `current` is false, and a request for a match waits for it. One read runs at a time with
  one more waiting, a read of every stretch drops a read of the newest under way and one
  waiting, hiding cancels every read, and a read that lands after a page more keeps that
  page. A read again that no longer finds the match the newest stretch begins at, as it was
  recorded, finds the history cleared (Clear history lets SQLite hand the IDs out again from
  1), and starts again from the newest page.
- **`AutopilotRowsContext`** (`AutopilotChip.tsx`) carries the routines' names and the chips
  to every row, App's around the inbox and the tab, and the Watching list's own chips around
  its rows, so a memo'd `ItemRow` renders again only in its chip and its marks
  (`TriageTag`'s *Snoozed by ⚡ ...*) when Autopilot changes. The chip reads `ItemHistory`
  only as its panel opens.
- **`WatchingList`** reads `Watching`, then the inbox's `ItemsByID` a chunk at a time
  (`readItems`, again on `inbox:updated`), and renders the inbox's own `ItemRow`s, with the
  review and issue sheets over the list through `useSheetHost` (`useSheetHost.tsx`: the
  sheets, the launches resolved with `ResolveItems`, the toasts, and the rows' triage and
  edits, which are the inbox's, through `components/useItemActions.ts`, which `InboxView`
  shares). It is keyed by routine, and marks and scrolls to the row Activity's **Open** asks
  for once (`highlight`, a number per ask, handed back with `onHighlighted`), never opening
  anything itself. An ask it can't show is handed back as soon as it knows: once it has
  read what it watches without the item, or couldn't read it. The tab drops an ask as soon
  as another routine or Activity is chosen, or the question about unsaved changes that
  going to its routine asked is answered Keep editing, so none lands later unasked; while
  that question is open, the ask waits.
- **Open** on an Activity line goes to the item whatever became of its routine: its card in
  the inbox while a list there holds it; else its row in a routine that still watches it
  (`Watching`, the line's own routine first, local reads alone); else its sheet, through the
  tab's own `useSheetHost`, while the inbox stores it (`ItemsByID`, read again once on
  `inbox:updated`, which follows every change to the stored inbox, the sheet's own triage
  included, so the sheet offers what fits the item as it is); else its page on GitHub.
- **`useRoutineChange`** switches a routine, changes its mode, and saves the builder's draft
  over it: switching one on, taking one that is on out of Dry run, or saving a change to what
  one that is on watches, fires on, checks, or fires once per (`watchChanged`) first asks
  `Matching` (with the draft, for a save), and with any match, any it can't tell yet, or a
  count that may be short (`incomplete`) asks the question in a `ConfirmDialog` whose two
  buttons are the answers (`SetEnabled`, `SetMode`, or `Save` with `current`, `run` or
  `fromNow`) and whose `onDismiss` makes Escape neither. A request may name where its failure
  goes (the builder's footer, for a save), else it is a toast; `asking` says the question is
  open.
- **The builder** (`builder/`) is the routine itself: `RoutineBuilder` lays out the name, a new
  routine's mode, the sentence (`Describe`'s, the last one that read kept while the draft has
  problems; the last reading's problems and warnings, and Save, stand until the next one
  arrives), the blocks (`WatchBlock`, `WhenBlock`, `IfBlock`, `ThenBlock` with
  `AgentStepBlock`, `LimitsBlock`), `TestPanel`, and the footer with Save (⌘S, `isSaveKey`,
  listening only while the tab shows, and never while a dialog or a sheet is open,
  `anyModalOpen`) and Discard. Each block renders from the catalog, its numbers' bounds and
  defaults too (`settingRange` over `agentSettings`, `stepSettings`, and `limits`), and shows
  the problems and warnings at its own part and field (`problemsAt`), invalid controls
  described by them; any no block draws, as an imported spec can hold, show at the top
  (`unshown`). A test's findings are keyed by what decides them (`testedKeys`): its matches by
  the watch, trigger, conditions, and Once per; its searches' notes and cost by the watch; and
  its warnings, which are by step, by the steps too. `TokenField` is the keyboard-operable
  token list with suggestions, a combobox over a listbox, as `SearchField` is, leaving an input
  method's Enter and comma to it. `lib/routineBuilder.ts` holds its pure half: a draft and its
  comparison (`canonical`, which leaves out what the spec leaves out when stored), what fits
  where (`addable`, `onlyIfChoices`, from the catalog's ordering rules, as `Validate` holds
  them; `withKind`, which drops what only a pull request has from a search of issues;
  `newTrigger`, which stores the schedule On a schedule shows), the fields and operators a
  condition can take, and the words (`dryRunWhy`, `costLine`, `testHeadline`).
  `AgentStepBlock` reuses `LaunchOverrideFields` for the model and the effort, showing one
  agent's fields alone, no extra flags, and, in the background, plan mode and the worktree as
  a background run has them. `AutopilotView` holds the one draft (`Editing`, keyed by the
  routine or `NEW_ROUTINE`; **New routine** again keeps it), keys each routine's builder and
  pane by the routine so no state crosses over, asks before leaving unsaved changes, and shows
  a routine a save just made until the list read again holds it.
- **Requests.** App's `openAutopilot(target)` numbers an `AutopilotRequest` and shows the tab,
  for a notification, a menu bar row, or a chip's **Show in Activity** (offered only for a
  match Activity lists, `inActivity`). A target that names a match shows Activity
  unfiltered and reads earlier pages until one holds the match, then marks its line, says
  what became of it when Activity lists no such match (`notListedText`, as for a baseline),
  or says it is gone when none does or its time differs; one that names a routine alone
  selects it while its creation time matches, else says it is gone; any other shows
  Activity. A request that names an open match marks its card in Needs you.
- **Needs you** (`NeedsYouSection`, above Activity) reads `NeedsYou` on every
  `autopilot:updated`: a card per match waiting for the user's OK, its hold shown with the
  launch hold dialog's own parts (`HoldDetails`, shared with `LaunchHoldDialog`), with
  **Run** (`Approve`, which confirms exactly the hold shown: Run on the head, Run in the
  worktree anyway, Clone and run behind a confirmation) and **Skip**; a GitHub action's card
  says what it would send next (`githubAheadText`) and words its button by what it waits on
  (`githubGoLabel`: Send it, Send anyway, which accepts exactly the warnings its words show,
  Send it again), still `Approve`; each routine's first-pass backlog is one card
  (`needsYou`'s `backlogs`, **Matched at once**), with **Run on them** (`ApproveBacklog`) and
  **Skip them**; a card per match that can't start yet, with **Check again**
  (`CheckReadiness`), Skip, and Open; the matches queued; and, while this Mac can't run some routines, a banner that opens Settings →
  Autopilot. The routine list and a routine's header say *Not ready on this Mac* with what
  is missing, from `Readiness`, and a waiting match's chip reads *Needs you*.
- **This Mac** (Settings → Autopilot) lists `Readiness`'s checks, each with its mark, what it
  found, and its fix as a command to copy or as words, and **Check again**.
- **An item's ⋯ menu** (`RoutineMenu`) holds **Autopilot ▸**: **Run a routine ▸** (`RunNow`;
  after a refusal it asks `Explain`, and offers **Run anyway** only when the routine doesn't
  match the item) and **Why didn't a routine run?**, which reads `Explain`. It reads the
  routines through one source made once, so the rows don't render again when they change.
- **The builder's agent step** draws Where from `catalog.places` (a place this build can't
  run in is shown off, with why; a new agent step starts in the terminal), and the Test
  panel's **Preview** shows `PreviewRun`'s plan with the launch menu's own `PlannedLaunch`
  and the trace (`Trace`).
