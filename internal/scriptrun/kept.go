package scriptrun

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
)

// Kept reads what the run id keeps beside its record (Spec.Keep) into into, and reports
// whether it keeps anything: nothing once its end is recorded, nor for a run whose kind
// keeps nothing, or whose state couldn't be written. A kind's Verifier reads it to settle a
// run from before the app restarted as its Finish would have.
func (e *Engine) Kept(id int64, into any) (bool, error) {
	data, err := os.ReadFile(e.files.kept(id))
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}

	if err == nil {
		err = json.Unmarshal(data, into)
	}

	if err != nil {
		return false, fmt.Errorf("read what run %d kept: %w", id, err)
	}

	return true, nil
}

// keepState writes what the run id keeps beside its record (Spec.Keep) into its own file,
// once its Before has run. One that can't be written is only logged: the run goes on, and
// its kind's Verifier finds nothing kept after a restart.
func (e *Engine) keepState(ctx context.Context, id int64, state any) {
	if state == nil {
		return
	}

	data, err := json.Marshal(state)
	if err == nil {
		err = e.files.writeKept(id, data)
	}

	if err != nil {
		e.logger.WarnContext(ctx, "couldn't keep a run's state beside its record", "run", id, "error", err)
	}
}

// dropKept removes what the run id kept beside its record, once its end is recorded.
func (e *Engine) dropKept(ctx context.Context, id int64) {
	if err := os.Remove(e.files.kept(id)); err != nil && !errors.Is(err, fs.ErrNotExist) {
		e.logger.WarnContext(ctx, "couldn't remove a run's kept state", "run", id, "error", err)
	}
}
