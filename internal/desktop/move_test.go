package desktop_test

import (
	"context"
	"errors"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/desktop"
	"github.com/mrz1836/nilbox/internal/event"
	"github.com/mrz1836/nilbox/internal/testutil"
	"github.com/mrz1836/nilbox/internal/uierr"
)

var errFakeExit = errors.New("fake: exit status 1")

// The paths the move tests use: a bundle opened from Downloads, with a space in its folder's
// name, and where moving it puts it.
const (
	movedFrom = "/Users/example/Down loads/Example.app"
	movedTo   = "/Applications/Example.app"
)

func TestTheMoveRunsDittoThenCodesignWithEachPathAnArgumentOfItsOwn(t *testing.T) {
	t.Parallel()

	assert.Equal(t, []string{"/usr/bin/ditto", "--noqtn", movedFrom, movedTo}, desktop.CopyArgs(movedFrom, movedTo))
	assert.Equal(t, []string{"/usr/bin/codesign", "--verify", "--deep", "--strict", movedTo}, desktop.VerifyArgs(movedTo))

	runner := &testutil.Runner{}
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
		{"/usr/bin/codesign", "--verify", "--deep", "--strict", movedTo},
	}, runner.Calls(), "by their absolute paths, every value one argument")
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
	}

	assert.Empty(t, runner.Calls(), "nothing runs")
}

func TestTheMoveSaysWhyAStepFailedInTheProgramsWords(t *testing.T) {
	t.Parallel()

	runner := &testutil.Runner{Script: func(_ context.Context, argv []string) ([]byte, []byte, error) {
		if argv[0] == desktop.DittoBin {
			return nil, []byte("\nditto: " + movedTo + "/Contents: No space left on device\nmore\n"), errFakeExit
		}

		return nil, nil, errFakeExit
	}}
	m := desktop.NewMoverClearing(runner, newRig().shell(), func(string) error { return nil })

	err := m.Copy(t.Context(), movedFrom, movedTo)
	require.ErrorIs(t, err, errFakeExit)
	assert.Equal(t, "ditto: "+movedTo+"/Contents: No space left on device", uierr.Message(err))

	err = m.Verify(t.Context(), movedTo)
	require.ErrorIs(t, err, errFakeExit)
	assert.Equal(t, "fake: exit status 1", uierr.Message(err), "with nothing written, the failure itself")
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
