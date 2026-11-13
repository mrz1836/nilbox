package threadrules_test

import (
	"maps"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/prefs/prefval"
	"github.com/mrz1836/nilbox/internal/threadrules"
)

// The stored spellings of a switch.
const (
	storedOn  = "true"
	storedOff = "false"
)

// finished is the moment most cases' items were finished.
var finished = time.Date(2026, 3, 4, 10, 0, 0, 0, time.UTC) //nolint:gochecknoglobals // a fixed moment the cases share

// autoRules are the defaults with clearing on its own turned on.
func autoRules() threadrules.Rules {
	r := threadrules.Defaults()
	r.Auto = true

	return r
}

// readRules are autoRules that mark a thread read rather than done.
func readRules() threadrules.Rules {
	r := autoRules()
	r.Action = threadrules.ActionRead

	return r
}

// with returns r changed by change.
func with(r threadrules.Rules, change func(*threadrules.Rules)) threadrules.Rules {
	change(&r)

	return r
}

// unread is an unread thread about an item of kind in state, updated within Grace of
// finished, for a reason other than watching.
func unread(kind item.Kind, state threadrules.State) threadrules.Thread {
	return threadrules.Thread{
		Kind: kind, Unread: true, UpdatedAt: finished.Add(time.Minute), Reason: "review_requested", State: state,
	}
}

// changed returns th changed by change.
func changed(th threadrules.Thread, change func(*threadrules.Thread)) threadrules.Thread {
	change(&th)

	return th
}

// inApp is a moment the user finished the item in the app with trigger.
func inApp(trigger threadrules.Trigger) threadrules.Moment {
	return threadrules.Moment{Trigger: trigger, At: finished}
}

// outside is a moment GitHub's state says the item was finished with trigger.
func outside(trigger threadrules.Trigger) threadrules.Moment {
	return threadrules.Moment{Trigger: trigger, At: finished, Outside: true}
}

// clears is the verdict that clears a thread for trigger.
func clears(trigger threadrules.Trigger) threadrules.Verdict {
	return threadrules.Verdict{Kind: threadrules.VerdictClear, Trigger: trigger}
}

// skips is the verdict that skips a thread for trigger, for reason.
func skips(trigger threadrules.Trigger, reason threadrules.Reason) threadrules.Verdict {
	return threadrules.Verdict{Kind: threadrules.VerdictSkip, Reason: reason, Trigger: trigger}
}

// holds is the verdict that holds a thread for trigger, for its new activity.
func holds(trigger threadrules.Trigger) threadrules.Verdict {
	return threadrules.Verdict{Kind: threadrules.VerdictHold, Reason: threadrules.ReasonNewActivity, Trigger: trigger}
}

// decideCase is one row of Decide's table.
type decideCase struct {
	name   string
	rules  threadrules.Rules
	thread threadrules.Thread
	moment threadrules.Moment
	want   threadrules.Verdict
}

// runDecide checks every case, each in a subtest of its own.
func runDecide(t *testing.T, cases []decideCase) {
	t.Helper()

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tc.want, threadrules.Decide(tc.rules, tc.thread, tc.moment))
		})
	}
}

func TestDecideNeverTouchesAThreadAboutAnythingElse(t *testing.T) {
	t.Parallel()

	release := changed(unread("", threadrules.StateUnknown), func(th *threadrules.Thread) { th.Unread = false })

	runDecide(t, []decideCase{
		{
			name:  "not even by hand",
			rules: autoRules(), thread: unread("", threadrules.StateUnknown), moment: inApp(threadrules.TriggerByHand),
			want: skips(threadrules.TriggerByHand, threadrules.ReasonNotItem),
		},
		{
			name:  "nor by a trigger",
			rules: autoRules(), thread: unread("", threadrules.StateMerged), moment: outside(threadrules.TriggerMerged),
			want: skips(threadrules.TriggerMerged, threadrules.ReasonNotItem),
		},
		{
			name:  "a kind the app doesn't know",
			rules: autoRules(), thread: unread("discussion", threadrules.StateClosed), moment: inApp(threadrules.TriggerDismissed),
			want: skips(threadrules.TriggerDismissed, threadrules.ReasonNotItem),
		},
		{
			name:  "before it is weighed as already read",
			rules: readRules(), thread: release, moment: inApp(threadrules.TriggerByHand),
			want: skips(threadrules.TriggerByHand, threadrules.ReasonNotItem),
		},
		{
			name:  "before clearing on its own is weighed",
			rules: threadrules.Defaults(), thread: release, moment: inApp(threadrules.TriggerMerged),
			want: skips(threadrules.TriggerMerged, threadrules.ReasonNotItem),
		},
	})
}

