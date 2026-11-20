package worktrees

import (
	"context"
	"errors"
	"slices"
	"strings"

	"golang.org/x/sync/errgroup"

	"github.com/mrz1836/nilbox/internal/gitread"
	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/paths"
	"github.com/mrz1836/nilbox/internal/repo"
	"github.com/mrz1836/nilbox/internal/worklog"
)

// Counting copied work as on GitHub.
//
// A worktree's commits that aren't on GitHub by commit ID count as on GitHub anyway, as
// copies (copyState.onGitHub), when either:
//
//   - this listing found each with an identical copy on one pushed branch of a GitHub
//     remote (Copies.full): the copies are on GitHub as that branch; or
//   - the branch its copy record names is gone, and the record still holds: it describes
//     the worktree (its HEAD is the record's; see liveCopies), it covers every commit the
//     worktree has that isn't on GitHub (each is one whose copy the record vouches for, or
//     is on the record's branch commit itself; see judgeCoverage), this listing read the
//     record's pull request from GitHub, and that pull request's head is the record's
//     branch commit or has it (prHolds). GitHub keeps a pull request's head commit, so the
//     copies stay on GitHub however the pull request merged and whatever became of its
//     branch. Copies.Recorded says so.
//
// The automatic cleanup takes a worktree whose work is on GitHub only as copies only once
// the record's pull request merged with them all (copyState.merged), and its own branch is
// deletable then; never while the pull request is open, and never by a branch's name.

// copyState is what the listing found of a worktree's copies.
type copyState struct {
	// result is what comparing its commits not on GitHub with the pushed branches found,
	// or nil when they weren't compared, and pr the pull request the branch they were all
	// copied onto heads, or nil.
	result *copyResult
	pr     *LinkedItem

	// record is its copy record while it describes the worktree, or nil; covered reports
	// that every commit it has that isn't on GitHub is one whose copy the record vouches
	// for, or is on the record's branch commit; recordPR is the record's pull request as
	// this listing read it, or nil.
	record   *worklog.Copy
	covered  bool
	recordPR *LinkedItem

	// onGitHub reports its commits that aren't on GitHub by commit ID counting as on
	// GitHub, as copies; merged, that the record's pull request merged with every copy.
	onGitHub bool
	merged   bool
}

// coverKey is what a record's coverage of a worktree was worked out from: the worktree's
// HEAD, its clone's GitHub remotes and remote-tracking branches, and the record's branch
// commit and commits.
type coverKey struct {
	head, remotes, refs, tip, commits string
}

// holdKey is a pull request's head and the record's branch commit it was checked for; refs
// is the clone's remote-tracking branches for an answer of no, which a fetch may change.
type holdKey struct {
	tip, head, refs string
}

// judgeCoverage attaches each worktree of repos its copy record, from live (by the
// worktree's resolved directory, the records that still describe their worktree), and
// works out whether the record covers every commit the worktree has that isn't on GitHub,
// a few clones at a time. A clone whose refs couldn't be read covers nothing.
func (s *Service) judgeCoverage(ctx context.Context, g git, repos []Repo, remotes map[string]map[string]repo.Name,
	refs map[string]cloneRefs, live map[string]worklog.Copy,
) {
	if len(live) == 0 {
		return
	}

	var eg errgroup.Group

	eg.SetLimit(listConcurrency)

	for i := range repos {
		r := &repos[i]

		cr, ok := refs[r.Path]
		if !ok {
			continue
		}

		eg.Go(func() error {
			s.judgeCloneCoverage(ctx, g, r, copyKey{remotes: remotesKey(remotes[r.Path], cr.names), refs: cr.fingerprint()}, live)

			return nil
		})
	}

	_ = eg.Wait() // a coverage that can't be worked out is only no coverage
}

