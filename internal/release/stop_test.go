package release

import (
	"context"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/event"
	"github.com/mrz1836/nilbox/internal/scriptrun"
	"github.com/mrz1836/nilbox/internal/testutil"
	"github.com/mrz1836/nilbox/internal/uierr"
)

// replayUpTo is a fake magex run that prints the recording called name up to and
// including its first line holding upTo, then waits a minute, as magex does mid-step.
func (r *releaseRig) replayUpTo(name, upTo string) testutil.FakeRun {
	r.t.Helper()

	rec := loadRecording(r.t, name)

	var out strings.Builder

	for _, l := range rec.Lines {
		out.WriteString(l.Text + "\n")

		if strings.Contains(l.Text, upTo) {
			return testutil.FakeRun{Stdout: out.String(), Sleep: time.Minute}
		}
	}

	require.Failf(r.t, "no such line", "%s prints no %q", name, upTo)

	return testutil.FakeRun{}
}

// run returns the run as the window reads it.
func (r *releaseRig) run(id int64) scriptrun.Run {
	r.t.Helper()

	got, err := r.runs.Run(r.t.Context(), id)
	require.NoError(r.t, err)

	return got
}

// killOnCleanup ends the run's process group with SIGKILL when the test ends, before the
// runner stops, as a crash would: a release held mid-pull is never signaled by the app,
// even as it quits, so its fake magex would outlive the test.
func (r *releaseRig) killOnCleanup(id int64) {
	r.t.Helper()

	var pgid int
	require.NoError(r.t, r.db.QueryRowContext(r.t.Context(), "SELECT pgid FROM script_runs WHERE id = ?", id).Scan(&pgid))
	require.Positive(r.t, pgid)

	r.t.Cleanup(func() {
		_ = syscall.Kill(-pgid, syscall.SIGKILL)

		assert.Eventually(r.t, func() bool {
			got, err := r.runs.Run(context.WithoutCancel(r.t.Context()), id)

			return err == nil && !got.Status.Live()
		}, runWait, runPoll, "run %d ends once killed", id)
	})
}

// waitProgress waits for the window to read the run's progress as want.
func (r *releaseRig) waitProgress(id int64, want scriptrun.Progress) {
	r.t.Helper()

	require.Eventually(r.t, func() bool { return r.run(id).Progress == want }, runWait, runPoll,
		"run %d reads as %+v", id, want)
}

func TestStopIsRefusedWhileMagexSwitchesOrPulls(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		upTo string
		want scriptrun.Progress
	}{
		{
			name: "switching to the branch", upTo: "Switching to branch 'master'",
			want: scriptrun.Progress{Working: string(StepOnBranch), StopRefused: stopOff},
		},
		{
			name: "switched, the pull not begun", upTo: "✅ Switched to branch 'master'",
			want: scriptrun.Progress{Step: string(StepOnBranch), StopRefused: stopOff},
		},
		{
			name: "pulling", upTo: "Fast-forward",
			want: scriptrun.Progress{Step: string(StepOnBranch), Working: string(StepUpdated), StopRefused: stopOff},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			r := newReleaseRig(t, rigOptions{base: shownVersion})
			testutil.RunGit(t, r.clone, "checkout", "-q", "-b", recFeature)
			r.magex("1.30.2", r.replayUpTo("patch-feature-behind", tt.upTo))

			id := r.release()
			r.killOnCleanup(id)
			r.waitProgress(id, tt.want)
			assert.False(t, r.run(id).CanStop, "the panel's Stop is off")

			err := r.runs.Stop(t.Context(), id)
			require.ErrorIs(t, err, scriptrun.ErrStopRefused)
			assert.Equal(t, "Stopping now could leave a rebase in progress.", uierr.Message(err))
			assert.True(t, r.run(id).Status.Live(), "magex goes on")
		})
	}
}

func TestStopIsLetThroughOutsideTheSwitchAndThePull(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		upTo string
		want scriptrun.Progress
	}{
		{name: "before the switch", upTo: "Using bump type", want: scriptrun.Progress{}},
		{
			name: "pulled, on another branch than the clone was", upTo: "Successfully pulled latest changes",
			want: scriptrun.Progress{
				Step: string(StepUpdated), StopConfirm: "Stopping now can leave your clone on master.",
			},
		},
		{
			name: "tagged, the tag not sent", upTo: "Created tag",
			want: scriptrun.Progress{
				Step:        string(StepTagCreated),
				StopConfirm: "Stopping now can leave v1.4.3 on this Mac but not on GitHub, and your clone on master.",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			r := newReleaseRig(t, rigOptions{base: shownVersion})
			testutil.RunGit(t, r.clone, "checkout", "-q", "-b", recFeature)
			r.magex("1.30.2", r.replayUpTo("patch-feature-behind", tt.upTo))

			id := r.release()
			r.waitProgress(id, tt.want)
			assert.True(t, r.run(id).CanStop)

			require.NoError(t, r.runs.Stop(t.Context(), id))

			run := r.ended(id)
			assert.Equal(t, scriptrun.StatusStopped, run.Status)
			assert.Equal(t, scriptrun.Progress{}, run.Progress, "a release that ended has no progress")
			assert.Empty(t, r.notices.all(), "nothing reached GitHub")
		})
	}
}

