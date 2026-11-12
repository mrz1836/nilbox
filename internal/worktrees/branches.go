package worktrees

import (
	"cmp"
	"context"
	"fmt"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/mrz1836/nilbox/internal/branchrules"
	"github.com/mrz1836/nilbox/internal/gitread"
	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/localrepos"
	"github.com/mrz1836/nilbox/internal/paths"
	"github.com/mrz1836/nilbox/internal/repo"
)

// Branches the app deletes, and how it decides.
//
// The app offers to delete a local branch in two places: with a worktree it removes or
// cleans up (the worktree's own worktree-<name> branch, and its pull request's own branch
// when the worktree has that checked out), and in the cleanup of a clone's merged branches
// (each branch that was the head of a pull request that merged). The user's rules in
// Settings only say which start ticked; the user picks every time, and the automatic
// sweep takes what the rules say. Whatever is picked, a branch goes only when it is
// deletable: every commit on it is on GitHub, nothing protects it (the user's protected
// patterns, or being a remote's default branch), and no worktree has it checked out. It is
// then deleted with `update-ref -d <ref> <commit>`, which git refuses once the branch has
// moved since it was checked.

// ownPullPrefix starts the name of a pull request's worktree, pr-<n>.
const ownPullPrefix = "pr-"

// markBranches fills in each linked worktree's Branches, listing the branches of each
// clone that has a worktree once.
func (s *Service) markBranches(ctx context.Context, g git, repos []Repo, rules branchrules.Rules) {
	for i := range repos {
		if offersBranches(&repos[i]) {
			s.markRepo(ctx, g, &repos[i], rules)
		}
	}
}

// markRepo fills in the Branches of each linked worktree of r. A clone whose branches
// cannot be listed offers none.
func (s *Service) markRepo(ctx context.Context, g git, r *Repo, rules branchrules.Rules) {
	locals, _, err := g.branches(ctx, r.Path)
	if err != nil {
		s.logger.DebugContext(ctx, "the branches could not be listed", "repo", r.DisplayPath, "error", err)

		return
	}

	defaults := g.remoteHeads(ctx, r.Path)

	for j := range r.Worktrees {
		wt := &r.Worktrees[j]
		if !wt.Main && !wt.Prunable {
			wt.Branches = s.branchesOf(ctx, g, r.Path, wt, locals, defaults, rules)
		}
	}
}

// offersBranches reports a repository with a linked worktree.
func offersBranches(r *Repo) bool {
	return slices.ContainsFunc(r.Worktrees, func(wt Worktree) bool { return !wt.Main && !wt.Prunable })
}

// branchesOf returns the branches a removal of wt can take: its own worktree-<name>
// branch, checked out or left beside it, and its pull request's own branch when it has
// that checked out.
func (s *Service) branchesOf(ctx context.Context, g git, repoPath string, wt *Worktree, locals []localBranch,
	defaults []string, rules branchrules.Rules,
) []Branch {
	out := []Branch{}
	own := ownBranchName(wt)

	switch {
	case own == "":
	case wt.Branch == own:
		out = append(out, s.judge(Branch{Name: own, Head: wt.Head, Kind: BranchOwn}, wt.Unpushed, wt.OnGitHub, "",
			defaults, rules))
	default:
		if lb, ok := findBranch(locals, own); ok {
			unpushed, onGitHub := s.branchOnGitHub(ctx, g, repoPath, lb.head, wt.Item)
			out = append(out, s.judge(Branch{Name: own, Head: lb.head, Kind: BranchOwn}, unpushed, onGitHub,
				s.checkedOut(repoPath, lb.worktree), defaults, rules))
		}
	}

	if wt.Item != nil && wt.Item.Kind == item.KindPR && wt.Branch != "" && wt.Branch != own &&
		wt.Branch == wt.Item.HeadRef {
		out = append(out, s.judge(Branch{Name: wt.Branch, Head: wt.Head, Kind: BranchPullRequest}, wt.Unpushed,
			wt.OnGitHub, "", defaults, rules))
	}

	return out
}