func TestDecideInReadModeSkipsAThreadAlreadyRead(t *testing.T) {
	t.Parallel()

	read := func(kind item.Kind, state threadrules.State) threadrules.Thread {
		return changed(unread(kind, state), func(th *threadrules.Thread) { th.Unread = false })
	}

	runDecide(t, []decideCase{
		{
			name:  "by hand",
			rules: readRules(), thread: read(item.KindPR, threadrules.StateOpen), moment: inApp(threadrules.TriggerByHand),
			want: skips(threadrules.TriggerByHand, threadrules.ReasonAlreadyRead),
		},
		{
			name:  "by a trigger that would clear it",
			rules: readRules(), thread: read(item.KindPR, threadrules.StateMerged), moment: outside(threadrules.TriggerMerged),
			want: skips(threadrules.TriggerMerged, threadrules.ReasonAlreadyRead),
		},
		{
			name:   "with clearing on its own off",
			rules:  with(readRules(), func(r *threadrules.Rules) { r.Auto = false }),
			thread: read(item.KindIssue, threadrules.StateClosed), moment: outside(threadrules.TriggerIssueClosed),
			want: skips(threadrules.TriggerIssueClosed, threadrules.ReasonAlreadyRead),
		},
		{
			name:  "done mode clears it by hand",
			rules: autoRules(), thread: read(item.KindPR, threadrules.StateOpen), moment: inApp(threadrules.TriggerByHand),
			want: clears(threadrules.TriggerByHand),
		},
		{
			name:  "done mode clears it by a trigger",
			rules: autoRules(), thread: read(item.KindPR, threadrules.StateMerged), moment: outside(threadrules.TriggerMerged),
			want: clears(threadrules.TriggerMerged),
		},
		{
			name:   "an unknown action is done",
			rules:  with(autoRules(), func(r *threadrules.Rules) { r.Action = "archive" }),
			thread: read(item.KindPR, threadrules.StateMerged), moment: outside(threadrules.TriggerMerged),
			want: clears(threadrules.TriggerMerged),
		},
		{
			name:  "read mode clears an unread one",
			rules: readRules(), thread: unread(item.KindPR, threadrules.StateMerged), moment: outside(threadrules.TriggerMerged),
			want: clears(threadrules.TriggerMerged),
		},
	})
}

func TestDecideClearsByHandWithoutAGuard(t *testing.T) {
	t.Parallel()

	allOff := with(threadrules.Defaults(), func(r *threadrules.Rules) {
		r.TriggersOff = threadrules.Triggers()
		r.Watching = false
		r.BotActivity = false
	})
	busy := changed(unread(item.KindIssue, threadrules.StateOpen), func(th *threadrules.Thread) {
		th.UpdatedAt = finished.Add(time.Hour)
		th.Reason = threadrules.WatchingReason
		th.AfterMoment = threadrules.AfterPerson
	})

	runDecide(t, []decideCase{
		{
			name:  "with clearing on its own off",
			rules: threadrules.Defaults(), thread: unread(item.KindPR, threadrules.StateOpen),
			moment: inApp(threadrules.TriggerByHand), want: clears(threadrules.TriggerByHand),
		},
		{
			name:  "with every rule against it",
			rules: allOff, thread: busy, moment: outside(threadrules.TriggerByHand),
			want: clears(threadrules.TriggerByHand),
		},
		{
			name:  "in read mode, an unread one with new activity",
			rules: readRules(), thread: busy, moment: inApp(threadrules.TriggerByHand),
			want: clears(threadrules.TriggerByHand),
		},
		{
			name:  "with no state known",
			rules: autoRules(), thread: unread(item.KindPR, threadrules.StateUnknown),
			moment: inApp(threadrules.TriggerByHand), want: clears(threadrules.TriggerByHand),
		},
	})
}

func TestDecideLeavesEveryThreadAloneWhileReadingIsOff(t *testing.T) {
	t.Parallel()

	triggers := append(threadrules.Triggers(), threadrules.TriggerByHand)
	modes := map[string]threadrules.Rules{"done": autoRules(), "read": readRules()}
	cases := make([]decideCase, 0, len(modes)*len(triggers)*2)

	for mode, rules := range modes {
		off := with(rules, func(r *threadrules.Rules) { r.Read = false })

		for _, trigger := range triggers {
			for _, m := range []threadrules.Moment{inApp(trigger), outside(trigger)} {
				cases = append(cases, decideCase{
					name:  mode + " " + string(trigger) + " outside " + boolWord(m.Outside),
					rules: off, thread: finishedBy(trigger), moment: m, want: skips(trigger, threadrules.ReasonOff),
				})
			}
		}
	}

	runDecide(t, cases)
}

//nolint:paralleltest // testing.AllocsPerRun counts every goroutine's allocations, so it runs alone
func TestDecideAllocatesNothing(t *testing.T) {
	rules := with(autoRules(), func(r *threadrules.Rules) {
		r.TriggersOff = []threadrules.Trigger{threadrules.TriggerReviewed, threadrules.TriggerDismissed}
	})
	thread := changed(unread(item.KindPR, threadrules.StateMerged), func(th *threadrules.Thread) {
		th.Reason = threadrules.WatchingReason
	})
	moment := outside(threadrules.TriggerMerged)

	require.Equal(t, clears(threadrules.TriggerMerged), threadrules.Decide(rules, thread, moment),
		"the case runs every step")

	allocs := testing.AllocsPerRun(100, func() {
		_ = threadrules.Decide(rules, thread, moment)
		_ = rules.On(threadrules.TriggerUnassigned)
	})
	assert.Zero(t, allocs, "deciding a thread allocates nothing")
}

