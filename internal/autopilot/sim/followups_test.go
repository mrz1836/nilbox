package sim_test

import (
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/autopilot"
	"github.com/mrz1836/nilbox/internal/autopilot/catalog"
	"github.com/mrz1836/nilbox/internal/autopilot/sim"
	"github.com/mrz1836/nilbox/internal/autopilot/spec"
	"github.com/mrz1836/nilbox/internal/autopilotprefs"
)

// The steps that follow a run: Merge it after a fix whose checks passed, and Post the
// agent's review after a read-only agent, each through the review sheet's fake.

// healThenMerge is Heal Dependabot PRs, then merge them: fix, wait for the checks if it
// pushed, and merge them with squash if they passed, as how says.
func healThenMerge(how catalog.MergeHow) spec.Spec {
	sp := sim.FixFailing()
	sp.Steps = append(sp.Steps, spec.Step{
		Merge: &spec.MergeStep{How: how, Method: catalog.MergeSquash}, OnlyIf: catalog.OutcomePassedChecks,
	})

	return sp
}

// startFix has #1's checks fail and the fix start, and returns its run.
func startFix(t *testing.T, s *sim.Sim) int64 {
	t.Helper()

	s.World.Fail(1)
	s.Refresh()

	return runningMatch(t, s).Runs[0].ID
}

// fixPasses has the fix of #1 push commit 2 and its checks pass, and returns the commit.
func fixPasses(t *testing.T, s *sim.Sim, run int64) string {
	t.Helper()

	fix := pushAndFinish(t, s, run, 2)
	s.World.Pass(1)
	s.Refresh()
	s.Refresh()

	return fix
}

func TestAFixWhoseChecksPassedIsMergedOnTheCommitTheyPassedOn(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		s, _ := started(t, healThenMerge(catalog.MergeAutoIfItWasOn), autopilotprefs.ModeAuto)
		fix := fixPasses(t, s, startFix(t, s))

		merges := s.GitHub.Merges()
		require.Len(t, merges, 1, "%+v", s.Ledger())
		assert.Equal(t, fix, merges[0].Head, "pinned to the commit its checks passed on")
		assert.False(t, merges[0].AutoMerge, "auto-merge wasn't on as it matched, so it merges now")
		assert.Equal(t, catalog.MergeSquash, merges[0].Method)

		m := only(t, s)
		assert.Equal(t, "done", m.Steps[2].Status, m.Words)
		assert.Equal(t, "Merges it (squash).", m.Steps[2].Merge)
		require.Len(t, m.Runs, 2, "the merge is a run of its own")
		assert.Equal(t, "github", m.Runs[1].Runner)
		assert.Equal(t, fix, m.Runs[1].HeadBefore)

		week, err := s.Service.Stats(t.Context(), m.RoutineID)
		require.NoError(t, err)
		assert.Equal(t, 1, week.Fixes, "This week counts the fix")
		assert.Equal(t, 1, week.Merges, "and the merge")
		assert.Zero(t, week.Actions, "a merge is no GitHub action")
	})
}

func TestAutoMergeComesBackOnWhenItWasOnAsTheRoutineMatched(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		s := sim.New(t)
		s.World.Add(sim.Item{Number: 1, Watched: true, Checks: passing, Passed: 4, AutoMerge: true})
		s.Begin()

		r := s.Save("Heal and merge", healThenMerge(catalog.MergeAutoIfItWasOn), autopilotprefs.ModeAuto)
		s.Enable(r.ID, autopilot.CurrentRun)
		s.Refresh()

		fixPasses(t, s, startFix(t, s))

		merges := s.GitHub.Merges()
		require.Len(t, merges, 1)
		assert.True(t, merges[0].AutoMerge, "auto-merge back on, as it was")
		assert.Equal(t, "Turns auto-merge back on, as it was.", only(t, s).Steps[2].Merge)
	})
}

