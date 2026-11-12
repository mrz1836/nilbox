package release

import (
	"encoding/json"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/brand"
	"github.com/mrz1836/nilbox/internal/clonelock"
	"github.com/mrz1836/nilbox/internal/gitread"
	"github.com/mrz1836/nilbox/internal/integrationprefs"
	"github.com/mrz1836/nilbox/internal/integrations"
	"github.com/mrz1836/nilbox/internal/repo"
)

// The commits the Decide tests name: newest is origin's master, tagged nothing, and older
// carries v1.4.2.
const (
	newestOID = "a1b2c3d4e5f60718293a4b5c6d7e8f9012345678"
	olderOID  = "0f1e2d3c4b5a69788796a5b4c3d2e1f001234567"
	magexPath = "/opt/tools/magex"

	// shownVersion is the newest version in readyFacts.
	shownVersion = "v1.4.2"
)

// allFeatures are every magex feature Release reads.
func allFeatures() []integrations.Feature {
	return []integrations.Feature{
		integrations.FeaturePushFails, integrations.FeatureExpect, integrations.FeatureBumpJSON,
		integrations.FeatureFastForwardPull,
	}
}

// readyFacts are a release that can start at once: magex with every feature, acme-org/sdk
// on GitHub with master as its default, the clone clean on master, in step with origin,
// whose newest commit is one past v1.4.2 with its checks passed.
func readyFacts() Facts {
	return Facts{
		Tool: ToolFacts{
			Path: magexPath, Version: "1.30.2", State: integrations.StateFound, Features: allFeatures(),
			MinVersion: "1.29.5", ExpectSince: "1.30.0",
		},
		Repo: RepoFacts{
			Name: repo.Name{Owner: "acme-org", Repo: "sdk"}, Found: true, Permission: "WRITE",
			DefaultBranch: branchMaster, HeadOID: newestOID, Checks: ChecksSuccess, Branches: []string{branchMaster},
		},
		Clone: CloneFacts{
			Dir: "/work/sdk", OriginMatches: true, Changes: []string{}, Branch: branchMaster, Elsewhere: []string{},
			LocalHead: newestOID, TrackingHead: newestOID,
			Upstream: gitread.Upstream{Remote: "origin", Ref: "refs/heads/master"},
			Reached:  true, RemoteHeads: map[string]string{branchMaster: newestOID},
			RemoteTags: map[string]gitread.RemoteTag{shownVersion: {Object: olderOID, Commit: olderOID}},
			LocalTags:  []string{shownVersion}, Hooks: []string{}, ShellRead: true,
		},
		Choice: Choice{RunsIn: integrationprefs.RunsInApp},
	}
}

// checkOf returns the offer's check with id, and whether it has one.
func checkOf(o Offer, id CheckID) (Check, bool) {
	i := slices.IndexFunc(o.Checks, func(c Check) bool { return c.ID == id })
	if i < 0 {
		return Check{}, false
	}

	return o.Checks[i], true
}

// bumpOf returns the offer's card for b.
func bumpOf(t *testing.T, o Offer, b Bump) BumpOffer {
	t.Helper()

	i := slices.IndexFunc(o.Bumps, func(c BumpOffer) bool { return c.Bump == b })
	require.GreaterOrEqual(t, i, 0, "no %s card", b)

	return o.Bumps[i]
}

func TestDecideReadyRelease(t *testing.T) {
	t.Parallel()

	o := Decide(readyFacts())

	assert.True(t, o.Ready)
	assert.False(t, o.Anyway)
	assert.Equal(t, "acme-org/sdk", o.Repo)
	assert.Equal(t, branchMaster, o.Branch)
	assert.Equal(t, branchMaster, o.Current)
	assert.Equal(t, newestOID, o.Head)
	assert.Equal(t, BumpPatch, o.Bump, "patch is preselected (D11)")
	assert.Equal(t, "v1.4.3", o.Version)
	assert.Equal(t, "Release v1.4.3", o.Button)
	assert.Equal(t, []BranchChoice{{Name: branchMaster, Default: true}}, o.Branches)
	assert.Empty(t, o.BranchNote)

	want := []Check{
		checkOK(CheckDirty, "No uncommitted changes"),
		checkOK(CheckWhere, "You're on master"),
		checkOK(CheckReach, "Reaches origin with your git credentials"),
		checkOK(CheckChecks, "Checks passed on a1b2c3d"),
	}
	assert.Equal(t, want, o.Checks, "a ready release shows only what passed")
}

// decideCase is one fact changed from readyFacts, and the check it makes.
type decideCase struct {
	name   string
	change func(f *Facts)
	id     CheckID
	state  CheckState
	text   string
	fixes  []Fix
	cmds   []string

	// absent reports the change takes the check's line away instead.
	absent bool
}

// assertCase runs one decideCase: the check it names, and whether the release can start.
func assertCase(t *testing.T, tc decideCase) {
	t.Helper()

	f := readyFacts()
	tc.change(&f)
	o := Decide(f)

	got, found := checkOf(o, tc.id)
	if tc.absent {
		assert.False(t, found, "no %s line, got %+v", tc.id, got)
		assert.True(t, o.Ready)

		return
	}

	require.True(t, found, "a %s line in %+v", tc.id, o.Checks)
	assert.Equal(t, tc.state, got.State)
	assert.Equal(t, tc.text, got.Text)
	assert.Equal(t, append([]Fix{}, tc.fixes...), got.Fixes)
	assert.Equal(t, append([]string{}, tc.cmds...), got.Commands)
	assert.Equal(t, tc.state != CheckBlock, o.Ready, "only a block stops the release")
	assert.Equal(t, tc.state == CheckWarn, o.Anyway)

	if tc.state == CheckWarn {
		assert.Equal(t, "Release v1.4.3 anyway", o.Button)
	}
}

