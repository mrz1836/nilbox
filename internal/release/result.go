package release

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"

	"github.com/mrz1836/nilbox/internal/scriptrun"
	"github.com/mrz1836/nilbox/internal/uierr"
)

var (
	// ErrNotARelease is returned by ReleaseResult for a run that isn't a release.
	ErrNotARelease = errors.New("the run isn't a release")

	// ErrNoResult is returned by ReleaseResult for a release with no result: one still
	// going, or one that ended before magex did, as when it couldn't start.
	ErrNoResult = errors.New("the release has no result")
)

// RunHistory reads a run back from the runner's history: the scriptrun Service.
type RunHistory interface {
	Run(ctx context.Context, id int64) (scriptrun.Run, error)
}

// resultKinds are the ways a release comes out.
func resultKinds() []ResultKind {
	return []ResultKind{ResultReleased, ResultNotPushed, ResultFailed, ResultUnverified}
}

// ReleaseResult returns a release's result, as its run recorded it once magex ended and
// the release was verified: the run's Result.Outcome, typed. The runner keeps a run's
// outcome as plain JSON, which the window can't name, so the window reads a release's
// here, and gets Result's own type. It fails for a run the history no longer holds
// (scriptrun.ErrNotFound), a run that isn't a release (ErrNotARelease), and a release
// with no result (ErrNoResult): one still going, or one that ended before magex did.
func (s *Service) ReleaseResult(ctx context.Context, runID int64) (Result, error) {
	if s.history == nil {
		return Result{}, ErrNotSetUp
	}

	run, err := s.history.Run(ctx, runID)
	if err != nil {
		return Result{}, err
	}

	switch {
	case run.Kind != scriptrun.KindRelease:
		return Result{}, uierr.New("This run isn't a release.", fmt.Errorf("%w: run %d", ErrNotARelease, runID))
	case run.Status.Live():
		return Result{}, uierr.New("This release is still going.", fmt.Errorf("%w: run %d", ErrNoResult, runID))
	}

	res, err := decodeResult(run.Result.Outcome)
	if err != nil {
		return Result{}, uierr.New("This release ended without a result to show.",
			fmt.Errorf("%w: run %d: %w", ErrNoResult, runID, err))
	}

	return res, nil
}

// errNoKind is a recorded outcome that names no way a release comes out.
var errNoKind = errors.New("the outcome names no result")

// decodeResult reads a release's result back from its record, where it was decoded as
// plain JSON values, with no nil list.
func decodeResult(recorded any) (Result, error) {
	data, err := json.Marshal(recorded)
	if err != nil {
		return Result{}, fmt.Errorf("read the outcome: %w", err)
	}

	var res Result
	if err = json.Unmarshal(data, &res); err != nil {
		return Result{}, fmt.Errorf("read the outcome: %w", err)
	}

	if !slices.Contains(resultKinds(), res.Kind) {
		return Result{}, fmt.Errorf("%w: %q", errNoKind, res.Kind)
	}

	if res.Commands == nil {
		res.Commands = []string{}
	}

	return res, nil
}
