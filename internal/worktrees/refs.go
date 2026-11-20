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

	// defaults maps each remote whose HEAD the clone knows to the branch it points at, the
	// repository's default branch, as in main.
	defaults map[string]string
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
// out, and so is a remote-tracking branch or a remote's HEAD whose remote or branch git
// could not be asked about by name.
func parseRefs(stdout []byte) cloneRefs {
	refs := cloneRefs{tracking: map[string]string{}, defaults: map[string]string{}}

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

	if remote, ok := remoteHeadOf(name); ok {
		if branch, ok := strings.CutPrefix(symref, remotesRef+remote+"/"); ok && gitread.ValidBranch(branch) {
			r.defaults[remote] = branch
		}

		return
	}

	if symref == "" && isTrackingRef(name) {
		r.tracking[name] = oid
	}
}

// remoteHeadOf returns the remote whose HEAD ref is, refs/remotes/<remote>/HEAD.
func remoteHeadOf(ref string) (string, bool) {
	if !isRemoteHead(ref) {
		return "", false
	}

	return strings.TrimSuffix(strings.TrimPrefix(ref, remotesRef), "/HEAD"), true
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

// trackingOf splits a remote-tracking branch into its remote and its branch.
func trackingOf(ref string) (remote, branch string, ok bool) {
	if !isTrackingRef(ref) {
		return "", "", false
	}

	return strings.Cut(strings.TrimPrefix(ref, remotesRef), "/")
}

// fingerprint sums up the remote-tracking branches, where each points, and the remotes'
// HEADs: two reads of a clone's refs with the same fingerprint saw the same remote state,
// so anything worked out from it alone, as which commits are on GitHub, still holds.
func (r cloneRefs) fingerprint() string {
	sum := sha256.New()

	for _, ref := range slices.Sorted(maps.Keys(r.tracking)) {
		sum.Write([]byte(ref + "\x00" + r.tracking[ref] + "\n"))
	}

	for _, remote := range slices.Sorted(maps.Keys(r.defaults)) {
		sum.Write([]byte("HEAD\x00" + remote + "\x00" + r.defaults[remote] + "\n"))
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
