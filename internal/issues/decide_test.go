package issues

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// readFacts are the facts of #12 once the sheet read it: open, assigned to the user, who
// may close it.
func readFacts() Facts {
	return Facts{Read: true, CanWrite: true, CanClose: true, Assigned: true, OpenedAt: "2026-11-06T17:00:00Z"}
}

func TestTheStandings(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		facts func(f *Facts)
		want  Standing
	}{
		{
			name:  "gone",
			facts: func(f *Facts) { f.Gone, f.Closed = true, true },
			want: Standing{State: StandingGone, Sentence: "GitHub can't find it any more: it was deleted or moved, " +
				"or your account can't see it"},
		},
		{
			name: "closed as completed, by a pull request",
			facts: func(f *Facts) {
				f.Closed, f.StateReason, f.ClosedBy, f.ClosedAt = true, ReasonCompleted, "acme-org/example-repo#14", "t"
			},
			want: Standing{State: StandingClosed, Sentence: "Closed as completed by acme-org/example-repo#14", At: "t"},
		},
		{
			name:  "closed as not planned",
			facts: func(f *Facts) { f.Closed, f.StateReason = true, ReasonNotPlanned },
			want:  Standing{State: StandingClosed, Sentence: "Closed as not planned"},
		},
		{
			name: "closed as a duplicate",
			facts: func(f *Facts) {
				f.Closed, f.StateReason, f.DuplicateOf, f.ClosedBy = true, ReasonDuplicate, "acme-org/example-repo#3", octoOther
			},
			want: Standing{State: StandingClosed, Sentence: "Closed as a duplicate of acme-org/example-repo#3 by octo-other"},
		},
		{
			name:  "closed as a duplicate of one the user can't see",
			facts: func(f *Facts) { f.Closed, f.StateReason = true, ReasonDuplicate },
			want:  Standing{State: StandingClosed, Sentence: "Closed as a duplicate"},
		},
		{
			name:  "closed before GitHub kept reasons",
			facts: func(f *Facts) { f.Closed = true },
			want:  Standing{State: StandingClosed, Sentence: "Closed as completed"},
		},
		{
			name:  "closed as a reason this build doesn't know",
			facts: func(f *Facts) { f.Closed, f.StateReason = true, "ARCHIVED" },
			want:  Standing{State: StandingClosed, Sentence: "Closed"},
		},
		{
			name:  "locked, with write access",
			facts: func(f *Facts) { f.Locked, f.LockReason = true, "OFF_TOPIC" },
			want:  Standing{State: StandingLocked, Sentence: "Locked as off topic · you can still comment"},
		},
		{
			name:  "locked, without",
			facts: func(f *Facts) { f.Locked, f.CanWrite = true, false },
			want:  Standing{State: StandingLocked, Sentence: "Locked · only people with write access can comment"},
		},
		{
			name: "a fix merged",
			facts: func(f *Facts) {
				f.Fixes = []FixFacts{
					{Ref: "acme-org/example-repo#14", Open: true},
					{Ref: "acme-org/example-repo#15", Base: "release-1.2", Merged: true, MergedAt: "m"},
				}
			},
			want: Standing{State: StandingFixMerged, Sentence: "Fix merged: acme-org/example-repo#15 into release-1.2", At: "m"},
		},
		{
			name: "a fix in progress, a draft whose checks fail",
			facts: func(f *Facts) {
				f.Fixes = []FixFacts{
					{Ref: "acme-org/example-repo#13"}, // closed unmerged
					{Ref: "acme-org/example-repo#14", Open: true, Draft: true, Checks: ChecksFailing},
				}
			},
			want: Standing{State: StandingFixOpen, Sentence: "Fix in progress: acme-org/example-repo#14 · draft · checks failing"},
		},
		{
			name: "fixes in progress",
			facts: func(f *Facts) {
				f.Fixes = []FixFacts{{Ref: "acme-org/example-repo#14", Open: true}, {Ref: "acme-org/api#9", Open: true}}
				f.MoreFixes = 2
			},
			want: Standing{State: StandingFixOpen, Sentence: "Fixes in progress: acme-org/example-repo#14 and 3 more"},
		},
		{
			name:  "only closed fixes",
			facts: func(f *Facts) { f.Fixes = []FixFacts{{Ref: "acme-org/example-repo#13"}} },
			want:  Standing{State: StandingAssigned, Sentence: "Assigned to you", At: "2026-11-06T17:00:00Z", Opened: true},
		},
		{
			name:  "the user replied last",
			facts: func(f *Facts) { f.RepliedLast, f.RepliedAt = true, "r" },
			want:  Standing{State: StandingRepliedLast, Sentence: "You replied last", At: "r"},
		},
		{
			name:  "assigned",
			facts: func(*Facts) {},
			want:  Standing{State: StandingAssigned, Sentence: "Assigned to you", At: "2026-11-06T17:00:00Z", Opened: true},
		},
		{
			name:  "open",
			facts: func(f *Facts) { f.Assigned = false },
			want:  Standing{State: StandingOpen, Sentence: "Open", At: "2026-11-06T17:00:00Z", Opened: true},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			f := readFacts()
			tt.facts(&f)
			assert.Equal(t, tt.want, Decide(f).Standing)
		})
	}
}

