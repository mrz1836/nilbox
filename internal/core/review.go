package core

import (
	"context"

	"github.com/mrz1836/nilbox/internal/github"
	"github.com/mrz1836/nilbox/internal/inbox"
	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/people"
	"github.com/mrz1836/nilbox/internal/repo"
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

// Peek runs peek on the pull request with id through the inbox, which stores nothing.
func (r reviewInbox) Peek(
	ctx context.Context, id int64, peek func(context.Context, github.Token, review.Target) error,
) error {
	return r.actor.PeekPull(ctx, id, func(ctx context.Context, tok github.Token, t inbox.PullTarget) error {
		return peek(ctx, tok, review.Target(t))
	})
}

// reviewSettings hands the review sheet the settings it follows.
type reviewSettings struct {
	settings *settings.Service
}

// MergePrefs returns how the user chose to merge, and whether the sheet's button merges
// their own pull request past the rules when it waits only on an approval.
func (r reviewSettings) MergePrefs(ctx context.Context) (review.Prefs, error) {
	m, err := r.settings.MergeMethod(ctx)
	if err != nil {
		return review.Prefs{}, err
	}

	bypassOwn, err := r.settings.BypassOwn(ctx)
	if err != nil {
		return review.Prefs{}, err
	}

	return review.Prefs{Method: review.MethodPref(m), BypassOwn: bypassOwn}, nil
}

// Sensitive returns which files are sensitive.
func (r reviewSettings) Sensitive(ctx context.Context) (sensitive.Config, error) {
	return settingsPart(ctx, r.settings, func(s settings.Settings) sensitive.Config { return s.SensitiveFiles })
}

// IgnoredWorkflows returns the workflows whose held runs never count.
func (r reviewSettings) IgnoredWorkflows(ctx context.Context) ([]string, error) {
	return settingsPart(ctx, r.settings, func(s settings.Settings) []string { return s.IgnoredWorkflows })
}

// reviewReplies hands the review sheet the review replies of the prompt library that apply
// to pull requests, filled in with what a launch on the pull request would fill a prompt
// with.
type reviewReplies struct {
	replyFiller
}

// Offered returns the replies for action, with the defaults for a person's and a bot's
// pull request, and, among the comment replies, the default for a nudge. A bot's falls
// back to the person's default when none is chosen.
func (r reviewReplies) Offered(ctx context.Context, action review.ReplyAction) (review.ReplyList, error) {
	lib, err := r.store.Library(ctx)
	if err != nil {
		return review.ReplyList{}, err
	}

	out := review.ReplyList{Replies: []review.Reply{}}

	for _, p := range lib.Prompts {
		if p.IsReply() && string(p.ReplyAction) == string(action) && p.AppliesTo.Covers(item.KindPR) {
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
		out.Default, out.DefaultBots, out.Nudge = d.Comment, d.Comment, d.Nudge
	}

	return out, nil
}

// Fill fills in the reply with replyID for the pull request with itemID.
func (r reviewReplies) Fill(ctx context.Context, replyID, itemID int64) (string, error) {
	return r.fill(ctx, replyID, itemID)
}

// reviewPeople hands the review sheet the people directory: who works on each repository,
// which the sheet learns into and suggests reviewers from.
type reviewPeople struct {
	directory *people.Service
}

// Learn keeps what a read saw people do on the pull requests of the repository name.
func (r reviewPeople) Learn(ctx context.Context, name repo.Name, seen []review.PersonSeen) error {
	out := make([]people.Seen, 0, len(seen))
	for _, s := range seen {
		out = append(out, people.Seen{
			Login: s.Login, Name: s.Name, Number: s.Number, Role: people.Role(s.Role), Access: s.Access, At: s.At,
		})
	}

	return r.directory.Learn(ctx, name, out)
}

// SetTeams keeps teams of the repository name: its teams list when listed, else teams a
// pull request asked to review.
func (r reviewPeople) SetTeams(ctx context.Context, name repo.Name, teams []review.TeamCandidate, listed bool) error {
	out := make([]people.Team, 0, len(teams))
	for _, t := range teams {
		out = append(out, people.Team(t))
	}

	return r.directory.SetTeams(ctx, name, out, listed)
}

// MarkRead records a read of the repository name's recent pull requests that went through.
func (r reviewPeople) MarkRead(ctx context.Context, name repo.Name, pulls int, org bool) error {
	return r.directory.MarkRead(ctx, name, pulls, org)
}

// MarkFailed records a read of the repository name's recent pull requests that failed.
func (r reviewPeople) MarkFailed(ctx context.Context, name repo.Name) error {
	return r.directory.MarkFailed(ctx, name)
}

// Ranked returns who works on the repository name.
func (r reviewPeople) Ranked(ctx context.Context, name repo.Name) (review.Directory, error) {
	got, err := r.directory.Ranked(ctx, name)
	if err != nil {
		return review.Directory{}, err
	}

	out := review.Directory{
		People: make([]review.KnownPerson, 0, len(got.People)), Teams: make([]review.TeamCandidate, 0, len(got.Teams)),
		ReadAt: got.ReadAt, Pulls: got.Pulls, Org: got.Org, Stale: got.Stale,
	}

	for _, p := range got.People {
		out.People = append(out.People, review.KnownPerson{
			Login: p.Login, Name: p.Name, Reviewed: p.Reviewed, Authored: p.Authored, Committed: p.Committed, LastAt: p.LastAt,
		})
	}

	for _, t := range got.Teams {
		out.Teams = append(out.Teams, review.TeamCandidate(t))
	}

	return out, nil
}

// Summary counts the people the directory knows, and the repositories it knows anyone of.
func (r reviewPeople) Summary(ctx context.Context) (review.PeopleSummary, error) {
	got, err := r.directory.Summary(ctx)
	if err != nil {
		return review.PeopleSummary{}, err
	}

	return review.PeopleSummary{People: got.People, Repos: got.Repos}, nil
}

// Forget empties the directory.
func (r reviewPeople) Forget(ctx context.Context) error {
	return r.directory.Forget(ctx)
}