// TestDecideChecks has a case for every check (§3.4.3), in each of its states.
func TestDecideChecks(t *testing.T) {
	t.Parallel()

	tests := slices.Concat(toolAndRepoCases(), cloneCases(), branchCases(), originCases(), signingCases(), otherCases())

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			assertCase(t, tc)
		})
	}
}

// toolAndRepoCases are magex, the repository on GitHub, the clone's origin, and access.
func toolAndRepoCases() []decideCase {
	return []decideCase{
		{
			name: "magex missing", id: CheckTool, state: CheckBlock, text: "magex isn't available",
			fixes:  []Fix{FixOpenSettings},
			change: func(f *Facts) { f.Tool = ToolFacts{State: integrations.StateMissing} },
		},
		{
			name: "magex off", id: CheckTool, state: CheckBlock, text: "mage-x is turned off in Settings",
			fixes: []Fix{FixOpenSettings}, change: func(f *Facts) { f.Tool.Off = true },
		},
		{
			name: "magex off and too old", id: CheckTool, state: CheckBlock, text: "mage-x is turned off in Settings",
			fixes: []Fix{FixOpenSettings}, change: func(f *Facts) {
				f.Tool.Off, f.Tool.State, f.Tool.Version = true, integrations.StateTooOld, "1.27.0"
			},
		},
		{
			name: "magex won't run", id: CheckTool, state: CheckBlock, text: "magex isn't available",
			fixes: []Fix{FixOpenSettings}, change: func(f *Facts) { f.Tool.State = integrations.StateWontRun },
		},
		{
			name: "magex too old", id: CheckTool, state: CheckBlock,
			text: "magex 1.27.0 is older than 1.29.5, which Release needs", fixes: []Fix{FixUpdate},
			change: func(f *Facts) {
				f.Tool.State, f.Tool.Version, f.Tool.Features = integrations.StateTooOld, "1.27.0", nil
			},
		},
		{
			name: "magex with an update", id: CheckTool, absent: true,
			change: func(f *Facts) { f.Tool.State = integrations.StateUpdate },
		},
		{
			name: "GitHub unread", id: CheckRepo, state: CheckBlock, text: "Couldn't read acme-org/sdk on GitHub.",
			fixes:  []Fix{FixCheckAgain},
			change: func(f *Facts) { f.Repo = RepoFacts{Name: f.Repo.Name, Error: "Couldn't read acme-org/sdk on GitHub."} },
		},
		{
			name: "not on GitHub", id: CheckRepo, state: CheckBlock, text: "GitHub doesn't show acme-org/sdk to you.",
			change: func(f *Facts) { f.Repo = RepoFacts{Name: f.Repo.Name} },
		},
		{
			name: "archived", id: CheckRepo, state: CheckBlock,
			text:   "acme-org/sdk is archived, so GitHub takes no new tags in it.",
			change: func(f *Facts) { f.Repo.Archived = true },
		},
		{
			name: "no commits", id: CheckRepo, state: CheckBlock, text: "acme-org/sdk has no commits on GitHub yet.",
			change: func(f *Facts) { f.Repo.DefaultBranch, f.Repo.HeadOID = "", "" },
		},
		{
			name: "read access", id: CheckAccess, state: CheckBlock,
			text:   "You have read access to acme-org/sdk. Pushing a tag needs write access.",
			change: func(f *Facts) { f.Repo.Permission = "READ" },
		},
		{
			name: "triage access", id: CheckAccess, state: CheckBlock,
			text:   "You have triage access to acme-org/sdk. Pushing a tag needs write access.",
			change: func(f *Facts) { f.Repo.Permission = "TRIAGE" },
		},
		{
			name: "no access", id: CheckAccess, state: CheckBlock,
			text:   "You have no access to acme-org/sdk. Pushing a tag needs write access.",
			change: func(f *Facts) { f.Repo.Permission = "NONE" },
		},
		{
			name: "access unknown", id: CheckAccess, absent: true, change: func(f *Facts) { f.Repo.Permission = "" },
		},
	}
}

// originCases are the clone's origin, and git failing to read it.
func originCases() []decideCase {
	return []decideCase{
		{
			name: "another origin", id: CheckOrigin, state: CheckBlock, text: "acme-org/sdk's clone has a different origin",
			change: func(f *Facts) { f.Clone.OriginMatches = false },
		},
		{
			name: "unreadable clone", id: CheckOrigin, state: CheckBlock,
			text: "Couldn't read this clone with git: not a git repository.", fixes: []Fix{FixCheckAgain},
			change: func(f *Facts) { f.Clone.Error = "not a git repository" },
		},
	}
}

