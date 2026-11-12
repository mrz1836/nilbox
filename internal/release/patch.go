package release

import (
	"context"
	"slices"

	"github.com/mrz1836/nilbox/internal/event"
	"github.com/mrz1836/nilbox/internal/repo"
)

// queuePatch asks the Refresher to read the released repository name again and patch its
// rows into the Release page's list (patchReleased), rather than put the whole list out of
// date: a release changes its own repository's rows alone, so the rest stay as current as
// they were, and the page isn't read again in full for one repository. Meanwhile the list
// is served as refreshing, and the window hears so now, so the page shows the read's
// progress until the patch lands. Without the Refresher running, the list goes out of date
// at once, and the window hears so now: the page then reads it again in full.
//
// A release's Finish calls it (released), once magex has ended, so the clone the patch
// reads is as the release left it. The patch only reads the clone, so the run's hold on
// it, let go of just after, keeps nothing from it; and the Refresher, a desktop worker,
// reads as tracked work, holding up neither the run's record nor the app's quitting.
func (s *Service) queuePatch(name repo.Name) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.background {
		s.outOfDate()
		s.events.Emit(event.ReleasesUpdated{})

		return
	}

	s.askPatch(name.Key())
}

// queueMerged asks the Refresher to read name again and patch its rows into the list, as
// queuePatch does for a release, once a pull request merged into it in the review sheet:
// the merge may give its default branch commits to release, which the list, read again in
// full only when the user asks, would otherwise never show. Unlike a release's, it asks
// only while the list could be patched now (patchable), and otherwise leaves the list as
// it is: a merge into a repository the page doesn't list, or into a list read again in
// full next anyway, changes nothing the page would show. Once asked, the patch is read as
// a release's is, and one that fails puts the list out of date.
func (s *Service) queueMerged(name repo.Name) {
	s.mu.Lock()
	defer s.mu.Unlock()

	key := name.Key()
	if s.background && s.patchable(s.root, map[string]bool{key: true}) != nil {
		s.askPatch(key)
	}
}

// askPatch marks the repository key to be read again, serves the list as refreshing, tells
// the window so, and wakes the Refresher. The caller holds mu.
func (s *Service) askPatch(key string) {
	s.unpatched[key] = true
	s.patching = true
	s.events.Emit(event.ReleasesUpdated{})

	select {
	case s.wakePatch <- struct{}{}:
	default:
	}
}

// patchReleased reads the repositories released, or merged into, since the last patch
// again and patches their rows into the list (patch), once the read of the list under
// way, a refresh or a patch, has landed (startReading): a refresh that asked GitHub before
// a release ended lands first, and the patch then lands over it, while one that started
// after the release read its repository already, so the patch leaves that one out. A list
// that can't be patched goes out of date instead, so the next Candidates reads it again in
// full. Either way the list is no longer served as refreshing, unless another release
// ended meanwhile, and the window hears (event.ReleasesUpdated).
func (s *Service) patchReleased(ctx context.Context) {
	if s.startReading(ctx) != nil {
		return // the app quits; Run puts the list out of date for what's left
	}

	defer s.doneReading()

	s.mu.Lock()
	names := s.unpatched
	s.unpatched = map[string]bool{}
	s.mu.Unlock()

	patched := len(names) == 0 || s.patch(ctx, names)

	s.mu.Lock()
	defer s.mu.Unlock()

	if !patched {
		s.outOfDate()
	}

	s.patching = len(s.unpatched) > 0
	s.events.Emit(event.ReleasesUpdated{})
}

// patch reads the repositories names keys again, as a refresh does: GitHub's status of
// each, the comparison its newest version needs (compareRequests), and the state of each
// of their clones the list holds, telling the window how far it has got
// (event.ReleasesProgress). It patches those rows into the last list, which is kept for
// the next start. The rest of the list, and when GitHub was last asked, stay as they were.
// It reports whether it patched: not when the list can't be patched (patchable), GitHub
// can't be read, or ctx ends first.
func (s *Service) patch(ctx context.Context, names map[string]bool) bool {
	roots, err := s.clones.Roots(ctx)
	if err != nil {
		return false
	}

	root := roots.Key()

	s.mu.Lock()
	listed := s.patchable(root, names)
	s.mu.Unlock()

	if len(listed) == 0 {
		return false
	}

	progress := newListProgress(s.events, s.progressEvery)
	defer progress.End()

	statuses, compares, err := s.askGitHub(ctx, listed, progress)
	if err != nil {
		if ctx.Err() == nil {
			s.logger.WarnContext(ctx, "couldn't read a released repository again; the list will be read again in full",
				"error", err)
		}

		return false
	}

	rows := s.candidates(listed, statuses, compares)
	s.checkClones(ctx, rows, statuses, progress)

	if ctx.Err() != nil {
		return false // a clone read as ctx ended is marked unreadable
	}

	// No refresh ran meanwhile (startReading), so the list is the one listed was taken from.
	s.mu.Lock()
	defer s.mu.Unlock()

	list := s.last
	list.Repos = slices.DeleteFunc(slices.Clone(s.last.Repos), func(c Candidate) bool { return names[c.origin.Key()] })
	list.Repos = append(list.Repos, rows...)
	sortCandidates(list.Repos)

	s.last = list
	s.keep(context.WithoutCancel(ctx), list, root)

	return true
}

// patchable returns the clones the last list holds of names, when it can be patched: it
// was read from the locations root keys, the repository locations as set now; it is
// current, however old, since one
// out of date is read again in full by the next Candidates anyway; it was read from GitHub
// in this run of the app, and its last read didn't fail, since the next read of a list
// kept from a past start or one that failed is a full one, which the page asks for (the
// read behind a kept list) or shows the failure until; and it lists every one of names,
// since a repository it doesn't list takes a scan of the locations. Otherwise it returns
// none. The caller holds mu.
func (s *Service) patchable(root string, names map[string]bool) []clone {
	if !s.has || s.root != root || !s.current || s.kept || s.failure != "" {
		return nil
	}

	listed := make([]clone, 0, len(names))
	seen := make(map[string]bool, len(names))

	for _, c := range s.last.Repos {
		if key := c.origin.Key(); names[key] {
			listed = append(listed, clone{dir: c.dir, origin: c.origin})
			seen[key] = true
		}
	}

	if len(seen) < len(names) {
		return nil
	}

	return listed
}

// outOfDate puts the last list out of date, so the next Candidates reads GitHub again,
// and so does a refresh under way, which lands out of date too: it may have asked GitHub
// before what put the list out of date happened. The caller holds mu.
func (s *Service) outOfDate() {
	s.current = false
	s.outdated++
}
