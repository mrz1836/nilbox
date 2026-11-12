package release

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/brand"
	"github.com/mrz1836/nilbox/internal/gitread"
	"github.com/mrz1836/nilbox/internal/scriptrun"
)

// magex 1.30.2's texts the recordings share: the line it continues an unsent tag's error
// on (1.30.0), and its warning that origin can't be reached, which comes before the push.
const (
	keptHere = "\ntag v1.4.3 was left in the local repository: push it with 'git push origin v1.4.3' " +
		"or delete it with 'git tag -d v1.4.3'"
	unreachableWarning = "Git remote validation failed: remote 'origin' exists but may not be accessible: " +
		"command failed [git ls-remote --exit-code origin HEAD]: exit status "
	unreachableError = "failed to push tag: remote 'origin' exists but may not be accessible: " +
		"command failed [git ls-remote --exit-code origin HEAD]: exit status "
)

// recorded1302Outputs is what each magex 1.30.2 recording should read as, leaving out the
// JSON result, which TestMagex1302ResultsAgreeWithGit checks. Most read as magex 1.29.5's
// run of the same name did, updating the branch fast-forward only; the rest are the
// changes 1.30's expect=, ff-only, and failed pushes made, and the runs only 1.30 has.
func recorded1302Outputs() map[string]Output {
	was := recordedOutputs()

	// ff is a run that updated the branch fast-forward only.
	ff := func(o Output) Output {
		o = o.clone()
		o.FastForward = true

		return o
	}

	// kept is a run whose tag magex left on this Mac, which magex 1.30 says on the line
	// after its error.
	kept := func(o Output) Output {
		o = ff(o)
		o.Errors[len(o.Errors)-1] += keptHere

		return o
	}

	// refused is a run ff-only stopped before it tagged, as magex said.
	refused := func(err string) Output {
		return Output{
			Step: StepOnBranch, Working: StepUpdated, Branch: recBranch, Bump: recBump, FastForward: true,
			Errors: []string{err},
		}
	}

	pushWhenUnreachable := func(o Output, status string) Output {
		o = o.clone()
		o.Errors = []string{unreachableError + status + keptHere}

		return o
	}

	ffAhead := refused("ff-only: branch 'master' has 1 commit that origin/master doesn't have, so it can't be " +
		"fast-forwarded; nothing was tagged\nto release it, push it first (git push origin master); to drop it, " +
		"reset the branch to origin in a terminal (git checkout master && git reset --hard origin/master)")
	ffAhead.BackOn = recFeature

	dryRun := was["dry-run"].clone()
	dryRun.Warnings = append(dryRun.Warnings, "expected v1.4.4, but the next patch is v1.4.3 (would fail in normal mode)")

	noUpstreamFromFeature := ff(released(recVersion, "Current version tag is not on HEAD - there are 2 commits since v1.4.2"))
	noUpstreamFromFeature.BackOn = recFeature

	unreachableBranch := ff(was["origin-unreachable-branch"])
	unreachableBranch.Errors = []string{
		"failed to fetch branch 'master' from origin: command failed " +
			"[git fetch --tags --force origin +refs/heads/master:refs/remotes/origin/master]: exit status 128",
	}

	outputs := map[string]Output{
		"dry-run":                     dryRun,
		"no-upstream":                 ff(released(recVersion, "Current version tag is not on HEAD - there are 2 commits since v1.4.2")),
		"no-upstream-from-feature":    noUpstreamFromFeature,
		"origin-unreachable-branch":   unreachableBranch,
		"origin-unreachable-nobranch": pushWhenUnreachable(was["origin-unreachable-nobranch"], "128"),
		"origin-without-head": pushWhenUnreachable(ff(Output{
			Step: StepTagCreated, Branch: recBranch, Bump: recBump, From: recFrom, Version: recVersion, Tag: recVersion,
			PushSkipped: true, PushSkipWarning: unreachableWarning + "2",
			Notes: scriptrun.Notes{Warnings: []string{notOnHead, unreachableWarning + "2"}},
		}), "2"),
		// magex 1.30.2 reads origin's annotated v1.4.3, at the clone's own HEAD, as at
		// another commit: its ls-remote leaves out the peeled ^{} line it compares.
		"remote-tag-same-commit": was["remote-tag-other-commit"],
		"sigint-during-pull-behind": {
			Step: StepOnBranch, Working: StepUpdated, Branch: recBranch, Bump: recBump, FastForward: true, Canceled: true,
			Errors: []string{
				"failed to fast-forward 'master' to origin/master: command failed " +
					"[git merge --ff-only refs/remotes/origin/master]: signal: interrupt",
			},
		},
		"unpushed-commits": refused("ff-only: branch 'master' has diverged from origin/master: it has 2 commits that " +
			"origin/master doesn't have, and origin/master has 1 commit it doesn't; nothing was tagged\nto release them, " +
			"rebase them onto origin/master and push them first (git checkout master && git pull --rebase origin master " +
			"&& git push origin master); to drop them, reset the branch to origin in a terminal " +
			"(git checkout master && git reset --hard origin/master)"),
		"ff-only-ahead": refused("ff-only: branch 'master' has 2 commits that origin/master doesn't have, so it can't be " +
			"fast-forwarded; nothing was tagged\nto release them, push them first (git push origin master); to drop them, " +
			"reset the branch to origin in a terminal (git checkout master && git reset --hard origin/master)"),
		"ff-only-ahead-from-feature": ffAhead,
		"version-moved": {
			Step: StepUpdated, Branch: recBranch, Bump: recBump, From: recVersion, Version: recNext, FastForward: true,
			Notes: scriptrun.Notes{Warnings: []string{"Current version tag is not on HEAD - there are 1 commits since v1.4.3"}},
			Errors: []string{
				"expected v1.4.3, but the next patch is v1.4.4: another version was tagged meanwhile; nothing was tagged",
			},
		},
	}

	for _, name := range []string{
		"detached-head", "local-only-tag", "major-confirm", "major-from-v0", "minor-on-master", "no-tags-patch",
		"no-tags-minor", "no-tags-major", "patch-feature-behind", "post-checkout-hook", "prerelease-minor",
		"signing-gpg-fails", "signing-ssh-missing-key", "signing-fails-from-feature", "tag-on-head",
	} {
		outputs[name] = ff(was[name])
	}

	// Each of these stopped before magex updated the branch, or it updated none.
	for _, name := range []string{
		"detached-no-branch", "dirty", "untracked-only", "major-no-confirm", "missing-branch", "other-worktree",
		"post-checkout-fails", "remote-tag-other-commit",
	} {
		outputs[name] = was[name]
	}

	for _, name := range []string{"pre-push-fails", "pre-push-fails-from-feature", "sigint-after-tag", "sigterm-after-tag"} {
		outputs[name] = kept(was[name])
	}

	return outputs
}

