package review

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/mrz1836/nilbox/internal/assignees"
	"github.com/mrz1836/nilbox/internal/github"
	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/textutil"
	"github.com/mrz1836/nilbox/internal/timeline"
	"github.com/mrz1836/nilbox/internal/uierr"
)

// The sheet manages a pull request's assignees as the issue sheet does an issue's: it reads
// them with the pull request, offers people to assign from the People directory through the
// assignee Finder both sheets share, with no request, and assigns and unassigns with at
// most two REST requests, whose answers say who the pull request is assigned to after, which
// the sheet keeps, so it rebuilds with no read.

// ErrCannotEditAssignees is returned for an assignee edit of a pull request the user can't
// make: it isn't open, the sheet hasn't read who it is assigned to, or their account has
// less than triage access.
var ErrCannotEditAssignees = errors.New("the pull request's assignees can't be changed by this account")

// ErrOwnAssignment is returned for an assignee edit that takes the user off, which they do
// with Unassign me.
var ErrOwnAssignment = errors.New("an assignee edit takes the signed-in user off")

// ErrTooManyAssignees is returned for an assignee edit that would leave more people
// assigned than GitHub allows (github.MaxAssignees).
var ErrTooManyAssignees = errors.New("an assignee edit assigns more people than GitHub allows")

// AssigneesRequest asks to change who the pull request with ItemID is assigned to: the
// people to assign (Add) and to unassign (Remove), by login.
type AssigneesRequest struct {
	ItemID int64    `json:"itemId"`
	Add    []string `json:"add"`
	Remove []string `json:"remove"`
}

// AssigneesResult is what an assignee edit made of a pull request on GitHub: Sent counts the
// requests GitHub made, and Failed says why the one after them failed, or "" when every one
// went.
type AssigneesResult struct {
	Sent   int    `json:"sent"`
	Failed string `json:"failed"`
}

// AssigneeCandidates returns whom the picker offers to assign to the pull request with id,
// from the sheet's cache and the People directory alone, with no request.
func (s *Service) AssigneeCandidates(ctx context.Context, id int64) (assignees.Candidates, error) {
	it, err := s.assigneeItem(ctx, id)
	if err != nil {
		return assignees.Candidates{People: []assignees.Candidate{}}, err
	}

	return s.finder.Candidates(ctx, it)
}

// LearnAssignees reads who can be assigned in the pull request's repository, when a read is
// due (Candidates.Stale), and returns AssigneeCandidates with what it learned: one GraphQL
// request of one point, at most weekly per repository, whichever sheet asks.
func (s *Service) LearnAssignees(ctx context.Context, id int64) (assignees.Candidates, error) {
	it, err := s.assigneeItem(ctx, id)
	if err != nil {
		return assignees.Candidates{People: []assignees.Candidate{}}, err
	}

	return s.finder.Learn(ctx, it, s.readAssignable(id))
}

// SearchAssignees returns whom the picker offers to assign to the pull request with id whose
// login or name matches query: from what the app holds when that answers the search, else
// after one GraphQL request of one point.
func (s *Service) SearchAssignees(ctx context.Context, id int64, query string) (assignees.Candidates, error) {
	it, err := s.assigneeItem(ctx, id)
	if err != nil {
		return assignees.Candidates{People: []assignees.Candidate{}}, err
	}

	return s.finder.Search(ctx, it, query, s.readAssignable(id))
}

// assigneeItem is the pull request with id as the assignee picker sees it, from the inbox
// and the sheet's cache.
func (s *Service) assigneeItem(ctx context.Context, id int64) (assignees.Item, error) {
	pull, err := s.pull(ctx, id)
	if err != nil {
		return assignees.Item{}, err
	}

	c, err := s.cache.load(ctx, id, 0)
	if err != nil {
		return assignees.Item{}, err
	}

	it := assignees.Item{
		Repo: pull.ref().Repo, Viewer: pull.Viewer, Author: pull.AuthorLogin, AuthorIsPerson: pull.AuthorType == typeUser,
	}

	if c != nil {
		it.Participants = participants(c.context)
	}

	return it, nil
}

// participants are the people who took part in the pull request, as pc read it: its
// reviewers, then whoever wrote what its conversation shows, bots left out.
func participants(pc pullContext) []string {
	var out []string

	for _, r := range pc.Reviews {
		if !r.Bot && r.Author != "" {
			out = append(out, r.Author)
		}
	}

	for _, e := range pc.Timeline {
		if (e.Kind == timeline.KindComment || e.Kind == timeline.KindReviewed) && e.Actor != "" && !e.ActorIsBot {
			out = append(out, e.Actor)
		}
	}

	return out
}