// cloneCases are the clone's own state: in progress, dirty, detached, and its hooks.
func cloneCases() []decideCase {
	return slices.Concat(inProgressCases(), []decideCase{
		{
			name: "dirty", id: CheckDirty, state: CheckBlock,
			text: "3 uncommitted changes: go.mod, main.go, notes.txt. magex won't tag with uncommitted work. " +
				"Commit or stash them.",
			fixes:  []Fix{FixOpenTerminal, FixCheckAgain},
			change: func(f *Facts) { f.Clone.Changes = []string{"go.mod", "main.go", "notes.txt"} },
		},
		{
			name: "one change", id: CheckDirty, state: CheckBlock,
			text:   "1 uncommitted change: go.mod. magex won't tag with uncommitted work. Commit or stash it.",
			fixes:  []Fix{FixOpenTerminal, FixCheckAgain},
			change: func(f *Facts) { f.Clone.Changes = []string{"go.mod"} },
		},
		{
			name: "many changes", id: CheckDirty, state: CheckBlock,
			text: "7 uncommitted changes: a.go, b.go, c.go, d.go, e.go, and 2 more. magex won't tag with " +
				"uncommitted work. Commit or stash them.",
			fixes: []Fix{FixOpenTerminal, FixCheckAgain},
			change: func(f *Facts) {
				f.Clone.Changes = []string{"a.go", "b.go", "c.go", "d.go", "e.go", "f.go", "g.go"}
			},
		},
		{
			name: "detached", id: CheckDetached, state: CheckBlock,
			text:   "Your clone is on a detached commit. Switch to a branch first.",
			change: func(f *Facts) { f.Clone.Branch, f.Clone.Detached = "", true },
		},
		{
			name: "pre-push hook", id: CheckHooks, state: CheckInfo,
			text:   "This clone has a pre-push hook. It runs when magex pushes, inside magex's 30 seconds.",
			change: func(f *Facts) { f.Clone.Hooks = []string{hookPrePush} },
		},
		{
			name: "post-checkout hook", id: CheckHooks, state: CheckInfo,
			text: "This clone has a post-checkout hook. It runs when magex switches branches, inside magex's 30 seconds.",
			change: func(f *Facts) {
				f.Clone.Hooks, f.Clone.Branch = []string{hookPostCheckout}, "feature-x"
			},
		},
		{
			name: "both hooks", id: CheckHooks, state: CheckInfo,
			text: "This clone has pre-push and post-checkout hooks. They run when magex switches branches and " +
				"pushes, inside magex's 30 seconds.",
			change: func(f *Facts) {
				f.Clone.Hooks, f.Clone.Branch = []string{hookPrePush, hookPostCheckout}, "feature-x"
			},
		},
		{
			name: "post-checkout hook, no switch", id: CheckHooks, absent: true,
			change: func(f *Facts) { f.Clone.Hooks = []string{hookPostCheckout} },
		},
	})
}

// inProgressCases are git in the middle of something.
func inProgressCases() []decideCase {
	ops := map[gitread.Operation]string{
		gitread.OperationRebasing: "A rebase", gitread.OperationMerging: "A merge",
		gitread.OperationCherryPicking: "A cherry-pick", gitread.OperationReverting: "A revert",
		gitread.Operation("bisecting"): "A git operation",
	}

	cases := make([]decideCase, 0, len(ops))

	for op, what := range ops {
		cases = append(cases, decideCase{
			name: string(op), id: CheckInProgress, state: CheckBlock,
			text:   what + " is in progress in this clone. Finish or abort it in your terminal.",
			change: func(f *Facts) { f.Clone.InProgress = op },
		})
	}

	return cases
}

// branchCases are where the clone is, the release's branch, and how it stands with
// origin's.
func branchCases() []decideCase {
	return slices.Concat(whereCases(), unpushedCases(), []decideCase{
		{
			name: "behind", id: CheckBehind, state: CheckOK,
			text:   "origin/master has commits your clone doesn't. magex pulls them first.",
			change: func(f *Facts) { f.Clone.Behind = true },
		},
		{
			name: "no upstream", id: CheckUpstream, state: CheckBlock,
			text: "master doesn't track origin/master, so magex can't pull it.",
			cmds: []string{"git branch --set-upstream-to=origin/master master"},
			change: func(f *Facts) {
				f.Clone.Upstream = gitread.Upstream{}
				f.Tool.Features = []integrations.Feature{integrations.FeatureExpect}
			},
		},
		{
			name: "upstream elsewhere", id: CheckUpstream, state: CheckBlock,
			text: "master doesn't track origin/master, so magex can't pull it.",
			cmds: []string{"git branch --set-upstream-to=origin/master master"},
			change: func(f *Facts) {
				f.Clone.Upstream = gitread.Upstream{Remote: "origin", Ref: "refs/heads/main"}
				f.Tool.Features = nil
			},
		},
		{
			name: "no upstream, but fast-forward only", id: CheckUpstream, absent: true,
			change: func(f *Facts) { f.Clone.Upstream = gitread.Upstream{} },
		},
		{
			name: "a branch Release doesn't offer", id: CheckBranch, state: CheckBlock,
			text:   "develop isn't a branch Release offers for acme-org/sdk.",
			change: func(f *Facts) { f.Choice.Branch = "develop" },
		},
		{
			name: "nowhere", id: CheckBranch, state: CheckBlock, text: "master doesn't exist in this clone or on origin.",
			change: func(f *Facts) {
				f.Clone.LocalHead, f.Clone.TrackingHead, f.Clone.RemoteHeads = "", "", map[string]string{}
			},
		},
		{
			name: "not fetched", id: CheckBranch, state: CheckBlock,
			text:   "This clone doesn't have master yet. Fetch origin in your terminal, then check again.",
			fixes:  []Fix{FixOpenTerminal, FixCheckAgain},
			change: func(f *Facts) { f.Clone.LocalHead, f.Clone.TrackingHead = "", "" },
		},
		{
			name: "only origin's", id: CheckBranch, absent: true,
			change: func(f *Facts) { f.Clone.LocalHead, f.Clone.Upstream = "", gitread.Upstream{} },
		},
	})
}

