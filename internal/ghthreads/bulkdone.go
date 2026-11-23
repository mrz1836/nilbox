package ghthreads

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"time"

	"github.com/mrz1836/nilbox/internal/brand"
	"github.com/mrz1836/nilbox/internal/budget"
	"github.com/mrz1836/nilbox/internal/event"
	"github.com/mrz1836/nilbox/internal/ghthreads/internal/queries"
	"github.com/mrz1836/nilbox/internal/prefs"
	"github.com/mrz1836/nilbox/internal/repo"
	"github.com/mrz1836/nilbox/internal/store"
	"github.com/mrz1836/nilbox/internal/uierr"
)

// Mark all done. GitHub's API has no call that marks many notifications done: only a
// DELETE of one thread, and a click waits seconds at most for the chore lane, which sends
// one a second, so the panel's Done marks a handful. The mirror holds only the newest
// MaxThreads, too, and a backlog runs past it. So a bulk mark is a job of its own, which
// the user starts (StartBulkDone) for the notifications older than an age they choose, in
// the repositories they tick or in all of them, and which the Syncer runs in the
// background, one notification at a time, until it is done, or the user stops it.
//
// A bulk run reads the listing straight from GitHub, before the job's cutoff (the click's
// time, less the age), so it reaches past the mirror's cap, and one page of 50 covers up to
// 50 marks. It marks each thread of the page in the job's scope done through the chore
// gate's bulk lane (budget.BulkPerMinute, budget.BulkPerHour, budget.BulkGap), so clicks
// and clears keep their own lane, and reads the page again at the same cutoff once its
// threads are marked, since those marked drop out. A page with none in scope moves the
// cursor past it, to its oldest update (ties kept), and a short page with none ends the
// job. A thread the mirror lists as updated since the cutoff is left alone: it has new
// activity.
//
// The job lives in the KeyBulkDone preference, so it carries on after a restart, where
// it left off, and the bulk lane's times are kept with the budget, so the pace holds
// across it too. Each mark the bulk run records drops the thread from the mirror and
// settles a clear queued for it, in one transaction with the job's count, and tells the
// window how far it got (event.GHThreadsBulk); the window reads the whole mirror again at
// most every bulkTellEvery, and at once for a pause, the end, or a stop.

const (
	// KeyBulkDone is the preference that holds the bulk mark, as JSON: whose notifications,
	// which, how far it got, and how it stands. Only this package writes it.
	KeyBulkDone = "ghthreads.bulkDone"

	// bulkTellEvery is how often, at most, a bulk run that changed only its counts has the
	// window read the whole mirror again (tell); a progress event follows every mark.
	bulkTellEvery = 10 * time.Second

	// bulkPageFor is how long a page a bulk run read stands before the next reads it again:
	// a thread on it that changed since is caught within that.
	bulkPageFor = time.Minute

	// maxBulkPages is the most pages one bulk run reads past repositories not in scope, and
	// bulkPagesPause how long the next waits after it: a backlog the job leaves alone is
	// skipped at 50 a page, at a pace that stays well under GitHub's limit.
	maxBulkPages   = 5
	bulkPagesPause = 5 * time.Second

	// maxBulkSkipped is how many threads a bulk mark gives up on, each after MaxAttempts
	// failed marks, before it stops: GitHub is refusing them.
	maxBulkSkipped = 200

	// maxBulkRepos is the most repositories a bulk mark names.
	maxBulkRepos = MaxThreads
)

// BulkAge is how old the notifications a bulk mark marks done are, at least: updated
// before the click's time less the age.
type BulkAge string

// The ages a bulk mark may choose.
const (
	// BulkAgeAny is every notification, updated before the click.
	BulkAgeAny BulkAge = "any"

	// BulkAgeDay is those not updated for a day.
	BulkAgeDay BulkAge = "day"

	// BulkAgeWeek is those not updated for a week.
	BulkAgeWeek BulkAge = "week"

	// BulkAgeMonth is those not updated for 30 days.
	BulkAgeMonth BulkAge = "month"

	// BulkAgeQuarter is those not updated for 90 days.
	BulkAgeQuarter BulkAge = "quarter"
)

// span is how long before the click the age reaches; ok is false for an age that isn't
// one of the constants.
func (a BulkAge) span() (time.Duration, bool) {
	const day = 24 * time.Hour

	switch a {
	case BulkAgeAny:
		return 0, true
	case BulkAgeDay:
		return day, true
	case BulkAgeWeek:
		return 7 * day, true
	case BulkAgeMonth:
		return 30 * day, true
	case BulkAgeQuarter:
		return 90 * day, true
	default:
		return 0, false
	}
}

