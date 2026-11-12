// Package release is the Release page's service: which clones have changes to release,
// and the release itself, through mage-x's magex. Prepare gathers a release's facts and
// shows the sheet Decide works out from them; Start gathers them again under the clone's
// lock, decides again, and runs magex through scriptrun; and once magex ends, whatever its
// exit status, the release is verified: origin is asked for the tag, and the clone for the
// branch it is on.
package release

import (
	"cmp"
	"context"
	"slices"
	"strings"
	"time"

	"github.com/mrz1836/nilbox/internal/paths"
	"github.com/mrz1836/nilbox/internal/repo"
	"github.com/mrz1836/nilbox/internal/reporules"
)

// Group is where the Release page lists a repository.
type Group string

// The groups, in the order the page shows them.
const (
	// GroupUnreleased has commits on its default branch since its newest version tag.
	GroupUnreleased Group = "unreleased"

	// GroupUpToDate has its newest version tag on its newest commit, or nothing since.
	GroupUpToDate Group = "upToDate"

	// GroupNoReleases has no vX.Y.Z tag yet.
	GroupNoReleases Group = "noReleases"

	// GroupCantRelease is archived, read-only to the user, or not found on GitHub.
	GroupCantRelease Group = "cantRelease"

	// GroupMuted is a repository the user muted, itself or its org (see the reporules
	// package): listed apart, and never asked about.
	GroupMuted Group = "muted"
)

// readOnlyPermissions are the GitHub permissions that may not push a tag. An empty one,
// GitHub not saying, isn't among them: magex finds out when it pushes.
func readOnlyPermissions() []string {
	return []string{"READ", "TRIAGE", "NONE"}
}

// Checks is the rollup state of the checks on a commit, as GitHub reports it.
type Checks string

// The checks' states; ChecksNone is a commit without checks.
const (
	ChecksNone     Checks = ""
	ChecksSuccess  Checks = "SUCCESS"
	ChecksPending  Checks = "PENDING"
	ChecksExpected Checks = "EXPECTED"
	ChecksFailure  Checks = "FAILURE"
	ChecksError    Checks = "ERROR"
)

// RepoStatus is what GitHub says about a repository, round one of the candidates: core
// adapts the client's answer to it.
type RepoStatus struct {
	// Archived and Permission are the repository's state and the user's access to it.
	Archived   bool
	Permission string

	// DefaultBranch is the default branch, and HeadOID, HeadAt, Headline, and Checks its
	// newest commit: its ID, when it was committed, its headline, and its checks' rollup
	// state.
	DefaultBranch string
	HeadOID       string
	HeadAt        time.Time
	Headline      string
	Checks        Checks

	// Tags are the newest tags by commit date, each with the commit it points at.
	Tags []Tag
}

// Tag is a tag and the commit it points at, peeled through an annotated tag.
type Tag struct {
	Name   string
	Commit string
}

// CompareRequest asks how far a branch is ahead of a tag.
type CompareRequest struct {
	Repo   repo.Name
	Tag    string
	Branch string
}

// Compare is how far a branch is ahead of a tag: the count, and the newest commits.
type Compare struct {
	Ahead   int
	Commits []Commit
}

// Commit is a commit as the Release page shows it.
type Commit struct {
	OID      string    `json:"oid"`
	Headline string    `json:"headline"`
	At       time.Time `json:"at"`
}

// GitHub is what Release reads from GitHub: each repository's status, then, for those
// with a version, how far the default branch is ahead of it. Both key their answers by
// repo.Name.Key(), and leave out a repository GitHub can't find. answered, when set, is
// told as GitHub answers each batch, with how many of repos or reqs it held, from any
// goroutine; nil hears nothing. Core adapts the client.
type GitHub interface {
	ReleaseStatus(ctx context.Context, repos []repo.Name, answered func(n int)) (map[string]RepoStatus, error)
	ReleaseCompare(ctx context.Context, reqs []CompareRequest, answered func(n int)) (map[string]Compare, error)
}

// Candidate is a repository the Release page lists, with the clone it releases from.
type Candidate struct {
	// Repo is the repository, "owner/name", and Dir its clone, with ~ for home.
	Repo string `json:"repo"`
	Dir  string `json:"dir"`

	// Group is where it's listed, and Reason, for GroupCantRelease, says why. A row in
	// GroupMuted may hold nothing but Repo and Dir, when GitHub wasn't asked about it.
	Group  Group  `json:"group"`
	Reason string `json:"reason"`

	// Version is the newest version tag, or "" for none, and Ahead the commits on the
	// default branch since it, the newest of them in Commits.
	Version string   `json:"version"`
	Ahead   int      `json:"ahead"`
	Commits []Commit `json:"commits"`

	// DefaultBranch, Headline, HeadAt, and Checks describe the default branch's newest
	// commit; HeadAt is RFC 3339, or "".
	DefaultBranch string `json:"defaultBranch"`
	Headline      string `json:"headline"`
	HeadAt        string `json:"headAt"`
	Checks        Checks `json:"checks"`

	// Clone is the clone's state, for the unreleased group only, or nil.
	Clone *CloneState `json:"clone"`

	// dir is the clone's absolute folder, origin its repository, and headAt the newest
	// commit's time, for sorting.
	dir    string
	origin repo.Name
	headAt time.Time
}

// CloneState is what a clone's read-only checks found, for the Release page's row: the
// branch it's on, its uncommitted files, its commits not on GitHub and those it lacks, and
// the version tags only it has.
type CloneState struct {
	Branch     string   `json:"branch"`
	Detached   bool     `json:"detached"`
	Dirty      int      `json:"dirty"`
	NotPushed  int      `json:"notPushed"`
	Behind     bool     `json:"behind"`
	LocalOnly  []string `json:"localOnly"`
	Unreadable bool     `json:"unreadable"`
}

