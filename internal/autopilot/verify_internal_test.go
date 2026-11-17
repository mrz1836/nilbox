package autopilot

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/mrz1836/nilbox/internal/autopilot/catalog"
	"github.com/mrz1836/nilbox/internal/autopilot/facts"
	"github.com/mrz1836/nilbox/internal/autopilot/spec"
)

// The commits of the verdict matrix: the head a run started from, the commit it pushed,
// and another's push.
const (
	headBefore = "aaaaaaa000000000000000000000000000000000"
	runCommit  = "bbbbbbb000000000000000000000000000000000"
	theirs     = "ccccccc000000000000000000000000000000000"
)

// verifyNow is the moment the matrix judges at.
func verifyNow() time.Time { return time.Date(2026, time.November, 16, 12, 0, 0, 0, time.UTC) }

// checksAt are an item's facts with its head at head and its checks counted, done (their
// last finished) at done, zero while their names aren't read, and read at the moment.
type checksAt struct {
	head                     string
	failed, pending, passed  int
	awaiting                 bool
	done                     time.Time
	failedNames              []string
	detailsAt                time.Time
	namesUnread, unreadCount bool
}

// facts builds them.
func (c checksAt) facts() facts.Facts {
	f := facts.Facts{
		Head: facts.Text(c.head), Values: map[catalog.FieldID]facts.Value{}, DetailsAt: c.detailsAt,
		ChecksDone: facts.Since(c.done),
	}

	if c.detailsAt.IsZero() {
		f.DetailsAt = verifyNow()
	}

	if c.namesUnread || c.done.IsZero() {
		f.ChecksDone = facts.Unknown("the check names aren't read yet", facts.NeedChecks)
	}

	if !c.unreadCount {
		f.Values[catalog.FieldChecksFailed] = facts.Number(c.failed)
		f.Values[catalog.FieldChecksPending] = facts.Number(c.pending)
		f.Values[catalog.FieldChecksPassed] = facts.Number(c.passed)
	}

	f.Values[catalog.FieldAwaitingApproval] = facts.Bool(c.awaiting)
	f.Values[catalog.FieldFailedNames] = facts.List(c.failedNames, false)

	return f
}

// pushed is a run that pushed runCommit, ended a minute before the matrix judges.
func pushed() verifyRun {
	return verifyRun{Commit: runCommit, HeadBefore: headBefore, HeadAfter: runCommit, EndedAt: verifyNow().Add(-time.Minute)}
}

