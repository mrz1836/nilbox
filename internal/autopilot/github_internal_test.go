package autopilot

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/autopilot/catalog"
	"github.com/mrz1836/nilbox/internal/autopilot/internal/queries"
	"github.com/mrz1836/nilbox/internal/autopilot/spec"
	"github.com/mrz1836/nilbox/internal/autopilotprefs"
)

// githubRun is a GitHub action of match, started at, as the day's counts read it.
func githubRun(match int64, at time.Time, outcome RunOutcome) queries.ListRunsSinceRow {
	return queries.ListRunsSinceRow{
		MatchID: match, RoutineID: 1, Runner: runnerGitHub, Status: runEnded, Outcome: string(outcome), QueuedAt: at,
		StartedAt: new(at),
	}
}

func TestThePaceCountsTheGitHubActionsOfTheLastMinuteAndHour(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.November, 16, 9, 30, 0, 0, time.UTC)
	d := dayCounts{
		routineRuns: map[int64]int{}, activeItems: map[int64]bool{}, counted: map[int64]map[int64]bool{}, now: now,
		spend: map[autopilotprefs.Unit]float64{}, routineSpend: map[int64]map[autopilotprefs.Unit]float64{},
	}
	day := dayStart(now)

	for i := range ActionsPerHour {
		d.add(githubRun(int64(i/2), now.Add(-time.Duration(i)*45*time.Second), RunDone), day)
	}

	d.add(githubRun(100, now.Add(-2*time.Hour), RunDone), day)
	d.add(githubRun(101, now.Add(-10*time.Second), RunNotSent), day)
	d.add(queries.ListRunsSinceRow{
		MatchID: 102, RoutineID: 1, Runner: "background", Status: runEnded,
		Outcome: string(RunDone), QueuedAt: now, StartedAt: new(now),
	}, day)

	assert.Equal(t, 2, d.paceMinute, "the two started under a minute ago")
	assert.Equal(t, ActionsPerHour, d.paceHour, "not one two hours old, one that sent nothing, nor an agent's")
	assert.Equal(t, ActionsPerHour+1, d.actions, "today's GitHub actions count each one sent")
	assert.Equal(t, ActionsPerHour/2+2, d.routineRuns[1], "a match is one of its routine's runs, its actions many")

	p := &pass{e: &Engine{}, now: now, counts: d}
	v := p.pace()
	assert.Equal(t, BlockWritesHeld, v.block)
	assert.Contains(t, v.words, "60 GitHub actions in the last hour")

	p.counts.paceHour, p.counts.paceMinute = 0, ActionsPerMinute
	assert.Contains(t, p.pace().words, "10 GitHub actions in the last minute")

	p.counts.paceMinute = 0
	assert.True(t, p.pace().through())

	p.e.holdWrites(now.Add(time.Minute), "GitHub asked to wait.")
	assert.Equal(t, "GitHub asked to wait.", p.pace().words, "the gate's hold holds until it said")

	p.now = now.Add(time.Minute)
	assert.True(t, p.pace().through())
}

func TestATryIsAMatchHoweverManyRunsItStarts(t *testing.T) {
	t.Parallel()

	at := time.Date(2026, time.November, 16, 9, 0, 0, 0, time.UTC)
	runs := []itemRun{
		{MatchID: 1, RoutineID: 7, Counted: true, QueuedAt: at},
		{MatchID: 1, RoutineID: 7, Counted: true, QueuedAt: at},
		{MatchID: 2, RoutineID: 7, Counted: true, QueuedAt: at},
		{MatchID: 3, RoutineID: 7, Counted: false, QueuedAt: at},
		{MatchID: 4, RoutineID: 8, Counted: true, QueuedAt: at},
	}

	assert.Equal(t, 2, history(7, runs, baseline{}, false, time.Time{}, "").Tries)
}

// errBroken is a send that failed after GitHub may have taken it.
var errBroken = errors.New("broken")

// fakeActions are the sheets' services as the GitHub dispatcher reaches them.
type fakeActions struct {
	sent  []GitHubSend
	got   GitHubSent
	err   error
	marks int
}

func (f *fakeActions) Read(context.Context, ItemRef) error { return nil }

func (f *fakeActions) Send(_ context.Context, req GitHubSend) (GitHubSent, error) {
	f.sent = append(f.sent, req)

	return f.got, f.err
}

func (f *fakeActions) MarkDone(context.Context, ItemRef) (int, error) { return f.marks, f.err }

func (f *fakeActions) Preview(context.Context, ItemRef, spec.GitHubAction) (string, error) {
	return "", nil
}

func TestTheGitHubDispatcherSendsTheJobsActionAndSaysHowItWent(t *testing.T) {
	t.Parallel()

	step := spec.Step{GitHub: []spec.GitHubAction{
		{Reply: &spec.ReplyAction{Prompt: spec.PromptRef{Nickname: "Question"}}},
		{Labels: &spec.LabelsAction{Add: []string{"needs-repro"}}},
	}}
	job := Job{Item: ItemRef{ID: 9, Number: 9}, Head: "abc", Step: step, Action: 1, Accept: "ok"}

	for _, c := range []struct {
		got  GitHubSent
		want Result
	}{
		{GitHubSent{Outcome: GitHubSentIt, Words: "Labeled."}, Result{Sent: true, Summary: "Labeled."}},
		{GitHubSent{Outcome: GitHubNothing, Words: "It has them."}, Result{Summary: "It has them."}},
		{GitHubSent{Outcome: GitHubNotOffered, Words: "it's closed"}, Result{NotOffered: "It's closed."}},
		{GitHubSent{Outcome: GitHubWarning, Words: "Careful."}, Result{Warning: "Careful."}},
		{GitHubSent{Outcome: GitHubPushedSince}, Result{PushedSince: true, Summary: pushedSinceWords}},
		{GitHubSent{Outcome: GitHubUnread, Words: "Offline."}, Result{PushedSince: true, Summary: "Offline."}},
		{
			GitHubSent{Outcome: GitHubHeld, Words: "Slow down.", RetryIn: time.Minute},
			Result{Held: "Slow down.", RetryIn: time.Minute},
		},
	} {
		f := &fakeActions{got: c.got}
		got, err := NewGitHub(f).Dispatch(t.Context(), job)
		require.NoError(t, err)
		assert.Equal(t, c.want, got, c.got.Outcome)
		require.Len(t, f.sent, 1)
		assert.Equal(t, catalog.GitHubLabels, f.sent[0].Action.Kind(), "the job's own action")
		assert.Equal(t, "abc", f.sent[0].Head)
		assert.Equal(t, "ok", f.sent[0].Accept)
	}

	_, err := NewGitHub(&fakeActions{}).Dispatch(t.Context(), Job{Step: step, Action: 2})
	require.ErrorIs(t, err, errNoAction)

	_, err = NewGitHub(&fakeActions{err: errBroken}).Dispatch(t.Context(), job)
	require.ErrorIs(t, err, errBroken, "a failure after which GitHub may have taken it fails the step")

	marked, err := NewGitHub(&fakeActions{marks: 2}).Dispatch(t.Context(), Job{Step: spec.Step{MarkDone: &spec.Empty{}}})
	require.NoError(t, err)
	assert.Equal(t, "Marked 2 GitHub notifications done.", marked.Summary)

	none, err := NewGitHub(&fakeActions{}).Dispatch(t.Context(), Job{Step: spec.Step{MarkDone: &spec.Empty{}}})
	require.NoError(t, err)
	assert.False(t, none.Sent)
	assert.Equal(t, "It had no GitHub notification to mark done.", none.Summary)
}
