package desktop

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"path/filepath"

	"github.com/mrz1836/nilbox/internal/execpath"
	"github.com/mrz1836/nilbox/internal/uierr"
	"github.com/mrz1836/nilbox/internal/update"
)

const (
	// DittoBin is macOS's ditto, which copies an app bundle with its signature, its
	// extended attributes, and its links intact.
	DittoBin = "/usr/bin/ditto"

	// CodesignBin is macOS's codesign, which checks a bundle's code signature.
	CodesignBin = "/usr/bin/codesign"
)

// The Mover is the update's.
var _ update.Mover = (*Mover)(nil)

// errNotABundle is returned for a path a move won't hand ditto or codesign: one that isn't
// the absolute, clean path of a .app bundle.
var errNotABundle = errors.New("not the path of an app bundle")

// CopyArgs is the command that copies the app bundle at from into to: `ditto --noqtn`, so
// ditto marks nothing in the copy as quarantined itself. Each path is an argument of its
// own, and ditto copies into to, which the move made empty for it. ditto still copies the
// quarantine mark a download carries, as it copies every extended attribute, so the Mover
// takes it off the copy afterwards (unquarantine).
func CopyArgs(from, to string) []string {
	return []string{DittoBin, "--noqtn", from, to}
}

// VerifyArgs is the command that checks the code signature of the bundle at bundle, every
// nested piece of code included, as strictly as codesign can: `codesign --verify --deep
// --strict`, the check the update's helper makes of a new version too.
func VerifyArgs(bundle string) []string {
	return []string{CodesignBin, "--verify", "--deep", "--strict", bundle}
}

// Mover moves the app into Applications for the update's Move to Applications
// (update.Mover): it copies with ditto and checks the copy with codesign, through its
// runner, and has the shell open the copy once the app has quit the normal way.
type Mover struct {
	runner execpath.Runner
	shell  *Shell

	// unmark takes the quarantine mark off a copy: unquarantine, which tests stand in for.
	unmark func(bundle string) error
}

// NewMover returns the Mover that runs ditto and codesign through runner, and quits and
// reopens through shell.
func NewMover(runner execpath.Runner, shell *Shell) *Mover {
	return &Mover{runner: runner, shell: shell, unmark: unquarantine}
}

// Copy copies the bundle at from into to with ditto (CopyArgs), then takes the quarantine
// mark a download carries off every file of the copy (unquarantine), so it opens from
// Applications as the copy the user already opened: never translocated, and with no
// question from macOS again. Its error says why in ditto's words, or the mark's.
func (m *Mover) Copy(ctx context.Context, from, to string) error {
	if !isBundle(from) || !isBundle(to) {
		return fmt.Errorf("%w: copy %q into %q", errNotABundle, from, to)
	}

	if err := m.run(ctx, CopyArgs(from, to)); err != nil {
		return err
	}

	if err := m.unmark(to); err != nil {
		return uierr.New("the download mark couldn't be taken off the copy", err)
	}

	return nil
}

// Verify checks the code signature of the bundle at bundle (VerifyArgs). Its error says why
// in codesign's words.
func (m *Mover) Verify(ctx context.Context, bundle string) error {
	if !isBundle(bundle) {
		return fmt.Errorf("%w: verify %q", errNotABundle, bundle)
	}

	return m.run(ctx, VerifyArgs(bundle))
}

// OpenAfterQuit quits the app the normal way and opens bundle once it has quit
// (Shell.OpenAfterQuit).
func (m *Mover) OpenAfterQuit(bundle string) {
	m.shell.OpenAfterQuit(bundle)
}

// run runs argv, and words a failure with the first line the program wrote to its error
// output, or, when it wrote none, why it didn't finish.
func (m *Mover) run(ctx context.Context, argv []string) error {
	_, stderr, err := m.runner.Run(ctx, argv[0], argv[1:]...)
	if err == nil {
		return nil
	}

	reason := cmp.Or(execpath.FirstLine(stderr), err.Error())
	if ctx.Err() != nil {
		reason = filepath.Base(argv[0]) + " took too long and was stopped"
	}

	return uierr.New(reason, err)
}

// OpenAfterQuit quits the app the normal way, which asks first while runs go in the app
// (ShouldQuit), and has it open the bundle at bundle once it has quit (RelaunchCommand): the
// copy a move to Applications made or found. Asked again, it opens one bundle, the last
// asked for, once; a quit the user keeps from going through (Keep running, event.QuitKept)
// forgets it, so a later quit opens nothing. Once the shell is stopping it does nothing.
func (s *Shell) OpenAfterQuit(bundle string) {
	if s.stopping.Load() {
		return
	}

	s.reopen.Store(&bundle)

	// The quit stops the shell, which waits for its tracked work, so the quit is not
	// tracked work itself.
	go s.app.Quit()
}

// RelaunchCommand works out the one command that opens the app again once it has quit, for
// the desktop runtime to run then, from the executable exe, on the sandbox's data directory
// when it ran on one (""): none while a restart to update quits the app, whose helper opens
// the new version; else the copy a move to Applications asked to open (OpenArgs), which a
// reset asked for too carries out as well, since it has the same data; else, after a reset,
// this bundle again (RelaunchArgs); else none.
func (s *Shell) RelaunchCommand(exe, sandbox string) ([]string, bool) {
	if s.restarting.Load() {
		return nil, false
	}

	if bundle := s.reopen.Load(); bundle != nil {
		return OpenArgs(*bundle, sandbox)
	}

	if s.relaunch.Load() {
		return RelaunchArgs(exe, sandbox)
	}

	return nil, false
}