// BulkPause is why a running bulk mark is waiting, other than its pace.
type BulkPause string

// The reasons a bulk mark waits.
const (
	// BulkPauseNone is no pause: it goes as fast as its lane lets it.
	BulkPauseNone BulkPause = "none"

	// BulkPauseHeld is GitHub asking the app to slow down, until NextAt.
	BulkPauseHeld BulkPause = "held"

	// BulkPauseReserve is the API budget down to the reserve the app leaves for
	// everything else, until it refills at NextAt.
	BulkPauseReserve BulkPause = "reserve"

	// BulkPauseOtherAccount is the GitHub token belonging to another account than the
	// job's, or none to borrow: it waits for the next pass.
	BulkPauseOtherAccount BulkPause = "otherAccount"

	// BulkPauseReadingOff is reading the notifications turned off: it waits until it is
	// back on.
	BulkPauseReadingOff BulkPause = "readingOff"

	// BulkPauseFailing is GitHub failing a listing or a mark, tried again at NextAt.
	BulkPauseFailing BulkPause = "failing"
)

var (
	// errBulkStart is a bulk mark that can't start; the uierr sentence says why.
	errBulkStart = errors.New("the notifications can't be marked done in bulk")

	// errBulkGaveUp is a bulk mark that stopped after giving up on maxBulkSkipped threads.
	errBulkGaveUp = errors.New("GitHub refused too many of the notifications")
)

// BulkDoneRequest is what the user chose to mark done: the notifications older than Age,
// in Repos ("owner/name", any case), or in every repository, and those about none, when
// All.
type BulkDoneRequest struct {
	Age   BulkAge  `json:"age"`
	Repos []string `json:"repos"`
	All   bool     `json:"all"`
}

// BulkDoneStatus is how the bulk mark stands, for the window: none before the first, or
// once the user dismissed the last. Every slice is non-nil.
type BulkDoneStatus struct {
	// Running is whether it is going; StartedAt is "" when there is none to show.
	Running bool `json:"running"`

	// Age, Before, All, and Repos are what it marks: those updated before Before (RFC 3339),
	// the click's time less Age, in Repos, or every one when All.
	Age    BulkAge  `json:"age"`
	Before string   `json:"before"`
	All    bool     `json:"all"`
	Repos  []string `json:"repos"`

	// Expected is how many the mirror listed in its scope when it started, Done how many
	// it marked (GitHub's own count of those it no longer had included), and Failed how
	// many it gave up on.
	Expected int `json:"expected"`
	Done     int `json:"done"`
	Failed   int `json:"failed"`

	// Left is how many in its scope the mirror still lists, those given up on aside, and
	// More whether GitHub holds more than the mirror's newest MaxThreads, so the job may
	// mark more than it counts.
	Left int  `json:"left"`
	More bool `json:"more"`

	// NextAt is when it goes on after a pause (RFC 3339, "" for none), Pause why it waits,
	// and Error the last failure, in a sentence.
	NextAt string    `json:"nextAt"`
	Pause  BulkPause `json:"pause"`
	Error  string    `json:"error"`

	// StartedAt and EndedAt are when it started and ended (RFC 3339, "" for not yet), and
	// Stopped whether the user stopped it.
	StartedAt string `json:"startedAt"`
	EndedAt   string `json:"endedAt"`
	Stopped   bool   `json:"stopped"`

	// PerMinute and PerHour are the most its lane marks in a minute and in an hour, from
	// which the window works out how long it takes.
	PerMinute int `json:"perMinute"`
	PerHour   int `json:"perHour"`
}

