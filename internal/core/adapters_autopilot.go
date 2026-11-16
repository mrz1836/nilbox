package core

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"slices"
	"strings"
	"time"

	"github.com/mrz1836/nilbox/internal/autopilot"
	"github.com/mrz1836/nilbox/internal/autopilot/catalog"
	"github.com/mrz1836/nilbox/internal/autopilot/facts"
	"github.com/mrz1836/nilbox/internal/autopilotprefs"
	"github.com/mrz1836/nilbox/internal/budget"
	"github.com/mrz1836/nilbox/internal/clock"
	"github.com/mrz1836/nilbox/internal/github"
	"github.com/mrz1836/nilbox/internal/inbox"
	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/periodic"
	prefsstore "github.com/mrz1836/nilbox/internal/prefs"
	"github.com/mrz1836/nilbox/internal/prompts"
)

// autopilotDeps are what Autopilot's service is built over: the inbox's store, whose last
// refresh says the account signed in, and the inbox service, which the builder reads
// through; the prompt library, which names the starters; the copy of the routines kept
// beside the database; and what a refresh's searches cost, for a test's estimate.
type autopilotDeps struct {
	inbox   *inbox.Store
	service *inbox.Routines
	prompts *prompts.Store
	file    *autopilot.File
	costs   autopilot.Costs
}

// newAutopilot builds Autopilot's service over pool and deps.
func newAutopilot(pool *sql.DB, opts Options, deps autopilotDeps, logger *slog.Logger) *autopilot.Service {
	return autopilot.NewService(autopilot.Config{
		Pool: pool, Prefs: autopilotPrefs{prefs: prefsstore.New(pool, opts.Clock)}, Accounts: deps.inbox,
		Names: starterNames{prompts: deps.prompts}, File: deps.file, Clock: opts.Clock, Logger: logger,
		Items: autopilotItems{store: deps.inbox, inbox: deps.service}, Costs: deps.costs,
	})
}

// autopilotWatches gives the inbox the routines' searches (inbox.WatchSource): the
// snapshot Autopilot's service keeps, built again first when the account signed in
// changed since. Core fills routines in once Autopilot's service is built, before the
// scheduler starts.
type autopilotWatches struct {
	searches *autopilot.Searches
}

// Watches returns the routines' searches for the account login.
func (w *autopilotWatches) Watches(login string) []inbox.Watch {
	if w.searches == nil {
		return nil
	}

	set := w.searches.Current()
	if !strings.EqualFold(set.Login, login) {
		set = w.searches.Rebuild(context.Background())
	}

	out := make([]inbox.Watch, 0, len(set.Searches))
	for _, s := range set.Searches {
		out = append(out, inbox.Watch{Key: s.Key, Query: s.Query, Follow: s.Follow})
	}

	return out
}

// autopilotItems reads the inbox for Autopilot's builder (autopilot.Items).
type autopilotItems struct {
	store *inbox.Store
	inbox *inbox.Routines
}

// Suggestions returns what the stored items hold that the builder suggests.
func (a autopilotItems) Suggestions(ctx context.Context) (autopilot.Suggestions, error) {
	s, err := a.store.Suggest(ctx)

	return autopilot.Suggestions{Authors: s.Authors, Bots: s.Bots, Owners: s.Owners, Labels: s.Labels, Checks: s.Checks}, err
}

// WatchedItems returns the stored items the lists name or the searches by key return.
func (a autopilotItems) WatchedItems(ctx context.Context, keys []string, lists []catalog.ListSource) ([]int64, error) {
	return a.store.WatchedItems(ctx, keys, inbox.Lists{
		ReviewRequested: slices.Contains(lists, catalog.SourceReviewRequested),
		Authored:        slices.Contains(lists, catalog.SourceAuthored),
		Assigned:        slices.Contains(lists, catalog.SourceAssigned),
	})
}

// Facts returns the facts of the stored items among ids, as of now.
func (a autopilotItems) Facts(ctx context.Context, ids []int64, now time.Time) ([]facts.Facts, error) {
	rows, err := a.store.FactsRows(ctx, ids)
	if err != nil {
		return nil, err
	}

	env, err := a.store.FactsEnv(ctx, now)
	if err != nil {
		return nil, err
	}

	out := make([]facts.Facts, 0, len(rows))
	for _, r := range rows {
		out = append(out, facts.Build(r, env))
	}

	return out, nil
}

// KnownOwners returns every owner of a repository the inbox has seen, in lower case.
func (a autopilotItems) KnownOwners(ctx context.Context) (map[string]bool, error) {
	return a.store.KnownOwners(ctx)
}

