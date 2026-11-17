package sim_test

import (
	"os"
	"path/filepath"
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
	"github.com/mrz1836/nilbox/internal/inbox"
	"github.com/mrz1836/nilbox/internal/item"
)

// The GitHub step's actions, through the real GitHub dispatcher over the fake of the
// sheets: each pinned to the head read right before it, decided again by the sheet, its
// own run, paced, and never sent twice.

// rebaseWhatConflicts is the "Ask Dependabot to rebase what conflicts" template, exactly as
// the template gallery holds it: Dependabot's pull requests, merge conflicts, Ask
// Dependabot to rebase, once per head commit.
func rebaseWhatConflicts(t *testing.T) spec.Spec {
	t.Helper()

	data, err := os.ReadFile(filepath.Join("..", "spec", "testdata", "templates", "ask-dependabot-to-rebase.json"))
	require.NoError(t, err)

	sp, err := spec.Load(data)
	require.NoError(t, err)

	return sp
}

// conflictsOn is Merge conflicts' fire key on commit c of #number.
func conflictsOn(number, c int) string {
	return "merge_conflicts:commit:" + sim.Commit(number, c)
}

// heads returns the heads each send was pinned to, in order.
func heads(sends []autopilot.GitHubSend) []string {
	out := make([]string, 0, len(sends))
	for _, s := range sends {
		out = append(out, s.Head)
	}

	return out
}

func TestAskDependabotToRebaseWhatConflicts(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		s := sim.New(t)
		s.World.Add(sim.Item{Number: 1, Watched: true, Conflicts: true})
		s.World.Add(sim.Item{Number: 2, Watched: true})
		s.Begin()
		s.Refresh()

		r := s.Save("Ask Dependabot to rebase what conflicts", rebaseWhatConflicts(t), autopilotprefs.ModeAuto)
		s.Enable(r.ID, autopilot.CurrentRun)
		s.Refresh()
		s.Refresh()

		sends := s.GitHub.Jobs()
		require.Len(t, sends, 1, "only the pull request that conflicts")
		assert.Equal(t, 1, sends[0].Item.Number)
		assert.Equal(t, catalog.GitHubRebase, sends[0].Action.Kind())
		assert.Equal(t, sim.Commit(1, 1), sends[0].Head, "pinned to the head the read right before it found")
		assert.Equal(t, []int{1}, s.GitHub.Reads(), "the sheet read it once, the read before the start")

		m := only(t, s)
		assert.Equal(t, autopilot.StateDone, m.State)
		assert.Equal(t, conflictsOn(1, 1), m.FireKey)
		require.Len(t, m.Runs, 1)
		assert.Equal(t, "github", m.Runs[0].Runner)
		assert.Equal(t, string(catalog.GitHubRebase), m.Runs[0].Action)
		assert.Equal(t, string(autopilot.RunDone), m.Runs[0].Outcome)
		assert.Equal(t, sim.Commit(1, 1), m.Runs[0].HeadBefore)

		s.Advance(10*time.Minute, true)
		assert.Len(t, s.GitHub.Jobs(), 1, "once per head commit")

		// Dependabot rebases, and the new commit conflicts too: the routine asks again, once,
		// after the wait between tries.
		s.World.Push(1, sim.Commit(1, 2))
		s.Advance(10*time.Minute, true)
		require.Len(t, s.GitHub.Jobs(), 1, "it waits between tries")
		s.Advance(30*time.Minute, true)

		sends = s.GitHub.Jobs()
		require.Len(t, sends, 2)
		assert.Equal(t, sim.Commit(1, 2), sends[1].Head)

		// Its next rebase clears the conflict: nothing more is asked.
		s.World.Push(1, sim.Commit(1, 3))
		s.World.Conflict(1, false)
		s.Advance(10*time.Minute, true)

		assert.Len(t, s.GitHub.Jobs(), 2)
		assert.Equal(t, []sim.Row{
			row(1, autopilot.StateDone, "", autopilot.ReasonRan, conflictsOn(1, 1)),
			row(1, autopilot.StateDone, "", autopilot.ReasonRan, conflictsOn(1, 2)),
		}, s.Ledger())
	})
}