func TestDecideSkipsEveryTriggerWhileClearingOnItsOwnIsOff(t *testing.T) {
	t.Parallel()

	cases := make([]decideCase, 0, len(threadrules.Triggers()))

	for _, trigger := range threadrules.Triggers() {
		cases = append(cases, decideCase{
			name: string(trigger), rules: threadrules.Defaults(), thread: finishedBy(trigger), moment: inApp(trigger),
			want: skips(trigger, threadrules.ReasonOff),
		})
	}

	runDecide(t, cases)
}

// finishedBy is an unread thread whose item trigger finished: the kind it applies to (a
// pull request where both do), in the state that bears it out.
func finishedBy(trigger threadrules.Trigger) threadrules.Thread {
	switch trigger {
	case threadrules.TriggerMerged:
		return unread(item.KindPR, threadrules.StateMerged)
	case threadrules.TriggerPRClosed:
		return unread(item.KindPR, threadrules.StateClosed)
	case threadrules.TriggerIssueClosed:
		return unread(item.KindIssue, threadrules.StateClosed)
	case threadrules.TriggerReviewed, threadrules.TriggerDismissed, threadrules.TriggerUnassigned,
		threadrules.TriggerByHand:
		return unread(item.KindPR, threadrules.StateOpen)
	}

	return unread(item.KindPR, threadrules.StateUnknown)
}

func TestDecideEachTriggerOnEachKind(t *testing.T) {
	t.Parallel()

	ruleOff := threadrules.ReasonRuleOff

	rows := []struct {
		trigger threadrules.Trigger
		kind    item.Kind
		state   threadrules.State
		reason  threadrules.Reason // "" clears
	}{
		{trigger: threadrules.TriggerMerged, kind: item.KindPR, state: threadrules.StateMerged},
		{trigger: threadrules.TriggerMerged, kind: item.KindIssue, state: threadrules.StateMerged, reason: ruleOff},
		{trigger: threadrules.TriggerMerged, kind: item.KindIssue, state: threadrules.StateClosed, reason: ruleOff},
		{trigger: threadrules.TriggerPRClosed, kind: item.KindPR, state: threadrules.StateClosed},
		{trigger: threadrules.TriggerPRClosed, kind: item.KindIssue, state: threadrules.StateClosed, reason: ruleOff},
		{trigger: threadrules.TriggerIssueClosed, kind: item.KindIssue, state: threadrules.StateClosed},
		{trigger: threadrules.TriggerIssueClosed, kind: item.KindPR, state: threadrules.StateClosed, reason: ruleOff},
		{trigger: threadrules.TriggerReviewed, kind: item.KindPR, state: threadrules.StateOpen},
		{trigger: threadrules.TriggerReviewed, kind: item.KindIssue, state: threadrules.StateOpen, reason: ruleOff},
		{trigger: threadrules.TriggerDismissed, kind: item.KindPR, state: threadrules.StateOpen},
		{trigger: threadrules.TriggerDismissed, kind: item.KindIssue, state: threadrules.StateOpen},
		{trigger: threadrules.TriggerUnassigned, kind: item.KindPR, state: threadrules.StateOpen},
		{trigger: threadrules.TriggerUnassigned, kind: item.KindIssue, state: threadrules.StateOpen},
		{trigger: threadrules.TriggerByHand, kind: item.KindPR, state: threadrules.StateOpen},
		{trigger: threadrules.TriggerByHand, kind: item.KindIssue, state: threadrules.StateOpen},
	}

	modes := map[string]threadrules.Rules{"done": autoRules(), "read": readRules()}
	cases := make([]decideCase, 0, len(rows)*len(modes))

	for mode, rules := range modes {
		for _, row := range rows {
			want := clears(row.trigger)
			if row.reason != "" {
				want = skips(row.trigger, row.reason)
			}

			cases = append(cases, decideCase{
				name:  mode + " " + string(row.trigger) + " on an " + string(row.kind) + " " + string(row.state),
				rules: rules, thread: unread(row.kind, row.state), moment: inApp(row.trigger), want: want,
			})
		}
	}

	runDecide(t, cases)
}

func TestDecideSkipsATriggerTurnedOff(t *testing.T) {
	t.Parallel()

	cases := make([]decideCase, 0, 2*len(threadrules.Triggers())*len(threadrules.Triggers()))

	for _, off := range threadrules.Triggers() {
		rules := with(autoRules(), func(r *threadrules.Rules) { r.TriggersOff = []threadrules.Trigger{off} })

		for _, trigger := range threadrules.Triggers() {
			want := clears(trigger)
			if trigger == off {
				want = skips(trigger, threadrules.ReasonRuleOff)
			}

			for _, m := range []threadrules.Moment{inApp(trigger), outside(trigger)} {
				cases = append(cases, decideCase{
					name:  string(off) + " off, " + string(trigger) + " outside " + boolWord(m.Outside),
					rules: rules, thread: finishedBy(trigger), moment: m, want: want,
				})
			}
		}
	}

	runDecide(t, cases)
}

// boolWord is "yes" or "no", for a case's name.
func boolWord(b bool) string {
	if b {
		return "yes"
	}

	return "no"
}

