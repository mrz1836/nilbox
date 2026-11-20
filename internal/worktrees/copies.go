package worktrees

import (
	"cmp"
	"context"
	"errors"
	"maps"
	"slices"
	"strconv"
	"strings"
	"sync"

	"golang.org/x/sync/errgroup"

	"github.com/mrz1836/nilbox/internal/execpath"
	"github.com/mrz1836/nilbox/internal/gitread"
	"github.com/mrz1836/nilbox/internal/paths"
	"github.com/mrz1836/nilbox/internal/repo"
	"github.com/mrz1836/nilbox/internal/textutil"
)

// Comparing a worktree's commits with the pushed branches'.
//
// A coding agent's session often works in a worktree of its own, on a branch no one pushes,
// while the session that started it copies its commits onto a branch of its own, by
// cherry-picking or rebasing them, and pushes that. A copy keeps the commit's change (git's
// patch ID), author, author time, and subject, but gets a new commit ID, so the original
// counts as not on GitHub (Worktree.Unpushed counts by commit ID) although its change is.
//
// For each linked worktree with commits not on GitHub (U), detectCopies compares them with
// the commits of the clone's pushed branches: the remote-tracking branches of its GitHub
// remotes, each remote's default branch aside. Per clone, one log reads U of every such
// worktree with at most copyLimit (copyUnpushedArgs; one per commit the worktrees were made
// at from GitHub), and one more the pushed commits not in the default branch whose commit
// time is at most copyWindow before the oldest of U's author times (copyCandidatesArgs): a
// copy keeps the author time and is made later. A pushed commit with a commit's author time,
// author email, and subject is its likely copy. For each pushed branch holding likely
// copies, most first and at most copyRefsMax of them, one rev-list --cherry-mark
// (copyCherryArgs) says which of U git takes for an identical change on it, which one log of
// their changes then confirms byte for byte (see copypatch.go: git's patch IDs leave
// whitespace out). A commit whose change and a likely copy's are the same has an identical
// copy; one with a likely copy whose change differs, if only by a space, has an edited copy,
// which is never taken for the commit; any other has none. A worktree whose every commit has an identical copy on one branch is fully
// copied onto it (Copies.full), and links to that branch's pull request when nothing else
// links it (LinkCopy). Anything that goes wrong, as git failing or warning, an object a
// partial clone lacks (the reads never fetch one), too many commits, a merge among U, or the
// copies spread over several branches, leaves it not fully copied, with the reason in
// Copies.Note, and never fails the listing.
//
// What a comparison found is kept in memory (copyCache) by the worktree's HEAD, the commit
// it was made at from GitHub, its clone's GitHub remotes, and every remote-tracking branch
// and where it points (cloneRefs.fingerprint), so a listing in which none of those changed
// runs no git for it.

// copyLimit is the most commits not on GitHub a worktree's copies are looked for among.
const copyLimit = detailLimit

// copyRefsMax is the most pushed branches one worktree's commits are compared with.
const copyRefsMax = 3

// copyBatchMax caps the commits one log of the commits not on GitHub lists, and
// copyCandidatesMax one log of the pushed commits: a log that reaches its cap compares
// nothing.
const (
	copyBatchMax      = 10_000
	copyCandidatesMax = 20_000
)

// copyWindow is how long before a commit's author time, in seconds, its copy's commit time
// may be: a copy is made after the commit, so only clocks that disagree put it before.
const copyWindow = 86_400

// CopyMark is how a commit that isn't on GitHub compares with the pushed branch its
// worktree's commits were compared with (Copies.Ref).
type CopyMark string

// How a commit compares with the pushed branch, as UnpushedCommit.Copy reports it.
const (
	// CopyNone is a commit with no copy there.
	CopyNone CopyMark = ""

	// CopyIdentical is a commit with a copy there, by its author, author time, and
	// subject, whose change is identical.
	CopyIdentical CopyMark = "identical"

	// CopyEdited is a commit with a copy there, by its author, author time, and subject,
	// whose change differs.
	CopyEdited CopyMark = "edited"
)

