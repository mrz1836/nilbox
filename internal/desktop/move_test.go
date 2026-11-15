package desktop_test

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"slices"
	"strconv"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/desktop"
	"github.com/mrz1836/nilbox/internal/event"
	"github.com/mrz1836/nilbox/internal/testutil"
	"github.com/mrz1836/nilbox/internal/uierr"
	"github.com/mrz1836/nilbox/internal/update"
)

var errFakeExit = errors.New("fake: exit status 1")

// exitError is a program's exit with status code, as execpath.Exec reports one.
type exitError struct{ code int }

func (e exitError) Error() string { return "exit status " + strconv.Itoa(e.code) }

func (e exitError) ExitCode() int { return e.code }

// The paths the move tests use: a bundle opened from Downloads, with a space in its folder's
// name, and where moving it puts it.
const (
	movedFrom = "/Users/example/Down loads/Example.app"
	movedTo   = "/Applications/Example.app"
)

// releaseRequirement is a release's designated requirement, as codesign writes it.
const releaseRequirement = `identifier "com.example.app" and certificate leaf = H"0123456789abcdef0123456789abcdef01234567"`

// signedLike answers codesign's display of the app that runs with requirement, as written
// (out), and every other command with nothing.
func signedLike(out string) func(context.Context, []string) ([]byte, []byte, error) {
	return func(_ context.Context, argv []string) ([]byte, []byte, error) {
		if slices.Contains(argv, "--display") {
			return []byte(out), []byte("Executable=" + movedFrom + "/Contents/MacOS/example\n"), nil
		}

		return nil, nil, nil
	}
}

func TestTheMoveRunsDittoThenCodesignWithEachPathAnArgumentOfItsOwn(t *testing.T) {
	t.Parallel()

	assert.Equal(t, []string{"/usr/bin/ditto", "--noqtn", movedFrom, movedTo}, desktop.CopyArgs(movedFrom, movedTo))
	assert.Equal(t, []string{"/usr/bin/codesign", "--display", "--requirements", "-", movedFrom},
		desktop.RequirementArgs(movedFrom))
	assert.Equal(t, []string{"/usr/bin/codesign", "--display", "--requirements", "-", "4242"},
		desktop.RequirementArgs("4242"), "a process by its ID")
	assert.Equal(t, []string{
		"/usr/bin/codesign", "--verify", "--deep", "--strict", "--test-requirement", "=" + releaseRequirement, movedTo,
	}, desktop.VerifyArgs(movedTo, releaseRequirement))

	runner := &testutil.Runner{Script: signedLike("designated => " + releaseRequirement + "\n")}
	cleared := []string{}
	m := desktop.NewMoverClearing(runner, newRig().shell(), func(bundle string) error {
		assert.Len(t, runner.Calls(), 1, "once ditto has copied")

		cleared = append(cleared, bundle)

		return nil
	})

	require.NoError(t, m.Copy(t.Context(), movedFrom, movedTo))
	assert.Equal(t, []string{movedTo}, cleared, "the copy's quarantine mark is taken off, not the original's")
	require.NoError(t, m.Verify(t.Context(), movedTo))

	assert.Equal(t, [][]string{
		{"/usr/bin/ditto", "--noqtn", movedFrom, movedTo},
		{"/usr/bin/codesign", "--display", "--requirements", "-", strconv.Itoa(os.Getpid())},
		{"/usr/bin/codesign", "--verify", "--deep", "--strict", "--test-requirement", "=" + releaseRequirement, movedTo},
	}, runner.Calls(), "by their absolute paths, every value one argument, the copy checked against the process that "+
		"runs, whatever has become of the path it ran from")
}

// codesign writes the requirement it works out for a signature that holds none of its own,
// as an ad hoc one, as a comment; either is the one the copy must meet.
func TestTheCopyMustMeetTheRequirementOfTheAppThatRuns(t *testing.T) {
	t.Parallel()

	adHoc := `cdhash H"2db3c2a6ce55241d061cdc6b05a107c58938355b"`

	for name, tt := range map[string]struct{ out, want string }{
		"a release":        {out: "designated => " + releaseRequirement + "\n", want: releaseRequirement},
		"ad hoc":           {out: "# designated => " + adHoc + "\n", want: adHoc},
		"among the others": {out: "host => anchor apple\n  designated => " + releaseRequirement + "  \n", want: releaseRequirement},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			runner := &testutil.Runner{Script: signedLike(tt.out)}
			m := desktop.NewMoverClearing(runner, newRig().shell(), func(string) error { return nil })

			require.NoError(t, m.Verify(t.Context(), movedTo))

			calls := runner.Calls()
			require.Len(t, calls, 2)
			assert.Equal(t, desktop.VerifyArgs(movedTo, tt.want), calls[1])
		})
	}
}