func TestDecideSkipsATriggerItDoesNotKnow(t *testing.T) {
	t.Parallel()

	runDecide(t, []decideCase{
		{
			name:  "a later trigger",
			rules: autoRules(), thread: unread(item.KindPR, threadrules.StateMerged), moment: outside("commented"),
			want: skips("commented", threadrules.ReasonRuleOff),
		},
		{
			name:  "no trigger",
			rules: autoRules(), thread: unread(item.KindIssue, threadrules.StateClosed), moment: inApp(""),
			want: skips("", threadrules.ReasonRuleOff),
		},
	})
}

func TestDecideWatchingGatesOnlyTheOutsideMoments(t *testing.T) {
	t.Parallel()

	watched := func(trigger threadrules.Trigger) threadrules.Thread {
		return changed(finishedBy(trigger), func(th *threadrules.Thread) { th.Reason = threadrules.WatchingReason })
	}
	notWatching := with(autoRules(), func(r *threadrules.Rules) { r.Watching = false })

	cases := make([]decideCase, 0, 4*len(threadrules.Triggers()))

	for _, trigger := range threadrules.Triggers() {
		cases = append(cases,
			decideCase{
				name:  string(trigger) + " from GitHub, watching off",
				rules: notWatching, thread: watched(trigger), moment: outside(trigger),
				want: skips(trigger, threadrules.ReasonRuleOff),
			},
			decideCase{
				name:  string(trigger) + " from GitHub, watching on",
				rules: autoRules(), thread: watched(trigger), moment: outside(trigger), want: clears(trigger),
			},
			decideCase{
				name:  string(trigger) + " in the app, watching off",
				rules: notWatching, thread: watched(trigger), moment: inApp(trigger), want: clears(trigger),
			},
			decideCase{
				name:  string(trigger) + " from GitHub, another reason, watching off",
				rules: notWatching, thread: finishedBy(trigger), moment: outside(trigger), want: clears(trigger),
			},
		)
	}

	runDecide(t, cases)
}

func TestDecideSkipsAStateThatDoesNotBearTheTriggerOut(t *testing.T) {
	t.Parallel()

	stillOpen := threadrules.ReasonStillOpen
	cases := []decideCase{}

	for _, row := range []struct {
		trigger threadrules.Trigger
		kind    item.Kind
		state   threadrules.State
	}{
		{trigger: threadrules.TriggerMerged, kind: item.KindPR, state: threadrules.StateOpen},
		{trigger: threadrules.TriggerMerged, kind: item.KindPR, state: threadrules.StateClosed},
		{trigger: threadrules.TriggerMerged, kind: item.KindPR, state: threadrules.StateUnknown},
		{trigger: threadrules.TriggerPRClosed, kind: item.KindPR, state: threadrules.StateMerged},
		{trigger: threadrules.TriggerPRClosed, kind: item.KindPR, state: threadrules.StateOpen},
		{trigger: threadrules.TriggerPRClosed, kind: item.KindPR, state: threadrules.StateUnknown},
		{trigger: threadrules.TriggerIssueClosed, kind: item.KindIssue, state: threadrules.StateOpen},
		{trigger: threadrules.TriggerIssueClosed, kind: item.KindIssue, state: threadrules.StateUnknown},
		{trigger: threadrules.TriggerIssueClosed, kind: item.KindIssue, state: "LOCKED"},
	} {
		for _, m := range []threadrules.Moment{inApp(row.trigger), outside(row.trigger)} {
			cases = append(cases, decideCase{
				name:  string(row.trigger) + " " + string(row.state) + " outside " + boolWord(m.Outside),
				rules: autoRules(), thread: unread(row.kind, row.state), moment: m, want: skips(row.trigger, stillOpen),
			})
		}
	}

	for _, trigger := range []threadrules.Trigger{
		threadrules.TriggerReviewed, threadrules.TriggerDismissed, threadrules.TriggerUnassigned,
	} {
		for _, state := range []threadrules.State{
			threadrules.StateUnknown, threadrules.StateOpen, threadrules.StateClosed, threadrules.StateMerged,
		} {
			cases = append(cases, decideCase{
				name:  string(trigger) + " needs no state, " + string(state),
				rules: autoRules(), thread: unread(item.KindPR, state), moment: inApp(trigger), want: clears(trigger),
			})
		}
	}

	runDecide(t, cases)
}

