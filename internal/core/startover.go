package core

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/mrz1836/nilbox/internal/appdata"
	"github.com/mrz1836/nilbox/internal/store"
)

// openData opens the database, starting over first when the user asked to: a reset
// request beside it, from Settings → About or magex app:reset. Then the database is set
// aside as <db>.reset-<stamp> for a fresh one (store.OpenFresh), whose start brings the
// prompts, the launch defaults, and the user's own scripts back from it, and the newest
// few such copies are kept; Describe it's conversations and proposals, which name routines
// that are gone, are removed. A request that can't be read is logged and dropped, never
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

	forgetConversations(ctx, p.Describe, logger)
	dropResetRequest(ctx, p.ResetRequest, logger)
	logger.InfoContext(ctx, "started over, as the user asked", "by", req.By, "asked_at", req.RequestedAt)

	return opened, nil
}

// describeForgotten are the folders of Describe it's folder a start over removes, as its
// layout names them (internal/describeit): the conversations, and the proposals, both the
// conversations' and those from the user's own Claude Code, which name routines that are
// gone. The plugin, its link, and the agent chosen for new conversations stay.
func describeForgotten() []string {
	return []string{"conversations", "proposals"}
}

// forgetConversations removes Describe it's conversations and proposals once the app has
// started over, logging what it couldn't remove: the worker makes the folders it needs
// again as it starts.
func forgetConversations(ctx context.Context, describe string, logger *slog.Logger) {
	if describe == "" {
		return
	}

	for _, name := range describeForgotten() {
		dir := filepath.Join(describe, name)
		if err := os.RemoveAll(dir); err != nil {
			logger.WarnContext(ctx, "could not remove Describe it's conversations from before the start over",
				"path", dir, "error", err)
		}
	}
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