// Copies says how the commits of a worktree that aren't on GitHub compare with the pushed
// branches' commits, where cherry-picking or rebasing them puts their copies. Every field
// is a plain value; the zero value says nothing was compared, as for a worktree with every
// commit on GitHub.
type Copies struct {
	// Ref is the pushed branch they were compared with, as in origin/feature-x: the one
	// with an identical copy of each when there is one, else the one with the most; "" when
	// none has a copy of any.
	Ref string `json:"ref"`

	// Count is how many there are (Worktree.Unpushed) when they were compared, or couldn't
	// be (Note); 0 when there was nothing to compare them with.
	Count int `json:"count"`

	// Identical, Edited, and None count those with an identical copy on Ref, those whose
	// copy there has another change, and those with neither: Count together, unless Note
	// says they couldn't be compared.
	Identical int `json:"identical"`
	Edited    int `json:"edited"`
	None      int `json:"none"`

	// Recorded reports that Ref is gone, and what an earlier listing recorded when it was
	// there vouches for them instead: each had an identical copy on Ref, and the pull
	// request Ref headed, which GitHub keeps, has those copies. Identical is Count then.
	Recorded bool `json:"recorded"`

	// Unless it is "", Note says in a sentence why they couldn't be compared, or why their
	// identical copies don't count.
	Note string `json:"note"`
}

// full reports a worktree each of whose commits that aren't on GitHub has an identical copy
// on Ref, as this listing found.
func (c Copies) full() bool {
	return c.Count > 0 && c.Note == "" && c.Ref != "" && c.Identical == c.Count && !c.Recorded
}

// commitMark is how one commit compares with the branch compared with: its mark, and its
// likely copy there, for an identical or an edited one.
type commitMark struct {
	mark CopyMark
	copy string
}

// copyResult is what comparing a worktree's commits found.
type copyResult struct {
	copies Copies

	// ref is Copies.Ref's remote-tracking branch, as in refs/remotes/origin/feature-x, and
	// tip the commit it pointed at; both "" when there is no Ref.
	ref string
	tip string

	// commits are the worktree's commits that aren't on GitHub, when they were read.
	commits []string

	// marks says how each of commits compares with ref, by commit ID; one with none isn't
	// there.
	marks map[string]commitMark
}

// full reports a worktree fully copied onto ref.
func (r *copyResult) full() bool {
	return r != nil && r.copies.full()
}

// copyKey is what a worktree's copyResult was worked out from: its HEAD, the commit it was
// made at from GitHub, and its clone's GitHub remotes and remote-tracking branches.
type copyKey struct {
	head, fetched, remotes, refs string
}

// copyEntry is a copyResult kept for a worktree, with its clone and its key.
type copyEntry struct {
	clone  string
	key    copyKey
	result copyResult
}

// coverEntry is whether a worktree's copy record covers it, kept with its clone and key.
type coverEntry struct {
	clone   string
	key     coverKey
	covered bool
}

// copyCache keeps across listings each worktree's copyResult and whether its copy record
// covers it, by the worktree's resolved directory, whether a pull request's head has a
// record's branch commit (see copyjudge.go), and whether a commit and a likely copy make the
// same change byte for byte (see copypatch.go). It is safe for concurrent use.
type copyCache struct {
	mu      sync.Mutex
	entries map[string]copyEntry
	covers  map[string]coverEntry
	holds   map[holdKey]bool
	pairs   map[copyPair]bool
}

// newCopyCache returns an empty copyCache.
func newCopyCache() *copyCache {
	return &copyCache{
		entries: map[string]copyEntry{}, covers: map[string]coverEntry{}, holds: map[holdKey]bool{},
		pairs: map[copyPair]bool{},
	}
}

// pairsMax is how many verdicts of samePatch the cache keeps before it starts over.
const pairsMax = 16384

// samePair returns whether a commit and a likely copy make the same change, when that is
// kept.
func (c *copyCache) samePair(pair copyPair) (same, ok bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	same, ok = c.pairs[pair]

	return same, ok
}