func TestAMergeGitHubHoldsForAReviewWaitsForAPerson(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		s, _ := started(t, healThenMerge(catalog.MergeWhenMergeable), autopilotprefs.ModeAuto)
		s.GitHub.NeedsReview(1, true)
		fixPasses(t, s, startFix(t, s))

		m := only(t, s)
		assert.Equal(t, autopilot.StateWaiting, m.State, m.Words)
		assert.Equal(t, autopilot.BlockReviewRequired, m.BlockedBy)
		assert.Contains(t, m.Words, "approving review")
		assert.Empty(t, s.GitHub.Merges(), "no routine approves, and nothing merges")
		assert.Len(t, s.GitHub.MergesTried(), 1)

		s.Advance(10*time.Minute, true)
		assert.Len(t, s.GitHub.MergesTried(), 1, "it waits for the user, asking nothing again")

		// The user approves it on GitHub, then lets the merge go.
		s.GitHub.NeedsReview(1, false)

		_, err := s.Service.Approve(t.Context(), []int64{m.ID})
		require.NoError(t, err)
		s.Advance(2*time.Minute, false)

		assert.Len(t, s.GitHub.Merges(), 1)
	})
}

func TestNothingMergesOnAHeadPushedToAfterItsChecksPassed(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		s, _ := started(t, healThenMerge(catalog.MergeWhenMergeable), autopilotprefs.ModeAuto)
		run := startFix(t, s)

		s.GitHub.OnFollowUp(func(number int) { s.World.Push(number, sim.Commit(number, 9)) })
		fixPasses(t, s, run)

		assert.Empty(t, s.GitHub.Merges(), "never a commit its checks didn't pass on")

		first := s.Matches()[0]
		assert.Equal(t, autopilot.StateClosed, first.State, first.Words)
		assert.Equal(t, autopilot.ReasonNotOffered, first.Reason)
	})
}

func TestAnAskFirstMergeAsksAgainShowingTheVerdict(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		s, _ := started(t, healThenMerge(catalog.MergeWhenMergeable), autopilotprefs.ModeAsk)
		s.World.Fail(1)
		s.Refresh()

		m := only(t, s)
		require.Equal(t, autopilot.BlockAsk, m.BlockedBy)

		_, err := s.Service.Approve(t.Context(), []int64{m.ID})
		require.NoError(t, err)
		s.Advance(2*time.Minute, false)

		fixPasses(t, s, runningMatch(t, s).Runs[0].ID)

		m = only(t, s)
		assert.Equal(t, autopilot.BlockAsk, m.BlockedBy, "the OK to fix is no OK to merge")
		assert.Equal(t, "Its checks passed: waiting for your OK to merge it.", m.Words)
		assert.Equal(t, autopilot.VerdictFixed, m.Steps[1].Verdict, "the card shows the verdict")
		assert.Empty(t, s.GitHub.Merges())

		needs, err := s.Service.NeedsYou(t.Context())
		require.NoError(t, err)
		require.Len(t, needs, 1, "it waits under Needs you")

		_, err = s.Service.Approve(t.Context(), []int64{m.ID})
		require.NoError(t, err)
		s.Advance(2*time.Minute, false)

		assert.Len(t, s.GitHub.Merges(), 1, "Merge lets it go")
	})
}

// reviewThenPost is Review members' PRs: a read-only review in the background, then post
// its review as st says.
func reviewThenPost(st spec.ReviewStep) spec.Spec {
	sp := sim.Reviews()
	sp.Steps = append(sp.Steps, spec.Step{PostReview: &st})

	return sp
}

// memberPR is a member's pull request, #1, whose one file is main.go.
func memberPR() sim.Item {
	return sim.Item{
		Number: 1, Watched: true, Checks: passing, Passed: 4, Author: "octo-member", Association: "MEMBER",
		Files: []string{"main.go"},
	}
}

