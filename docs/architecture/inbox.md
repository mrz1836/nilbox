# internal/inbox

[Docs](../README.md) › [Architecture](../README.md#architecture) · Code: [`internal/inbox`](../../internal/inbox) · Guide: [Inbox cards](../guide/inbox-cards.md), [Filtering](../guide/filtering.md), [Snooze, dismiss, and pin](../guide/snooze-dismiss-pin.md), [The item menu and edits on GitHub](../guide/item-menu.md), [Routine searches](../guide/autopilot/routine-searches.md)

These notes cover what the inbox does for Autopilot: the routines' searches, which each
refresh reads after the inbox's own four searches, and the reads Autopilot makes through it.
The inbox's own refresh, triage, edits, and the review sheet's Actor are described in the
package's doc comments.

## Two lists from four searches

The refresh reads three searches (review requested, authored, assigned) and stores each one's flag
on the item (`reason_review_requested`, `reason_authored`, `reason_assigned`), and `inbox.beyond.*`
keeps what each matched past its page. A fourth (`reviewed`: `reviewed-by:@me -author:@me`, the most
recently updated first) lists nothing by itself: submitting a review takes the user's request off a
pull request, so one its author pushed to without asking again would leave the inbox for good. It
returns each pull request with its head commit and the user's own verdict (`github.Verdict`, from
`latestOpinionatedReviews`), which costs the query no point (measured in November 2026). It stores
`reason_reviewed` (the raw match) and, when the verdict requested changes at a commit other than the
head (`github.Item.PushedSinceChanges`, any head move by someone else: a push, a rebase, or Update
branch), `reason_pushed`, which lists the pull request in Review requested.

Commits the user wrote never count, as a merge of the base they pushed to fix its checks: the
fourth search can't say who wrote the commits between the verdict and the head without a point
more every refresh, so the refresh asks (`coverOwnPushes`, `(*github.Client).FetchCommitAuthors`:
the newest 20 commits of each, with the login each author's email matches, one point a batch of
50) only about those whose head moved past the verdict, once per verdict and head, which
`ownPushes` keeps in memory while the app runs. When the user wrote every commit after the
verdict's (`ownSince`; an author matching no account is someone else, and so is a verdict older
than the commits read), the refresh moves the verdict's commit onto the head before it stores
it, so `viewer_review_commit` holds the newest commit the verdict covers, which only this rule
reads; a read that fails leaves it pushed to, and is asked again next time. It doesn't
ask GitHub for `review:changes_requested`, which matches the overall decision, someone
else's request included.

Each row keeps the verdict (`viewer_review_state`, `viewer_review_commit`), so the card's next step
says **Re-review** in any list (`Details.PushedSinceReview`, also while `review_left_at` holds it
back), and hands the card the verdict itself (`Details.ViewerVerdict`: `changes`, `approved`, or
`""` with none, read from `viewer_review_state`, so it costs no request), which lets a card whose
user requested changes and whose author hasn't pushed since say it waits on the author, and one the
user approved say so; one past the fourth search's page holds none, and its card falls back to the
other rules. A pull request the fourth search returns alone and not pushed to is in no list: its
details are never read (`nodeIDs`, `fileTargets`), and no file check runs. `UpsertItem` gates
`reason_pushed` by `review_left_at` as it gates the request, so a review the user leaves from the
inbox (`recordReview`: `LeaveReview`, and `NoteViewerReview`, the new verdict at the head they saw)
takes it out at once and a refresh whose search trails it keeps it out until the pull request moves.

A read of the pull request alone reads the verdict too, at no extra point (`opinionsField` in the
item query beside one node, never in the followers' batches, where it would cost a point more per
25; `ItemUpdate.VerdictRead` says it was asked, so a nil verdict is none rather than unread):
`RefreshItem` (a card's refresh, `Rereads` after an agent session stops working, Autopilot's fresh
read) and the review sheet's `ReadPull`. Its writer stores it in the read's transaction
(`noteReadVerdict`, `verdicts.go`) only when the token's account, as the last refresh learned it
(`Service.viewers`, never a request), is the item's, since GitHub names the viewer's verdict by the
token. Each row keeps when its verdict was submitted (`viewer_review_at`, moving and cleared with
the verdict) and when the newest verdict seen was (`viewer_review_seen_at`, which only rises;
migration `0101_inbox`). The same review (state and second) is left as stored, so a commit
`coverOwnPushes` moved stays moved; an older one is left too (a review from the inbox since the read
went out); any other is stored whole, and none clears it (a dismissed review). A read never lists a
pull request as pushed to, which takes the whole refresh's question of who pushed, but one whose
verdict is no longer pushed past (an approval, changes requested at its head, or none) leaves Review
requested at once (`LeavePushed`, unstamped: the fourth search reads the verdict live and agrees). A
replay of the read over a whole refresh that outlived it stores its verdict again, since the
refresh's search may be older.

