package inbox

import (
	"context"
	"errors"
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
// the last whole refresh that stored, the last watch pass's answer to each search, and
// whether the last refresh found no one signed in.
type ReadStatus struct {
	Viewer      string
	LastRefresh time.Time

	// WatchedAt is when the last watch pass stored, Paused why the last refresh sent no
	// searches, and Searches each search's last answer, by key, to the nanosecond, so an
	// answer can be told from a change a moment before it.
	WatchedAt time.Time
	Paused    string
	Searches  map[string]SearchRead

	// SignedOut reports that the last whole refresh failed for want of a sign-in: the
	// GitHub CLI has no token, isn't there, or GitHub refused the token. The next refresh
	// that stores clears it.
	SignedOut bool
}

// SearchRead is one routine search's last answer, as ReadStatus reads it.
type SearchRead struct {
	Answered  bool
	At        time.Time
	Error     string
	Truncated bool
	Partial   bool
}

// WatchHolder is a WatchSource that says whether this copy of the app reads the routines'
// searches at all: only the copy that runs Autopilot's engine does, so two copies on one
// data folder never replace each other's rows, and the other sends nothing.
type WatchHolder interface {
	HoldsWatches() bool
}

// holdsWatches reports whether this copy reads src's searches: always for a source that
// says nothing of it.
func holdsWatches(src WatchSource) bool {
	h, ok := src.(WatchHolder)

	return !ok || h.HoldsWatches()
}

// signedOutText is what prefSignedOut holds after a refresh that failed with err: "1" for
// one that found no one signed in, else "".
func signedOutText(err error) string {
	if errors.Is(err, github.ErrNotLoggedIn) || errors.Is(err, github.ErrGHNotFound) || github.IsBadCredentials(err) {
		return "1"
	}

	return ""
}

// ReadStatus returns when the inbox last read what the routines watch. It reads the
// database alone.
func (s *Store) ReadStatus(ctx context.Context) (ReadStatus, error) {
	status, err := readStatus(ctx, s.prefs)
	if err != nil {
		return ReadStatus{}, err
	}

	watches, err := readWatchState(ctx, s.prefs)
	if err != nil {
		return ReadStatus{}, err
	}

	signedOut, _, err := s.prefs.Get(ctx, prefSignedOut)
	if err != nil {
		return ReadStatus{}, fmt.Errorf("read whether anyone is signed in: %w", err)
	}

	out := ReadStatus{
		Viewer: status[prefViewer], WatchedAt: watches.At, Paused: watches.Paused, SignedOut: signedOut != "",
		Searches: make(map[string]SearchRead, len(watches.Searches)),
	}

	if at, parseErr := time.Parse(time.RFC3339, status[prefLastRefreshAt]); parseErr == nil {
		out.LastRefresh = at
	}

	for key, st := range watches.Searches {
		read := SearchRead{Answered: st.Answered, Error: st.Error, Truncated: st.Truncated, Partial: st.Partial}
		if st.At != nil {
			read.At = *st.At
		}

		out.Searches[key] = read
	}

	return out, nil
}

// WatchNow sends the routines' searches now, outside a whole refresh, as a refresh's watch
// pass sends them, and waits for the pass to store what they watch: a routine turned on
// with a search no pass has read yet reads it at once, for a point or so, rather than at
// the next refresh. It sends nothing while a pass runs, before an account signed in, or
// while the routines' searches can't be told for that account yet; the pass runs on the
// watch passes' worker, so nothing reads them without it.
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
