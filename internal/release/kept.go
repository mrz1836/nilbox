package release

import (
	"context"
	"fmt"
	"time"

	"github.com/mrz1836/nilbox/internal/event"
	"github.com/mrz1836/nilbox/internal/repo"
	"github.com/mrz1836/nilbox/internal/store"
)

// keptKey is the pref the last list is kept under between starts, with the clone root it
// was read from. The release service alone writes it.
const keptKey = "release.lastCandidates"

// keptVersion is the shape of the kept list this build writes; a list kept in any other is
// not read.
const keptVersion = 1

// Keeper keeps the last list between starts: core's prefs store. The service reads and
// writes keptKey alone.
type Keeper interface {
	Get(ctx context.Context, key string) (value string, ok bool, err error)
	Set(ctx context.Context, key, value string) error
}

// keptList is the last list as it is kept: its shape's version, the clone root it was read
// from, and the list, each repository with what the page isn't shown of it.
type keptList struct {
	Version       int        `json:"version"`
	Root          string     `json:"root"`
	WithoutGitHub int        `json:"withoutGitHub"`
	CheckedAt     string     `json:"checkedAt"`
	Repos         []keptRepo `json:"repos"`
}

// keptRepo is a listed repository as it is kept: the row the page shows, and its clone's
// absolute folder, its origin, and its newest commit's time, which the row holds only as
// text.
type keptRepo struct {
	Candidate

	Folder string    `json:"folder"`
	Owner  string    `json:"owner"`
	Name   string    `json:"name"`
	Newest time.Time `json:"newest"`
}

// encodeKept writes list, read from root, as it is kept.
func encodeKept(list Candidates, root string) (string, error) {
	kept := keptList{
		Version: keptVersion, Root: root, WithoutGitHub: list.WithoutGitHub, CheckedAt: list.CheckedAt,
		Repos: make([]keptRepo, 0, len(list.Repos)),
	}

	for _, c := range list.Repos {
		kept.Repos = append(kept.Repos, keptRepo{
			Candidate: c, Folder: c.dir, Owner: c.origin.Owner, Name: c.origin.Repo, Newest: c.headAt,
		})
	}

	raw, err := store.EncodeJSON(kept)
	if err != nil {
		return "", fmt.Errorf("encode the kept list: %w", err)
	}

	return raw, nil
}

// decodeKept reads a kept list back: the list as the page lists it, and the clone root it
// was read from. ok is false for a list in another shape.
func decodeKept(raw string) (list Candidates, root string, ok bool, err error) {
	kept, err := store.DecodeJSON[keptList](raw)
	if err != nil {
		return Candidates{}, "", false, fmt.Errorf("read the kept list: %w", err)
	}

	if kept.Version != keptVersion || kept.Root == "" {
		return Candidates{}, "", false, nil
	}

	list = Candidates{Repos: make([]Candidate, 0, len(kept.Repos)), WithoutGitHub: kept.WithoutGitHub, CheckedAt: kept.CheckedAt}

	for _, k := range kept.Repos {
		c := k.Candidate
		c.dir, c.origin, c.headAt = k.Folder, repo.Name{Owner: k.Owner, Repo: k.Name}, k.Newest

		if c.Commits == nil {
			c.Commits = []Commit{}
		}

		if c.Clone != nil && c.Clone.LocalOnly == nil {
			c.Clone.LocalOnly = []string{}
		}

		list.Repos = append(list.Repos, c)
	}

	return list, kept.Root, true, nil
}

// restore reads the list kept from a past start, once, before the first list is served:
// it stands as the last list, from the clone root it was read from, marked as kept, until
// a refresh replaces it. It is never fresh, so serving it reads GitHub again. A kept list
// that can't be read is logged and left; one a refresh already replaced isn't read.
func (s *Service) restore(ctx context.Context) {
	if s.keeper == nil || s.restoredYet() {
		return
	}

	raw, found, err := s.keeper.Get(ctx, keptKey)
	if err != nil && ctx.Err() != nil {
		return // read again by the next call
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.restored || s.has {
		return
	}

	s.restored = true

	list, root, ok, err := keptOf(raw, found, err)
	if err != nil {
		s.logger.WarnContext(ctx, "couldn't read the Release page's kept list", "error", err)
	}

	if ok {
		s.last, s.root, s.has, s.kept = list, root, true, true
	}
}

// restoredYet reports whether the kept list was read, or a refresh made it needless.
func (s *Service) restoredYet() bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.restored || s.has
}