// keepPair keeps whether a commit and a likely copy make the same change, for good: neither
// commit ever changes.
func (c *copyCache) keepPair(pair copyPair, same bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if len(c.pairs) >= pairsMax {
		clear(c.pairs)
	}

	c.pairs[pair] = same
}

// coverage returns whether the record of the worktree at path covers it, when that was
// worked out from key.
func (c *copyCache) coverage(path string, key coverKey) (covered, ok bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	e, found := c.covers[path]

	return e.covered, found && e.key == key
}

// keepCoverage keeps whether the record of the worktree at path, of clone, covers it, as
// worked out from key.
func (c *copyCache) keepCoverage(clone, path string, key coverKey, covered bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.covers[path] = coverEntry{clone: clone, key: key, covered: covered}
}

// holdsMax is how many answers of prHolds the cache keeps before it starts over.
const holdsMax = 4096

// held returns whether a pull request's head has a record's branch commit, as key names
// them, when that is kept: a yes for good, a no for the remote-tracking branches fp sums up.
func (c *copyCache) held(key holdKey, fp string) (held, ok bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.holds[key] {
		return true, true
	}

	key.refs = fp
	_, ok = c.holds[key]

	return false, ok
}

// keepHeld keeps whether a pull request's head has a record's branch commit, as key names
// them, a no for the remote-tracking branches fp sums up.
func (c *copyCache) keepHeld(key holdKey, fp string, held bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if len(c.holds) >= holdsMax {
		clear(c.holds)
	}

	if !held {
		key.refs = fp
	}

	c.holds[key] = held
}

// lookup returns the result kept for the worktree at path, when it was worked out from key.
func (c *copyCache) lookup(path string, key copyKey) (copyResult, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	e, ok := c.entries[path]

	return e.result, ok && e.key == key
}

// entry returns what is kept for the worktree at path.
func (c *copyCache) entry(path string) (copyEntry, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	e, ok := c.entries[path]

	return e, ok
}

// keep keeps the result worked out for the worktree at path, of clone, from key.
func (c *copyCache) keep(clone, path string, key copyKey, result copyResult) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.entries[path] = copyEntry{clone: clone, key: key, result: result}
}

// prune drops what is kept for clone's worktrees but those at listed.
func (c *copyCache) prune(clone string, listed map[string]bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	maps.DeleteFunc(c.entries, func(path string, e copyEntry) bool { return e.clone == clone && !listed[path] })
	maps.DeleteFunc(c.covers, func(path string, e coverEntry) bool { return e.clone == clone && !listed[path] })
}

// remotesKey sums up a clone's GitHub remotes, the only ones whose branches it compares
// with.
func remotesKey(remotes map[string]repo.Name) string {
	var b strings.Builder

	for _, name := range slices.Sorted(maps.Keys(remotes)) {
		b.WriteString(name + "=" + remotes[name].Key() + "\n")
	}

	return b.String()
}

// copyWork is one worktree whose commits a clone's comparison compares.
type copyWork struct {
	wt   *Worktree
	path string
	key  copyKey

	// result is what was found, once done; retry marks one git couldn't be run for, which the
	// next listing works out again rather than keep.
	result copyResult
	done   bool
	retry  bool

	// unpushed are its commits not on GitHub, once read.
	unpushed []copyCommit
}

// copyCommit is a commit as a comparison's logs read it: a commit not on GitHub with its
// parents, or a pushed one with the branch it was found on.
type copyCommit struct {
	oid     string
	parents []string
	ref     string
	at      int64
	email   string
	subject string
}

// likeness is what a copy keeps of a commit: its author time, author email, and subject.
func (c copyCommit) likeness() string {
	return strconv.FormatInt(c.at, 10) + "\x00" + c.email + "\x00" + c.subject
}