func TestOutputReadsEveryMagex1302Recording(t *testing.T) {
	t.Parallel()

	wants := recorded1302Outputs()
	names := recordingNamesIn(t, magex1302Recordings)
	require.Len(t, names, 39, "every recorded version:bump run is checked")

	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			want, ok := wants[name]
			require.True(t, ok, "no expected output for %s", name)

			rec := loadRecordingIn(t, magex1302Recordings, name)
			got := readOutput(rec.log("\n"))

			require.NotNil(t, got.Result, "every run was sent json=true")
			got.Result = nil
			assert.Equal(t, expected(want), got)
		})
	}

	for name := range wants {
		assert.Contains(t, names, name, "an expected output with no recording")
	}
}

// recordedBranch is a recorded branch as the JSON result names it: "" for a detached HEAD.
func recordedBranch(branch string) string {
	if branch == "(detached)" {
		return ""
	}

	return branch
}

func TestMagex1302ResultsAgreeWithGit(t *testing.T) {
	t.Parallel()

	// What magex 1.30.2 gets wrong in its JSON result, as git told the recorder: a signal
	// cuts its last read of the branch, so it says the clone ended on a detached HEAD; and
	// it reads origin's annotated tag at the clone's HEAD as another commit's, so it says
	// it sent nothing. The app reads both from git itself.
	cutBranch := map[string]bool{"sigint-after-tag": true, "sigterm-after-tag": true, "sigint-during-pull-behind": true}
	misreadPush := map[string]bool{"remote-tag-same-commit": true}

	for _, name := range recordingNamesIn(t, magex1302Recordings) {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			rec := loadRecordingIn(t, magex1302Recordings, name)
			got := readOutput(rec.log("\n"))
			require.NotNil(t, got.Result)

			r := *got.Result

			assert.Equal(t, rec.param("expect"), r.Expect)
			assert.Equal(t, rec.param("bump"), r.Bump)
			assert.Equal(t, got.DryRun, r.DryRun)
			assert.Equal(t, got.From, r.From, "the summary's From: line")
			assert.Equal(t, got.Version, r.To, "the summary's To: line")
			assert.Equal(t, got.Tag, r.Tag, "the tag magex said it made")
			assert.Equal(t, got.TagPushed, r.Pushed, "magex's lines and its result agree on the push")
			assert.Equal(t, lastOf(got.Errors), r.Error, "its last error, with the lines it goes on to")
			assert.Equal(t, recordedBranch(rec.Before.Branch), r.StartBranch)

			if cutBranch[name] {
				assert.Empty(t, r.Branch, "magex couldn't read the branch")
				assert.NotEqual(t, "(detached)", rec.After.Branch)
			} else {
				assert.Equal(t, recordedBranch(rec.After.Branch), r.Branch)
			}

			if r.Tag == "" {
				assert.Empty(t, r.Commit)

				return
			}

			local, ok := localTagCommit(rec.After, r.Tag)
			require.True(t, ok, "the clone has %s", r.Tag)
			assert.True(t, strings.HasPrefix(r.Commit, local), "%s tags %s, not %s", r.Tag, local, r.Commit)

			origin := remoteTagCommit(rec.After, r.Tag)
			assert.Equal(t, !misreadPush[name], r.Pushed == (origin == r.Commit), "origin has the tag at its commit")
		})
	}
}

