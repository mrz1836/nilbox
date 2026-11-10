package core

import (
	"context"
	"fmt"
	"strings"

	"github.com/mrz1836/nilbox/internal/github"
	"github.com/mrz1836/nilbox/internal/inbox"
	"github.com/mrz1836/nilbox/internal/prompts"
	"github.com/mrz1836/nilbox/internal/review"
	"github.com/mrz1836/nilbox/internal/sensitive"
	"github.com/mrz1836/nilbox/internal/settings"
)

// reviewInbox hands the review sheet the inbox's stored pull requests, and runs its reads
// and actions through the inbox's Actor.
type reviewInbox struct {
	store *inbox.Store
	actor *inbox.Actor
}

// Pull returns the stored pull request with id.
func (r reviewInbox) Pull(ctx context.Context, id int64) (review.StoredPull, error) {
	it, err := r.store.ReviewItem(ctx, id)
	if err != nil {
		return review.StoredPull{}, err
	}

	// The two types have the same fields, so a conversion copies every one, and a field
	// added to one but not the other stops the build here.
	return review.StoredPull(it), nil
}

// Read runs read on the pull request with id through the inbox, which stores what it found.
func (r reviewInbox) Read(
	ctx context.Context, id int64, read func(context.Context, github.Token, review.Target) (review.PullRead, error),
) error {
	_, err := r.actor.ReadPull(ctx, id, func(ctx context.Context, tok github.Token, t inbox.PullTarget) (inbox.PullRead, error) {
		got, readErr := read(ctx, tok, review.Target(t))

		return inbox.PullRead(got), readErr
	})

	return err
}

// Act runs act on the pull request with id through the inbox, which records what it did.
func (r reviewInbox) Act(
	ctx context.Context, id int64, act func(context.Context, github.Token, review.Target) (review.Acted, error),
) error {
	_, err := r.actor.Act(ctx, id, func(ctx context.Context, tok github.Token, t inbox.PullTarget) (inbox.Acted, error) {
		acted, actErr := act(ctx, tok, review.Target(t))

		return inbox.Acted(acted), actErr
	})

	return err
}

// reviewSettings hands the review sheet the settings it follows.
type reviewSettings struct {
	settings *settings.Service
}

// MergeMethod returns how the user chose to merge.
func (r reviewSettings) MergeMethod(ctx context.Context) (review.MethodPref, error) {
	m, err := r.settings.MergeMethod(ctx)
	if err != nil {
		return "", err
	}

	return review.MethodPref(m), nil
}

// Sensitive returns which files are sensitive.
func (r reviewSettings) Sensitive(ctx context.Context) (sensitive.Config, error) {
	all, err := r.settings.Get(ctx)
	if err != nil {
		return sensitive.Config{}, err
	}

	return all.SensitiveFiles, nil
}

// reviewReplies hands the review sheet the review replies of the prompt library, filled in
// with what a launch on the pull request would fill a prompt with.
type reviewReplies struct {
	store  *prompts.Store
	source prompts.PreviewSource
}

// Offered returns the replies for action, with the defaults for a person's and a bot's
// pull request. A bot's falls back to the person's default when none is chosen.
func (r reviewReplies) Offered(ctx context.Context, action review.ReplyAction) (review.ReplyList, error) {
	lib, err := r.store.Library(ctx)
	if err != nil {
		return review.ReplyList{}, err
	}

	out := review.ReplyList{Replies: []review.Reply{}}

	for _, p := range lib.Prompts {
		if p.IsReply() && string(p.ReplyAction) == string(action) {
			out.Replies = append(out.Replies, review.Reply{
				ID: p.ID, Name: p.Name, Nickname: p.Nickname, Action: review.ReplyAction(p.ReplyAction),
			})
		}
	}

	d := lib.Defaults.Replies

	switch action {
	case review.ReplyApprove:
		out.Default, out.DefaultBots = d.Approve, d.ApproveBots
		if out.DefaultBots == 0 {
			out.DefaultBots = d.Approve
		}
	case review.ReplyRequestChanges:
		out.Default, out.DefaultBots = d.RequestChanges, d.RequestChanges
	case review.ReplyComment:
		out.Default, out.DefaultBots = d.Comment, d.Comment
	}

	return out, nil
}

// Fill fills in the reply with replyID for the inbox item with itemID.
func (r reviewReplies) Fill(ctx context.Context, replyID, itemID int64) (string, error) {
	p, err := r.store.Lookup(ctx, replyID)
	if err != nil {
		return "", err
	}

	if !p.IsReply() {
		return "", fmt.Errorf("%w: %q", prompts.ErrNotReply, p.Nickname)
	}

	tmpl, err := p.Template()
	if err != nil {
		return "", err
	}

	data, err := r.source.PreviewData(ctx, itemID)
	if err != nil {
		return "", err
	}

	out, err := tmpl.Execute(data)
	if err != nil {
		return "", err
	}

	return strings.TrimRight(out, "\n"), nil
}