func TestAPullRequestThatStopsConflictingBeforeTheSendIsLeftAlone(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		s := sim.New(t)
		s.World.Add(sim.Item{Number: 1, Watched: true, Conflicts: true})
		s.Begin()
		s.Refresh()

		r := s.Save("Rebase", rebaseWhatConflicts(t), autopilotprefs.ModeAsk)
		s.Enable(r.ID, autopilot.CurrentRun)
		s.Refresh()

		m := only(t, s)
		require.Equal(t, autopilot.BlockAsk, m.BlockedBy, "Ask first waits for the user's OK")

		// The conflict goes before the user says yes: the read before the start finds it.
		s.World.Conflict(1, false)

		_, err := s.Service.Approve(t.Context(), []int64{m.ID})
		require.NoError(t, err)
		s.Refresh()
		s.Refresh()

		assert.Empty(t, s.GitHub.Jobs(), "nothing is sent once the routine no longer matches")
		assert.Equal(t, autopilot.ReasonNoLongerMatches, only(t, s).Reason)
	})
}

func TestAGitHubActionPushedToSinceItsReadSendsNothingAndIsNoTry(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		s := sim.New(t)
		s.World.Add(sim.Item{Number: 1, Watched: true})
		s.Begin()

		once := false

		s.GitHub.OnSend(func(autopilot.GitHubSend) {
			if !once {
				once = true

				s.World.Push(1, sim.Commit(1, 2)) // a push lands between the read and the send
			}
		})

		sp := sim.Rebases()
		sp.Limits.TriesPerItem = new(1)

		r := s.Save("Rebase", sp, autopilotprefs.ModeAuto)
		s.Enable(r.ID, autopilot.CurrentRun)
		s.Refresh()
		s.Refresh()
		s.Advance(2*time.Minute, true)

		assert.Equal(t, []string{sim.Commit(1, 1), sim.Commit(1, 2)}, heads(s.GitHub.Tried()),
			"read again and pinned to the new head")
		assert.Equal(t, []string{sim.Commit(1, 2)}, heads(s.GitHub.Jobs()), "sent once, at the head it read")

		m := only(t, s)
		assert.Equal(t, autopilot.StateDone, m.State, "the push since was no try: one try was enough")
		require.Len(t, m.Runs, 2)
		assert.Equal(t, string(autopilot.RunNotSent), m.Runs[0].Outcome)
		assert.Equal(t, string(autopilot.RunDone), m.Runs[1].Outcome)
	})
}

func TestAWarningWaitsForTheUsersOKAndGoesWithItAccepted(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		const warning = "Dependabot stops rebasing a pull request once someone else changes its branch."

		s := sim.New(t)
		s.World.Add(sim.Item{Number: 1, Watched: true})
		s.Begin()
		s.GitHub.Warn(1, warning)

		sp := sim.Rebases()
		sp.Steps = []spec.Step{{GitHub: []spec.GitHubAction{{UpdateBranch: &spec.Empty{}}}}}

		r := s.Save("Update", sp, autopilotprefs.ModeAuto)
		s.Enable(r.ID, autopilot.CurrentRun)
		s.Refresh()
		s.Refresh()

		m := only(t, s)
		assert.Equal(t, autopilot.StateWaiting, m.State, "a routine never accepts a warning")
		assert.Equal(t, autopilot.BlockWarning, m.BlockedBy)
		assert.Equal(t, warning, m.Words, "the warnings, as the sheet would show them")
		assert.Empty(t, s.GitHub.Jobs())

		_, err := s.Service.Approve(t.Context(), []int64{m.ID})
		require.NoError(t, err)
		s.Refresh()
		s.Refresh()

		sends := s.GitHub.Jobs()
		require.Len(t, sends, 1)
		assert.Equal(t, warning, sends[0].Accept, "sent with the warnings the user accepted")
		assert.Equal(t, autopilot.StateDone, only(t, s).State)
	})
}

func TestAskFirstsOKIsNoAcceptanceOfAWarning(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		s := sim.New(t)
		s.World.Add(sim.Item{Number: 1, Watched: true})
		s.Begin()
		s.GitHub.Warn(1, "It changes a branch Dependabot keeps.")

		r := s.Save("Rebase", sim.Rebases(), autopilotprefs.ModeAsk)
		s.Enable(r.ID, autopilot.CurrentRun)
		s.Refresh()

		_, err := s.Service.Approve(t.Context(), []int64{only(t, s).ID})
		require.NoError(t, err)
		s.Refresh()
		s.Refresh()

		assert.Equal(t, autopilot.BlockWarning, only(t, s).BlockedBy, "the OK to run is no OK of the warning")
		assert.Empty(t, s.GitHub.Jobs())
	})
}