func TestACopySignedOtherwiseFailsInWords(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		script func(context.Context, []string) ([]byte, []byte, error)
		words  string
		own    bool // the app's own signature couldn't be read, so the copy was never judged
	}{
		{
			name: "signed otherwise, as a build from source beside a release",
			script: func(ctx context.Context, argv []string) ([]byte, []byte, error) {
				if slices.Contains(argv, "--verify") {
					return nil, []byte("test-requirement: code failed to satisfy specified code requirement(s)\n"),
						exitError{code: 3}
				}

				return signedLike("designated => "+releaseRequirement)(ctx, argv)
			},
			words: "it isn't signed the way this copy is",
		},
		{
			name: "the app that runs can't be read",
			script: func(_ context.Context, argv []string) ([]byte, []byte, error) {
				return nil, []byte(argv[len(argv)-1] + ": code object is not signed at all\n"), exitError{code: 1}
			},
			words: "code object is not signed at all",
			own:   true,
		},
		{
			name:   "no requirement named",
			script: signedLike("host => anchor apple\n"),
			words:  "it names no requirement to check a copy against",
			own:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			m := desktop.NewMoverClearing(&testutil.Runner{Script: tt.script}, newRig().shell(), func(string) error { return nil })

			err := m.Verify(t.Context(), movedTo)
			require.Error(t, err)
			assert.Equal(t, tt.words, uierr.Message(err))
			assert.Equal(t, tt.own, errors.Is(err, update.ErrOwnSignature))
		})
	}
}

func TestACopyWhoseMarkStaysFails(t *testing.T) {
	t.Parallel()

	m := desktop.NewMoverClearing(&testutil.Runner{}, newRig().shell(), func(string) error { return errFakeExit })

	err := m.Copy(t.Context(), movedFrom, movedTo)
	require.ErrorIs(t, err, errFakeExit)
	assert.Equal(t, "the download mark couldn't be taken off the copy", uierr.Message(err))
}

func TestTheMoveRefusesAnythingButABundlesPath(t *testing.T) {
	t.Parallel()

	runner := &testutil.Runner{}
	m := desktop.NewMoverClearing(runner, newRig().shell(), func(string) error { return nil })

	for _, path := range []string{
		"", "Example.app", "/Applications/../tmp/Example.app", "/Applications/Example", "/.app",
		"-rf",
	} {
		require.Error(t, m.Copy(t.Context(), path, movedTo), path)
		require.Error(t, m.Copy(t.Context(), movedFrom, path), path)
		require.Error(t, m.Verify(t.Context(), path), path)

		_, err := m.Translocates(path)
		require.Error(t, err, path)
	}

	assert.Empty(t, runner.Calls(), "nothing runs")
}

func TestTheMoveSaysWhyAStepFailedInTheProgramsWords(t *testing.T) {
	t.Parallel()

	runner := &testutil.Runner{Script: func(ctx context.Context, argv []string) ([]byte, []byte, error) {
		switch {
		case argv[0] == desktop.DittoBin:
			return nil, []byte("\nditto: " + movedTo + "/Contents: No space left on device\nmore\n"), errFakeExit
		case slices.Contains(argv, "--verify"):
			return nil, nil, fmt.Errorf("run codesign: %w", exitError{code: 1})
		}

		return signedLike("designated => "+releaseRequirement)(ctx, argv)
	}}
	m := desktop.NewMoverClearing(runner, newRig().shell(), func(string) error { return nil })

	err := m.Copy(t.Context(), movedFrom, movedTo)
	require.ErrorIs(t, err, errFakeExit)
	assert.Equal(t, "ditto: "+movedTo+"/Contents: No space left on device", uierr.Message(err))

	err = m.Verify(t.Context(), movedTo)
	require.ErrorAs(t, err, new(exitError))
	assert.Equal(t, "codesign exited with status 1", uierr.Message(err), "with nothing written, how it ended")
}

