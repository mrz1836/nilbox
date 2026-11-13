package issues

import (
	"strconv"
	"strings"
)

// Facts are what the sheet knows of an issue, from its card and its last read, for Decide.
type Facts struct {
	// Read reports that the sheet has read the issue from GitHub; Gone that GitHub can't
	// find it any more.
	Read bool
	Gone bool

	// Closed reports a closed issue: StateReason why, ClosedBy who or what closed it
	// ("octo-other", "acme-org/api#14"), ClosedAt when, and DuplicateOf what it duplicates.
	Closed      bool
	StateReason StateReason
	ClosedBy    string
	ClosedAt    string
	DuplicateOf string

	// Locked reports a locked conversation, LockReason why; CanWrite that the user has
	// write access, so may comment on it anyway; CanClose that GitHub lets them close it.
	Locked     bool
	LockReason string
	CanWrite   bool
	CanClose   bool

	// Fixes are the pull requests that close it once merged, and MoreFixes how many others.
	Fixes     []FixFacts
	MoreFixes int

	// RepliedLast reports that the newest comment is the user's, written at RepliedAt.
	RepliedLast bool
	RepliedAt   string

	// Assigned reports that it is in the user's Assigned list; OpenedAt when it opened.
	Assigned bool
	OpenedAt string
}

// FixFacts is a pull request that closes the issue: its name, as in "acme-org/api#14", the
// branch it merges into, whether it merged and when, whether it is a draft, and its checks.
type FixFacts struct {
	Ref      string
	Base     string
	Merged   bool
	MergedAt string
	Open     bool
	Draft    bool
	Checks   ChecksWord
}

// Plan is what Decide works out: the offers, the button's action, and the standing.
type Plan struct {
	Offers   []Offer
	Primary  Action
	Standing Standing
}

// offer returns the plan's offer of a; ok is false when it makes none.
func (p Plan) offer(a Action) (Offer, bool) {
	for _, o := range p.Offers {
		if o.Action == a {
			return o, true
		}
	}

	return Offer{}, false
}

// The offers' words.
const (
	waitForRead   = "Wait for it to load from GitHub."
	lockedComment = "The conversation is locked: only people with write access to the repository can comment."
	lockedClose   = "The conversation is locked, so GitHub won't take your comment. Clear your text to close it " +
		"without a comment."
)

// Decide works out where the issue stands, what the sheet offers, and its button. A closed
// issue, or one GitHub can't find, offers nothing.
func Decide(f Facts) Plan {
	p := Plan{Offers: []Offer{}, Standing: standing(f)}
	if f.Closed || f.Gone {
		return p
	}

	locked := f.Locked && !f.CanWrite
	comment := Offer{Action: ActionComment, Label: "Comment", Enabled: !locked}

	if locked {
		comment.Reason = lockedComment
	}

	p.Offers = append(p.Offers, comment)

	for _, a := range []Action{ActionCloseCompleted, ActionCloseNotPlanned} {
		if o, ok := closeOffer(f, a, locked); ok {
			p.Offers = append(p.Offers, o)
		}
	}

	p.Primary = primary(f, p)

	return p
}

// closeOffer is the offer of a close, which waits for a read, since only a read says
// whether GitHub lets the user close the issue, and is left out when it doesn't.
func closeOffer(f Facts, a Action, locked bool) (Offer, bool) {
	label := "Close as completed"
	if a == ActionCloseNotPlanned {
		label = "Close as not planned"
	}

	o := Offer{Action: a, Label: label, Enabled: true, Confirm: true, CommentsFirst: true}

	switch {
	case !f.Read:
		o.Enabled, o.Reason = false, waitForRead
	case !f.CanClose:
		return Offer{}, false
	case locked:
		o.CommentBlocked = lockedClose
	}

	return o, true
}

// primary is the sheet's button: closing as completed once a fix merged, whatever else the
// issue's standing says first (a lock), else a comment, whichever is enabled.
func primary(f Facts, p Plan) Action {
	_, merged := mergedFix(f)

	switch {
	case merged && p.enabled(ActionCloseCompleted):
		return ActionCloseCompleted
	case p.enabled(ActionComment):
		return ActionComment
	default:
		return ActionNone
	}
}

// enabled reports that the plan offers a, and that it can run now.
func (p Plan) enabled(a Action) bool {
	o, ok := p.offer(a)

	return ok && o.Enabled
}

// standing works out where the issue stands: the first standing, highest first, that holds.
func standing(f Facts) Standing {
	switch {
	case f.Gone:
		return Standing{State: StandingGone, Sentence: "GitHub can't find it any more: it was deleted or moved, " +
			"or your account can't see it"}
	case f.Closed:
		return Standing{State: StandingClosed, Sentence: closedSentence(f), At: f.ClosedAt}
	case f.Locked:
		return Standing{State: StandingLocked, Sentence: lockedSentence(f)}
	}

	if s, ok := fixStanding(f); ok {
		return s
	}

	switch {
	case f.RepliedLast:
		return Standing{State: StandingRepliedLast, Sentence: "You replied last", At: f.RepliedAt}
	case f.Assigned:
		return Standing{State: StandingAssigned, Sentence: "Assigned to you", At: f.OpenedAt, Opened: true}
	default:
		return Standing{State: StandingOpen, Sentence: "Open", At: f.OpenedAt, Opened: true}
	}
}

// closedSentence says how a closed issue was closed, and by whom when the read says.
func closedSentence(f Facts) string {
	var s string

	switch f.StateReason {
	case ReasonNotPlanned:
		s = "Closed as not planned"
	case ReasonDuplicate:
		s = "Closed as a duplicate"
		if f.DuplicateOf != "" {
			s += " of " + f.DuplicateOf
		}
	case ReasonCompleted, ReasonNone, ReasonReopened:
		s = "Closed as completed"
	default:
		s = "Closed"
	}

	if f.ClosedBy != "" {
		s += " by " + f.ClosedBy
	}

	return s
}

// lockedSentence says why the conversation is locked, and whether the user can still
// comment.
func lockedSentence(f Facts) string {
	s := "Locked"
	if f.LockReason != "" {
		s += " as " + strings.ReplaceAll(strings.ToLower(f.LockReason), "_", " ")
	}

	if f.CanWrite {
		return s + " · you can still comment"
	}

	return s + " · only people with write access can comment"
}

// mergedFix returns the first of the issue's fixes that merged; ok is false when none did.
func mergedFix(f Facts) (FixFacts, bool) {
	for _, fix := range f.Fixes {
		if fix.Merged {
			return fix, true
		}
	}

	return FixFacts{}, false
}

// fixStanding is the standing a fix gives: one merged, else one in progress.
func fixStanding(f Facts) (Standing, bool) {
	if fix, ok := mergedFix(f); ok {
		return Standing{
			State: StandingFixMerged, Sentence: "Fix merged: " + fix.Ref + " into " + fix.Base, At: fix.MergedAt,
		}, true
	}

	var open []FixFacts

	for _, fix := range f.Fixes {
		if fix.Open {
			open = append(open, fix)
		}
	}

	if len(open) == 0 {
		return Standing{}, false
	}

	s := "Fix in progress: " + open[0].Ref
	if open[0].Draft {
		s += " · draft"
	}

	if open[0].Checks == ChecksFailing {
		s += " · checks failing"
	}

	if more := len(open) - 1 + f.MoreFixes; more > 0 {
		s = "Fixes in progress: " + strings.TrimPrefix(s, "Fix in progress: ") + " and " + strconv.Itoa(more) + " more"
	}

	return Standing{State: StandingFixOpen, Sentence: s}, true
}