// ownBranchName is the worktree-<name> branch of a worktree the app or Claude Code made,
// or "" for any other worktree.
func ownBranchName(wt *Worktree) string {
	switch {
	case wt.OwnBranch:
		return wt.Branch
	case wt.Managed || wt.Legacy:
		return ownBranchPrefix + filepath.Base(wt.Path)
	default:
		return ""
	}
}

// findBranch returns the local branch named name.
func findBranch(locals []localBranch, name string) (localBranch, bool) {
	for _, lb := range locals {
		if lb.name == name {
			return lb, true
		}
	}

	return localBranch{}, false
}

// branchOnGitHub reports how many commits on head no remote-tracking branch has, and
// whether every commit on it is on GitHub: none is missing from those, or head is the head
// of the pull request linked, which GitHub keeps.
func (s *Service) branchOnGitHub(ctx context.Context, g git, repoPath, head string, it *LinkedItem) (int, bool) {
	if it != nil && it.HeadOID != "" && it.HeadOID == head {
		return 0, true
	}

	n, err := g.unpushedOf(ctx, repoPath, head)
	if err != nil {
		return -1, false
	}

	return n, n == 0
}

// judge sets whether b may go, and why not: it is a remote's default branch, the user
// protects it, the clone or a worktree other than the one removed has it checked out
// (elsewhere, the reason checkedOut gives, or ""), or some commit on it is not on GitHub.
func (s *Service) judge(b Branch, unpushed int, onGitHub bool, elsewhere string, defaults []string,
	rules branchrules.Rules,
) Branch {
	switch {
	case slices.Contains(defaults, b.Name):
		b.Reason = "it is the repository's default branch"
	case branchrules.Matches(b.Name, rules.Protected):
		b.Reason = "it is protected in Settings"
	case elsewhere != "":
		b.Reason = elsewhere
	case !onGitHub:
		b.Reason = unpushedText(unpushed) + " on it " + aren(unpushed) + " on GitHub"
	default:
		b.Deletable = true
	}

	return b
}

// checkedOut says why a branch checked out at where stays: it is the clone's current
// branch, or a worktree has it. It is "" when where is "", for a branch nothing has
// checked out.
func (s *Service) checkedOut(clone, where string) string {
	switch {
	case where == "":
		return ""
	case paths.Same(clone, where):
		return "it is the clone's current branch"
	default:
		return "it is checked out in " + s.display(where)
	}
}

// aren is "isn't" for one commit and "aren't" otherwise.
func aren(n int) string {
	if n == 1 {
		return "isn't"
	}

	return "aren't"
}

// defaultBranches names the branches of wt the user's rules delete with it, of those that
// may go: what the automatic sweep takes.
func defaultBranches(wt Worktree, rules branchrules.Rules) []string {
	var out []string

	for _, b := range wt.Branches {
		if b.Deletable && ((b.Kind == BranchOwn && rules.Own) || (b.Kind == BranchPullRequest && rules.PullRequest)) {
			out = append(out, b.Name)
		}
	}

	return out
}

// deleteChosen deletes the branches of a removed worktree that names picks, each only when
// the worktree offered it and it may go, and only while it still points at the commit
// assessed. It returns the branches deleted, and why each other branch picked was kept.
func (s *Service) deleteChosen(ctx context.Context, g git, repoPath string, wt Worktree, names []string) ([]string, []string) {
	deleted := []string{}

	var kept []string

	for _, name := range names {
		i := slices.IndexFunc(wt.Branches, func(b Branch) bool { return b.Name == name })

		switch {
		case i < 0:
			kept = append(kept, "kept "+name+": it is not this worktree's to delete")
		case !wt.Branches[i].Deletable:
			kept = append(kept, "kept "+name+": "+wt.Branches[i].Reason)
		default:
			if err := s.deleteBranchAt(ctx, g, repoPath, name, wt.Branches[i].Head); err != nil {
				kept = append(kept, "kept "+name+": "+reasonOf(err))
			} else {
				deleted = append(deleted, name)
			}
		}
	}

	return deleted, kept
}