// A program that writes nothing to say why is never worded in Go's own text.
func TestAStepThatSaysNothingIsWorded(t *testing.T) {
	t.Parallel()

	for name, tt := range map[string]struct {
		err   error
		words string
	}{
		"an exit status": {err: fmt.Errorf("run ditto: %w", exitError{code: 1}), words: "ditto exited with status 1"},
		"no start":       {err: fmt.Errorf("run ditto: %w", fs.ErrNotExist), words: "ditto couldn't be run"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			m := desktop.NewMoverClearing(&testutil.Runner{Err: tt.err}, newRig().shell(), func(string) error { return nil })

			err := m.Copy(t.Context(), movedFrom, movedTo)
			require.ErrorIs(t, err, tt.err)
			assert.Equal(t, tt.words, uierr.Message(err))
		})
	}
}

func TestAMoveStoppedAtItsLimitSaysSo(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		runner := &testutil.Runner{Script: func(ctx context.Context, _ []string) ([]byte, []byte, error) {
			<-ctx.Done()

			return nil, []byte("ditto: interrupted"), ctx.Err()
		}}
		m := desktop.NewMoverClearing(runner, newRig().shell(), func(string) error { return nil })

		ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
		defer cancel()

		err := m.Copy(ctx, movedFrom, movedTo)
		require.ErrorIs(t, err, context.DeadlineExceeded)
		assert.Equal(t, "ditto took too long and was stopped", uierr.Message(err))
	})
}

func TestTranslocatesReadsTheCopysMarkAndRunsNothing(t *testing.T) {
	t.Parallel()

	runner := &testutil.Runner{}
	asked := []string{}
	m := desktop.NewMoverMarked(runner, newRig().shell(), func(bundle string) (bool, error) {
		asked = append(asked, bundle)

		return true, nil
	})

	marked, err := m.Translocates(movedTo)
	require.NoError(t, err)
	assert.True(t, marked)
	assert.Equal(t, []string{movedTo}, asked)
	assert.Empty(t, runner.Calls())
}

func TestOpenArgsOpenTheBundleAsANewInstance(t *testing.T) {
	t.Parallel()

	got, ok := desktop.OpenArgs("/Users/example/Applications/My Example.app", "")
	assert.True(t, ok)
	assert.Equal(t, []string{desktop.OpenBin, "-n", "/Users/example/Applications/My Example.app"}, got)

	got, ok = desktop.OpenArgs(movedTo, "/tmp/example-fresh-1")
	assert.True(t, ok)
	assert.Equal(t, []string{desktop.OpenBin, "-n", movedTo, "--args", "--data-dir=/tmp/example-fresh-1"}, got)

	for _, bundle := range []string{"", "Example.app", "/Applications/Example", "/Applications/./Example.app", "/.app"} {
		got, ok = desktop.OpenArgs(bundle, "")
		assert.False(t, ok, bundle)
		assert.Nil(t, got, bundle)
	}
}

func TestAMovedCopyOpensOnceTheAppHasQuit(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		r := newRig()
		s := r.shell()

		_, ok := s.RelaunchCommand(movedFrom+"/Contents/MacOS/example", "")
		assert.False(t, ok, "nothing opens after a plain quit")

		desktop.NewMover(&testutil.Runner{}, s).OpenAfterQuit(movedTo)
		synctest.Wait()

		assert.Equal(t, 1, r.app.quitCount(), "the app quits the normal way")

		argv, ok := s.RelaunchCommand(movedFrom+"/Contents/MacOS/example", "")
		require.True(t, ok)
		assert.Equal(t, []string{desktop.OpenBin, "-n", movedTo}, argv, "the copy, not the bundle that ran")

		argv, ok = s.RelaunchCommand(movedFrom+"/Contents/MacOS/example", "/tmp/example-fresh-1")
		require.True(t, ok)
		assert.Equal(t, []string{desktop.OpenBin, "-n", movedTo, "--args", "--data-dir=/tmp/example-fresh-1"}, argv,
			"on the sandbox's data, when it ran on one")

		require.NoError(t, s.Stop())
	})
}