func TestDecideHoldsAThreadWithNewActivity(t *testing.T) {
	t.Parallel()

	merged := threadrules.TriggerMerged
	updated := func(at time.Time, after threadrules.AfterMoment) threadrules.Thread {
		return changed(unread(item.KindPR, threadrules.StateMerged), func(th *threadrules.Thread) {
			th.UpdatedAt = at
			th.AfterMoment = after
		})
	}
	noBots := with(autoRules(), func(r *threadrules.Rules) { r.BotActivity = false })
	edge := finished.Add(threadrules.Grace)
	late := finished.Add(time.Hour)

	runDecide(t, []decideCase{
		{
			name:  "updated exactly at the end of the grace",
			rules: autoRules(), thread: updated(edge, ""), moment: outside(merged), want: clears(merged),
		},
		{
			name:  "updated a nanosecond past the grace",
			rules: autoRules(), thread: updated(edge.Add(time.Nanosecond), ""), moment: outside(merged), want: holds(merged),
		},
		{
			name:  "updated before the moment",
			rules: autoRules(), thread: updated(finished.Add(-time.Hour), ""), moment: outside(merged), want: clears(merged),
		},
		{
			name:  "updated at the moment",
			rules: autoRules(), thread: updated(finished, ""), moment: inApp(merged), want: clears(merged),
		},
		{
			name:  "read, then updated later",
			rules: autoRules(), moment: outside(merged), want: clears(merged),
			thread: changed(updated(late, threadrules.AfterPerson), func(th *threadrules.Thread) { th.Unread = false }),
		},
		{
			name:  "a bot after, and bots let pass",
			rules: autoRules(), thread: updated(late, threadrules.AfterBot), moment: outside(merged), want: clears(merged),
		},
		{
			name:  "a bot after, and bots count",
			rules: noBots, thread: updated(late, threadrules.AfterBot), moment: outside(merged), want: holds(merged),
		},
		{
			name:  "a person after",
			rules: autoRules(), thread: updated(late, threadrules.AfterPerson), moment: outside(merged), want: holds(merged),
		},
		{
			name:  "no one known after",
			rules: autoRules(), thread: updated(late, threadrules.AfterUnknown), moment: outside(merged), want: holds(merged),
		},
		{
			name:  "read mode, within the grace",
			rules: readRules(), thread: updated(edge, ""), moment: outside(merged), want: clears(merged),
		},
		{
			name:  "read mode, past the grace",
			rules: readRules(), thread: updated(edge.Add(time.Nanosecond), ""), moment: outside(merged), want: holds(merged),
		},
		{
			name:  "read mode, a bot after",
			rules: readRules(), thread: updated(late, threadrules.AfterBot), moment: outside(merged), want: clears(merged),
		},
		{
			name:  "a review in the app, updated past the grace",
			rules: autoRules(), moment: inApp(threadrules.TriggerReviewed), want: holds(threadrules.TriggerReviewed),
			thread: changed(unread(item.KindPR, threadrules.StateOpen), func(th *threadrules.Thread) { th.UpdatedAt = late }),
		},
		{
			name:  "unread, updated at a time not known",
			rules: autoRules(), thread: updated(time.Time{}, ""), moment: outside(merged), want: holds(merged),
		},
		{
			name:  "unread, updated at a time not known, a person after",
			rules: autoRules(), thread: updated(time.Time{}, threadrules.AfterPerson), moment: outside(merged),
			want: holds(merged),
		},
		{
			name:  "unread, updated at a time not known, a bot after, and bots let pass",
			rules: autoRules(), thread: updated(time.Time{}, threadrules.AfterBot), moment: outside(merged),
			want: clears(merged),
		},
		{
			name:  "unread, updated at a time not known, a bot after, and bots count",
			rules: noBots, thread: updated(time.Time{}, threadrules.AfterBot), moment: outside(merged), want: holds(merged),
		},
		{
			name:  "unread, updated at a time not known, in read mode",
			rules: readRules(), thread: updated(time.Time{}, ""), moment: inApp(merged), want: holds(merged),
		},
		{
			name:  "read, updated at a time not known",
			rules: autoRules(), moment: outside(merged), want: clears(merged),
			thread: changed(updated(time.Time{}, ""), func(th *threadrules.Thread) { th.Unread = false }),
		},
		{
			name:  "unread, a moment at a time not known",
			rules: autoRules(), thread: updated(finished, ""), moment: threadrules.Moment{Trigger: merged},
			want: holds(merged),
		},
	})
}

func TestDecideSkipsAReopenedItem(t *testing.T) {
	t.Parallel()

	runDecide(t, []decideCase{
		{
			name:  "an issue closed, then reopened",
			rules: autoRules(), thread: unread(item.KindIssue, threadrules.StateOpen),
			moment: outside(threadrules.TriggerIssueClosed),
			want:   skips(threadrules.TriggerIssueClosed, threadrules.ReasonStillOpen),
		},
		{
			name:  "a pull request closed, then reopened",
			rules: autoRules(), thread: unread(item.KindPR, threadrules.StateOpen),
			moment: inApp(threadrules.TriggerPRClosed),
			want:   skips(threadrules.TriggerPRClosed, threadrules.ReasonStillOpen),
		},
	})
}