// lastOf returns the last of list, or "".
func lastOf(list []string) string {
	if len(list) == 0 {
		return ""
	}

	return list[len(list)-1]
}

// localTagCommit returns the abbreviated commit the clone's tag points at, from the
// recorded "<name> <object> <commit>" lines; a lightweight tag has no third field and
// points at its object.
func localTagCommit(state cloneRecord, tag string) (string, bool) {
	for line := range strings.SplitSeq(state.LocalTags, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 || fields[0] != tag {
			continue
		}

		return fields[len(fields)-1], true
	}

	return "", false
}

// remoteTagCommit returns the full commit origin's tag points at, peeled, from the
// recorded ls-remote lines, or "".
func remoteTagCommit(state cloneRecord, tag string) string {
	direct := ""

	for line := range strings.SplitSeq(state.RemoteTags, "\n") {
		oid, ref, ok := strings.Cut(line, "\t")
		if !ok {
			continue
		}

		switch ref {
		case "refs/tags/" + tag + "^{}":
			return oid
		case "refs/tags/" + tag:
			direct = oid
		}
	}

	return direct
}

// errOriginSilent is origin not answering the recorder's ls-remote.
var errOriginSilent = errors.New("origin didn't answer")

// seenAfter is what verifying would have found after rec's run, from the clone's state
// the recorder read: origin's tag, or why origin couldn't say, the clone's tag, its
// branch, and what git is in the middle of there. The clone's abbreviated commit is
// written out in full when origin's commit or magex's result names it.
func seenAfter(rec magexRecording, said Output, tag string) seen {
	got := seen{
		tag: tag, branch: recordedBranch(rec.After.Branch), branchKnown: true,
		op: gitread.Operation(rec.After.InProgress),
	}

	if strings.HasPrefix(rec.After.RemoteTags, "fatal:") {
		got.err = fmt.Errorf("%w: %s", errOriginSilent, rec.After.RemoteTags)
	} else {
		got.commit = remoteTagCommit(rec.After, tag)
	}

	if local, ok := localTagCommit(rec.After, tag); ok {
		got.local = local

		for _, full := range []string{got.commit, resultCommit(said)} {
			if full != "" && strings.HasPrefix(full, local) {
				got.local = full
			}
		}
	}

	return got
}