// A quit that asks first, as when runs go in the app, keeps the copy for the quit the user
// lets through, and asking again, as Open it does, still opens one bundle, once.
func TestAQuitThatAsksFirstOpensTheCopyOnceTheUserLetsItThrough(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		r := newRig()
		guard := &fakeGuard{}
		guard.active.Store(true)

		s := r.shell(func(c *desktop.Config) { c.QuitGuard = guard })

		s.OpenAfterQuit(movedTo)
		synctest.Wait()

		assert.Equal(t, 1, r.app.quitCount(), "the quit is asked for")
		assert.False(t, s.ShouldQuit(), "and asks first, as every quit does while runs go")
		synctest.Wait()
		assert.Equal(t, []event.WindowEvent{event.QuitRequested{}}, r.page.events())

		// Another bundle asked for before the user answers is the one opened.
		s.OpenAfterQuit("/Users/example/Applications/Example.app")
		synctest.Wait()

		argv, ok := s.RelaunchCommand(movedFrom+"/Contents/MacOS/example", "")
		require.True(t, ok)
		assert.Equal(t, []string{desktop.OpenBin, "-n", "/Users/example/Applications/Example.app"}, argv,
			"one command, opening the last copy asked for once")

		// Stop and quit lets it through.
		s.Emit(event.QuitConfirmed{})
		synctest.Wait()
		assert.True(t, s.ShouldQuit())

		require.NoError(t, s.Stop())
	})
}

// Keep running forgets the copy: the app runs on from where it is, and a later quit, days
// on, opens nothing it didn't ask for then. Open it asks again.
func TestKeepRunningForgetsTheCopy(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		r := newRig()
		guard := &fakeGuard{}
		guard.active.Store(true)

		s := r.shell(func(c *desktop.Config) { c.QuitGuard = guard })
		exe := movedFrom + "/Contents/MacOS/example"

		s.OpenAfterQuit(movedTo)
		synctest.Wait()
		assert.False(t, s.ShouldQuit())

		s.Emit(event.QuitKept{})
		synctest.Wait()

		_, ok := s.RelaunchCommand(exe, "")
		assert.False(t, ok, "a later quit opens nothing")

		s.OpenAfterQuit(movedTo)
		synctest.Wait()

		argv, ok := s.RelaunchCommand(exe, "")
		require.True(t, ok, "Open it asks again")
		assert.Equal(t, []string{desktop.OpenBin, "-n", movedTo}, argv)

		// Keep running beside a reset still opens this bundle, for the reset.
		s.Emit(event.QuitKept{})
		s.Emit(event.ResetRequested{})
		synctest.Wait()

		argv, ok = s.RelaunchCommand(exe, "")
		require.True(t, ok)
		assert.Equal(t, []string{desktop.OpenBin, "-n", movedFrom}, argv)

		require.NoError(t, s.Stop())
	})
}

// A move holds every quit until it ends, so its copy is never cut off partway: the window
// comes forward, where the move shows, nothing is asked, and the quit goes ahead, once, as
// the last move lets go.
func TestAMoveHoldsEveryQuitUntilItEnds(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		r := newRig()
		s := r.shell(func(c *desktop.Config) { c.QuitGuard = &fakeGuard{} })

		first := s.HoldQuit()
		second := s.HoldQuit()

		assert.False(t, s.ShouldQuit(), "a quit waits for the move")
		synctest.Wait()
		assert.Equal(t, 1, r.window.shown(), "the window comes forward, where the move shows")
		assert.Empty(t, r.page.events(), "and nothing is asked")

		s.Emit(event.QuitConfirmed{})
		synctest.Wait()
		assert.Equal(t, 1, r.app.quitCount(), "Stop and quit asks for the quit")
		assert.False(t, s.ShouldQuit(), "which waits for the move too")

		first()
		synctest.Wait()
		assert.Equal(t, 1, r.app.quitCount(), "not while another move runs")
		assert.False(t, s.ShouldQuit())

		second()
		second()
		synctest.Wait()
		assert.Equal(t, 2, r.app.quitCount(), "the held quit goes ahead once, as the last move lets go")
		assert.True(t, s.ShouldQuit())

		require.NoError(t, s.Stop())
	})
}