func TestDecideOrder(t *testing.T) {
	t.Parallel()

	// Every rule below applies at once; each case takes away the one before it, so the
	// next one wins.
	everything := threadrules.Thread{
		Kind: "", Unread: false, UpdatedAt: finished.Add(time.Hour), Reason: threadrules.WatchingReason,
		State: threadrules.StateOpen, AfterMoment: threadrules.AfterPerson,
	}
	rules := with(readRules(), func(r *threadrules.Rules) {
		r.Read = false
		r.Auto = false
		r.Watching = false
	})
	merged := outside(threadrules.TriggerMerged)

	pr := changed(everything, func(th *threadrules.Thread) { th.Kind = item.KindPR })
	reading := with(rules, func(r *threadrules.Rules) { r.Read = true })
	isUnread := changed(pr, func(th *threadrules.Thread) { th.Unread = true })
	isMerged := changed(isUnread, func(th *threadrules.Thread) { th.State = threadrules.StateMerged })
	notWatched := changed(isMerged, func(th *threadrules.Thread) { th.Reason = "author" })
	auto := with(reading, func(r *threadrules.Rules) { r.Auto = true })

	runDecide(t, []decideCase{
		{
			name: "1 not an item", rules: rules, thread: everything, moment: merged,
			want: skips(merged.Trigger, threadrules.ReasonNotItem),
		},
		{
			name: "2 reading off", rules: rules, thread: pr, moment: merged,
			want: skips(merged.Trigger, threadrules.ReasonOff),
		},
		{
			name: "3 already read", rules: reading, thread: pr, moment: merged,
			want: skips(merged.Trigger, threadrules.ReasonAlreadyRead),
		},
		{
			name: "4 by hand", rules: reading, thread: isUnread, moment: outside(threadrules.TriggerByHand),
			want: clears(threadrules.TriggerByHand),
		},
		{
			name: "5 off", rules: reading, thread: isUnread, moment: merged,
			want: skips(merged.Trigger, threadrules.ReasonOff),
		},
		{
			name: "6 rule off", rules: auto, thread: isUnread, moment: merged,
			want: skips(merged.Trigger, threadrules.ReasonRuleOff),
		},
		{
			name: "7 still open", rules: auto, thread: changed(isUnread, func(th *threadrules.Thread) { th.Reason = "author" }),
			moment: merged, want: skips(merged.Trigger, threadrules.ReasonStillOpen),
		},
		{name: "8 new activity", rules: auto, thread: notWatched, moment: merged, want: holds(merged.Trigger)},
		{
			name: "9 clear", rules: auto, moment: merged, want: clears(merged.Trigger),
			thread: changed(notWatched, func(th *threadrules.Thread) { th.UpdatedAt = finished }),
		},
	})
}

func TestTriggers(t *testing.T) {
	t.Parallel()

	assert.Equal(t, []threadrules.Trigger{
		"merged", "pr_closed", "issue_closed", "reviewed", "dismissed", "unassigned",
	}, threadrules.Triggers())
	assert.Equal(t, threadrules.TriggerByHand, threadrules.Trigger("by_hand"))

	for _, trigger := range threadrules.Triggers() {
		assert.True(t, trigger.Valid(), trigger)
		assert.True(t, trigger.HasSwitch(), trigger)
	}

	assert.True(t, threadrules.TriggerByHand.Valid())
	assert.False(t, threadrules.TriggerByHand.HasSwitch(), "clearing by hand has no switch")

	for _, unknown := range []threadrules.Trigger{"", "commented", "Merged", "by-hand"} {
		assert.False(t, unknown.Valid(), unknown)
		assert.False(t, unknown.HasSwitch(), unknown)
		assert.False(t, unknown.AppliesTo(item.KindPR), unknown)
		assert.False(t, unknown.MatchesState(threadrules.StateMerged), unknown)
	}
}

func TestAppliesTo(t *testing.T) {
	t.Parallel()

	for trigger, kinds := range map[threadrules.Trigger][]item.Kind{
		threadrules.TriggerMerged:      {item.KindPR},
		threadrules.TriggerPRClosed:    {item.KindPR},
		threadrules.TriggerIssueClosed: {item.KindIssue},
		threadrules.TriggerReviewed:    {item.KindPR},
		threadrules.TriggerDismissed:   {item.KindPR, item.KindIssue},
		threadrules.TriggerUnassigned:  {item.KindPR, item.KindIssue},
		threadrules.TriggerByHand:      {item.KindPR, item.KindIssue},
	} {
		for _, kind := range item.Kinds() {
			assert.Equal(t, slices.Contains(kinds, kind), trigger.AppliesTo(kind), "%s on %s", trigger, kind)
		}

		assert.False(t, trigger.AppliesTo(""), trigger)
		assert.False(t, trigger.AppliesTo("discussion"), trigger)
	}
}

func TestMatchesState(t *testing.T) {
	t.Parallel()

	states := []threadrules.State{
		threadrules.StateUnknown, threadrules.StateOpen, threadrules.StateClosed, threadrules.StateMerged,
	}

	for trigger, want := range map[threadrules.Trigger][]threadrules.State{
		threadrules.TriggerMerged:      {threadrules.StateMerged},
		threadrules.TriggerPRClosed:    {threadrules.StateClosed},
		threadrules.TriggerIssueClosed: {threadrules.StateClosed},
		threadrules.TriggerReviewed:    states,
		threadrules.TriggerDismissed:   states,
		threadrules.TriggerUnassigned:  states,
		threadrules.TriggerByHand:      states,
	} {
		for _, state := range states {
			assert.Equal(t, slices.Contains(want, state), trigger.MatchesState(state), "%s in %q", trigger, state)
		}
	}
}

