package core

import (
	"context"

	"github.com/mrz1836/nilbox/internal/github"
	"github.com/mrz1836/nilbox/internal/inbox"
	"github.com/mrz1836/nilbox/internal/issues"
)

// issuesInbox hands the issue sheet the inbox's stored issues, and runs its reads and
// actions through the inbox's Actor.
type issuesInbox struct {
	store *inbox.Store
	actor *inbox.Actor
}

// Issue returns the stored issue with id.
func (r issuesInbox) Issue(ctx context.Context, id int64) (issues.StoredIssue, error) {
	it, err := r.store.IssueItem(ctx, id)
	if err != nil {
		return issues.StoredIssue{}, err
	}

	// The two types have the same fields, so a conversion copies every one, and a field
	// added to one but not the other stops the build here.
	return issues.StoredIssue(it), nil
}

// Read runs read on the issue with id through the inbox, which stores what it found.
func (r issuesInbox) Read(
	ctx context.Context, id int64, read func(context.Context, github.Token, issues.Target) (issues.IssueRead, error),
) error {
	_, err := r.actor.ReadIssue(ctx, id,
		func(ctx context.Context, tok github.Token, t inbox.IssueTarget) (inbox.IssueRead, error) {
			got, readErr := read(ctx, tok, issues.Target(t))

			return inbox.IssueRead(got), readErr
		})

	return err
}

// Act runs act on the issue with id through the inbox, which records what it did.
func (r issuesInbox) Act(
	ctx context.Context, id int64, act func(context.Context, github.Token, issues.Target) (issues.Acted, error),
) error {
	_, err := r.actor.ActIssue(ctx, id,
		func(ctx context.Context, tok github.Token, t inbox.IssueTarget) (inbox.IssueActed, error) {
			acted, actErr := act(ctx, tok, issues.Target(t))

			return inbox.IssueActed(acted), actErr
		})

	return err
}

// Peek runs peek on the issue with id through the inbox, which stores nothing.
func (r issuesInbox) Peek(
	ctx context.Context, id int64, peek func(context.Context, github.Token, issues.Target) error,
) error {
	return r.actor.PeekIssue(ctx, id, func(ctx context.Context, tok github.Token, t inbox.IssueTarget) error {
		return peek(ctx, tok, issues.Target(t))
	})
}
