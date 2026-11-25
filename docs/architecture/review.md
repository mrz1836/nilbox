# internal/review

[Docs](../README.md) › [Architecture](../README.md#architecture) · Code: [`internal/review`](../../internal/review) · Guide: [Reviewing a pull request](../guide/reviewing-a-pull-request.md)

`internal/review/` is the review sheet's service. `Decide` is pure: from a pull request's facts it
works out every action's offer and the one primary, and `Act` runs it again before any write. An
offer with warnings asks first (`offers` sets `Offer.Confirm` for it), since `Act` refuses it unless
the request accepts them; the window confirms a merge with its methods and any other action with
none.

Asking Dependabot to rebase is offered on every open Dependabot pull request, enabled once the sheet
read it, for a user with push access, while the branch is behind or has conflicts; Update branch
whenever GitHub's `viewerCanUpdateBranch` lets the user (with a warning on Dependabot's), and,
disabled with why, on any other pull request GitHub reports as behind or the compare counts behind
(`Facts.BehindBy`). Each is the primary only while the pull request is behind (the rebase also with
conflicts), never as a fallback: with no primary and offers enabled, the sheet's button only opens
the menu.

How far behind comes with the one read: `baseCompare: baseRef @include(if: $compare) {
compare(headRef: $compareHead) { behindBy } }`, with `$compareHead` the pull request's
`refs/pull/<n>/head` (`github.PullHeadRef`), which GitHub resolves for a fork's pull request too,
where the branch's name doesn't; the read still costs one point (measured in November 2026 on a
same-repository pull request and a fork's), and a compare GitHub can't resolve comes back null
beside a not-found error that leaves the rest of the answer, so `PullReview.BehindBy` is 0. The
cache keeps it (`pullContext.BehindBy`, 0 in an older build's), and `Review.Behind` (open, and
`viewerCanUpdateBranch`, `BEHIND`, or a count) with `Review.BehindBy` feed the sheet's strip
(`BehindBase`), which offers the rebase on Dependabot's, else Update branch, or why not, in amber
only while the readiness is `behind`. Update branch merges the base in (REST `PUT
.../update-branch`, pinned with `expected_head_sha`); it never rebases.

Once the author pushed since the user requested changes (`Facts.ChangesRequestedSince`: `YourReview`
is CHANGES_REQUESTED with a `Since`), `rerequest_review` ("Re-request my review") is offered on
someone else's pull request, disabled while the card's `review_users` name the user already
(`Facts.AskedAgain`) or without write access, never the button and run only from the menu or the
strip of what came since (`menuOnly`). `Act` asks once (`(*github.Client).RequestReview`, REST `POST
.../requested_reviewers` naming the account the inbox checked the token against, `Target.Login`,
through `edit`; GitHub's answer must name the user, else `ErrEditIgnored`), never again across the
token retry, and hands the inbox who GitHub asks now (`Acted.Rerequested`, `ReviewRequests`,
`RerequestedAt`), which puts the pull request back in Review requested at once
(`review_rerequested`). It dismisses nothing, sends no text, and Autopilot's GitHub step can't reach
it (`reviewActionOf` allows only the rebase, Update branch, and a comment).

Every write goes through the inbox's `Actor` (the item lock, the account check, and the token retry,
never bound, since its methods take funcs), is pinned to the head the user saw (an approval reads
the head first, since GitHub takes a review of an older commit), and a merge first reads
`mergeStateStatus` fresh and merges only when GitHub says it can, so an admin's token never merges
past branch protection on its own: only `merge_bypass` ("merge past the rules") does, offered when
GitHub sets `viewerCanMergeAsAdmin`, with each rule it skips to accept, and never the sheet's button
unless the user turns on `review.bypassOwn` (`Prefs.BypassOwn`, which `Decide` takes with the merge
method): then `primary` puts it first on the user's own pull request that waits only on an approval
(`waitsOnReview`: BLOCKED, an approval required, no required check failing or pending, no changes
requested), and it still asks first.

The one other `merge_bypass` is a routine's (`ActRequest.Routine`, `json:"-"`, so the window can't
send it; `pastrules.go`): `Act` refuses it unless the read finds the user's own pull request in that
same state, offered past the rules (`OwnAwaitsApproval`, which the sheet also returns), warning of
nothing but the approval (`OwnApprovalSkipped`) and, with `RoutineMerge.Sensitive`, the sensitive
files (`SensitiveWarning`, `StrayWarning`); never takes its `Accept` for accepted failing checks;
and its guard reads `FetchOwnMergeState` instead and merges past the rules only when `pastRules`
finds every check holding: the viewer opened it; every commit, at most 100, has a signature GitHub
verified whose signer is the viewer; each ruleset rule is one it can account for (`allowedRules`:
the pull request rule, required checks and signatures, no force push, no deletion, creation, the
push-time file rules, Copilot's review, and a linear history for squash or rebase) and classic
protection neither requires deployments nor locks the branch; BLOCKED, `viewerCanMergeAsAdmin`, and
`REVIEW_REQUIRED`; the rollup SUCCESS with each required check (the rules' and classic protection's)
present by name and passed; and no review thread unresolved. A list longer than its page, and a
null, fail closed. GitHub still working it out (UNKNOWN, a rollup pending, expected, or missing) is
`ErrPastRulesNotYet`; anything else `ErrPastRulesRefused`, which wraps `ErrNotOffered`. A state
GitHub would merge anyway merges as any other.

An UNSTABLE pull request's readiness line and merge warning say what GitHub counts as not passing:
optional checks failing or running, and the head commit's workflow runs that ended without a check
(`Unrun`, read in the review query from the head commit's own check suites, `headSuites:
commits(last: 1)`, which the details fragment asks for under the same alias and arguments, so
GraphQL merges the two and a fork whose rollup is null still shows them: a fork's runs awaiting
approval, each with its run's ID and page, or runs that failed to start), which the sheet names
beneath its checks, each a link to its run.

Runs awaiting approval are approved with `approve_runs` ("Approve workflows to run"), offered in a
bar under the sheet's header and in the menu, never the button: held back until the sheet read every
one's run ID (a cache an older build kept has none), for a user with write access, and asking first,
with a warning, when the pull request changes workflow files. A run GitHub approves only on its site
(`Unrun.OnGitHub`, from `github.UnrunSuite.OnGitHub`; see internal/github) is never offered
(`Facts.OnGitHub`): the bar lists it apart as waiting for approval on GitHub, with **Open on
GitHub** (the run's page, or the pull request's checks for several) and no button, and an approval
names only the others.

A held run that doesn't count (`Unrun.Ignored`, marked as the sheet is built, by `Open`, `Refresh`,
`Rebuild`, and `Act` alike, from `heldruns.Ignored` with the settings' ignored workflows and the
merge state the sheet read) is left out of the readiness (an UNSTABLE pull request whose only
unpassing statuses are such runs reads **Ready to merge**), never offered to approve nor named by an
approval, and not in the bar: the checks list names it quietly, a line a run (**{workflow} · held on
GitHub** or **· held for approval**), with Open on GitHub, and Ignore this workflow while it isn't
named. A merge's warning still says every held run, since GitHub still counts it. `Rebuild` builds
the sheet again from the cache, with no request and no new visit, which the window asks for when the
list changes while the sheet is open.

`build` takes the inbox's stored state (`StoredPull.State`, the item's `state`, which a merge or
close the inbox recorded sets, as a routine's merge does, dropping the cache): MERGED or CLOSED ends
the sheet as a read that finds it would, so an open sheet whose card leaves the list moves on, with
no request.

Should GitHub still refuse one for what it is (`github.ErrRunNotApprovable`), `Act` remembers its
run ID for as long as the app runs (`Service.onGitHub`, in memory, never stored; `markOnGitHub`
applies it to every cache the sheet loads, over any read since), never sends it again, and goes on
with the rest; with none approved, the result is `runs_on_github` with a short note ("Auto-merge
Trigger can only be approved on GitHub"), not an error, and the sheet reads again to list it there.
`Act` approves the runs the cache read at the head the user saw (`loadAct` refuses another head as
`pushed_since`), one REST `POST .../actions/runs/{id}/approve` each
(`(*github.Client).ApproveWorkflowRun`, through `edit`), never one twice across the token retry;
once GitHub approved any, a refusal of the rest is the result's `Reason` (`runs_approved`, "Approved
2 of 4 ..."), not an error. The sheet stays open after it, with the draft, and reads again
(`ReadAgain`) at once, then three seconds apart up to four more times, until fewer runs are held and
a check is running; each read rewrites the card, which the checks follower then keeps current. Each
step is reported as an `event.ReviewProgress`, which the sheet shows while the user waits.

Line comments are checked against the cached diff (`hunks.go`) before anything is sent; a routine's
Post the agent's review first keeps only those the diff shows (`Fitter.Fit`, never bound, the same
`checkComment` over the cache alone, counting the rest), so its review never fails whole on a line
GitHub would refuse. `Decide` marks every offer with what its text goes with (`Offer.Reviews`,
`Offer.CommentsFirst`, and `Offer.CommentBlocked`, why GitHub can't take the comment now), and `Act`
and the window both go by those marks.

A merge with no review of its own (`merge_now`, `merge_bypass`, `auto_merge`) posts the composer's
text and line comments, when there are any (by `textutil.HasText`, which trims as the window's
`trim()` does), as a COMMENT review at the head the user saw, the way `comment` does (never an
approval, so it works on the user's own pull request), but only once the fresh merge state says
GitHub would merge (auto-merge: that it is open, not a draft, at that head), so a refused merge
strands no comment; then it merges exactly as it would without them: no auto-merge fallback, and
`merge_bypass` still needs its rules accepted. A comment that fails merges nothing, and once one is
posted nothing after it is an error: a merge GitHub won't make, or any failure, is
`commented_not_merged`, a result whose words, like `Result.Commented`, say the comment was posted,
so the sheet drops the text and never posts it twice.

A close (`close`) is offered on an open pull request, held back ("Wait for it to load") until the
sheet read it, then only when GitHub's own `viewerCanClose` says so (`Facts.CanClose`, read by the
review query and kept in the cache), and always confirmed. It is never the button: the window runs
it only when picked from the menu (`menuOnly`), never keeps it as the chosen action or runs it with
⌘↩, counts it as nothing to do, and sends with it only text the user wrote or picked, never a
default reply. GitHub takes no head to pin a close to, so every try (a retry with a fresh token
included) reads the head first (`FetchMergeState`): one no longer open is refused, and one pushed to
is `pushed_since` with nothing sent, or, after the comment, `commented_not_closed`. Then it posts
the text and line comments as a COMMENT review the same way, and closes with one REST `PATCH`
(`(*github.Client).ClosePull`, whose refusals `closeError` words as a close, never as a merge or a
push). A comment that fails closes nothing, and anything that fails after it is
`commented_not_closed`, decided in one place (`failedAfterComment`).

A merge or a close takes the pull request out of every list (`Acted.Merged`, `Acted.Closed`) and
drops its cache. So does a read that finds one merged or closed, as a card still listed can be until
the next refresh: the inbox's `ReadPull` stores its state from the answer, which takes it out of the
lists and tells the window, and `Refresh` (and `ReadAgain`) returns it built from that read alone
(`endedRead`: its context, with no files, since a pull request no longer open is never listed), its
`State` saying `MERGED` or `CLOSED`, as the issue sheet returns a closed issue. The window learns it
from the read it makes on every opening, with no request more (built from the card alone, with no
cache, it would read as open, and the window would read it twice more for a merge state GitHub never
works out). A read GitHub can't resolve still returns the card.

It owns `review_diffs` and `review_diff_files`, a bounded cache of the diffs it read (per pull
request, in all, and by age), pruned in the same transaction that writes. The one read of the pull
request (`FetchPullReview`, one point, measured) brings its conversation too: the newest hundred
entries of its timeline (comments, reviews with their first twenty line comments, each with the last
lines of its diff hunk, and events; its commits left out), counted by GitHub's `filteredCount`,
since a pull request's `totalCount` counts every type, and twenty when the answer is too large, with
the description's edited time and reactions. Reviews still pending, and ones that only comment and
hold nothing, are dropped; a review of another commit than the head is marked `Outdated`. The
context the cache keeps holds the conversation (`timeline.Entries`), cut to fit 1 MiB (`fitContext`,
`timeline.Cut` at 16 KiB, then 4 KiB, then nothing, each cut marked) on every write, and a read
under 20 seconds old is served again only while the card shows no activity since it (`recent`), so a
new comment always shows. `Open` starts a visit as the issue sheet's does: `review_diffs.seen_at`
holds GitHub's `updatedAt` of the read it shows, which only `Open` moves (`Review.LastSeenAt`).
`Earlier` reads the conversation's page before a cursor (`FetchTimeline`) through the inbox's
`PeekPull`, and stores it nowhere.

The commits stay out of the conversation: the same read lists the newest hundred (`commitList:
commits(last: 100)`, each with its headline, its committed time, and its one author, never the
authors' list, which costs points) and every reviewer's latest verdict (`latestOpinionatedReviews`,
each with `viewerDidAuthor`), from which `github.Verdict` keeps the user's own: the latest that
approved or requested changes, which a later review that only comments, as a reply to a line
comment, doesn't hide as it hides `viewerLatestReview`. The whole read still costs one point
(measured in November 2026 on a pull request of 515 commits and 2,752 timeline items); beside the
inbox card's details the verdicts would cost a point more per batch of 25, so the card never reads
them. The cache keeps both (`pullContext.Commits`, each headline cut to 200 runes, never cut by
`fitContext`; `Opinion`), and an older build's cache reads with none.

`yourReview` works out what happened since the user's verdict by where its commit is among the
commits, never by their dates, which a rebase keeps: found, the commits after it were pushed since
(`SincePushed`, with the count); missing from every commit, the branch was rewritten
(`SinceRewritten`); missing from the newest hundred only, it was rewritten when a force push in the
conversation follows the review, and is further back otherwise (`SinceBeyond`); with no commits
read, nothing is said. Commits the signed-in user wrote, every one after the reviewed commit, as a
merge of the base they pushed (Update branch's own merge commit is the clicker's), are nothing since
(`allBy`), as the inbox's `ownSince` judges them, so neither the strip nor Re-request my review
shows. The sheet shows them in its Commits tab, the third, and says so above the tabs
(`SinceYourReview`) while the pull request is open.

The summary lists each reviewer's latest review without its text, which is in the conversation, then
who the card says the pull request still asks to review (`withRequests`, from `review_users` and
`review_teams`, no request), above the checks, which show every failing one and fold the rest under
their counts once they pass `openChecks` rows.

A merge that went through tells its `Merges` the repository, without waiting (core: the Release
page's `Refresher`, which reads that repository's row again); a close tells it nothing. `EditLabels`
adds and takes off a pull request's labels as the issue sheet's edit changes an issue's, at the same
issues endpoint (`AddLabels`, `RemoveLabel`, which the client takes for a pull request too): pinned
to the head the request names (another is `PushedSince`, with nothing sent), for triage access or
more (`ErrCannotEditLabels`), through the Actor, each step sent once across the token retry, and a
step GitHub made standing (`LabelsResult`); Autopilot's labels action is its caller. Nothing here
lets an agent approve, merge, or close.

## An open sheet stays current with its card

The inbox's refresh, its checks follower, a read of one pull request (`RefreshItem`, a push from
this Mac through `internal/pushwatch`), and the sheet's own read all rewrite the card's details,
while the sheet's cache moves only with the sheet's own reads; the inbox never fills the cache, so a
closed sheet costs nothing. Every build (`Open`, `Refresh`, `Rebuild`, and `Act`'s decision) takes
the title, labels, draft, review decision, review requests, and head (`Stale`) from the card
(`inbox.Store.ReviewItem`, whose `GetReviewItem` reads the merge state and the held runs too,
`heldruns.Unnamed` filling in the runs of no name). When the card's details are newer than the
cache's context (`inboxNewer`: `DetailsAt` after `contextAt`, never against a context `expire` left
without a time), the card wins for what it holds: the summary's counts (`Review.Counts`, `Newer`
set, the card's rollup counts and their time), auto-merge, and the readiness's facts
(`Facts.fromInbox`): GitHub's merge state (but `UNKNOWN`, which leaves the cache's), the checks
failing and running, and the held runs, counted with the ignored workflows `markIgnored` keeps on
the cache. The card counts no check as required, so the required ones are none while the merge state
says nothing required holds the merge (`CLEAN`, `HAS_HOOKS`, `UNSTABLE`), and otherwise the cache's,
capped at the card's totals; an approval names only runs the cache read (`Approvable` capped at what
is asked, so it waits for a read while the card holds more), and runs that failed to start on
another head count for nothing. The checks list, the held runs (`Unrun`), and the conversation stay
as the sheet read them, and `Review.Moved` says the card's head, checks, or held runs moved since:
the cache keeps the read's own rollup counts beside the list (`pullContext.Counts`, the
`github.Checks` the card took from the same answer, nil in an older build's cache), and `Moved` is
any difference from the card's, which the window answers with one `Refresh` (see frontend). A sheet
never read shows the card's counts too, with its readiness still waiting for the read. `recent` also
wants the card's checks state, counts, and held runs to match the cache's (`contextCounts.matches`):
a check that ends moves no activity, so a sheet reopened within 20 seconds of its read, after the
checks follower rewrote the card, reads again. A read replaces the whole context (`SetDiffContext`),
its checks list with it, but the list is the rollup's first hundred (`contexts(first: 100)`), so
`Review.MoreChecks` counts what the read's rollup counted past it, which the summary links to on
GitHub.

## Who reviews it (`reviewers.go`)

The sheet asks people and teams to review a pull request, and takes their requests off, from a
picker that suggests the right people with no request and offers only people who work on the
repository, from the `People` directory it declares (core adapts `*people.Service`, `reviewPeople`
in `internal/core/review.go`; review never imports it, and without one the sheet learns no one and
suggests only GitHub's suggestions). Every read of the sheet learns for free (`learn`, after
`store`): who opened the pull request and who reviewed it, people only, each with whether GitHub's
association gives them access (`hasAccess`: OWNER, MEMBER, or COLLABORATOR), who committed to it
(each linked account once), who and which teams it asks, and whom GitHub suggests; a failure is
logged and never fails the read. The cache keeps GitHub's suggestions (`pullContext.Suggested`, none
in an older build's). `ReviewerCandidates` is local alone, with no request: the suggestions first,
in GitHub's order, with what the directory knows of each, then the directory's people, the highest
ranked first, never the pull request's author, the signed-in user (who keeps Re-request my review
and Remove me as reviewer), or a login only a bot has, each once and at most 100 (`maxCandidates`,
the avatars' `MaxLogins`), with the repository's teams, how many pull requests its last read read,
when, and whether one is due (`Reviewers.Stale`). `LearnReviewers` is the seed: when the directory
says a read is due (never read, or a week old, and not within 30 minutes of a failed one), it makes
one `FetchRepoPeople` through the inbox's `Peek`, and, for an organization's repository, one
`FetchRepoTeams`, whose refusal or failure leaves the teams the app saw asked; one runs at a time
per repository (`Service.learns`, a `flight.Group` keyed by its name), so the picker opened on two
of its pull requests reads it once. It keeps who opened, reviewed, and was asked on each pull
request read, the teams list when GitHub gave it, the teams asked, and the read (`MarkRead`), or
records a failure (`MarkFailed`) and says so ("Couldn't learn who works on acme-org/example-repo:
..."), then returns `ReviewerCandidates`. The sheet's extended read and the seed read each cost one
point (measured in November 2026). `EditReviewers` changes who the pull request asks: names trimmed
and taken once ignoring case (`namesOnce`), one both added and taken off sent not at all
(`namesApart`), people and teams apart, and at most two REST requests through the Actor
(`sendReviewers`: `"+"`, `RequestReviewers` for everyone added, then `"-"`, `RemoveReviewRequests`
for everyone taken off), each never sent twice across the token retry. A review request depends on
no commit, so it isn't pinned to the head, but it needs `Review.CanEditReviewers`: open, read by the
sheet, and triage access or more (`github.CanTriage`, as GitHub's roles give the right to request
reviews), else `ErrCannotEditReviewers` with why; the user is refused (`ErrOwnReviewRequest`, with
the sheet's own action to use) and so is the author (`ErrAuthorReview`), before anything is sent.
Each step GitHub made hands the inbox who the pull request asks after it (`Acted.RequestsEdited`,
`ReviewRequests`), which rewrites the card at once, so the window rebuilds the sheet with no read;
nothing is `settle`d, since the cache holds no requests. A step that fails after one went is the
result's `Failed`, as a label edit's is, and a failure before any is the error. Each person asked is
kept as `picked` in the directory, so the people the user asks most rank up.

## Who it is assigned to (`assignees.go`)

The sheet reads a pull request's assignees with its one read and keeps them in the cache
(`pullContext.Assignees`, nil in an older build's, which keeps them from being edited until the
sheet reads the pull request again: `Review.Assignees` is then empty and `CanEditAssignees` false,
with "Reload the pull request" as why). It offers people to assign through the `assignees.Finder`
core hands both sheets (see internal/assignees): `AssigneeCandidates` with no request,
`LearnAssignees` when a read of the repository's assignable users is due, and `SearchAssignees` for
words, each made of the pull request as the Finder sees it (`assigneeItem`: the repository, the
user, the author when a person, and who took part, its reviewers and whoever wrote what its
conversation shows, bots left out) and of the read the sheet makes (`readAssignable`:
`FetchAssignable(KindPR)` through the inbox's `Peek`, which stores nothing). Every read records who
is assigned (`learn`, `Finder.Seen`). `EditAssignees` changes who it is assigned to: names taken
once and apart as a reviewer edit's are, then at most two REST requests through the Actor
(`sendAssignees`: `"-"`, `RemoveAssignees` for everyone taken off, then `"+"`, `AddAssignees` for
everyone added), each never sent twice across the token retry; taking off first lets a swap work at
GitHub's cap. It needs `Review.CanEditAssignees` (as `CanEditReviewers`, with the assignees read),
else `ErrCannotEditAssignees` with why; taking the user off is refused (`ErrOwnAssignment`: that is
Unassign me, on the card), and so is an edit that would leave more than ten assigned
(`ErrTooManyAssignees`), before anything is sent. After each step GitHub made, the sheet rewrites
the cache's assignees with GitHub's answer (`cacheStore.setAssignees`: one transaction of `GetDiff`,
the new list, `fitContext`, and `SetDiffContext` with the row's own `context_at`, so the next
refresh is no sooner), and the window rebuilds the sheet with no read; a cache it can't rewrite is
expired instead (`settle`), so the next refresh reads it. The inbox's card is not touched unless the
edit assigns the user: GitHub's answer then tells the inbox (`Acted.SelfAssigned`), and the card
reads as assigned to them at once. Each person assigned is recorded as assigned in the repository. A
read already under way when an edit lands can overwrite the rewritten list with the one it read; the
next read puts it right. The other way round, the inbox's own Unassign me reaches the cache too:
`InboxEdits` (`NewInboxEdits`, apart from the bound `Service`, since only core calls it) has
`Unassigned` take the user off the cached assignees (`cacheStore.takeOffAssignee`, through the same
`rewriteAssignees` as `setAssignees`, with the row's own `context_at`), which core's
`sheetOutcomes`, the last sink of the inbox's `outcomesFanOut`, calls for each
`inbox.OutcomeUnassigned` of a pull request (`Outcome.ItemID`). A sheet opened in the next moments,
whose read under 20 seconds old is served again (`recent`), shows the user gone at once, and the
next read, which may still name them in the moment after the edit, comes no sooner. A pull request
with no cache, or one whose cache says nothing of its assignees or doesn't name the user, is left
as it is. `PeopleSummary` and `ForgetPeople` (`people.go`) count and empty the People
directory for Settings › Reviewing › People; forgetting answers with what is left, so the row needs
no second read.
