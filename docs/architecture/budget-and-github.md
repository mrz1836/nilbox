# internal/budget and internal/github

[Docs](../README.md) › [Architecture](../README.md#architecture) · Code: [`internal/budget`](../../internal/budget), [`internal/github`](../../internal/github) · Guide: [The item menu and edits on GitHub](../guide/item-menu.md), [The window](../guide/window.md) · See also: [Performance](../performance.md)

`internal/budget/` keeps the GitHub API budget current. The one shared `github.Client` hands every
response's rate-limit reading, and what the request spent, to its `Tracker` (`github.RateObserver`),
whose state the `Saver` keeps across runs.

Every GitHub request goes through `(*github.Client).do`, apart from three reads that aren't the API
and carry no token: a profile picture (`FetchAvatar`, by the URL GitHub gives: the user's own, and a
bot author's, which the item fields ask GitHub for, since the avatar host answers a bot's login with
a generated pattern; and anyone's by login through `FetchLoginAvatar`, which builds the avatar
host's URL itself and refuses a login no user could have, such as a bot's; either way https on the
avatar host alone, every redirect on it too), a public release file (`DownloadAsset`, https only,
its host and every redirect's on `DownloadHosts`), and a status page's summary (`FetchStatus`:
GitHub's, Claude's, or OpenAI's, https on one of `StatusHosts` alone, a clean absolute path, every
redirect on the request's own host, at most 1 MiB, conditional on its last ETag, so a 304 carries no
body). Never build an HTTP request anywhere else, nor run `gh api`;
`internal/repoguard/githubrequests_test.go` fails the build if anything does.

Every write goes through the `WriteGate`, a REST one through `edit` and a GraphQL mutation through
`mutate` (`postGraphQL` refuses a mutation), but the chore lane's: marking a notification thread
done or read, sent only by a rule the user turned on or by the user's click, goes through `chore`
and the client's own `ChoreGate` (nil refuses every chore), whose `AllowChore` paces it apart from
the edits, with counts of its own (50 a minute, 300 an hour, a second apart), the same reserve, and
the same hold, so a wait GitHub asks of either lane stops both;
`internal/repoguard/githubwrites_test.go` keeps it so.

The edits' own lane (`AllowWrite`) sends at most `budget.WritesPerMinute` (60) a minute and
`WritesPerHour` (400) an hour, Autopilot's included: under GitHub's own limits on requests that
create content (80 a minute and 500 an hour), with room left for the user's other tools, which share
them. The reserve every lane keeps is `budget.Reserve(limit)`, a hundredth of the budget's limit
and never under 10 points (50 of a 5,000-point budget): an edit or a chore is refused while the
budget it spends from (the REST budget for `edit` and `chore`, the GraphQL budget for `mutate`), as
last read, is down to its reserve, until the budget refills, so that much is left for everything
else signed in as the user (`gh`, coding agents). Every change a lane allows is counted, sent or
not, so the pace errs on the side of fewer, and a refusal says when the next may go, in words and
as `budget.RetryIn`: the longest wait when several limits refuse at once.

A bulk mark (Mark all done) sends the same `DELETE` (`MarkThreadDoneInBulk`), through `chore` with
its lane named last, which asks the gate's `AllowBulkChore`: a third lane, the bulk lane
(`budget.BulkPerMinute` 40, `BulkPerHour` 1,200, `BulkGap` 1.5 seconds apart), so a long job never
uses up the chores clicks and clears send. It keeps the reserve and the hold, and the two
notification lanes never send within `ChoreGap` of each other. The pace is safe for a half-hour job:
a `DELETE` costs 5 of GitHub's 900 REST points a minute, so 40 a minute spend 200; one every 1.5
seconds is slower than the one a second GitHub asks of a client that writes many times; and marking
done creates no content. The lane's times are kept with the rest of the budget's state
(`savedState.Bulk`), so a job that goes on after a restart keeps its pace, and the window sees them
as `Gauge.Bulk`.

The notifications listing is conditional (page 1 sent with its last ETag as `If-None-Match`, through
`do`'s `withETag`: measured in November 2026, github.com answered `If-Modified-Since` with a charged
200 and `If-None-Match` with a free 304), and a 304 spends nothing, but says only that page 1 is as
it was. `ThreadQuery.Before` lists only the threads updated before a time (GitHub's `before`, in
whole seconds, UTC), which a bulk mark reads past the mirror's cap with, and such a listing is never
conditional, nor keeps its ETag; `internal/repoguard/githubnotifications_test.go` holds the client
to `GET /notifications` and `DELETE` and `PATCH` on `/notifications/threads/{id}`, each write sent
by `chore`. GitHub has no call that marks many done, and the client sends neither of those that mark
many read (`PUT /notifications`, `PUT /repos/{owner}/{name}/notifications`).

Every request carries what it was sent for (`github.WithPurpose`: the inbox's refresh, the routines'
searches, the checks follower, or Autopilot's own reads), and the `Tracker` splits each budget's
spend by it (`Reading.Purposes`, which add up to `Spent`); the budget menu says Autopilot's share in
one line, only once it spent something.

## The token and the account it belongs to

The token is the GitHub CLI's, borrowed and kept in memory only. `github.TokenSource` runs
`gh auth token --hostname github.com` and hands what it prints to every caller for
`github.TokenTTL` (10 minutes), and callers that ask while `gh` runs share that one run. It asks
`gh` again sooner once the token is dropped:

- `WithToken` runs a call with the token, and when GitHub refuses it as bad credentials
  (`IsBadCredentials`: a 401, or a `Bad credentials` message) it calls `Forget` and runs the call
  once more with a fresh token. `Forget` drops the token only when it is the one held, so callers
  refused with the same old token never throw away one borrowed since.
- A token borrowed before `gh`'s hosts file changed is never handed out again: core hands the
  source the file `GHHostsFile` finds (`WithHostsFile`: `hosts.yml` in `GH_CONFIG_DIR`, else in
  `XDG_CONFIG_HOME/gh`, else in `~/.config/gh`), whose size and modification time each `Token`
  compares with what they were as the borrow began, and `gh auth switch`, `login`, and `logout`
  rewrite it.
- `Core.Close` calls `Clear`, which drops the token, whatever it is, as the app quits.

A missing `gh` is `ErrGHNotFound` and a `gh` with no token `ErrNotLoggedIn`, each with a sentence
for the user, and neither carries the command's output. A `Token` prints as `[redacted]` in every
format, and every error that leaves the package is scrubbed of the token's text (`Redact`).

`github.ViewerCache` (`internal/github/viewercache.go`) remembers the account the last token it was
told of belongs to, so its holder asks GitHub who a token is (`FetchViewer`, one GraphQL point)
only for a token it hasn't seen. It keeps the token in memory alone, as the `TokenSource` does, and
only compares it (`Token.Same`, in constant time); the zero value is ready. The inbox holds one
(`Service.viewers`), which each refresh tells of the inbox query's viewer and which an edit checks
against the account whose inbox the user sees (`checkViewer`), so an edit never acts for another
account after `gh auth switch`; the GitHub notifications' service (`internal/ghthreads`) holds
another.

## The budget the window shows

`budget.Service` is the `Tracker`'s bound face. `Gauge` returns every budget (`GraphQL`, the REST
budget as `Core`, and `Search`) as the app's latest responses left it, with what the app spent from
each since it last refilled (`Reading.Spent` and `Requests`; `Complete` is false when the app opened
after the budget last refilled, so it missed what was spent before); what the last whole inbox
refresh cost (`Gauge.Refresh`: its GraphQL points and how many items it tracked, which the inbox
records with `Tracker.Refreshed`); `RefreshesPerHour`, an hour divided by the inbox's refresh
interval as Settings → Refreshing sets it now, from which the window works out the app's hourly
pace; and the three lanes (`Writes`, `Chores`, `Bulk`, each with the REST budget's reserve as
`Writes.Reserve`). `Check` reads every budget from GitHub's `GET /rate_limit` (`FetchRateLimits`,
which costs no budget, through `WithToken`) unless all three were read within `budget.FreshFor` (10
seconds), and calls that come while one runs share it; the readings reach the `Tracker` through the
client's `RateObserver`, as every response's do, and a failed read returns the gauge as it was, with
the error. The window reads `Gauge` again on each `event.BudgetUpdated`, which the `Tracker` emits
on every change (`lib/useBudget`), and its budget menu calls `Check` as its panel opens and every 30
seconds while it stays open (`checkEveryMs`). Nothing reads `GET /rate_limit` while the panel is
closed: the toolbar's gauge follows the app's own responses, the inbox's refresh for GraphQL and
the notifications' conditional read for REST.

### A budget GitHub counts in two windows

GitHub sometimes counts one budget in two hourly windows at once, each with its own reset and its
own count, and answers each request from one or the other by turns: two identical requests a moment
apart can report 135 used, refilling in 15 minutes, and 338 used, refilling in 55. Its rate limit
endpoint and a `rateLimit`-only query take turns as well. So the `Tracker` holds each budget's
windows apart (`Tracker.budgets`, keyed by the reset each reading names): a reading updates its own
window, the newest reading of it winning (the one with the most used), and counts its request's
spend there, so the app's share in each window stays right, and no report of one window replaces
another or clears what the app spent in it. A reading of a window not held adds it, unless that
window is over already, which makes it a late answer, dropped. Adding one lets go of every window
over by then, and of the one reported longest ago while `maxWindows` (3) are held.

The gauge shows one window of each budget (`shownLocked`): of those
reported within `unseenFor` (15 minutes) of the budget's newest report, the one still running with
the least left, since a request may count against either, else the one that refilled last. The
edits' and chores' reserve checks use the same window. A window no response reported for longer,
while another of its budget was, is an account's the token no longer belongs to, as after `gh auth
switch`, so the new account's windows take over within `unseenFor`; until then the gauge shows the
lower of the two. `Check` counts a budget as read when any of its windows was.

At startup core starts the `Tracker` from the state the `Saver` kept (`Restore`, from the prefs key
`budget.state`), every window of each budget with what the app spent in it. The state lists the
window the gauge shows first, so a build that holds one window per budget, which takes a budget's
first entry, keeps that one, and the state's version stays 1. Without a state it can read, as on
the first run of a build that keeps it, it seeds it from the readings the last refresh stored
(`Tracker.Seed`), each for a budget it has no reading of yet, so the window shows them until the
first response; a seeded budget counts nothing the app spent, which was spent in another run.

## Held workflow runs

GitHub's approve endpoint (`POST .../actions/runs/{id}/approve`, `ApproveWorkflowRun`) takes only "a
fork pull request" or a run "queued by the Actions bot", in its own words, and refuses any other run
it holds, as one a review (`pull_request_review`) started in the base repository, even on a fork's
pull request. The details fragment reads, of each head commit suite, its creator's login and its
run's event, and the pull request's `isCrossRepository`, none of which costs a point (measured in
November 2026), and `suiteHold` tells the held runs (suites that ended `ACTION_REQUIRED` with a run)
apart: a `pull_request` run on a fork's pull request, or one whose suite `github-actions[bot]`
created, is approvable (`Checks.Awaiting`); any other waits for approval on GitHub
(`Checks.AwaitingOnGitHub`, `UnrunSuite.OnGitHub`). The fragment reads each run's workflow's name
too (`workflowRun { workflow { name } }`, one object, which GitHub's point formula never counts),
and `Checks.Held` lists the held runs with it, for the ignored workflows (internal/heldruns). A run
waiting on a deployment environment's reviewers hasn't ended, so it is neither: its job's check
counts as running. A refusal for what the run is still comes back as a 403 or 422 like any other,
with no code of its own, so `approveError` tells it by GitHub's message ("fork pull request") and
wraps `ErrRunNotApprovable`, keeping GitHub's words.

## How an issue is planned

The details fragment reads, of each issue, what GitHub's sidebar shows beyond its projects
(`issueInfoFields`, `github.IssueInfo`): its type, milestone, parent, the open issues it is blocked
by and blocking (`issueDependenciesSummary`), and its values in its organization's issue fields
(`issueFieldValues`, single select, multi select, number, text, and date). All are single objects
but the field values, one connection per issue, and a batch's price stays where it was (measured in
November 2026: 1 point for 25 issues with them and without, 2 with the projects either way). Each
kind of field value aliases its value apart (`number`, `text`, `date`) so values of different types
never share a name. Because the fragment is shared, a card's own read and the issue sheet's view
read them too. The inbox stores them as one JSON object, `item_details.issue_info` (migration
`0104_issue_info`), and the card's peek shows them as **The issue**, in the column a pull request's
change takes.

## An issue's closing pull requests

The issue's part of the fragment also reads the first five open or merged pull requests that close
it once they merge (`closingPullsField`, GitHub's `closedByPullRequestsReferences`, aliased
`linkedPulls` so it never meets the issue sheet's own `fixes`, which take other arguments), each
with who opened it and whether its branch is in a fork. Measured in November 2026, it leaves a
batch's price where it was (1 point for 25 issues, 2 with the projects); the same link read on a
pull request's side (`closingIssuesReferences`) would cost a point more a batch, so it is read on
the issue's side alone. The inbox stores them in `item_details.closing_pulls` (migration
`0105_closing_pulls`), with the author, and decides each one's trust as the snapshot is built, with
the trust policy of the moment: the user's own, or one the policy trusts (a fork's never is). In the
window, `lib/cardLinks` indexes the lists: an issue's card links to its closing pull requests, and a
pull request's card finds the listed issues that name it, so a pull request's own read asks for
nothing. A trusted fix turns an issue's story to **Fix in progress** or **Fix merged** (`nextStep`);
an untrusted one only links.

## Auto-merge bots

Many orgs run a GitHub App of their own (such as `acme-org-automerge`) that approves a bot's pull
request and merges it once its checks pass. The fragment's `latestReviews(first: 50) { nodes { state
body author { __typename login } } }` reads each latest review's author with its type, scalars
GitHub's point formula doesn't count, so the read costs no request and no point more.
`github.ItemDetails.ApprovingBots` lists the bots whose latest review is `APPROVED`, by login,
lower-cased (a person whose login looks like a bot's, a bot that only commented, and a dismissed
approval never count), and the inbox stores them as `item_details.approving_bots` (migration
`0114_approving_bots`; a row stored before it reads `[]` until the next details read). The match
happens as the snapshot is built: the `trust` package declares the setting, `trust.mergeBotsOn` (off
by default) and `trust.mergeBots` (empty; no build ships a name), which settings alone writes
(`SetMergeBotsOn`, `SetMergeBots`, normalized as the trusted bots are, in `Portable`), and
`trust.MergeBots.Match` names the first listed bot that approved as `Details.MergeBot`. Both keys
are in the snapshot's `settingsKey`, so a change builds the next snapshot from what is stored and
asks GitHub nothing. The card shows an **Auto-merge** chip first among its chips (`MergeBotChip`),
the compact row its icon, and the review sheet a tag beside the trust tag; `nextStep` and the state
track are left alone, since the bot usually turns on GitHub's own auto-merge too, which the track
already shows.

## A card's comment count

GitHub's `totalCommentsCount` counts a pull request's comments and its review comments, but not a
review's own text, such as a bot's note as it approves, which the review sheet's Comments tab counts
(`commentCount`). The same `latestReviews` read takes each review's `body` (text, no point more),
and `ItemDetails.ReviewTexts` counts the latest reviews that say something, a pending draft left
out. The inbox stores it apart, as `item_details.review_texts` (migration `0115_review_texts`), and
adds it to `comments` only as the snapshot is built (`Details.Comments`), so `comments` stays
GitHub's own count, the one a snooze's wake on comments compares. It counts one review per reviewer,
the latest, so a reviewer's earlier reviews with text count only in the sheet. The text itself is
never kept.

## Who an item is assigned to

The inbox query's searches match through GitHub's search index, which trails an edit, while a node's
fields are read live, so the details are what can tell an item the user left on GitHub from one the
search still returns. The fragment reads a pull request's review requests already, and each pull
request's and issue's assignees too: `assignees(first: 10) { totalCount nodes { login } }`, every
assignee GitHub allows (`MaxAssignees`), the selection the issue sheet's and the review sheet's own
queries ask for, so GraphQL merges the two where they spread the fragment. `ItemDetails.Assignees`
lists the logins in GitHub's order, never nil when GitHub brought them all, and nil when the read
can't tell: the field absent or null, or fewer logins than `totalCount` (`allLogins`). It is never
stored; the inbox only decides which lists an item is in with it (see [internal/inbox](inbox.md)).
Measured with `rateLimit(dryRun: true)` in November 2026, it costs no point: a batch of 25 costs 1
point with it and without, and a read of 100 items 5 either way, where in the inbox query's assigned
search it would double that search's price, 1 point to 2. The dry run gave GitHub's documented price
for a batch of 50 issues with their projects, 4 points, so it counts nested lists as a real read
does.

## Reviewers

The review sheet's reviewer edit asks everyone it adds with one REST request and takes every request
it drops off with one more (`RequestReviewers`, `POST .../requested_reviewers`, and
`RemoveReviewRequests`, `DELETE` at the same path), each through `edit`, people and teams together:
`reviewers` always, empty or not, since GitHub's `DELETE` requires it, and `team_reviewers` when
there are any. Both share one sender (`reviewEdit`) with the sheet's own `RequestReview` and
`RemoveReviewRequest`, whose bodies stay as they were and keep their words about "you". Nothing is
sent without someone to ask, a login GitHub could have issued (`ErrInvalidLogin`), and a team slug
of letters, digits, dots, hyphens, and underscores (`ErrInvalidTeam`). GitHub answers with who the
pull request asks after the edit: a login or slug asked for that it doesn't list, or dropped that it
still lists, is a change GitHub ignored (`ErrEditIgnored`), named in the user's words ("GitHub
didn't ask octo-other, acme-org/web to review acme-org/example-repo#42.", a team as owner/slug), and
a 422, as for someone without access to the repository or the pull request's author, is worded with
GitHub's own message (`reviewersError`). The sheet's one read (`FetchPullReview`) also reads, at no
extra point (measured in November 2026), the reviewers GitHub suggests (`suggestedReviewers`, whose
`isAuthor` is `SuggestedReviewer.ChangedFiles` and `isCommenter` `Commented`), each latest review's
author with their type, name, and association (`Review.AuthorType`, `AuthorName`, `Association`),
and the names of who it asks to review, under the same `reviewRequests(first: 50)` the details
fragment asks for, which GraphQL merges into one field: `PullReview.Requested` holds the people of
them (never a bot or a mannequin) and `RequestedTeams` the teams. `FetchRepoPeople` reads who works
on a pull request's repository, by the pull request's node: whether an organization owns it, and its
50 most recently updated pull requests, each with its author, each reviewer's latest review
(`latestReviews(first: 10)`), and who it asks (`reviewRequests(first: 10)`), every author with their
association; people only, and a pull request GitHub can't find is `ErrPullGone`. It costs one point
(measured in November 2026: 101 connection nodes, which GitHub's formula rounds to one).
`FetchRepoTeams` reads an organization's repository's teams (`GET
/repos/{owner}/{repo}/teams?per_page=100`, through `do`, one REST point); GitHub refuses (403) or
hides (404) them from an account it won't show them to, as it hides them from one that isn't a
member, which is `ErrTeamsHidden`. `FetchMergeState` reads, by node, what a merge checks right
before it (the merge state, the head, open, draft, merge queue, auto-merge, and
`viewerCanMergeAsAdmin`), one point. `FetchOwnMergeState` sends the same query with `$own` set,
which adds, under `@include(if: $own)`, what a routine's merge of the viewer's own pull request past
the rules checks (`OwnMergeFields`, `ownmerge.go`): the viewer's login, `viewerDidAuthor`,
`reviewDecision`, the head's `statusCheckRollup` (its state and first hundred contexts), the newest
hundred commits' `signature { isValid signer { login } }`, the first hundred review threads, and the
base branch's classic protection and ruleset rules (the first fifty, with a required-checks rule's
contexts). Every list keeps GitHub's count beside its page, and a null reads as not read, so
`ChecksWhole`, `CommitsWhole`, `ThreadsWhole`, and `RulesWhole` fail closed; any GraphQL error but
the pull request's being gone fails the read. It still costs one point (measured in November 2026 on
a pull request with 39 checks, one signed commit, and a ruleset of five rules); every other merge
sends `$own` false and reads what it always did.

## Assignees

Both sheets assign and unassign people with at most two REST requests at the issues endpoint, where
GitHub keeps a pull request's assignees too: `RemoveAssignees` takes everyone off in one `DELETE
.../issues/{number}/assignees`, and `AddAssignees` assigns everyone in one `POST` at the same path,
both through `edit` and one shared sender (`assigneesEdit`), which `RemoveAssignee`, the inbox's
Unassign me, uses too and keeps its words about "you". Every login is checked before anything is
sent (`ErrInvalidLogin`). GitHub answers with the item as it stands after the edit, whose assignees
the result holds (`Edited.Assignees`, never nil after an assignee edit): a login added that it
doesn't list, or taken off that it still lists, is a change GitHub ignored (`ErrEditIgnored`), named
in the user's words ("GitHub kept octo-other assigned to acme-org/example-repo#42."). GitHub assigns
at most ten people (`MaxAssignees`). `FetchAssignable(kind, id, query)` reads who an issue's or a
pull request's repository can assign, by the item's node (`... on RepositoryNode { repository {
assignableUsers(first: 100, query: $query) { totalCount nodes { login name } } } }`), `""` for
everyone: the first page (`AssignableMax`) and how many match (`AssignableUsers.Total`), one point;
an item GitHub can't find is `ErrIssueGone` or `ErrPullGone`, by kind, through the same
`itemRepoRead` the labels read uses. The review sheet's read (`FetchPullReview`) asks for the pull
request's assignees too (`assignees(first: 10) { totalCount nodes { login } }`, all of them, since
GitHub assigns ten at most), into `PullReview.Assignees`; the read still costs one point (GitHub's
dry run, in November 2026: 2,722 nodes, ten more than without them), and so does `FetchAssignable`
(100 nodes).
