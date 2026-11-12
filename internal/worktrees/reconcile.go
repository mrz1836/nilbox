package worktrees

import (
	"cmp"
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/mrz1836/nilbox/internal/clock"
	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/paths"
	"github.com/mrz1836/nilbox/internal/worklog"
)

// reconcile checks every record against the worktrees git listed, and returns the records
// whose worktree is there, keyed by the worktree's resolved path, with the rows they name
// marked as recorded:
//
//   - a record is matched to a worktree by its directory, else by its branch in its
//     clone, which catches a worktree moved with `git worktree move`, whose record then
//     follows it;
//   - a record whose worktree is gone (removed outside the app, or pruned), or whose clone
//     is gone, is dropped;
//   - a record whose clone was not listed, or could not be, is kept as it is, since
//     nothing says its worktree is gone.
//
// A failed write only costs a stale record until the next listing, so it is logged.
func (s *Service) reconcile(ctx context.Context, listings []cloneListing, records []worklog.Record) map[string]worklog.Record {
	live := map[string]worklog.Record{}

	for _, r := range records {
		l := listingOf(listings, r.ClonePath)

		switch {
		case l == nil:
			if cloneGone(r.ClonePath) {
				s.forget(ctx, r)
			}

			continue
		case l.repo.repo.Error != "":
			continue
		}

		wt := recordRow(l, r)
		if wt == nil {
			s.forgetStale(ctx, r)

			continue
		}

		if !paths.Same(wt.Path, r.Dir()) {
			r.Path, r.Branch = wt.Path, wt.Branch
			s.rerecord(ctx, r)
		}

		markRecorded(wt, r)
		live[paths.Resolved(wt.Path)] = r
	}

	return live
}

// adopt records each worktree that has no record but is the app's or on an item's branch,
// once GitHub has linked it to its item, so the next launch on the item resumes it and the
// automatic cleanup finds its clone. A worktree for an item that already has a recorded
// worktree in the clone is left alone.
func (s *Service) adopt(ctx context.Context, repos []Repo, recorded map[string]worklog.Record) {
	if s.launches == nil {
		return
	}

	taken := map[string]bool{}
	for _, r := range recorded {
		taken[paths.Resolved(r.ClonePath)+"\x00"+r.Worktree] = true
	}

	for repo, wt := range rows(repos) {
		if !adoptable(wt, recorded) {
			continue
		}

		name := item.WorktreeName(wt.Item.Kind, wt.Item.Number)
		key := paths.Resolved(repo.Path) + "\x00" + name

		if taken[key] {
			continue
		}

		r := worklog.Record{
			ClonePath: repo.Path, Worktree: name, Path: wt.Path, Branch: wt.Branch, Owner: wt.Item.Owner,
			Name: wt.Item.Name, Kind: wt.Item.Kind, Number: wt.Item.Number, CreatedAt: s.clock.Now(),
		}

		if err := s.launches.Record(ctx, r); err != nil {
			s.logger.WarnContext(ctx, "could not record a worktree", "error", err)

			continue
		}

		taken[key] = true
		recorded[paths.Resolved(wt.Path)] = r
		markRecorded(wt, r)
	}
}

// adoptable reports a linked worktree with no record that the app made, Claude Code made,
// or that has an item's own branch checked out.
func adoptable(wt *Worktree, recorded map[string]worklog.Record) bool {
	if wt.Main || wt.Prunable || wt.Item == nil {
		return false
	}

	if _, ok := recorded[paths.Resolved(wt.Path)]; ok {
		return false
	}

	return wt.Managed || ownBranchItem(wt.Branch)
}

// ownBranchItem reports an item's own branch, worktree-pr-<n> or worktree-issue-<n>.
func ownBranchItem(branch string) bool {
	name, ok := strings.CutPrefix(branch, ownBranchPrefix)
	if !ok {
		return false
	}

	_, _, ok = item.ParseWorktreeName(name)

	return ok
}

