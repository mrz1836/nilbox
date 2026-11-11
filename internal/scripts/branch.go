package scripts

import (
	"context"
	"strings"
	"time"

	"github.com/mrz1836/nilbox/internal/clock"
	"github.com/mrz1836/nilbox/internal/gitread"
	"github.com/mrz1836/nilbox/internal/paths"
)

// OriginKept is how long the answer origin gave about a clone's branches is kept: a
// branch's status within it asks origin nothing, unless refreshed.
const OriginKept = 5 * time.Minute

// The remote a branch is pulled from, and the prefix of its branches' refs.
const (
	originRemote = "origin"
	headsRef     = "refs/heads/"
)

// shortLen is how many characters of a commit a sentence shows.
const shortLen = 7

// BranchState is where a clone's branch stands against the branch it follows on origin.
type BranchState string

// The states of a clone's branch.
const (
	// BranchUpToDate is a branch at origin's commit.
	BranchUpToDate BranchState = "upToDate"

	// BranchBehind is a branch origin has commits for, with none of its own: a pull
	// fast-forwards it. A commit of origin's the clone hasn't fetched counts as behind
	// too, since only the pull can tell; it refuses, fast-forward only, a branch that
	// turns out to have commits of its own.
	BranchBehind BranchState = "behind"

	// BranchAhead is a branch with commits origin doesn't have, and none to bring.
	BranchAhead BranchState = "ahead"

	// BranchDiverged is a branch with commits of its own while origin has others: no
	// fast-forward brings it level.
	BranchDiverged BranchState = "diverged"

	// BranchDetached is a clone whose HEAD is detached, on no branch.
	BranchDetached BranchState = "detached"

	// BranchNoUpstream is a branch that follows no branch on origin: none at all, another
	// remote's, or one origin no longer has.
	BranchNoUpstream BranchState = "noUpstream"

	// BranchInProgress is a clone git is in the middle of a merge, a rebase, a
	// cherry-pick, or a revert in.
	BranchInProgress BranchState = "inProgress"

	// BranchUnknown is a status that couldn't be worked out: origin didn't answer, or the
	// clone couldn't be read.
	BranchUnknown BranchState = "unknown"

	// BranchOff is no status at all: the Scripts integration is off, so neither the clone
	// nor origin is read, and nothing is pulled.
	BranchOff BranchState = "off"
)

// BranchStatus is a clone's branch against origin's, for Repo scripts' header: whether a
// pull would bring it newer commits. Only a branch that is behind is offered one.
type BranchStatus struct {
	// Dir is the clone, with ~; Branch the branch it is on, or "" when HEAD is detached;
	// and Upstream origin's branch it follows, or "" when it follows none there.
	Dir      string `json:"dir"`
	Branch   string `json:"branch"`
	Upstream string `json:"upstream"`

	State BranchState `json:"state"`

	// Local and Origin are the branch's commit and origin's, shortened, or "" when
	// unknown.
	Local  string `json:"local"`
	Origin string `json:"origin"`

	// The note says where the branch stands in a sentence, and for one no pull is
	// offered on, why.
	Note string `json:"note"`

	// CheckedAt is when origin was asked, as RFC 3339, or "" when it wasn't.
	CheckedAt string `json:"checkedAt"`
}

// keptHeads is origin's branches, each with its commit, as a clone last asked, and when.
type keptHeads struct {
	heads map[string]string
	at    time.Time
}

