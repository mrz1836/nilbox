package scriptrun

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/mrz1836/nilbox/internal/repo"
	"github.com/mrz1836/nilbox/internal/scriptrun/internal/queries"
	"github.com/mrz1836/nilbox/internal/store"
)

// runStore reads and writes script_runs.
type runStore struct {
	db *sql.DB
	q  *queries.Queries
}

// started is what a run's row records as it starts.
type started struct {
	spec    Spec
	command string
	runsIn  RunsIn
	result  Result
	at      time.Time
}

// insert records a run as it starts, running, and returns its ID.
func (s runStore) insert(ctx context.Context, st started) (int64, error) {
	argv, err := store.EncodeJSON(st.spec.Argv)
	if err != nil {
		return 0, fmt.Errorf("record the run's command: %w", err)
	}

	result, err := store.EncodeJSON(st.result)
	if err != nil {
		return 0, fmt.Errorf("%w: its intent: %w", ErrInvalidSpec, err)
	}

	id, err := s.q.InsertRun(ctx, queries.InsertRunParams{
		Kind: string(st.spec.Kind), Title: st.spec.Title, Source: st.spec.Source, RepoOwner: st.spec.Repo.Owner,
		RepoName: st.spec.Repo.Repo, Dir: st.spec.Dir, Argv: argv, Command: st.command, RunsIn: string(st.runsIn),
		Status: string(StatusRunning), Result: result, StartedAt: st.at.UTC(),
	})
	if err != nil {
		return 0, fmt.Errorf("record the run: %w", err)
	}

	return id, nil
}

// setProcess records a run's process, its group, and when the process started.
func (s runStore) setProcess(ctx context.Context, id int64, pid, pgid int, startedAt time.Time) error {
	err := s.q.SetRunProcess(ctx, queries.SetRunProcessParams{
		ID: id, Pid: int64(pid), Pgid: int64(pgid), ProcStartedAt: optionalTime(startedAt),
	})
	if err != nil {
		return fmt.Errorf("record the run's process: %w", err)
	}

	return nil
}

// setStatus changes the status and summary of a run that has not ended.
func (s runStore) setStatus(ctx context.Context, id int64, status Status, summary string) error {
	_, err := s.q.SetRunStatus(ctx, queries.SetRunStatusParams{ID: id, Status: string(status), Summary: summary})
	if err != nil {
		return fmt.Errorf("record the run's status: %w", err)
	}

	return nil
}

// finished is what a run's row records as it ends.
type finished struct {
	id       int64
	status   Status
	exitCode *int
	summary  string
	result   Result
	logBytes int64
	at       time.Time
}

// errNotRecorded is a run's end that changed no record: the run had ended already, or is
// no longer held.
var errNotRecorded = errors.New("no run's end was recorded")

// finish records how a run ended. A run that ended already, or that the history no longer
// holds, is left as it is, and errNotRecorded says so: its end was recorded once already,
// so nothing is told of it again.
func (s runStore) finish(ctx context.Context, f finished) error {
	result, err := store.EncodeJSON(f.result)
	if err != nil {
		return fmt.Errorf("record the run's result: %w", err)
	}

	var exitCode *int64

	if f.exitCode != nil {
		code := int64(*f.exitCode)
		exitCode = &code
	}

	at := f.at.UTC()

	n, err := s.q.FinishRun(ctx, queries.FinishRunParams{
		ID: f.id, Status: string(f.status), ExitCode: exitCode, Summary: f.summary, Result: result,
		LogBytes: f.logBytes, FinishedAt: &at,
	})
	if err != nil {
		return fmt.Errorf("record how the run ended: %w", err)
	}

	if n == 0 {
		return fmt.Errorf("%w: run %d isn't recorded as going", errNotRecorded, f.id)
	}

	return nil
}

// amend rewrites the summary and the kind's own result of a run that has ended, in one
// transaction with the read of the run that change is given: change returns what to
// write, or false to leave the run as it is. It reports whether it changed the run. A run
// the history no longer holds fails with ErrNotFound, and one still going with
// ErrStillGoing.
func (s runStore) amend(ctx context.Context, id int64, change func(Record) (Amendment, bool)) (bool, error) {
	changed := false

	err := store.InTx(ctx, s.db, func(tx *sql.Tx) error {
		var err error

		changed, err = amendIn(ctx, s.q.WithTx(tx), id, change)

		return err
	})
	if err != nil {
		return false, err
	}

	return changed, nil
}