func TestOn(t *testing.T) {
	t.Parallel()

	r := with(threadrules.Defaults(), func(r *threadrules.Rules) {
		r.TriggersOff = []threadrules.Trigger{threadrules.TriggerReviewed, threadrules.TriggerByHand}
	})

	for _, trigger := range threadrules.Triggers() {
		assert.Equal(t, trigger != threadrules.TriggerReviewed, r.On(trigger), trigger)
	}

	assert.True(t, r.On(threadrules.TriggerByHand), "clearing by hand is always on, even listed")
	assert.False(t, r.On("commented"), "an unknown trigger is never on")
	assert.False(t, r.On(""))
}

func TestActions(t *testing.T) {
	t.Parallel()

	assert.Equal(t, []threadrules.Action{"done", "read"}, threadrules.Actions())
	assert.True(t, threadrules.ActionDone.Valid())
	assert.True(t, threadrules.ActionRead.Valid())
	assert.False(t, threadrules.Action("").Valid())
	assert.False(t, threadrules.Action("archive").Valid())

	assert.Equal(t, threadrules.ActionRead, threadrules.DecodeAction("read"))

	for _, stored := range []string{"", "done", "Read", "archive", " read"} {
		assert.Equal(t, threadrules.ActionDone, threadrules.DecodeAction(stored), stored)
	}

	for _, action := range threadrules.Actions() {
		stored, err := threadrules.EncodeAction(action)
		require.NoError(t, err)
		assert.Equal(t, string(action), stored)
		assert.Equal(t, action, threadrules.DecodeAction(stored))
	}

	for _, bad := range []threadrules.Action{"", "archive", "READ"} {
		_, err := threadrules.EncodeAction(bad)
		require.ErrorIs(t, err, threadrules.ErrUnknownAction, bad)
	}
}

func TestSwitches(t *testing.T) {
	t.Parallel()

	assert.Equal(t, []threadrules.Switch{"read", "auto", "watching", "botActivity"}, threadrules.Switches())

	for s, key := range map[threadrules.Switch]string{
		threadrules.SwitchRead:        "threadrules.read",
		threadrules.SwitchAuto:        "threadrules.auto",
		threadrules.SwitchWatching:    "threadrules.watching",
		threadrules.SwitchBotActivity: "threadrules.botActivity",
	} {
		assert.True(t, s.Valid(), s)
		assert.Equal(t, key, s.Key(), s)
	}

	for _, unknown := range []threadrules.Switch{"", "action", "cleanupCardDismissed", "Read"} {
		assert.False(t, unknown.Valid(), unknown)
		assert.Empty(t, unknown.Key(), unknown)
	}
}

func TestKeys(t *testing.T) {
	t.Parallel()

	assert.Equal(t, []string{
		"threadrules.read", "threadrules.auto", "threadrules.watching", "threadrules.botActivity",
		"threadrules.action", "threadrules.triggersOff", "threadrules.cleanupCardDismissed",
	}, threadrules.Keys())
}

func TestDefaults(t *testing.T) {
	t.Parallel()

	want := threadrules.Rules{
		Read: true, Auto: false, Action: threadrules.ActionDone, TriggersOff: []threadrules.Trigger{},
		Watching: true, BotActivity: true, CleanupCardDismissed: false,
	}

	assert.Equal(t, want, threadrules.Defaults())
	assert.Equal(t, want, threadrules.Decode(map[string]string{}))

	for _, trigger := range threadrules.Triggers() {
		assert.True(t, threadrules.Defaults().On(trigger), "%s starts on", trigger)
	}
}

func TestDecodeFallsBackOnBadValues(t *testing.T) {
	t.Parallel()

	got := threadrules.Decode(map[string]string{
		threadrules.KeyRead:                 "yes",
		threadrules.KeyAuto:                 "on",
		threadrules.KeyAction:               "archive",
		threadrules.KeyTriggersOff:          `{"merged":true}`,
		threadrules.KeyWatching:             "",
		threadrules.KeyBotActivity:          "maybe",
		threadrules.KeyCleanupCardDismissed: "2",
	})

	assert.Equal(t, threadrules.Defaults(), got)
}

func TestDecodeReadsTheStoredRules(t *testing.T) {
	t.Parallel()

	got := threadrules.Decode(map[string]string{
		threadrules.KeyRead:                 storedOff,
		threadrules.KeyAuto:                 storedOn,
		threadrules.KeyAction:               "read",
		threadrules.KeyTriggersOff:          `["unassigned","merged"]`,
		threadrules.KeyWatching:             storedOff,
		threadrules.KeyBotActivity:          "0",
		threadrules.KeyCleanupCardDismissed: storedOn,
	})

	assert.Equal(t, threadrules.Rules{
		Read: false, Auto: true, Action: threadrules.ActionRead,
		TriggersOff: []threadrules.Trigger{threadrules.TriggerMerged, threadrules.TriggerUnassigned},
		Watching:    false, BotActivity: false, CleanupCardDismissed: true,
	}, got, "the triggers in the order of Triggers")
}