A pull request newly pushed to arrives as `ArrivalReviewRequested` with `Arrival.Pushed` only when
it moved since the last refresh, so the first refresh after an upgrade tells no burst.
`RemoveReviewRequest` still refuses one with no request (`ReviewRequestPushed`).

The review sheet's **Re-request my review**, on the user's click alone, asks GitHub for the user's
review again through the `Actor`, which records it at once (`Acted.Rerequested`, `recordRerequest`,
one transaction): who GitHub answered the pull request asks now (`setReviewRequests`), and
`RejoinReview`, which sets `reason_review_requested`, clears `review_left_at`, and stamps
`review_requested_at` with the later of GitHub's time of the edit and the item's, leaving
`reason_pushed`, so the card reads as an ordinary request (`ReviewRequestYou`). The sheet's reviewer
edit, which asks others, is recorded at once too, but only its answer (`Acted.RequestsEdited`,
`recordRequests`, one transaction): who GitHub answered the pull request asks after the edit's last
step (`setReviewRequests`), and nothing else, so it stays in whatever list it was in; two tries of
one edit keep the later answer (`Acted.with`). A pull request pushed to since that asks the user by
name again, as the details last read it (`review_users`), whoever asked, is
`Details.ReviewRequestedAgain`, and its card says **Review re-requested** in place of **Re-review**:
at once after the user's own re-request, and through a refresh whose search trails it, which lists
it as pushed to again until the next. It was listed already, so the next refresh tells no arrival;
it tells the outcome sink nothing (`pullOutcomes`), since it is no review, merge, or close.

Autopilot's facts and searches keep GitHub's own three lists. The snapshot alone partitions them
(`addItems`): an item that asks for the user's review, or was pushed to since the user requested
changes, is in `ReviewRequested`, whatever else it is, and one the user opened or is assigned is
otherwise in `Yours`, with `Item.Authored` and `Item.Assigned` saying which, so no item is in both
and the lists' counts add up to `NeedsYou`. `Yours.Total` adds both searches' counts past their
pages, an upper bound once either was cut. The window keys its sections by the list
(`settings.ListKey`); `settings.AdoptSections` carried the former Authored and Assigned lists'
window keys over to Yours once, and an older backup's inbox order is read the same way
(`adoptFormerOrder`). Autopilot's sources and conditions keep the three reasons
(`catalog.ListSource`), worded as reasons rather than lists.

## Lists left or joined on GitHub

The searches match through GitHub's index, which trails an edit, while each item's fields are read
live, so for a while after the user leaves an item on github.com the search still returns it, with
its new activity, and a refresh that believed it would list it again as just moved. `dropClosed`
already reads the live state for a close or a merge, and the inbox's own Unassign me and Remove me
as reviewer stamp `assign_left_at` and `review_left_at` (see `UpsertItem`), but those stamps cover
only what the user does from the inbox. A whole refresh reads each listed item's assignees with its
details, at no point more (`ItemDetails.Assignees`, see [internal/budget and
internal/github](budget-and-github.md)), and once every listed item's details are in hand (after
`readMoreDetails`, before the file checks), `dropLeft` takes out of the assigned search's results
the items no longer assigned to the viewer (`unassigned`, ignoring case) and out of Review
requested's the pull requests that no longer ask the viewer (`answeredReview`), each counted out of
its list's total (`countOut`), as `dropClosed` does. An item without details keeps the search's
word, and so does one whose read can't tell: assignees the read didn't bring in full, or a pull
request that asks a team, lists no requests, or lists `MaxReviewRequests`. The other lists and the
pushed flag are left alone, and an item in no list leaves the inbox as it would once the index
caught up, with no arrival and no stamp, so an assignment or a request made again between the
details read and the search is believed at the next refresh. A single-item read that knows the
account (`RefreshItem`, the checks follower, the review sheet's `ReadPull`, and the issue sheet's
`ReadIssue`) does the same for the assigned list as `leaveAnsweredReview` does for Review requested:
`leaveUnassigned` runs `LeaveUnassigned`, which clears `reason_assigned` and `assigned_at` of an
item in the list and stamps `assign_left_at` with its `updatedAt` as read, so a lagging search keeps
it out until it moves, and tells no outcome. The other way round, a sheet's assignee picker offers
the user themselves, and an edit that assigns them (the issue sheet's `Edit`, the review sheet's
`EditAssignees`: the user among those added, and in GitHub's answer) reports `Acted.SelfAssigned`
with GitHub's time of the edit, which `recordSelfAssigned` records at once (`JoinAssigned`, one
transaction): it sets `reason_assigned`, clears `assign_left_at`, and stamps `assigned_at` with that
time (now without one) unless it holds one. The card says Assigned to you at once, and the next
refresh, finding it listed before, tells no `ArrivalAssigned` of the user's own assignment. As with
Re-request my review, a refresh that lands inside the index's lag leaves it out once, and the one
after it, which lists it, tells it as an arrival. Assigning only others records nothing, so someone
else's assignment of the user still arrives.

