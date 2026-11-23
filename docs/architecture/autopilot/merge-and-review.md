# Merge it and Post the agent's review

[Docs](../../README.md) › [Architecture](../../README.md#architecture) › [`internal/autopilot`](README.md) · Code: [`internal/autopilot`](../../../internal/autopilot) · Guide: [Merge it and Post the agent's review](../../guide/autopilot/merge-and-post-review.md)

- **The seam.** Both are GitHub writes of their own (`catalog.DispatchMerge`,
  `DispatchReview`), registered beside the GitHub step over the same `GitHubActions`
  (`registerWrites`), so each is its own `autopilot_runs` row (`runner = github`), recorded
  before it is sent, paced by `pace` and the GitHub actions a day, and never sent twice on its
  own (`writesGitHub`). The dispatcher's `ReadItem` is the sheet's read, and the job carries
  the commit it is pinned to (`followUp`): for Merge it, `mergeHead`, the commit the step's
  Wait for the checks passed on (`stepRun.Commit`, which `applyVerdict` records), or, with
  no agent step and no wait, the match's own head, only while its fire key is that commit's
  `checks_passed` key (`agentlessHead`, `eval.CommitKey`: a click's run, keyed `manual:`,
  merges nothing), `Job.AutoMerge`, whether to turn auto-merge back on (`autoMergeBack`:
  `MergeHow` says so, and it was on as the routine matched,
  `autopilot_matches.auto_merge_at_match`), and `Job.PastReview`, `Sensitive`, and
  `PastReviewWhy` (`pastReview`, below); for Post the agent's review, `agentReviewOf`, the review
  the read-only agent's report held (`agentrun.Report.Review`, kept to `ReportSchema`'s
  `review` object, carried on `RunEnd.Review` into the agent step's `stepRun.Review`) and the
  commit its run started at, and `Job.Review`, what `reviewEvent` decided to post. A run whose
  agent wrote no review skips the step (`noReview`).
- **The guards in Go,** whatever the spec says. `mergeRules`, first in the gate, closes a
  Merge it on an outside contributor's pull request (`not_offered`); a review on one waits
  `outside_contributor`, as a GitHub action does. `reviewEvent` is pure: a comment unless the
  step posts its verdict and the user didn't ask for a comment (`stepRun.PostAs`,
  `Service.PostAsComment`); an approval only with `AllowApprove`, a member's or a trusted
  bot's pull request (`guardOf`, from `FieldTrust`), every file checked and none sensitive
  (`FieldSensitive`, known and not partial), and no routine's run that recorded a commit
  there (`pushedTo`); else it posts as a comment, saying which guard held it.
- **An agentless merge is decided again** (`recheck`, second in the gate): a match that ran a
  step before its Merge it is never settled again by `closeMoved`, so right before the merge
  (and again on the cycle after its fresh read) the routine's decision on the item must be yes
  on the match's own fire key. A yes on another key closes it `moved_on`; a no whose trigger
  held (a condition, such as a label taken off) `no_longer_matches`; anything else, a trigger
  that doesn't hold now (checks running again) or a condition not known yet, waits
  `settling`, reading nothing.
- **Past the rules** (`pastrules.go`). `pastReview` lets a Merge it go past the rules only
  with every consent the user gave and the engine can see: the step's `AllowBypass`
  (`AllowSensitive` for sensitive files); a routine's own run (`matchWhy`: not a click's, at
  the commit its checks passed on); `autopilotprefs.Config.MergePastRules` on; the routine's
  `past_rules_hash` equal to the full SHA-256 of its stored spec (`accepted`), and the match's
  steps that routine's (`SpecHash`); and no routine's run having pushed to it (`pushedTo`,
  as for an approval). A step that allows it but may not says why (`PastReviewWhy`); one that
  doesn't allow it says nothing. When the why is the switch (`PastRulesOffWords`), the job
  says so (`Job.PastRulesOff`, `MergeSend.PastRulesOff`), and a merge that needs to go past
  the rules comes back `GitHubPastRulesOff` (`Result.PastRulesOff`): `unsent` puts the step
  back, as one that sent nothing, and the match waits `blocked` on `past_rules_off`
  (`pastRulesWaitWords`), at that step, its earlier steps kept as done; the gate
  `pastRulesSwitch` keeps it waiting, with no read of the item, while the switch is off, and
  lets the merge go again once it is on. Any other `not_offered` ends its step `skipped`
  (*Not sent: …*) as it closes the match, so no step stays `running` on a closed match. An
  earlier build closed such a match instead, `not_offered` with `PastRulesOffWords`:
  `resumePastRules`, once per engine (`Engine.resumed`, on its first cycle), sets each one
  closed within the last week (`ListClosedMatches`) waiting on `past_rules_off` again, at its
  merge, for the gates to decide (recheck closes one whose pull request moved on). `Service.Save` stores the hash only with
  `Draft.AcceptPastRules`, or when the new spec text is the very one accepted
  (`pastRulesToWrite`), refuses a spec with a tick otherwise as a problem at its first
  `merge.allowBypass` (`ProblemsError`), and clears it for a spec with no tick; `Routine.PastRules`
  (`allowed`, `needsTick`, or `""`) says where it stands. Duplicate, Restore, and Import
  insert none; a backup's import clears it; the prompt rename's follower carries it only while
  it stood; no export, backup, or `autopilot.json` holds it.