func TestDecodeTriggersOff(t *testing.T) {
	t.Parallel()

	for stored, want := range map[string][]threadrules.Trigger{
		``:                          {},
		`[]`:                        {},
		`null`:                      {},
		`["merged"`:                 {},
		`"merged"`:                  {},
		`["merged",7]`:              {},
		`["by_hand"]`:               {},
		`["commented","","Merged"]`: {},
		`["reviewed","by_hand","reviewed","pr_closed","later"]`: {
			threadrules.TriggerPRClosed, threadrules.TriggerReviewed,
		},
	} {
		got := threadrules.DecodeTriggersOff(stored)
		require.NotNil(t, got, stored)
		assert.Equal(t, want, got, stored)
	}
}

func TestEncodeTriggersOff(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "[]", threadrules.EncodeTriggersOff(nil), "none is stored as an empty list, never null")
	assert.Equal(t, `["merged","dismissed"]`, threadrules.EncodeTriggersOff([]threadrules.Trigger{
		threadrules.TriggerDismissed, threadrules.TriggerByHand, "later", threadrules.TriggerMerged,
		threadrules.TriggerDismissed,
	}), "in order, each once, only the triggers with a switch")
}

// storedAs writes r as the settings store it, one key at a time: each switch with
// prefval.EncodeBool, the action with EncodeAction, and the triggers that are off with
// EncodeTriggersOff.
func storedAs(t *testing.T, r threadrules.Rules) map[string]string {
	t.Helper()

	action, err := threadrules.EncodeAction(r.Action)
	require.NoError(t, err)

	return map[string]string{
		threadrules.KeyRead:                 prefval.EncodeBool(r.Read),
		threadrules.KeyAuto:                 prefval.EncodeBool(r.Auto),
		threadrules.KeyAction:               action,
		threadrules.KeyTriggersOff:          threadrules.EncodeTriggersOff(r.TriggersOff),
		threadrules.KeyWatching:             prefval.EncodeBool(r.Watching),
		threadrules.KeyBotActivity:          prefval.EncodeBool(r.BotActivity),
		threadrules.KeyCleanupCardDismissed: prefval.EncodeBool(r.CleanupCardDismissed),
	}
}

func TestTheStoredRulesReadBackAsThemselves(t *testing.T) {
	t.Parallel()

	for name, rules := range map[string]threadrules.Rules{
		"defaults": threadrules.Defaults(),
		"everything changed": {
			Read: false, Auto: true, Action: threadrules.ActionRead, TriggersOff: threadrules.Triggers(),
			Watching: false, BotActivity: false, CleanupCardDismissed: true,
		},
		"some triggers off": with(autoRules(), func(r *threadrules.Rules) {
			r.TriggersOff = []threadrules.Trigger{threadrules.TriggerIssueClosed, threadrules.TriggerDismissed}
		}),
	} {
		stored := storedAs(t, rules)
		assert.ElementsMatch(t, threadrules.Keys(), slices.Collect(maps.Keys(stored)), "%s: every key", name)
		assert.Equal(t, rules, threadrules.Decode(stored), name)
	}

	assert.Equal(t, map[string]string{
		threadrules.KeyRead: storedOn, threadrules.KeyAuto: storedOff, threadrules.KeyAction: "done",
		threadrules.KeyTriggersOff: "[]", threadrules.KeyWatching: storedOn, threadrules.KeyBotActivity: storedOn,
		threadrules.KeyCleanupCardDismissed: storedOff,
	}, storedAs(t, threadrules.Defaults()))
}

func TestSetTrigger(t *testing.T) {
	t.Parallel()

	off, err := threadrules.SetTrigger(nil, threadrules.TriggerUnassigned, false)
	require.NoError(t, err)
	assert.Equal(t, []threadrules.Trigger{threadrules.TriggerUnassigned}, off)

	off, err = threadrules.SetTrigger(off, threadrules.TriggerMerged, false)
	require.NoError(t, err)
	assert.Equal(t, []threadrules.Trigger{threadrules.TriggerMerged, threadrules.TriggerUnassigned}, off,
		"in the order of Triggers")

	off, err = threadrules.SetTrigger(off, threadrules.TriggerMerged, false)
	require.NoError(t, err)
	assert.Equal(t, []threadrules.Trigger{threadrules.TriggerMerged, threadrules.TriggerUnassigned}, off,
		"turning one off twice lists it once")

	off, err = threadrules.SetTrigger(off, threadrules.TriggerUnassigned, true)
	require.NoError(t, err)
	assert.Equal(t, []threadrules.Trigger{threadrules.TriggerMerged}, off)

	off, err = threadrules.SetTrigger([]threadrules.Trigger{"later", threadrules.TriggerByHand}, threadrules.TriggerReviewed,
		true)
	require.NoError(t, err)
	assert.Equal(t, []threadrules.Trigger{}, off, "never nil, and only the triggers with a switch")

	_, err = threadrules.SetTrigger(nil, threadrules.TriggerByHand, false)
	require.ErrorIs(t, err, threadrules.ErrNoSwitch)

	for _, unknown := range []threadrules.Trigger{"", "commented"} {
		_, err = threadrules.SetTrigger(nil, unknown, false)
		require.ErrorIs(t, err, threadrules.ErrUnknownTrigger, unknown)
	}
}