// detectCopies compares the commits not on GitHub of each linked worktree of repos with the
// pushed branches', a few clones at a time, and fills in each one's Copies (see this file's
// doc). remotes maps each repository's Path to its GitHub remotes, and refs to its refs, as
// read for this listing. A clone whose refs couldn't be read compares nothing, and one with
// no pushed branch of a GitHub remote has nothing to compare with, which takes no git.
func (s *Service) detectCopies(ctx context.Context, g git, repos []Repo, remotes map[string]map[string]repo.Name,
	refs map[string]cloneRefs,
) {
	var eg errgroup.Group

	eg.SetLimit(listConcurrency)

	for i := range repos {
		r := &repos[i]

		cr, ok := refs[r.Path]
		if !ok {
			continue
		}

		eg.Go(func() error {
			wts := make([]*Worktree, 0, len(r.Worktrees))
			for j := range r.Worktrees {
				wts = append(wts, &r.Worktrees[j])
			}

			s.detectCloneCopies(ctx, g, r.Path, wts, remotes[r.Path], cr, true)

			return nil
		})
	}

	_ = eg.Wait() // a comparison that fails only leaves its worktrees not copied
}

// detectCloneCopies compares the commits not on GitHub of wts, worktrees of the clone at
// dir, with what the cache keeps for those whose key is the same. With prune, wts are all
// the clone's worktrees, and what the cache keeps for any other is dropped.
func (s *Service) detectCloneCopies(ctx context.Context, g git, dir string, wts []*Worktree,
	remotes map[string]repo.Name, refs cloneRefs, prune bool,
) {
	todo, listed := s.copiesToCompare(wts, copyKey{remotes: remotesKey(remotes), refs: refs.fingerprint()})

	if prune {
		s.copies.prune(dir, listed)
	}

	if len(todo) == 0 {
		return
	}

	compareCopies(ctx, g, s.copies, dir, remotes, refs, todo)

	for _, w := range todo {
		w.wt.copies.result, w.wt.Copies = &w.result, w.result.copies

		if !w.retry && ctx.Err() == nil {
			s.copies.keep(dir, w.path, w.key, w.result)
		}
	}
}

// copiesToCompare fills in what the cache keeps for each of wts that may have copies, whose
// key, clone's key with the worktree's HEAD and the commit it was made at, is the same, and
// returns the others, to compare, and the resolved directories of all of them.
func (s *Service) copiesToCompare(wts []*Worktree, clone copyKey) ([]*copyWork, map[string]bool) {
	var todo []*copyWork

	listed := map[string]bool{}

	for _, wt := range wts {
		if !mayHaveCopies(wt) {
			continue
		}

		path := paths.Resolved(wt.Path)
		listed[path] = true
		key := copyKey{head: wt.Head, fetched: wt.fetched, remotes: clone.remotes, refs: clone.refs}

		// The same key means the same commits not on GitHub, and so the same count; a count
		// that differs was read amiss, and is compared again.
		if result, ok := s.copies.lookup(path, key); ok && (result.copies.Count == 0 || result.copies.Count == wt.Unpushed) {
			wt.copies.result, wt.Copies = &result, result.copies
		} else {
			todo = append(todo, &copyWork{wt: wt, path: path, key: key})
		}
	}

	return todo, listed
}

// mayHaveCopies reports a linked worktree whose directory is there, with commits not on
// GitHub, at a HEAD git can be asked about.
func mayHaveCopies(wt *Worktree) bool {
	return !wt.Main && !wt.Prunable && wt.Unpushed > 0 && gitread.IsCommitID(wt.Head)
}

// compareCopies compares the commits not on GitHub of todo, worktrees of the clone at dir,
// with its pushed branches, settling each one's result. A clone with no pushed branch has
// nothing to compare them with.
func compareCopies(ctx context.Context, g git, cache *copyCache, dir string, remotes map[string]repo.Name, refs cloneRefs,
	todo []*copyWork,
) {
	branches := pushedBranches(remotes, refs)

	for _, w := range todo {
		switch {
		case len(branches) == 0:
			w.done, w.result = true, copyResult{marks: map[string]commitMark{}}
		case w.wt.Unpushed > copyLimit:
			w.finish("It has more than " + strconv.Itoa(copyLimit) + " commits that aren't on GitHub, more than " +
				"are compared with the pushed branches.")
		}
	}

	readUnpushed(ctx, g, dir, todo)

	candidates, err := readCandidates(ctx, g, dir, remotes, refs, todo, branches)

	for _, w := range todo {
		switch {
		case w.done:
		case err != nil:
			w.fail(ctx, err)
		default:
			w.compare(ctx, g, cache, dir, refs, candidates)
		}
	}
}

