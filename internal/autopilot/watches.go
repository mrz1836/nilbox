package autopilot

import (
	"context"
	"slices"
	"strings"
	"sync/atomic"
	"time"

	"github.com/mrz1836/nilbox/internal/autopilot/catalog"
	"github.com/mrz1836/nilbox/internal/autopilot/spec"
	"github.com/mrz1836/nilbox/internal/autopilot/watch"
)

// The routines' searches reach the inbox as one snapshot, a WatchSet, which the Service
// builds again from the stored routines after every change to one, one build at a time,
// and keeps in an atomic value: each inbox refresh reads it as it starts, and waits on
// nothing of Autopilot's but, when the account signed in changed, a build of its own.
// Only the enabled routines of the account signed in, that load, are in it, and none while
// Autopilot is off; a search two routines share is in it once, under its key.

// WatchSearch is one routine search the inbox reads beside its own lists.
type WatchSearch struct {
	// Key is what the routines that share the search share: its query, written in one
	// canonical order (watch.Query.Key).
	Key string

	// Query is what GitHub is sent, with "your account" written as the account's login.
	Query string

	// Follow says the checks follower follows the checks of what it returns: one of its
	// routines' triggers reads checks.
	Follow bool

	// Routines are the routines that use it, by ID.
	Routines []int64
}

// WatchSet is the routines' searches for one account, as of the last change to a routine.
type WatchSet struct {
	// Login is the account the searches were built for; "" before one signed in.
	Login string

	// Searches are the searches, each key once, in the routines' order.
	Searches []WatchSearch

	// BuiltAt is when the set was built.
	BuiltAt time.Time
}

// watchSet holds the Service's snapshot. build is held, as a one-slot channel, from a
// rebuild's read of the routines to its store, so rebuilds run one at a time and the last
// to read stores last: two side by side could otherwise store the older read over the
// newer.
type watchSet struct {
	current atomic.Pointer[WatchSet]
	build   chan struct{}
}

// Searches is the face of the routines' searches the inbox reads, which the window never
// reaches (the Service is bound, so these are not its methods). Core hands the inbox a
// WatchSource over it.
type Searches struct {
	svc *Service
}

// NewSearches returns the face of s's routines' searches.
func NewSearches(s *Service) *Searches {
	return &Searches{svc: s}
}

// Current returns the routines' searches as of the last change to a routine, at once.
func (w *Searches) Current() WatchSet {
	return w.svc.currentWatches()
}

// Rebuild builds the routines' searches again from the stored routines, keeps them, and
// returns them, as a change to a routine does: once at startup, and when the account
// signed in changed since the last build.
func (w *Searches) Rebuild(ctx context.Context) WatchSet {
	return w.svc.rebuildWatches(ctx)
}

// currentWatches returns the routines' searches as of the last change to a routine, at
// once: it reads the snapshot rebuildWatches keeps. Before the first build it is empty.
func (s *Service) currentWatches() WatchSet {
	if set := s.watches.current.Load(); set != nil {
		return *set
	}

	return WatchSet{Searches: []WatchSearch{}}
}

// rebuildWatches builds the snapshot of the routines' searches again from the stored
// routines and the account signed in, keeps it, and returns it. A routine that is off,
// broken, of another account, or whose search won't build for the account is left out,
// and every routine while Autopilot is off. It reads the database, a few milliseconds'
// work, one rebuild at a time (watchSet.build); a read that fails, or a ctx that ends while
// another rebuild runs, keeps the snapshot as it was, logged.
func (s *Service) rebuildWatches(ctx context.Context) WatchSet {
	select {
	case s.watches.build <- struct{}{}:
		defer func() { <-s.watches.build }()
	case <-ctx.Done():
		s.logger.WarnContext(ctx, "gave up building the routines' searches", "error", ctx.Err())

		return s.currentWatches()
	}

	on, err := s.enabled(ctx)
	if err != nil {
		s.logger.WarnContext(ctx, "could not read whether Autopilot is on for the routines' searches", "error", err)

		return s.currentWatches()
	}

	rows, err := s.store.all(ctx)
	if err != nil {
		s.logger.WarnContext(ctx, "could not read the routines for their searches", "error", err)

		return s.currentWatches()
	}

	login := s.viewer(ctx)
	set := WatchSet{Login: login, Searches: []WatchSearch{}, BuiltAt: s.clock.Now().UTC()}
	byKey := map[string]int{}

	for _, row := range rows {
		if !on || !row.Enabled || otherAccount(row.AccountLogin, login) {
			continue
		}

		if l := load(row.Spec); l.broken == nil {
			set.add(row.ID, l.spec, byKey)
		}
	}

	s.watches.current.Store(&set)

	return set
}

// add adds the searches of the routine id, whose spec is sp, each key once: one another
// routine has already is shared, and follows the checks when either routine's trigger
// reads them. byKey holds each key's place in the set.
func (set *WatchSet) add(id int64, sp spec.Spec, byKey map[string]int) {
	for _, q := range searchesOf(sp, set.Login) {
		i, ok := byKey[q.Key]
		if !ok {
			i = len(set.Searches)
			byKey[q.Key] = i
			set.Searches = append(set.Searches, WatchSearch{Key: q.Key, Query: q.Text, Routines: []int64{}})
		}

		w := &set.Searches[i]
		w.Follow = w.Follow || readsChecks(sp.Trigger.Event)

		if !slices.Contains(w.Routines, id) {
			w.Routines = append(w.Routines, id)
		}
	}
}

// searchesOf builds a spec's searches for the account login, each once; one that won't
// build for it (your account with no one signed in) is left out.
func searchesOf(sp spec.Spec, login string) []watch.Query {
	var out []watch.Query

	for _, src := range sp.Watch.Sources {
		if src.Search == nil {
			continue
		}

		q, err := watch.Build(*src.Search, login)
		if err != nil {
			continue
		}

		if !slices.ContainsFunc(out, func(o watch.Query) bool { return o.Key == q.Key }) {
			out = append(out, q)
		}
	}

	return out
}

// listsOf returns the inbox lists a spec watches, each once.
func listsOf(sp spec.Spec) []catalog.ListSource {
	var out []catalog.ListSource

	for _, src := range sp.Watch.Sources {
		for _, l := range src.Lists {
			if !slices.Contains(out, l) {
				out = append(out, l)
			}
		}
	}

	return out
}

// readsChecks reports a trigger that fires on the checks, whose routine's items the checks
// follower follows.
func readsChecks(e catalog.Event) bool {
	return e == catalog.EventChecksFailed || e == catalog.EventChecksPassed
}

// sameLogin reports whether two logins name one account, ignoring case.
func sameLogin(a, b string) bool {
	return strings.EqualFold(a, b)
}
