package release

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/gitread"
	"github.com/mrz1836/nilbox/internal/integrationprefs"
	"github.com/mrz1836/nilbox/internal/integrations"
	"github.com/mrz1836/nilbox/internal/scriptrun"
	"github.com/mrz1836/nilbox/internal/testutil"
	"github.com/mrz1836/nilbox/internal/textutil"
	"github.com/mrz1836/nilbox/internal/uierr"
)

// errNoAnswer is origin not answering, and errBlank a failure that says nothing.
var (
	errNoAnswer = errors.New("git ls-remote: Could not read from remote repository.: exit status 128")
	errBlank    = errors.New(" ")
)

func TestRunEnv(t *testing.T) {
	t.Parallel()

	base := []string{"LANG=C", "NO_COLOR=0", "GIT_TERMINAL_PROMPT=1", "PATH=/usr/bin", "EMPTY"}

	assert.Equal(t, []string{
		"LANG=C", "PATH=/usr/bin", "EMPTY", "GIT_TERMINAL_PROMPT=0", "MAGEX_NO_UPDATE_CHECK=1", "NO_COLOR=1",
	}, runEnv(base, integrationprefs.RunsInApp), "the shell's, quiet, and with no prompt no one can answer")

	assert.Equal(t, []string{"MAGEX_NO_UPDATE_CHECK=1"}, runEnv(base, integrationprefs.RunsInTerminal),
		"only what the script exports, with colors kept")
}

func TestTagOf(t *testing.T) {
	t.Parallel()

	in := Intent{Version: recVersion}

	assert.Equal(t, recVersion, tagOf(Output{}, in))
	assert.Equal(t, "v1.4.4", tagOf(Output{Version: "v1.4.4"}, in), "the summary's To: line")
	assert.Equal(t, "v1.4.5", tagOf(Output{Version: "v1.4.4", Tag: "v1.4.5"}, in), "the tag magex made")
	assert.Equal(t, "v1.4.6", tagOf(Output{Tag: "v1.4.5", Result: &BumpResult{Tag: "v1.4.6"}}, in), "its JSON")
}