// SearchNow reads queries from GitHub now, storing what they return, or sends nothing
// while the GraphQL budget is low (autopilot.ErrLowBudget).
func (a autopilotItems) SearchNow(ctx context.Context, queries []string) (map[string]autopilot.SearchResult, int, error) {
	got, points, err := a.inbox.SearchNow(ctx, queries)

	switch {
	case errors.Is(err, inbox.ErrLowBudget):
		return nil, 0, fmt.Errorf("%w: %w", autopilot.ErrLowBudget, err)
	case err != nil:
		return nil, 0, err
	}

	out := make(map[string]autopilot.SearchResult, len(got))
	for q, o := range got {
		out[q] = autopilot.SearchResult{IDs: o.IDs, Count: o.Count, Truncated: o.Truncated, Err: o.Err}
	}

	return out, points, nil
}

// ReadChecks reads and stores the check names of the pull requests among ids, and
// returns what that cost in GraphQL points.
func (a autopilotItems) ReadChecks(ctx context.Context, ids []int64) (int, error) {
	return a.inbox.ReadChecks(ctx, ids)
}

// watchCosts estimates what routine searches add to a refresh (autopilot.Costs): as many
// requests as the refresh packs them into, each at what the last watch pass cost a
// request (a point before one ran), at the inbox's refresh pace, as a share of the GraphQL
// budget as last read (5,000 before it was).
type watchCosts struct {
	inbox  *inbox.Routines
	every  periodic.Every
	budget *budget.Service
}

// Estimate returns what searches that returned counts results each last add.
func (c watchCosts) Estimate(ctx context.Context, counts []int) (autopilot.Estimate, error) {
	requests := github.WatchRequests(counts)

	perRequest := 1.0
	if pass, ok := c.inbox.LastWatchPass(); ok && pass.Requests > 0 {
		perRequest = float64(pass.Points) / float64(pass.Requests)
	}

	points := int(math.Ceil(float64(requests) * perRequest))

	perHour := 0
	if every := c.every(ctx); every > 0 {
		perHour = points * int(time.Hour/every)
	}

	limit := c.budget.Gauge(ctx).GraphQL.Limit
	if limit <= 0 {
		limit = defaultGraphQLLimit
	}

	return autopilot.Estimate{
		Requests: requests, PointsPerRefresh: points, PointsPerHour: perHour,
		Percent: float64(perHour) * 100 / float64(limit),
	}, nil
}

// defaultGraphQLLimit is GitHub's hourly GraphQL budget for a user's token.
const defaultGraphQLLimit = 5000

// stateNoter is the GitHub client ghthreads reads the state of the user's notifications'
// items through, which tells the inbox each state it read, as the inbox's own reads do.
type stateNoter struct {
	*github.Client

	store  *inbox.Store
	clock  clock.Clock
	logger *slog.Logger
}

// FetchItemStates reads the states of refs, and records them in the inbox as read when the
// request went out.
func (n stateNoter) FetchItemStates(ctx context.Context, tok github.Token, refs []item.Ref,
) (map[string]github.ItemState, error) {
	readAt := clock.Or(n.clock).Now()

	states, err := n.Client.FetchItemStates(ctx, tok, refs)
	if err == nil {
		noteStates(ctx, n.store, readAt, refs, states, n.logger)
	}

	return states, err
}

// noteStates records in the inbox the states another service read, in a read that went
// out at readAt, logging a failure: the inbox's next read records them anyway.
func noteStates(ctx context.Context, store *inbox.Store, readAt time.Time, refs []item.Ref,
	states map[string]github.ItemState, logger *slog.Logger,
) {
	if store == nil {
		return
	}

	if err := store.NoteStates(ctx, readAt, refs, states); err != nil {
		logger.DebugContext(ctx, "could not record the items' states in the inbox", "error", err)
	}
}

// autopilotPrefs reads Autopilot's settings from the prefs, which settings writes
// (autopilot.Prefs).
type autopilotPrefs struct {
	prefs *prefsstore.Store
}

// Autopilot returns Autopilot's settings as stored, each one missing or refused as its
// default.
func (p autopilotPrefs) Autopilot(ctx context.Context) (autopilotprefs.Config, error) {
	stored, err := p.prefs.GetMany(ctx, autopilotprefs.Keys()...)
	if err != nil {
		return autopilotprefs.Config{}, fmt.Errorf("read Autopilot's settings: %w", err)
	}

	return autopilotprefs.Decode(stored), nil
}

// starterNames names each starter prompt by the nickname of the user's copy of it, for a
// routine's sentence (autopilot.Names).
type starterNames struct {
	prompts *prompts.Store
}

// StarterNames returns each starter's key with the nickname of the user's copy of it; a
// starter the user deleted is left out.
func (n starterNames) StarterNames(ctx context.Context) (map[string]string, error) {
	all, err := n.prompts.All(ctx)
	if err != nil {
		return nil, err
	}

	out := map[string]string{}

	for _, p := range all {
		if p.StarterKey != "" {
			out[p.StarterKey] = p.Nickname
		}
	}

	return out, nil
}