## Nudges

`Nudge` (`edits.go`), on the user's click alone, posts a comment on the conversation of an open pull
request someone else opened (`(*github.Client).AddComment`, one REST request through the
`WriteGate`'s `edit`, and no read: the account check is the one every edit makes, a request only for
a token no refresh has used). It runs under the item's lock, like `Unassign`, and refuses an issue
(`ErrNotPull`), the user's own pull request or one closed or merged (`ErrNotNudgeable`), and an
empty text (`ErrEmptyNudge`). Once GitHub has posted it, one transaction records when (`NoteNudge`,
`items.nudged_at`, migration `0107_nudges`; GitHub's time for the comment, now without one) and
counts the comment on the card at once (`CountOwnComment`, `item_details.comments + 1`), so a snooze
the user takes right after records a baseline that already holds it, and a comments wake rule never
wakes on the user's own comment. A store that fails after GitHub posted says so in the window
("GitHub posted the comment, but…"), and the next refresh catches the count up. `Item.NudgedAt`
shows `nudged_at` only while it is later than the user's standing verdict (`viewer_review_at`), so a
nudge from a past round of review reads as none. The result, `Nudged`, holds the snapshot and the
comment's address. The window decides when to offer one (`lib/nudge.ts`) and fills the text in
locally from the prompt library's default **Nudge** reply, so a nudge spends one REST request and
nothing else.

## A refresh the window can watch

`runRefresh` sets `Service.refreshing`, which every snapshot read reports (`Snapshot.Refreshing`,
set on the copy returned, never on the one kept), and tells its phases as `event.InboxProgress`
through a `progress.Tracker` (the search; the details of the items new since the last refresh,
counted per answered batch through `github.DetailOptions.Progress`; the file checks, counted per
pull request), whoever started it. It tells no `InboxUpdated` as it starts, so the engine's wake on
that stays as it was, and it ends the tracker and clears the flag before the `changed()` that ends
it, on success and failure, so the update's snapshot says it ended and no progress follows it.

## A refresh GitHub failed

A whole refresh that fails for a reason a later try may well not meet (`github.Retryable`: GitHub
answering a 5xx, refusing it for its rate limits or asking the app to wait, or a request it never
answered in full) is tried again soon, rather than at the scheduler's interval (`retry.go`).
`runRefresh` records how each refresh ended in `Service.retries` before the `changed()` that ends
it: such a failure counts one more in a row and plans the next try `retryWait` after it, 15 seconds
after the first, doubling up to `retryMost` (`DefaultInterval`, five minutes), and never sooner than
GitHub asked (`github.RetryAfter`); a success, or a failure only the user can change (a refused
token, a missing scope, a store that failed), ends the plan. A refresh cut short by its ctx leaves
it as it was. The plan is kept in memory, never stored: the app's start refreshes at once anyway.

The `Scheduler` starts the try whoever ran the refresh that failed. `NewScheduler` hands the
`Refresher` its job's `Wake` (`onRetry`), which each planned try calls, and its `Every` is `due`:
the user's interval, or, while a try is planned (`retryAt`), the time from the end of the scheduler's
own last refresh to that try, so a failed click, a menu bar Refresh Now, or the app's start is tried
again on time, and never later than the interval. Each try is a whole refresh, so it joins one a
click started meanwhile and costs what any refresh costs.

Every snapshot read says the plan (`Snapshot.Retry`: the next try's time and the failures in a row,
set on the copy returned, as `Refreshing` is), and `LastError` is recorded as for any failure. The
window shows a planned try as a countdown beside the time of the last refresh (`RetryStatus`) in
place of the error banner, and the menu bar's status says when it is due.

## The searches, after the refresh's store

`Config.Watches` (`WatchSource`) gives the searches of the enabled routines of the account signed
in, each key once (none while Autopilot (beta) is off, when no copy holds the watches either:
`WatchHolder`), from the snapshot Autopilot's service keeps (core's `autopilotWatches`, which builds
it again first when the account changed, and can't answer for the account while that leaves another
account's: the refresh's ctx ended, a read failed, or the account changed again). `runRefresh` hands
the watch pass's worker (`WatchPasses`, a desktop worker the shell starts before the scheduler) a
job as the refresh starts, and the pass waits, on the worker's goroutine and outside the refresh's
`errgroup`, until the refresh has stored the inbox and told the window (the job's release); only
then do its requests go out, so they never share GitHub's time with the inbox query (beside it, the
heavier searches cost the inbox's store about 0.3 s, measured in November 2026), and a search that
fails, or a request that does, never fails the refresh: the refresh stores and returns on the
inbox's own requests, and never waits for the pass. One pass runs at a time: a refresh that finds
one running hands over no job (the scheduler never skips a tick; `periodic.RunEvery` waits after
each refresh). No job is handed over before an account is known, nor while the source can't answer
for it, so every search keeps its rows and last answer rather than being read again as new, nor,
once the searches' rows are gone, while no routine has a search. While the GraphQL budget the inbox
query just left is under a quarter of its size (and won't refill first), the pass sends no request
and only records `PausedLowBudget`, keeping every row. A pass that fails is logged, and one that
panics is recovered; either way its `WatchPass` is kept and `AwaitPass` told (`endPass`, in a
`defer`).

## The request

`github.FetchWatches` packs the searches into requests by how many results each returned last time
(`inbox.watches` keeps each search's last count; a new one counts as a full page), up to about 120
results and ten searches a request, at most four requests side by side, a point each. GitHub
resolves a request's aliases one after another and answers one that runs past about 10 seconds with
a 502 and an HTML body: measured in November 2026, five searches of 295 results in one request
failed that way every time. A request that fails, as with that 502 or with errors and no data
(`EXCESSIVE_PAGINATION`), fails each of its searches; an error whose path stops at one alias or its
`nodes` fails that search alone; one whose path goes on to a result (`["w0", "nodes", 3, ...]`)
leaves that result out and marks the search `Partial`; one with no path marks every search of its
request `Partial`, keeping the data. A query that can't be written into a request (not UTF-8) fails
its search before anything is sent, and the others go on. Only a refused token fails the call, so
`WithToken` borrows a fresh one. The searches read a lean fragment (`WatchFields`): what the inbox
stores of an item (its own fields, its state, when it closed), its head commit, base branch, and the
checks' overall state (finished checks don't move `updatedAt`, so the state must be in the search),
and its repository's node ID, owner, name, and archived flag. `mergeable`, `mergeStateStatus`,
`reviewDecision`, and the repository's other fields cost GitHub time per result, so they come with
the details, read by ID.

## The pass

Once the inbox is stored and the requests answered, `planPass` gathers the open results (a closed or
merged one, which the index can trail, only records its state, and takes the item out of the open
results another search returned; an archived repository's is left out), each node once with the
searches that returned it and whether any follows the checks, and compares each with what is stored
(`ListWatchBaseline`, a read, no lock). A listed item has its details from the refresh, and only its
rows are stored; one a later read stored (its `fetched_at` or its details' after the pass's request
went out) is left as that read stored it, and keeps no row when that read left it closed; one stored
merged keeps no row, since nothing comes back from a merge and the search's open is older; any other
is stored, and its details read when `needsDetails` says: no details; another update time, draft
flag, or state; a pull request's other head commit, base branch, or checks' state, checks still
running at the last read, or a merge state GitHub hadn't worked out; or, new to the watches, details
missing or older than `detailsStaleAfter`. Details at least `detailsStaleAfter` (30 minutes) old,
and nothing else, are read as room allows (`fillStale`, oldest first): in the room the moved items
leave in the last request, or one request when none moved, so they never add more than one, and with
some 300 watched items each comes round about every 20 passes. The details are read with the
repository whole (`DetailOptions.Repository`, the `DetailRepo` fragment), with no lock,
`watchDetailsBatch` (15) to a request (`DetailOptions.Batch`), the requests that succeeded kept when
one fails (`DetailOptions.Partial`): GitHub reads an item it hasn't lately more slowly, and four
requests of 25 newly watched pull requests side by side ran past its 10-second limit (measured in
November 2026), while of 15 none did.

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

## What keeps watched items out of the inbox

Every query that reads or writes the five reason flags (the three lists', `reason_pushed`, and
`reason_reviewed`) is decided in `reasonFlagDecisions` (`flags_test.go`),
and `TestEveryReasonFlagQueryIsDecided` fails on a query it doesn't list: the lists, the snapshot,
and the arrivals read listed items alone, so a watched item is in no list, no count, and not the
menu bar's; `PruneFiles` keeps a watched item's file check; `ListRepoCounts` counts watched items,
so a repository the user only watches can have launch defaults; `ListRunningChecks` adds the watched
pull requests a search follows (`item_watches.follow`, set for routines whose trigger reads checks),
but only while this copy reads the routines' searches (`watched`, `holdsWatches`: so in none while
Autopilot (beta) is off, when what the searches stored is stale), and `followable` leaves out a
muted repository's only for a listed one no search follows, since a routine watches a muted
repository on purpose, and takes the listed ones before those only a search follows when a round
holds more than `followMax` (50); a round reads them `followBatch` (25) to a request, side by side,
storing what the answered requests read when another fails, since GitHub works out each one's merge
state as it answers; `GetEditItem` says whether an item is watched, so the Actor acts on a watched
item's sheet as on a listed one's, while the review and issue sheets' reads take any stored item.

## The follow of a merge (`merges.go`)

A merge into a branch changes whether every other open pull request on it merges cleanly, and GitHub
works that out again only once asked, answering `UNKNOWN` for a few seconds after the branch moved,
so the cards would say what was true before the merge until the next whole refresh. Every merge the
inbox learns of is noted (`noteMerged`, which never waits): the Actor's (`recordActed`, the review
sheet's merge and Autopilot's Merge it, into the base `GetEditItem` reads), a stored single read
that finds a pull request merged (the follower's `storeFollowed`, `RefreshItem`, and the Actor's
`storeRead`, `noteMergedRead`, into the base its details read), and a whole refresh's pull requests
the search still listed though they merged (`noteMergedClosed`), whatever `tellOutcomes` says. The
`mergeQueue` keeps one follow per account and repository (its name compared ignoring case), with the
bases merged into (any base when one wasn't known), and remembers each merged pull request for
`mergeRecent` (an hour) by node ID, so a read that sees it merged again starts no follow. A follow's
first round is due `mergeSettle` (10 s) after its latest merge, so two merges in a row share it. The
`MergeFollower`, a desktop worker in its own select loop (recovering and logging a panic; its timer
tests run in a `testing/synctest` bubble), runs each round due: `ListOpenPullsInRepo` lists the
account's open pull requests in the repository on a base merged into whose details were read before
the first round was due, or that GitHub left `UNKNOWN`, the listed ones neither dismissed nor
snoozed and those a routine's search follows while this copy holds the watches (as
`ListRunningChecks`, and `followable` leaves out a muted repository's listed ones and takes the
first `followMax`); `followRows` reads them `followBatch` to a request, side by side, under their
locks, as the checks follower reads its own, with `github.PurposeFollower`, then settles and tells
the window as a follower round does. A round whose stored answers left GitHub still working one out,
or that failed, has the follow read again `mergeRetry` (30 s) later, at most `mergeRounds` (3)
rounds in all; a merge noted while a round ran keeps its own round. It costs about a point per 25
pull requests a round, and nothing while no merge happened.

## The issues a merge closes (`closes.go`)

GitHub closes an issue a merged pull request closes (its `ClosingPull`) a second or two after the
merge, so the issue's card would say a fix is on its way to an issue already closed until the next
whole refresh. Every merge `noteMerged` notes the first time also owes its issues a follow in the
`closeQueue`, one per account and merged pull request (its `item.Ref.Key`), which wakes the
`MergeFollower` on the merge queue's channel; the follower runs these rounds first, since a card
waits on them. A round is due `closeSettle` (5 s) after the merge: `ListClosingIssues` lists the
account's open listed issues, whatever their triage, since a closed one leaves every tab, whose
`closing_pulls` are not empty, `closedBy` keeps those naming the merged pull request (its repository
compared ignoring case, so an issue in another repository counts too), and `followRows` reads and
stores them as the merge's own follow reads its pull requests (`followRow.Kind` names an issue, so
the outcome told is one). A stored answer that finds an issue closed takes it out of the lists, as
any read does; one that finds it still open, or a round that failed, has the follow read again
`closeRetry` (5 s) later, at most `closeRounds` (3) rounds, so an issue a merge into a branch other
than the default leaves open waits for the whole refresh. The inbox's own store says which issues
name the pull request, so a merge none names asks GitHub nothing, and one some name costs about a
point a round.