// whereCases are the branch the clone is on.
func whereCases() []decideCase {
	return []decideCase{
		{
			name: "on another branch", id: CheckWhere, state: CheckOK,
			text:   "You're on feature-x. magex switches to master, then back.",
			change: func(f *Facts) { f.Clone.Branch = "feature-x" },
		},
		{
			name: "checked out elsewhere", id: CheckWhere, state: CheckBlock,
			text: "master is checked out in another worktree at ~/projects/sdk-worktrees/fix-retries, so git can't " +
				"switch to it here.",
			change: func(f *Facts) {
				f.Clone.Branch, f.Clone.Elsewhere = "feature-x", []string{"~/projects/sdk-worktrees/fix-retries"}
			},
		},
		{
			name: "detached has no where", id: CheckWhere, absent: true,
			change: func(f *Facts) {
				f.Clone.Branch, f.Clone.Detached = "", false // as git couldn't say; Detached blocks on its own
			},
		},
	}
}

// unpushedCases are commits on the branch that origin's lacks.
func unpushedCases() []decideCase {
	const fix = "Push them, or reset master to origin, in your terminal."

	noFastForward := func(f *Facts) { f.Tool.Features = []integrations.Feature{integrations.FeatureExpect} }

	return []decideCase{
		{
			name: "unpushed", id: CheckUnpushed, state: CheckBlock,
			text: "master has 2 commits that aren't on origin. magex would rebase them and tag a commit GitHub " +
				"doesn't have. " + fix,
			change: func(f *Facts) { noFastForward(f); f.Clone.Unpushed = 2 },
		},
		{
			name: "one unpushed", id: CheckUnpushed, state: CheckBlock,
			text: "master has 1 commit that isn't on origin. magex would rebase it and tag a commit GitHub doesn't " +
				"have. Push it, or reset master to origin, in your terminal.",
			change: func(f *Facts) { noFastForward(f); f.Clone.Unpushed = 1 },
		},
		{
			name: "some unpushed", id: CheckUnpushed, state: CheckBlock,
			text: "master has commits that aren't on origin. magex would rebase them and tag a commit GitHub " +
				"doesn't have. " + fix,
			change: func(f *Facts) { noFastForward(f); f.Clone.Unpushed = UnpushedSome },
		},
		{
			name: "unpushed, fast-forward only", id: CheckUnpushed, state: CheckBlock,
			text:   "master has 2 commits that aren't on origin. magex can't fast-forward master, so it would tag nothing. " + fix,
			change: func(f *Facts) { f.Clone.Unpushed = 2 },
		},
		{
			name: "unpushed unknown", id: CheckUnpushed, state: CheckBlock,
			text: "Can't tell whether master has commits that aren't on origin: this clone has neither " +
				"origin/master nor origin's newest commit. Fetch origin in your terminal, then check again.",
			fixes:  []Fix{FixOpenTerminal, FixCheckAgain},
			change: func(f *Facts) { f.Clone.Unpushed = UnpushedUnknown },
		},
		{
			name: "unpushed on a branch magex makes", id: CheckUnpushed, absent: true,
			change: func(f *Facts) {
				f.Clone.LocalHead, f.Clone.Upstream, f.Clone.Unpushed = "", gitread.Upstream{}, UnpushedUnknown
			},
		},
	}
}

// signingCases are signed tags, in the app and in the terminal.
func signingCases() []decideCase {
	inTerminal := func(f *Facts) { f.Choice.RunsIn, f.Choice.Terminal = integrationprefs.RunsInTerminal, "iTerm2" }

	return []decideCase{
		{
			name: "ssh agent", id: CheckSigning, state: CheckOK,
			text:   "Your git signs tags with ssh, and your ssh agent has the key",
			change: func(f *Facts) { f.Clone.Signing = SigningFacts{Format: SignSSH, AgentHasKey: true} },
		},
		{
			name: "ssh key file", id: CheckSigning, state: CheckOK,
			text:   "Your git signs tags with ssh, from a key file that needs no passphrase",
			change: func(f *Facts) { f.Clone.Signing = SigningFacts{Format: SignSSH, OpenKey: true} },
		},
		{
			name: "ssh needs a passphrase", id: CheckSigning, state: CheckWarn,
			text: "Your git signs tags with ssh, but your ssh agent doesn't have that key, so signing will fail " +
				"here. Run ssh-add, or release in your terminal.",
			cmds:   []string{"ssh-add"},
			change: func(f *Facts) { f.Clone.Signing = SigningFacts{Format: SignSSH} },
		},
		{
			name: "ssh needs a passphrase, in the terminal", id: CheckSigning, state: CheckInfo,
			text: "Your git signs tags with ssh, and your ssh agent doesn't have that key, so your terminal will " +
				"ask for its passphrase",
			change: func(f *Facts) { inTerminal(f); f.Clone.Signing = SigningFacts{Format: SignSSH} },
		},
		{
			name: "gpg in a window", id: CheckSigning, state: CheckOK,
			text:   "Your git signs tags with gpg, through a pinentry that can ask in a window",
			change: func(f *Facts) { f.Clone.Signing = SigningFacts{Format: SignGPG, WindowPinentry: true} },
		},
		{
			name: "gpg in a terminal", id: CheckSigning, state: CheckWarn,
			text:   "Your git signs tags with gpg, which asks for its passphrase in a terminal. Release in your terminal.",
			change: func(f *Facts) { f.Clone.Signing = SigningFacts{Format: SignGPG} },
		},
		{
			name: "gpg, in the terminal", id: CheckSigning, state: CheckInfo,
			text:   "Your git signs tags with gpg, which asks for its passphrase in your terminal",
			change: func(f *Facts) { inTerminal(f); f.Clone.Signing = SigningFacts{Format: SignGPG} },
		},
		{
			name: "a format unknown", id: CheckSigning, absent: true,
			change: func(f *Facts) { f.Clone.Signing = SigningFacts{Format: "other"} },
		},
		{
			name: "x509", id: CheckSigning, absent: true,
			change: func(f *Facts) { f.Clone.Signing = SigningFacts{Format: SignX509} },
		},
	}
}