// deleteBranchAt deletes a branch while it still points at head, refusing while any
// worktree has it checked out.
func (s *Service) deleteBranchAt(ctx context.Context, g git, repoPath, branch, head string) error {
	if !gitread.IsCommitID(head) {
		return fmt.Errorf("%w: no commit to compare the branch with", ErrChanged)
	}

	entries, stderr, err := g.list(ctx, repoPath)
	if err != nil {
		return fmt.Errorf("list the worktrees of %s: %w", s.display(repoPath), gitError(err, stderr))
	}

	for _, e := range entries {
		if e.Branch == branch {
			return ErrBranchInUse
		}
	}

	if stderr, err = g.deleteBranch(ctx, repoPath, branch, head); err != nil {
		return gitError(err, stderr)
	}

	return nil
}

// MergedBranches lists the local branches of every clone under the clone root whose pull
// request merged on GitHub: each branch that was the head branch of a merged pull request
// in one of the clone's GitHub repositories, and each worktree-pr-<n> branch whose pull
// request merged. Branches the user protects and each remote's default branch are left
// out. Each says whether it may go: its commits are all on GitHub (it is at the merged
// head, behind it, or pushed), and no worktree has it checked out. It asks GitHub once per
// fifty branch names, only for clones with a branch to ask about, and changes nothing.
func (s *Service) MergedBranches(ctx context.Context) ([]MergedBranch, error) {
	if s.github == nil {
		return nil, ErrGitHubUnavailable
	}

	g, root, prefs, err := s.prepare(ctx)
	if err != nil {
		return nil, err
	}

	clones, err := localrepos.Scan(ctx, root, s.run)
	if err != nil {
		return nil, err
	}

	scans := s.scanCandidates(ctx, g, clones, prefs.Branches)

	answers, states, err := s.askMerged(ctx, scans)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrGitHubUnavailable, err)
	}

	out := []MergedBranch{}

	for _, scan := range scans {
		for _, lb := range scan.candidates {
			if mb, ok := s.mergedBranch(ctx, g, scan, lb, answers, states); ok {
				out = append(out, mb)
			}
		}
	}

	slices.SortFunc(out, func(a, b MergedBranch) int {
		return cmp.Or(cmp.Compare(a.DisplayRepo, b.DisplayRepo), cmp.Compare(a.Branch.Name, b.Branch.Name))
	})

	return out, nil
}

// scanCandidates lists each clone's branches that might have merged, a few clones at a
// time.
func (s *Service) scanCandidates(ctx context.Context, g git, clones []localrepos.LocalRepo,
	rules branchrules.Rules,
) []cloneBranches {
	scans := make([]cloneBranches, len(clones))

	var eg errgroup.Group

	eg.SetLimit(listConcurrency)

	for i, clone := range clones {
		eg.Go(func() error {
			scans[i] = s.branchCandidates(ctx, g, clone, rules)

			return nil
		})
	}

	_ = eg.Wait() // branchCandidates never fails: a clone it cannot list has no candidates

	return scans
}

// cloneBranches is one clone's branches that might have merged: every local branch but
// those protected and the remotes' default branches.
type cloneBranches struct {
	clone      localrepos.LocalRepo
	candidates []localBranch
}

// branchCandidates lists the clone's branches that might have merged. A clone whose
// branches cannot be listed has none.
func (s *Service) branchCandidates(ctx context.Context, g git, clone localrepos.LocalRepo, rules branchrules.Rules) cloneBranches {
	out := cloneBranches{clone: clone}
	if len(clone.Remotes) == 0 {
		return out
	}

	locals, _, err := g.branches(ctx, clone.Path)
	if err != nil {
		s.logger.DebugContext(ctx, "the branches could not be listed", "repo", s.display(clone.Path), "error", err)

		return out
	}

	defaults := g.remoteHeads(ctx, clone.Path)

	for _, lb := range locals {
		if !slices.Contains(defaults, lb.name) && !branchrules.Matches(lb.name, rules.Protected) && gitread.ValidBranch(lb.name) {
			out.candidates = append(out.candidates, lb)
		}
	}

	return out
}