// bulkState is the bulk mark, as KeyBulkDone holds it.
type bulkState struct {
	// Account is whose notifications it marks, and Age and Before which: those updated
	// before Before, the click's time less Age, in whole seconds.
	Account string    `json:"account"`
	Age     BulkAge   `json:"age"`
	Before  time.Time `json:"before"`

	// Repos are the repositories it marks ("owner/name"), or every one when All.
	Repos []string `json:"repos,omitempty"`
	All   bool     `json:"all,omitempty"`

	// Cursor is where the next listing starts, past pages with none in scope; zero starts
	// at Before.
	Cursor time.Time `json:"cursor,omitzero"`

	// StartedAt names the job, and EndedAt is when it ended, zero while it runs; Stopped is
	// whether the user stopped it.
	StartedAt time.Time `json:"startedAt"`
	EndedAt   time.Time `json:"endedAt,omitzero"`
	Stopped   bool      `json:"stopped,omitempty"`

	// Expected is how many the mirror listed in its scope at the start, Done how many it
	// marked, and Failed how many it gave up on, whose IDs Skipped holds.
	Expected int     `json:"expected"`
	Done     int     `json:"done"`
	Failed   int     `json:"failed"`
	Skipped  []int64 `json:"skipped,omitempty"`

	// Attempt is the thread whose mark last failed, and how many times in a row.
	Attempt bulkAttempt `json:"attempt,omitzero"`

	// NextAt is when it goes on after a pause, Pause why it waits, and LastError the last
	// failure, in a sentence.
	NextAt    time.Time `json:"nextAt,omitzero"`
	Pause     BulkPause `json:"pause,omitempty"`
	LastError string    `json:"lastError,omitempty"`
}

// bulkAttempt is a thread whose mark failed Count times in a row.
type bulkAttempt struct {
	Thread int64 `json:"thread"`
	Count  int   `json:"count"`
}

// StartBulkDone starts marking done on GitHub, in the background, the notifications of the
// account the last pass read that req names: those updated before now less req.Age, in
// req.Repos or every repository, in the mirror or past it. It refuses, with a sentence
// that says why, while reading is off, before a pass read an account, while GitHub refuses
// the listing, for a request that names nothing, and while another bulk mark runs. It
// returns the Snapshot.
func (s *Service) StartBulkDone(ctx context.Context, req BulkDoneRequest) (Snapshot, error) {
	b, err := s.startBulk(ctx, req)
	if err != nil {
		return Snapshot{}, err
	}

	s.logger.InfoContext(ctx, "the user started marking GitHub notifications done in bulk",
		slog.String("age", string(b.Age)), slog.Bool("all", b.All), slog.Int("repos", len(b.Repos)),
		slog.Int("expected", b.Expected))

	s.bulkBy(b.StartedAt)
	s.nudges.signal()
	s.events.Emit(event.GHThreadsUpdated{})

	return s.Snapshot(ctx)
}

// StopBulkDone stops the bulk mark that runs, if any: what it marked stays marked, and the
// rest stay in the user's inbox. It returns the Snapshot.
func (s *Service) StopBulkDone(ctx context.Context) (Snapshot, error) {
	now := s.clock.Now()

	b, stopped, err := s.changeBulk(ctx, func(_ *queries.Queries, b *bulkState) (bool, error) {
		if !b.running() {
			return false, nil
		}

		b.EndedAt, b.Stopped, b.Pause, b.NextAt = now, true, BulkPauseNone, time.Time{}

		return true, nil
	})
	if err != nil {
		return Snapshot{}, err
	}

	if stopped {
		s.bulkOff()
		s.logger.InfoContext(ctx, "the user stopped marking GitHub notifications done in bulk",
			slog.Int("done", b.Done), slog.Int("failed", b.Failed))
		s.events.Emit(event.GHThreadsUpdated{})
	}

	return s.Snapshot(ctx)
}

// DismissBulkDone forgets the summary of a bulk mark that ended, so the window no longer
// shows it, this run or the next. One still running is left as it is. It returns the
// Snapshot.
func (s *Service) DismissBulkDone(ctx context.Context) (Snapshot, error) {
	dismissed := false

	err := store.InTx(ctx, s.db, func(tx *sql.Tx) error {
		p := s.prefs.WithTx(tx)

		b, err := s.loadBulkFrom(ctx, p)
		if err != nil || b.StartedAt.IsZero() || b.running() {
			return err
		}

		dismissed = true

		if err = p.Delete(ctx, KeyBulkDone); err != nil {
			return fmt.Errorf("forget the bulk mark of GitHub notifications: %w", err)
		}

		return nil
	})
	if err != nil {
		return Snapshot{}, err
	}

	if dismissed {
		s.events.Emit(event.GHThreadsUpdated{})
	}

	return s.Snapshot(ctx)
}

