package worktrees

import (
	"context"
	"slices"

	"github.com/mrz1836/nilbox/internal/gitread"
	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/paths"
	"github.com/mrz1836/nilbox/internal/worklog"
)

// The copy records.
//
// A worktree fully copied onto a pushed branch (see copies.go) stays copied only while that
// branch is there: a squash merge leaves its copies out of the default branch, and deleting
// the branch then leaves them on no remote-tracking branch, so a later listing finds no copy.
// The pull request keeps them, though: GitHub keeps every pull request's head commit. So
// when a listing finds a worktree fully copied onto a branch whose pull request it knows, it
// records that pull request (worklog.Copy): the worktree's HEAD, the branch, the pull
// request, the commits not on GitHub, and the branch's commit, which moves on with the
// branch while every copy is still on it (CopyTip), so it is most likely the pull request's
// last head. A record describes its worktree only while the worktree's HEAD is the one it
// names: one whose worktree is gone, or whose HEAD moved, is dropped. Nothing else changes a
// record, and nothing reads one but the listing.

// copyRecords reads the copy records. Without them a worktree whose branch is gone is only
// not copied, so a failure is only logged.
func (s *Service) copyRecords(ctx context.Context) []worklog.Copy {
	if s.copyLog == nil {
		return nil
	}

	records, err := s.copyLog.Copies(ctx)
	if err != nil {
		s.logger.WarnContext(ctx, "could not read the worktrees' copy records", "error", err)
	}

	return records
}

// keepCopies checks every copy record against the worktrees listings listed, then records
// each worktree of repos fully copied onto a branch whose pull request this listing knows.
// A record whose clone was listed but has no worktree at its directory, or has one whose
// directory is gone or whose HEAD isn't the record's, is dropped, and so is one whose clone
// is gone from a folder that is still there; one whose clone wasn't listed, or couldn't be,
// is kept as it is. A worktree whose record already says all a new one would is not written
// again. A failed write only costs a record until the next listing, so it is logged.
func (s *Service) keepCopies(ctx context.Context, listings []cloneListing, repos []Repo, records []worklog.Copy) {
	if s.copyLog == nil {
		return
	}

	live := s.liveCopies(ctx, listings, records)

	for repo, wt := range rows(repos) {
		r, ok := copyRecordOf(repo, wt)
		if !ok {
			continue
		}

		if kept, found := live[r.WorktreePath]; found && sameCopy(kept, r) {
			continue
		}

		if err := s.copyLog.RecordCopy(ctx, r); err != nil {
			s.logger.WarnContext(ctx, "could not record the pull request a worktree's commits were copied into",
				"error", err)
		}
	}
}

// liveCopies drops each of records that no longer describes its worktree (copyRecordHolds)
// and returns the others, by their worktree's resolved directory.
func (s *Service) liveCopies(ctx context.Context, listings []cloneListing, records []worklog.Copy) map[string]worklog.Copy {
	live := make(map[string]worklog.Copy, len(records))

	for _, r := range records {
		if copyRecordHolds(listings, r) {
			live[paths.Resolved(r.WorktreePath)] = r
		} else {
			s.forgetCopy(ctx, r.WorktreePath)
		}
	}

	return live
}

// copyRecordHolds reports a copy record that may still describe its worktree: its clone
// wasn't listed, or couldn't be, and isn't gone; or the listing has its worktree, there, at
// the record's HEAD.
func copyRecordHolds(listings []cloneListing, r worklog.Copy) bool {
	l := listingOf(listings, r.ClonePath)

	switch {
	case l == nil:
		return !cloneGone(r.ClonePath)
	case l.repo.repo.Error != "":
		return true
	}

	i := slices.IndexFunc(l.rows, func(wt Worktree) bool { return !wt.Main && paths.Same(wt.Path, r.WorktreePath) })

	return i >= 0 && !l.rows[i].Prunable && gitread.IsCommitID(r.Head) && l.rows[i].Head == r.Head
}

// copyRecordOf is the copy record of wt, a worktree of repo, when it is fully copied onto a
// branch whose pull request this listing knows.
func copyRecordOf(repo *Repo, wt *Worktree) (worklog.Copy, bool) {
	c, pr := wt.copies, wt.copyPR
	if !c.full() || pr == nil || pr.Kind != item.KindPR || !gitread.IsCommitID(c.tip) {
		return worklog.Copy{}, false
	}

	return worklog.Copy{
		ClonePath: paths.Resolved(repo.Path), WorktreePath: paths.Resolved(wt.Path), Head: wt.Head, Ref: c.ref,
		Owner: pr.Owner, Name: pr.Name, Number: pr.Number, CopyTip: c.tip, Commits: slices.Clone(c.commits),
	}, true
}

// sameCopy reports two copy records that say the same of one worktree.
func sameCopy(a, b worklog.Copy) bool {
	return a.ClonePath == b.ClonePath && a.Head == b.Head && a.Ref == b.Ref && a.Owner == b.Owner && a.Name == b.Name &&
		a.Number == b.Number && a.CopyTip == b.CopyTip && slices.Equal(a.Commits, b.Commits)
}

// forgetCopy drops the copy record of the worktree at path, if there is one. A record left
// behind is dropped by the next listing, so a failure is only logged.
func (s *Service) forgetCopy(ctx context.Context, path string) {
	if s.copyLog == nil {
		return
	}

	if err := s.copyLog.ForgetCopy(ctx, path); err != nil {
		s.logger.WarnContext(ctx, "could not forget a worktree's copy record", "error", err)
	}
}
