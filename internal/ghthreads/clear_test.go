package ghthreads

import (
	"context"
	"errors"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/budget"
	"github.com/mrz1836/nilbox/internal/clock"
	"github.com/mrz1836/nilbox/internal/github"
	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/threadrules"
)

// errToastUnread is a toast setting that can't be read.
var errToastUnread = errors.New("the settings can't be read")

func TestPassQueuesOutsideMergeWithoutHold(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	r.clearing()
	r.gh.set(r.gh.mergedThreads(1))

	got := r.mustPass()
	assert.Equal(t, []int64{firstID}, idsOf(ofKind(got, kindDone)), "merged elsewhere: marked done in the same pass")

	ledger := r.ledger()
	require.Len(t, ledger, 1)
	assert.Equal(t, "merged", ledger[0].trigger)
	assert.Equal(t, kindDone, ledger[0].action)
	assert.Equal(t, "sent", ledger[0].state)
	assert.True(t, ledger[0].sendAfter.Equal(ledger[0].queuedAt), "no hold: GitHub's word needs no Undo")

	_, ok := r.row(viewerLogin, firstID)
	assert.False(t, ok, "done, so out of the mirror")

	snap := r.snapshot()
	assert.Zero(t, snap.Status.Count)
	assert.Empty(t, snap.Queued)
	require.Len(t, snap.Recent, 1)
	assert.Equal(t, ClearSent, snap.Recent[0].State)
	assert.Equal(t, clock.Format(start), snap.Recent[0].At)
}

func TestPassHoldsTheAppsOwnMoment(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		kind    OutcomeKind
		item    item.Kind
		trigger threadrules.Trigger
	}{
		{name: "merged in the app", kind: OutcomeMerged, item: item.KindPR, trigger: threadrules.TriggerMerged},
		{name: "pull request closed in the app", kind: OutcomeClosed, item: item.KindPR, trigger: threadrules.TriggerPRClosed},
		{name: "issue closed in the app", kind: OutcomeClosed, item: item.KindIssue, trigger: threadrules.TriggerIssueClosed},
		{name: "reviewed", kind: OutcomeReviewed, item: item.KindPR, trigger: threadrules.TriggerReviewed},
		{name: "dismissed", kind: OutcomeDismissed, item: item.KindIssue, trigger: threadrules.TriggerDismissed},
		{name: "unassigned", kind: OutcomeUnassigned, item: item.KindIssue, trigger: threadrules.TriggerUnassigned},
		{name: "unrequested", kind: OutcomeUnrequested, item: item.KindPR, trigger: threadrules.TriggerUnassigned},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			thread := prThread(firstID, 1, start)
			if tt.item == item.KindIssue {
				thread = issueThread(firstID, 1, start)
			}

			r := clearingRig(t, thread)
			r.clock.Advance(3 * time.Second)

			got := r.settle(outcome(tt.kind, tt.item, 1, r.clock.Now()))
			assert.Nil(t, pagesOf(got), "the mirror is fresh: no listing")
			assert.Empty(t, writes(got), "held for the Undo")

			ledger := r.ledger()
			require.Len(t, ledger, 1)
			assert.Equal(t, string(tt.trigger), ledger[0].trigger)
			assert.Equal(t, "queued", ledger[0].state)
			assert.True(t, ledger[0].sendAfter.Equal(r.clock.Now().Add(MinHold)), "heard of now, held 15 seconds")
			assert.Equal(t, MinHold, r.untilSend())

			r.clock.Advance(MinHold - time.Second)
			assert.Empty(t, writes(r.send()), "not before the hold ends")

			r.clock.Advance(time.Second)
			assert.Equal(t, []int64{firstID}, idsOf(ofKind(r.send(), kindDone)))
			assert.Equal(t, "sent", r.ledger()[0].state)
		})
	}
}

func TestHoldFollowsToastSeconds(t *testing.T) {
	t.Parallel()

	for toast, want := range map[int]time.Duration{
		5: 15 * time.Second, 12: 15 * time.Second, 30: 33 * time.Second, 60: 63 * time.Second,
	} {
		assert.Equal(t, want, Hold(toast), "%d seconds", toast)
	}

	for name, tc := range map[string]struct {
		holds HoldSource
		want  time.Duration
	}{
		"toasts of 30 seconds": {holds: fakeHolds{seconds: 30}, want: 33 * time.Second},
		"unreadable":           {holds: fakeHolds{err: errToastUnread}, want: MinHold},
		"none":                 {want: MinHold},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			r := newRig(t)
			r.holds = tc.holds
			r.restart()
			r.clearing()
			r.gh.set([]fakeThread{prThread(firstID, 1, start)})
			r.mustPass()

			r.settle(outcome(OutcomeDismissed, item.KindPR, 1, start))
			assert.True(t, r.ledger()[0].sendAfter.Equal(start.Add(tc.want)))
		})
	}
}