While a follow is owed, every snapshot marks `Item.Closing` on the listed issues whose closing
pull requests, as their details were last read, name its merged pull request, so the window fades
their cards under **Fix merged · closing on GitHub…** from the moment the merge is noted rather
than leaving them as they were for the seconds until a round reads them closed. The mark is run
state, as `Refreshing` and `Retry` are: `withRunState` asks `closeQueue.mark` for it over a copy of
the kept snapshot, under the queue's lock, with the account the snapshot was built for
(`cachedSnapshot.accountID`), so another account's merge marks nothing, and a list it marks an
item of is copied before the write, never the kept one. Every place that notes a merge
(`noteMerged`) stores and tells the window after it (the Actor's `changed`, a follower round's, an
item's read, the whole refresh's ending update), so the snapshot that takes the merged pull request
out already shows its issues closing, and noting asks nothing more. `mark` also records which of
the account's follows the snapshot showed (`closeNote.shown`, set afresh by every snapshot read),
and `done` reports a follow that ends while the last snapshot still showed one of its issues
closing: an issue GitHub kept open, a last round that failed or panicked, or an answer a later
refresh superseded. The `MergeFollower` then tells the window (`ended`, the Service's `notify`),
so the card shows the issue as it is. A follow whose issues all closed tells nothing more, since
its round's own update took them out, and one that showed nothing tells nothing.

## A bot author's picture

The item fields every inbox read shares (`ItemFields`) ask a bot author for its picture's URL, at no
point more, and `items.author_avatar_url` keeps it, `''` for any other author (migration
`0101_inbox`): the refresh's `UpsertItem` and an item's own read (`UpdateItem`) write it, a
routine's search (`UpsertWatchedItem`, whose lean fields don't ask) leaves it, and `UpdateItem`
keeps the stored URL when a read brought none for the same author, since a whole refresh applies a
pass's read again over its own. The lists and a routine's Watching list carry it as
`Item.AuthorAvatarURL`, which the window hands the avatars service for a bot's picture on its card.

