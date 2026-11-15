package core

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/mrz1836/nilbox/internal/appdata"
	"github.com/mrz1836/nilbox/internal/store"
)

// openData opens the database, starting over first when the user asked to: a reset
// request beside it, from Settings → About or magex app:reset. Then the database is set
// aside as <db>.reset-<stamp> for a fresh one (store.OpenFresh), whose start brings the
// prompts, the launch defaults, and the user's own scripts back from it, and the newest
// few such copies are kept. A request that can't be read is logged and dropped, never
// carried out. A database another copy of the app still has open stops the start, as any
// lock does, with the request kept for the next start: the app never starts silently on
// the old data.
func openData(ctx context.Context, p appdata.Paths, logger *slog.Logger) (store.Opened, error) {
	req, requested, err := appdata.ReadResetRequest(p.ResetRequest)
	if err != nil {
		logger.WarnContext(ctx, "could not read the request to start over; starting on the data as it is",
			"path", p.ResetRequest, "error", err)
		dropResetRequest(ctx, p.ResetRequest, logger)
	}

	if !requested {
		return store.Open(ctx, p.DB)
	}

	opened, err := store.OpenFresh(ctx, p.DB)
	if err != nil {
		return store.Opened{}, fmt.Errorf("start over, as you asked: %w", err)
	}

	if err = store.PruneResetCopies(p.DB); err != nil {
		logger.WarnContext(ctx, "could not delete the oldest databases a reset set aside", "error", err)
	}

	dropResetRequest(ctx, p.ResetRequest, logger)
	logger.InfoContext(ctx, "started over, as the user asked", "by", req.By, "asked_at", req.RequestedAt)

	return opened, nil
}

// dropResetRequest removes the request to start over, once carried out or found
// unreadable. One that can't be removed is logged loudly: the next start would carry it
// out again.
func dropResetRequest(ctx context.Context, path string, logger *slog.Logger) {
	if err := appdata.ClearResetRequest(path); err != nil {
		logger.ErrorContext(ctx, "could not remove the request to start over; remove it by hand, or the next start "+
			"starts over again", "path", path, "error", err)
	}
}