// readAssignable is the read of who can be assigned in the repository of the pull request
// with id, through the inbox's footing, which stores nothing.
func (s *Service) readAssignable(id int64) assignees.Read {
	return func(ctx context.Context, query string) (assignees.Found, error) {
		var got *github.AssignableUsers

		err := s.inbox.Peek(ctx, id, func(ctx context.Context, tok github.Token, t Target) error {
			read, readErr := s.github.FetchAssignable(ctx, tok, item.KindPR, t.NodeID, query)
			got = read

			return readErr
		})
		if err != nil {
			return assignees.Found{}, err
		}

		return foundOf(got), nil
	}
}

// foundOf converts what a read of who can be assigned found.
func foundOf(got *github.AssignableUsers) assignees.Found {
	out := assignees.Found{People: make([]assignees.Person, 0, len(got.People)), Total: got.Total}
	for _, p := range got.People {
		out.People = append(out.People, assignees.Person(p))
	}

	return out
}

// canEditAssignees reports whether the sheet may change who pull is assigned to, as c, its
// cache, read it: the sheet read its assignees, it is open, and the user's account has triage
// access or more, as GitHub's roles give the right to assign.
func canEditAssignees(pull StoredPull, c *cached) bool {
	return canEditReviewers(pull, c) && c.context.Assignees != nil
}

// cannotEditAssignees says why the user can't change who pull is assigned to, as c, its
// cache, read it.
func cannotEditAssignees(pull StoredPull, c *cached) error {
	if c != nil && c.context.Assignees == nil && canEditReviewers(pull, c) {
		return uierr.New("Reload the pull request to read who it is assigned to, then try again.",
			fmt.Errorf("%w: assignees of item %d not read", ErrCannotEditAssignees, pull.ID))
	}

	switch {
	case c == nil:
		return uierr.New("Wait for the pull request to load from GitHub, then try again.",
			fmt.Errorf("%w: item %d not read", ErrCannotEditAssignees, pull.ID))
	case !github.CanTriage(pull.ViewerPermission):
		return uierr.New("Your account can't change this pull request's assignees: that takes triage access.",
			fmt.Errorf("%w: permission %q", ErrCannotEditAssignees, pull.ViewerPermission))
	default:
		return uierr.New("This pull request is no longer open, so its assignees can't change.",
			fmt.Errorf("%w: state %q", ErrCannotEditAssignees, c.context.State))
	}
}

// EditAssignees changes who the pull request with req.ItemID is assigned to as req asks:
// one REST request takes off everyone unassigned, then one assigns everyone added, through
// the inbox's Actor (the item's lock, the account check, and a token borrowed again once
// when GitHub refuses one, which never sends a step twice). Taking off first lets a swap
// work at GitHub's cap of ten. It needs the sheet to have read who the pull request is
// assigned to, open, and the user's account to have triage access or more. A name given
// twice is sent once, and one both added and taken off not at all; taking the user off
// (Unassign me) and an edit that would leave more than ten assigned are refused, before
// anything is sent. GitHub's answers say who the pull request is assigned to after each
// step, which the sheet keeps in its cache, so it rebuilds with no read; a cache it can't
// rewrite is marked out of date instead. Once GitHub made a step, the result counts it and
// says why the next failed, while a failure before any is the error. Each person assigned
// is kept in the People directory as assigned in the repository. An edit that assigns the
// user tells the inbox so (Acted.SelfAssigned), so the card reads as assigned to them at
// once, and the next refresh tells no arrival of their own assignment.
func (s *Service) EditAssignees(ctx context.Context, req AssigneesRequest) (AssigneesResult, error) {
	pull, err := s.pull(ctx, req.ItemID)
	if err != nil {
		return AssigneesResult{}, err
	}

	c, err := s.cache.load(ctx, req.ItemID, 0)
	if err != nil {
		return AssigneesResult{}, err
	}

	if !canEditAssignees(pull, c) {
		return AssigneesResult{}, cannotEditAssignees(pull, c)
	}

	add, remove := namesApart(namesOnce(req.Add), namesOnce(req.Remove))

	if err = checkAssignees(pull, c.context.Assignees.Logins, add, remove); err != nil {
		return AssigneesResult{}, err
	}

	if len(add)+len(remove) == 0 {
		return AssigneesResult{}, nil
	}

	done := map[string]bool{}

	var assigned *github.Edited

	err = s.inbox.Act(ctx, req.ItemID, func(ctx context.Context, tok github.Token, t Target) (Acted, error) {
		got, sendErr := s.sendAssignees(ctx, tok, t, add, remove, done)
		if got != nil {
			assigned = got
		}

		return selfAssigned(t.Login, add, assigned), sendErr
	})

	if done["+"] {
		s.finder.Seen(ctx, pull.ref().Repo, add)
	}

	switch sent := len(done); {
	case err == nil:
		return AssigneesResult{Sent: sent}, nil
	case sent == 0:
		return AssigneesResult{}, err
	default:
		return AssigneesResult{Sent: sent, Failed: uierr.Message(err)}, nil
	}
}