// resultCommit is the commit magex's JSON result names, or "".
func resultCommit(said Output) string {
	if said.Result == nil {
		return ""
	}

	return said.Result.Commit
}

func TestMagex1302RunsVerifyAsTheyEnded(t *testing.T) {
	t.Parallel()

	type verdict struct {
		kind    ResultKind
		failure FailureKind // "" for none
	}

	released := verdict{kind: ResultReleased}
	failed := func(f FailureKind) verdict { return verdict{kind: ResultFailed, failure: f} }

	wants := map[string]verdict{
		"detached-head": released, "detached-no-branch": released, "local-only-tag": released,
		"major-confirm": released, "major-from-v0": released, "minor-on-master": released,
		"no-tags-patch": released, "no-tags-minor": released, "no-tags-major": released,
		"no-upstream": released, "no-upstream-from-feature": released, "patch-feature-behind": released,
		"post-checkout-hook": released, "prerelease-minor": released,

		// Origin has the version at the commit magex tagged, whatever magex said of it.
		"remote-tag-same-commit": released,

		// A tag magex kept because origin had no HEAD: the result quotes magex's warning,
		// and names no failure as well.
		"origin-without-head":         {kind: ResultNotPushed},
		"pre-push-fails":              {kind: ResultNotPushed, failure: FailureHook},
		"pre-push-fails-from-feature": {kind: ResultNotPushed, failure: FailureHook},
		"sigint-after-tag":            {kind: ResultNotPushed, failure: FailureOther},
		"sigterm-after-tag":           {kind: ResultNotPushed, failure: FailureOther},

		// Origin doesn't answer, so whether the version is on GitHub can't be told.
		"origin-unreachable-branch":   {kind: ResultUnverified, failure: FailureOther},
		"origin-unreachable-nobranch": {kind: ResultUnverified, failure: FailureOther},

		"dirty": failed(FailureDirty), "untracked-only": failed(FailureDirty),
		"ff-only-ahead": failed(FailureOther), "ff-only-ahead-from-feature": failed(FailureOther),
		"unpushed-commits": failed(FailureOther), "major-no-confirm": failed(FailureUnconfirmed),
		"missing-branch": failed(FailureNoBranch), "other-worktree": failed(FailureOther),
		"post-checkout-fails": failed(FailureOther), "sigint-during-pull-behind": failed(FailureMidPull),
		"signing-gpg-fails": failed(FailureSigning), "signing-ssh-missing-key": failed(FailureSigning),
		"signing-fails-from-feature": failed(FailureSigning), "tag-on-head": failed(FailureNothingNew),
		"version-moved": failed(FailureMoved), "remote-tag-other-commit": failed(FailureTaken),
	}

	names := recordingNamesIn(t, magex1302Recordings)
	sdk := name("sdk")

	for _, recording := range names {
		if recording == "dry-run" {
			continue // the app never sends dry-run
		}

		t.Run(recording, func(t *testing.T) {
			t.Parallel()

			want, ok := wants[recording]
			require.True(t, ok, "no verdict for %s", recording)

			rec := loadRecordingIn(t, magex1302Recordings, recording)
			said := readOutput(rec.log("\n"))
			in := Intent{
				Version: rec.param("expect"), Bump: Bump(rec.param("bump")), Branch: rec.param("branch"),
				Original: recordedBranch(rec.Before.Branch),
			}

			res := judge(sdk, in, said, exitedWith(rec.Exit), seenAfter(rec, said, tagOf(said, in)))

			assert.Equal(t, want.kind, res.Kind)

			if want.failure == "" {
				assert.Nil(t, res.Failure)
			} else if assert.NotNil(t, res.Failure) {
				assert.Equal(t, want.failure, res.Failure.Kind)
			}

			assert.Empty(t, res.InProgress, "no recorded run left git in the middle of anything")
		})
	}

	assert.Len(t, wants, len(names)-1, "every recording but the dry run has a verdict")
}

