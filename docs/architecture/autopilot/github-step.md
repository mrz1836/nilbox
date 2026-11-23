# The GitHub step

[Docs](../../README.md) › [Architecture](../../README.md#architecture) › [`internal/autopilot`](README.md) · Code: [`internal/autopilot`](../../../internal/autopilot) · Guide: [What Autopilot may change on GitHub](../../guide/autopilot/changes-on-github.md)

## The seam (`github.go`)

The GitHub dispatcher (`NewGitHub`, which the engine registers over the Service's `Config.GitHub`
unless `EngineConfig.Dispatchers` names one) reaches the app only through `GitHubActions`: `Read`
(the item's sheet reads it again, one point, which rewrites the inbox's card from the same answer),
`Send` (one action, as its sheet sends it), `MarkDone`, and `Preview`, beside `Merge` and
`PostReview`, which only Merge it and Post the agent's review call. Core's `githubSteps`
(`internal/core/adapters_githubsteps.go`) adapts the review sheet (`ReadAgain`, `Refresh`, `Act`,
and `EditLabels`, its label edit of a pull request at the issues endpoint), the issue sheet
(`ReadAgain`, `Refresh`, `Act`, `Edit`), the mirror of the GitHub notifications (`ClearItemThreads`,
which Mark the GitHub notification done runs to clear the item's threads), and the reply filler the
sheets share, each call marked `github.PurposeAutopilot`. A pull request's actions are exactly
`reviewActionOf`'s (`rebase_bot`, `update_branch`, `comment`), and
`TestTheGitHubStepReachesNoForbiddenAction` parses the adapter and fails on any other sheet action
it names, so no merge, approval, request for changes, close, or approval of workflow runs is
reachable from a GitHub step.

## One action, one run

A GitHub step's actions go one at a time, each its own `autopilot_runs` row (`runner = github`,
`github_action` its kind), recorded before it is sent: the step's stored state counts those that
went (`stepRun.Action`), so a quit or a crash interrupts only the one under way, which is never sent
again on its own (`settleRun`), and the user's OK sends that one alone. The read before each start
is the sheet's (`itemReader`: the dispatcher reads in place of the inbox's single-item read, so an
action costs one point, not two); the cycle after it decides the match again on what it stored, and
the action is pinned to the head that read found (`Job.Head`, `headNow`), which the cycle just
decided on. The adapter then decides from the sheet's own offers on that read (its `Refresh` serves
it with no request): absent or disabled is `NotOffered` (the match closes `not_offered`, with the
sheet's reason); offered with warnings the user hasn't accepted is `Warning` (the match waits
`warning`, its words the warnings); a head other than the one pinned is `PushedSince`, and a sheet
that couldn't read the item just before is `GitHubUnread`, which goes the same way. Only then does
it call the sheet's `Act` (`Accept` only for warnings the user accepted), whose own `Decide` runs
again, and whose refusals map the same way; the write gate's refusal before anything was sent
(`budget.RetryIn`, its pace, the reserve, or GitHub's hold) is `Held`. A labels action sends only
what changes the labels its read found, and says when there is nothing (`GitHubNothing`).

**What each answer does** (`dispatchAnswered`): a sent action moves to the step's next, or
ends the step (`stepEnded`); `Warning`, `PushedSince`, and `Held` end the run `not_sent`,
counted as no try, and take back the attempt (`notSent`), so a match that sent nothing is
decided again like one that started nothing; `PushedSince` reads again before its next start,
and `Held` holds every GitHub action until the gate said the next may go (`holdWrites`,
`writes_held`). `Approve` of a match waiting on warnings records them on its step
(`stepRun.Accepted`), which the next send hands the sheet (`Job.Accept`), so a different
warning waits again; the routine's own Ask first OK accepts none. `ApproveBacklog` is Run on
them: the OK for every match of a routine waiting as a backlog. `SkipBacklog` is Skip them:
one command and one transaction that closes as `dismissed` (`you_skipped`) each of the matches
the card showed (its IDs) that still waits as the routine's backlog, a compare-and-set on what
the card showed, so a match recorded since still waits and one let go or closed meanwhile is
left as it is; when one can't be closed, none is.

## Limits

A match is one try and one of its routine's runs a day however many runs it starts (`history` and
`dayCounts` count distinct matches; `limits` holds only a match's first run to the tries, the
routine's runs, and the wait), while the GitHub actions a day count each one sent (a `not_sent` run
counts toward nothing). Autopilot keeps under the write gate's edit lane (60 a minute, 400 an hour):
its GitHub actions an hour (`Config.ActionsPerHour`, 120 by default, at most 300), and a sixth of
that a minute (`actionsPerMinute`, at most `MaxActionsPerMinute`, 40), from the runs started in the
last minute and hour, manual ones too (`pace`, `writes_held`); the gate's own pace still holds under
it. An outside contributor's item waits for the user's OK in every mode (`outside_contributor`), and
the first pass's cap (`FirstCap`) holds a routine with a GitHub action, as in [The engine and the
ledger](engine-and-ledger.md). Mark the GitHub notification done is a local step: it runs at once,
through the chore lane, recorded as no run.

## Replies

A reply action posts a comment's reply alone: `LibraryPrompt.ReplyFor` names the review a library
reply is written for, and `promptProblem` refuses an approval's (LGTM, Update OK) or a request for
changes' in `Describe` and `Save`; the engine checks the step's replies again on its worker right
before each action (`promptHold`, before the read, so a reply changed since holds the action back as
`blocked: not_ready` and spends no point), and the adapter refuses one that isn't a comment's
(`ErrNotACommentReply`) before filling it in.

**Preview** (`PreviewActions`, bound) builds what a draft's GitHub actions would send one item,
through `GitHubActions.Preview`: the rebase's comment, the branch's update, the reply filled in
for the item, or the labels it adds and takes off, with why one can't be built; it sends
nothing.

**The simulator** runs the GitHub step through the real dispatcher over a fake of the sheets
(`sim.GitHub`, over the scripted World: its read is the inbox's own single-item read, a send is
pinned to the World's head, and a scenario can withdraw an offer, add warnings, have the gate
refuse, or change the World as a send arrives), with a fake prompt library (`sim.Library`);
its scenarios include *Ask Dependabot to rebase what conflicts* as `spec/testdata/templates` writes
it.