// pushedBranches are the remote-tracking branches of a clone's GitHub remotes that copies
// are looked for on: every one but each remote's default branch.
func pushedBranches(remotes map[string]repo.Name, refs cloneRefs) map[string]bool {
	out := map[string]bool{}

	for ref := range refs.tracking {
		remote, branch, ok := trackingOf(ref)
		if _, onGitHub := remotes[remote]; ok && onGitHub && refs.defaults[remote] != branch {
			out[ref] = true
		}
	}

	return out
}

// finish settles a worktree whose commits couldn't be compared, with why.
func (w *copyWork) finish(note string) {
	w.done = true
	w.result = copyResult{copies: Copies{Count: w.wt.Unpushed, Note: note}, marks: map[string]commitMark{}}
}

// fail settles a worktree whose commits couldn't be compared because of err. One git could
// not be run for at all, or that ran past ctx's end, is compared again next time; any other
// failure would be the same again for the same key.
func (w *copyWork) fail(ctx context.Context, err error) {
	w.finish(compareFailure(err))
	w.retry = retryable(ctx, err)
}

// readUnpushed reads the commits not on GitHub of the worktrees of todo not yet settled, in
// one log for each commit they were made at from GitHub ("" for none), and settles each one
// whose commits can't be compared: a log that failed, a count that isn't the listing's (they
// changed meanwhile), or a merge among them.
func readUnpushed(ctx context.Context, g git, dir string, todo []*copyWork) {
	groups := map[string][]*copyWork{}

	for _, w := range todo {
		if !w.done {
			groups[w.wt.fetched] = append(groups[w.wt.fetched], w)
		}
	}

	for _, fetched := range slices.Sorted(maps.Keys(groups)) {
		group := groups[fetched]
		set, err := g.copyUnpushed(ctx, dir, headsOf(group), fetched)

		for _, w := range group {
			if err != nil {
				w.fail(ctx, err)
			} else {
				w.readFrom(set)
			}
		}
	}
}

// headsOf lists the HEADs of group's worktrees, each once.
func headsOf(group []*copyWork) []string {
	heads := make([]string, 0, len(group))

	for _, w := range group {
		if !slices.Contains(heads, w.wt.Head) {
			heads = append(heads, w.wt.Head)
		}
	}

	return heads
}

// readFrom takes the worktree's commits not on GitHub from set, its clone's worktrees'
// commits not on GitHub by commit ID, which reach one another through their parents.
func (w *copyWork) readFrom(set map[string]copyCommit) {
	reached := reach(set, w.wt.Head)

	switch {
	case len(reached) != w.wt.Unpushed:
		w.finish("Its commits that aren't on GitHub changed while they were compared with the pushed branches.")
	case slices.ContainsFunc(reached, func(c copyCommit) bool { return len(c.parents) > 1 }):
		w.finish("A merge is among its commits that aren't on GitHub, and a merge isn't compared with the " +
			"pushed branches.")
	default:
		w.unpushed = reached
	}
}

// reach returns the commits of set that head reaches through their parents, head first,
// each once: those set has of the commits head has.
func reach(set map[string]copyCommit, head string) []copyCommit {
	var out []copyCommit

	seen := map[string]bool{}
	stack := []string{head}

	for len(stack) > 0 {
		oid := stack[len(stack)-1]
		stack = stack[:len(stack)-1]

		c, ok := set[oid]
		if !ok || seen[oid] {
			continue
		}

		seen[oid] = true

		out = append(out, c)
		stack = append(stack, c.parents...)
	}

	return out
}

