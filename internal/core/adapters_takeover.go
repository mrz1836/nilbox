package core

import (
	"context"
	"fmt"
	"time"

	"github.com/mrz1836/nilbox/internal/agentrun"
	"github.com/mrz1836/nilbox/internal/agentrun/runevent"
	"github.com/mrz1836/nilbox/internal/fence"
	"github.com/mrz1836/nilbox/internal/launcher"
	"github.com/mrz1836/nilbox/internal/uierr"
)

// The run view's reads of a background run, beside its events (autopilot.RunViews), and
// Take over (launcher.TakeOvers): stop the run, tell Autopilot it is taken over, and wait
// for its end, so its conversation is whole on disk when it resumes in the terminal.

// takeOverWait bounds how long Take over waits for a run to end once it is stopped: the
// stop ladder's SIGINT, SIGTERM five seconds later, and SIGKILL ten after that, with room to
// record its end.
const takeOverWait = 30 * time.Second

var _ launcher.TakeOvers = (*backgroundAgents)(nil)

// EventsBefore returns a page of the ledger's run's events read back from its log
// (autopilot.RunViews).
func (b *backgroundAgents) EventsBefore(ctx context.Context, runID, before int64) (runevent.Page, error) {
	if b.runner == nil {
		return runevent.Page{}, errNoBackground
	}

	return b.runner.EventsBefore(ctx, runID, before)
}

// Diff returns the ledger's run's worktree's changes since it started (autopilot.RunViews).
func (b *backgroundAgents) Diff(ctx context.Context, runID int64) (runevent.Diff, error) {
	if b.runner == nil {
		return runevent.Diff{}, errNoBackground
	}

	return b.runner.Diff(ctx, runID)
}

// TakeOver takes the background run scriptRunID, its ID in the run history, over: it tells
// Autopilot's engine first, so the run's end closes its match as taken over and tells
// nothing, then stops the run while it works and waits for its end to be recorded
// (launcher.TakeOvers). An engine that can't take the word, as while another copy holds it,
// leaves the match to the run's end as any Stop does; the take over goes on.
func (b *backgroundAgents) TakeOver(ctx context.Context, scriptRunID int64) error {
	if b.runner == nil {
		return errNoBackground
	}

	taken, ok := b.runner.TakenOf(ctx, scriptRunID)
	if !ok {
		return uierr.New("That run is no longer in the history.", fmt.Errorf("%w: run %d", launcher.ErrNotBackground,
			scriptRunID))
	}

	if e := b.engineNow(); e != nil {
		if err := e.TakeOver(ctx, taken.RunID); err != nil {
			b.logger.WarnContext(ctx, "Autopilot couldn't record that a run was taken over", "run", taken.RunID, "error", err)
		}
	}

	if live, _ := b.runner.Alive(ctx, taken.RunID); live == agentrun.LiveAlive {
		if err := b.runner.Stop(ctx, taken.RunID); err != nil {
			return uierr.New("The run couldn't be stopped, so it wasn't taken over.", err)
		}
	}

	wait, cancel := context.WithTimeout(ctx, takeOverWait)
	defer cancel()

	if err := b.runner.Wait(wait, taken.RunID); err != nil {
		return uierr.New("The run hasn't stopped yet. Try Take over again in a moment.", err)
	}

	return nil
}

// fenceOf returns the fence the background run scriptRunID's conversation resumes under,
// taken over: its preset and its own folder; nil for no such run.
func (b *backgroundAgents) fenceOf(ctx context.Context, scriptRunID int64) *launcher.Fence {
	if b == nil || b.runner == nil || scriptRunID == 0 {
		return nil
	}

	taken, ok := b.runner.TakenOf(ctx, scriptRunID)
	if !ok || taken.Folder == "" {
		return nil
	}

	return &launcher.Fence{Preset: fence.Preset(taken.Preset), RunDir: taken.Folder}
}
