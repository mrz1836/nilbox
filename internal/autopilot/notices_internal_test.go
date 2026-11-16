package autopilot

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/autopilot/catalog"
	"github.com/mrz1836/nilbox/internal/autopilot/eval"
	"github.com/mrz1836/nilbox/internal/autopilot/spec"
	"github.com/mrz1836/nilbox/internal/item"
)

var errTold = errors.New("the notifications are full")

// heard records the notices told, or fails with err.
type heard struct {
	got []Notice
	err error
}

func (h *heard) Notice(_ context.Context, n Notice) error {
	if h.err != nil {
		return h.err
	}

	h.got = append(h.got, n)

	return nil
}

// noticeJob is a Notify me on acme-org/example-repo #88.
func noticeJob() Job {
	return Job{
		MatchID: 12, RoutineID: 3, RoutineName: "Tell me about security failures",
		MatchCreated: time.Date(2026, 11, 16, 9, 0, 0, 0, time.UTC),
		Item: ItemRef{
			ID: 40, Kind: item.KindPR, Repo: "acme-org/example-repo", Number: 88, Title: "Bump golang.org/x/net",
			URL: "https://github.com/acme-org/example-repo/pull/88",
		},
		Step:   spec.Step{Notify: &spec.NotifyStep{Note: "Have a look before lunch"}},
		Reason: "1 check failed on a1b2c3d; a failed check's name contains security (Security / gitleaks)",
	}
}

func TestNotifyMeTellsWhyWithTheUsersLine(t *testing.T) {
	t.Parallel()

	h := &heard{}

	res, err := NewNotices(h).Dispatch(t.Context(), noticeJob())
	require.NoError(t, err)
	assert.Equal(t, Result{Summary: "Told you."}, res)

	require.Len(t, h.got, 1)

	job := noticeJob()
	assert.Equal(t, Notice{
		Kind: NoticeAlert, RoutineID: 3, Routine: "Tell me about security failures", MatchID: 12,
		CreatedAt: job.MatchCreated, Item: &job.Item, Title: "Bump golang.org/x/net",
		Body: "1 check failed on a1b2c3d; a failed check's name contains security (Security / gitleaks). " +
			"Have a look before lunch.",
	}, h.got[0])
}

func TestTheBannerChoiceIsTheStepsThenTheRoutines(t *testing.T) {
	t.Parallel()

	for name, c := range map[string]struct {
		routine, step *bool
		want          *bool
	}{
		"neither says":           {},
		"the routine says":       {routine: new(false), want: new(false)},
		"the step says":          {step: new(true), want: new(true)},
		"the step over the rest": {routine: new(false), step: new(true), want: new(true)},
	} {
		job := noticeJob()
		job.Notices = &spec.Notices{Alert: c.routine}
		job.Step.Notify.Banner = c.step

		assert.Equal(t, c.want, alertOf(job).Banner, name)
	}
}

func TestTellMeSaysHowTheStepsBeforeItWent(t *testing.T) {
	t.Parallel()

	before := []MatchStep{
		{Kind: catalog.StepNotify, Label: "Notify me", Status: stepDone, Note: "Told you."},
		{Kind: catalog.StepPin, Label: "Pin", Status: stepSkipped, Note: "Its Only if didn't hold."},
		{Kind: catalog.StepAgent, Label: "Run a coding agent", Status: stepDone, Outcome: string(catalog.OutcomePushed)},
	}

	job := noticeJob()
	job.Step, job.Before = spec.Step{TellMe: &spec.TellStep{}}, before

	n := alertOf(job)
	assert.Equal(t, NoticeAlert, n.Kind)
	assert.Equal(t, "It reached Tell me. Notify me: done. Pin: skipped (its Only if didn't hold). "+
		"Run a coding agent: done, it pushed.", n.Body)

	job.Step = spec.Step{TellMe: &spec.TellStep{When: catalog.TellVerdict}}
	assert.Equal(t, "Notify me: done. Pin: skipped (its Only if didn't hold). Run a coding agent: done, it pushed.",
		alertOf(job).Body, "a verdict says how they went alone")

	job.Before = nil
	assert.Equal(t, "It reached Tell me.", alertOf(job).Body, "with nothing before it, it says it was reached")
	assert.Equal(t, "Pin: not run.", soFar([]MatchStep{{Label: "Pin"}}))
}

