package release

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/brand"
	"github.com/mrz1836/nilbox/internal/scriptrun"
)

// exitedWith is a run that ended with code, as the engine sees it.
func exitedWith(code int) scriptrun.Ending {
	return scriptrun.Ending{ExitCode: &code}
}

// failureCase is a failed run and the heading it should read as.
type failureCase struct {
	in   FailureInput
	want Failure
}

// recordedFailures are the recorded runs that failed, with the headings they read as.
func recordedFailures(t *testing.T) map[string]failureCase {
	t.Helper()

	dirty := Failure{Kind: FailureDirty, Heading: "The clone has uncommitted changes. Nothing was tagged."}
	hook := Failure{
		Kind: FailureHook, Heading: "Your pre-push hook stopped the push: pre-push: blocked by policy.",
		Quote: "pre-push: blocked by policy",
	}
	signing := Failure{
		Kind:    FailureSigning,
		Heading: "Git couldn't sign the tag here. Release in your terminal, where signing can ask you.",
	}
	midPull := Failure{Kind: FailureMidPull, Heading: "Stopped while magex was pulling. A rebase may be in progress."}
	stoppedAfterTag := func(signal string) Failure {
		quote := "failed to push tag: command 'git' was canceled after 30s: context canceled " +
			"(command failed [git push origin v1.4.3]: signal: " + signal + ")"

		return Failure{Kind: FailureOther, Heading: "magex stopped: " + quote + ".", Quote: quote}
	}
	other := func(quote string) Failure {
		return Failure{Kind: FailureOther, Heading: "magex stopped: " + quote + ".", Quote: quote}
	}

	wants := map[string]Failure{
		"dirty":          dirty,
		"untracked-only": dirty,
		"tag-on-head":    {Kind: FailureNothingNew, Heading: "Nothing new: the newest commit already has a version."},
		"remote-tag-other-commit": {
			Kind: FailureTaken, Heading: "That version is already on GitHub at another commit. Someone released it first.",
		},
		"missing-branch": {Kind: FailureNoBranch, Heading: "release doesn't exist in this clone or on origin."},
		"major-no-confirm": {
			Kind: FailureUnconfirmed, Heading: "magex wanted a confirmation " + brand.Name + " didn't send.", Bug: true,
		},
		"ruleset-refused": {
			Kind:    FailureRefused,
			Heading: "GitHub refused the tag v0.0.2: Cannot create ref due to creations being restricted.",
			Quote:   "Cannot create ref due to creations being restricted.",
		},
		"pre-push-fails":              hook,
		"pre-push-fails-from-feature": hook,
		"signing-gpg-fails":           signing,
		"signing-ssh-missing-key":     signing,
		"signing-fails-from-feature":  signing,
		"sigint-mid-rebase":           midPull,
		"sigint-during-pull-diverged": midPull,
		"sigint-after-tag":            stoppedAfterTag("interrupt"),
		"sigterm-after-tag":           stoppedAfterTag("terminated"),
		"no-upstream":                 other("failed to pull latest changes: command failed [git pull --rebase origin]: exit status 1"),
		"no-upstream-from-feature": other("failed to pull latest changes on branch 'master': failed to pull latest " +
			"changes: command failed [git pull --rebase origin]: exit status 1"),
		"origin-unreachable-branch": other("failed to fetch from origin: command failed [git fetch --tags --force origin]: " +
			"exit status 128"),
		"other-worktree": other("failed to checkout branch 'master': local branch not found (command failed " +
			"[git checkout master]: exit status 128) and remote checkout failed (command failed " +
			"[git checkout -b master origin/master]: exit status 128)"),
		"post-checkout-fails": other("failed to checkout branch 'master': local branch not found (command failed " +
			"[git checkout master]: exit status 1) and remote checkout failed (command failed " +
			"[git checkout -b master origin/master]: exit status 128)"),
	}

	cases := make(map[string]failureCase, len(wants))
	for name, want := range wants {
		rec := loadRecording(t, name)
		require.NotZero(t, rec.Exit, "%s failed", name)

		version := recVersion
		if name == "ruleset-refused" {
			version = "v0.0.2"
		}

		cases[name] = failureCase{
			in: FailureInput{
				Output: readOutput(rec.log("\n")), End: exitedWith(rec.Exit), Version: version, Branch: rec.param("branch"),
			},
			want: want,
		}
	}

	return cases
}

