package inbox

import (
	"context"
	"strings"
	"sync"

	"github.com/mrz1836/nilbox/internal/github"
)

// A pull request the user requested changes on is pushed to since their review
// (reason_pushed, Details.PushedSinceReview) only when someone else pushed to it: commits
// the user pushed themselves, such as a merge of its base to fix its checks, never count,
// since the author still owes the changes. The fourth search reads the head and the user's
// verdict, but not who wrote the commits between them, and asking it for them would cost
// every refresh a point more. So a refresh asks, in one small read
// (github.FetchCommitAuthors), only about the pull requests whose head moved past the
// verdict, and only once per verdict and head: ownPushes remembers each answer while the
// app runs. When every commit after the verdict's is the user's own, the verdict covers
// the head: the refresh stores the head as the verdict's commit (viewer_review_commit,
// which only this rule reads), so the list and the card read it as not pushed to. A read
// that fails leaves it pushed to, as it was before, and is asked again next time.

// ownPushes remembers, by pull request node ID, what the last read found of the commits
// between the user's verdict and the head.
type ownPushes struct {
	mu   sync.Mutex
	seen map[string]coverage
}

// coverage is what a read found for one verdict and head: covered when every commit after
// the verdict's is the user's own.
type coverage struct {
	verdict string
	head    string
	covered bool
}

// coverOwnPushes moves the verdict of each pull request in the fourth search that only
// the user pushed to since onto its head (see ownPushes), reading the commit authors of
// those it hasn't judged at their verdict and head with tok.
func (s *Service) coverOwnPushes(ctx context.Context, tok github.Token, inbox *github.Inbox) {
	items := inbox.Reviewed.Items
	known, ask := s.own.judged(items)

	if len(ask) > 0 {
		commits, err := s.client.FetchCommitAuthors(ctx, tok, ask)
		if err != nil {
			s.logger.DebugContext(ctx, "could not read who pushed to the pull requests reviewed", "error", err)
		}

		judge(items, commits, inbox.Viewer.Login, known)
	}

	s.own.keep(known)

	for i := range items {
		it := &items[i]
		if c, ok := known[it.NodeID]; ok && c.covered && it.PushedSinceChanges() {
			v := *it.Verdict
			v.Commit = it.HeadOID
			it.Verdict = &v
		}
	}
}

// judged returns what is known of the pull requests among items pushed to past the
// user's verdict, by node ID, and the IDs of those to ask about: judged at another verdict
// or head, or never.
func (o *ownPushes) judged(items []github.Item) (map[string]coverage, []string) {
	o.mu.Lock()
	defer o.mu.Unlock()

	known := make(map[string]coverage)

	var ask []string

	for _, it := range items {
		if !it.PushedSinceChanges() {
			continue
		}

		if c, ok := o.seen[it.NodeID]; ok && c.at(it) {
			known[it.NodeID] = c
		} else {
			ask = append(ask, it.NodeID)
		}
	}

	return known, ask
}

// keep remembers known, and forgets what the last refresh knew of anything else.
func (o *ownPushes) keep(known map[string]coverage) {
	o.mu.Lock()
	defer o.mu.Unlock()

	o.seen = known
}

// at reports a judgment of it as it is now: at its verdict and head.
func (c coverage) at(it github.Item) bool {
	return strings.EqualFold(c.verdict, it.Verdict.Commit) && strings.EqualFold(c.head, it.HeadOID)
}

// judge adds to known what commits, the commit authors read by node ID, say of each pull
// request among items pushed to past the user's verdict, viewer.
func judge(items []github.Item, commits map[string][]github.CommitAuthor, viewer string, known map[string]coverage) {
	for _, it := range items {
		got, read := commits[it.NodeID]
		if !read || !it.PushedSinceChanges() {
			continue
		}

		if covered, ok := ownSince(got, it.Verdict.Commit, it.HeadOID, viewer); ok {
			known[it.NodeID] = coverage{verdict: it.Verdict.Commit, head: it.HeadOID, covered: covered}
		}
	}
}

// ownSince reports whether every commit after verdict, among a pull request's newest
// commits (oldest first), is viewer's own, with ok false when the commits read end
// elsewhere than head, as when it was pushed to between the two reads. A commit whose
// author matches no account counts as someone else's, and so does a verdict older than
// every commit read.
func ownSince(commits []github.CommitAuthor, verdict, head, viewer string) (covered, ok bool) {
	if len(commits) == 0 || !strings.EqualFold(commits[len(commits)-1].OID, head) {
		return false, false
	}

	for i := len(commits) - 1; i >= 0; i-- {
		c := commits[i]

		switch {
		case strings.EqualFold(c.OID, verdict):
			return true, true
		case viewer == "" || !strings.EqualFold(c.Login, viewer):
			return false, true
		}
	}

	return false, true
}
