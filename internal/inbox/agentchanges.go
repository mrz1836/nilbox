package inbox

import (
	"cmp"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/mrz1836/nilbox/internal/fence"
	"github.com/mrz1836/nilbox/internal/inbox/internal/queries"
	"github.com/mrz1836/nilbox/internal/sensitive"
	"github.com/mrz1836/nilbox/internal/store"
)

// maxAgentChangeHeads caps the heads an item keeps what agent runs changed at: each run
// notes two at most, so this is the last several runs.
const maxAgentChangeHeads = 20

// NoteAgentChanges records the files an agent run's commits changed on an item, as git
// listed them, at each of its heads: the head the run started from, whose worktree holds
// its commits whether it pushed or not, and its own last commit. A launch on the item at
// either head then holds for the agent files among them (AgentChanged), whoever opened
// the item. Paths already noted at a head are kept; a run that changed nothing notes
// nothing. The Store is never bound: the agent runs' verifier calls it as a run ends.
func (s *Store) NoteAgentChanges(ctx context.Context, itemID int64, heads, files []string, runID int64) error {
	files = cleanPaths(files)
	if len(files) == 0 {
		return nil
	}

	return store.InTx(ctx, s.db, func(tx *sql.Tx) error {
		q := s.q.WithTx(tx)

		for _, head := range cleanHeads(heads) {
			if err := noteAgentChangesAt(ctx, q, itemID, head, files, runID, s.clock.Now().UTC()); err != nil {
				return err
			}
		}

		return nil
	})
}

// noteAgentChangesAt adds a run's paths to what is noted at one head.
func noteAgentChangesAt(
	ctx context.Context, q *queries.Queries, itemID int64, head string, files []string, runID int64, now time.Time,
) error {
	paths, total, complete := files, len(files), true

	stored, err := q.GetAgentChanges(ctx, queries.GetAgentChangesParams{ItemID: itemID, HeadOID: head})

	switch {
	case errors.Is(err, sql.ErrNoRows):
	case err != nil:
		return fmt.Errorf("read what agent runs changed: %w", err)
	default:
		before, decodeErr := store.DecodeJSON[[]string](stored.Files)
		if decodeErr != nil {
			before = nil
		}

		paths = cleanPaths(append(before, files...))
		total, complete = max(int(stored.Total)+len(files), len(paths)), stored.Complete
	}

	if len(paths) > fence.MaxChanged {
		paths, complete = paths[:fence.MaxChanged], false
	}

	encoded, err := store.EncodeJSON(paths)
	if err != nil {
		return err
	}

	err = q.UpsertAgentChanges(ctx, queries.UpsertAgentChangesParams{
		ItemID: itemID, HeadOID: head, Files: encoded, Total: int64(total), Complete: complete, RunID: max(runID, 0),
		NotedAt: now,
	})
	if err != nil {
		return fmt.Errorf("note what an agent run changed: %w", err)
	}

	if err = q.PruneAgentChanges(ctx, queries.PruneAgentChangesParams{ItemID: itemID, RowOffset: maxAgentChangeHeads}); err != nil {
		return fmt.Errorf("prune what agent runs changed: %w", err)
	}

	return nil
}

// AgentChanged returns what agent runs changed on an item at a head (its stored head for
// ""), as a file check with the user's categories applied and the fence's denied ones
// always on, and whether any run noted anything there.
func (s *Store) AgentChanged(ctx context.Context, itemID int64, head string) (sensitive.Check, bool, error) {
	item, err := s.q.GetCheckItem(ctx, itemID)
	if err != nil {
		return sensitive.Check{}, false, fmt.Errorf("read the item: %w", err)
	}

	status, err := readStatus(ctx, s.prefs)
	if err != nil {
		return sensitive.Check{}, false, err
	}

	head = strings.ToLower(strings.TrimSpace(cmp.Or(head, store.Deref(item.HeadOID))))
	if head == "" {
		return sensitive.Check{}, false, nil
	}

	stored, err := s.q.GetAgentChanges(ctx, queries.GetAgentChangesParams{ItemID: itemID, HeadOID: head})
	if errors.Is(err, sql.ErrNoRows) {
		return sensitive.Check{}, false, nil
	}

	if err != nil {
		return sensitive.Check{}, false, fmt.Errorf("read what agent runs changed: %w", err)
	}

	paths, err := store.DecodeJSON[[]string](stored.Files)
	if err != nil {
		return sensitive.Check{}, false, fmt.Errorf("read what agent runs changed: %w", err)
	}

	check := fence.CheckChanged(head, paths, sensitive.DecodeConfig(status))
	if !stored.Complete || int(stored.Total) > len(paths) {
		check.Complete = false
		check.Total = max(int(stored.Total), len(paths))
	}

	return check, true, nil
}

// cleanPaths returns the non-empty paths, each once, in order.
func cleanPaths(files []string) []string {
	out := []string{}

	for _, f := range files {
		if f = strings.TrimSpace(f); f != "" && !slices.Contains(out, f) {
			out = append(out, f)
		}
	}

	return out
}

// cleanHeads returns the non-empty heads, lowercased, each once.
func cleanHeads(heads []string) []string {
	out := []string{}

	for _, h := range heads {
		if h = strings.ToLower(strings.TrimSpace(h)); h != "" && !slices.Contains(out, h) {
			out = append(out, h)
		}
	}

	return out
}