// askMerged asks GitHub about every candidate: the merged pull requests whose head branch
// had each name, and the state of the pull request each worktree-pr-<n> branch was made
// for, in each of the clone's GitHub repositories.
func (s *Service) askMerged(ctx context.Context, scans []cloneBranches) (map[BranchKey][]MergedPR, map[string]ItemState, error) {
	var (
		queries []BranchQuery
		refs    []item.Ref
	)

	for _, scan := range scans {
		q, r := scan.questions()
		queries, refs = append(queries, q...), append(refs, r...)
	}

	answers := map[BranchKey][]MergedPR{}

	if len(queries) > 0 {
		got, err := s.github.MergedPullRequests(ctx, queries)
		if err != nil {
			return nil, nil, err
		}

		answers = got
	}

	states := map[string]ItemState{}

	if len(refs) > 0 {
		got, err := s.github.ItemStates(ctx, refs)
		if err != nil {
			return nil, nil, err
		}

		states = got
	}

	return answers, states, nil
}

// questions are what to ask GitHub about the clone's candidates: the branch names, in
// each of its GitHub repositories, and the pull request of each worktree-pr-<n> branch.
func (c cloneBranches) questions() ([]BranchQuery, []item.Ref) {
	var (
		names []string
		refs  []item.Ref
	)

	repos := githubRepos(c.clone)

	for _, lb := range c.candidates {
		n, ok := ownPullNumber(lb.name)
		if !ok {
			names = append(names, lb.name)

			continue
		}

		for _, r := range repos {
			refs = append(refs, item.Ref{Kind: item.KindPR, Repo: r, Number: n})
		}
	}

	if len(names) == 0 {
		return nil, refs
	}

	queries := make([]BranchQuery, 0, len(repos))
	for _, r := range repos {
		queries = append(queries, BranchQuery{Repo: r, Branches: names})
	}

	return queries, refs
}

// githubRepos returns the clone's GitHub repositories, each once, in name order.
func githubRepos(clone localrepos.LocalRepo) []repo.Name {
	var out []repo.Name

	for _, r := range clone.Remotes {
		if !slices.ContainsFunc(out, func(o repo.Name) bool { return o.Key() == r.Key() }) {
			out = append(out, r)
		}
	}

	slices.SortFunc(out, func(a, b repo.Name) int { return cmp.Compare(a.Key(), b.Key()) })

	return out
}

// ownPullNumber reads n from a worktree-pr-<n> branch.
func ownPullNumber(name string) (int, bool) {
	digits, ok := strings.CutPrefix(name, ownBranchPrefix+ownPullPrefix)
	if !ok {
		return 0, false
	}

	n, err := strconv.Atoi(digits)

	return n, err == nil && n > 0 && strconv.Itoa(n) == digits
}