## The seen stamp (`seen.go`, migration `0101_inbox`)

`items.seen_activity_at` is the activity on an item the user last saw, and `Item.Unread`, in the
lists and a routine's Watching list alike, is true while it is NULL or `item_updated_at` is later;
the window shows it as a dot on the card while **Unread dots** (`cards.unread`) is on. Besides the
marks, only a new row's insert writes it: a whole refresh inserts NULL, unread, except on an
account's first load (`noteFirstLoad`: no item stored for it before that refresh, `HasItems`), when
it inserts the item's activity, so a first sign-in isn't a wall of dots; a routine's search
(`UpsertWatchedItem`) inserts the activity, since a watched item isn't news; and no `ON CONFLICT`
update and no `UpdateItem` touches it, so newer activity from any read makes a card the user saw
unread. `StoreFixture` follows the refresh's rule. The bound `MarkSeen(ids)` sets each of the
signed-in account's rows among ids to the later of its activity and now (the injected clock), so a
read that lands after the mark with activity from before it stays seen: opening a sheet runs `Open`
and then a `Refresh` that can store a newer `item_updated_at`, which a stamp of the activity alone
would leave unread while the sheet is open. `MarkUnseen(id)` sets one row to NULL. Another account's
rows are left alone. A mark that changed a row empties the snapshot cache, so the next `Snapshot`
shows it, but emits no `InboxUpdated`, which would wake Autopilot's engine (`engineOnInbox`) on
every sheet opening; the window changes the card optimistically and reverts it on an error, as it
does triage. The window, not the `Actor`, counts what the user saw and did as seen: it marks an item
as its sheet opens (from the inbox or a routine's Watching list) and again for the item a sheet
steps away from or closes on, which catches what the sheet read while open and the user's own
actions in it, as the user opens it on GitHub, and after a card's own edits on GitHub succeed,
whatever the setting, which gates only the dot and Mark as read and Mark as unread. The `Actor` is
shared with Autopilot, whose steps aren't the user's, and most of its actions record no time of
GitHub's, so it can't say which activity was the user's own. Something the user does on github.com
itself reads as unread, since GitHub's last activity doesn't say who acted. Nothing of it goes to
GitHub.

## State, arrivals, and the prune

`items.state` and `closed_at` are written by every read that carries them: the inbox's searches
(`UpsertItem`), an item's own read and the follower's (`UpdateItem`, `UnlistItem`, which also drops
a closed or gone item's watch rows), the closed items a refresh's searches returned (`SetRefState`),
the routines' searches and a test's (`SetRefStateBefore` for their closed results), and the states
the worktree sweep, the notifications mirror, and Autopilot's engine read (`Store.NoteStates`,
through core's `itemStates` and `stateNoter`). Every one of them stamps `fetched_at`, and none
writes over a `MERGED`, which is final: not `UpsertItem`, `UpdateItem`, `UnlistItem`,
`UpsertWatchedItem`, `SetRefState`, `SetRefStateBefore`, or `SetItemState`. A close can open again,
so it is ordered rather than guarded: another service numbers its read in the inbox's write order as
it goes out (`Store.StartRead`, a `StateRead`; core hands the Service its Store as `Config.Store`,
so the two share one order), and `NoteStates` stores by it as a pass does: an item stored at or
after the read went out keeps that newer read (`SetRefStateBefore`), an item's own read that went
out before it stores nothing over it (`stated`), and a whole refresh still fetching that went out
before it applies the state again on top of its own. `review_requested_at` and `assigned_at` keep
when the inbox first saw an item ask for the user's review and assigned to them while it stays so,
cleared once it leaves (`ClearArrivalStamps`, and every query that takes it out), for the routines'
fire keys. Each whole refresh prunes, in its own transaction, the items in no list, watched by no
search, with no triage, that no read brought for 30 days (`PruneItems`); what else refers to an item
comes from `Config.Keep` (`ItemKeeper`), read before the transaction, so another owner of a
reference extends the rule there. Core hands it `inbox.Keepers` (one failing fails the whole, and
then the refresh prunes nothing, keeping everything, logs why, and goes on) over the agent sessions'
items (`sessions.Store.KeptItems`: `agent_sessions.item_id` has no foreign key, so a pruned item
would leave a dangling id, or a reused one) and the notifications' (`notify.Store.KeptItems`:
`notifications.item_id` would be cleared, and the entry would no longer open its item), and
Autopilot's ledger, which core hands in as the third. A worktree record and a GitHub notification
thread name their item by repository and number, so a pruned item comes back for them with the next
read. The rows that hang off an item by its id go with it, by their foreign keys' cascade: its
details, file check, and check names, and the review sheet's `review_diffs` (with
`review_diff_files`) and the issue sheet's `issue_views`, which are only caches and are read again
on the next visit. (An item with watch rows or local state is never pruned.)

