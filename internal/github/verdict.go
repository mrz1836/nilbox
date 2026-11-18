package github

import "time"

// opinionsField reads the latest review of each reviewer that approved the pull request
// or requested changes (latestOpinionatedReviews), the first 50, each with whether the
// viewer wrote it. A later review that only comments, as a reply to a line comment is,
// never hides it, as it hides the viewer's latest review (viewerLatestReview). Asked
// beside the review sheet's other fields, or beside one item's in the item query (with or
// without its projects), it costs no point (measured in November 2026); beside the inbox
// card's details it costs a point more per batch of 25, so the details and the followers'
// batches never ask for it.
const opinionsField = `opinions: latestOpinionatedReviews(first: 50) {
    nodes { state url submittedAt viewerDidAuthor commit { oid } }
  }`

// Verdict is the viewer's own standing review of a pull request: their latest review that
// approved it or requested changes.
type Verdict struct {
	// State is APPROVED or CHANGES_REQUESTED.
	State string

	// Commit is the head commit the review was left on, "" when GitHub doesn't say.
	Commit string

	// SubmittedAt is when it was submitted, and URL its page.
	SubmittedAt time.Time
	URL         string
}

// ChangesRequested reports a verdict that requests changes.
func (v *Verdict) ChangesRequested() bool {
	return v != nil && v.State == ReviewStateChangesRequested
}

// graphQLOpinions is what opinionsField reads. GitHub's nulls decode to zero values.
type graphQLOpinions struct {
	Nodes []*struct {
		State           string         `json:"state"`
		URL             string         `json:"url"`
		SubmittedAt     time.Time      `json:"submittedAt"`
		ViewerDidAuthor bool           `json:"viewerDidAuthor"`
		Commit          *graphQLCommit `json:"commit"`
	} `json:"nodes"`
}

// verdict returns the viewer's own review among the opinions that approves or requests
// changes, or nil when there is none: a dismissed review is no verdict.
func (o graphQLOpinions) verdict() *Verdict {
	for _, n := range o.Nodes {
		if n == nil || !n.ViewerDidAuthor {
			continue
		}

		if n.State != ReviewStateApproved && n.State != ReviewStateChangesRequested {
			continue
		}

		return &Verdict{
			State: n.State, Commit: n.Commit.oid(), SubmittedAt: n.SubmittedAt.UTC(), URL: n.URL,
		}
	}

	return nil
}