func TestTheOffers(t *testing.T) {
	t.Parallel()

	comment := Offer{Action: ActionComment, Label: "Comment", Enabled: true}
	completed := Offer{
		Action: ActionCloseCompleted, Label: "Close as completed", Enabled: true, Confirm: true, CommentsFirst: true,
	}
	notPlanned := Offer{
		Action: ActionCloseNotPlanned, Label: "Close as not planned", Enabled: true, Confirm: true, CommentsFirst: true,
	}

	assert.Equal(t, Plan{
		Offers: []Offer{comment, completed, notPlanned}, Primary: ActionComment, Standing: Decide(readFacts()).Standing,
	}, Decide(readFacts()), "a comment is the button")

	before := Decide(Facts{Assigned: true})
	waiting := func(o Offer) Offer {
		o.Enabled, o.Reason = false, waitForRead

		return o
	}
	assert.Equal(t, []Offer{comment, waiting(completed), waiting(notPlanned)}, before.Offers, "before a read")
	assert.Equal(t, ActionComment, before.Primary)

	f := readFacts()
	f.CanClose = false
	assert.Equal(t, []Offer{comment}, Decide(f).Offers, "GitHub doesn't let the user close it")

	f = readFacts()
	f.Locked, f.CanWrite = true, false
	blocked := func(o Offer) Offer {
		o.CommentBlocked = lockedClose

		return o
	}
	locked := Decide(f)
	assert.Equal(t, []Offer{
		{Action: ActionComment, Label: "Comment", Reason: lockedComment}, blocked(completed), blocked(notPlanned),
	}, locked.Offers)
	assert.Equal(t, ActionNone, locked.Primary, "nothing the button can do: the menu has the closes")

	for _, gone := range []func(f *Facts){
		func(f *Facts) { f.Closed = true },
		func(f *Facts) { f.Gone = true },
	} {
		f = readFacts()
		gone(&f)
		assert.Empty(t, Decide(f).Offers)
		assert.Equal(t, ActionNone, Decide(f).Primary)
		assert.NotNil(t, Decide(f).Offers, "never nil")
	}
}

func TestAMergedFixMakesClosingTheButton(t *testing.T) {
	t.Parallel()

	f := readFacts()
	f.Fixes = []FixFacts{{Ref: "acme-org/example-repo#14", Base: "main", Merged: true}}
	assert.Equal(t, ActionCloseCompleted, Decide(f).Primary)

	f.Read = false
	assert.Equal(t, ActionComment, Decide(f).Primary, "not before the read lets it close")

	f = readFacts()
	f.Fixes = []FixFacts{{Ref: "acme-org/example-repo#14", Merged: true}}
	f.CanClose = false
	assert.Equal(t, ActionComment, Decide(f).Primary, "nor when GitHub won't let the user close it")

	f = readFacts()
	f.Fixes = []FixFacts{{Ref: "acme-org/example-repo#14", Merged: true}}
	f.Locked = true
	locked := Decide(f)
	assert.Equal(t, StandingLocked, locked.Standing.State, "the lock is what the sentence says")
	assert.Equal(t, ActionCloseCompleted, locked.Primary, "and the merged fix is what the button does")
}

func TestPlanOffer(t *testing.T) {
	t.Parallel()

	p := Decide(readFacts())

	o, ok := p.offer(ActionCloseNotPlanned)
	assert.True(t, ok)
	assert.Equal(t, "Close as not planned", o.Label)

	_, ok = p.offer(ActionNone)
	assert.False(t, ok)
}
