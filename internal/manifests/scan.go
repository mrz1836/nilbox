package manifests

import (
	"cmp"
	"context"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/mrz1836/nilbox/internal/event"
	"github.com/mrz1836/nilbox/internal/localrepos"
	"github.com/mrz1836/nilbox/internal/paths"
	"github.com/mrz1836/nilbox/internal/progress"
	"github.com/mrz1836/nilbox/internal/reporules"
	"github.com/mrz1836/nilbox/internal/uierr"
)

// dirOutcome is what came of one clone in a read of the clones.
type dirOutcome int

// The outcomes of a clone: read, left out as muted, or left out as not on GitHub.
const (
	outcomeRead dirOutcome = iota
	outcomeMuted
	outcomeOffGitHub
)

// dirRead is one clone as a read of the clones found it: its outcome, and, for one read,
// its folder resolved and its row.
type dirRead struct {
	outcome dirOutcome
	key     string
	row     Clone
}

// scanAll reads every clone in roots under rules and keeps the page it makes. A read that
// fails keeps the last page, with why, unless there is none to keep, or the locations
// changed. A read cut short, as by ScanTimeout or the app's quit, keeps no page.
func (s *Service) scanAll(ctx context.Context, roots localrepos.Roots, rules reporules.Rules) error {
	page, read, err := s.scan(ctx, roots, rules)
	if err == nil {
		err = ctx.Err()
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if ctx.Err() != nil {
		return ctx.Err()
	}

	if err != nil {
		if !s.has || !s.roots.Equal(roots) {
			return err
		}

		s.logger.WarnContext(ctx, "couldn't read the clones' manifests; showing the last read", "error", err)
		s.failure = uierr.Message(err)

		return nil
	}

	s.has, s.roots, s.ruled, s.scanned, s.failure, s.page = true, roots, rules, s.clock.Now(), "", page

	for key := range s.kept {
		if !read[key] {
			delete(s.kept, key) // a clone no longer read keeps nothing
		}
	}

	return nil
}

// scan lists the clones in roots and reads each, scanParallel at a time, telling the
// window how far it has got, and returns the page and the clones it read, by folder
// resolved.
func (s *Service) scan(ctx context.Context, roots localrepos.Roots, rules reporules.Rules) (Page, map[string]bool, error) {
	told := progress.New(s.events, progress.Every, func(phase event.ManifestsPhase, done, total int) event.Event {
		return event.ManifestsProgress{Phase: phase, Done: done, Total: total}
	})
	defer told.End()

	told.Start(event.ManifestsFindingClones, 0)

	dirs, err := s.clones.Dirs(ctx, roots)
	if err != nil {
		return Page{}, nil, uierr.New("Couldn't list the clones in "+roots.Where(s.home)+".", err)
	}

	told.Start(event.ManifestsReadingClones, len(dirs))

	reads := make([]dirRead, len(dirs))

	parallel(dirs, func(i int, dir string) {
		reads[i] = s.readDir(ctx, dir, rules)

		told.Add(1)
	})

	read := map[string]bool{}

	for _, r := range reads {
		if r.outcome == outcomeRead {
			read[r.key] = true
		}
	}

	return assemble(reads), read, nil
}

// parallel calls fn for each of dirs, scanParallel at a time, and returns once all have.
func parallel(dirs []string, fn func(i int, dir string)) {
	var wg sync.WaitGroup

	slots := make(chan struct{}, scanParallel)

	for i, dir := range dirs {
		slots <- struct{}{}

		wg.Go(func() {
			defer func() { <-slots }()

			fn(i, dir)
		})
	}

	wg.Wait()
}

// readDir reads the clone at dir under rules, within cloneTimeout, so one on a disk that
// stopped answering doesn't hold up the rest: left out when its origin isn't on GitHub,
// or can't be read, or its repository is muted; read otherwise, its repository marked
// when it is pinned.
func (s *Service) readDir(ctx context.Context, dir string, rules reporules.Rules) dirRead {
	ctx, cancel := context.WithTimeout(ctx, cloneTimeout)
	defer cancel()

	name, onGitHub, err := s.clones.Origin(ctx, dir)

	switch {
	case err != nil || !onGitHub:
		return dirRead{outcome: outcomeOffGitHub}
	case rules.IsMuted(name):
		return dirRead{outcome: outcomeMuted}
	}

	c := candidate{dir: dir, key: paths.Resolved(dir), name: name, pinned: rules.IsPinned(name)}

	return dirRead{outcome: outcomeRead, key: c.key, row: s.readClone(ctx, c)}
}

// assemble makes the page of reads: the clones that hold a module or couldn't be read,
// stalest first, and how many were left out.
func assemble(reads []dirRead) Page {
	page := emptyPage()

	for _, r := range reads {
		switch {
		case r.outcome == outcomeMuted:
			page.Muted++
		case r.outcome == outcomeOffGitHub:
			page.WithoutGitHub++
		case len(r.row.Modules) == 0 && r.row.Error == "":
			page.WithoutModules++
		default:
			page.Clones = append(page.Clones, r.row)
			page.Modules += len(r.row.Modules)
		}
	}

	slices.SortStableFunc(page.Clones, func(a, b Clone) int {
		return cmp.Or(compareAges(a.oldest.IsZero(), b.oldest.IsZero(), a.oldest.Compare(b.oldest)),
			cmp.Compare(strings.ToLower(a.Repo), strings.ToLower(b.Repo)), cmp.Compare(a.Dir, b.Dir))
	})

	return page
}

// oldestOf returns the stalest of modules' ages, sorted as sortModules sorts them, or the
// zero time when none is known.
func oldestOf(modules []Module) time.Time {
	if len(modules) == 0 {
		return time.Time{}
	}

	return modules[0].changed
}
