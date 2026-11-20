package issues

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/mrz1836/nilbox/internal/github"
	"github.com/mrz1836/nilbox/internal/uierr"
)

// ErrCannotEdit is returned for an edit of an issue's labels or assignees the user can't
// make: their account has less than triage access, the sheet hasn't read the issue or read
// it closed, the edit takes the user off it, which is Unassign me, or it would leave more
// people assigned than GitHub allows (github.MaxAssignees).
var ErrCannotEdit = errors.New("the issue's labels or assignees can't be changed that way")

// The keys of an edit's assignee steps.
const (
	stepUnassign = "-assignee"
	stepAssign   = "+assignee"
)

// Edit changes the labels and assignees of the issue with req.ItemID as req asks, with one
// REST request for the labels added, one for each label taken off, one for everyone
// unassigned, then one for everyone assigned, through the inbox's Actor (the item's lock, the
// account check, and a token borrowed again once when GitHub refuses one, which never sends
// a step twice). Unassigning first lets a swap work at GitHub's cap of ten. GitHub ignores a
// change an account can't make rather than refusing it, so every answer is checked. It stops
// at the first step that fails, and those before it stand: once GitHub made any, the result
// counts them and says why the next failed, while a failure before any is the error. A name
// given twice is sent once, and one both added and taken off not at all. The sheet's copy of
// the issue is marked out of date, for the read the sheet makes next, and each person
// assigned is kept in the People directory as assigned in the repository.
func (s *Service) Edit(ctx context.Context, req EditRequest) (EditResult, error) {
	card, c, err := s.load(ctx, req.ItemID)
	if err != nil {
		return EditResult{}, err
	}

	if !github.CanTriage(card.ViewerPermission) {
		return EditResult{}, uierr.New(
			"Your account can't change this issue's labels or assignees: that takes triage access.",
			fmt.Errorf("%w: permission %q", ErrCannotEdit, card.ViewerPermission),
		)
	}

	if !canEdit(card, c) {
		return EditResult{}, uierr.New("Read the issue from GitHub before changing its labels or assignees, "+
			"and only while it is open.", fmt.Errorf("%w: not read, or closed", ErrCannotEdit))
	}

	e := planEdit(req)

	if slices.ContainsFunc(e.unassign, func(login string) bool { return isViewer(login, card.Viewer) }) {
		return EditResult{}, uierr.New("Take yourself off the issue with Unassign me.",
			fmt.Errorf("%w: unassigning the viewer", ErrCannotEdit))
	}

	if n := assignedAfter(c.view.Assignees, e.assign, e.unassign); n > github.MaxAssignees {
		return EditResult{}, uierr.New(fmt.Sprintf("GitHub assigns an issue to %d people at most. Unassign someone first.",
			github.MaxAssignees), fmt.Errorf("%w: %d assigned", ErrCannotEdit, n))
	}

	if e.empty() {
		return EditResult{}, nil
	}

	err = s.inbox.Act(ctx, req.ItemID, func(ctx context.Context, tok github.Token, t Target) (Acted, error) {
		editErr := e.run(ctx, s.github, tok, t)
		s.settle(ctx, req.ItemID, false)

		return Acted{}, editErr
	})

	if e.done[stepAssign] {
		s.finder.Seen(ctx, card.ref().Repo, e.assign)
	}

	sent := len(e.done)

	switch {
	case err == nil:
		return EditResult{Sent: sent}, nil
	case sent == 0:
		return EditResult{}, err
	default:
		return EditResult{Sent: sent, Failed: uierr.Message(err)}, nil
	}
}

// edit is an edit of an issue's labels and assignees as it will be sent, and the steps
// GitHub has made of it, by key, which a retry with a fresh token never sends again.
type edit struct {
	label, unlabel, assign, unassign []string

	done map[string]bool
}

// editStep is one request of an edit: its key, and how to send it.
type editStep struct {
	key  string
	send func(ctx context.Context, tok github.Token) error
}

// planEdit is req's edit, each list trimmed and without a name given twice, ignoring case,
// and a label or a person both added and taken off left out of both.
func planEdit(req EditRequest) *edit {
	label, unlabel := apart(names(req.AddLabels), names(req.RemoveLabels))
	assign, unassign := apart(names(req.AddAssignees), names(req.RemoveAssignees))

	return &edit{label: label, unlabel: unlabel, assign: assign, unassign: unassign, done: map[string]bool{}}
}

// apart is add and remove without the names, ignoring case, that both hold.
func apart(add, remove []string) ([]string, []string) {
	in := func(list []string) func(string) bool {
		return func(name string) bool {
			return slices.ContainsFunc(list, func(n string) bool { return strings.EqualFold(n, name) })
		}
	}

	return slices.DeleteFunc(slices.Clone(add), in(remove)), slices.DeleteFunc(slices.Clone(remove), in(add))
}

// names is list trimmed, without the empty names, and with each name once, ignoring case.
func names(list []string) []string {
	out := []string{}

	for _, n := range list {
		n = strings.TrimSpace(n)
		if n != "" && !slices.ContainsFunc(out, func(o string) bool { return strings.EqualFold(o, n) }) {
			out = append(out, n)
		}
	}

	return out
}

// assignedAfter counts the people an issue assigned to now would be assigned to once it
// unassigns unassign and assigns assign, ignoring case.
func assignedAfter(now, assign, unassign []string) int {
	after, _ := apart(names(now), unassign)
	extra, _ := apart(assign, after)

	return len(after) + len(extra)
}

// empty reports an edit with nothing to send.
func (e *edit) empty() bool {
	return len(e.label)+len(e.unlabel)+len(e.assign)+len(e.unassign) == 0
}

// run sends each step GitHub hasn't made yet, in order, and stops at the first that fails.
func (e *edit) run(ctx context.Context, gh GitHub, tok github.Token, t Target) error {
	for _, s := range e.steps(gh, t) {
		if e.done[s.key] {
			continue
		}

		if err := s.send(ctx, tok); err != nil {
			return err
		}

		e.done[s.key] = true
	}

	return nil
}

// steps are the edit's requests to the issue t, in order: the labels added, each label
// taken off, everyone unassigned, then everyone assigned.
func (e *edit) steps(gh GitHub, t Target) []editStep {
	var out []editStep

	if len(e.label) > 0 {
		out = append(out, editStep{key: "+label", send: func(ctx context.Context, tok github.Token) error {
			_, err := gh.AddLabels(ctx, tok, t.Ref, e.label)

			return err
		}})
	}

	for _, name := range e.unlabel {
		out = append(out, editStep{key: "-label:" + name, send: func(ctx context.Context, tok github.Token) error {
			_, err := gh.RemoveLabel(ctx, tok, t.Ref, name)

			return err
		}})
	}

	if len(e.unassign) > 0 {
		out = append(out, editStep{key: stepUnassign, send: func(ctx context.Context, tok github.Token) error {
			_, err := gh.RemoveAssignees(ctx, tok, t.Ref, e.unassign)

			return err
		}})
	}

	if len(e.assign) > 0 {
		out = append(out, editStep{key: stepAssign, send: func(ctx context.Context, tok github.Token) error {
			_, err := gh.AddAssignees(ctx, tok, t.Ref, e.assign)

			return err
		}})
	}

	return out
}