func TestAnActionItsSheetNoLongerOffersClosesTheMatch(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		s := sim.New(t)
		s.World.Add(sim.Item{Number: 1, Watched: true})
		s.Begin()
		s.GitHub.NotOffer(1, "It's up to date with main, so there's nothing to rebase.")

		r := s.Save("Rebase", sim.Rebases(), autopilotprefs.ModeAuto)
		s.Enable(r.ID, autopilot.CurrentRun)
		s.Refresh()
		s.Refresh()

		m := only(t, s)
		assert.Equal(t, autopilot.StateClosed, m.State)
		assert.Equal(t, autopilot.ReasonNotOffered, m.Reason)
		assert.Equal(t, "It's up to date with main, so there's nothing to rebase.", m.Words)
		assert.Empty(t, s.GitHub.Jobs())
	})
}

func TestGitHubActionsKeepToAutopilotsShareOfTheWriteGate(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		const n = autopilot.ActionsPerMinute + 2

		s := sim.New(t)
		for i := 1; i <= n; i++ {
			s.World.Add(sim.Item{Number: i, Watched: true})
		}

		s.Begin()
		s.Refresh()

		r := s.Save("Rebase", manyRebases(), autopilotprefs.ModeAuto)
		s.Enable(r.ID, autopilot.CurrentRun)
		s.Refresh()
		s.Refresh()

		assert.Len(t, s.GitHub.Jobs(), autopilot.ActionsPerMinute, "at most ten a minute")
		assert.Equal(t, 2, held(s), "the rest wait on the pace")

		s.Advance(2*time.Minute, false)
		assert.Len(t, s.GitHub.Jobs(), n, "the rest go once the minute has passed")
	})
}

func TestABacklogsRunOnThemSendsItAtThePace(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		const n = autopilot.FirstCap + 1

		s := sim.New(t)
		for i := 1; i <= n; i++ {
			s.World.Add(sim.Item{Number: i, Watched: true})
		}

		s.Begin()
		s.Refresh()

		r := s.Save("Rebase", manyRebases(), autopilotprefs.ModeAuto)
		s.Enable(r.ID, autopilot.CurrentRun)
		s.Refresh()
		s.Refresh()
		require.Empty(t, s.GitHub.Jobs(), "over the first pass's cap it queues none")

		got, err := s.Service.ApproveBacklog(t.Context(), r.ID)
		require.NoError(t, err)
		assert.Len(t, got, n, "Run on them gives each its OK")

		s.Refresh()
		assert.Len(t, s.GitHub.Jobs(), autopilot.ActionsPerMinute, "at the pace")

		s.Advance(3*time.Minute, false)
		assert.Len(t, s.GitHub.Jobs(), n)
	})
}

// manyRebases is Rebases with the routine's own runs a day at their most, so only the pace
// holds its actions back.
func manyRebases() spec.Spec {
	sp := sim.Rebases()
	sp.Limits.RunsPerDay = new(catalog.MaxRunsPerDay)

	return sp
}

// held counts the matches waiting on the write gate's pace.
func held(s *sim.Sim) int {
	n := 0

	for _, m := range s.Matches() {
		if m.BlockedBy == autopilot.BlockWritesHeld {
			n++
		}
	}

	return n
}

func TestAWriteTheGateRefusesWaitsUntilItSaysTheNextMayGo(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		s := sim.New(t)
		s.World.Add(sim.Item{Number: 1, Watched: true})
		s.Begin()
		s.GitHub.HoldWrites(5 * time.Minute)

		sp := sim.Rebases()
		sp.Limits.TriesPerItem = new(1)

		r := s.Save("Rebase", sp, autopilotprefs.ModeAuto)
		s.Enable(r.ID, autopilot.CurrentRun)
		s.Refresh()
		s.Refresh()

		m := only(t, s)
		assert.Equal(t, autopilot.BlockWritesHeld, m.BlockedBy)
		assert.Contains(t, m.Words, "GitHub asked for a pause")
		assert.Empty(t, s.GitHub.Jobs())

		tried := len(s.GitHub.Tried())
		s.Advance(3*time.Minute, false)
		assert.Len(t, s.GitHub.Tried(), tried, "nothing is tried again while the gate holds")

		s.Advance(3*time.Minute, false)
		assert.Len(t, s.GitHub.Jobs(), 1, "it goes once the gate lets it, as one try")
		assert.Equal(t, autopilot.StateDone, only(t, s).State)
	})
}

