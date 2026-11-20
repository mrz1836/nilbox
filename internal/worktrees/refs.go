package worktrees

import (
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"maps"
	"slices"
	"strings"

	"github.com/mrz1836/nilbox/internal/gitread"
	"github.com/mrz1836/nilbox/internal/repo"
)

// refsArgs lists a clone's local branches, its remote-tracking branches, and each remote's
// HEAD, one a line: each one's full name, the commit it points at, a local branch's
// upstream, the ref a symbolic ref points at, and the worktree that has a local branch
// checked out, separated by NULs.
func refsArgs() []string {
	return []string{
		"for-each-ref", "--format=%(refname)%00%(objectname)%00%(upstream)%00%(symref)%00%(worktreepath)",
		headsPrefix, remotesRef,
	}
}

// cloneRefs is a clone's branches as one read of its refs found them (refsArgs): what
// linking a worktree by its branch or by its commits' copies needs, and what the branches
// a removal offers are judged by.
type cloneRefs struct {
	// locals are the clone's local branches, each with its upstream.
	locals []localBranch

	// tracking maps each remote-tracking branch, as in refs/remotes/origin/feature-x, to the
	// commit it points at. A remote's HEAD is left out.
	tracking map[string]string

	// owners maps each of tracking to the remote it is of and its branch there, of those
	// whose remote the clone names (withOwners); one of no remote it names isn't any
	// remote's.
	owners map[string]remoteBranch

	// names are every remote the clone has, on GitHub or not, that owners were worked out
	// among.
	names []string

	// defaults maps each remote whose HEAD the clone knows to the branch it points at, the
	// repository's default branch, as in main: a remote of a plain name's from the read
	// itself, and one whose name holds a slash's once withOwners knows it is a remote.
	defaults map[string]string

	// heads maps each remote's HEAD the clone has, by the name between refs/remotes/ and
	// /HEAD, as in origin or origin/mirror, to the ref it points at.
	heads map[string]string
}

// remoteBranch is a remote-tracking branch's remote and its branch there.
type remoteBranch struct {
	remote, branch string
}

// withOwners returns refs with each remote-tracking branch's remote worked out among names,
// every remote the clone has: the one with the longest name that, with a slash, begins the
// branch's name past refs/remotes/. A remote's name may hold a slash, so the
// refs/remotes/origin/mirror/main of a remote called origin/mirror is that remote's main,
// never origin's branch mirror/main. Each of names whose HEAD the clone has has its default
// branch, as refs/remotes/origin/mirror/HEAD gives origin/mirror's.
func withOwners(refs cloneRefs, names []string) cloneRefs {
	refs.names = slices.Sorted(slices.Values(names))
	refs.owners = make(map[string]remoteBranch, len(refs.tracking))
	refs.defaults = defaultsAmong(refs, names)

	for ref := range refs.tracking {
		if owner, ok := ownerAmong(ref, names); ok {
			refs.owners[ref] = owner
		}
	}

	return refs
}

// ownerAmong works out the remote of names a remote-tracking branch, ref, is of, and its
// branch there (see withOwners), or false when none of names is its remote.
func ownerAmong(ref string, names []string) (remoteBranch, bool) {
	rest, ok := strings.CutPrefix(ref, remotesRef)
	if !ok {
		return remoteBranch{}, false
	}

	owner := ""

	for _, name := range names {
		if len(name) > len(owner) && strings.HasPrefix(rest, name+"/") {
			owner = name
		}
	}

	branch := strings.TrimPrefix(rest, owner+"/")

	return remoteBranch{remote: owner, branch: branch}, owner != "" && gitread.ValidBranch(branch)
}

// defaultsAmong returns refs' default branches with those of each of names whose HEAD refs
// has, a remote whose name holds a slash among them, in a map of its own.
func defaultsAmong(refs cloneRefs, names []string) map[string]string {
	out := make(map[string]string, len(refs.defaults))
	maps.Copy(out, refs.defaults)

	for remote, symref := range refs.heads {
		if branch, ok := defaultOf(remote, symref); ok && slices.Contains(names, remote) {
			out[remote] = branch
		}
	}

	return out
}

// readRefs reads the refs of the clone at dir in one git run.
func (g git) readRefs(ctx context.Context, dir string) (cloneRefs, error) {
	stdout, _, err := g.do(ctx, dir, refsArgs()...)
	if err != nil {
		return cloneRefs{}, err
	}

	return parseRefs(stdout), nil
}

