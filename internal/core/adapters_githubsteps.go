package core

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/mrz1836/nilbox/internal/autopilot"
	"github.com/mrz1836/nilbox/internal/autopilot/catalog"
	"github.com/mrz1836/nilbox/internal/autopilot/spec"
	"github.com/mrz1836/nilbox/internal/budget"
	"github.com/mrz1836/nilbox/internal/ghthreads"
	"github.com/mrz1836/nilbox/internal/github"
	"github.com/mrz1836/nilbox/internal/issues"
	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/prompts"
	"github.com/mrz1836/nilbox/internal/review"
	"github.com/mrz1836/nilbox/internal/textutil"
	"github.com/mrz1836/nilbox/internal/uierr"
)

// githubSteps sends Autopilot's GitHub step through the sheets' own services
// (autopilot.GitHubActions), never the client: a pull request's actions through the review
// sheet, an issue's through the issue sheet, and Mark the GitHub notification done through
// the mirror of the user's GitHub notifications, which clears the item's threads by hand
// (ghthreads.ClearItemThreads). Each read and write says it is Autopilot's
// (github.PurposeAutopilot), and every write goes through the inbox's Actor and the write
// gate, as the sheet's own do. The review sheet's actions it sends are exactly
// reviewActionOf's: asking Dependabot to rebase, updating the branch, and a comment; no
// merge, approval, request for changes, close, or approval of workflow runs, which a guard
// test holds to.
type githubSteps struct {
	review  *review.Service
	issues  *issues.Service
	threads *ghthreads.Service
	prompts *prompts.Store
	replies replyFiller

	// changes reads what agent runs changed on an item, which no Merge it follows into
	// workflow, agent, or tooling files; nil reads nothing.
	changes agentChanges
}

// ErrNotACommentReply is a GitHub step's reply that isn't a comment's: one written for an
// approval or a request for changes, which a GitHub step never posts.
var ErrNotACommentReply = errors.New("a GitHub step comments only a comment's reply")

// errNoReply is a GitHub step's reply the library doesn't have, and errNoGitHubAction an
// action this build doesn't know.
var (
	errNoReply        = errors.New("the reply isn't in the prompt library")
	errNoGitHubAction = errors.New("no such GitHub action")
)

// reviewActionOf is the review sheet's action a GitHub action sends on a pull request: the
// only three a GitHub step may send there. Labels go through the sheet's label edit.
func reviewActionOf(kind catalog.GitHubActionKind) (review.Action, bool) {
	switch kind {
	case catalog.GitHubRebase:
		return review.ActionRebaseBot, true
	case catalog.GitHubUpdateBranch:
		return review.ActionUpdateBranch, true
	case catalog.GitHubReply:
		return review.ActionComment, true
	case catalog.GitHubLabels:
		return review.ActionNone, false
	}

	return review.ActionNone, false
}

// Read reads the item again through its sheet, one point, which rewrites the inbox's card
// from the same answer: the read right before an action starts.
func (g githubSteps) Read(ctx context.Context, it autopilot.ItemRef) error {
	ctx = github.WithPurpose(ctx, github.PurposeAutopilot)

	if it.Kind == item.KindPR {
		_, err := g.review.ReadAgain(ctx, it.ID)

		return err
	}

	_, err := g.issues.ReadAgain(ctx, it.ID)

	return err
}

// Send sends one action through the item's sheet.
func (g githubSteps) Send(ctx context.Context, req autopilot.GitHubSend) (autopilot.GitHubSent, error) {
	ctx = github.WithPurpose(ctx, github.PurposeAutopilot)

	if req.Item.Kind == item.KindPR {
		return g.sendPull(ctx, req)
	}

	return g.sendIssue(ctx, req)
}

// MarkDone clears the item's GitHub notifications through the mirror, by hand
// (ghthreads.ClearItemThreads), and returns how many it queued.
func (g githubSteps) MarkDone(ctx context.Context, it autopilot.ItemRef) (int, error) {
	ref := it.Ref()

	return g.threads.ClearItemThreads(ctx, ref.Repo.Owner, ref.Repo.Repo, ref.Number)
}

