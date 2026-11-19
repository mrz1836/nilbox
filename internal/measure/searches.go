package measure

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/mrz1836/nilbox/internal/autopilot"
	"github.com/mrz1836/nilbox/internal/autopilot/catalog"
	"github.com/mrz1836/nilbox/internal/autopilot/spec"
	"github.com/mrz1836/nilbox/internal/autopilot/watch"
	"github.com/mrz1836/nilbox/internal/core"
	"github.com/mrz1836/nilbox/internal/inbox"
	"github.com/mrz1836/nilbox/internal/item"
)

// SearchesFlag sets how many routine searches RefreshFlag's refreshes read beside the
// inbox's own, as --measure-searches=5.
const SearchesFlag = "--measure-searches="

// MaxSearches is the most routine searches a measurement reads: one for each of
// typicalSearchesList.
const MaxSearches = 5

// passTimeout bounds the wait for one refresh's watch pass.
const passTimeout = 2 * time.Minute

// ErrNoAccount is returned when the warm-up refresh signed no account in, so no search
// over the account can be built.
var ErrNoAccount = errors.New("the warm-up refresh signed no account in")

// typicalSearchesList are the searches a measurement's routines watch, each over the
// signed-in account, as routines commonly do: Dependabot's pull requests, those ready for
// review, the issues, those whose checks fail, and those whose checks run.
var typicalSearchesList = []watch.Search{ //nolint:gochecknoglobals // the measurement's fixed searches
	{Kind: item.KindPR, Authors: []string{"app/dependabot"}, Where: []string{watch.Me}},
	{Kind: item.KindPR, Where: []string{watch.Me}, State: []watch.State{watch.StateReady}},
	{Kind: item.KindIssue, Where: []string{watch.Me}},
	{Kind: item.KindPR, Where: []string{watch.Me}, State: []watch.State{watch.StateFailing}},
	{Kind: item.KindPR, Where: []string{watch.Me}, State: []watch.State{watch.StatePending}},
}

// PassTime is what one refresh's watch pass did, as the measurement reads it.
type PassTime struct {
	// After is how long it ran after the inbox was stored, and Took how long in all.
	After time.Duration
	Took  time.Duration

	// Requests and Points are what its searches and details cost.
	Requests int
	Points   int

	// Items is how many results its searches returned, and Read how many of them it read
	// the details of.
	Items int
	Read  int
}

// timeWithSearches makes a warm-up refresh, so the account is known, adds a routine for
// each of the first n typical searches, then times runs refreshes, each with its watch
// pass, run as the desktop shell runs it.
func timeWithSearches(ctx context.Context, c *core.Core, runs, n int) (RefreshReport, error) {
	passCtx, cancel := context.WithCancel(ctx)

	var wg sync.WaitGroup

	wg.Go(func() { c.WatchPasses.Run(passCtx) })

	defer func() {
		cancel()
		wg.Wait()
	}()

	warm, err := c.Inbox.Refresh(ctx)
	if err != nil {
		return RefreshReport{}, fmt.Errorf("the warm-up refresh: %w", err)
	}

	if warm.Account.Login == "" {
		return RefreshReport{}, ErrNoAccount
	}

	if err = addRoutines(ctx, c.Autopilot, n); err != nil {
		return RefreshReport{}, err
	}

	report := RefreshReport{Runs: make([]time.Duration, 0, runs), Searches: n, Passes: make([]PassTime, 0, runs)}

	for i := range runs {
		run, pass, runErr := timeOneWithSearches(ctx, c)
		if runErr != nil {
			return report, fmt.Errorf("refresh %d of %d: %w", i+1, runs, runErr)
		}

		report.Runs = append(report.Runs, run.took)
		report.Passes = append(report.Passes, pass)
		report.Cost = max(report.Cost, run.cost)
		report.Limit = run.limit
	}

	return report, nil
}

// timedRun is one timed refresh: how long it took to store the inbox, and what it cost.
type timedRun struct {
	took  time.Duration
	cost  int
	limit int
}

// timeOneWithSearches times one refresh to the inbox's store, then waits for its watch
// pass.
func timeOneWithSearches(ctx context.Context, c *core.Core) (timedRun, PassTime, error) {
	start := time.Now()

	snap, err := c.Inbox.Refresh(ctx)
	if err != nil {
		return timedRun{}, PassTime{}, err
	}

	run := timedRun{took: time.Since(start), cost: snap.Rate.GraphQL.Cost, limit: snap.Rate.GraphQL.Limit}

	if !snap.Rate.GraphQL.Known {
		return run, PassTime{}, ErrNoGraphQLReading
	}

	waitCtx, cancel := context.WithTimeout(ctx, passTimeout)
	defer cancel()

	pass, err := c.InboxRoutines.AwaitPass(waitCtx, start)
	if err != nil {
		return run, PassTime{}, fmt.Errorf("wait for the watch pass: %w", err)
	}

	return run, passTime(pass), nil
}