// otherCases are origin, versions, GitHub's checks, the lock, and the shell.
func otherCases() []decideCase {
	return []decideCase{
		{
			name: "unreachable", id: CheckReach, state: CheckBlock,
			text:  "Can't reach origin with your git credentials: Permission denied (publickey).",
			fixes: []Fix{FixOpenTerminal, FixCheckAgain},
			change: func(f *Facts) {
				f.Clone.Reached, f.Clone.ReachError = false, "Permission denied (publickey)."
				f.Clone.RemoteHeads, f.Clone.RemoteTags = map[string]string{}, map[string]gitread.RemoteTag{}
			},
		},
		{
			name: "unreachable, git silent", id: CheckReach, state: CheckBlock,
			text: "Can't reach origin with your git credentials.", fixes: []Fix{FixOpenTerminal, FixCheckAgain},
			change: func(f *Facts) { f.Clone.Reached = false },
		},
		{
			name: "only on this Mac", id: CheckLocalOnly, state: CheckBlock,
			text: "v1.4.3 is tagged on this Mac but isn't on GitHub. magex would skip it and leave a gap. Push it " +
				"or delete it in your terminal:",
			fixes:  []Fix{FixOpenTerminal},
			cmds:   []string{"git push origin v1.4.3", "git tag -d v1.4.3"},
			change: func(f *Facts) { f.Clone.LocalTags = []string{shownVersion, "v1.4.3", "v1.3.9"} },
		},
		{
			name: "two only on this Mac", id: CheckLocalOnly, state: CheckBlock,
			text: "v1.4.3 and v1.5.0 are tagged on this Mac but aren't on GitHub. magex would skip them and leave " +
				"a gap. Push them or delete them in your terminal:",
			fixes: []Fix{FixOpenTerminal},
			cmds: []string{
				"git push origin v1.4.3", "git push origin v1.5.0", "git tag -d v1.4.3", "git tag -d v1.5.0",
			},
			change: func(f *Facts) { f.Clone.LocalTags = []string{shownVersion, "v1.4.3", "v1.5.0"} },
		},
		{
			name: "nothing new", id: CheckNothingNew, state: CheckBlock,
			text: "Nothing new: v1.4.2 is already on the newest commit of master.",
			change: func(f *Facts) {
				f.Clone.RemoteTags[shownVersion] = gitread.RemoteTag{Object: olderOID, Commit: newestOID}
				f.Clone.RemoteTags["release-notes"] = gitread.RemoteTag{Commit: newestOID}
			},
		},
		{
			name: "checks failing", id: CheckChecks, state: CheckWarn, text: "Checks are failing on a1b2c3d",
			fixes: []Fix{FixView}, change: func(f *Facts) { f.Repo.Checks = ChecksFailure },
		},
		{
			name: "checks erred", id: CheckChecks, state: CheckWarn, text: "Checks are failing on a1b2c3d",
			fixes: []Fix{FixView}, change: func(f *Facts) { f.Repo.Checks = ChecksError },
		},
		{
			name: "checks running", id: CheckChecks, state: CheckWarn, text: "Checks are still running on a1b2c3d",
			change: func(f *Facts) { f.Repo.Checks = ChecksPending },
		},
		{
			name: "checks expected", id: CheckChecks, state: CheckWarn, text: "Checks are still running on a1b2c3d",
			change: func(f *Facts) { f.Repo.Checks = ChecksExpected },
		},
		{name: "no checks", id: CheckChecks, absent: true, change: func(f *Facts) { f.Repo.Checks = ChecksNone }},
		{name: "checks unknown", id: CheckChecks, absent: true, change: func(f *Facts) { f.Repo.Checks = "NEW" }},
		{
			name: "busy", id: CheckBusy, state: CheckBlock, text: "A checks run is using acme-org/sdk right now.",
			fixes: []Fix{FixWatch},
			change: func(f *Facts) {
				f.Clone.Busy = true
				f.Clone.Holder = clonelock.Holder{Kind: clonelock.KindChecks, Repo: "acme-org/sdk", RunID: 7}
			},
		},
		{
			name: "busy with a worktree", id: CheckBusy, state: CheckBlock,
			text: "A worktree change is using this clone right now.",
			change: func(f *Facts) {
				f.Clone.Busy, f.Clone.Holder = true, clonelock.Holder{Kind: clonelock.KindWorktree}
			},
		},
		{
			name: "shell unread", id: CheckShell, state: CheckWarn,
			text: brand.Name + " couldn't read your shell's environment, so your ssh agent (or your password " +
				"manager's) may be missing. If the push fails, release in your terminal.",
			fixes: []Fix{FixCheckAgain}, change: func(f *Facts) { f.Clone.ShellRead = false },
		},
		{
			name: "shell unread, in the terminal", id: CheckShell, absent: true,
			change: func(f *Facts) {
				f.Clone.ShellRead, f.Choice.RunsIn = false, integrationprefs.RunsInTerminal
			},
		},
	}
}