// judgeCloneCoverage attaches the records of r's worktrees and works out their coverage:
// with no git when the commits this listing compared are all the record's, else from what
// the cache keeps for the same coverKey, else in one log per record branch commit, of the
// commits each worktree has that neither a remote-tracking branch nor that commit has
// (copyUnpushedArgs with the branch commit after --remotes), which must all be the
// record's. clone holds the clone's remotes and refs. A worktree made at a commit fetched
// from GitHub, which that log can't leave out too, is never covered.
func (s *Service) judgeCloneCoverage(ctx context.Context, g git, r *Repo, clone copyKey, live map[string]worklog.Copy) {
	groups := map[string][]*Worktree{}

	for j := range r.Worktrees {
		wt := &r.Worktrees[j]
		if !attachRecord(wt, live) || wt.Unpushed <= 0 || wt.fetched != "" {
			continue
		}

		if covered, ok := s.knownCoverage(wt, clone); ok {
			wt.copies.covered = covered
		} else {
			groups[wt.copies.record.CopyTip] = append(groups[wt.copies.record.CopyTip], wt)
		}
	}

	for tip, group := range groups {
		s.readCoverage(ctx, g, r.Path, tip, group, clone)
	}
}

// knownCoverage is whether wt's record covers it, when that is known without git: yes when
// the commits this listing compared are all the record's, or what the cache keeps for the
// same coverKey.
func (s *Service) knownCoverage(wt *Worktree, clone copyKey) (covered, ok bool) {
	if res := wt.copies.result; res != nil && res.commits != nil && holdsAll(res.commits, wt.copies.record.Commits) {
		return true, true
	}

	return s.copies.coverage(paths.Resolved(wt.Path), coverOf(wt, clone))
}

// readCoverage works out whether the records of group, worktrees of the clone at dir whose
// records' branch commit is tip, cover them, in one log, keeping what it can in the cache.
func (s *Service) readCoverage(ctx context.Context, g git, dir, tip string, group []*Worktree, clone copyKey) {
	heads := make([]string, 0, len(group))
	for _, wt := range group {
		heads = append(heads, wt.Head)
	}

	set, err := g.copyUnpushed(ctx, dir, slices.Compact(slices.Sorted(slices.Values(heads))), tip)

	for _, wt := range group {
		wt.copies.covered = err == nil && holdsAll(oidsOf(reach(set, wt.Head)), wt.copies.record.Commits)

		if !retryable(ctx, err) {
			s.copies.keepCoverage(dir, paths.Resolved(wt.Path), coverOf(wt, clone), wt.copies.covered)
		}
	}
}

// oidsOf lists the commit IDs of commits.
func oidsOf(commits []copyCommit) []string {
	out := make([]string, 0, len(commits))
	for _, c := range commits {
		out = append(out, c.oid)
	}

	return out
}

// attachRecord gives wt its copy record from live, when one describes it, and reports
// whether it did.
func attachRecord(wt *Worktree, live map[string]worklog.Copy) bool {
	if wt.Main || wt.Prunable {
		return false
	}

	rec, ok := live[paths.Resolved(wt.Path)]
	if !ok || rec.Head != wt.Head || !gitread.IsCommitID(rec.CopyTip) {
		return false
	}

	wt.copies.record = &rec

	return true
}

// coverOf is the coverKey of wt's record coverage, clone holding its clone's remotes and
// refs.
func coverOf(wt *Worktree, clone copyKey) coverKey {
	rec := wt.copies.record

	return coverKey{
		head: wt.Head, remotes: clone.remotes, refs: clone.refs, tip: rec.CopyTip, commits: strings.Join(rec.Commits, " "),
	}
}

// holdsAll reports that every one of oids is among held.
func holdsAll(oids, held []string) bool {
	return !slices.ContainsFunc(oids, func(oid string) bool { return !slices.Contains(held, oid) })
}

// recordTarget is a worktree whose covering copy record's pull request the listing asks
// GitHub about, and whether the record's branch is gone.
type recordTarget struct {
	wt   *Worktree
	ref  item.Ref
	gone bool
}

// recordTargets lists the worktrees of repos with commits not on GitHub whose copy record
// covers them, with the record's pull request. refs holds each repository's refs, by its
// Path.
func recordTargets(repos []Repo, refs map[string]cloneRefs) []recordTarget {
	var out []recordTarget

	for r, wt := range rows(repos) {
		rec := wt.copies.record
		if rec == nil || !wt.copies.covered || wt.Unpushed <= 0 {
			continue
		}

		_, there := refs[r.Path].tracking[rec.Ref]
		out = append(out, recordTarget{
			wt: wt, ref: item.Ref{Repo: repo.Name{Owner: rec.Owner, Repo: rec.Name}, Number: rec.Number, Kind: item.KindPR},
			gone: !there,
		})
	}

	return out
}