// startBulk checks req and stores the bulk mark it starts, counting what the mirror lists
// in its scope, in one transaction.
func (s *Service) startBulk(ctx context.Context, req BulkDoneRequest) (bulkState, error) {
	now := s.clock.Now()

	b, err := s.bulkFor(ctx, req, now)
	if err != nil {
		return bulkState{}, err
	}

	err = store.InTx(ctx, s.db, func(tx *sql.Tx) error {
		p := s.prefs.WithTx(tx)

		was, loadErr := s.loadBulkFrom(ctx, p)
		if loadErr != nil {
			return loadErr
		}

		if was.running() {
			return uierr.New("Notifications are being marked done already. Stop that first to start again.", errBulkStart)
		}

		rows, listErr := s.q.WithTx(tx).ListThreads(ctx, b.Account)
		if listErr != nil {
			return fmt.Errorf("read the GitHub notifications: %w", listErr)
		}

		b.Expected = b.left(rows)

		return saveBulk(ctx, p, &b)
	})

	return b, err
}

// bulkFor is the bulk mark req asks for, starting at now, once the rules and the state of
// the passes let one start.
func (s *Service) bulkFor(ctx context.Context, req BulkDoneRequest, now time.Time) (bulkState, error) {
	rules, err := s.rules.ThreadRules(ctx)
	if err != nil {
		return bulkState{}, fmt.Errorf("read the rules for GitHub notifications: %w", err)
	}

	st, err := s.loadState(ctx)
	if err != nil {
		return bulkState{}, err
	}

	switch {
	case !rules.Read:
		return bulkState{}, uierr.New(brand.Name+" isn't reading your GitHub notifications. Turn reading on first.", errBulkStart)
	case st.Account == "":
		return bulkState{}, uierr.New("Check your GitHub notifications first, so "+brand.Name+" knows whose to mark.",
			errBulkStart)
	case st.access() != AccessOK:
		return bulkState{}, uierr.New(refusedMessage, errBulkStart)
	}

	span, ok := req.Age.span()
	if !ok {
		return bulkState{}, uierr.New("Choose how old the notifications to mark done are.", errBulkStart)
	}

	repos, err := bulkRepos(req)
	if err != nil {
		return bulkState{}, err
	}

	return bulkState{
		Account: st.Account, Age: req.Age, Before: now.Add(-span).UTC().Truncate(time.Second), Repos: repos,
		All: req.All, StartedAt: now.UTC(), Pause: BulkPauseNone,
	}, nil
}

// bulkRepos returns req's repositories, each once, in order, ignoring case; none for All.
// A name that isn't a repository's, or none at all without All, refuses the request.
func bulkRepos(req BulkDoneRequest) ([]string, error) {
	if req.All {
		return nil, nil
	}

	out := make([]string, 0, len(req.Repos))

	for _, raw := range req.Repos {
		name, err := repo.Parse(strings.TrimSpace(raw))
		if err != nil {
			return nil, uierr.New(fmt.Sprintf("%q isn't a GitHub repository.", raw), errors.Join(errBulkStart, err))
		}

		if full := name.String(); !slices.ContainsFunc(out, func(r string) bool { return strings.EqualFold(r, full) }) {
			out = append(out, full)
		}
	}

	switch {
	case len(out) == 0:
		return nil, uierr.New("Tick at least one repository to mark its notifications done.", errBulkStart)
	case len(out) > maxBulkRepos:
		return nil, uierr.New("That's more repositories than a bulk mark takes. Choose All instead.", errBulkStart)
	}

	slices.SortFunc(out, func(a, b string) int { return strings.Compare(strings.ToLower(a), strings.ToLower(b)) })

	return out, nil
}

// running reports whether the bulk mark goes on.
func (b *bulkState) running() bool {
	return !b.StartedAt.IsZero() && b.EndedAt.IsZero()
}

// cursor is where the next listing starts: the cursor, or Before before it moved.
func (b *bulkState) cursor() time.Time {
	if b.Cursor.IsZero() {
		return b.Before
	}

	return b.Cursor
}

// inScope reports whether a thread in owner/name is in the bulk mark's repositories.
func (b *bulkState) inScope(owner, name string) bool {
	if b.All {
		return true
	}

	full := owner + "/" + name

	return slices.ContainsFunc(b.Repos, func(r string) bool { return strings.EqualFold(r, full) })
}

// marks reports whether the bulk mark marks a thread in owner/name, updated at updated,
// with GitHub's id: in scope, updated before the cutoff, and not given up on.
func (b *bulkState) marks(id int64, owner, name string, updated time.Time) bool {
	return updated.Before(b.Before) && b.inScope(owner, name) && !slices.Contains(b.Skipped, id)
}