## Held workflow runs (migration `0101_inbox`)

A details read stores the runs GitHub holds for approval apart (`github.Checks`):
`item_details.checks_awaiting` counts those the user can approve from the review sheet,
`checks_awaiting_on_github` those GitHub approves only on its site, and `checks_held` lists each
with its workflow's name. The snapshot counts them again as each card is built, with
`heldruns.Count` over the held runs, the stored `merge_state`, and **Ignored workflows**
(`checks.ignoredWorkflows`, in the snapshot cache's `settingsKey`, so a change of the list builds
the next snapshot again from what is stored and asks GitHub nothing; the window reads it again as it
saves the settings, `inboxKey`): a run of an ignored workflow never counts, and one held on GitHub
counts only while GitHub reports the merge `BLOCKED`. The snapshot's `Checks.Awaiting` and
`AwaitingOnGitHub` carry what counts to the card, whose **{n} run(s) await your approval** (rule 6)
counts the first alone, while its track marks both held and says the second as **{n} workflow run(s)
waiting for approval on GitHub**; a run that doesn't count is in neither, so its checks read as they
are, its worst detail and rule 15 never see it, and a fork's approved pull request whose only held
run is one a review started reads **Approved · ready to merge**. A routine's facts count every held
run (`checks.awaitingApproval`), ignored or not, since either way the checks haven't all run. A row
stored before the squashed `0023_checks_awaiting_on_github` counts every held run as approvable,
and one stored before `0024_checks_held` has no names, so
its runs match no ignored workflow, until the next whole refresh reads its details again.

