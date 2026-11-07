// Package snooze decides how long a snooze lasts and what new activity ends one early.
//
// A snooze sets an inbox item aside until a time. When it starts, the item's activity is
// recorded as a State (its head commit, commit and comment counts, and reviews). Each
// refresh compares the item's State then with its State now under the user's Rules, and
// Wake reports what new activity, if any, should bring the item back before its time:
// new commits or a force-push, a review, or new comments, each past its threshold.
//
// The rules are kept in the prefs table under the snooze.* keys, which the settings
// service writes and the inbox reads on every refresh. A missing or unreadable value
// falls back to its default.
package snooze

import (
	"errors"
	"fmt"
	"strconv"
)

// Bounds on a snooze's length, in days.
const (
	MinDays = 1
	MaxDays = 365
)

// MaxThreshold is the largest count of new commits or comments a rule can wait for.
const MaxThreshold = 99

// Prefs keys for the snooze settings, which the settings package writes.
const (
	// PrefDays is how many days the Snooze button sets an item aside.
	PrefDays = "snooze.days"

	// PrefWakeCommits is how many new commits wake a snooze (0 for never).
	PrefWakeCommits = "snooze.wake_commits"

	// PrefWakeReviews is which reviews wake a snooze: ReviewsNever, ReviewsDecisions, or
	// ReviewsAny.
	PrefWakeReviews = "snooze.wake_reviews"

	// PrefWakeComments is how many new comments wake a snooze (0 for never).
	PrefWakeComments = "snooze.wake_comments"
)

// Which reviews wake a snooze.
const (
	// ReviewsNever ignores reviews.
	ReviewsNever = "never"

	// ReviewsDecisions wakes on a new approval or request for changes, or a change of
	// the review decision to either.
	ReviewsDecisions = "decisions"

	// ReviewsAny also wakes on a review that only comments.
	ReviewsAny = "any"
)

// Review outcomes a wake reports, most decisive first.
const (
	ReviewApproved         = "approved"
	ReviewChangesRequested = "changes_requested"
	ReviewNew              = "reviewed"
)

// GitHub's review decisions a State records.
const (
	decisionApproved         = "APPROVED"
	decisionChangesRequested = "CHANGES_REQUESTED"
)

// Defaults for a fresh database: three days, waking on any push or a review decision,
// and not on comments, which bots post as often as people do.
const (
	DefaultDays         = 3
	DefaultWakeCommits  = 1
	DefaultWakeReviews  = ReviewsDecisions
	DefaultWakeComments = 0
)

var (
	// ErrInvalidDays is returned for a snooze shorter or longer than allowed.
	ErrInvalidDays = errors.New("invalid snooze length")

	// ErrInvalidRules is returned for a wake rule that is out of range or unknown.
	ErrInvalidRules = errors.New("invalid snooze wake rules")
)

// Rules decide which new activity ends a snooze early. Every field is a plain value.
type Rules struct {
	// Commits is how many new commits wake a snooze; 0 is never. At 1, any push does,
	// a force-push included, except on a bot's pull request, where a force-push is
	// usually a rebase.
	Commits int `json:"commits"`

	// Reviews is ReviewsNever, ReviewsDecisions, or ReviewsAny.
	Reviews string `json:"reviews"`

	// Comments is how many new comments wake a snooze; 0 is never. Review comments
	// count, and so do bots'.
	Comments int `json:"comments"`
}

// DefaultRules are the wake rules of a fresh database.
func DefaultRules() Rules {
	return Rules{Commits: DefaultWakeCommits, Reviews: DefaultWakeReviews, Comments: DefaultWakeComments}
}

// Validate reports whether every rule is in range.
func (r Rules) Validate() error {
	switch {
	case r.Commits < 0 || r.Commits > MaxThreshold:
		return fmt.Errorf("%w: %d new commits; choose 0 to %d", ErrInvalidRules, r.Commits, MaxThreshold)
	case r.Comments < 0 || r.Comments > MaxThreshold:
		return fmt.Errorf("%w: %d new comments; choose 0 to %d", ErrInvalidRules, r.Comments, MaxThreshold)
	case !validReviews(r.Reviews):
		return fmt.Errorf("%w: reviews %q; choose %q, %q, or %q", ErrInvalidRules, r.Reviews,
			ReviewsNever, ReviewsDecisions, ReviewsAny)
	default:
		return nil
	}
}