// readCandidates reads the commits of the clone's pushed branches that may be copies of the
// todo worktrees' commits not on GitHub, in one log: those not in the default branch whose
// commit time is at most copyWindow before the oldest of those commits' author times, each
// with the branch it was found on, kept to branches. None is read when no worktree's
// commits were.
func readCandidates(ctx context.Context, g git, dir string, remotes map[string]repo.Name, refs cloneRefs,
	todo []*copyWork, branches map[string]bool,
) ([]copyCommit, error) {
	since := int64(-1)

	for _, w := range todo {
		for _, c := range w.unpushed {
			if since < 0 || c.at < since {
				since = c.at
			}
		}
	}

	if since < 0 {
		return nil, nil
	}

	candidates, err := g.copyCandidates(ctx, dir, max(since-copyWindow, 0), defaultBase(remotes, refs))
	if err != nil {
		return nil, err
	}

	return slices.DeleteFunc(candidates, func(c copyCommit) bool { return !branches[c.ref] }), nil
}

// defaultBase is the default branch of the clone's first GitHub remote, origin first, whose
// HEAD the clone knows and whose branch it has, as in refs/remotes/origin/main, or "".
func defaultBase(remotes map[string]repo.Name, refs cloneRefs) string {
	for _, remote := range rankedRemotes(remotes) {
		branch, ok := refs.defaults[remote]
		if !ok {
			continue
		}

		if ref := remotesRef + remote + "/" + branch; refs.tracking[ref] != "" {
			return ref
		}
	}

	return ""
}

// compare compares the worktree's commits not on GitHub with candidates, the commits of
// its clone's pushed branches that may be their copies, and settles its result: one
// rev-list --cherry-mark for each branch holding likely copies of them, the most first and
// at most copyRefsMax, until one has an identical copy of every one.
func (w *copyWork) compare(ctx context.Context, g git, cache *copyCache, dir string, refs cloneRefs, candidates []copyCommit) {
	likely := likelyCopies(w.unpushed, candidates)

	var best *refComparison

	anywhere := map[string]bool{}

	for _, ref := range rankRefs(likely) {
		got, err := compareWith(ctx, g, cache, dir, w.wt.Head, ref, refs.tracking[ref], w.unpushed, likely[ref])
		if err != nil {
			w.fail(ctx, err)

			return
		}

		got.identicalTo(anywhere)

		if best == nil || got.closer(*best) {
			best = &got
		}

		if got.identical == len(w.unpushed) {
			break
		}
	}

	w.settle(best)

	if best != nil && best.identical < len(w.unpushed) && len(anywhere) == len(w.unpushed) {
		w.result.copies.Note = "Each of its commits has an identical copy on one of the pushed branches, but no " +
			"one branch has them all."
	}
}

// settle fills in the worktree's result from the comparison with the branch that came
// closest, or, for nil, as having no copy on any.
func (w *copyWork) settle(best *refComparison) {
	w.done = true
	w.result = copyResult{copies: Copies{Count: w.wt.Unpushed, None: w.wt.Unpushed}, marks: map[string]commitMark{}}

	w.result.commits = make([]string, 0, len(w.unpushed))
	for _, c := range w.unpushed {
		w.result.commits = append(w.result.commits, c.oid)
	}

	if best == nil {
		return
	}

	w.result.ref, w.result.tip, w.result.marks = best.ref, best.tip, best.marks
	w.result.copies.Ref = strings.TrimPrefix(best.ref, remotesRef)
	w.result.copies.Identical, w.result.copies.Edited = best.identical, best.edited
	w.result.copies.None = w.wt.Unpushed - best.identical - best.edited
}

// likelyCopies are, by pushed branch, the likely copies there of each of unpushed, by its
// commit ID: candidates with its author time, author email, and subject, newest first.
func likelyCopies(unpushed, candidates []copyCommit) map[string]map[string][]string {
	wanted := map[string]bool{}
	for _, c := range unpushed {
		wanted[c.likeness()] = true
	}

	found := map[string][]copyCommit{}

	for _, c := range candidates {
		if wanted[c.likeness()] {
			found[c.likeness()] = append(found[c.likeness()], c)
		}
	}

	out := map[string]map[string][]string{}

	for _, u := range unpushed {
		for _, c := range found[u.likeness()] {
			if out[c.ref] == nil {
				out[c.ref] = map[string][]string{}
			}

			out[c.ref][u.oid] = append(out[c.ref][u.oid], c.oid)
		}
	}

	return out
}