## The reads for Autopilot

The bound Service adds only `ItemsByID`, the rows of a routine's Watching list (listed and watched
items alike, as a snapshot shows them). Everything else is off the window: `Store.FactsRows` (one
query, `ListFactsRows`, into `facts.Row`, with the repository rules applied), `Store.FactsEnv`,
`Store.WatchedItems` (a routine's lists and its searches' rows, stale ones included),
`Store.Suggest` and `KnownOwners` (the builder's suggestions, labels and check names counted in Go
from their JSON columns), `Store.WatchStatus`, and, through `inbox.Routines`, `SearchNow` (a draft's
searches read now and stored as a pass stores them, `recheck` and the write order included, with no
rows, so the prune takes them in time; the details of an item no search watches are read only when
missing, older than `detailsStaleAfter`, or its state moved, since the passes keep the watched ones
current and a test only looks, and its card is stored all the same; nothing is sent while the budget
is under a quarter, `ErrLowBudget`) and `ReadChecks` (`github.FetchChecks` into `item_checks`,
returning what it cost, which a test adds to its points, and `ErrLowBudget` too).

## A routine's triage (`triage_routine.go`, migrations `0101_inbox` and `0103_autopilot`)

Autopilot's local steps run the inbox's own triage through `inbox.Routines`, never the bound
Service: `Triage` runs a Pin, a Snooze (for the routine's length, recording the item's activity as
the user's Snooze does, so the user's wake rules end it early), a Dismiss, or a Restore on one item,
marked with the routine (`item_local_state.set_aside_by` for a dismissal, a snooze, or a routine's
Restore, and `pinned_by` for a pin: two columns, since a pin is independent of a snooze or a
dismissal on the same row, each `REFERENCES autopilot_routines ON DELETE SET NULL`). In one
transaction it reads the item's local state (`LocalState`), works out the change, hands it to the
request's `Record` before writing it, so Autopilot's effect and the change are stored together or
not at all, then writes it.

