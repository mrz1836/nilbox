package settings

import (
	"context"
	"errors"
)

// KeyReviewTab is the review sheet's tab the user last chose, stored as one of the
// ReviewTab choices: the window's memory, which a backup never holds.
const KeyReviewTab = "ui.reviewTab"

// ReviewTab is a tab of the review sheet's larger column: the files a pull request changes,
// beneath its description, or its conversation.
type ReviewTab string

// The review sheet's tabs.
const (
	ReviewTabFiles    ReviewTab = "files"
	ReviewTabComments ReviewTab = "comments"
)

// DefaultReviewTab is the tab a fresh database opens the review sheet on.
const DefaultReviewTab = ReviewTabFiles

// ErrInvalidReviewTab is returned for a review sheet tab that is not one of the choices.
var ErrInvalidReviewTab = errors.New("unknown review sheet tab")

// reviewTabs is the review sheet's tab choice, falling back to DefaultReviewTab for a stored
// tab that is unset or no longer a choice.
func reviewTabs() choice[ReviewTab] {
	return choice[ReviewTab]{
		values:   []ReviewTab{ReviewTabFiles, ReviewTabComments},
		fallback: DefaultReviewTab, invalid: ErrInvalidReviewTab,
	}
}

// SetReviewTab stores the review sheet's tab the user chose, ReviewTabFiles or
// ReviewTabComments, which the sheet opens on from then on, and returns the settings with
// it.
func (s *Service) SetReviewTab(ctx context.Context, tab ReviewTab) (Settings, error) {
	if err := reviewTabs().check(tab); err != nil {
		return Settings{}, err
	}

	return s.save(ctx, setting(KeyReviewTab, string(tab)))
}