// markRecorded fills in what a record says about the row of its worktree.
func markRecorded(wt *Worktree, r worklog.Record) {
	wt.Managed = true
	wt.Agent = r.Agent
	wt.Launches = r.Launches
	wt.OwnBranch = !wt.Detached && wt.Branch == ownBranchPrefix+r.Worktree

	if !r.LastAgentAt.IsZero() {
		wt.LastLaunchedAt = clock.Format(r.LastAgentAt)
	}
}

// listingOf returns the listing of the clone at clonePath, or nil when it was not listed.
func listingOf(listings []cloneListing, clonePath string) *cloneListing {
	key := paths.Resolved(clonePath)

	for i := range listings {
		if l := &listings[i]; paths.Resolved(l.repo.repo.Path) == key || l.repo.key == key {
			return l
		}
	}

	return nil
}

// recordRow returns the row of the worktree a record names in its clone's listing: the
// live worktree at its directory, else the one with its branch checked out, or nil.
func recordRow(l *cloneListing, r worklog.Record) *Worktree {
	branch := cmp.Or(r.Branch, ownBranchPrefix+r.Worktree)

	for _, match := range []func(*Worktree) bool{
		func(wt *Worktree) bool { return paths.Same(wt.Path, r.Dir()) },
		func(wt *Worktree) bool { return wt.Branch == branch },
	} {
		for i := range l.rows {
			if wt := &l.rows[i]; !wt.Main && !wt.Prunable && match(wt) {
				return wt
			}
		}
	}

	return nil
}

// cloneGone reports a clone directory that is certainly gone.
func cloneGone(path string) bool {
	_, err := os.Stat(path)

	return errors.Is(err, fs.ErrNotExist)
}

// forgetStale forgets a record the listing found no worktree for, once git still has
// none under the clone's lock. A launch holds that lock from finding the item's worktree
// to recording it, so a listing that ran before the launch added the worktree never
// forgets the record the launch just wrote. When git cannot say, or another job holds the
// clone, the record stays for the next listing to decide: a listing never waits.
func (s *Service) forgetStale(ctx context.Context, r worklog.Record) {
	unlock, ok := s.tryClone(r.ClonePath)
	if !ok {
		return
	}

	defer unlock()

	g, err := s.git()
	if err != nil {
		return
	}

	entries, _, err := g.list(ctx, r.ClonePath)
	if err != nil {
		return
	}

	l := &cloneListing{rows: make([]Worktree, 0, len(entries))}
	for _, e := range entries {
		l.rows = append(l.rows, Worktree{Path: e.Path, Branch: e.Branch, Main: e.Main, Prunable: e.Prunable})
	}

	if recordRow(l, r) == nil {
		s.forget(ctx, r)
	}
}

// forget drops a record whose worktree is gone.
func (s *Service) forget(ctx context.Context, r worklog.Record) {
	if err := s.launches.Forget(ctx, r.ClonePath, r.Worktree); err != nil {
		s.logger.WarnContext(ctx, "could not forget a worktree record", "error", err)
	}
}

// rerecord stores a record whose worktree moved.
func (s *Service) rerecord(ctx context.Context, r worklog.Record) {
	if err := s.launches.Record(ctx, r); err != nil {
		s.logger.WarnContext(ctx, "could not record where a worktree moved", "error", err)
	}
}

// managedClone reports a clone with worktrees the app made, or Claude Code made, found
// without running git: its managed root or its .claude/worktrees holds an entry.
func managedClone(dir string) bool {
	return hasEntries(ManagedRoot(dir)) || hasEntries(legacyRoot(dir))
}

// hasEntries reports a directory with at least one entry.
func hasEntries(dir string) bool {
	f, err := os.Open(filepath.Clean(dir))
	if err != nil {
		return false
	}

	defer func() { _ = f.Close() }()

	names, err := f.Readdirnames(1)

	return err == nil && len(names) > 0
}