func TestDecideCheckDetails(t *testing.T) {
	t.Parallel()

	t.Run("a failing check links its commit's checks", func(t *testing.T) {
		t.Parallel()

		f := readyFacts()
		f.Repo.Checks = ChecksFailure

		got, ok := checkOf(Decide(f), CheckChecks)
		require.True(t, ok)
		assert.Equal(t, "https://github.com/acme-org/sdk/commit/"+newestOID+"/checks", got.Link)
	})

	t.Run("the busy run can be watched", func(t *testing.T) {
		t.Parallel()

		f := readyFacts()
		f.Clone.Busy, f.Clone.Holder = true, clonelock.Holder{Kind: clonelock.KindRelease, RunID: 42}

		got, ok := checkOf(Decide(f), CheckBusy)
		require.True(t, ok)
		assert.Equal(t, int64(42), got.RunID)
	})

	t.Run("a branch other than the default has no checks line", func(t *testing.T) {
		t.Parallel()

		f := readyFacts()
		f.Repo.Branches, f.Choice.Branch = []string{branchMain, branchMaster}, branchMain
		f.Clone.Branch, f.Clone.RemoteHeads[branchMain] = branchMain, newestOID
		f.Clone.Upstream.Ref = "refs/heads/main"

		o := Decide(f)
		_, ok := checkOf(o, CheckChecks)
		assert.False(t, ok)
		assert.True(t, o.Ready)
	})

	t.Run("an unreadable clone shows nothing it would have read", func(t *testing.T) {
		t.Parallel()

		f := readyFacts()
		f.Clone.Error = "not a git repository"
		f.Clone.Changes, f.Clone.Detached = []string{"x"}, true
		f.Clone.Signing = SigningFacts{Format: SignGPG}

		checks := Decide(f).Checks
		ids := make([]CheckID, 0, len(checks))

		for _, c := range checks {
			ids = append(ids, c.ID)
		}

		assert.Equal(t, []CheckID{CheckOrigin, CheckReach, CheckChecks}, ids)
	})

	t.Run("an unreachable origin hides what needs its tags", func(t *testing.T) {
		t.Parallel()

		f := readyFacts()
		f.Clone.Reached, f.Clone.RemoteTags = false, map[string]gitread.RemoteTag{}
		f.Clone.LocalTags = []string{shownVersion, "v9.9.9"}

		o := Decide(f)
		_, local := checkOf(o, CheckLocalOnly)
		assert.False(t, local)
		assert.Equal(t, newestOID, o.Head, "GitHub's newest commit stands in for origin's")
	})
}

func TestDecideBumps(t *testing.T) {
	t.Parallel()

	t.Run("each version is over every tag, the clone's and origin's", func(t *testing.T) {
		t.Parallel()

		f := readyFacts()
		f.Clone.LocalTags = []string{shownVersion, "notes"}
		f.Clone.RemoteTags["v1.4.5"] = gitread.RemoteTag{Commit: olderOID}
		f.Clone.RemoteTags["sdk/v3.0.0"] = gitread.RemoteTag{Commit: olderOID}

		o := Decide(f)
		assert.Equal(t, "v1.4.6", bumpOf(t, o, BumpPatch).Version)
		assert.Equal(t, "v1.5.0", bumpOf(t, o, BumpMinor).Version)
		assert.Equal(t, "v2.0.0", bumpOf(t, o, BumpMajor).Version)
		assert.Equal(t, "v1.4.6", o.Version)
	})

	t.Run("no tags start from v0.0.0", func(t *testing.T) {
		t.Parallel()

		f := readyFacts()
		f.Clone.LocalTags, f.Clone.RemoteTags = []string{}, map[string]gitread.RemoteTag{}

		o := Decide(f)
		assert.Equal(t, "v0.0.1", bumpOf(t, o, BumpPatch).Version)
		assert.Equal(t, "v0.1.0", bumpOf(t, o, BumpMinor).Version)
		assert.Equal(t, "v1.0.0", bumpOf(t, o, BumpMajor).Version)
		assert.True(t, o.Major.FirstStable)
	})

	t.Run("nothing new offers no card", func(t *testing.T) {
		t.Parallel()

		f := readyFacts()
		f.Clone.RemoteTags[shownVersion] = gitread.RemoteTag{Commit: newestOID}

		o := Decide(f)
		for _, b := range o.Bumps {
			assert.False(t, b.Offered, "%s", b.Bump)
		}

		assert.False(t, o.Ready)
	})

	t.Run("every card is offered with a magex that has expect", func(t *testing.T) {
		t.Parallel()

		o := Decide(readyFacts())
		for _, b := range o.Bumps {
			assert.True(t, b.Offered, "%s", b.Bump)
			assert.Empty(t, b.Note)
			assert.Equal(t, []Fix{}, b.Fixes)
		}
	})

	t.Run("major needs a magex with expect", func(t *testing.T) {
		t.Parallel()

		f := readyFacts()
		f.Tool.Version, f.Tool.Features = "1.29.5", []integrations.Feature{}

		major := bumpOf(t, Decide(f), BumpMajor)
		assert.False(t, major.Offered)
		assert.Equal(t, "Needs magex 1.30.0 or later", major.Note)
		assert.Equal(t, []Fix{FixUpdate}, major.Fixes)
		assert.True(t, bumpOf(t, Decide(f), BumpPatch).Offered)
	})

	t.Run("major isn't available while no magex has expect", func(t *testing.T) {
		t.Parallel()

		f := readyFacts()
		f.Tool.Features, f.Tool.ExpectSince = []integrations.Feature{}, ""

		major := bumpOf(t, Decide(f), BumpMajor)
		assert.False(t, major.Offered)
		assert.Equal(t, "Not available yet: magex can't be told which version to make.", major.Note)
		assert.Equal(t, []Fix{}, major.Fixes)
	})
}