// reviewed starts the routine over item in mode, has its files checked as an unattended
// run's start does when check says so, and its agent finish with review.
func reviewed(t *testing.T, it sim.Item, st spec.ReviewStep, mode autopilotprefs.Mode, check bool,
	review autopilot.AgentReview,
) *sim.Sim {
	t.Helper()

	s := sim.New(t)
	s.World.Add(it)

	if it.Association != "MEMBER" {
		// Agents run, read only, on outside contributors' items of the repository.
		s.Outside = &sim.Outside{}
		s.Outside.Allow(sim.Owner+"/"+sim.Repo, true)
	}

	s.Begin()

	r := s.Save("Review members", reviewThenPost(st), mode)
	s.Enable(r.ID, autopilot.CurrentRun)
	s.Refresh()

	m := only(t, s)
	if mode == autopilotprefs.ModeAsk {
		_, err := s.Service.Approve(t.Context(), []int64{m.ID})
		require.NoError(t, err)
		s.Advance(2*time.Minute, false)
	}

	if check {
		_, err := s.Routines.CheckFilesUnattended(t.Context(), m.Item.ID)
		require.NoError(t, err)
	}

	s.Runner.Finish(runningMatch(t, s).Runs[0].ID, autopilot.RunEnd{Summary: "Finished: reviewed.", Review: &review})
	s.Advance(2*time.Minute, false)

	return s
}

// approval is the agent's approving review, with one line comment.
func approval() autopilot.AgentReview {
	return autopilot.AgentReview{
		Verdict: autopilot.ReviewApprove, Body: "Looks good.",
		Comments: []autopilot.ReviewLine{{Path: "main.go", Line: 3, Body: "Nice."}},
	}
}

func TestAnAgentsReviewIsPostedAsItsVerdict(t *testing.T) {
	t.Parallel()

	verdict := spec.ReviewStep{As: catalog.ReviewAsVerdict, AllowApprove: true}

	for name, c := range map[string]struct {
		st     spec.ReviewStep
		review autopilot.AgentReview
		event  autopilot.ReviewEvent
	}{
		"an approval, allowed":  {st: verdict, review: approval(), event: autopilot.EventApprove},
		"a request for changes": {st: verdict, review: autopilot.AgentReview{Verdict: autopilot.ReviewRequestChanges, Body: "Fix the test."}, event: autopilot.EventRequestChanges},
		"a comment":             {st: verdict, review: autopilot.AgentReview{Verdict: autopilot.ReviewComment, Body: "A thought."}, event: autopilot.EventComment},
		"posted as a comment":   {st: spec.ReviewStep{As: catalog.ReviewAsComment}, review: approval(), event: autopilot.EventComment},
		"approving not ticked":  {st: spec.ReviewStep{As: catalog.ReviewAsVerdict}, review: approval(), event: autopilot.EventComment},
		"changes, as a comment": {st: spec.ReviewStep{As: catalog.ReviewAsComment}, review: autopilot.AgentReview{Verdict: autopilot.ReviewRequestChanges, Body: "x"}, event: autopilot.EventComment},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			synctest.Test(t, func(t *testing.T) {
				s := reviewed(t, memberPR(), c.st, autopilotprefs.ModeAuto, true, c.review)

				posted := s.GitHub.Reviews()
				require.Len(t, posted, 1, "%+v", s.Ledger())
				assert.Equal(t, c.event, posted[0].Event, "%+v", only(t, s).Runs)
				assert.Equal(t, c.review.Body, posted[0].Body)
				assert.Equal(t, sim.Commit(1, 1), posted[0].Head, "pinned to the commit the agent read")
				assert.Len(t, posted[0].Comments, len(c.review.Comments))

				m := only(t, s)
				assert.Equal(t, autopilot.StateDone, m.State, m.Words)
				assert.Equal(t, "done", m.Steps[1].Status)

				week, err := s.Service.Stats(t.Context(), m.RoutineID)
				require.NoError(t, err)
				assert.Equal(t, 1, week.Reviews, "This week counts the review posted")
				assert.Equal(t, 1, week.Runs, "and the agent's run")
			})
		})
	}
}