// left counts the rows the bulk mark still marks.
func (b *bulkState) left(rows []queries.ListThreadsRow) int {
	n := 0

	for i := range rows {
		if b.marks(rows[i].ThreadID, rows[i].RepoOwner, rows[i].RepoName, rows[i].UpdatedAt) {
			n++
		}
	}

	return n
}

// status is the bulk mark as the window shows it, with rows the account's threads the
// mirror lists and truncated whether GitHub holds more.
func (b *bulkState) status(rows []queries.ListThreadsRow, truncated bool) BulkDoneStatus {
	out := BulkDoneStatus{
		Age: BulkAgeAny, Repos: []string{}, Pause: BulkPauseNone, PerMinute: budget.BulkPerMinute, PerHour: budget.BulkPerHour,
	}

	if b.StartedAt.IsZero() {
		return out
	}

	out.Running, out.Age, out.Before, out.All = b.running(), b.Age, stamp(b.Before), b.All
	out.Repos = append(out.Repos, b.Repos...)
	out.Expected, out.Done, out.Failed, out.Error = b.Expected, b.Done, b.Failed, b.LastError
	out.StartedAt, out.EndedAt, out.Stopped = stamp(b.StartedAt), stamp(b.EndedAt), b.Stopped

	if b.Pause != "" {
		out.Pause = b.Pause
	}

	if out.Running {
		out.Left, out.More = b.left(rows), truncated
		out.NextAt = stamp(b.NextAt)
	}

	return out
}

// settled is the bulk mark with what changes as it goes on alone left out (its counts,
// its cursor, and, without a pause, when it goes on): what a bulk run tells the window of
// at once when it changed.
func (b *bulkState) settled() bulkState {
	out := *b
	out.Cursor, out.Done, out.Failed, out.Skipped, out.Attempt = time.Time{}, 0, 0, nil, bulkAttempt{}

	if out.Pause == "" || out.Pause == BulkPauseNone {
		out.Pause, out.NextAt = BulkPauseNone, time.Time{}
	}

	return out
}

// loadBulk reads the bulk mark.
func (s *Service) loadBulk(ctx context.Context) (bulkState, error) {
	return s.loadBulkFrom(ctx, s.prefs)
}

// loadBulkFrom reads the bulk mark through p, the pool's preferences or a transaction's:
// the zero state when none was stored, or one can't be decoded, which is logged.
func (s *Service) loadBulkFrom(ctx context.Context, p *prefs.Store) (bulkState, error) {
	stored, ok, err := p.Get(ctx, KeyBulkDone)
	if err != nil {
		return bulkState{}, fmt.Errorf("read the bulk mark of GitHub notifications: %w", err)
	}

	if !ok {
		return bulkState{}, nil
	}

	b, err := store.DecodeJSON[bulkState](stored)
	if err != nil {
		s.logger.WarnContext(ctx, "the bulk mark of GitHub notifications can't be read; forgetting it",
			slog.Any("error", err))

		return bulkState{}, nil
	}

	return b, nil
}

// saveBulk writes b through p.
func saveBulk(ctx context.Context, p *prefs.Store, b *bulkState) error {
	encoded, err := store.EncodeJSON(b)
	if err != nil {
		return err
	}

	if err = p.Set(ctx, KeyBulkDone, encoded); err != nil {
		return fmt.Errorf("store the bulk mark of GitHub notifications: %w", err)
	}

	return nil
}

// changeBulk reads the bulk mark, has change change it, and stores it when change says
// so, in one transaction, and returns it as it left it. change gets the transaction's
// queries, for what changes with it.
func (s *Service) changeBulk(
	ctx context.Context, change func(q *queries.Queries, b *bulkState) (bool, error),
) (bulkState, bool, error) {
	var (
		out     bulkState
		changed bool
	)

	err := store.InTx(ctx, s.db, func(tx *sql.Tx) error {
		p := s.prefs.WithTx(tx)

		b, err := s.loadBulkFrom(ctx, p)
		if err != nil {
			return err
		}

		if changed, err = change(s.q.WithTx(tx), &b); err != nil || !changed {
			out = b

			return err
		}

		out = b

		return saveBulk(ctx, p, &b)
	})

	return out, changed, err
}

// bulkStatus reads how the bulk mark stands, for st's account's rows.
func (s *Service) bulkStatus(ctx context.Context, rows []queries.ListThreadsRow, truncated bool) (BulkDoneStatus, error) {
	b, err := s.loadBulk(ctx)
	if err != nil {
		return BulkDoneStatus{}, err
	}

	return b.status(rows, truncated), nil
}