func TestAReleaseOnGitHubIsToldOnce(t *testing.T) {
	t.Parallel()

	r := newReleaseRig(t, rigOptions{base: shownVersion})
	testutil.RunGit(t, r.clone, "checkout", "-q", "-b", recFeature)
	r.magex("1.30.2", r.replay("patch-feature-behind"))
	r.after.set(r.tagged(recVersion, r.second, true))

	run := r.ended(r.release())
	require.Equal(t, scriptrun.StatusSucceeded, run.Status)

	assert.Equal(t, []Notice{{
		Repo: name("sdk"), Title: "v1.4.3 is on GitHub", Body: "Tagged " + r.second[:shortOIDLen] + " on master.",
		URL: "https://github.com/acme-org/sdk/releases/tag/v1.4.3",
	}}, r.notices.all())
}

func TestAReleaseEndingTellsTheReleasePage(t *testing.T) {
	t.Parallel()

	r := newReleaseRig(t, rigOptions{base: shownVersion})
	r.magex("1.29.5", r.replay("signing-gpg-fails"))

	_, err := r.svc.Candidates(t.Context(), false)
	require.NoError(t, err)

	before := r.events.Count()

	r.ended(r.release())

	assert.Equal(t, []event.Event{event.ReleasesUpdated{}}, r.events.All()[before:],
		"the page reads its list again, whether or not the sheet is open")

	_, err = r.svc.Candidates(t.Context(), false)
	require.NoError(t, err)
	assert.Equal(t, 3, r.github.calls(), "and the list, out of date, is read again from GitHub")
}

func TestAReleaseIsToldOnceEvenWhenItsRecordFails(t *testing.T) {
	t.Parallel()

	r := newReleaseRig(t, rigOptions{base: shownVersion})
	r.magex("1.30.2", r.replay("patch-feature-behind"))
	r.after.set(r.tagged(recVersion, r.second, true))

	_, err := r.db.ExecContext(t.Context(), `CREATE TRIGGER refuse_finish BEFORE UPDATE ON script_runs
		WHEN NEW.finished_at IS NOT NULL BEGIN SELECT RAISE(ABORT, 'refused by the test'); END`)
	require.NoError(t, err)

	id := r.release()

	require.Eventually(t, func() bool {
		_, held := r.locks.Holder(r.clone)

		return !held
	}, runWait, runPoll, "the release ends and lets go of its clone")
	require.NoError(t, r.after.failure())

	assert.True(t, r.run(id).Status.Live(), "its end couldn't be recorded")
	assert.Empty(t, r.notices.all(), "so nothing is told yet")

	_, err = r.db.ExecContext(t.Context(), `DROP TRIGGER refuse_finish`)
	require.NoError(t, err)

	// The next start finds it, verifies it, and records it: now it is told, once.
	next := scriptrun.NewEngine(scriptrun.Config{
		DB: r.db, Locks: r.locks, LogDir: r.logDir, Home: r.root, Logger: r.logs.Logger(),
	})
	next.RegisterVerifier(scriptrun.KindRelease, NewVerifier(r.svc))

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})

	go func() {
		defer close(done)

		next.Run(ctx)
	}()

	runs := scriptrun.NewService(next)

	require.Eventually(t, func() bool {
		got, runErr := runs.Run(t.Context(), id)

		return runErr == nil && !got.Status.Live()
	}, runWait, runPoll)

	cancel()
	<-done

	told := r.notices.all()
	require.Len(t, told, 1, "told once, by the start whose record held")
	assert.Equal(t, "v1.4.3 is on GitHub", told[0].Title)

	again := scriptrun.NewEngine(scriptrun.Config{DB: r.db, Locks: r.locks, LogDir: r.logDir, Home: r.root})
	again.RegisterVerifier(scriptrun.KindRelease, NewVerifier(r.svc))

	ctx, cancel = context.WithCancel(t.Context())
	cancel()
	again.Run(ctx)

	assert.Len(t, r.notices.all(), 1, "a start after that finds nothing left to tell")
}

func TestAReleaseNotOnGitHubIsntTold(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		recording string
		tags      bool
		kind      ResultKind
	}{
		{name: "failed", recording: "signing-gpg-fails", kind: ResultFailed},
		{name: "tagged on this Mac only", recording: "origin-unreachable-nobranch", tags: true, kind: ResultNotPushed},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			r := newReleaseRig(t, rigOptions{base: shownVersion})
			r.magex("1.29.5", r.replay(tt.recording))

			if tt.tags {
				r.after.set(r.tagged(recVersion, r.second, false))
			}

			run := r.ended(r.release())
			assert.Equal(t, scriptrun.StatusFailed, run.Status)
			assert.Equal(t, tt.kind, resultOf(t, run).Kind)
			assert.Empty(t, r.notices.all(), "only a release on GitHub is told")
		})
	}
}

func TestNoticeOf(t *testing.T) {
	t.Parallel()

	moved := noticeOf(name("sdk"), Intent{Version: recVersion}, Result{
		Kind: ResultReleased, Version: recVersion, Tag: recNext, Moved: true, Commit: "abc",
		TagURL: "https://github.com/acme-org/sdk/releases/tag/v1.4.4",
	})

	assert.Equal(t, Notice{
		Repo: name("sdk"), Title: "v1.4.4 is on GitHub", Body: "Tagged abc. v1.4.3 appeared on GitHub meanwhile.",
		URL: "https://github.com/acme-org/sdk/releases/tag/v1.4.4",
	}, moved, "a short commit as it is, no branch when none is known, and the version that moved")
}
