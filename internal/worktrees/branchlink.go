package worktrees

import (
	"strings"
	"sync"
	"time"

	"github.com/mrz1836/nilbox/internal/gitread"
	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/repo"
)

// Linking a worktree by its branch.
//
// A worktree with no item by its record, its name, or an item's own branch, on a local
// branch that was pushed, is linked to the pull request whose head is that branch in the
// repository it was pushed to: the remote-tracking branch of the same name, its upstream's
// first, then origin's. A remote's default branch never links. The pull request's head
// must be in that same repository, so a fork's branch of the same name never links; of
// several, the open one wins, else the one updated last. GitHub is asked in the request
// the listing makes for the items' states (GitHub.LinkStates), and its answer is kept
// (headCache) for the commit the branch was pushed at, so a listing that finds nothing new
// asks about the pull request's state alone. Linking by branch never makes the automatic
// cleanup easier: autoCleanable keeps its rule.

// headRecheck is how long a branch's answer is kept when it may change while the branch
// stays where it is: that no pull request heads it, as before one is opened, or that the
// one that does is done, as when the branch is reused for another, with or without a new
// push. A listing in between links it as GitHub last said.
const headRecheck = 10 * time.Minute

// headCacheMax is how many branches' answers headCache keeps before it starts over.
const headCacheMax = 4096

// headAsk is a pushed branch a worktree may be linked through: the repository of the
// remote it was pushed to, its name there, and the commit the clone last saw it at.
type headAsk struct {
	repo   repo.Name
	branch string
	tip    string
}

// key is the branch's key in GitHub's answer.
func (a headAsk) key() BranchKey {
	return BranchKey{Repo: a.repo.Key(), Branch: a.branch}
}

// cacheKey is the branch's key in headCache: its key at the commit seen.
func (a headAsk) cacheKey() headKey {
	return headKey{branch: a.key(), tip: a.tip}
}

// headTarget is a worktree that may be linked through a pushed branch.
type headTarget struct {
	wt  *Worktree
	ask headAsk
}

// headKey identifies a branch at a commit in headCache.
type headKey struct {
	branch BranchKey
	tip    string
}

// headEntry is what GitHub last said about a branch at a commit: the number of the pull
// request it heads, 0 for none, its state and head commit as last read, and when the
// branch itself was last asked about.
type headEntry struct {
	number  int
	state   ItemStatus
	head    string
	askedAt time.Time
}

// headCache keeps which pull request each pushed branch heads, by the commit the branch was
// seen at, across listings. It is safe for concurrent use.
type headCache struct {
	mu      sync.Mutex
	entries map[headKey]headEntry
}

// newHeadCache returns an empty headCache.
func newHeadCache() *headCache {
	return &headCache{entries: map[headKey]headEntry{}}
}

// headPlan is what a listing asks GitHub about its pushed branches: the branches to ask
// about (asks, each once), and the pull request already known for the others, asked about
// by number with the items.
type headPlan struct {
	asks  []headAsk
	known map[headKey]int
}

// plan works out what to ask about targets' branches at now: a branch at a commit it was
// never asked about at is asked about; one known to head no pull request, or only one that
// is done, is asked about again once headRecheck has passed, as a pull request opened since
// on the same branch at the same commit is the one it heads, and links as last said until
// then; one known to head an open pull request is asked about by that pull request's
// number alone.
func (c *headCache) plan(targets []headTarget, now time.Time) headPlan {
	c.mu.Lock()
	defer c.mu.Unlock()

	p := headPlan{known: map[headKey]int{}}
	asked := map[BranchKey]bool{}

	for _, t := range targets {
		key := t.ask.cacheKey()
		if _, done := p.known[key]; done {
			continue
		}

		e, ok := c.entries[key]
		stale := e.number == 0 || e.state != ItemOpen

		switch {
		case ok && (!stale || now.Sub(e.askedAt) < headRecheck):
			p.known[key] = e.number
		case !asked[t.ask.key()]:
			asked[t.ask.key()] = true
			p.asks = append(p.asks, t.ask)
		}
	}

	return p
}

// learn keeps what GitHub said about a branch at a commit: the pull request it heads, or
// none for nil, asked about at now.
func (c *headCache) learn(key headKey, pr *HeadPR, now time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if len(c.entries) >= headCacheMax {
		clear(c.entries)
	}

	e := headEntry{askedAt: now}
	if pr != nil {
		e.number, e.state, e.head = pr.Number, itemStatus(pr.State.State), pr.State.HeadOID
	}

	c.entries[key] = e
}