// mergedFrom returns n threads from the ID id on, each about its own pull request from
// number on, the newest updated at newest and each a second older, which the fake says
// GitHub merged as its thread was last updated.
func (f *fakeGitHub) mergedFrom(id int64, number, n int, newest time.Time) []fakeThread {
	f.mu.Lock()
	defer f.mu.Unlock()

	out := make([]fakeThread, 0, n)

	for i := range n {
		ref := exampleRef(strconv.Itoa(number + i))
		at := newest.Add(-time.Duration(i) * time.Second)
		f.states[ref], f.closedAt[ref] = github.StateMerged, at
		out = append(out, prThread(id+int64(i), number+i, at))
	}

	return out
}

func TestFirstPassAfterRuleChangeCapsAt25(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	r.clearing()

	threads := r.gh.mergedThreads(30)
	r.gh.set(threads)

	assert.Empty(t, writes(r.mustPass()), "30 would clear: more than the first pass clears by itself")
	assert.Empty(t, r.ledger(), "none is queued")
	assert.Equal(t, 30, r.snapshot().Backlog, "they wait for review")
	assert.Empty(t, r.state().RulesSeen, "the first pass is still to come")

	preview, err := r.svc.Preview(t.Context())
	require.NoError(t, err)
	assert.Equal(t, 30, preview.Count, "the sheet shows them")

	r.clock.Advance(5 * time.Minute)
	assert.Empty(t, writes(r.mustPass()), "and they keep waiting")

	// The user clears ten from the sheet: the rest are now few enough.
	ids := make([]int64, 0, 10)
	for _, th := range threads[:10] {
		ids = append(ids, th.id)
	}

	_, err = r.svc.Clear(t.Context(), ids)
	require.NoError(t, err)
	assert.Len(t, writes(r.send()), 10, "by hand, no cap")

	r.clock.Advance(5 * time.Minute)
	assert.Len(t, writes(r.mustPass()), 20, "twenty, under the first pass's cap")
	assert.Zero(t, r.snapshot().Backlog)
	assert.NotEmpty(t, r.state().RulesSeen, "the rules are seen")

	// A rule changes, and 26 more would clear: the next pass is the first again.
	r.rules.change(func(rules *threadrules.Rules) { rules.BotActivity = false })
	r.gh.set(r.gh.mergedFrom(firstID+500, 100, 26, start.Add(time.Hour)))
	r.clock.Advance(5 * time.Minute)

	assert.Empty(t, writes(r.mustPass()))
	assert.Equal(t, 26, r.snapshot().Backlog)

	// Exactly as many as the cap clear.
	r.gh.set(r.gh.mergedFrom(firstID+500, 100, FirstCap, start.Add(2*time.Hour)))
	r.clock.Advance(5 * time.Minute)

	assert.Len(t, writes(r.mustPass()), FirstCap)
	assert.Zero(t, r.snapshot().Backlog)
}

func TestLaterPassCapsAt150(t *testing.T) {
	t.Parallel()

	r := clearingRig(t, prThread(firstID+900, 900, start))
	require.NotEmpty(t, r.state().RulesSeen, "a first pass ran, with nothing to clear")

	threads := r.gh.mergedFrom(firstID, 1, Cap+1, start.Add(time.Hour))
	r.gh.set(threads)
	r.clock.Advance(5 * time.Minute)

	assert.Empty(t, writes(r.mustPass()), "151 is more than a pass clears by itself")
	assert.Equal(t, Cap+1, r.snapshot().Backlog)

	_, err := r.svc.Clear(t.Context(), []int64{threads[0].id})
	require.NoError(t, err)
	assert.Len(t, writes(r.send()), 1)

	r.clock.Advance(5 * time.Minute)
	assert.Len(t, writes(r.mustPass()), Cap, "150 clear on their own")
	assert.Zero(t, r.snapshot().Backlog)
}