func TestJudgeAndOutcome(t *testing.T) {
	t.Parallel()

	in := Intent{Version: recVersion, Branch: branchMaster, Original: recFeature, Head: newestOID}
	exited := func(code int) scriptrun.Ending { return scriptrun.Ending{ExitCode: &code} }

	tests := []struct {
		name    string
		said    Output
		end     scriptrun.Ending
		engine  scriptrun.Outcome
		got     seen
		status  scriptrun.Status
		summary string
		kind    ResultKind
		failure FailureKind
		taken   bool
		cmds    []string
	}{
		{
			name: "origin couldn't be asked", end: exited(0), engine: scriptrun.Outcome{Status: scriptrun.StatusSucceeded},
			got:    seen{tag: recVersion, err: errNoAnswer, branch: branchMaster, branchKnown: true},
			status: scriptrun.StatusFailed, kind: ResultUnverified,
			summary: "Couldn't check whether v1.4.3 reached GitHub: Could not read from remote repository.: " +
				"exit status 128.",
		},
		{
			name: "stopped before it did anything", end: scriptrun.Ending{Stopped: true},
			engine: scriptrun.Outcome{Status: scriptrun.StatusStopped, Summary: "Stopped."},
			got:    seen{tag: recVersion}, status: scriptrun.StatusStopped, summary: "Stopped.", kind: ResultFailed,
			failure: FailureOther,
		},
		{
			name: "stopped while it pulled", said: Output{Working: StepUpdated}, end: scriptrun.Ending{Stopped: true},
			engine: scriptrun.Outcome{Status: scriptrun.StatusStopped, Summary: "Stopped."},
			got:    seen{tag: recVersion, op: gitread.OperationRebasing}, status: scriptrun.StatusStopped,
			summary: "Stopped while magex was pulling. A rebase may be in progress.", kind: ResultFailed,
			failure: FailureMidPull,
		},
		{
			name: "origin couldn't be asked, and git said nothing", end: exited(0),
			engine: scriptrun.Outcome{Status: scriptrun.StatusSucceeded},
			got:    seen{tag: recVersion, err: errBlank}, status: scriptrun.StatusFailed, kind: ResultUnverified,
			summary: "Couldn't check whether v1.4.3 reached GitHub.",
		},
		{
			name: "interrupted after the tag, which reached GitHub", said: Output{Tag: recVersion},
			end:    scriptrun.Ending{Interrupted: "closed"},
			engine: scriptrun.Outcome{Status: scriptrun.StatusInterrupted, Summary: "closed"},
			got:    seen{tag: recVersion, commit: newestOID, local: newestOID}, status: scriptrun.StatusSucceeded,
			summary: "v1.4.3 is on GitHub.", kind: ResultReleased,
		},
		{
			name: "the JSON result names the commit it tagged", said: Output{Result: &BumpResult{
				Tag: recVersion, Commit: newestOID,
			}},
			end: exited(0), engine: scriptrun.Outcome{Status: scriptrun.StatusSucceeded},
			got: seen{tag: recVersion, commit: newestOID}, status: scriptrun.StatusSucceeded,
			summary: "v1.4.3 is on GitHub.", kind: ResultReleased,
		},
		{
			name: "the JSON result names another commit than origin's", said: Output{Result: &BumpResult{
				Tag: recVersion, Commit: olderOID,
			}},
			end: exited(1), engine: scriptrun.Outcome{Status: scriptrun.StatusFailed, Summary: "It exited with status 1."},
			got: seen{tag: recVersion, commit: newestOID, local: olderOID}, status: scriptrun.StatusFailed,
			summary: takenHeading, kind: ResultFailed, failure: FailureTaken, taken: true,
			cmds: []string{"git tag -d v1.4.3"},
		},
		{
			name: "magex tagged here, and origin has the version at another commit", said: Output{
				Tag: recVersion, Errors: []string{"tag already exists on remote: v1.4.3"},
			},
			end: exited(1), engine: scriptrun.Outcome{Status: scriptrun.StatusFailed, Summary: "It exited with status 1."},
			got: seen{tag: recVersion, commit: olderOID, local: newestOID}, status: scriptrun.StatusFailed,
			summary: takenHeading, kind: ResultFailed, failure: FailureTaken, taken: true,
			cmds: []string{"git tag -d v1.4.3"},
		},
		{
			name: "stopped before the tag, and someone else released the version", end: scriptrun.Ending{Stopped: true},
			engine: scriptrun.Outcome{Status: scriptrun.StatusStopped, Summary: "Stopped."},
			got:    seen{tag: recVersion, commit: newestOID}, status: scriptrun.StatusStopped,
			summary: "Someone else released v1.4.3 meanwhile.", kind: ResultFailed, failure: FailureTaken, taken: true,
		},
		{
			name: "the version someone else released, fetched into the clone", end: scriptrun.Ending{Stopped: true},
			engine: scriptrun.Outcome{Status: scriptrun.StatusStopped, Summary: "Stopped."},
			got:    seen{tag: recVersion, commit: newestOID, local: newestOID}, status: scriptrun.StatusStopped,
			summary: "Someone else released v1.4.3 meanwhile.", kind: ResultFailed, failure: FailureTaken, taken: true,
		},
		{
			name: "a tag only the clone has, which magex didn't mention", end: exited(130),
			engine: scriptrun.Outcome{Status: scriptrun.StatusFailed, Summary: "It exited with status 130."},
			got:    seen{tag: recVersion, local: newestOID}, status: scriptrun.StatusFailed,
			summary: "v1.4.3 is tagged on this Mac but isn't on GitHub.", kind: ResultNotPushed,
			cmds: []string{"git push origin v1.4.3", "git tag -d v1.4.3"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			res := judge(name("sdk"), in, tt.said, tt.end, tt.got)
			out := outcomeOf(res, tt.said, tt.engine)

			assert.Equal(t, tt.kind, res.Kind)
			assert.Equal(t, tt.status, out.Status)
			assert.Equal(t, tt.summary, out.Summary)
			assert.Equal(t, res, out.Detail)
			assert.Equal(t, tt.taken, res.Taken)
			assert.Equal(t, textutil.NonNil(tt.cmds), res.Commands, "never nil")

			if tt.failure == "" {
				assert.Nil(t, res.Failure)
			} else {
				require.NotNil(t, res.Failure)
				assert.Equal(t, tt.failure, res.Failure.Kind)
			}
		})
	}
}

func TestJudgeWithoutARepositoryLinksNothing(t *testing.T) {
	t.Parallel()

	res := judge(scriptrun.Record{}.Repo, Intent{Version: recVersion}, Output{Tag: recVersion}, scriptrun.Ending{},
		seen{tag: recVersion, commit: newestOID, local: newestOID})

	assert.Equal(t, ResultReleased, res.Kind)
	assert.Empty(t, res.TagURL)
	assert.Empty(t, res.WorkflowURL)
	assert.False(t, res.Newer, "no head was shown to compare with")
}

// slowGit is a git whose ls-remote holds on well past its context's end, as one whose ssh
// keeps its output open does. Its reads of the clone fail.
type slowGit struct {
	started  chan struct{}
	finished atomic.Bool
}

func (g *slowGit) LocalTagCommit(context.Context, string, string) (string, error) {
	return "", errNoAnswer
}

func (g *slowGit) CurrentBranch(context.Context, string) (string, error) { return "", errNoAnswer }