// refresh keeps the state GitHub read of a known pull request by its number, or forgets
// the branch's entry when GitHub no longer knows it as that branch's, so the next listing
// asks about the branch again.
func (c *headCache) refresh(key headKey, state ItemState, ok bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	e, found := c.entries[key]
	if !found {
		return
	}

	if !ok {
		delete(c.entries, key)

		return
	}

	e.state, e.head = itemStatus(state.State), state.HeadOID
	c.entries[key] = e
}

// itemStatus reads a state GitHub reported: merged and closed as they are, anything else
// as open.
func itemStatus(s ItemStatus) ItemStatus {
	if s == ItemMerged || s == ItemClosed {
		return s
	}

	return ItemOpen
}

// headQueries are the questions GitHub is asked about asks.
func headQueries(asks []headAsk) []HeadQuery {
	out := make([]HeadQuery, 0, len(asks))
	for _, a := range asks {
		out = append(out, HeadQuery{Repo: a.repo, Branch: a.branch})
	}

	return out
}

// knownRef is the pull request number known for a branch, asked about by number.
func knownRef(a headAsk, number int) item.Ref {
	return item.Ref{Repo: a.repo, Number: number, Kind: item.KindPR}
}

// pickHead chooses the pull request a branch of repo heads among GitHub's answer for it,
// newest updated first, of those whose head branch has that name in that repository, so a
// fork's same-named branch never counts: the open one, when there is one, else the one
// updated last. An old pull request of the branch updated since, by a comment, say, never
// wins over the open one. It is nil when none heads it.
func pickHead(prs []HeadPR, a headAsk) *HeadPR {
	var chosen *HeadPR

	for i := range prs {
		pr := &prs[i]

		switch {
		case pr.Number <= 0 || !pr.State.PullRequest || pr.State.HeadRef != a.branch ||
			!strings.EqualFold(pr.HeadRepo, a.repo.String()):
		case itemStatus(pr.State.State) == ItemOpen:
			return pr
		case chosen == nil:
			chosen = pr
		}
	}

	return chosen
}

// resolveHeads works out the pull request each target's branch heads from GitHub's answer
// and plan, keeps what it learned in the cache, and returns each branch's pull request by
// its key in the cache, nil when it heads none.
func (c *headCache) resolveHeads(targets []headTarget, p headPlan, answer LinkAnswer, now time.Time,
) map[headKey]*LinkedItem {
	out := map[headKey]*LinkedItem{}

	for _, t := range targets {
		key := t.ask.cacheKey()
		if _, done := out[key]; !done {
			out[key] = headItem(t.ask, c.resolveHead(t.ask, p, answer, now))
		}
	}

	return out
}

// resolveHead works out the pull request a's branch heads, nil for none: from GitHub's
// answer about the branch when it was asked about, which the cache keeps, or else by the
// number the cache knows, when GitHub still answers it as that branch's pull request.
func (c *headCache) resolveHead(a headAsk, p headPlan, answer LinkAnswer, now time.Time) *HeadPR {
	key := a.cacheKey()
	number, known := p.known[key]

	switch {
	case !known:
		pr := pickHead(answer.Heads[a.key()], a)
		c.learn(key, pr, now)

		return pr
	case number == 0:
		return nil
	}

	state, ok := answer.Items[knownRef(a, number).Key()]
	ok = ok && state.PullRequest && state.HeadRef == a.branch
	c.refresh(key, state, ok)

	if !ok {
		return nil
	}

	return &HeadPR{Number: number, State: state}
}

// refs lists the items to ask about: items, then the pull request known for each of
// targets' branches, each once.
func (p headPlan) refs(items []item.Ref, targets []headTarget) []item.Ref {
	for _, t := range targets {
		if n := p.known[t.ask.cacheKey()]; n > 0 {
			items = appendRef(items, knownRef(t.ask, n))
		}
	}

	return items
}

// linkHeads links each of targets that has no item yet to the pull request its branch
// heads, by kind, from prs, which resolveHeads returned.
func linkHeads(targets []headTarget, prs map[headKey]*LinkedItem, kind LinkKind) {
	for _, t := range targets {
		if pr := prs[t.ask.cacheKey()]; pr != nil && t.wt.Item == nil {
			linked := *pr
			t.wt.Item, t.wt.LinkedBy = &linked, kind
		}
	}
}

