# internal/issues

[Docs](../README.md) › [Architecture](../README.md#architecture) · Code: [`internal/issues`](../../internal/issues) · Guide: [Reading an issue](../guide/reading-an-issue.md)

`internal/issues/` is the issue sheet's service, beside the review sheet's: neither imports the
other (the repoguard boundary test), and they share only the building blocks, the conversation's
among them (`internal/timeline`). `Decide` is pure: from an issue's facts it works out where it
stands (gone, closed as what and by whom, locked, a fix merged or in progress, the user replied
last, assigned, open), the offers, and the one primary, and `Act` runs it again before any write.
Comment is offered always, but held back on a locked conversation for a user without write access;
the two closes (as completed, as not planned) wait for the sheet's first read ("Wait for it to
load"), are left out when GitHub's `viewerCanClose` says no, always confirm, and post the user's
text first (`Offer.CommentsFirst`, with `Offer.CommentBlocked` while a lock refuses it). The button
is Comment, or Close as completed once a fix merged.

`Open` serves the issue from its cache and the inbox card with no request, and starts a visit:
`seen_at` holds GitHub's `updatedAt` of the read it shows, which only `Open` moves, so what a later
read brings that is newer is new to the user (`Issue.LastSeenAt`, GitHub's own times, no clock
skew). `Refresh` reads the issue (one GraphQL point, `FetchIssueView`) unless a read under 20
seconds old matches the card's activity, `ReadAgain` whenever the user asks, one read per issue at a
time (`flight.Group`), through the inbox's `Actor` (`ReadIssue`), which rewrites the card from the
same answer and under whose lock the cache is written; a read that finds the issue closed or gone is
returned once, built from the read, and the cache dropped. One found closed (its `State`) is how the
window learns an issue was closed since the inbox's last refresh, which closes its sheet (see
frontend); one gone shows as gone in the sheet. `Earlier` reads the timeline's page before a cursor
(`FetchTimeline`, the review sheet's query too) through `PeekIssue`, and stores it nowhere.

Every action goes through `ActIssue` and tells each step as an `event.IssueProgress`. A close is
pinned to the comments the user saw (the earlier of the request's `Seen` and the read's): every try,
a token retry included, first reads the state and the newest ten comments (`FetchIssueState`); a
comment someone else wrote since, or ten newer with more before them, is `new_activity` with nothing
sent, and an issue someone closed first is `already_closed` and leaves the inbox. Then the text is
posted once, and the issue closed with one REST `PATCH` (`(*github.Client).CloseIssue`; an answer
that names another account as the closer, or another reason than the one asked, means someone closed
it first). Once GitHub answered a step, nothing after it is an error: `failedAfterStep` makes a
close whose comment was posted `commented_not_closed`, and keeps any other outcome, logging the
inbox's own failure. After any action, under the item's lock, the cache is dropped (a close) or
marked out of date. It owns `issue_views`, a bounded cache (1 MiB per issue, whose long texts are
cut to 16 KiB, then 4 KiB, then emptied, each cut marked, by `timeline.Cut`; 200 issues, 32 MiB, and
14 days unused in all), pruned in the same transaction that writes.

The read brings the first 25 sub-issues, in GitHub's order, each with its state, its first three
assignees and how many it has, its own sub-issues' counts, and when it closed, at no extra cost
(`github.SubIssue`). The sheet shows each as a `SubIssue`: the issue as a `timeline.Ref`, its
assignees as `Person`s, the user's own marked, `MoreAssignees`, `SubIssuesTotal` and
`SubIssuesDone` (kept in bounds), and `ClosedAt`. The cache keeps each as a `cachedSubIssue`, which
embeds the `timeline.Ref`, so its fields sit at the top of the JSON object, as an older build kept a
sub-issue's ref alone: that build's row still reads, with no assignees, counts, or close time, until
the next read. `Issue.Mentioned` lists each pull request and issue the timeline read says mentioned
the issue once, leaving out those the sheet shows in their own place: the fixes, the sub-issues the
read brought, the parent, and the issue it duplicates.

Unassign me is the inbox's own `Unassign`, which marks the cache out of date as an action does:
`InboxEdits` (`NewInboxEdits`, apart from the bound `Service`, since only core calls it) has
`Unassigned` expire the issue's view, which core's `sheetOutcomes` calls for each
`inbox.OutcomeUnassigned` of an issue, so a sheet opened just after reads the issue again, however
recently it read it. `Replies` and `FillReply` offer and fill in the prompt
library's comment replies for issues (a comment reply may apply to issues; a default reply, which
only the review sheet starts from, must apply to pull requests), through the reply filler core
shares with the review sheet, which refuses a reply for another kind of item.

With triage access or more (`github.CanTriage`, `Issue.CanEdit`, on an open issue the sheet has
read), `Labels` lists the repository's labels (one GraphQL point, one read at a time, kept in memory
ten minutes), and `AssigneeCandidates`, `LearnAssignees`, and `SearchAssignees` offer people to
assign through the `assignees.Finder` the review sheet shares (`assignees.go`: the issue as the
Finder sees it, `issueItem`, with its participants as the sheet last read them, and the read it
makes, `FetchAssignable(KindIssue)` through `PeekIssue`); every read records who is assigned
(`Finder.Seen`).

`Edit` adds and removes labels and assignees through `ActIssue`: the labels added, each label taken
off, then one request that unassigns everyone taken off (`RemoveAssignees`) before one that assigns
everyone added (`AddAssignees`), so a swap works at GitHub's cap of ten, past which an edit is
refused before anything is sent. Each answer is checked (GitHub ignores a change it won't make), no
step is sent twice, it stops at the first that fails, and it says how many GitHub made
(`EditResult`), so the window reads the issue again only then; each person assigned is recorded as
assigned, and the user takes themselves off with Unassign me alone.