// replyAndLabel is a GitHub step of two actions on the watched issues: comment the Question
// reply, then add needs-repro.
func replyAndLabel() spec.Spec {
	sp := sim.Rebases()
	sp.Watch.Sources[0].Search.Kind = item.KindIssue
	sp.Steps = []spec.Step{{GitHub: []spec.GitHubAction{
		{Reply: &spec.ReplyAction{Prompt: spec.PromptRef{Nickname: "Question"}}},
		{Labels: &spec.LabelsAction{Add: []string{"needs-repro"}}},
	}}}
	sp.Limits.TriesPerItem = new(1)

	return sp
}

func TestEachOfAStepsActionsIsARunOfItsOwnAndTheStepOneTry(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		s := sim.New(t)
		s.World.Add(sim.Item{Number: 1, Issue: true, Watched: true})
		s.Begin()

		r := s.Save("Ask for a repro", replyAndLabel(), autopilotprefs.ModeAuto)
		s.Enable(r.ID, autopilot.CurrentRun)
		s.Refresh()
		s.Refresh()
		s.Advance(time.Minute, false)

		sends := s.GitHub.Jobs()
		require.Len(t, sends, 2, "one try lets the whole step go")
		assert.Equal(t, catalog.GitHubReply, sends[0].Action.Kind())
		assert.Equal(t, catalog.GitHubLabels, sends[1].Action.Kind())
		assert.Empty(t, sends[0].Head, "an issue has no head")

		m := only(t, s)
		assert.Equal(t, autopilot.StateDone, m.State)
		require.Len(t, m.Runs, 2, "each action its own run")
		assert.Equal(t, string(catalog.GitHubReply), m.Runs[0].Action)
		assert.Equal(t, string(catalog.GitHubLabels), m.Runs[1].Action)
	})
}

func TestAQuitBetweenAStepsActionsNeverSendsTheFirstAgain(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		s := sim.New(t)
		s.World.Add(sim.Item{Number: 1, Issue: true, Watched: true})
		s.Begin()

		r := s.Save("Ask for a repro", replyAndLabel(), autopilotprefs.ModeAuto)
		s.Enable(r.ID, autopilot.CurrentRun)

		// The comment goes; the label goes out, and the quit cuts off its answer.
		s.GitHub.OnSend(func(req autopilot.GitHubSend) {
			if req.Action.Labels != nil {
				s.GitHub.Hold()
			}
		})
		s.Refresh()
		s.Refresh()
		s.Advance(time.Minute, false)
		require.Len(t, s.GitHub.Tried(), 2)

		s.Crash()
		s.GitHub.OnSend(nil)
		s.GitHub.Release()
		s.Begin()
		s.Refresh()
		s.Advance(3*time.Minute, true)

		assert.Len(t, s.GitHub.Tried(), 2, "neither is sent again on its own")

		m := only(t, s)
		assert.Equal(t, autopilot.BlockInterrupted, m.BlockedBy)

		_, err := s.Service.Approve(t.Context(), []int64{m.ID})
		require.NoError(t, err)
		s.Refresh()
		s.Refresh()

		tried := s.GitHub.Tried()
		require.Len(t, tried, 3, "the user's OK sends what was cut off, once")
		assert.Equal(t, catalog.GitHubLabels, tried[2].Action.Kind(), "and never the comment again")
		assert.Equal(t, autopilot.StateDone, only(t, s).State)
	})
}

func TestAReplyMadeAnApprovalSinceHoldsTheActionBackAndReadsNothing(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		question := autopilot.LibraryPrompt{
			Starter: "reply-question", Nickname: "Question", Reply: true, ReplyFor: autopilot.ReplyForComment,
			Kinds: item.Kinds(),
		}

		s := sim.New(t)
		s.Library = &sim.Library{}
		s.Library.Put(question)
		s.World.Add(sim.Item{Number: 1, Issue: true, Watched: true})
		s.Begin()

		r := s.Save("Ask for a repro", replyAndLabel(), autopilotprefs.ModeAuto)

		// The user turns the reply into an approval's after saving the routine.
		question.ReplyFor = autopilot.ReplyForApproval
		s.Library.Put(question)

		s.Enable(r.ID, autopilot.CurrentRun)
		s.Refresh()
		s.Refresh()

		m := only(t, s)
		assert.Equal(t, autopilot.StateBlocked, m.State)
		assert.Equal(t, autopilot.BlockNotReady, m.BlockedBy)
		assert.Contains(t, m.Words, "a GitHub step only comments")
		assert.Empty(t, s.GitHub.Reads(), "nothing is read from GitHub for it")
		assert.Empty(t, s.GitHub.Tried())
	})
}