// headItem is the pull request a branch heads, as a worktree links to it, or nil.
func headItem(a headAsk, pr *HeadPR) *LinkedItem {
	if pr == nil {
		return nil
	}

	ref := knownRef(a, pr.Number)

	return linkedItem(linkTarget{kind: item.KindPR}, ref, pr.State)
}

// branchTargets lists the worktrees of repos with no item by name (named holds those that
// have one) that are on a pushed branch, with the branch each may be linked through (see
// branchAsk). remotes maps each repository's Path to its GitHub remotes, and refs to its
// refs, as read for this listing.
func branchTargets(repos []Repo, remotes map[string]map[string]repo.Name, refs map[string]cloneRefs,
	named map[*Worktree]bool,
) []headTarget {
	var out []headTarget

	for r, wt := range rows(repos) {
		if named[wt] {
			continue
		}

		cr, ok := refs[r.Path]
		if !ok {
			continue
		}

		if ask, ok := branchAsk(wt, remotes[r.Path], cr); ok {
			out = append(out, headTarget{wt: wt, ask: ask})
		}
	}

	return out
}

// copyTargets lists the worktrees of repos fully copied onto a pushed branch (see
// copies.go), with that branch, whose pull request each may be linked to. remotes maps each
// repository's Path to its GitHub remotes.
func copyTargets(repos []Repo, remotes map[string]map[string]repo.Name) []headTarget {
	var out []headTarget

	for r, wt := range rows(repos) {
		if !wt.copies.result.full() {
			continue
		}

		remote, branch, ok := trackingOf(wt.copies.result.ref)
		if target, onGitHub := remotes[r.Path][remote]; ok && onGitHub {
			out = append(out, headTarget{wt: wt, ask: headAsk{repo: target, branch: branch, tip: wt.copies.result.tip}})
		}
	}

	return out
}

// copiedInto keeps, for each of targets, worktrees fully copied onto a pushed branch, the
// pull request that branch heads, from prs, which resolveHeads returned, whatever links it.
func copiedInto(targets []headTarget, prs map[headKey]*LinkedItem) {
	for _, t := range targets {
		if pr := prs[t.ask.cacheKey()]; pr != nil {
			linked := *pr
			t.wt.copies.pr = &linked
		}
	}
}

// branchAsk works out the pushed branch a linked worktree on a local branch may be linked
// through: the remote-tracking branch of the same name the clone has, of its upstream's
// remote first, then origin's, then the other GitHub remotes' by name. An upstream of
// another name is not followed: a branch made from a remote-tracking branch follows the
// branch it started from, its base rather than its head. None of a remote's default
// branches ever links.
func branchAsk(wt *Worktree, remotes map[string]repo.Name, refs cloneRefs) (headAsk, bool) {
	if !onOwnBranch(wt) || refs.isDefault(wt.Branch) {
		return headAsk{}, false
	}

	for _, ref := range pushedAs(wt.Branch, remotes, refs) {
		remote, branch, ok := trackingOf(ref)
		tip, pushed := refs.tracking[ref]
		target, onGitHub := remotes[remote]

		if ok && pushed && onGitHub && refs.defaults[remote] != branch {
			return headAsk{repo: target, branch: branch, tip: tip}, true
		}
	}

	return headAsk{}, false
}

// onOwnBranch reports a linked worktree whose directory is there, on a local branch git
// can be asked about by name.
func onOwnBranch(wt *Worktree) bool {
	return !wt.Main && !wt.Prunable && !wt.Detached && gitread.ValidBranch(wt.Branch)
}

// pushedAs lists the remote-tracking branches a local branch may have been pushed as, most
// likely first: its upstream, when that has the same name, then that name on each GitHub
// remote, origin's first.
func pushedAs(branch string, remotes map[string]repo.Name, refs cloneRefs) []string {
	var out []string

	if lb, ok := refs.local(branch); ok {
		if _, name, ok := trackingOf(lb.upstream); ok && name == branch {
			out = append(out, lb.upstream)
		}
	}

	for _, remote := range rankedRemotes(remotes) {
		out = append(out, remotesRef+remote+"/"+branch)
	}

	return out
}