// mergedBranch is a candidate's entry in MergedBranches, when it was the head of a merged
// pull request: the newest one, and whether the branch may go.
func (s *Service) mergedBranch(ctx context.Context, g git, scan cloneBranches, lb localBranch,
	answers map[BranchKey][]MergedPR, states map[string]ItemState,
) (MergedBranch, bool) {
	prs, prRepos := mergedFor(scan.clone, lb.name, answers, states)
	if len(prs) == 0 {
		return MergedBranch{}, false
	}

	newest := 0
	for i, pr := range prs {
		if pr.MergedAt.After(prs[newest].MergedAt) {
			newest = i
		}
	}

	out := MergedBranch{
		RepoPath:    scan.clone.Path,
		DisplayRepo: s.display(scan.clone.Path),
		Branch:      Branch{Name: lb.name, Head: lb.head, Kind: BranchMerged},
		PullRequest: MergedPullRequest{
			Repo: prRepos[newest], Number: prs[newest].Number, Title: prs[newest].Title, URL: prs[newest].URL,
			MergedAt: prs[newest].MergedAt.UTC().Format(time.RFC3339),
		},
	}

	switch {
	case lb.worktree != "":
		out.Branch.Reason = s.checkedOut(scan.clone.Path, lb.worktree)
	case slices.ContainsFunc(prs, func(pr MergedPR) bool { return s.inMerged(ctx, g, scan.clone.Path, lb.head, pr) }):
		out.Branch.Deletable = true
	default:
		n, err := g.unpushedOf(ctx, scan.clone.Path, lb.head)
		if err == nil && n == 0 {
			out.Branch.Deletable = true
		} else {
			out.Branch.Reason = "it has commits that aren't in pull request #" + strconv.Itoa(prs[newest].Number) +
				" or on GitHub"
		}
	}

	return out, true
}

// inMerged reports a branch at head whose commits are all in pr: it is at pr's head, or
// behind it, which GitHub keeps.
func (s *Service) inMerged(ctx context.Context, g git, repoPath, head string, pr MergedPR) bool {
	if pr.HeadOID == "" {
		return false
	}

	return pr.HeadOID == head || (gitread.IsCommitID(pr.HeadOID) && g.isAncestor(ctx, repoPath, head, pr.HeadOID))
}

// mergedFor returns the merged pull requests a branch of the clone was the head of, with
// the repository of each: by name in any of the clone's repositories, or, for a
// worktree-pr-<n> branch, pull request n when it merged.
func mergedFor(clone localrepos.LocalRepo, name string, answers map[BranchKey][]MergedPR,
	states map[string]ItemState,
) ([]MergedPR, []string) {
	var (
		prs   []MergedPR
		repos []string
	)

	n, own := ownPullNumber(name)

	for _, r := range githubRepos(clone) {
		if !own {
			for _, pr := range answers[BranchKey{Repo: r.Key(), Branch: name}] {
				prs, repos = append(prs, pr), append(repos, r.String())
			}

			continue
		}

		st, ok := states[item.Ref{Kind: item.KindPR, Repo: r, Number: n}.Key()]
		if ok && st.PullRequest && st.State == ItemMerged {
			prs = append(prs, MergedPR{Number: n, Title: st.Title, URL: st.URL, MergedAt: st.ClosedAt, HeadOID: st.HeadOID})
			repos = append(repos, r.String())
		}
	}

	return prs, repos
}

// DeleteBranches deletes the branches the user ticked in the cleanup of merged branches,
// each checked again first: its clone must be under the clone root, the user must not
// protect it, it must not be a remote's default branch or checked out in any worktree,
// and it must still point at the commit it was offered at. Each clone is read once and its
// branches deleted in turn, a few clones at a time. It reports each outcome, in the order
// picked, with the commit that restores a deleted branch.
func (s *Service) DeleteBranches(ctx context.Context, picks []BranchPick) ([]BranchResult, error) {
	s.act.Lock()
	defer s.act.Unlock()

	g, root, prefs, err := s.prepare(ctx)
	if err != nil {
		return nil, err
	}

	out := make([]BranchResult, len(picks))
	byClone := map[string][]int{}

	var clones []string

	for i, p := range picks {
		out[i] = BranchResult{RepoPath: p.RepoPath, DisplayRepo: s.display(p.RepoPath), Name: p.Name, Head: p.Head}

		if _, seen := byClone[p.RepoPath]; !seen {
			clones = append(clones, p.RepoPath)
		}

		byClone[p.RepoPath] = append(byClone[p.RepoPath], i)
	}

	var eg errgroup.Group

	eg.SetLimit(listConcurrency)

	for _, clone := range clones {
		eg.Go(func() error {
			s.deleteInClone(ctx, g, root, clone, byClone[clone], picks, out, prefs.Branches)

			return nil
		})
	}

	_ = eg.Wait() // deleteInClone never fails: it reports each pick's outcome

	return out, ctx.Err()
}