// keptOf reads back what the keeper answered: ok is false for no kept list, one that
// couldn't be read, and one in another shape.
func keptOf(raw string, found bool, err error) (list Candidates, root string, ok bool, _ error) {
	if err != nil {
		return Candidates{}, "", false, fmt.Errorf("read the kept list: %w", err)
	}

	if !found {
		return Candidates{}, "", false, nil
	}

	return decodeKept(raw)
}

// keep writes the list that just landed, read from root, so the next start shows it at
// once. A list that can't be kept is logged: this start has it anyway. The caller holds
// mu, so lists are kept in the order they land.
func (s *Service) keep(ctx context.Context, list Candidates, root string) {
	if s.keeper == nil {
		return
	}

	raw, err := encodeKept(list, root)
	if err == nil {
		err = s.keeper.Set(ctx, keptKey, raw)
	}

	if err != nil {
		s.logger.WarnContext(ctx, "couldn't keep the Release page's list for the next start", "error", err)
	}
}

// Refresher reads the Release page's list again behind the page, when the page was shown
// the list kept from a past start, and reads the repository each release ends in again,
// patching its rows into the list (queuePatch). Meanwhile the list reads as refreshing,
// event.ReleasesProgress tells the page how far the read has got, and
// event.ReleasesUpdated when it lands. The desktop shell runs it as a worker, from start
// until quit; it is not bound. While it isn't running, a page shown the kept list waits for
// GitHub instead, and a release that ends puts the whole list out of date.
type Refresher struct {
	s *Service
}

// NewRefresher returns the worker that reads s's list again behind the page.
func NewRefresher(s *Service) *Refresher {
	return &Refresher{s: s}
}

// Run reads the list again each time the page is shown the kept list, and the released
// repositories each time a release ends, until ctx ends, and returns once the read under
// way has. A read cut short by ctx keeps nothing, and a release it never read again leaves
// the list out of date, no longer refreshing, as one that ends while it isn't running does,
// and the window hears so (event.ReleasesUpdated).
func (r *Refresher) Run(ctx context.Context) {
	s := r.s

	s.mu.Lock()
	s.background = true
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		defer s.mu.Unlock()

		s.background, s.pending = false, false

		if len(s.unpatched) > 0 || s.patching {
			s.unpatched, s.patching = map[string]bool{}, false
			s.outOfDate()
			s.events.Emit(event.ReleasesUpdated{})
		}
	}()

	for {
		select {
		case <-ctx.Done():
			return
		case <-s.wake:
			s.refreshBehind(ctx)
		case <-s.wakePatch:
			s.patchReleased(ctx)
		}
	}
}

// askBehind asks the Refresher to read the list again, unless it already is. The caller
// holds mu.
func (s *Service) askBehind() {
	if s.pending {
		return
	}

	s.pending = true

	select {
	case s.wake <- struct{}{}:
	default:
	}
}

// refreshBehind reads the list again for the page shown the kept list, unless a refresh
// replaced it meanwhile. A refresh already under way, as one the page asked for itself,
// answers it.
func (s *Service) refreshBehind(ctx context.Context) {
	defer func() {
		s.mu.Lock()
		s.pending = false
		s.mu.Unlock()
	}()

	root, err := s.clones.Root(ctx)
	if err != nil {
		s.logger.WarnContext(ctx, "couldn't read the clone root to list the repositories to release", "error", err)

		return
	}

	s.mu.Lock()
	stale := s.has && s.kept && s.root == root
	s.pending = stale
	s.mu.Unlock()

	if !stale {
		return
	}

	// A failure is kept with the list, and logged, by refresh; a cut short one keeps nothing.
	_, _ = s.refreshes.Do(ctx, root, func(ctx context.Context) (Candidates, error) {
		return s.refresh(ctx, root)
	})
}