func TestEveryRecordedFailureIsCovered(t *testing.T) {
	t.Parallel()

	cases := recordedFailures(t)

	for _, name := range recordingNames(t) {
		if rec := loadRecording(t, name); rec.Exit != 0 {
			assert.Contains(t, cases, name, "a recorded failure with no heading checked")
		}
	}
}

// craftedFailures are failures no recording holds, built from magex's and git's texts.
func craftedFailures() map[string]failureCase {
	read := func(lines ...string) Output { return readLines(lines...) }
	failed := exitedWith(1)

	moved := "❌ Error: expected v1.4.3, but the next patch is v1.4.4: another version was tagged meanwhile; " +
		"nothing was tagged"
	sshDenied := []string{
		"10:00:00 [INFO] Already on target branch 'master'",
		"10:00:00 [INFO] Pulling latest changes...",
		"git@github.com: Permission denied (publickey).",
		cantReachOrigin, "", accessRights, repoExists,
		"❌ Error: failed to fetch from origin: command failed [git fetch --tags --force origin]: exit status 128",
	}
	auth := Failure{Kind: FailureAuth, Heading: "origin refused your git credentials."}
	timeout := Failure{Kind: FailureTimeout, Heading: "A git step took longer than magex's 30 seconds."}
	midPull := Failure{Kind: FailureMidPull, Heading: "Stopped while magex was pulling. A rebase may be in progress."}
	pulling := func(more ...string) Output {
		return read(append([]string{
			"10:00:00 [INFO] Already on target branch 'master'", "10:00:00 [INFO] Pulling latest changes...",
		}, more...)...)
	}

	return map[string]failureCase{
		"a version that moved": {
			in:   FailureInput{Output: read(moved), End: failed, Version: recVersion, Branch: recBranch},
			want: Failure{Kind: FailureMoved, Heading: "v1.4.3 appeared on GitHub meanwhile, so magex tagged nothing."},
		},
		"a version that moved, read from the JSON result": {
			in: FailureInput{Output: Output{Result: &BumpResult{
				Error: "expected v1.4.3, but the next minor is v1.5.0: another version was tagged meanwhile; nothing was tagged",
			}}, End: failed},
			want: Failure{Kind: FailureMoved, Heading: "v1.4.3 appeared on GitHub meanwhile, so magex tagged nothing."},
		},
		"an expected version magex would pass over": {
			in: FailureInput{
				Output:  read("❌ Error: expected v1.4.5, but the next patch is v1.4.3; nothing was tagged"),
				End:     failed,
				Version: "v1.4.5",
			},
			want: Failure{
				Kind:    FailureOther,
				Heading: "magex stopped: expected v1.4.5, but the next patch is v1.4.3; nothing was tagged.",
				Quote:   "expected v1.4.5, but the next patch is v1.4.3; nothing was tagged",
			},
		},
		"a missing branch the input doesn't name": {
			in:   FailureInput{Output: read("❌ Error: branch does not exist locally or remotely: 'release'"), End: failed},
			want: Failure{Kind: FailureNoBranch, Heading: "release doesn't exist in this clone or on origin."},
		},
		"the safety check": {
			in: FailureInput{Output: read(
				"10:00:00 [WARN] ⚠️  unexpected major version jump from v1.4.2 to v3.0.0 when BUMP=patch",
				"10:00:00 [WARN] To proceed anyway, add 'force' parameter",
				"10:00:00 [WARN] Or use 'dry-run' to preview the change first",
				"❌ Error: version bump blocked due to safety check - use FORCE_VERSION_BUMP=true to override",
			), End: failed},
			want: Failure{
				Kind:    FailureUnsafe,
				Heading: "magex refused this bump as unsafe: unexpected major version jump from v1.4.2 to v3.0.0 when BUMP=patch.",
				Quote:   "unexpected major version jump from v1.4.2 to v3.0.0 when BUMP=patch",
			},
		},
		"the safety check without its reason": {
			in: FailureInput{Output: read(
				"❌ Error: version bump blocked due to safety check - use FORCE_VERSION_BUMP=true to override",
			), End: failed},
			want: Failure{Kind: FailureUnsafe, Heading: "magex refused this bump as unsafe."},
		},
		"ssh credentials": {
			in:   FailureInput{Output: read(sshDenied...), End: failed},
			want: auth,
		},
		"https credentials on the push": {
			in: FailureInput{Output: read(
				"10:00:00 [INFO] Pushing tag to remote...",
				"remote: Permission to acme-org/example-repo.git denied to someone.",
				"fatal: unable to access 'https://github.com/acme-org/example-repo.git/': The requested URL returned error: 403",
				"10:00:00 [ERROR] ❌ Failed to push tag to remote",
				"❌ Error: failed to push tag: command failed [git push origin v1.4.3]: exit status 128",
			), End: failed},
			want: auth,
		},
		"no credentials to give": {
			in: FailureInput{Output: read(
				"fatal: could not read Username for 'https://github.com': terminal prompts disabled",
				"❌ Error: failed to push tag: remote 'origin' exists but may not be accessible: command failed "+
					"[git ls-remote --exit-code origin HEAD]: exit status 128",
				"tag v1.4.3 was left in the local repository: push it with 'git push origin v1.4.3' or delete it "+
					"with 'git tag -d v1.4.3'",
			), End: failed},
			want: auth,
		},
		"a timeout on the push": {
			in: FailureInput{Output: read(
				"10:00:00 [INFO] Pushing tag to remote...",
				"❌ Error: failed to push tag: command 'git' exceeded timeout of 30s (context deadline exceeded): "+
					"signal: interrupt",
			), End: failed},
			want: timeout,
		},
		"a timeout on the pull comes before a stop": {
			in: FailureInput{Output: pulling(
				"❌ Error: failed to pull latest changes: command 'git' in '/work/clone' exceeded timeout of 30s: " +
					"context deadline exceeded",
			), End: exitedWith(130)},
			want: timeout,
		},
		"a closed terminal while magex pulled": {
			in: FailureInput{
				Output: pulling(),
				End:    scriptrun.Ending{Interrupted: "The terminal closed while this was running."},
			},
			want: midPull,
		},
		"a crash while magex pulled": {
			in:   FailureInput{Output: pulling(), End: exitedWith(137)},
			want: midPull,
		},
		"the app quitting while magex pulled": {
			in:   FailureInput{Output: pulling(), End: scriptrun.Ending{Quit: true}},
			want: midPull,
		},
		"a ruleset without its rule": {
			in: FailureInput{Output: read(
				"✅ ✅ Created tag: v1.4.4",
				"10:00:00 [INFO] Pushing tag to remote...",
				" ! [remote rejected] v1.4.4 -> v1.4.4 (pre-receive hook declined)",
				"error: failed to push some refs to 'https://github.com/acme-org/example-repo.git'",
				"❌ Error: failed to push tag: command failed [git push origin v1.4.4]: exit status 1",
			), End: failed, Version: recVersion},
			want: Failure{
				Kind: FailureRefused, Heading: "GitHub refused the tag v1.4.4: pre-receive hook declined.",
				Quote: "pre-receive hook declined",
			},
		},
		"a hook that said nothing": {
			in: FailureInput{Output: read(
				"10:00:00 [INFO] Pushing tag to remote...",
				"error: failed to push some refs to '/work/origin.git'",
				"❌ Error: failed to push tag: command failed [git push origin v1.4.3]: exit status 1",
			), End: failed},
			want: Failure{Kind: FailureHook, Heading: "Your pre-push hook stopped the push."},
		},
		"a tag git itself rejected isn't a hook": {
			in: FailureInput{Output: read(
				"10:00:00 [INFO] Pushing tag to remote...",
				" ! [rejected]        v1.4.3 -> v1.4.3 (already exists)",
				"error: failed to push some refs to '/work/origin.git'",
				"❌ Error: failed to push tag: command failed [git push origin v1.4.3]: exit status 1",
			), End: failed},
			want: Failure{
				Kind:    FailureOther,
				Heading: "magex stopped: failed to push tag: command failed [git push origin v1.4.3]: exit status 1.",
				Quote:   "failed to push tag: command failed [git push origin v1.4.3]: exit status 1",
			},
		},
		"only a logged error": {
			in:   FailureInput{Output: read("10:00:00 [ERROR] ❌ Something broke."), End: failed},
			want: Failure{Kind: FailureOther, Heading: "magex stopped: Something broke.", Quote: "Something broke."},
		},
		"nothing said at all": {
			in:   FailureInput{End: failed},
			want: Failure{Kind: FailureOther, Heading: "magex stopped."},
		},
	}
}