func TestTheVerdictMatrix(t *testing.T) {
	t.Parallel()

	now := verifyNow()
	settledAt := now.Add(-5 * time.Minute)
	began := now.Add(-10 * time.Minute)

	for _, c := range []struct {
		name    string
		run     verifyRun
		checks  checksAt
		started time.Time
		seen    *time.Time

		verdict Verdict
		outcome catalog.Outcome
		words   string
		read    bool
		names   bool
	}{
		{
			name: "every check passes on its commit: fixed", run: pushed(),
			checks:  checksAt{head: runCommit, passed: 4, done: settledAt},
			verdict: VerdictFixed, outcome: catalog.OutcomePassedChecks, words: "Fixed: checks pass on bbbbbbb.",
		},
		{
			name: "a check fails on its commit: still failing, named", run: pushed(),
			checks:  checksAt{head: runCommit, failed: 1, passed: 3, done: settledAt, failedNames: []string{"CodeQL"}},
			verdict: VerdictFailing, outcome: catalog.OutcomeFailingChecks, words: "Still failing: CodeQL.",
		},
		{
			name: "many fail: the first named, the rest counted", run: pushed(),
			checks: checksAt{
				head: runCommit, failed: 5, done: settledAt, failedNames: []string{"a", "b", "c", "d", "e"},
			},
			verdict: VerdictFailing, outcome: catalog.OutcomeFailingChecks, words: "Still failing: a, b, c, and 2 more.",
		},
		{
			name: "checks still run: it waits, and the follower reads them", run: pushed(),
			checks: checksAt{head: runCommit, pending: 2, passed: 1},
		},
		{
			name: "checks still run where nothing read them lately: it reads the item", run: pushed(),
			checks: checksAt{head: runCommit, pending: 2, detailsAt: now.Add(-5 * time.Minute)}, read: true,
		},
		{
			name: "checks still run past the give-up time: unknown", run: pushed(), started: now.Add(-3 * time.Hour),
			checks:  checksAt{head: runCommit, pending: 1},
			verdict: VerdictUnknown, words: "Unknown: the checks on bbbbbbb hadn't finished after 2 hours.",
		},
		{
			name: "the last check finished a moment ago: it waits for them to settle", run: pushed(),
			checks: checksAt{head: runCommit, passed: 4, done: now.Add(-30 * time.Second)},
		},
		{
			name: "every check finished, their names unread: it reads them", run: pushed(),
			checks: checksAt{head: runCommit, failed: 1, passed: 3}, names: true,
		},
		{
			name: "workflow runs wait for a maintainer: unknown, at once", run: pushed(),
			checks:  checksAt{head: runCommit, awaiting: true},
			verdict: VerdictUnknown, words: "Unknown: the workflow runs on bbbbbbb wait for a maintainer to approve them.",
		},
		{
			name: "no check has appeared yet: it reads the item again", run: pushed(), seen: new(now.Add(-time.Minute)),
			checks: checksAt{head: runCommit, detailsAt: now.Add(-3 * time.Minute)}, read: true,
		},
		{
			name: "no check appeared for a while: unknown", run: pushed(), seen: new(now.Add(-11 * time.Minute)),
			checks:  checksAt{head: runCommit},
			verdict: VerdictUnknown, words: "Unknown: no checks ran on bbbbbbb.",
		},
		{
			name: "its push isn't seen yet: it reads the item", run: pushed(),
			checks: checksAt{head: headBefore, failed: 1, detailsAt: now.Add(-3 * time.Minute)}, read: true,
		},
		{
			name: "its push is never seen: unknown", run: pushed(), started: now.Add(-3 * time.Hour),
			checks:  checksAt{head: headBefore, failed: 1},
			verdict: VerdictUnknown, words: "Unknown: its push of bbbbbbb wasn't seen on GitHub within 2 hours.",
		},
		{
			name: "someone else pushed after it: overtaken", run: pushed(),
			checks:  checksAt{head: theirs, pending: 1},
			verdict: VerdictOvertaken, words: "Pushed bbbbbbb, but someone else pushed after it, so its fix wasn't checked.",
		},
		{
			name: "its commit was seen, then the head went back: overtaken", run: pushed(), seen: new(now.Add(-time.Minute)),
			checks:  checksAt{head: headBefore},
			verdict: VerdictOvertaken, words: "Pushed bbbbbbb, but someone else pushed after it, so its fix wasn't checked.",
		},
		{
			name:    "the agent made no commit: no push, in its words",
			run:     verifyRun{HeadBefore: headBefore, HeadAfter: headBefore, Summary: "Finished: nothing to do."},
			checks:  checksAt{head: headBefore, failed: 1},
			verdict: VerdictNoPush, words: "No push: nothing to do.",
		},
		{
			name: "a commit it didn't push while the branch moved: its push was rejected",
			run: verifyRun{
				HeadBefore: headBefore, HeadAfter: runCommit, Summary: "Finished: the push was refused.",
				EndedAt: now.Add(-2 * time.Minute),
			},
			checks:  checksAt{head: theirs, pending: 1, detailsAt: now.Add(-time.Minute)},
			verdict: VerdictRejected, words: "Its push was rejected: the branch moved while it worked.",
		},
		{
			name: "a commit it didn't push, the item not read since: it reads it first",
			run: verifyRun{
				HeadBefore: headBefore, HeadAfter: runCommit, Summary: "Finished: blocked.", EndedAt: now.Add(-time.Minute),
			},
			checks: checksAt{head: headBefore, failed: 1, detailsAt: now.Add(-5 * time.Minute)}, read: true,
		},
		{
			name: "a commit it didn't push, the branch where it was: no push",
			run: verifyRun{
				HeadBefore: headBefore, HeadAfter: runCommit, Summary: "Finished: blocked on a review.",
				EndedAt: now.Add(-2 * time.Minute),
			},
			checks:  checksAt{head: headBefore, failed: 1, detailsAt: now.Add(-time.Minute)},
			verdict: VerdictNoPush, words: "No push: blocked on a review.",
		},
		{
			name:    "its report didn't say, but its commit is the head: its checks decide",
			run:     verifyRun{HeadBefore: headBefore, HeadAfter: runCommit, EndedAt: now.Add(-2 * time.Minute)},
			checks:  checksAt{head: runCommit, passed: 2, done: settledAt},
			verdict: VerdictFixed, outcome: catalog.OutcomePassedChecks, words: "Fixed: checks pass on bbbbbbb.",
		},
		{
			name: "the counts aren't read: it reads the item", run: pushed(),
			checks: checksAt{head: runCommit, unreadCount: true}, read: true,
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			started := c.started
			if started.IsZero() {
				started = began
			}

			got := judgeVerify(verifyCase{
				run: c.run, wait: spec.WaitStep{}, facts: c.checks.facts(), known: true, started: started, seen: c.seen,
				now: now,
			})

			assert.Equal(t, c.verdict, got.verdict)
			assert.Equal(t, c.outcome, got.outcome)
			assert.Equal(t, c.words, got.words)
			assert.Equal(t, c.read, got.read, "read the item again")
			assert.Equal(t, c.names, got.names, "read the check names")
		})
	}
}

func TestAVerdictOnTheChecksWaitsForTheStepsOwnSettleAndGiveUp(t *testing.T) {
	t.Parallel()

	now := verifyNow()
	wait := spec.WaitStep{SettleMinutes: new(10), GiveUpHours: new(1)}

	settling := judgeVerify(verifyCase{
		run: pushed(), wait: wait, known: true, started: now.Add(-20 * time.Minute), now: now,
		facts: checksAt{head: runCommit, passed: 3, done: now.Add(-5 * time.Minute)}.facts(),
	})
	assert.True(t, settling.waiting(), "five minutes after the last check is short of its ten")

	gaveUp := judgeVerify(verifyCase{
		run: pushed(), wait: wait, known: true, started: now.Add(-61 * time.Minute), now: now,
		facts: checksAt{head: runCommit, pending: 1}.facts(),
	})
	assert.Equal(t, VerdictUnknown, gaveUp.verdict)
	assert.Equal(t, "Unknown: the checks on bbbbbbb hadn't finished after 1 hour.", gaveUp.words)
}

func TestAnItemNotStoredIsReadBeforeAVerdict(t *testing.T) {
	t.Parallel()

	got := judgeVerify(verifyCase{run: pushed(), known: false, started: verifyNow(), now: verifyNow()})
	assert.True(t, got.waiting())
	assert.True(t, got.read)
}

func TestTheLastTryThatStillFailsSaysItGaveUp(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "Gave up after 2 tries.", gaveUpWords(2))
	assert.Equal(t, "Gave up after 1 try.", gaveUpWords(1))
	assert.True(t, strings.HasPrefix(failingWords(nil, 2, runCommit), "Still failing: 2 checks failed on bbbbbbb"),
		"a failure whose names aren't known counts them")
}