func TestTuesdayHundredMergedByAppClearUnattended(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	r.gh.gate = budget.NewTracker(r.clock, nil)
	r.restart()
	r.clearing()

	// A first pass already ran under the rules, with nothing to clear.
	r.gh.set([]fakeThread{prThread(firstID+900, 900, start)})
	r.mustPass()
	require.NotEmpty(t, r.state().RulesSeen)

	// A hundred Dependabot pull requests merged by an app at start, outside every list:
	// each thread unread, with a bot's comment five minutes after the merge.
	threads := r.gh.mergedFrom(firstID, 1, 100, start)
	for i := range threads {
		threads[i].reason, threads[i].title = "subscribed", "Bump example-lib from 1.2."+strconv.Itoa(i)+" to 1.3.0"
	}

	r.gh.set(append(threads, prThread(firstID+900, 900, start)))

	for i := range threads {
		r.gh.commented(threads[i].id, int64(5000+i), "Bot", start.Add(5*time.Minute))
	}

	r.clock.Advance(6 * time.Minute)

	got := r.mustPass()
	assert.Len(t, ofKind(got, "comment"), 100, "who commented after the merge, once each, in the one pass")

	sent, passes := writes(got), 1

	// The Syncer: a send as soon as the lane lets the next go, and a pass the interval on
	// once nothing waits.
	for range 1000 {
		if len(sent) == len(threads) {
			break
		}

		if wait, ok := r.svc.untilSend(); ok {
			r.clock.Advance(wait)
			sent = append(sent, writes(r.send())...)

			continue
		}

		r.clock.Advance(DefaultInterval)

		sent, passes = append(sent, writes(r.mustPass())...), passes+1
	}

	require.Len(t, sent, 100, "every one cleared, with no click")
	assert.Equal(t, 1, passes, "all queued by the one pass that found them")

	for i := 1; i < len(sent); i++ {
		assert.GreaterOrEqual(t, sent[i].at.Sub(sent[i-1].at), budget.ChoreGap, "never two within a second")
	}

	took := sent[len(sent)-1].at.Sub(sent[0].at)
	assert.GreaterOrEqual(t, took, 99*time.Second, "about one a second")
	assert.Less(t, took, 2*time.Minute, "within the lane's 50 a minute: about two minutes")

	for _, l := range r.ledger() {
		assert.Equal(t, "sent", l.state)
		assert.Zero(t, l.attempts, "the lane's refusals cost no attempt")
	}

	assert.Equal(t, 1, r.snapshot().Status.Count, "only the open one is left")
}

func TestAfterMomentBotCommentClears(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	r.clearing()
	r.gh.set(r.gh.mergedThreads(1))
	r.gh.commented(firstID, 900, "Bot", start.Add(10*time.Minute))
	r.clock.Advance(11 * time.Minute)

	got := r.mustPass()
	assert.Equal(t, []int64{900}, idsOf(ofKind(got, "comment")), "one REST read of the latest comment")
	assert.Equal(t, []int64{firstID}, idsOf(ofKind(got, kindDone)), "a bot's: seen, so cleared")
}

func TestAfterMomentSubjectURLCountsAsBot(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	r.clearing()

	threads := r.gh.mergedThreads(1)
	threads[0].updated = start.Add(10 * time.Minute) // the latest comment's URL is the subject's own
	r.gh.set(threads)
	r.clock.Advance(11 * time.Minute)

	got := r.mustPass()
	assert.Empty(t, ofKind(got, "comment"), "no request: a merge or a close leaves the subject's URL")
	assert.Equal(t, []int64{firstID}, idsOf(ofKind(got, kindDone)))
}

func TestAfterMomentPersonSticksUntilRead(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	r.clearing()
	r.gh.set(r.gh.mergedThreads(1))
	r.gh.commented(firstID, 900, "User", start.Add(5*time.Minute))
	r.clock.Advance(6 * time.Minute)

	got := r.mustPass()
	assert.Len(t, ofKind(got, "comment"), 1)
	assert.Empty(t, writes(got), "a person commented after the merge: held")

	// A bot comments after the person: the person's comment stays unseen.
	r.gh.commented(firstID, 901, "Bot", start.Add(10*time.Minute))
	r.clock.Advance(5 * time.Minute)

	got = r.mustPass()
	assert.Empty(t, ofKind(got, "comment"), "a person's activity sticks: not asked again")
	assert.Empty(t, writes(got))

	preview, err := r.svc.Preview(t.Context())
	require.NoError(t, err)
	require.Len(t, preview.Held, 1)
	assert.Equal(t, SkipNewActivity, preview.Held[0].Reason)

	// The user reads it on GitHub.
	r.gh.read(firstID)
	r.clock.Advance(5 * time.Minute)

	assert.Equal(t, []int64{firstID}, idsOf(ofKind(r.mustPass(), kindDone)), "read, so seen")
}