// parseRefs reads what refsArgs prints. A line that is not a branch and a commit is left
// out, and so is a remote-tracking branch whose remote or branch git could not be asked
// about by name. A remote's HEAD names its default branch here only when the remote's name
// is plain: one whose name holds a slash waits for withOwners.
func parseRefs(stdout []byte) cloneRefs {
	refs := cloneRefs{tracking: map[string]string{}, defaults: map[string]string{}, heads: map[string]string{}}

	for line := range strings.Lines(string(stdout)) {
		fields := strings.Split(strings.TrimRight(line, "\n"), "\x00")
		if len(fields) == 5 && gitread.IsCommitID(fields[1]) {
			addRef(&refs, fields[0], fields[1], fields[2], fields[3], fields[4])
		}
	}

	return refs
}

// addRef keeps in r one ref refsArgs printed: name, the commit oid it points at, the upstream of a
// local branch, the ref a symbolic ref points at, and the worktree a local branch is
// checked out in.
func addRef(r *cloneRefs, name, oid, upstream, symref, worktree string) {
	if branch, ok := strings.CutPrefix(name, headsPrefix); ok && branch != "" {
		r.locals = append(r.locals, localBranch{name: branch, head: oid, worktree: worktree, upstream: upstream})

		return
	}

	remote, head := remoteHeadOf(name)
	if head && symref != "" {
		r.heads[remote] = symref
	}

	switch {
	case head && gitread.ValidRemoteName(remote):
		if branch, ok := defaultOf(remote, symref); ok {
			r.defaults[remote] = branch
		}
	case symref == "" && isTrackingRef(name):
		r.tracking[name] = oid
	}
}

// remoteHeadOf returns the name whose HEAD ref is, refs/remotes/<name>/HEAD, which may hold
// a slash.
func remoteHeadOf(ref string) (string, bool) {
	rest, ok := strings.CutPrefix(ref, remotesRef)
	if !ok {
		return "", false
	}

	remote, ok := strings.CutSuffix(rest, "/HEAD")

	return remote, ok && remote != ""
}

// defaultOf returns the branch symref, where remote's HEAD points, names on remote itself:
// its default branch. One on another remote, or of a name git refuses, is none.
func defaultOf(remote, symref string) (string, bool) {
	branch, ok := strings.CutPrefix(symref, remotesRef+remote+"/")

	return branch, ok && gitread.ValidBranch(branch)
}

// defaultNames lists the branches the remotes' HEADs point at, each once, in name order:
// the repository's default branches, which nothing deletes.
func (r cloneRefs) defaultNames() []string {
	names := slices.Sorted(maps.Values(r.defaults))

	return slices.Compact(names)
}

// isDefault reports a branch that is some remote's default branch.
func (r cloneRefs) isDefault(branch string) bool {
	return slices.Contains(slices.Collect(maps.Values(r.defaults)), branch)
}

// local returns the local branch named name.
func (r cloneRefs) local(name string) (localBranch, bool) {
	return findBranch(r.locals, name)
}

// owner returns the remote a remote-tracking branch the clone has is of, and its branch
// there (see withOwners), or false for one the clone hasn't, or of no remote it names.
func (r cloneRefs) owner(ref string) (remote, branch string, ok bool) {
	o, ok := r.owners[ref]

	return o.remote, o.branch, ok
}

// branchOf returns the branch a remote-tracking branch is of on its remote (see withOwners),
// whether the clone still has it or not, as a recorded one may be gone: "" when no remote
// the clone names is its remote.
func (r cloneRefs) branchOf(ref string) string {
	o, ok := r.owners[ref]
	if !ok {
		o, ok = ownerAmong(ref, r.names)
	}

	if !ok {
		return ""
	}

	return o.branch
}

// fingerprint sums up the remote-tracking branches, where each points, and the remotes'
// HEADs: two reads of a clone's refs with the same fingerprint saw the same remote state,
// so anything worked out from it alone, as which commits are on GitHub, still holds.
func (r cloneRefs) fingerprint() string {
	sum := sha256.New()

	for _, ref := range slices.Sorted(maps.Keys(r.tracking)) {
		sum.Write([]byte(ref + "\x00" + r.tracking[ref] + "\n"))
	}

	for _, remote := range slices.Sorted(maps.Keys(r.heads)) {
		sum.Write([]byte("HEAD\x00" + remote + "\x00" + r.heads[remote] + "\n"))
	}

	return hex.EncodeToString(sum.Sum(nil))
}

// rankedRemotes lists the remote names of remotes, a clone's GitHub remotes, origin's
// first, then the others by name.
func rankedRemotes(remotes map[string]repo.Name) []string {
	return slices.SortedFunc(maps.Keys(remotes), func(a, b string) int {
		return cmp.Or(cmp.Compare(remoteRank(a), remoteRank(b)), strings.Compare(a, b))
	})
}