// Preview says what an action would send to the item, built and not sent: the comment
// that asks Dependabot to rebase, the branch's update, the reply filled in for the item,
// or the labels it adds and takes off.
func (g githubSteps) Preview(ctx context.Context, it autopilot.ItemRef, a spec.GitHubAction) (string, error) {
	switch {
	case a.Rebase != nil:
		return review.DependabotRebase, nil
	case a.UpdateBranch != nil:
		return "Merges its base branch into its branch, as the review sheet's Update branch does.", nil
	case a.Reply != nil:
		return g.fill(ctx, it.ID, a.Reply.Prompt)
	case a.Labels != nil:
		return labelWords(a.Labels.Add, a.Labels.Remove, true), nil
	}

	return "", fmt.Errorf("%w: %q", errNoGitHubAction, a.Kind())
}

// sendPull sends one action to a pull request through the review sheet: decided again on
// the sheet's read just made (its Refresh serves it, with no request), pinned to req.Head.
func (g githubSteps) sendPull(ctx context.Context, req autopilot.GitHubSend) (autopilot.GitHubSent, error) {
	rev, err := g.review.Refresh(ctx, req.Item.ID)
	if err != nil {
		return unread(err), nil
	}

	if !strings.EqualFold(rev.Head, req.Head) {
		return autopilot.GitHubSent{Outcome: autopilot.GitHubPushedSince}, nil
	}

	if req.Action.Labels != nil {
		return g.pullLabels(ctx, req, rev)
	}

	action, ok := reviewActionOf(req.Action.Kind())
	if !ok {
		return autopilot.GitHubSent{}, fmt.Errorf("%w for a pull request: %q", errNoGitHubAction, req.Action.Kind())
	}

	if sent, decided := pullOffer(rev.Offers, action, req.Accept); decided {
		return sent, nil
	}

	var body string

	if req.Action.Reply != nil {
		if body, err = g.fill(ctx, req.Item.ID, req.Action.Reply.Prompt); err != nil {
			return autopilot.GitHubSent{}, err
		}
	}

	res, err := g.review.Act(ctx, review.ActRequest{
		ItemID: req.Item.ID, Action: action, Head: req.Head, Body: body, Accept: req.Accept != "",
	})

	switch {
	case err != nil:
		return refusal(err, review.ErrNotOffered, review.ErrWarningsNotAccepted)
	case res.Outcome == review.OutcomePushedSince:
		return autopilot.GitHubSent{Outcome: autopilot.GitHubPushedSince}, nil
	default:
		return autopilot.GitHubSent{Outcome: autopilot.GitHubSentIt, Words: res.Message}, nil
	}
}

// pullOffer is what the review sheet's offers say of action: not offered (absent, or not
// enabled now, with the sheet's reason), offered with warnings the user hasn't accepted
// (accept, as the warnings were worded when they did), or decided by nothing, so it goes.
func pullOffer(offers []review.Offer, action review.Action, accept string) (autopilot.GitHubSent, bool) {
	i := slices.IndexFunc(offers, func(o review.Offer) bool { return o.Action == action })

	switch {
	case i < 0:
		return notOffered("The review sheet doesn't offer it for this pull request now."), true
	case !offers[i].Enabled:
		return notOffered(offers[i].Reason), true
	}

	if warnings := strings.Join(offers[i].Warnings, " "); warnings != "" && warnings != accept {
		return autopilot.GitHubSent{Outcome: autopilot.GitHubWarning, Words: warnings}, true
	}

	return autopilot.GitHubSent{}, false
}

// pullLabels adds and takes off a pull request's labels through the review sheet's label
// edit, sending only what changes the labels its read found.
func (g githubSteps) pullLabels(ctx context.Context, req autopilot.GitHubSend, rev review.Review,
) (autopilot.GitHubSent, error) {
	add, remove := labelChanges(*req.Action.Labels, rev.Labels)
	if len(add)+len(remove) == 0 {
		return autopilot.GitHubSent{Outcome: autopilot.GitHubNothing, Words: "Its labels were already so."}, nil
	}

	res, err := g.review.EditLabels(ctx, review.LabelsRequest{ItemID: req.Item.ID, Head: req.Head, Add: add, Remove: remove})

	switch {
	case err != nil:
		return refusal(err, review.ErrCannotEditLabels)
	case res.PushedSince:
		return autopilot.GitHubSent{Outcome: autopilot.GitHubPushedSince}, nil
	default:
		return labelsSent(add, remove, res.Sent, res.Failed), nil
	}
}

