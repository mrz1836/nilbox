package review

import (
	"context"
	"errors"
)

// ErrPushedSince is returned for line comments fitted to a head the pull request has moved
// past, or that the sheet hasn't read.
var ErrPushedSince = errors.New("the pull request was pushed to since that head")

// Fitter fits line comments a routine posts to a pull request's cached diff. It is never
// bound: only Autopilot's Post the agent's review, through core, uses it.
type Fitter struct {
	s *Service
}

// NewFitter returns the comment fitter over s's cache.
func NewFitter(s *Service) Fitter {
	return Fitter{s: s}
}

// Fit returns, of comments meant for the pull request itemID at head, those its cached
// diff shows, as Act would take them, and how many it left out: a review a routine posts
// drops a line comment GitHub would refuse rather than fail whole. It reads the cache
// alone; a head the cache isn't at is ErrPushedSince.
func (f Fitter) Fit(ctx context.Context, itemID int64, head string, comments []LineComment,
) ([]LineComment, int, error) {
	kept := make([]LineComment, 0, len(comments))
	if len(comments) == 0 {
		return kept, 0, nil
	}

	req := ActRequest{ItemID: itemID, Head: head, Comments: comments}

	_, c, err := f.s.loadAct(ctx, req)
	if err != nil {
		return kept, 0, err
	}

	if c == nil {
		return kept, 0, ErrPushedSince
	}

	byPath, patches, err := f.s.commentedPatches(ctx, req, c)
	if err != nil {
		return kept, 0, err
	}

	for _, cm := range comments {
		file, ok := byPath[cm.Path]
		patch, has := patches[file.position]

		if checkComment(cm, patch, ok && has) == nil {
			kept = append(kept, cm)
		}
	}

	return kept, len(comments) - len(kept), nil
}