func TestNotPushedWhenOriginCantBeReachedNamesNoFailure(t *testing.T) {
	t.Parallel()

	rec := loadRecordingIn(t, magex1302Recordings, "origin-without-head")
	said := readOutput(rec.log("\n"))
	in := Intent{Version: recVersion, Bump: recBump, Branch: recBranch, Original: recBranch}

	res := judge(name("sdk"), in, said, exitedWith(rec.Exit), seenAfter(rec, said, recVersion))

	require.Equal(t, ResultNotPushed, res.Kind)
	assert.Equal(t, unreachableWarning+"2", res.PushWarning, "the result quotes why magex didn't send the tag")
	assert.Nil(t, res.Failure, "and says it once")
	assert.Equal(t, []string{"git push origin v1.4.3", "git tag -d v1.4.3"}, res.Commands)

	// A failure that names something else is still named.
	auth := said.clone()
	auth.Auth = "git@github.com: Permission denied (publickey)."
	res = judge(name("sdk"), in, auth, exitedWith(rec.Exit), seenAfter(rec, auth, recVersion))
	require.NotNil(t, res.Failure)
	assert.Equal(t, FailureAuth, res.Failure.Kind)

	// So is an error that isn't the warning again, though magex warned first.
	stopped := said.clone()
	stopped.Errors = []string{"failed to push tag: command 'git' was canceled after 30s: context canceled " +
		"(command failed [git push origin v1.4.3]: signal: interrupt)" + keptHere}
	res = judge(name("sdk"), in, stopped, exitedWith(130), seenAfter(rec, stopped, recVersion))
	require.Equal(t, ResultNotPushed, res.Kind)
	require.NotNil(t, res.Failure, "the warning doesn't say why this push stopped")
	assert.Equal(t, FailureOther, res.Failure.Kind)
	assert.Contains(t, res.Failure.Quote, "signal: interrupt")
}

func TestRepeatsPushWarning(t *testing.T) {
	t.Parallel()

	warning := unreachableWarning + "128"

	assert.True(t, repeatsPushWarning(unreachableError+"128", warning))
	assert.False(t, repeatsPushWarning(unreachableError+"2", warning), "another cause")
	assert.False(t, repeatsPushWarning("failed to push tag: something else", warning))
	assert.False(t, repeatsPushWarning(unreachableError+"128", ""), "no warning")
	assert.False(t, repeatsPushWarning("x", "x"), "a warning that isn't magex's")
	assert.False(t, repeatsPushWarning("", warning), "no error")
}

