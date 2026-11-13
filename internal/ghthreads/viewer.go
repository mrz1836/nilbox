package ghthreads

import (
	"context"
	"fmt"

	"github.com/mrz1836/nilbox/internal/github"
)

// viewer returns the login tok belongs to: the one remembered for it, or else GitHub's
// answer (one GraphQL point), remembered for the next pass.
func (s *Service) viewer(ctx context.Context, tok github.Token) (string, error) {
	if login, ok := s.viewers.Get(tok); ok {
		return login, nil
	}

	login, err := s.client.FetchViewer(ctx, tok)
	if err != nil {
		return "", fmt.Errorf("check the signed-in account: %w", err)
	}

	s.viewers.Remember(tok, login)

	return login, nil
}
