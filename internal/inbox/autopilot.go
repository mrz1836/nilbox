package inbox

import (
	"context"
	"fmt"
	"time"

	"github.com/mrz1836/nilbox/internal/github"
	"github.com/mrz1836/nilbox/internal/item"
)

// The inbox's reads for Autopilot's engine: what is stored of the items' states, and when
// the inbox last read what the routines watch; and the routines' searches sent at once,
// outside a whole refresh.

// StoredState is what is stored of one item's state, for Autopilot's engine.
type StoredState struct {
	// Ref names the item.
	Ref item.Ref

	// State is OPEN, CLOSED, or MERGED as the last read that carries it said, "" before
	// one did.
	State string

	// FetchedAt is when a read last brought it; Listed and Watched report it in one of the
	// inbox's lists, and returned by a routine's search.
	FetchedAt time.Time
	Listed    bool
	Watched   bool
}

// ItemStates returns what is stored of each item among ids' state, by id; an id with no
// stored item is left out. It reads the database alone.
func (s *Store) ItemStates(ctx context.Context, ids []int64) (map[int64]StoredState, error) {
	out := make(map[int64]StoredState, len(ids))
	if len(ids) == 0 {
		return out, nil
	}

	rows, err := s.q.ListItemStates(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("read the items' states: %w", err)
	}

	for _, r := range rows {
		out[r.ID] = StoredState{
			Ref: refOf(r.RepoOwner, r.RepoName, r.Number, item.Kind(r.Kind)), State: r.State, FetchedAt: r.FetchedAt.UTC(),
			Listed: r.Listed, Watched: r.Watched,
		}
	}

	return out, nil
}

// ReadStatus is when the inbox last read what the routines watch: the account signed in,
// the last whole refresh that stored, and the last watch pass's answer to each search.
type ReadStatus struct {
	Viewer      string
	LastRefresh time.Time
	Watches     WatchStatus
	WatchedAt   time.Time
}

// ReadStatus returns when the inbox last read what the routines watch. It reads the
// database alone.
func (s *Store) ReadStatus(ctx context.Context) (ReadStatus, error) {
	status, err := readStatus(ctx, s.prefs)
	if err != nil {
		return ReadStatus{}, err
	}

	watches, err := s.WatchStatus(ctx)
	if err != nil {
		return ReadStatus{}, err
	}

	out := ReadStatus{Viewer: status[prefViewer], Watches: watches}

	if at, parseErr := time.Parse(time.RFC3339, status[prefLastRefreshAt]); parseErr == nil {
		out.LastRefresh = at
	}

	if at, parseErr := time.Parse(time.RFC3339, watches.At); parseErr == nil {
		out.WatchedAt = at
	}

	return out, nil
}

// WatchNow sends the routines' searches now, outside a whole refresh, as a refresh's watch
// pass sends them, and waits for the pass to store what they watch: a routine turned on
// with a search no pass has read yet reads it at once, for a point or so, rather than at
// the next refresh. It sends nothing while a pass runs, and nothing before an account
// signed in; the pass runs on the watch passes' worker, so nothing reads them without it.
func (r *Routines) WatchNow(ctx context.Context) error {
	s := r.svc
	started := s.now()

	job := s.startWatchPass(github.WithPurpose(ctx, github.PurposeAutopilot))
	if job == nil {
		return nil
	}

	job.release()

	_, err := s.awaitPass(ctx, started)

	return err
}