func (g *slowGit) InProgress(context.Context, string) (gitread.Operation, error) {
	return gitread.OperationNone, errNoAnswer
}

func (g *slowGit) RemoteTagCommit(ctx context.Context, _, _ string) (string, error) {
	close(g.started)
	<-ctx.Done()
	time.Sleep(time.Minute)
	g.finished.Store(true)

	return "", ctx.Err()
}

func TestVerifyingReturnsAtOnceWhenTheAppQuits(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		s := NewService(Config{})
		slow := &slowGit{started: make(chan struct{})}
		s.git = slow

		ctx, cancel := context.WithCancel(t.Context())

		go func() {
			<-slow.started
			cancel()
		}()

		got := s.look(ctx, "/work/sdk", recVersion)
		require.ErrorIs(t, got.err, context.Canceled)
		assert.False(t, slow.finished.Load(), "it didn't wait for git")

		ended, stop := context.WithCancel(t.Context())
		stop()

		require.ErrorIs(t, s.look(ended, "/work/sdk", recVersion).err, context.Canceled, "nor start it once ended")

		time.Sleep(2 * time.Minute)
		assert.True(t, slow.finished.Load(), "git ends on its own, its answer unread")
	})
}

func TestAFailureOnlyABugCausesIsLogged(t *testing.T) {
	t.Parallel()

	logs := &testutil.Logs{}
	s := NewService(Config{Logger: logs.Logger()})

	s.released(t.Context(), name("sdk"), "/work/sdk", Result{Version: majorVersion, Failure: &Failure{Bug: true}})
	assert.Contains(t, logs.String(), "this is a bug")
}

func TestVerifierNeedsAnIntent(t *testing.T) {
	t.Parallel()

	logs := &testutil.Logs{}
	v := NewVerifier(NewService(Config{Git: gitread.New(gitread.Config{}), Logger: logs.Logger()}))

	for _, intent := range []any{nil, map[string]any{"version": "soon"}, map[string]any{"version": 3}, func() {}} {
		got := v.Verify(scriptrun.Record{ID: 7, Result: scriptrun.Result{Intent: intent}})
		assert.Nil(t, got.Output, "%v", intent)
		assert.Nil(t, got.Finish, "%v", intent)
	}

	assert.Contains(t, logs.String(), "can't be verified")

	got := v.Verify(scriptrun.Record{Result: scriptrun.Result{Intent: map[string]any{"version": recVersion}}})
	assert.NotNil(t, got.Output)
	assert.NotNil(t, got.Finish)

	none := NewVerifier(NewService(Config{})).Verify(scriptrun.Record{
		Result: scriptrun.Result{Intent: map[string]any{"version": recVersion}},
	})
	assert.Nil(t, none.Finish, "a Service that can't read git verifies nothing")
}

func TestAReleaseFromBeforeARestartWaitsForTheShell(t *testing.T) {
	t.Parallel()

	never := make(chan struct{})
	s := NewService(Config{Git: gitread.New(gitread.Config{}), Ready: func() <-chan struct{} { return never }})

	got := NewVerifier(s).Verify(scriptrun.Record{
		Repo: name("sdk"), Dir: "/work/sdk", Result: scriptrun.Result{Intent: map[string]any{"version": recVersion}},
	})

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	out := got.Finish(ctx, scriptrun.Ending{Interrupted: "closed"},
		scriptrun.Outcome{Status: scriptrun.StatusInterrupted, Summary: "closed"})

	assert.Equal(t, scriptrun.Outcome{Unsettled: true}, out,
		"the app quit before the shell was read: the next start verifies it")
}

func TestNotReadyReason(t *testing.T) {
	t.Parallel()

	gated := Offer{Bump: BumpMajor, Bumps: []BumpOffer{{Bump: BumpMajor, Note: "Needs magex 1.30.0 or later"}}}
	assert.Equal(t, "Needs magex 1.30.0 or later.", notReadyReason(gated))

	assert.Equal(t, "The release can't start yet. Check again.", notReadyReason(Offer{Bump: BumpPatch}))
}

func TestAVersionThatWentDownIsSaidPlainly(t *testing.T) {
	t.Parallel()

	err := movedError(StartRequest{Version: "v1.4.5"}, readyFacts(), Offer{Bump: BumpPatch, Version: recVersion})

	require.ErrorIs(t, err, ErrVersionMoved)
	assert.Equal(t, "The next patch is v1.4.3 now, not v1.4.5.", uierr.Message(err))
}

