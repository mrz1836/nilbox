package inbox

import (
	"context"
	"fmt"
	"time"

	"github.com/mrz1836/nilbox/internal/inbox/internal/queries"
)

// BotCounts are the pull requests bots opened in the inbox lists lately, which suggest
// Autopilot's templates: those Dependabot opened, and those any bot opened in the Review
// requested and Assigned lists.
type BotCounts struct {
	Dependabot int
	Bots       int
}

// BotPullRequests counts the pull requests in the signed-in account's inbox lists opened
// since since, by bots, from what is stored alone. With no one signed in, it counts none.
func (s *Store) BotPullRequests(ctx context.Context, since time.Time) (BotCounts, error) {
	login, err := s.ViewerLogin(ctx)
	if err != nil || login == "" {
		return BotCounts{}, err
	}

	row, err := s.q.CountBotPullRequests(ctx, queries.CountBotPullRequestsParams{Login: login, Since: since.UTC()})
	if err != nil {
		return BotCounts{}, fmt.Errorf("count the bots' pull requests: %w", err)
	}

	return BotCounts{Dependabot: int(row.Dependabot), Bots: int(row.Bots)}, nil
}