// cloneBranchState is what each branch picked in a clone is checked against, read once:
// the clone's local branches, its remotes' default branches, and the branches any of its
// worktrees has checked out.
type cloneBranchState struct {
	locals   []localBranch
	defaults []string
	inUse    map[string]bool
}

// readCloneBranches reads a clone's branch state, refusing a clone outside the clone root.
func (s *Service) readCloneBranches(ctx context.Context, g git, root, clone string) (cloneBranchState, error) {
	if !s.underRoot(clone, root) {
		return cloneBranchState{}, fmt.Errorf("%w: %s", ErrOutsideCloneRoot, s.display(clone))
	}

	locals, stderr, err := g.branches(ctx, clone)
	if err != nil {
		return cloneBranchState{}, fmt.Errorf("list the branches of %s: %w", s.display(clone), gitError(err, stderr))
	}

	entries, stderr, err := g.list(ctx, clone)
	if err != nil {
		return cloneBranchState{}, fmt.Errorf("list the worktrees of %s: %w", s.display(clone), gitError(err, stderr))
	}

	inUse := map[string]bool{}

	for _, e := range entries {
		if e.Branch != "" {
			inUse[e.Branch] = true
		}
	}

	for _, lb := range locals {
		if lb.worktree != "" {
			inUse[lb.name] = true
		}
	}

	return cloneBranchState{locals: locals, defaults: g.remoteHeads(ctx, clone), inUse: inUse}, nil
}

// deleteInClone deletes the picks at idx, all in one clone, recording each outcome in out.
// It holds the clone's lock throughout, and a run using the clone refuses every pick, naming
// the run (see holdClone).
func (s *Service) deleteInClone(ctx context.Context, g git, root, clone string, idx []int, picks []BranchPick,
	out []BranchResult, rules branchrules.Rules,
) {
	unlock, err := s.holdClone(ctx, clone)
	if err != nil {
		for _, i := range idx {
			out[i].Reason = reasonOf(err)
		}

		return
	}

	defer unlock()

	state, readErr := s.readCloneBranches(ctx, g, root, clone)

	for _, i := range idx {
		err := cmp.Or(ctx.Err(), readErr)
		if err == nil {
			err = s.deletePicked(ctx, g, clone, picks[i], &state, rules)
		}

		if err != nil {
			out[i].Reason = reasonOf(err)
		} else {
			out[i].Deleted = true
		}
	}
}

// deletePicked checks one ticked branch again against its clone's state and deletes it,
// dropping it from the state so a second pick of it finds it gone.
func (s *Service) deletePicked(ctx context.Context, g git, clone string, p BranchPick, state *cloneBranchState,
	rules branchrules.Rules,
) error {
	switch {
	case !gitread.ValidBranch(p.Name) || !gitread.IsCommitID(p.Head):
		return fmt.Errorf("%w: %q at %q", ErrInvalidRequest, p.Name, p.Head)
	case branchrules.Matches(p.Name, rules.Protected):
		return fmt.Errorf("%w: it is protected in Settings", ErrBranchProtected)
	case slices.Contains(state.defaults, p.Name):
		return fmt.Errorf("%w: it is the repository's default branch", ErrBranchProtected)
	}

	lb, ok := findBranch(state.locals, p.Name)

	switch {
	case !ok:
		return ErrBranchGone
	case lb.head != p.Head:
		return ErrBranchMoved
	case state.inUse[p.Name]:
		return ErrBranchInUse
	}

	if stderr, err := g.deleteBranch(ctx, clone, p.Name, p.Head); err != nil {
		return gitError(err, stderr)
	}

	state.locals = slices.DeleteFunc(state.locals, func(b localBranch) bool { return b.name == p.Name })

	return nil
}