func TestRecheckCompletesTheIntent(t *testing.T) {
	t.Parallel()

	r := newReleaseRig(t, rigOptions{base: shownVersion})
	testutil.RunGit(t, r.clone, "checkout", "-q", "-b", recFeature)

	req := StartRequest{Repo: sdkRepo, Bump: BumpPatch, Version: recVersion}
	gather := GatherRequest{Repo: name("sdk"), Dir: r.clone, Choice: Choice{RunsIn: integrationprefs.RunsInApp}, Locked: true}
	in := &Intent{Version: recVersion, Bump: BumpPatch}

	argv, err := r.svc.recheck(t.Context(), req, gather, in)
	require.NoError(t, err)
	assert.Equal(t, []string{
		r.tool.Path, "version:bump", "bump=patch", "branch=master", "push=true", "expect=v1.4.3", "json=true", "ff-only",
	}, argv)
	assert.Equal(t, Intent{
		Version: recVersion, Bump: BumpPatch, Branch: branchMaster, Original: recFeature, Head: r.second,
	}, *in, "what the sheet didn't say, the facts do")

	ended, stop := context.WithCancel(t.Context())
	stop()

	_, err = r.svc.recheck(ended, req, gather, in)
	require.ErrorIs(t, err, context.Canceled)
}

func TestShellWord(t *testing.T) {
	t.Parallel()

	tests := []struct {
		word, quoted string
	}{
		{word: "master", quoted: "master"},
		{word: "origin/feature/x-1", quoted: "origin/feature/x-1"},
		{word: "v1.4.3+build.7", quoted: "v1.4.3+build.7"},
		{word: "", quoted: "''"},
		{word: "a b", quoted: "'a b'"},
		{word: "$(touch pwned)", quoted: "'$(touch pwned)'"},
		{word: "`id`", quoted: "'`id`'"},
		{word: "it's", quoted: `'it'\''s'`},
		{word: "x;rm -rf ~", quoted: "'x;rm -rf ~'"},
	}

	for _, tt := range tests {
		assert.Equal(t, tt.quoted, shellWord(tt.word), tt.word)
	}
}

func TestCopyableCommandsAreQuoted(t *testing.T) {
	t.Parallel()

	hostile := "main$(touch pwned)"

	f := readyFacts()
	f.Tool.Features = []integrations.Feature{integrations.FeatureExpect} // pulls with --rebase, so needs an upstream
	f.Repo.DefaultBranch, f.Repo.Branches = hostile, []string{}
	f.Clone.Branch, f.Clone.Upstream = hostile, gitread.Upstream{}

	upstream, found := checkOf(Decide(f), CheckUpstream)
	require.True(t, found)
	assert.Equal(t, []string{"git branch --set-upstream-to='origin/main$(touch pwned)' 'main$(touch pwned)'"},
		upstream.Commands)

	// A local-only tag parses as a version, which holds only characters a shell reads as
	// themselves.
	f = readyFacts()
	f.Clone.LocalTags = append(f.Clone.LocalTags, "v9.9.9-rc.1+build.7")

	local, found := checkOf(Decide(f), CheckLocalOnly)
	require.True(t, found)
	assert.Equal(t, []string{"git push origin v9.9.9-rc.1+build.7", "git tag -d v9.9.9-rc.1+build.7"}, local.Commands)

	res := judge(name("sdk"), Intent{Version: recVersion}, Output{}, scriptrun.Ending{},
		seen{tag: "v1.4.3$(id)", local: newestOID})
	assert.Equal(t, []string{"git push origin 'v1.4.3$(id)'", "git tag -d 'v1.4.3$(id)'"}, res.Commands)
}

func TestWithReason(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "magex stopped.", withReason("magex stopped", "  "))
	assert.Equal(t, "magex stopped: failed to push tag.", withReason("magex stopped", "failed to push tag"))
	assert.Equal(t, "magex stopped: is it there?", withReason("magex stopped", "is it there?"))

	origin, found := checkOf(Decide(Facts{Clone: CloneFacts{Error: " "}}), CheckOrigin)
	require.True(t, found)
	assert.Equal(t, "Couldn't read this clone with git.", origin.Text, "never \": .\"")
}

func TestCloneForSaysWhyItCantLook(t *testing.T) {
	t.Parallel()

	s := NewService(Config{Clones: &fakeClones{err: errGitHubDown}})

	_, err := s.cloneFor(t.Context(), name("sdk"), "")
	require.ErrorIs(t, err, errGitHubDown)
}

func TestAMajorWithoutMagexIsNotReadyRatherThanABug(t *testing.T) {
	t.Parallel()

	r := newReleaseRig(t, rigOptions{base: shownVersion})
	r.tool = ToolFacts{Features: []integrations.Feature{}, Off: true}

	_, err := r.svc.Start(t.Context(), StartRequest{Repo: sdkRepo, Bump: BumpMajor, Version: majorVersion, Typed: majorVersion})
	require.ErrorIs(t, err, ErrNotReady)
	assert.Equal(t, "magex isn't available.", uierr.Message(err))
	assert.NotContains(t, r.logs.String(), "this is a bug")
}