// clone is a clone the candidates are worked out for: its folder and its origin.
type clone struct {
	dir    string
	origin repo.Name
}

// candidateFor works out where a clone's repository is listed from GitHub's status,
// and, when it has a version, the comparison with it. compared reports whether the
// comparison was asked for and answered.
func candidateFor(c clone, status RepoStatus, found bool, since Compare, compared bool) Candidate {
	out := Candidate{
		Repo: c.origin.String(), dir: c.dir, origin: c.origin, Commits: []Commit{},
		DefaultBranch: status.DefaultBranch, Headline: status.Headline, Checks: status.Checks,
		headAt: status.HeadAt,
	}

	if !status.HeadAt.IsZero() {
		out.HeadAt = status.HeadAt.UTC().Format(time.RFC3339)
	}

	switch {
	case !found:
		out.Group, out.Reason = GroupCantRelease, "GitHub doesn't show this repository to you."
	case status.Archived:
		out.Group, out.Reason = GroupCantRelease, "It's archived."
	case slices.Contains(readOnlyPermissions(), status.Permission):
		out.Group, out.Reason = GroupCantRelease, "You can only read it."
	case status.DefaultBranch == "" || status.HeadOID == "":
		out.Group, out.Reason = GroupCantRelease, "It has no commits on GitHub yet."
	default:
		placeVersioned(&out, status, since, compared)
	}

	return out
}

// placeVersioned lists a releasable repository by its newest version: none yet, on the
// newest commit, or with commits since.
func placeVersioned(out *Candidate, status RepoStatus, since Compare, compared bool) {
	tag, ok := newestVersionTag(status.Tags)
	if !ok {
		out.Group = GroupNoReleases

		return
	}

	out.Version = tag.Name

	switch {
	case tag.Commit == status.HeadOID:
		out.Group = GroupUpToDate
	case !compared:
		// GitHub couldn't compare the tag with the branch: list it as unreleased, with
		// no count, rather than hide changes that may be there.
		out.Group = GroupUnreleased
	case since.Ahead > 0:
		out.Group, out.Ahead = GroupUnreleased, since.Ahead
		out.Commits = slices.Clone(since.Commits)
		slices.SortStableFunc(out.Commits, func(a, b Commit) int { return b.At.Compare(a.At) })
	default:
		out.Group = GroupUpToDate
	}
}

// newestVersionTag is the tag of the highest version among tags, as magex picks it.
func newestVersionTag(tags []Tag) (Tag, bool) {
	var (
		best    Tag
		version Version
		found   bool
	)

	for _, t := range tags {
		v, ok := ParseVersion(t.Name)
		if ok && (!found || version.Less(v)) {
			best, version, found = t, v, true
		}
	}

	return best, found
}

// mutedCandidate is the row of a clone whose repository is muted when the list is read:
// its repository and folder, with ~ for home, and nothing GitHub would say of it, since
// GitHub isn't asked.
func mutedCandidate(c clone, home string) Candidate {
	return Candidate{
		Repo: c.origin.String(), Dir: paths.Tilde(c.dir, home), Group: GroupMuted, Commits: []Commit{},
		dir: c.dir, origin: c.origin,
	}
}

// unread reports whether the row is one GitHub wasn't asked about, as its repository was
// muted when it was read.
func (c *Candidate) unread() bool {
	return c.Group == GroupMuted
}

// groupOrder is where each group comes on the page.
func groupOrder(g Group) int {
	switch g {
	case GroupUnreleased:
		return 0
	case GroupUpToDate:
		return 1
	case GroupNoReleases:
		return 2
	case GroupCantRelease:
		return 3
	case GroupMuted:
		return 4
	default:
		return 5
	}
}

// arranged returns a copy of list as the page shows it under rules: a row whose repository
// is muted now in GroupMuted, whatever GitHub said of it, so it shows in its own group again
// as soon as it is unmuted, and, in each group, the pinned repositories first, in the
// group's own order. list is never changed.
func arranged(list []Candidate, rules reporules.Rules) []Candidate {
	out := make([]Candidate, 0, len(list))

	for _, c := range list {
		if rules.IsMuted(c.origin) {
			c.Group = GroupMuted
		}

		out = append(out, c)
	}

	sortCandidates(out)

	slices.SortStableFunc(out, func(a, b Candidate) int {
		if c := cmp.Compare(groupOrder(a.Group), groupOrder(b.Group)); c != 0 {
			return c
		}

		return cmp.Compare(pinRank(a, rules), pinRank(b, rules))
	})

	return out
}

// pinRank is 0 for a row whose repository is pinned, itself or its org, else 1.
func pinRank(c Candidate, rules reporules.Rules) int {
	if rules.IsPinned(c.origin) {
		return 0
	}

	return 1
}

// sortCandidates orders the candidates as the page lists them: by group, the unreleased
// most recent commit first, the rest by repository.
func sortCandidates(list []Candidate) {
	slices.SortStableFunc(list, func(a, b Candidate) int {
		if c := cmp.Compare(groupOrder(a.Group), groupOrder(b.Group)); c != 0 {
			return c
		}

		if a.Group == GroupUnreleased {
			if c := b.headAt.Compare(a.headAt); c != 0 {
				return c
			}
		}

		return cmp.Compare(strings.ToLower(a.Repo), strings.ToLower(b.Repo))
	})
}