func TestDecideMajor(t *testing.T) {
	t.Parallel()

	major := func(change func(f *Facts)) Offer {
		f := readyFacts()
		f.Choice.Bump = BumpMajor
		change(&f)

		return Decide(f)
	}

	t.Run("needs the version typed", func(t *testing.T) {
		t.Parallel()

		o := major(func(*Facts) {})
		assert.False(t, o.Ready)
		assert.False(t, o.Major.Confirmed)
		assert.NotContains(t, o.Argv, "major-confirm")

		o = major(func(f *Facts) { f.Choice.Typed = "v2.0" })
		assert.False(t, o.Ready, "only the exact version")

		o = major(func(f *Facts) { f.Choice.Typed = " v2.0.0 " })
		assert.True(t, o.Ready)
		assert.True(t, o.Major.Confirmed)
		assert.Equal(t, "Release v2.0.0 (major)", o.Button)
		assert.Equal(t, []string{
			magexPath, "version:bump", "bump=major", "branch=master", "push=true", "expect=v2.0.0", "json=true",
			"ff-only", "major-confirm",
		}, o.Argv)
	})

	t.Run("a Go module needs its path, or the box ticked", func(t *testing.T) {
		t.Parallel()

		o := major(func(f *Facts) { f.Choice.Typed, f.Clone.ModulePath = "v2.0.0", "github.com/acme-org/sdk" })
		assert.False(t, o.Ready)
		assert.Equal(t, "github.com/acme-org/sdk/v2", o.Major.ModuleWant)
		assert.Equal(t, "go.mod says module github.com/acme-org/sdk. Go can't install v2.0.0 until it says "+
			"github.com/acme-org/sdk/v2.", o.Major.ModuleText)
		assert.NotContains(t, o.Argv, "major-confirm")

		o = major(func(f *Facts) {
			f.Choice.Typed, f.Clone.ModulePath, f.Choice.AllowModulePath = "v2.0.0", "github.com/acme-org/sdk", true
		})
		assert.True(t, o.Ready)
		assert.Contains(t, o.Argv, "major-confirm")

		o = major(func(f *Facts) { f.Choice.Typed, f.Clone.ModulePath = "v2.0.0", "github.com/acme-org/sdk/v2" })
		assert.True(t, o.Ready, "go.mod already says /v2")
		assert.Empty(t, o.Major.ModuleText)
	})

	t.Run("the first stable release skips the go.mod check", func(t *testing.T) {
		t.Parallel()

		o := major(func(f *Facts) {
			f.Clone.LocalTags = []string{"v0.3.1"}
			f.Clone.RemoteTags = map[string]gitread.RemoteTag{"v0.3.1": {Commit: olderOID}}
			f.Choice.Typed, f.Clone.ModulePath = "v1.0.0", "github.com/acme-org/sdk"
		})
		assert.True(t, o.Major.FirstStable)
		assert.Equal(t, "v1.0.0", o.Major.Version)
		assert.Empty(t, o.Major.ModuleWant)
		assert.True(t, o.Ready)
	})

	t.Run("a magex without expect can't start one", func(t *testing.T) {
		t.Parallel()

		o := major(func(f *Facts) { f.Choice.Typed, f.Tool.Features = "v2.0.0", []integrations.Feature{} })
		assert.False(t, o.Ready)
		assert.Equal(t, []string{magexPath, "version:bump", "bump=major", "branch=master", "push=true"}, o.Argv)
	})
}

func TestDecideArgv(t *testing.T) {
	t.Parallel()

	base := []string{magexPath, "version:bump", "bump=patch", "branch=master", "push=true"}

	tests := []struct {
		name     string
		features []integrations.Feature
		want     []string
	}{
		{name: "1.29.5", features: []integrations.Feature{}, want: base},
		{
			name: "1.30.0", features: []integrations.Feature{integrations.FeaturePushFails, integrations.FeatureExpect},
			want: append(slices.Clone(base), "expect=v1.4.3"),
		},
		{
			name: "1.30.1",
			features: []integrations.Feature{
				integrations.FeaturePushFails, integrations.FeatureExpect, integrations.FeatureBumpJSON,
			},
			want: append(slices.Clone(base), "expect=v1.4.3", "json=true"),
		},
		{name: "1.30.2", features: allFeatures(), want: append(slices.Clone(base), "expect=v1.4.3", "json=true", "ff-only")},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			f := readyFacts()
			f.Tool.Version, f.Tool.Features = tc.name, tc.features

			o := Decide(f)
			assert.Equal(t, tc.want, o.Argv)
			assert.NotContains(t, o.Argv, "push", "push=true, never the bare word")
			assert.True(t, o.Ready)
		})
	}

	t.Run("minor on another branch", func(t *testing.T) {
		t.Parallel()

		f := readyFacts()
		f.Repo.Branches, f.Clone.RemoteHeads[branchMain] = []string{branchMain, branchMaster}, newestOID
		f.Choice.Bump, f.Choice.Branch = BumpMinor, branchMain

		assert.Equal(t, []string{
			magexPath, "version:bump", "bump=minor", "branch=main", "push=true", "expect=v1.5.0", "json=true", "ff-only",
		}, Decide(f).Argv)
	})

	t.Run("nothing to run without magex", func(t *testing.T) {
		t.Parallel()

		f := readyFacts()
		f.Tool = ToolFacts{State: integrations.StateMissing}

		o := Decide(f)
		assert.Equal(t, []string{}, o.Argv)
		assert.False(t, o.Ready)
	})

	t.Run("nothing to run for a bump that isn't one", func(t *testing.T) {
		t.Parallel()

		f := readyFacts()
		f.Choice.Bump = "huge"

		o := Decide(f)
		assert.Equal(t, []string{}, o.Argv)
		assert.False(t, o.Ready)
		assert.Empty(t, o.Version)
		assert.Empty(t, o.Button)
		assert.Empty(t, o.Sentence)
	})
}

