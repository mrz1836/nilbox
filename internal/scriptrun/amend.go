package scriptrun

import "context"

// Amendment is what Amend writes over the record of a run that has ended: the sentence
// that says how it came out, and its kind's own result (Result.Outcome), which must encode
// as JSON. The rest of the record stays as it is.
type Amendment struct {
	Summary string
	Outcome any
}

// Amend rewrites what the record of a run that has ended says about how it came out, as a
// release does while it follows its workflow on GitHub after magex ended. change is given
// the run as recorded now, inside the transaction that writes it, and returns what to
// write, or false to leave the run as it is. Amend reports whether it changed the run, and
// the window hears of a change (Config.Changed). A run the history no longer holds fails
// with ErrNotFound, and one still going with ErrStillGoing: its end, once recorded, would
// write over the amendment.
func (e *Engine) Amend(ctx context.Context, id int64, change func(Record) (Amendment, bool)) (bool, error) {
	changed, err := e.store.amend(ctx, id, change)
	if changed {
		e.notify()
	}

	return changed, err
}

// Ended returns the runs of kind that have ended, oldest first, as the history holds them,
// reading only those: a release's follow of its workflow is resumed from them when the app
// starts again.
func (e *Engine) Ended(ctx context.Context, kind Kind) ([]Record, error) {
	return e.store.ended(ctx, kind)
}
