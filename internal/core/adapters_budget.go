package core

import (
	"context"
	"database/sql"
	"time"

	"github.com/mrz1836/nilbox/internal/budget"
	"github.com/mrz1836/nilbox/internal/clock"
	"github.com/mrz1836/nilbox/internal/github"
	"github.com/mrz1836/nilbox/internal/inbox"
	"github.com/mrz1836/nilbox/internal/prefs"
)

// rateReader reads every API budget from GitHub's free rate limit endpoint, with the GitHub
// CLI's token, for the budget service. The readings reach the budget tracker through the
// client.
type rateReader struct {
	tokens github.Tokens
	client *github.Client
}

// ReadRates reads every budget. A token GitHub refuses is borrowed again once.
func (r rateReader) ReadRates(ctx context.Context) error {
	_, err := github.WithToken(ctx, r.tokens, func(tok github.Token) (*github.RateLimits, error) {
		return r.client.FetchRateLimits(ctx, tok)
	})

	return err
}

// keyBudgetState is where the API budget's state is kept between runs. Only the budget
// writes it, through budgetKeeper.
const keyBudgetState = "budget.state"

// budgetKeeper keeps the API budget's state in the prefs table, for the budget's Saver.
type budgetKeeper struct {
	prefs *prefs.Store
}

// newBudgetKeeper returns a budgetKeeper over the prefs in pool, stamped from c.
func newBudgetKeeper(pool *sql.DB, c clock.Clock) budgetKeeper {
	return budgetKeeper{prefs: prefs.New(pool, c)}
}

// Load returns the state kept, or "" when none is.
func (k budgetKeeper) Load(ctx context.Context) (string, error) {
	state, _, err := k.prefs.Get(ctx, keyBudgetState)

	return state, err
}

// Save replaces the state kept.
func (k budgetKeeper) Save(ctx context.Context, state string) error {
	return k.prefs.Set(ctx, keyBudgetState, state)
}

// storedRates converts the budget readings the last refresh stored, for seeding the budget
// tracker.
func storedRates(gauge inbox.RateGauge) []budget.Stored {
	out := make([]budget.Stored, 0, 3)

	for resource, r := range map[string]inbox.Rate{
		github.ResourceGraphQL: gauge.GraphQL, github.ResourceCore: gauge.Core, github.ResourceSearch: gauge.Search,
	} {
		if !r.Known {
			continue
		}

		resetAt, errReset := time.Parse(time.RFC3339, r.ResetAt)
		takenAt, errTaken := time.Parse(time.RFC3339, r.TakenAt)

		if errReset != nil || errTaken != nil {
			continue
		}

		out = append(out, budget.Stored{
			Reading: github.RateReading{
				Resource: resource, Limit: r.Limit, Remaining: r.Remaining, Used: r.Used, ResetAt: resetAt,
			},
			TakenAt: takenAt,
		})
	}

	return out
}
