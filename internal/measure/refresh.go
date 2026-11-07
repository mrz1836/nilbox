package measure

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/mrz1836/nilbox/internal/appdata"
	"github.com/mrz1836/nilbox/internal/brand"
	"github.com/mrz1836/nilbox/internal/core"
	"github.com/mrz1836/nilbox/internal/execpath"
	"github.com/mrz1836/nilbox/internal/inbox"
)

// RefreshFlag makes the desktop binary time inbox refreshes against a scratch database
// and print the report, without opening a window.
const RefreshFlag = "--measure-refresh"

// RefreshRuns is how many refreshes RefreshFlag times.
const RefreshRuns = 5

// RefreshesPerHour is the scheduler's steady-state cadence: one refresh every five
// minutes.
const RefreshesPerHour = 12

// defaultHourlyBudget is GitHub's hourly GraphQL point budget for a signed-in user, used
// when a refresh recorded no limit of its own.
const defaultHourlyBudget = 5000

// percent turns a ratio into a percentage.
const percent = 100

// ErrNoGraphQLReading is returned when a refresh recorded no GraphQL rate reading, so the
// query's cost is unknown.
var ErrNoGraphQLReading = errors.New("the refresh recorded no GraphQL rate reading")

// Refresher refreshes the inbox from GitHub. *inbox.Service implements it.
type Refresher interface {
	Refresh(ctx context.Context) (inbox.Snapshot, error)
}

// RefreshReport is a series of timed refreshes and their GraphQL point cost.
type RefreshReport struct {
	// Runs is each refresh's duration, in order: borrowing the token, the REST rate-limit
	// request, the GraphQL inbox query, and the details of the items already listed (made
	// concurrently), the details of any item new to the lists, and the database write.
	Runs []time.Duration

	// Cost is the GraphQL point cost GitHub charged for one refresh, the inbox query and
	// the details requests together (the highest seen across the runs).
	Cost int

	// Limit is the hourly GraphQL point budget GitHub reported.
	Limit int
}

// TimeRefreshes runs refresh runs times, one after another, and records each duration and
// the query cost. It stops at the first failure.
func TimeRefreshes(ctx context.Context, r Refresher, runs int, now func() time.Time) (RefreshReport, error) {
	if runs < 1 {
		return RefreshReport{}, ErrTooFewRuns
	}

	report := RefreshReport{Runs: make([]time.Duration, 0, runs)}

	for i := range runs {
		start := now()

		snap, err := r.Refresh(ctx)
		if err != nil {
			return report, fmt.Errorf("refresh %d of %d: %w", i+1, runs, err)
		}

		report.Runs = append(report.Runs, now().Sub(start))

		if !snap.Rate.GraphQL.Known {
			return report, fmt.Errorf("refresh %d of %d: %w", i+1, runs, ErrNoGraphQLReading)
		}

		report.Cost = max(report.Cost, snap.Rate.GraphQL.Cost)
		report.Limit = snap.Rate.GraphQL.Limit
	}

	return report, nil
}

// Median returns the median refresh duration.
func (r RefreshReport) Median() time.Duration {
	return Median(r.Runs)
}

// HourlyBudget returns the hourly GraphQL budget: the limit GitHub reported, or GitHub's
// standard 5,000 points when none was recorded.
func (r RefreshReport) HourlyBudget() int {
	if r.Limit > 0 {
		return r.Limit
	}

	return defaultHourlyBudget
}

// HourlyPoints returns the GraphQL points the scheduler spends per hour.
func (r RefreshReport) HourlyPoints() int {
	return r.Cost * RefreshesPerHour
}

// BudgetPercent returns the share of the hourly GraphQL budget the scheduler spends.
func (r RefreshReport) BudgetPercent() float64 {
	return float64(r.HourlyPoints()) / float64(r.HourlyBudget()) * percent
}

// Lines renders the report as key=value lines: one per refresh, then the summary.
func (r RefreshReport) Lines() []string {
	lines := make([]string, 0, len(r.Runs)+6)

	for i, d := range r.Runs {
		lines = append(lines, fmt.Sprintf("refresh.run=%d ms=%s", i+1, milliseconds(d)))
	}

	return append(lines,
		fmt.Sprintf("refresh.median_ms=%s median_s=%s", milliseconds(r.Median()), seconds(r.Median())),
		fmt.Sprintf("refresh.graphql_cost=%d", r.Cost),
		fmt.Sprintf("refresh.graphql_limit=%d", r.HourlyBudget()),
		fmt.Sprintf("refresh.hourly_points=%d (cost x %d refreshes per hour)", r.HourlyPoints(), RefreshesPerHour),
		fmt.Sprintf("refresh.budget_pct=%.2f", r.BudgetPercent()),
	)
}

// RefreshOptions configure RunRefresh. Home is required.
type RefreshOptions struct {
	// Home is the user's home directory, where the GitHub CLI and its login are found.
	Home string

	// Version is the build's version, sent to GitHub in the user agent.
	Version string

	// Runs is how many refreshes to time; zero means RefreshRuns.
	Runs int

	// TempDir is where the scratch data directory is created; "" uses the system default.
	TempDir string

	// Runner runs external programs (the GitHub CLI); nil uses the app's default.
	Runner execpath.Runner

	// Logger receives the app's log records; nil uses slog.Default().
	Logger *slog.Logger
}

// RunRefresh times live inbox refreshes the way the app makes them, against a scratch
// database that is deleted afterwards, so the user's own data is never touched. Every
// request is a read.
func RunRefresh(ctx context.Context, opts RefreshOptions) (RefreshReport, error) {
	runs := opts.Runs
	if runs == 0 {
		runs = RefreshRuns
	}

	dir, err := os.MkdirTemp(opts.TempDir, brand.Slug+"-measure-")
	if err != nil {
		return RefreshReport{}, fmt.Errorf("create the scratch data directory: %w", err)
	}

	defer func() { _ = os.RemoveAll(dir) }()

	c, err := core.Bootstrap(ctx, core.Options{
		Paths:   appdata.NewPaths(filepath.Join(dir, "data")),
		Home:    opts.Home,
		Version: opts.Version,
		Runner:  opts.Runner,
		Logger:  opts.Logger,
	})
	if err != nil {
		return RefreshReport{}, err
	}

	defer func() { _ = c.Close() }()

	return TimeRefreshes(ctx, c.Inbox, runs, time.Now)
}