- **Core** (`internal/core/adapters_followups.go`, `githubSteps.Merge` and `PostReview`):
  each reads through the review sheet (`Refresh`, serving the read just made), then decides
  on it. A merge of the user's own pull request that lacks only an approval
  (`Review.OwnAwaitsApproval`), with `PastReview` or `PastReviewWhy` set, goes to
  `pastReview` (`adapters_pastreview.go`, the only file of core that names
  `review.ActionMergeBypass`, each request there with `Routine` set, which
  `TestOnlyThePastReviewAdapterMergesPastTheRules` parses core to hold): `PastReviewWhy`
  closes it `not_offered` with the reason, or, with `PastRulesOff`, answers `GitHubPastRulesOff`
  (`pastRulesOff`), which waits for the switch; else it reads the switch again
  (`autopilotSettings`, off answering `GitHubPastRulesOff` too), refuses an offer warning of more than the approval and, as the step
  allows, sensitive files (`pastReviewOffer`), and sends `merge_bypass` with
  `review.RoutineMerge`; `ErrPastRulesNotYet` comes back `GitHubNotYet`, which waits
  `settling` and is tried a tick later, never asking the user, and a merge past the rules says
  *Squash-merged #12 past the rules: it lacked only an approval from someone else.* Any other
  merge goes on with the pure `mergeDecision` or `reviewDecision`: a head other than the pinned one
  is not offered; auto-merge already on is nothing; a merge the sheet won't send but would
  approve and merge (`waitsOnReview`) is `GitHubNeedsReview`, which waits
  `review_required` for the user, never approving; and anything else goes only as the sheet
  offers it now (`pullOffer`, warnings only the user accepted). A merge first asks the inbox
  what the agent's commits at that head changed (`agentChanges.AgentChanged`, the fence's
  check): a sensitive group, or a check not complete, merges nothing. A review's line
  comments are fitted to the cached diff (`review.Fitter`, never bound: the sheet's own
  check of each against its hunks, `checkComment`, the rest left out and counted, so a review
  never fails whole on a line GitHub would refuse). Then the sheet's `Act` sends it under
  the inbox's `Actor` and the write gate: only `merge_now` (`mergeMethodOf`, the step's
  method) or `auto_merge`, and only `comment`, `request_changes`, or `approve`;
  `TestMergeAndReviewReachNoForbiddenAction` parses `adapters_followups.go` and fails on
  `merge_bypass`, `approve_auto_merge`, a close, approving workflow runs, or sending
  `approve_merge`, whose offer it only reads. A merge GitHub refuses, by the merge state the
  sheet reads right before it or by the merge itself (`github.ErrNotMergeable`), merged
  nothing, so `mergeSent` makes it not offered, in the sheet's words, never a failure after
  which GitHub may have taken it.
- **Ask first** (`askFirst`): both steps ask again once reached (`followsRun`, the wait's
  words from `followWords`), whatever the routine's OK to start, a click's run's too, and `Approve` records the
  OK on the step (`stepRun.OK`), so the card shows what the run came to. The window's card
  reads it from `MatchStep` (`Merge`, the merge's words; `AutoMerge`; `Review`, `ReviewAs`,
  `AllowApprove`, `PostAs`).
- **Stats** count a sent merge as `Merges` and a posted review as `Reviews`, by the step's
  kind, never as a GitHub action, and the review's run ends with the event it posted as its
  step's outcome (`approve`, `requestChanges`), which the ⚡ chip reads.