// checkAssignees refuses an assignee edit of pull, assigned to now, that takes the
// signed-in user off, or would leave more people assigned than GitHub allows.
func checkAssignees(pull StoredPull, now, add, remove []string) error {
	is := func(login string) func(string) bool {
		return func(name string) bool { return login != "" && strings.EqualFold(name, login) }
	}

	if slices.ContainsFunc(remove, is(pull.Viewer)) {
		return uierr.New("Take yourself off with Unassign me, in the card's menu.",
			fmt.Errorf("%w: %s taken off", ErrOwnAssignment, pull.Viewer))
	}

	after := slices.DeleteFunc(slices.Clone(now), func(login string) bool {
		return slices.ContainsFunc(remove, is(login))
	})
	for _, login := range add {
		if !slices.ContainsFunc(after, is(login)) {
			after = append(after, login)
		}
	}

	if len(after) > github.MaxAssignees {
		return uierr.New(fmt.Sprintf("GitHub assigns a pull request to %d people at most. Unassign someone first.",
			github.MaxAssignees), fmt.Errorf("%w: %d", ErrTooManyAssignees, len(after)))
	}

	return nil
}

// selfAssigned is what an assignee edit that assigned add made happen that the inbox
// records: the user (login) assigned, when they were among add and GitHub's answer to the
// step that assigned them (assigned, nil before it made it) has them, so the pull request
// is in the inbox's assigned list at once. Assigning only others records nothing: an
// assignment of the user someone else made still arrives.
func selfAssigned(login string, add []string, assigned *github.Edited) Acted {
	if assigned == nil || login == "" || !textutil.ContainsFold(add, login) ||
		!textutil.ContainsFold(assigned.Assignees, login) {
		return Acted{}
	}

	return Acted{SelfAssigned: true, SelfAssignedAt: assigned.UpdatedAt}
}

// sendAssignees sends each request of an assignee edit GitHub hasn't made yet, by its key in
// done, in order: "-" takes everyone in remove off, then "+" assigns everyone in add. It
// stops at the first that fails. After each, it keeps who the pull request is assigned to
// as GitHub answered, in the sheet's cache, or marks the cache out of date when it can't.
// It returns GitHub's answer to "+" when this call made it, and nil otherwise.
func (s *Service) sendAssignees(
	ctx context.Context, tok github.Token, t Target, add, remove []string, done map[string]bool,
) (*github.Edited, error) {
	var assigned *github.Edited

	steps := []struct {
		key  string
		who  []string
		send func(context.Context, github.Token, item.Ref, []string) (*github.Edited, error)
	}{
		{key: "-", who: remove, send: s.github.RemoveAssignees},
		{key: "+", who: add, send: s.github.AddAssignees},
	}

	for _, step := range steps {
		if done[step.key] || len(step.who) == 0 {
			continue
		}

		edited, err := step.send(ctx, tok, t.Ref, step.who)
		if err != nil {
			return assigned, err
		}

		done[step.key] = true

		if step.key == "+" {
			assigned = edited
		}

		if keepErr := s.cache.setAssignees(ctx, t.ID, edited.Assignees, s.now()); keepErr != nil {
			s.logger.DebugContext(ctx, "could not keep the pull request's assignees", "error", keepErr)
			s.settle(ctx, t.ID, false)
		}
	}

	return assigned, nil
}

// shownAssignees are who the cache c says the pull request is assigned to, as the sheet
// shows them, the user's (viewer's) marked, never nil, and how many others GitHub has; none
// before the sheet read them, or in a cache an older build wrote.
func shownAssignees(c *cached, viewer string) ([]Assignee, int) {
	if c == nil || c.context.Assignees == nil {
		return []Assignee{}, 0
	}

	a := c.context.Assignees
	out := make([]Assignee, 0, len(a.Logins))

	for _, login := range a.Logins {
		out = append(out, Assignee{Login: login, Mine: viewer != "" && strings.EqualFold(login, viewer)})
	}

	return out, max(a.Total-len(out), 0)
}