func TestAfterMomentCheckedOncePerUpdate(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	r.clearing()
	r.gh.set(r.gh.mergedThreads(1))
	r.gh.commented(firstID, 900, "", start.Add(5*time.Minute)) // a deleted account's: can't tell
	r.clock.Advance(6 * time.Minute)

	assert.Len(t, ofKind(r.mustPass(), "comment"), 1)

	r.clock.Advance(5 * time.Minute)

	got := r.mustPass()
	assert.Empty(t, ofKind(got, "comment"), "the thread didn't change: not asked again")
	assert.Empty(t, writes(got), "still held")

	r.gh.commented(firstID, 901, "", start.Add(12*time.Minute))
	r.clock.Advance(5 * time.Minute)

	assert.Equal(t, []int64{901}, idsOf(ofKind(r.mustPass(), "comment")), "updated: asked once more")

	// No latest comment at all stays unknown, and asks nothing.
	r.gh.update(firstID, start.Add(20*time.Minute))
	r.gh.configure(func(f *fakeGitHub) { f.threads[0].noComment = true })
	r.clock.Advance(5 * time.Minute)

	got = r.mustPass()
	assert.Empty(t, ofKind(got, "comment"))
	assert.Empty(t, writes(got))
}

func TestReadModeMarksReadAndSkipsAlreadyRead(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	r.rules.change(func(rules *threadrules.Rules) { rules.Auto, rules.Action = true, threadrules.ActionRead })

	threads := r.gh.mergedThreads(2)
	threads[1].unread = false
	r.gh.set(threads)

	got := r.mustPass()
	assert.Equal(t, []int64{firstID}, idsOf(ofKind(got, kindRead)), "the unread one is marked read")
	assert.Empty(t, ofKind(got, kindDone), "nothing is marked done")

	ledger := r.ledger()
	require.Len(t, ledger, 1, "the one already read is left alone")
	assert.Equal(t, kindRead, ledger[0].action)
	assert.Equal(t, "sent", ledger[0].state)

	_, ok := r.row(viewerLogin, firstID)
	assert.True(t, ok, "marked read, it stays in the mirror")

	snap := r.snapshot()
	assert.Equal(t, 2, snap.Status.Count)
	assert.Zero(t, snap.Status.Unread, "read now")

	preview, err := r.svc.Preview(t.Context())
	require.NoError(t, err)
	assert.Zero(t, preview.Count)
	require.Len(t, preview.Held, 1)
	assert.Equal(t, SkipAlreadyRead, preview.Held[0].Reason)
	assert.Equal(t, 2, preview.Held[0].Count, "both are read now")
}

func TestPruneOldMomentsAndClears(t *testing.T) {
	t.Parallel()

	r := clearingRig(t, prThread(firstID, 1, start), prThread(firstID+1, 2, start))
	r.settle(outcome(OutcomeDismissed, item.KindPR, 1, start))
	r.clock.Advance(MinHold)
	require.Len(t, writes(r.send()), 1)

	// A moment whose trigger is off never clears; it is dropped all the same.
	r.rules.change(func(rules *threadrules.Rules) { rules.TriggersOff = []threadrules.Trigger{threadrules.TriggerReviewed} })
	r.settle(outcome(OutcomeReviewed, item.KindPR, 2, r.clock.Now()))
	require.Len(t, r.moments(), 2)
	require.Len(t, r.ledger(), 1)

	r.clock.Advance(29 * 24 * time.Hour)
	r.mustPass()
	assert.Len(t, r.moments(), 2, "kept 30 days")
	assert.Len(t, r.ledger(), 1)

	r.clock.Advance(2 * 24 * time.Hour)
	r.mustPass()
	assert.Empty(t, r.moments(), "dropped 30 days after they were heard of")
	assert.Empty(t, r.ledger(), "dropped 30 days after it settled")
}

// endToEnd is one trigger, from what the inbox tells or GitHub's state, to the write it
// sends.
type endToEnd struct {
	name string

	// setup prepares the rig before its pass; act does what makes the thread clear, and
	// returns the requests it sent.
	setup func(r *rig)
	act   func(t *testing.T, r *rig) []listedRequest

	// want is the write expected: done or read.
	want string
}