// ValidDays reports whether days is a snooze length allowed.
func ValidDays(days int) bool {
	return days >= MinDays && days <= MaxDays
}

// DecodeDays reads the stored snooze length, or the default when it is missing or bad.
func DecodeDays(prefs map[string]string) int {
	if v, err := strconv.Atoi(prefs[PrefDays]); err == nil && ValidDays(v) {
		return v
	}

	return DefaultDays
}

// DecodeRules reads the stored wake rules; each one missing or bad falls back to its
// default.
func DecodeRules(prefs map[string]string) Rules {
	rules := DefaultRules()

	if v, err := strconv.Atoi(prefs[PrefWakeCommits]); err == nil && v >= 0 && v <= MaxThreshold {
		rules.Commits = v
	}

	if v := prefs[PrefWakeReviews]; validReviews(v) {
		rules.Reviews = v
	}

	if v, err := strconv.Atoi(prefs[PrefWakeComments]); err == nil && v >= 0 && v <= MaxThreshold {
		rules.Comments = v
	}

	return rules
}

// Encode returns the prefs that store the rules.
func (r Rules) Encode() map[string]string {
	return map[string]string{
		PrefWakeCommits:  strconv.Itoa(r.Commits),
		PrefWakeReviews:  r.Reviews,
		PrefWakeComments: strconv.Itoa(r.Comments),
	}
}

// validReviews reports whether v is one of the review rules.
func validReviews(v string) bool {
	return v == ReviewsNever || v == ReviewsDecisions || v == ReviewsAny
}

// State is an item's activity at one moment: what a snooze compares.
type State struct {
	// Head is a pull request's head commit SHA ("" for an issue, or before it is known).
	Head string `json:"head"`

	Commits  int `json:"commits"`
	Comments int `json:"comments"`

	// Decision is GitHub's review decision ("" when the repository requires none).
	Decision string `json:"decision"`

	// Reviewed, Approvals, and ChangesRequested count reviewers as the inbox does.
	Reviewed         int `json:"reviewed"`
	Approvals        int `json:"approvals"`
	ChangesRequested int `json:"changesRequested"`
}

// Reasons say what new activity ended a snooze early. Every field is a plain value.
type Reasons struct {
	// Commits is how many commits were added since the snooze began.
	Commits int `json:"commits"`

	// ForcePushed is set when the pull request was pushed without adding commits.
	ForcePushed bool `json:"forcePushed"`

	// Review is ReviewApproved, ReviewChangesRequested, ReviewNew, or "".
	Review string `json:"review"`

	// Comments is how many comments were added since the snooze began.
	Comments int `json:"comments"`
}

// Woke reports whether any reason is set.
func (r Reasons) Woke() bool {
	return r.Commits > 0 || r.ForcePushed || r.Review != "" || r.Comments > 0
}

// Wake compares an item's State when it was snoozed with its State now, under rules,
// and returns the new activity that should end the snooze. Only activity past a rule's
// threshold is reported; empty Reasons mean the snooze goes on. botAuthor is set for a
// bot's pull request, whose force-pushes are rebases and do not count.
func Wake(rules Rules, then, now State, botAuthor bool) Reasons {
	var out Reasons

	if rules.Commits > 0 {
		added := now.Commits - then.Commits

		switch {
		case added >= rules.Commits:
			out.Commits = added
		case rules.Commits == 1 && !botAuthor && then.Head != "" && now.Head != "" && now.Head != then.Head:
			out.ForcePushed = true
		}
	}

	out.Review = reviewWake(rules.Reviews, then, now)

	if added := now.Comments - then.Comments; rules.Comments > 0 && added >= rules.Comments {
		out.Comments = added
	}

	return out
}

// reviewWake returns the review outcome that should end a snooze under rule, or "".
func reviewWake(rule string, then, now State) string {
	if rule != ReviewsDecisions && rule != ReviewsAny {
		return ""
	}

	switch {
	case now.ChangesRequested > then.ChangesRequested || turned(then, now, decisionChangesRequested):
		return ReviewChangesRequested
	case now.Approvals > then.Approvals || turned(then, now, decisionApproved):
		return ReviewApproved
	case rule == ReviewsAny && now.Reviewed > then.Reviewed:
		return ReviewNew
	default:
		return ""
	}
}

// turned reports whether the review decision became decision since then.
func turned(then, now State, decision string) bool {
	return now.Decision == decision && then.Decision != decision
}