// passTime converts what a watch pass did.
func passTime(p inbox.WatchPass) PassTime {
	return PassTime{After: p.After, Took: p.Took, Requests: p.Requests, Points: p.Points, Items: p.Items, Read: p.Read}
}

// addRoutines saves and switches on a routine for each of the first n typical searches,
// each notifying when its pull requests' checks fail, so their searches follow the checks;
// the issues' routine, which Checks fail never fires for, notifies when one appears.
func addRoutines(ctx context.Context, routines *autopilot.Service, n int) error {
	for i, search := range typicalSearchesList[:n] {
		event := catalog.EventChecksFailed
		if search.Kind == item.KindIssue {
			event = catalog.EventAppears
		}

		s := spec.Spec{
			Version: spec.Version,
			Watch:   spec.Watch{Sources: []spec.Source{{Search: &search}}},
			Trigger: spec.Trigger{Event: event},
			Steps:   []spec.Step{{Notify: &spec.NotifyStep{}}},
		}

		saved, err := routines.Save(ctx, autopilot.Draft{Name: fmt.Sprintf("Measure %d", i+1), Spec: s})
		if err != nil {
			return fmt.Errorf("add the routine for search %d: %w", i+1, err)
		}

		if _, err = routines.SetEnabled(ctx, saved.ID, true, autopilot.CurrentRun); err != nil {
			return fmt.Errorf("switch on the routine for search %d: %w", i+1, err)
		}
	}

	return nil
}

// passLines renders the watch passes: each one's time after the inbox's store and its
// cost, the steady state's median (every pass but the first, which reads the details of
// every item new to the searches), and the budget with them.
func (r RefreshReport) passLines() []string {
	if r.Searches == 0 || len(r.Passes) == 0 {
		return nil
	}

	lines := []string{fmt.Sprintf("watch.searches=%d", r.Searches)}

	for i, p := range r.Passes {
		lines = append(lines, fmt.Sprintf("watch.pass=%d after_ms=%s took_ms=%s requests=%d points=%d items=%d read=%d",
			i+1, milliseconds(p.After), milliseconds(p.Took), p.Requests, p.Points, p.Items, p.Read))
	}

	steady := r.steadyPasses()

	return append(lines,
		fmt.Sprintf("watch.steady_after_median_ms=%s", milliseconds(Median(durations(steady, PassTime.after)))),
		fmt.Sprintf("watch.steady_points=%d", maxPoints(steady)),
		fmt.Sprintf("refresh.total_cost=%d (inbox %d + watches %d)", r.Cost+maxPoints(steady), r.Cost, maxPoints(steady)),
		fmt.Sprintf("refresh.total_budget_pct=%.2f", float64((r.Cost+maxPoints(steady))*RefreshesPerHour)/
			float64(r.HourlyBudget())*percent),
	)
}

// steadyPasses returns the passes after the first, or the first alone when there is one.
func (r RefreshReport) steadyPasses() []PassTime {
	if len(r.Passes) < 2 {
		return r.Passes
	}

	return r.Passes[1:]
}

// after returns the pass's time after the inbox's store.
func (p PassTime) after() time.Duration { return p.After }

// durations maps passes to one of their times.
func durations(passes []PassTime, of func(PassTime) time.Duration) []time.Duration {
	out := make([]time.Duration, 0, len(passes))
	for _, p := range passes {
		out = append(out, of(p))
	}

	return out
}

// maxPoints is the most points one of passes cost.
func maxPoints(passes []PassTime) int {
	points := make([]int, 0, len(passes))
	for _, p := range passes {
		points = append(points, p.Points)
	}

	if len(points) == 0 {
		return 0
	}

	return slices.Max(points)
}

// SearchesArg reads SearchesFlag's count from args, 0 when it is missing or not a count.
func SearchesArg(args []string) int {
	for _, a := range args {
		if v, ok := strings.CutPrefix(a, SearchesFlag); ok {
			var n int
			if _, err := fmt.Sscanf(v, "%d", &n); err == nil && n > 0 {
				return n
			}
		}
	}

	return 0
}