func TestApprovingIsHeldBackWhereARoutineMayNotApprove(t *testing.T) {
	t.Parallel()

	verdict := spec.ReviewStep{As: catalog.ReviewAsVerdict, AllowApprove: true}
	outsider := memberPR()
	outsider.Author, outsider.Association = "stranger", "CONTRIBUTOR"

	sensitive := memberPR()
	sensitive.Files = []string{"main.go", ".github/workflows/ci.yml"}

	for name, c := range map[string]struct {
		item  sim.Item
		check bool
		why   string
	}{
		"files not checked":        {item: memberPR(), why: "every file was checked"},
		"a sensitive file":         {item: sensitive, check: true, why: "every file was checked and none is sensitive"},
		"an outside contributor's": {item: outsider, check: true, why: "a member's or a trusted bot's"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			synctest.Test(t, func(t *testing.T) {
				got := reviewed(t, c.item, verdict, autopilotprefs.ModeAuto, c.check, approval())

				if m := only(t, got); m.BlockedBy == autopilot.BlockOutside {
					// A write to an outside contributor's item waits for the user's OK first.
					_, err := got.Service.Approve(t.Context(), []int64{m.ID})
					require.NoError(t, err)
					got.Advance(2*time.Minute, false)
				}

				posted := got.GitHub.Reviews()
				require.Len(t, posted, 1, "%+v", got.Ledger())
				assert.Equal(t, autopilot.EventComment, posted[0].Event, "posted as a comment, never an approval")
				assert.Contains(t, only(t, got).Runs[1].Summary, c.why)
			})
		})
	}
}

func TestNoReviewIsPostedOnAHeadPushedToAfterTheAgentReadIt(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		s := sim.New(t)
		s.World.Add(memberPR())
		s.Begin()

		r := s.Save("Review members", reviewThenPost(spec.ReviewStep{As: catalog.ReviewAsVerdict, AllowApprove: true}),
			autopilotprefs.ModeAuto)
		s.Enable(r.ID, autopilot.CurrentRun)
		s.Refresh()

		s.GitHub.OnFollowUp(func(number int) { s.World.Push(number, sim.Commit(number, 5)) })
		s.Runner.Finish(only(t, s).Runs[0].ID, autopilot.RunEnd{Review: new(approval())})
		s.Advance(2*time.Minute, false)

		assert.Empty(t, s.GitHub.Reviews(), "a push since posts nothing")
		assert.Equal(t, autopilot.ReasonNotOffered, only(t, s).Reason)
	})
}

func TestAnAskFirstReviewWaitsWithItsTextAndCanPostAsAComment(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		s := reviewed(t, memberPR(), spec.ReviewStep{As: catalog.ReviewAsVerdict, AllowApprove: true},
			autopilotprefs.ModeAsk, true, approval())

		m := only(t, s)
		require.Equal(t, autopilot.BlockAsk, m.BlockedBy, m.Words)
		assert.Equal(t, "The agent's review is ready: waiting for your OK to post it.", m.Words)
		require.NotNil(t, m.Steps[0].Review, "the card shows the review's text")
		assert.Equal(t, "Looks good.", m.Steps[0].Review.Body)
		assert.Equal(t, string(catalog.OutcomeApprove), m.Steps[0].Outcome, "its verdict is the agent step's outcome")
		assert.True(t, m.Steps[1].AllowApprove)
		assert.Empty(t, s.GitHub.Reviews())

		_, err := s.Service.PostAsComment(t.Context(), m.ID)
		require.NoError(t, err)
		s.Advance(2*time.Minute, false)

		posted := s.GitHub.Reviews()
		require.Len(t, posted, 1)
		assert.Equal(t, autopilot.EventComment, posted[0].Event, "Post as comment posts it as a comment")
		assert.Equal(t, autopilot.PostAsComment, only(t, s).Steps[1].PostAs)
	})
}

func TestAReadOnlyRunWithNoReviewSkipsThePost(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		s := sim.New(t)
		s.World.Add(memberPR())
		s.Begin()

		r := s.Save("Review members", reviewThenPost(spec.ReviewStep{As: catalog.ReviewAsComment}), autopilotprefs.ModeAuto)
		s.Enable(r.ID, autopilot.CurrentRun)
		s.Refresh()

		s.Runner.Finish(only(t, s).Runs[0].ID, autopilot.RunEnd{Summary: "Finished: nothing to say."})
		s.Advance(2*time.Minute, false)

		m := only(t, s)
		assert.Equal(t, autopilot.StateDone, m.State)
		assert.Equal(t, "skipped", m.Steps[1].Status)
		assert.Equal(t, "The agent wrote no review to post.", m.Steps[1].Note)
		assert.Empty(t, s.GitHub.Reviews())
	})
}