// sendIssue sends one action to an issue through the issue sheet: decided again on the
// sheet's read just made, which its Refresh serves with no request.
func (g githubSteps) sendIssue(ctx context.Context, req autopilot.GitHubSend) (autopilot.GitHubSent, error) {
	is, err := g.issues.Refresh(ctx, req.Item.ID)
	if err != nil {
		return unread(err), nil
	}

	switch {
	case req.Action.Labels != nil:
		return g.issueLabels(ctx, req, is)
	case req.Action.Reply == nil:
		return notOffered("Only a pull request has a branch."), nil
	}

	i := slices.IndexFunc(is.Offers, func(o issues.Offer) bool { return o.Action == issues.ActionComment })

	switch {
	case i < 0:
		return notOffered("The issue sheet doesn't offer a comment on it now."), nil
	case !is.Offers[i].Enabled:
		return notOffered(is.Offers[i].Reason), nil
	case is.Offers[i].CommentBlocked != "":
		return notOffered(is.Offers[i].CommentBlocked), nil
	}

	body, err := g.fill(ctx, req.Item.ID, req.Action.Reply.Prompt)
	if err != nil {
		return autopilot.GitHubSent{}, err
	}

	res, err := g.issues.Act(ctx, issues.ActRequest{ItemID: req.Item.ID, Action: issues.ActionComment, Body: body})
	if err != nil {
		return refusal(err, issues.ErrNotOffered)
	}

	return autopilot.GitHubSent{Outcome: autopilot.GitHubSentIt, Words: res.Message}, nil
}

// issueLabels adds and takes off an issue's labels through the issue sheet's edit, sending
// only what changes the labels its read found.
func (g githubSteps) issueLabels(ctx context.Context, req autopilot.GitHubSend, is issues.Issue,
) (autopilot.GitHubSent, error) {
	if !is.CanEdit {
		return notOffered("Your account can't change this issue's labels: that takes triage access."), nil
	}

	have := make([]string, 0, len(is.Labels))
	for _, l := range is.Labels {
		have = append(have, l.Name)
	}

	add, remove := labelChanges(*req.Action.Labels, have)
	if len(add)+len(remove) == 0 {
		return autopilot.GitHubSent{Outcome: autopilot.GitHubNothing, Words: "Its labels were already so."}, nil
	}

	res, err := g.issues.Edit(ctx, issues.EditRequest{ItemID: req.Item.ID, AddLabels: add, RemoveLabels: remove})
	if err != nil {
		return refusal(err, issues.ErrCannotEdit)
	}

	return labelsSent(add, remove, res.Sent, res.Failed), nil
}

// labelChanges are the labels to add that the item doesn't have, and those to take off
// that it has, ignoring case, so an action sends nothing the labels are already.
func labelChanges(l spec.LabelsAction, have []string) ([]string, []string) {
	has := func(name string) bool { return textutil.ContainsFold(have, name) }

	add := slices.DeleteFunc(slices.Clone(l.Add), has)
	remove := slices.DeleteFunc(slices.Clone(l.Remove), func(name string) bool { return !has(name) })

	return add, remove
}

// labelsSent says what a label edit GitHub made did: every change, or the ones it made
// before one failed, and why.
func labelsSent(add, remove []string, sent int, failed string) autopilot.GitHubSent {
	words := labelWords(add, remove, false)
	if failed != "" {
		words = fmt.Sprintf("%s: %d of its changes went, then: %s", words, sent, failed)
	}

	return autopilot.GitHubSent{Outcome: autopilot.GitHubSentIt, Words: words}
}

