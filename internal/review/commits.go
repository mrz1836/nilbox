package review

import (
	"slices"
	"strings"

	"github.com/mrz1836/nilbox/internal/clock"
	"github.com/mrz1836/nilbox/internal/timeline"
)

// Since is what happened to a pull request's branch since the signed-in user's own
// standing review of it.
type Since string

// What happened since the user's review.
const (
	// SinceNone is nothing the sheet can tell: the review is at the head, or the sheet hasn't
	// read the commits.
	SinceNone Since = ""

	// SincePushed is YourReview.Count commits on top of the commit the review was left on.
	SincePushed Since = "pushed"

	// SinceBeyond is more commits on top of it than the sheet reads: the reviewed commit is
	// among the earlier ones GitHub lists.
	SinceBeyond Since = "beyond"

	// SinceRewritten is a branch that no longer holds the reviewed commit: a force push, as
	// a rebase is.
	SinceRewritten Since = "rewritten"
)

// shortSHA is how many of a commit's SHA the sheet shows.
const shortSHA = 7

// Commit is one of a pull request's commits, as the sheet shows it.
type Commit struct {
	// OID is its SHA, and Short the first seven characters of it.
	OID   string `json:"oid"`
	Short string `json:"short"`

	// Headline is its message's first line.
	Headline string `json:"headline"`

	// Author is its author's GitHub login, or else the name the commit gives.
	Author string `json:"author"`

	// CommittedAt is when it was committed, as RFC 3339, and URL its page in the pull request.
	CommittedAt string `json:"committedAt"`
	URL         string `json:"url"`
}

// YourReview is the signed-in user's own standing review of a pull request, the latest that
// approved it or requested changes, and what happened to the branch since.
type YourReview struct {
	// State is APPROVED or CHANGES_REQUESTED, and Commit the head commit it was left on.
	State  timeline.ReviewState `json:"state"`
	Commit string               `json:"commit"`

	// At is when it was submitted, as RFC 3339, or "", and URL its page.
	At  string `json:"at"`
	URL string `json:"url"`

	// Since is what happened to the branch since, and Count the commits on top of the
	// reviewed one, under SincePushed.
	Since Since `json:"since"`
	Count int   `json:"count"`
}

// changesRequestedSince reports a standing request for changes the branch moved since: the
// commits after it, or the branch rewritten. It is false for nil.
func (y *YourReview) changesRequestedSince() bool {
	return y != nil && y.State == timeline.ReviewChangesRequested && y.Since != SinceNone
}

// commitsOf converts the cached commits for the pull request whose page is pullURL.
func commitsOf(cached []contextCommit, pullURL string) []Commit {
	out := make([]Commit, 0, len(cached))

	for _, c := range cached {
		author := c.Author
		if author == "" {
			author = c.AuthorName
		}

		commit := Commit{
			OID: c.OID, Short: c.OID[:min(len(c.OID), shortSHA)], Headline: c.Headline, Author: author,
			URL: strings.TrimSuffix(pullURL, "/") + "/commits/" + c.OID,
		}

		if !c.CommittedAt.IsZero() {
			commit.CommittedAt = clock.Format(c.CommittedAt)
		}

		out = append(out, commit)
	}

	return out
}

// yourReview works out the user's standing review op and what happened to the branch
// since, from the newest commits, of total in all, and the conversation's newest entries.
// It goes by where the reviewed commit is among the commits, never by their dates, which a
// rebase keeps: found, the commits after it were pushed since; missing from every commit
// the pull request has, the branch was rewritten; missing from the newest only, it was
// rewritten when a force push follows the review, and is further back otherwise. With no
// commits read, as from an older build's cache, it says nothing of since. It returns nil
// without a standing review.
func yourReview(op *contextReview, commits []contextCommit, total int, entries []timeline.Entry) *YourReview {
	if op == nil || op.Commit == "" {
		return nil
	}

	out := &YourReview{State: timeline.ReviewState(op.State), Commit: op.Commit, URL: op.URL}
	if !op.SubmittedAt.IsZero() {
		out.At = clock.Format(op.SubmittedAt)
	}

	if len(commits) == 0 {
		return out
	}

	at := slices.IndexFunc(commits, func(c contextCommit) bool { return strings.EqualFold(c.OID, op.Commit) })

	switch {
	case at >= 0 && at < len(commits)-1:
		out.Since, out.Count = SincePushed, len(commits)-1-at
	case at >= 0:
		// The review is at the head: nothing since.
	case total <= len(commits), forcePushedAfter(entries, out.At):
		out.Since = SinceRewritten
	default:
		out.Since = SinceBeyond
	}

	return out
}

// forcePushedAfter reports a force push among entries after at, an RFC 3339 time; with no
// time, any force push counts.
func forcePushedAfter(entries []timeline.Entry, at string) bool {
	for _, e := range entries {
		if e.Kind == timeline.KindForcePushed && (at == "" || e.At > at) {
			return true
		}
	}

	return false
}
