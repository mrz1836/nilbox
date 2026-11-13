package core

import (
	"context"

	"github.com/mrz1836/nilbox/internal/github"
	"github.com/mrz1836/nilbox/internal/inbox"
	"github.com/mrz1836/nilbox/internal/issues"
	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/prompts"
)

// issuesReplies hands the issue sheet the prompt library's comment replies that apply to
// issues, filled in with what a launch on the issue would fill a prompt with.
type issuesReplies struct {
	replyFiller
}

// Offered returns the comment replies that apply to issues, in the library's order. None is
// a default: the review sheet's defaults are for pull requests.
func (r issuesReplies) Offered(ctx context.Context) ([]issues.Reply, error) {
	lib, err := r.store.Library(ctx)
	if err != nil {
		return nil, err
	}

	out := []issues.Reply{}

	for _, p := range lib.Prompts {
		if p.IsReply() && p.ReplyAction == prompts.ReplyComment && p.AppliesTo.Covers(item.KindIssue) {
			out = append(out, issues.Reply{ID: p.ID, Name: p.Name, Nickname: p.Nickname})
		}
	}

	return out, nil
}

// Fill fills in the reply with replyID for the issue with itemID.
func (r issuesReplies) Fill(ctx context.Context, replyID, itemID int64) (string, error) {
	return r.fill(ctx, replyID, itemID)
}

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
