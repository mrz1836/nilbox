package release

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/scriptrun"
	"github.com/mrz1836/nilbox/internal/testutil"
	"github.com/mrz1836/nilbox/internal/uierr"
)

// insertRun records a run that ended, of kind, with result as its recorded result.
func (r *releaseRig) insertRun(kind scriptrun.Kind, result string) int64 {
	r.t.Helper()

	now := time.Now().UTC()

	got, err := r.db.ExecContext(r.t.Context(), `INSERT INTO script_runs (kind, title, dir, argv, status, result,
		started_at, finished_at) VALUES (?, 'A run', ?, '["/opt/tools/magex"]', 'failed', ?, ?, ?)`,
		string(kind), r.clone, result, now, now)
	require.NoError(r.t, err)

	id, err := got.LastInsertId()
	require.NoError(r.t, err)

	return id
}

func TestReleaseResultReadsTheRunsResultTyped(t *testing.T) {
	t.Parallel()

	r := newReleaseRig(t, rigOptions{base: shownVersion})
	testutil.RunGit(t, r.clone, "checkout", "-q", "-b", recFeature)
	r.magex("1.30.2", r.replay("patch-feature-behind"))
	r.after.set(r.tagged(recVersion, r.second, true))

	run := r.ended(r.release())

	got, err := r.svc.ReleaseResult(t.Context(), run.ID)
	require.NoError(t, err)
	assert.Equal(t, resultOf(t, run), got, "the outcome the run recorded")
	assert.Equal(t, ResultReleased, got.Kind)
	assert.Equal(t, []string{}, got.Commands)
}

func TestReleaseResultOfARunWithoutOne(t *testing.T) {
	t.Parallel()

	r := newReleaseRig(t, rigOptions{base: shownVersion})
	ctx := t.Context()

	script := r.insertRun(scriptrun.KindScript, `{"outcome":{"kind":"released"}}`)
	_, err := r.svc.ReleaseResult(ctx, script)
	require.ErrorIs(t, err, ErrNotARelease)
	assert.Equal(t, "This run isn't a release.", uierr.Message(err))

	never := r.insertRun(scriptrun.KindRelease, `{}`)
	_, err = r.svc.ReleaseResult(ctx, never)
	require.ErrorIs(t, err, ErrNoResult, "a release that couldn't start has no outcome")
	assert.Equal(t, "This release ended without a result to show.", uierr.Message(err))

	odd := r.insertRun(scriptrun.KindRelease, `{"outcome":{"kind":"published"}}`)
	_, err = r.svc.ReleaseResult(ctx, odd)
	require.ErrorIs(t, err, ErrNoResult, "an outcome naming no result this build knows")

	sparse := r.insertRun(scriptrun.KindRelease, `{"outcome":{"kind":"failed","tag":"v1.4.3"}}`)
	got, err := r.svc.ReleaseResult(ctx, sparse)
	require.NoError(t, err)
	assert.Equal(t, Result{Kind: ResultFailed, Tag: recVersion, Commands: []string{}}, got,
		"an older build's sparse outcome reads with no null list")

	_, err = r.svc.ReleaseResult(ctx, sparse+100)
	require.ErrorIs(t, err, scriptrun.ErrNotFound)

	r.magex("1.30.2", r.replayUpTo("patch-feature-behind", "Using bump type"))
	going := r.release()

	_, err = r.svc.ReleaseResult(ctx, going)
	require.ErrorIs(t, err, ErrNoResult)
	assert.Equal(t, "This release is still going.", uierr.Message(err))

	_, err = NewService(Config{}).ReleaseResult(ctx, going)
	require.ErrorIs(t, err, ErrNotSetUp)
}

func TestDecodeResult(t *testing.T) {
	t.Parallel()

	_, err := decodeResult(func() {})
	require.Error(t, err, "a value JSON can't hold")

	_, err = decodeResult("released")
	require.Error(t, err, "not an object")

	_, err = decodeResult(nil)
	require.ErrorIs(t, err, errNoKind)
}