// labelWords says what a label edit did, "Added “needs-repro”; took off “bug”.", or, ahead,
// what it does: "Adds “needs-repro”; takes off “bug”."
func labelWords(add, remove []string, ahead bool) string {
	quote := func(names []string) string {
		out := make([]string, 0, len(names))
		for _, n := range names {
			out = append(out, "“"+n+"”")
		}

		return strings.Join(out, ", ")
	}

	adds, takes := "added ", "took off "
	if ahead {
		adds, takes = "adds ", "takes off "
	}

	var parts []string

	if len(add) > 0 {
		parts = append(parts, adds+quote(add))
	}

	if len(remove) > 0 {
		parts = append(parts, takes+quote(remove))
	}

	if len(parts) == 0 {
		return "It changes no label."
	}

	out := strings.Join(parts, "; ")

	return strings.ToUpper(out[:1]) + out[1:] + "."
}

// refusal is how a sheet's refusal of an action says it went: one the write gate refused
// before anything was sent waits until it says the next may go; one of the sheet's own
// refusals (notOfferedErrs, such as an action no longer offered, or labels the account
// can't change) is no longer offered, in the sheet's words; and any other is an error,
// after which GitHub may have taken it.
func refusal(err error, notOfferedErrs ...error) (autopilot.GitHubSent, error) {
	if wait := budget.RetryIn(err); wait > 0 || errors.Is(err, budget.ErrWritePace) ||
		errors.Is(err, budget.ErrWritesHeld) || errors.Is(err, budget.ErrBudgetReserved) {
		return autopilot.GitHubSent{Outcome: autopilot.GitHubHeld, Words: uierr.Message(err), RetryIn: wait}, nil
	}

	for _, target := range notOfferedErrs {
		if errors.Is(err, target) {
			return notOffered(uierr.Message(err)), nil
		}
	}

	return autopilot.GitHubSent{}, err
}

// unread is an action whose sheet couldn't read the item before it, so nothing was sent:
// it is read and decided again, as one pushed to since is.
func unread(err error) autopilot.GitHubSent {
	return autopilot.GitHubSent{
		Outcome: autopilot.GitHubUnread, Words: "It couldn't be read from GitHub right before it went, so nothing was " +
			"sent: " + uierr.Message(err),
	}
}

// notOffered is an action its sheet doesn't offer now, saying why.
func notOffered(why string) autopilot.GitHubSent {
	return autopilot.GitHubSent{Outcome: autopilot.GitHubNotOffered, Words: why}
}

// fill fills in the comment reply ref names for the item, as the sheets fill one: the
// reply must be in the library and a comment's, never an approval's or a request for
// changes', since a GitHub step only comments.
func (g githubSteps) fill(ctx context.Context, itemID int64, ref spec.PromptRef) (string, error) {
	p, err := g.reply(ctx, ref)
	if err != nil {
		return "", err
	}

	return g.replies.fill(ctx, p.ID, itemID)
}

// reply returns the library's comment reply ref names: the user's copy of a starter, or
// the one with the nickname, ignoring case.
func (g githubSteps) reply(ctx context.Context, ref spec.PromptRef) (prompts.Prompt, error) {
	all, err := g.prompts.All(ctx)
	if err != nil {
		return prompts.Prompt{}, err
	}

	i := slices.IndexFunc(all, func(p prompts.Prompt) bool {
		return (ref.Starter != "" && p.StarterKey == ref.Starter) ||
			(ref.Starter == "" && ref.Nickname != "" && strings.EqualFold(p.Nickname, ref.Nickname))
	})

	switch {
	case i < 0:
		return prompts.Prompt{}, uierr.New("Its reply isn't in your prompt library any more, so nothing was sent.",
			errNoReply)
	case !all[i].IsReply() || all[i].ReplyAction != prompts.ReplyComment:
		return prompts.Prompt{}, uierr.New(fmt.Sprintf("“%s” isn't a comment's reply, and a GitHub step only "+
			"comments, so nothing was sent.", all[i].Nickname), fmt.Errorf("%w: %q", ErrNotACommentReply, all[i].Nickname))
	}

	return all[i], nil
}
