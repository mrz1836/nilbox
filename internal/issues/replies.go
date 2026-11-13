package issues

import (
	"context"

	"github.com/mrz1836/nilbox/internal/textutil"
)

// Replies returns the replies the composer offers: the prompt library's comment replies
// that apply to issues. None is a default, so the sheet never fills in text the user
// didn't pick.
func (s *Service) Replies(ctx context.Context) ([]Reply, error) {
	list, err := s.replies.Offered(ctx)
	if err != nil {
		return []Reply{}, err
	}

	return textutil.NonNil(list), nil
}

// FillReply fills in the reply with replyID for the issue with itemID, as the text the
// comment starts with.
func (s *Service) FillReply(ctx context.Context, itemID, replyID int64) (string, error) {
	if _, err := s.issue(ctx, itemID); err != nil {
		return "", err
	}

	return s.replies.Fill(ctx, replyID, itemID)
}