func TestEveryMagex1302FailureIsNamed(t *testing.T) {
	t.Parallel()

	signing := Failure{
		Kind:    FailureSigning,
		Heading: "Git couldn't sign the tag here. Release in your terminal, where signing can ask you.",
	}
	hook := Failure{
		Kind: FailureHook, Heading: "Your pre-push hook stopped the push: pre-push: blocked by policy.",
		Quote: "pre-push: blocked by policy",
	}
	dirty := Failure{Kind: FailureDirty, Heading: "The clone has uncommitted changes. Nothing was tagged."}
	taken := Failure{
		Kind: FailureTaken, Heading: "That version is already on GitHub at another commit. Someone released it first.",
	}
	other := func(quote string) Failure {
		return Failure{Kind: FailureOther, Heading: "magex stopped: " + quote + ".", Quote: quote}
	}
	stoppedAfterTag := func(signal string) Failure {
		return other("failed to push tag: command 'git' was canceled after 30s: context canceled " +
			"(command failed [git push origin v1.4.3]: signal: " + signal + ")")
	}

	wants := map[string]Failure{
		"dirty": dirty, "untracked-only": dirty,
		"signing-gpg-fails": signing, "signing-ssh-missing-key": signing, "signing-fails-from-feature": signing,
		"pre-push-fails": hook, "pre-push-fails-from-feature": hook,
		"remote-tag-other-commit": taken, "remote-tag-same-commit": taken,
		"sigint-after-tag": stoppedAfterTag("interrupt"), "sigterm-after-tag": stoppedAfterTag("terminated"),
		"tag-on-head":      {Kind: FailureNothingNew, Heading: "Nothing new: the newest commit already has a version."},
		"missing-branch":   {Kind: FailureNoBranch, Heading: "release doesn't exist in this clone or on origin."},
		"version-moved":    {Kind: FailureMoved, Heading: "v1.4.3 appeared on GitHub meanwhile, so magex tagged nothing."},
		"major-no-confirm": {Kind: FailureUnconfirmed, Heading: "magex wanted a confirmation " + brand.Name + " didn't send.", Bug: true},
		"sigint-during-pull-behind": {
			Kind: FailureMidPull, Heading: "Stopped while magex was fast-forwarding master, which may be partly updated. Nothing was tagged.",
		},
		"ff-only-ahead": other("ff-only: branch 'master' has 2 commits that origin/master doesn't have, so it can't " +
			"be fast-forwarded; nothing was tagged"),
		"ff-only-ahead-from-feature": other("ff-only: branch 'master' has 1 commit that origin/master doesn't have, " +
			"so it can't be fast-forwarded; nothing was tagged"),
		"unpushed-commits": other("ff-only: branch 'master' has diverged from origin/master: it has 2 commits that " +
			"origin/master doesn't have, and origin/master has 1 commit it doesn't; nothing was tagged"),
		"origin-unreachable-branch": other("failed to fetch branch 'master' from origin: command failed " +
			"[git fetch --tags --force origin +refs/heads/master:refs/remotes/origin/master]: exit status 128"),
		"origin-unreachable-nobranch": other(unreachableError + "128"),
		"origin-without-head":         other(unreachableError + "2"),
		"other-worktree": other("failed to checkout branch 'master': local branch not found (command failed " +
			"[git checkout master]: exit status 128) and remote checkout failed (command failed " +
			"[git checkout -b master origin/master]: exit status 128)"),
		"post-checkout-fails": other("failed to checkout branch 'master': local branch not found (command failed " +
			"[git checkout master]: exit status 1) and remote checkout failed (command failed " +
			"[git checkout -b master origin/master]: exit status 128)"),
	}

	for _, name := range recordingNamesIn(t, magex1302Recordings) {
		rec := loadRecordingIn(t, magex1302Recordings, name)
		if rec.Exit == 0 {
			assert.NotContains(t, wants, name, "%s didn't fail", name)

			continue
		}

		want, ok := wants[name]
		if !assert.True(t, ok, "a recorded failure with no heading checked: %s", name) {
			continue
		}

		got := ExplainFailure(FailureInput{
			Output: readOutput(rec.log("\n")), End: exitedWith(rec.Exit), Version: rec.param("expect"),
			Branch: rec.param("branch"),
		})
		assert.Equal(t, want, got, name)
	}
}

func TestStopMidPullSaysWhatTheUpdateLeaves(t *testing.T) {
	t.Parallel()

	stopped := exitedWith(130)

	pulling := readLines(append(bumpHeader(), "10:00:00 [INFO] Pulling latest changes...")...)
	assert.Equal(t, "Stopped while magex was pulling. A rebase may be in progress.",
		ExplainFailure(FailureInput{Output: pulling, End: stopped, Branch: recBranch}).Heading)

	forward := readLines(append(bumpHeader(),
		"10:00:00 [INFO] Updating 'main' from origin/main (fast-forward only)...")...)
	assert.Equal(t, "Stopped while magex was fast-forwarding release, which may be partly updated. Nothing was tagged.",
		ExplainFailure(FailureInput{Output: forward, End: stopped, Branch: "release"}).Heading, "the branch released from")

	assert.Equal(t, "Stopped while magex was fast-forwarding the branch, which may be partly updated. Nothing was tagged.",
		ExplainFailure(FailureInput{Output: forward, End: stopped}).Heading, "no branch known")

	forward.Branch = "main"
	assert.Equal(t, "Stopped while magex was fast-forwarding main, which may be partly updated. Nothing was tagged.",
		ExplainFailure(FailureInput{Output: forward, End: stopped}).Heading, "the branch magex said it was on")
}