// linkRecords fills in each target's record pull request from GitHub's answer about items,
// and links a worktree nothing else links whose record's branch is gone to it (LinkCopy).
func linkRecords(targets []recordTarget, items map[string]ItemState) {
	for _, t := range targets {
		state, ok := items[t.ref.Key()]
		if !ok || !state.PullRequest {
			continue
		}

		pr := linkedItem(linkTarget{kind: item.KindPR}, t.ref, state)
		t.wt.copies.recordPR = pr

		if t.gone && t.wt.Item == nil {
			linked := *pr
			t.wt.Item, t.wt.LinkedBy = &linked, LinkCopy
		}
	}
}

// judgeCopied works out, for each worktree of repos, whether its commits that aren't on
// GitHub by commit ID count as on GitHub as copies, and whether its record's pull request
// merged with them all (see this file's doc), a few clones at a time. refs holds each
// repository's refs, by its Path; a clone whose refs couldn't be read counts nothing so.
func (s *Service) judgeCopied(ctx context.Context, g git, repos []Repo, refs map[string]cloneRefs) {
	var eg errgroup.Group

	eg.SetLimit(listConcurrency)

	for i := range repos {
		r := &repos[i]

		cr, ok := refs[r.Path]
		if !ok {
			continue
		}

		eg.Go(func() error {
			for j := range r.Worktrees {
				s.judgeOneCopied(ctx, g, r.Path, &r.Worktrees[j], cr)
			}

			return nil
		})
	}

	_ = eg.Wait() // judgeOneCopied never fails: what it can't tell counts as not on GitHub
}

// judgeOneCopied works out whether wt, a worktree of the clone at dir whose refs are refs,
// has its commits not on GitHub by commit ID on GitHub as copies, and whether its record's
// pull request merged with them all.
func (s *Service) judgeOneCopied(ctx context.Context, g git, dir string, wt *Worktree, refs cloneRefs) {
	st := &wt.copies
	st.onGitHub = wt.Unpushed > 0 && st.result.full()

	rec, pr := st.record, st.recordPR
	if rec == nil || !st.covered || pr == nil || wt.Unpushed <= 0 ||
		!s.prHolds(ctx, g, dir, rec.CopyTip, pr.HeadOID, refs.fingerprint()) {
		return
	}

	st.merged = pr.State == ItemMerged

	if _, there := refs.tracking[rec.Ref]; !there && !st.onGitHub {
		st.onGitHub = true
		wt.Copies = Copies{
			Ref: strings.TrimPrefix(rec.Ref, remotesRef), Count: wt.Unpushed, Identical: wt.Unpushed, Recorded: true,
		}
	}
}

// prHolds reports a pull request's head, head, that is tip or has it, in the clone at dir,
// whose remote-tracking branches fp sums up: by an ancestor's check, which a commit the
// clone lacks fails. A yes is kept for good, as a pull request's head has the commits it
// had; a no only while the remote-tracking branches are the same.
func (s *Service) prHolds(ctx context.Context, g git, dir, tip, head, fp string) bool {
	switch {
	case tip == head:
		return gitread.IsCommitID(tip)
	case !gitread.IsCommitID(tip) || !gitread.IsCommitID(head):
		return false
	}

	if held, ok := s.copies.held(holdKey{tip: tip, head: head}, fp); ok {
		return held
	}

	held := g.isAncestor(ctx, dir, tip, head)
	if ctx.Err() == nil {
		s.copies.keepHeld(holdKey{tip: tip, head: head}, fp, held)
	}

	return held
}

// retryable reports a comparison's or a coverage's failure worked out again next time
// rather than kept: git couldn't be run at all, or ran past ctx's end. Any other answer
// would be the same again from the same commits.
func retryable(ctx context.Context, err error) bool {
	if err == nil {
		return ctx.Err() != nil
	}

	gitErr, ok := errors.AsType[*copyGitError](err)

	return ctx.Err() != nil || (ok && !errors.Is(gitErr.err, errGitWarned) && !gitRefused(ctx, gitErr.err))
}

// onGitHubByID reports a worktree every commit of which is on GitHub by its commit ID: none
// is missing from the remote-tracking branches, or HEAD is its pull request's head.
func onGitHubByID(wt *Worktree) bool {
	return wt.Unpushed == 0 || headIsItemHead(wt)
}