func TestAnOutsideContributorsItemWaitsForTheUsersOKInEveryMode(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		s := sim.New(t)
		s.World.Add(sim.Item{Number: 1, Watched: true, Author: "stranger", Bot: false, Association: "NONE"})
		s.Begin()

		r := s.Save("Rebase", sim.Rebases(), autopilotprefs.ModeAuto)
		s.Enable(r.ID, autopilot.CurrentRun)
		s.Refresh()
		s.Refresh()

		m := only(t, s)
		assert.Equal(t, autopilot.BlockOutside, m.BlockedBy, "what it says is visible to them")
		assert.Empty(t, s.GitHub.Jobs())

		_, err := s.Service.Approve(t.Context(), []int64{m.ID})
		require.NoError(t, err)
		s.Refresh()
		s.Refresh()

		assert.Len(t, s.GitHub.Jobs(), 1, "sent once the user said yes")
	})
}

func TestMarkTheNotificationDoneRunsAtOnceWithTheLocalSteps(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		s := sim.New(t)
		s.World.Add(sim.Item{Number: 1, Watched: true})
		s.Begin()
		s.GitHub.Marks(2)

		sp := sim.Rebases()
		sp.Steps = []spec.Step{{MarkDone: &spec.Empty{}}}

		r := s.Save("Done", sp, autopilotprefs.ModeAsk)
		s.Enable(r.ID, autopilot.CurrentRun)
		s.Refresh()
		s.Refresh()

		assert.Equal(t, []int{1}, s.GitHub.Marked(), "at once, even in Ask first")

		m := only(t, s)
		assert.Equal(t, autopilot.StateDone, m.State)
		assert.Empty(t, m.Runs, "a local step is no run")
		require.Len(t, m.Steps, 1)
		assert.Equal(t, "Marked 2 GitHub notifications done.", m.Steps[0].Note)
	})
}

func TestPreviewSaysWhatEachActionWouldSend(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		s := sim.New(t)
		s.World.Add(sim.Item{Number: 1, Listed: true})
		s.Begin()
		s.Refresh()

		sp := sim.Rebases()
		sp.Steps = []spec.Step{
			{GitHub: []spec.GitHubAction{{Rebase: &spec.Empty{}}, {Labels: &spec.LabelsAction{Add: []string{"deps"}}}}},
			{MarkDone: &spec.Empty{}},
		}

		got, err := s.Service.PreviewActions(t.Context(), autopilot.Draft{Name: "Preview", Spec: sp}, itemID(t, s, 1))
		require.NoError(t, err)
		require.Len(t, got, 3)
		assert.Equal(t, "@dependabot rebase", got[0].Sends)
		assert.Equal(t, "Ask Dependabot to rebase", got[0].Label)
		assert.Equal(t, "Adds deps", got[1].Sends)
		assert.Equal(t, catalog.StepMarkDone, got[2].Kind)
		assert.Empty(t, s.GitHub.Tried(), "a preview sends nothing")
	})
}

// itemID returns the inbox's ID of the scripted item number, in the Authored list.
func itemID(t *testing.T, s *sim.Sim, number int) int64 {
	t.Helper()

	ids, err := s.Store.WatchedItems(t.Context(), nil, inbox.Lists{Authored: true})
	require.NoError(t, err)

	for _, id := range ids {
		got, err := s.Store.ItemStates(t.Context(), []int64{id})
		require.NoError(t, err)

		if got[id].Ref.Number == number {
			return id
		}
	}

	require.FailNow(t, "no such item", "#%d", number)

	return 0
}

func TestAGitHubActionThatWaitsForTheUserTellsTheirNeedsYourOKOnce(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		s := sim.New(t)
		s.TellWaits = true
		s.World.Add(sim.Item{Number: 1, Watched: true})
		s.Begin()
		s.GitHub.Warn(1, "It changes a branch Dependabot keeps.")

		r := s.Save("Rebase", sim.Rebases(), autopilotprefs.ModeAuto)
		s.Enable(r.ID, autopilot.CurrentRun)
		s.Refresh()
		s.Refresh()
		s.Advance(3*time.Minute, true)

		m := only(t, s)
		require.Equal(t, autopilot.BlockWarning, m.BlockedBy)

		var needs []autopilot.Notice

		for _, n := range s.Notices.Told() {
			if n.Kind == autopilot.NoticeNeedsYou {
				needs = append(needs, n)
			}
		}

		require.Len(t, needs, 1, "one Needs your OK, however often a cycle sees it wait")
		assert.Equal(t, m.ID, needs[0].MatchID, "its banner opens its card")
		assert.Contains(t, needs[0].Body, "It changes a branch Dependabot keeps.", "with the warnings to accept")
	})
}