func TestExplainFailure(t *testing.T) {
	t.Parallel()

	all := craftedFailures()
	for name, tc := range recordedFailures(t) {
		all["recorded "+name] = tc
	}

	for name, tc := range all {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, ExplainFailure(tc.in))
		})
	}
}

func TestEveryHeadingIsProduced(t *testing.T) {
	t.Parallel()

	produced := map[FailureKind]bool{}
	for _, tc := range craftedFailures() {
		produced[ExplainFailure(tc.in).Kind] = true
	}

	for _, tc := range recordedFailures(t) {
		produced[ExplainFailure(tc.in).Kind] = true
	}

	for _, kind := range []FailureKind{
		FailureUnconfirmed, FailureMoved, FailureDirty, FailureNothingNew, FailureNoBranch, FailureUnsafe,
		FailureTaken, FailureTimeout, FailureMidPull, FailureSigning, FailureAuth, FailureRefused, FailureHook,
		FailureOther,
	} {
		assert.True(t, produced[kind], "no case reads as %s", kind)
	}

	rules := failureRules()
	assert.Len(t, rules, 13, "every kind but FailureOther has a rule")

	for _, rule := range rules {
		assert.True(t, produced[rule.kind], "rule %s is never used", rule.kind)
	}
}

func TestAStopOnlyCountsWhileMagexPulls(t *testing.T) {
	t.Parallel()

	// The same error with a clean exit and nothing canceled is any other failure.
	out := readLines(
		"10:00:00 [INFO] Already on target branch 'master'",
		"10:00:00 [INFO] Pulling latest changes...",
		"❌ Error: failed to pull latest changes: command failed [git pull --rebase origin]: exit status 1",
	)
	assert.Equal(t, FailureOther, ExplainFailure(FailureInput{Output: out, End: exitedWith(1)}).Kind)
	assert.Equal(t, FailureOther, ExplainFailure(FailureInput{Output: out}).Kind, "an unknown status isn't a signal")
	assert.Equal(t, FailureMidPull, ExplainFailure(FailureInput{Output: out, End: scriptrun.Ending{Stopped: true}}).Kind)
	assert.Equal(t, FailureMidPull, ExplainFailure(FailureInput{Output: out, End: scriptrun.Ending{TimedOut: true}}).Kind)

	// Stopped while it switched branches is no pull.
	switching := readLines("10:00:00 [INFO] Switching to branch 'master'...")
	assert.Equal(t, FailureOther, ExplainFailure(FailureInput{Output: switching, End: exitedWith(130)}).Kind)
}