// rankRefs orders the branches of likely by how many commits have a likely copy there, most
// first, then by name, and keeps the first copyRefsMax.
func rankRefs(likely map[string]map[string][]string) []string {
	refs := slices.SortedFunc(maps.Keys(likely), func(a, b string) int {
		return cmp.Or(cmp.Compare(len(likely[b]), len(likely[a])), strings.Compare(a, b))
	})

	return refs[:min(len(refs), copyRefsMax)]
}

// refComparison is how a worktree's commits compare with one pushed branch, ref, at tip.
type refComparison struct {
	ref, tip          string
	identical, edited int
	marks             map[string]commitMark
}

// identicalTo adds to found each commit with an identical copy on the branch compared with.
func (c refComparison) identicalTo(found map[string]bool) {
	for oid, m := range c.marks {
		if m.mark == CopyIdentical {
			found[oid] = true
		}
	}
}

// closer reports a comparison that came closer than other: more identical copies, then more
// edited ones. A tie keeps other, the branch with more likely copies.
func (c refComparison) closer(other refComparison) bool {
	return c.identical > other.identical || (c.identical == other.identical && c.edited > other.edited)
}

// compareWith compares unpushed, head's commits not on GitHub, with the pushed branch ref,
// at tip, by their changes, in one rev-list --cherry-mark, then confirms each = byte for
// byte (confirmCopies). likely holds the likely copies there of each, by its commit ID: a
// commit marked = with a likely copy whose change is the same byte for byte has an identical
// copy, one with a likely copy whose change differs, by as little as a space, an edited
// copy, and any other none, whatever its change.
func compareWith(ctx context.Context, g git, cache *copyCache, dir, head, ref, tip string, unpushed []copyCommit,
	likely map[string][]string,
) (refComparison, error) {
	got := refComparison{ref: ref, tip: tip, marks: map[string]commitMark{}}

	same, err := g.cherryMarks(ctx, dir, tip, head)
	if err != nil {
		return refComparison{}, err
	}

	marked := map[string][]string{}

	for _, u := range unpushed {
		identical, listed := same[u.oid]

		switch {
		case !listed:
			return refComparison{}, errCherryIncomplete
		case identical && len(likely[u.oid]) > 0:
			marked[u.oid] = likely[u.oid]
		}
	}

	confirmed, err := confirmCopies(ctx, g, cache, dir, marked)
	if err != nil {
		return refComparison{}, err
	}

	for _, u := range unpushed {
		copies := likely[u.oid]

		switch {
		case len(copies) == 0:
		case confirmed[u.oid] != "":
			got.identical++
			got.marks[u.oid] = commitMark{mark: CopyIdentical, copy: confirmed[u.oid]}
		default:
			got.edited++
			got.marks[u.oid] = commitMark{mark: CopyEdited, copy: copies[0]}
		}
	}

	return got, nil
}

// errCherryIncomplete is returned when git's comparison with a pushed branch leaves out a
// commit that isn't on GitHub, which it lists whenever the two reads agree.
var errCherryIncomplete = errors.New("the comparison left out a commit that isn't on GitHub")

// compareFailure words why a worktree's commits couldn't be compared with the pushed
// branches, for Copies.Note: in git's own words when it gave any.
func compareFailure(err error) string {
	why := ""

	switch gitErr, ok := errors.AsType[*copyGitError](err); {
	case errors.Is(err, errTooManyCommits), errors.Is(err, errTooManyCandidates):
		why = textutil.Sentence(err.Error())
	case ok:
		why = textutil.Sentence(execpath.GitMessage(gitErr.stderr))
	}

	return strings.TrimSpace("Couldn't compare its commits with the pushed branches. " + why)
}