func TestANoticeThatCantBeToldFailsItsStep(t *testing.T) {
	t.Parallel()

	_, err := NewNotices(&heard{err: errTold}).Dispatch(t.Context(), noticeJob())
	require.ErrorIs(t, err, errTold)

	_, err = NewNotices(nil).Dispatch(t.Context(), noticeJob())
	require.ErrorIs(t, err, errNoSink)
}

func TestANoticeAboutAnItemWithNoTitleNamesItsRepository(t *testing.T) {
	t.Parallel()

	job := noticeJob()
	job.Item.Title = ""

	assert.Equal(t, "acme-org/example-repo", alertOf(job).Title)
}

func TestTheReasonIsWhatFiredAndWhatHeld(t *testing.T) {
	t.Parallel()

	trace := []eval.Trace{
		{Path: "trigger", Condition: "Checks fail: when checks fail", Result: eval.Yes, Actual: "1 check failed on a1b2c3d"},
		{Path: "conditions.of[0]", Condition: "a failed check's name contains security", Result: eval.Yes, Actual: "Security / gitleaks"},
		{Path: "conditions.of[1]", Condition: "it isn't a draft", Result: eval.Yes},
		{Path: "conditions.of[2]", Condition: "the author is dependabot", Result: eval.No, Actual: "renovate"},
	}

	assert.Equal(t, "1 check failed on a1b2c3d; a failed check's name contains security (Security / gitleaks); "+
		"it isn't a draft", reasonOf(trace))
	assert.Empty(t, reasonOf(nil))

	long := []eval.Trace{{Path: "trigger", Result: eval.Yes, Actual: strings.Repeat("checks failed ", 40)}}
	assert.LessOrEqual(t, len([]rune(reasonOf(long))), maxReasonRunes, "a long reason is cut")
	assert.NotContains(t, reasonOf([]eval.Trace{{Path: "trigger", Actual: "one\nline"}}), "\n", "on one line")
}

func TestAProblemIsANoticeAboutNoItem(t *testing.T) {
	t.Parallel()

	h := &heard{}
	made := time.Date(2026, 11, 1, 8, 0, 0, 0, time.UTC)

	require.NoError(t, NewProblemNotices(h).Problem(t.Context(), ProblemNotice{
		Problem: Problem{RoutineID: 3, Kind: ProblemSearchFailed, Detail: "GitHub couldn't answer its search: timeout"},
		Routine: "Tell me about security failures", RoutineCreated: made, Banner: new(true), Burst: true,
	}))
	require.NoError(t, NewProblemNotices(h).Problem(t.Context(), ProblemNotice{
		Problem: Problem{RoutineID: 4, Kind: ProblemWontLoad, Detail: "Made by a newer version"},
	}))

	assert.Equal(t, []Notice{
		{
			Kind: NoticeProblem, RoutineID: 3, Routine: "Tell me about security failures", CreatedAt: made,
			Title: "Tell me about security failures", Body: "GitHub couldn't answer its search: timeout.",
			Banner: new(true), Burst: true,
		},
		{Kind: NoticeProblem, RoutineID: 4, Title: "A routine", Body: "Made by a newer version."},
	}, h.got, "a routine whose name can't be read is still told of")

	require.ErrorIs(t, NewProblemNotices(nil).Problem(t.Context(), ProblemNotice{}), errNoSink)
}

func TestEveryNoticeKind(t *testing.T) {
	t.Parallel()

	assert.Equal(t, []NoticeKind{"alert", "needsYou", "finished", "checks", "problem"}, NoticeKinds())
}