Each step is a compare-and-set: `RoutineTriage.Expect` is the state the routine decided on
(`Routines.LocalStates`, which the engine reads before an item's facts), and a step whose part of
the triage (the pin, or the dismissal and the snooze) differs now changes nothing (`KeptChanged`),
nor does one whose routine's own mark, on either part, was there when it decided and is gone now
(`lostMark`: the user unpinned what the routine pinned before its next step landed), so the user's
own change in between always wins. A routine's Snooze and Dismiss never replace what the user set
aside: they leave an item the user dismissed or snoozed (still set aside, with no routine's mark) as
the user left it (`KeptYours`); its Pin leaves a pinned item (`KeptPinned`), and its Dismiss a
dismissed one (`KeptDismissed`). Its Restore is for bringing an item back to the user's attention
when the routine's conditions hold, the user's own snooze or dismissal included, and has nothing to
do on an item not set aside (`KeptNotSetAside`); each that changes nothing records nothing.

A routine's Restore keeps its mark on the row with nothing set aside, which `PruneState` then keeps,
so its Undo can tell the item is still as it left it, until the user's own triage of the item clears
it, or a whole refresh drops it once it is older than Autopilot's history (`PruneRestoreMarks`,
`autopilot.historyDays`, read through `autopilotprefs`), unless Autopilot still offers that
Restore's Undo (`Config.Marks`, a `MarkKeeper`: core's `engineWaker`, answered by `Engine.KeptMarks`
from the effects neither undone nor taken back, read before the refresh's transaction; one that
can't be read drops no mark); it tells the outcome sink of the restore, as the user's does, so the
mirror of the GitHub notifications takes a dismissal's clears back, and no other routine step tells
anything.

`UndoTriage` puts back, in one transaction and newest first, each change's state before, only where
the item is still as the change left it (`asLeftBy`: the pin and who pinned it, or the dismissal,
the snooze's end, and who set them, so the user's own triage, a snooze the wake rules ended, or a
later step leaves it as it is), and hands its `record` which it put back; a routine named in a state
put back that is gone for good is dropped from it. Undoing a routine's Restore of the user's own
dismissal tells the outcome sink of that dismissal again, at its own time, so the mirror records it
as the user's and clears by the user's own rules, as before the Restore; no other Undo tells
anything. Two edges of that, accepted: a clear the mirror marked kept while the routine's Restore
stood stays kept after the Undo, so it sends less, never more; and a dismissal whose routine was
since deleted for good reads as the user's (its mark is gone), so undoing a later Restore of it lets
the user's own rules clear that thread, only ever after the user's own Undo. `AsLeft` asks the same
without writing, for Activity's Undo.

The snapshot carries the marks (`Item.SetAsideBy`, `Item.PinnedBy`). Every triage path of the user's
own clears the mark it changes (`SetTriage`, `Pin`, `Unpin`, and `Restore` all write it), so
"Snoozed by a routine" never outlives the user's own re-snooze, takes its time once its transaction
holds the write lock, so a reversal's time comes after every routine's step that committed before
it, and tells the outcome sink before the window and the engine hear the inbox changed; one that
takes back what a routine did tells the outcome sink with `Outcome.Reversed` (the routine, the item,
and what it did): a Restore of what a routine dismissed or snoozed, still in effect; a Snooze over
that (`OutcomeSnoozed`, told only then); and an Unpin of what a routine pinned (`OutcomeUnpinned`,
only then). Setting aside again what a routine's Restore brought back is the user handling the item,
never a reversal, and going further than a routine (a dismissal over its snooze, the user's pin over
its pin) takes nothing back either, nor does snoozing again what a routine snoozed once that snooze
has ended.

## Check names

`github.FetchChecks` reads each pull request's checks on its head commit, a fragment of its own
(`CheckFields`) modeled on the review sheet's `checkList` selection, with each run's `startedAt` and
`completedAt` and each status's `createdAt`, under one alias per pull request (`node(id:)`) in
batches of 25, four side by side, a point each: whether a check is required takes the pull request's
own ID (`isRequired(pullRequestId:)`), which a fragment can't take, so each alias asks for it beside
the fragment, over the same contexts in the same order. A name repeats, once per event that ran it;
`item_checks` stores every run, and `facts.Build` dedupes them. `HeadChecks.Settled` is the newest
completion once every check has finished, stored as `settled_at`, by the facts' own rule
(`HeadCheck.Running`): a check runs while it is pending, or has no completion time and wasn't
skipped. A head with more than a page (100) of checks is read in part (`Complete` false): its names
are partial, so a negation over them is unknown, and it settles once every check read has finished
and the rollup over all of them (`HeadChecks.Rollup`, and the details' checks state for the facts)
says the rest have too, at the newest time read.

## The budget's purposes

Each request's context names what it was sent for (`github.WithPurpose`): the refresh is
`PurposeInbox`, the watch pass `PurposeWatches`, the checks follower and the follow of a merge
`PurposeFollower`, and `SearchNow` and `ReadChecks` `PurposeAutopilot`, as are the reads and writes
core makes for Autopilot's engine and its steps; anything else is `PurposeOther`. The client hands
the purpose to the `Tracker` with each reading (`RateReading.Purpose`, which the tracker strips
before it keeps the reading), and the tracker splits each budget's spend by it (`Reading.Purposes`,
kept across runs, a state saved before the split, and a purpose this build doesn't know, counting as
the rest), so the shares add up to `Spent`. The budget menu's one line for Autopilot is `watches`
and `autopilot` together, shown only while they are above zero; the follower's rounds, which read
the watched pull requests a routine follows in the listed ones' requests, stay `follower`.

## What a repository's launch reads

`Store.RepoFacts` (the query `GetRepoFacts`, beside `GetRepoDefaultBranch`) reads what the last
refresh stored about a repository, matched ignoring case, from the database alone: its default
branch, the viewer's permission, whether GitHub has it archived, disabled, or locked, and why, and
whether it is a fork, and of what (`RepoFacts`, with `Found` false for one the inbox never saw).
Core converts it field for field into the launcher's `StoredRepo` (`repoFacts` in
`adapters_launch.go`), which a repository's launch checks ([internal/launcher](launcher.md)), so the
launcher never imports the inbox.