// heldUntilSent settles o, checks its clear waits the hold, and sends it.
func heldUntilSent(o Outcome) func(t *testing.T, r *rig) []listedRequest {
	return func(t *testing.T, r *rig) []listedRequest {
		t.Helper()

		o.At = r.clock.Now()
		require.Empty(t, writes(r.settle(o)), "held for the Undo")

		r.clock.Advance(MinHold)

		return r.send()
	}
}

// outsideState has GitHub say the fixtures' item 1 is in state, and runs a pass.
func outsideState(state string) func(t *testing.T, r *rig) []listedRequest {
	return func(t *testing.T, r *rig) []listedRequest {
		t.Helper()

		r.gh.configure(func(f *fakeGitHub) { f.states[exampleRef("1")] = state })
		r.gh.update(firstID, start.Add(time.Minute)) // a merge or a close updates the thread
		r.clock.Advance(5 * time.Minute)

		return r.mustPass()
	}
}

func TestTriggersEndToEnd(t *testing.T) {
	t.Parallel()

	pr := func(kind OutcomeKind) Outcome { return outcome(kind, item.KindPR, 1, time.Time{}) }
	issue := func(kind OutcomeKind) Outcome { return outcome(kind, item.KindIssue, 1, time.Time{}) }
	isIssue := func(r *rig) { r.gh.set([]fakeThread{issueThread(firstID, 1, start)}) }

	tests := []endToEnd{
		{name: "T1 merged in the app", act: heldUntilSent(pr(OutcomeMerged)), want: kindDone},
		{name: "T2 merged elsewhere", act: outsideState(github.StateMerged), want: kindDone},
		{name: "T3 closed in the app", act: heldUntilSent(pr(OutcomeClosed)), want: kindDone},
		{name: "T3 closed elsewhere", act: outsideState(github.StateClosed), want: kindDone},
		{name: "T4 closed in the app", setup: isIssue, act: heldUntilSent(issue(OutcomeClosed)), want: kindDone},
		{name: "T4 closed elsewhere", setup: isIssue, act: outsideState(github.StateClosed), want: kindDone},
		{name: "T5 reviewed", act: heldUntilSent(pr(OutcomeReviewed)), want: kindDone},
		{name: "T6 dismissed", act: heldUntilSent(pr(OutcomeDismissed)), want: kindDone},
		{name: "T7 unassigned", setup: isIssue, act: heldUntilSent(issue(OutcomeUnassigned)), want: kindDone},
		{name: "T7 removed as reviewer", act: heldUntilSent(pr(OutcomeUnrequested)), want: kindDone},
		{
			name: "T8 by hand", want: kindDone,
			act: func(t *testing.T, r *rig) []listedRequest {
				t.Helper()

				r.rules.change(func(rules *threadrules.Rules) { rules.Auto = false })
				_, err := r.svc.Clear(t.Context(), []int64{firstID})
				require.NoError(t, err)

				return r.send()
			},
		},
		{
			name: "T2 marked read", want: kindRead,
			setup: func(r *rig) {
				r.rules.change(func(rules *threadrules.Rules) { rules.Action = threadrules.ActionRead })
			},
			act: outsideState(github.StateMerged),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			r := newRig(t)
			r.clearing()
			r.gh.set([]fakeThread{prThread(firstID, 1, start)})

			if tt.setup != nil {
				tt.setup(r)
			}

			r.mustPass()
			require.Empty(t, r.ledger(), "nothing finished yet")

			got := writes(tt.act(t, r))
			require.Len(t, got, 1)
			assert.Equal(t, tt.want, got[0].kind, "GitHub got the write")
			assert.Equal(t, firstID, got[0].id)
			assert.Equal(t, "sent", r.ledger()[0].state)
		})
	}
}

// failingRules are rules that can't be read.
type failingRules struct{}

func (failingRules) ThreadRules(context.Context) (threadrules.Rules, error) {
	return threadrules.Rules{}, errToastUnread
}

func TestASendRunThatCannotReadTheRulesTriesAgainLater(t *testing.T) {
	t.Parallel()

	r := clearingRig(t, prThread(firstID, 1, start))
	r.settle(outcome(OutcomeDismissed, item.KindPR, 1, start))

	r.svc.rules = failingRules{}
	r.clock.Advance(MinHold)

	require.Error(t, r.svc.runPass(t.Context(), passRequest{send: true}))
	assert.Equal(t, FailedRetry, r.untilSend(), "never one send run right after another that failed")
	assertNoRequests(t, r.gh.requests(), "")
}
