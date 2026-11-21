package review

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/mrz1836/nilbox/internal/github"
	"github.com/mrz1836/nilbox/internal/textutil"
	"github.com/mrz1836/nilbox/internal/uierr"
)

// ErrCannotEditLabels is returned for a label edit of a pull request the user can't make:
// their account has less than triage access.
var ErrCannotEditLabels = errors.New("the pull request's labels can't be changed by this account")

// LabelsRequest asks to change the labels of the pull request with ItemID, by name: those
// to add and those to take off, at Head, the head commit the asker saw.
type LabelsRequest struct {
	ItemID int64    `json:"itemId"`
	Head   string   `json:"head"`
	Add    []string `json:"add"`
	Remove []string `json:"remove"`
}

// LabelsResult is what a label edit made of a pull request on GitHub: Sent counts the
// requests GitHub made, and Failed says why the one after them failed, or "" when every
// one went. PushedSince reports an edit that sent nothing, because the pull request was
// pushed to since Head.
type LabelsResult struct {
	Sent        int    `json:"sent"`
	Failed      string `json:"failed"`
	PushedSince bool   `json:"pushedSince"`
}

// EditLabels changes the labels of the pull request with req.ItemID as req asks, as the
// issue sheet's label edit changes an issue's, at the same GitHub endpoint: one REST
// request for the labels added and one for each label taken off, through the inbox's Actor
// (the item's lock, the account check, and a token borrowed again once when GitHub refuses
// one, which never sends a step twice). It is pinned to the head the request names, as
// every action of the sheet is, so a pull request pushed to since sends nothing
// (LabelsResult.PushedSince), and it needs the sheet to have read the pull request and the
// user's account to have triage access or more. GitHub ignores a change an account can't
// make rather than refusing it, so every answer is checked. It stops at the first step
// that fails, and those before it stand: once GitHub made any, the result counts them and
// says why the next failed, while a failure before any is the error. A name given twice is
// sent once, and one both added and taken off not at all.
func (s *Service) EditLabels(ctx context.Context, req LabelsRequest) (LabelsResult, error) {
	pull, c, err := s.loadAct(ctx, ActRequest{ItemID: req.ItemID, Head: req.Head})
	if err != nil {
		return LabelsResult{}, err
	}

	if c == nil {
		s.settle(ctx, req.ItemID, false)

		return LabelsResult{PushedSince: true}, nil
	}

	if !github.CanTriage(pull.ViewerPermission) {
		return LabelsResult{}, uierr.New(
			"Your account can't change this pull request's labels: that takes triage access.",
			fmt.Errorf("%w: permission %q", ErrCannotEditLabels, pull.ViewerPermission),
		)
	}

	add, remove := namesApart(namesOnce(req.Add), namesOnce(req.Remove))
	if len(add)+len(remove) == 0 {
		return LabelsResult{}, nil
	}

	done := map[string]bool{}

	err = s.inbox.Act(ctx, req.ItemID, func(ctx context.Context, tok github.Token, t Target) (Acted, error) {
		return Acted{}, sendLabels(ctx, s.github, tok, t, add, remove, done)
	})

	s.settle(ctx, req.ItemID, false)

	switch sent := len(done); {
	case err == nil:
		return LabelsResult{Sent: sent}, nil
	case sent == 0:
		return LabelsResult{}, err
	default:
		return LabelsResult{Sent: sent, Failed: uierr.Message(err)}, nil
	}
}

// sendLabels sends each request of a label edit GitHub hasn't made yet, by its key in done,
// in order: the labels added, then each label taken off. It stops at the first that fails.
func sendLabels(ctx context.Context, gh GitHub, tok github.Token, t Target, add, remove []string, done map[string]bool,
) error {
	if len(add) > 0 && !done["+"] {
		if _, err := gh.AddLabels(ctx, tok, t.Ref, add); err != nil {
			return err
		}

		done["+"] = true
	}

	for _, name := range remove {
		key := "-" + strings.ToLower(name)
		if done[key] {
			continue
		}

		if _, err := gh.RemoveLabel(ctx, tok, t.Ref, name); err != nil {
			return err
		}

		done[key] = true
	}

	return nil
}

// namesApart is add and remove without the names, ignoring case, that both hold: the
// labels, people, or teams an edit both adds and takes off, which it sends not at all.
func namesApart(add, remove []string) ([]string, []string) {
	return textutil.ApartFold(add, remove), textutil.ApartFold(remove, add)
}

// namesOnce is list trimmed, without the empty names, and with each name once, ignoring
// case: the labels, logins, or team slugs an edit names.
func namesOnce(list []string) []string {
	return textutil.UniqueFold(list, strings.TrimSpace)
}