// BranchStatus returns where the clone at dir, as the picker lists it, with ~, stands
// against origin: whether its branch is behind the branch it follows on origin, up to
// date, ahead, or diverged, or why there is nothing to pull. It reads the clone with
// gitread every time, and asks origin for its branches with one ls-remote, which it keeps
// for OriginKept: within it, nothing asks origin again unless refresh is set. It never
// fetches. A clone that can't be read, or an origin that doesn't answer, is a status that
// says why, not an error. While the Scripts integration is off, it reads nothing and says
// so (BranchOff).
func (s *Service) BranchStatus(ctx context.Context, dir string, refresh bool) (BranchStatus, error) {
	if s.branchGit == nil {
		return BranchStatus{}, ErrNotSetUp
	}

	c, err := s.cloneAt(ctx, dir)
	if err != nil {
		return BranchStatus{}, err
	}

	cfg, err := s.config(ctx)
	if err != nil {
		return BranchStatus{}, err
	}

	st := BranchStatus{Dir: paths.Tilde(c.dir, s.home), State: BranchOff, Note: scriptsOff(cfg, s.toolsNow(ctx))}
	if st.Note == "" {
		st = s.branchStatus(ctx, c, refresh)
	}

	s.mu.Lock()
	s.shown[c.dir] = st.Branch
	s.mu.Unlock()

	return st, nil
}

// branchStatus works out the clone c's branch status, asking origin again when refresh
// is set or its answer is older than OriginKept.
func (s *Service) branchStatus(ctx context.Context, c clone, refresh bool) BranchStatus {
	f, stop, why := s.readBranch(ctx, c.dir)
	st := BranchStatus{Dir: paths.Tilde(c.dir, s.home), Branch: f.branch, Upstream: f.upstream}

	if stop != "" {
		st.State, st.Note = stop, why

		return st
	}

	kept, err := s.originHeads(ctx, c, refresh)
	if err != nil {
		return st.unknown("Couldn't ask origin about " + f.upstream + ": " + gitread.Message(err))
	}

	st.CheckedAt = clock.Format(kept.at)

	oid := kept.heads[f.upstream]
	if oid == "" {
		st.State, st.Note = BranchNoUpstream, "origin has no branch "+f.upstream+"."

		return st
	}

	st.Local, st.Origin = short(f.local), short(oid)

	if st.State, err = compare(ctx, s.branchGit, c.dir, f.local, oid); err != nil {
		return st.unknown("Couldn't compare " + f.branch + " with origin's: " + gitread.Message(err))
	}

	st.Note = stateNote(st)

	return st
}

// branchFacts is what a branch status and a pull read of the clone itself: the branch it
// is on, the branch that one follows on origin, and the branch's commit, "" for a branch
// with none yet.
type branchFacts struct {
	branch   string
	upstream string
	local    string
}

// readBranch reads the clone at dir's branch, what it follows on origin, and its commit.
// When there is nothing to pull, or no telling, it says so with the state that ends the
// look there, and why: HEAD detached, no branch followed on origin, git in the middle of
// something, or a read that failed.
func (s *Service) readBranch(ctx context.Context, dir string) (f branchFacts, stop BranchState, why string) {
	g := s.branchGit

	branch, err := g.CurrentBranch(ctx, dir)

	switch {
	case err != nil:
		return f, BranchUnknown, "Couldn't read the clone's branch: " + gitread.Message(err)
	case branch == "":
		return f, BranchDetached, "HEAD is detached, so there's no branch to pull."
	}

	f.branch = branch

	up, err := g.Upstream(ctx, dir, branch)
	if err != nil {
		return f, BranchUnknown, "Couldn't read what " + branch + " tracks: " + gitread.Message(err)
	}

	if f.upstream, why = originBranch(branch, up); why != "" {
		return f, BranchNoUpstream, why
	}

	op, err := g.InProgress(ctx, dir)

	switch {
	case err != nil:
		return f, BranchUnknown, "Couldn't tell whether git is in the middle of something here: " + gitread.Message(err)
	case op != gitread.OperationNone:
		return f, BranchInProgress, inProgressNote(op)
	}

	if f.local, err = g.LocalBranch(ctx, dir, branch); err != nil {
		return f, BranchUnknown, "Couldn't read " + branch + ": " + gitread.Message(err)
	}

	return f, "", ""
}

// unknown returns st as a status that couldn't be worked out, saying why.
func (st BranchStatus) unknown(why string) BranchStatus {
	st.State, st.Note, st.Local, st.Origin = BranchUnknown, why, "", ""

	return st
}