// amendIn is amend's work inside its transaction, through q.
func amendIn(ctx context.Context, q *queries.Queries, id int64, change func(Record) (Amendment, bool)) (bool, error) {
	row, err := q.GetRun(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return false, fmt.Errorf("%w: %d", ErrNotFound, id)
	}

	if err != nil {
		return false, fmt.Errorf("read the run: %w", err)
	}

	rec := recordOf(row)
	if rec.Status.Live() {
		return false, fmt.Errorf("%w: %d", ErrStillGoing, id)
	}

	a, ok := change(rec)
	if !ok {
		return false, nil
	}

	result := rec.Result
	result.Outcome = a.Outcome

	raw, err := store.EncodeJSON(result)
	if err == nil {
		_, err = q.AmendRun(ctx, queries.AmendRunParams{ID: id, Summary: a.Summary, Result: raw})
	}

	if err != nil {
		return false, fmt.Errorf("record the run's result: %w", err)
	}

	return true, nil
}

// get returns one run, or ErrNotFound.
func (s runStore) get(ctx context.Context, id int64) (Record, error) {
	row, err := s.q.GetRun(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return Record{}, fmt.Errorf("%w: %d", ErrNotFound, id)
	}

	if err != nil {
		return Record{}, fmt.Errorf("read the run: %w", err)
	}

	return recordOf(row), nil
}

// list returns every run, newest first.
func (s runStore) list(ctx context.Context) ([]Record, error) {
	rows, err := s.q.ListRuns(ctx)
	if err != nil {
		return nil, fmt.Errorf("read the runs: %w", err)
	}

	return recordsOf(rows), nil
}

// live returns every run that has not ended, oldest first.
func (s runStore) live(ctx context.Context) ([]Record, error) {
	rows, err := s.q.LiveRuns(ctx)
	if err != nil {
		return nil, fmt.Errorf("read the runs going: %w", err)
	}

	return recordsOf(rows), nil
}

// ids returns the ID of every run.
func (s runStore) ids(ctx context.Context) ([]int64, error) {
	ids, err := s.q.RunIDs(ctx)
	if err != nil {
		return nil, fmt.Errorf("read the runs: %w", err)
	}

	return ids, nil
}

// markSeen records that the user saw a run, or returns ErrNotFound.
func (s runStore) markSeen(ctx context.Context, id int64, at time.Time) error {
	at = at.UTC()

	n, err := s.q.MarkRunSeen(ctx, queries.MarkRunSeenParams{ID: id, SeenAt: &at})
	if err != nil {
		return fmt.Errorf("record that the run was seen: %w", err)
	}

	if n == 0 {
		return fmt.Errorf("%w: %d", ErrNotFound, id)
	}

	return nil
}

// delete deletes the runs named that have ended.
func (s runStore) delete(ctx context.Context, ids []int64) error {
	if len(ids) == 0 {
		return nil
	}

	if err := s.q.DeleteRuns(ctx, ids); err != nil {
		return fmt.Errorf("forget the oldest runs: %w", err)
	}

	return nil
}

// recordsOf returns rows as records.
func recordsOf(rows []queries.ScriptRun) []Record {
	out := make([]Record, 0, len(rows))
	for _, row := range rows {
		out = append(out, recordOf(row))
	}

	return out
}

// recordOf returns a row as a record. A column that doesn't decode reads as empty.
func recordOf(row queries.ScriptRun) Record {
	argv, err := store.DecodeJSON[[]string](row.Argv)
	if err != nil || argv == nil {
		argv = []string{}
	}

	result, err := store.DecodeJSON[Result](row.Result)
	if err != nil {
		result = Result{}
	}

	rec := Record{
		ID: row.ID, Kind: Kind(row.Kind), Title: row.Title, Source: row.Source,
		Repo: repo.Name{Owner: row.RepoOwner, Repo: row.RepoName}, Dir: row.Dir, Argv: argv, Command: row.Command,
		RunsIn: RunsIn(row.RunsIn), Status: Status(row.Status), PID: int(row.Pid), PGID: int(row.Pgid),
		ProcStartedAt: timeOf(row.ProcStartedAt), Summary: row.Summary, Result: withLists(result),
		LogBytes: row.LogBytes, SeenAt: timeOf(row.SeenAt), StartedAt: row.StartedAt.UTC(),
		FinishedAt: timeOf(row.FinishedAt),
	}

	if row.ExitCode != nil {
		code := int(*row.ExitCode)
		rec.ExitCode = &code
	}

	return rec
}

// withLists returns r with its lists empty rather than nil.
func withLists(r Result) Result {
	if r.Warnings == nil {
		r.Warnings = []string{}
	}

	if r.Unrecognized == nil {
		r.Unrecognized = []string{}
	}

	return r
}

// timeOf returns a nullable column's time in UTC, or zero.
func timeOf(t *time.Time) time.Time {
	if t == nil {
		return time.Time{}
	}

	return t.UTC()
}

// optionalTime returns t in UTC for a nullable column, or nil for the zero time.
func optionalTime(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}

	t = t.UTC()

	return &t
}