func TestAMoveThatEndsWithNoQuitAskedQuitsNothing(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		r := newRig()
		s := r.shell()

		s.HoldQuit()()
		synctest.Wait()
		assert.Zero(t, r.app.quitCount())
		assert.True(t, s.ShouldQuit())

		require.NoError(t, s.Stop())
	})
}

// A move that goes ahead asks for the quit as it ends, and the quit waits for the move to
// let go, so the app quits once.
func TestAMovedCopyOpensOnceTheMoveLetsGo(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		r := newRig()
		s := r.shell()
		m := desktop.NewMover(&testutil.Runner{}, s)

		release := m.HoldQuit()
		m.OpenAfterQuit(movedTo)
		synctest.Wait()
		assert.Zero(t, r.app.quitCount(), "not while the move holds the quit")

		release()
		synctest.Wait()
		assert.Equal(t, 1, r.app.quitCount())

		argv, ok := s.RelaunchCommand(movedFrom+"/Contents/MacOS/example", "")
		require.True(t, ok)
		assert.Equal(t, []string{desktop.OpenBin, "-n", movedTo}, argv)

		require.NoError(t, s.Stop())
	})
}

// A restart to update goes through whatever a move holds, since its helper opens the new
// version; and once the shell stops, a move's end quits nothing.
func TestARestartOrAStopGoesThroughAMove(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		r := newRig()
		s := r.shell()
		release := s.HoldQuit()

		s.Emit(event.QuitConfirmed{Restarting: true})
		synctest.Wait()
		assert.True(t, s.ShouldQuit())

		s.Emit(event.UpdateRestartFailed{})
		synctest.Wait()
		assert.False(t, s.ShouldQuit())

		require.NoError(t, s.Stop())
		assert.True(t, s.ShouldQuit())

		release()
		synctest.Wait()
		assert.Zero(t, r.app.quitCount())
	})
}

// A reset asked for beside a move opens the copy alone: it has the same data, so its start
// carries the reset out, and the app never opens twice.
func TestAResetBesideAMoveOpensTheCopyAlone(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		r := newRig()
		s := r.shell()
		exe := movedFrom + "/Contents/MacOS/example"

		s.Emit(event.ResetRequested{})
		synctest.Wait()

		argv, ok := s.RelaunchCommand(exe, "")
		require.True(t, ok)
		assert.Equal(t, []string{desktop.OpenBin, "-n", movedFrom}, argv, "a reset alone opens this bundle again")

		s.OpenAfterQuit(movedTo)
		synctest.Wait()

		argv, ok = s.RelaunchCommand(exe, "")
		require.True(t, ok)
		assert.Equal(t, []string{desktop.OpenBin, "-n", movedTo}, argv)

		_, ok = s.RelaunchCommand("/tmp/go-build/example", "")
		assert.True(t, ok, "the copy opens even when this run had no bundle")

		require.NoError(t, s.Stop())
	})
}

// A restart to update owns the app's reopening: its helper opens the new version, so the
// shell opens nothing beside it, until the restart fails.
func TestARestartToUpdateOpensNothingBeside(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		r := newRig()
		s := r.shell()
		exe := movedFrom + "/Contents/MacOS/example"

		s.OpenAfterQuit(movedTo)
		s.Emit(event.QuitConfirmed{Restarting: true})
		synctest.Wait()

		_, ok := s.RelaunchCommand(exe, "")
		assert.False(t, ok)

		s.Emit(event.UpdateRestartFailed{})
		synctest.Wait()

		argv, ok := s.RelaunchCommand(exe, "")
		require.True(t, ok)
		assert.Equal(t, []string{desktop.OpenBin, "-n", movedTo}, argv)

		require.NoError(t, s.Stop())
	})
}

func TestNothingOpensOnceTheShellIsStopping(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		r := newRig()
		s := r.shell()
		require.NoError(t, s.Stop())

		s.OpenAfterQuit(movedTo)
		synctest.Wait()

		assert.Zero(t, r.app.quitCount())

		_, ok := s.RelaunchCommand(movedFrom+"/Contents/MacOS/example", "")
		assert.False(t, ok)
	})
}
