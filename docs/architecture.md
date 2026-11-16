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
`Refresher`, which reads that repository's row again); a close tells it nothing. Nothing
here lets an agent approve, merge, or close.

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
thread that isn't a pull request's or an issue's); `Keep` takes queued clears back; and
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
  settings, the limits, and their ranges.

`GitHubActions` lists no entry until the GitHub dispatcher is built; the spec's format
already has the four actions (rebase, updateBranch, reply, labels). The catalog has no entry
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
holding a quote, a comma, a backslash (which could read as an escaped quote), or a control
character, so no value can smuggle in a qualifier; quotes any value holding a space of any
kind, Unicode's included; passes the head branch's prefix through exactly, quoted, since
GitHub reads `head:` as a plain prefix, refusing in a branch any space, quote, or
backslash, since the base goes in unquoted; writes a bot as `author:app/<slug>` (the bare
slug matches nothing, so
`Normalize` writes `dependabot`, `renovate`, and `github-actions` as apps and `x[bot]` as
`app/x`); refuses a hand-typed qualifier it doesn't list, free text, a backslash, and the
search's own (`is:` but public, private, locked, or unlocked, `sort:`, `archived:`, `in:`);
and is at
most 256 characters, refused over that with the hint to split it. GitHub doesn't error on
a longer query, it answers nothing, which is why the cap holds the whole query. "Your
account" (`@me` in Where) becomes `user:<login>` in `Build`, so a routine follows
whichever account is signed in, while `Check`, which `spec.Validate` calls, builds with the
longest login GitHub allows, so a search it passes fits any account. The query is written
in one canonical order from the normalized search, so it is its own dedup key
(`Query.Key`).

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
  forward yet.
- `Sentence` writes the one sentence every place shows, from `Phrase` for each condition,
  with `PromptNames` naming a starter as the library does; `TestSentenceGolden` pins each
  template's.

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
keeps its name and sentence; names are unique, ignoring case, among the routines not deleted
(a partial unique index, and a worded check before it). Its queries are in
`internal/autopilot/internal/queries`, generated by sqlc.

**The bound Service** (`List`, `Get`, `Save`, `Delete`, `Duplicate`, `SetEnabled`, `SetMode`,
`Reorder`, `Catalog`) never returns a nil list, and words every refusal with `uierr`.

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
  a backup, which name none) becomes the editor's.
- A routine whose steps include a kind the catalog doesn't offer yet (`Step.Offered`, or a
  GitHub step with an action `GitHubActions` doesn't offer) runs in Dry run alone:
  `Routine.DryRunOnly` names the kinds, `Save` stores it as `dryRun` whatever the mode, and
  `SetMode` refuses Ask first and Autopilot for it with a sentence naming the steps. As each
  dispatcher is built and its steps offered, the same routines take any mode.
- `List` marks a routine of another account than the one signed in (`OtherAccount`; logins
  compare ignoring case, and with no one signed in none is another's). It doesn't run.
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
run keeps it in Dry run), its spec compacted but as it was, so a newer build's stays broken
and unrewritten; a spec that isn't JSON at all is left out, and two of one name are told
apart by a number. Core (`internal/core/kept.go`) runs it after the prompts and the launch
defaults, so the prompts a routine names by nickname are back first, and writes the copy at
startup only when it is missing or routines were just restored, so a loss in the database
never overwrites it. The window's restore notice counts them (`appinfo.Restored.Routines`).

**Backups.** The Autopilot section (`backup.SectionAutopilot`) is the export format,
through `NewBackupPart`, a type of its own that core adapts in `adapters_backup.go`, never a
method of the bound Service. `PlanBackup` leaves out, with a note, a routine with no name, a
second of a name, and one whose spec won't load (as a newer build's); a mode this build
doesn't know takes the mode new routines start in. Routines are matched by name, ignoring
case: one whose definition, description, and mode are the file's is left as it is, any other
is saved over and switched off, and one this Mac lacks is added, off, at the end (Merge) or in
the file's order (Replace, which also deletes, softly, every routine the file doesn't name).
`BackupDone` writes the copy again.

**The settings** are `internal/autopilotprefs`'s, a leaf that imports only `prefval`:
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
logs past the history are refused. Backups carry them as `Portable.Autopilot`, the history
and the logs checked together against this Mac's, and the bug report states them, with the
routines counted by mode, by switch, broken, and another account's, none named.