// originBranch returns the branch on origin that branch follows, as its upstream says, or
// else why it follows none there: no upstream, another remote's branch, or a name the app
// wouldn't hand git.
func originBranch(branch string, up gitread.Upstream) (upstream, why string) {
	name, onHeads := strings.CutPrefix(up.Ref, headsRef)

	switch {
	case up.Remote == "" || up.Ref == "":
		return "", branch + " doesn't track a branch on origin."
	case up.Remote == ".":
		return "", branch + " tracks another local branch, not one on origin."
	case up.Remote != originRemote:
		return "", branch + " tracks a branch on " + up.Remote + ", not origin."
	case !onHeads || !gitread.ValidBranch(name):
		return "", branch + " doesn't track a branch on origin."
	}

	return name, ""
}

// inProgressNote says that git is in the middle of op in the clone, which a pull waits for.
func inProgressNote(op gitread.Operation) string {
	return "git is " + string(op) + " here. Finish or abort that before pulling."
}

// originHeads returns origin's branches as the clone c last asked, while that answer is
// younger than OriginKept and refresh isn't set, and else asks origin again, one ls-remote
// per clone at a time, and keeps the answer.
func (s *Service) originHeads(ctx context.Context, c clone, refresh bool) (keptHeads, error) {
	if !refresh {
		s.mu.Lock()
		kept, ok := s.heads[c.dir]
		s.mu.Unlock()

		if age := s.clock.Now().Sub(kept.at); ok && age >= 0 && age < OriginKept {
			return kept, nil
		}
	}

	return s.asking.Do(ctx, c.dir, func(ctx context.Context) (keptHeads, error) {
		refs, err := s.branchGit.RemoteRefs(ctx, c.dir)
		if err != nil {
			return keptHeads{}, err
		}

		return s.keepHeads(c.dir, refs.Heads), nil
	})
}

// keepHeads keeps heads as origin's branches as the clone at dir asked just now, and
// returns them as kept.
func (s *Service) keepHeads(dir string, heads map[string]string) keptHeads {
	if heads == nil {
		heads = map[string]string{}
	}

	kept := keptHeads{heads: heads, at: s.clock.Now()}

	s.mu.Lock()
	s.heads[dir] = kept
	s.mu.Unlock()

	return kept
}

// compare works out where the branch at local stands against origin's commit oid. A
// branch with no commit yet takes origin's, and a commit the clone hasn't fetched is
// behind: only a pull can tell more, and a fast-forward-only one refuses safely.
func compare(ctx context.Context, g BranchGit, dir, local, oid string) (BranchState, error) {
	switch local {
	case oid:
		return BranchUpToDate, nil
	case "":
		return BranchBehind, nil
	}

	has, err := g.HasCommit(ctx, dir, oid)
	if err != nil || !has {
		return BranchBehind, err
	}

	behind, err := g.IsAncestor(ctx, dir, local, oid)
	if err != nil || behind {
		return BranchBehind, err
	}

	ahead, err := g.IsAncestor(ctx, dir, oid, local)
	if err != nil || ahead {
		return BranchAhead, err
	}

	return BranchDiverged, nil
}

// stateNote says where a branch that was compared with origin's stands.
func stateNote(st BranchStatus) string {
	theirs := "origin/" + st.Upstream

	switch st.State {
	case BranchUpToDate:
		return st.Branch + " is up to date with " + theirs + "."
	case BranchBehind:
		return theirs + " has newer commits. Pull fast-forwards " + st.Branch + " to them."
	case BranchAhead:
		return st.Branch + " has commits that aren't on " + theirs + " yet, and nothing to pull."
	case BranchDiverged:
		return st.Branch + " and " + theirs + " both have commits the other doesn't, so it can't be fast-forwarded."
	case BranchDetached, BranchNoUpstream, BranchInProgress, BranchUnknown, BranchOff:
	}

	return ""
}

// short shortens a commit for a sentence, or returns "" for none.
func short(oid string) string {
	if len(oid) > shortLen {
		return oid[:shortLen]
	}

	return oid
}