func TestDecideSentence(t *testing.T) {
	t.Parallel()

	inApp := " It runs here in " + brand.Name + "."

	tests := []struct {
		name   string
		change func(f *Facts)
		want   string
		button string
	}{
		{
			name: "on the branch", change: func(*Facts) {}, button: "Release v1.4.3",
			want: "magex pulls master, tags the newest commit as v1.4.3, and pushes the tag to origin." + inApp,
		},
		{
			name: "on another branch", change: func(f *Facts) { f.Clone.Branch = "feature-x" }, button: "Release v1.4.3",
			want: "magex switches to master, pulls, tags the newest commit as v1.4.3, pushes the tag to origin, and " +
				"switches back to feature-x." + inApp,
		},
		{
			name: "detached", change: func(f *Facts) { f.Clone.Branch, f.Clone.Detached = "", true },
			button: "Release v1.4.3",
			want:   "magex switches to master, pulls, tags the newest commit as v1.4.3, and pushes the tag to origin." + inApp,
		},
		{
			name: "in iTerm2", button: "Release v1.4.3 in iTerm2",
			change: func(f *Facts) {
				f.Choice.RunsIn, f.Choice.Terminal = integrationprefs.RunsInTerminal, "iTerm2"
			},
			want: "magex pulls master, tags the newest commit as v1.4.3, and pushes the tag to origin. It runs in iTerm2.",
		},
		{
			name: "in a terminal without a name", button: "Release v1.4.5 anyway in your terminal",
			change: func(f *Facts) {
				f.Choice.RunsIn, f.Choice.Bump, f.Repo.Checks = integrationprefs.RunsInTerminal, BumpPatch, ChecksPending
				f.Clone.RemoteTags["v1.4.4"] = gitread.RemoteTag{Commit: olderOID}
			},
			want: "magex pulls master, tags the newest commit as v1.4.5, and pushes the tag to origin. It runs in " +
				"your terminal.",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			f := readyFacts()
			tc.change(&f)

			o := Decide(f)
			assert.Equal(t, tc.want, o.Sentence)
			assert.Equal(t, tc.button, o.Button)
		})
	}
}

func TestDecideBranches(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		def     string
		have    []string
		want    []BranchChoice
		choose  string
		note    string
		blocked bool
	}{
		{
			name: "master, and main on origin", def: branchMaster, have: []string{branchMain, branchMaster},
			want: []BranchChoice{{Name: branchMaster, Default: true}, {Name: branchMain}},
		},
		{
			name: "main, and master on origin", def: branchMain, have: []string{branchMaster},
			want: []BranchChoice{{Name: branchMain, Default: true}, {Name: branchMaster}},
		},
		{
			name: "another default", def: "develop", have: []string{branchMain},
			want: []BranchChoice{{Name: "develop", Default: true}, {Name: branchMain}},
		},
		{
			name: "only the default", def: branchMaster, have: []string{},
			want: []BranchChoice{{Name: branchMaster, Default: true}},
		},
		{
			name: "choosing the other", def: branchMain, have: []string{branchMain, branchMaster}, choose: branchMaster,
			want: []BranchChoice{{Name: branchMain, Default: true}, {Name: branchMaster}},
			note: "master isn't the default branch on GitHub (main is).",
		},
		{
			name: "choosing one origin lacks", def: branchMaster, have: []string{branchMaster}, choose: branchMain,
			want: []BranchChoice{{Name: branchMaster, Default: true}}, blocked: true,
			note: "main isn't the default branch on GitHub (master is).",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			f := readyFacts()
			f.Repo.DefaultBranch, f.Repo.Branches, f.Choice.Branch = tc.def, tc.have, tc.choose

			o := Decide(f)
			assert.Equal(t, tc.want, o.Branches)
			assert.Equal(t, tc.note, o.BranchNote)

			_, blocked := checkOf(o, CheckBranch)
			assert.Equal(t, tc.blocked, blocked)
		})
	}

	t.Run("none before GitHub answers", func(t *testing.T) {
		t.Parallel()

		f := readyFacts()
		f.Repo = RepoFacts{Name: f.Repo.Name, Error: "Couldn't read acme-org/sdk on GitHub."}

		o := Decide(f)
		assert.Equal(t, []BranchChoice{}, o.Branches)
		assert.Empty(t, o.Branch)
		assert.Empty(t, o.Sentence)
		assert.False(t, o.Ready)
	})
}

func TestDecideNeverHoldsNull(t *testing.T) {
	t.Parallel()

	for _, f := range []Facts{{}, readyFacts()} {
		data, err := json.Marshal(Decide(f))
		require.NoError(t, err)
		assert.NotContains(t, string(data), "null", "the window gets [], never null")
	}
}

func TestModuleFor(t *testing.T) {
	t.Parallel()

	tests := []struct {
		path  string
		major int
		want  string
	}{
		{path: "github.com/acme-org/sdk", major: 2, want: "github.com/acme-org/sdk/v2"},
		{path: "github.com/acme-org/sdk/v2", major: 2, want: "github.com/acme-org/sdk/v2"},
		{path: "github.com/acme-org/sdk/v2", major: 3, want: "github.com/acme-org/sdk/v3"},
		{path: "example.com/v2x", major: 2, want: "example.com/v2x/v2"},
		{path: "example.com/sdk/v", major: 2, want: "example.com/sdk/v/v2"},
		{path: "gopkg.in/yaml.v3", major: 4, want: "gopkg.in/yaml.v4"},
		{path: "gopkg.in/check.v1", major: 2, want: "gopkg.in/check.v2"},
	}

	for _, tc := range tests {
		assert.Equal(t, tc.want, moduleFor(tc.path, tc.major), "%s to v%d", tc.path, tc.major)
	}
}
