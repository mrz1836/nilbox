package desktop

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

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

// codesignUnmet is the status codesign exits with when the code it checks doesn't meet a
// requirement it was given (--test-requirement).
const codesignUnmet = 3

var (
	// errNotABundle is returned for a path a move won't hand ditto or codesign: one that
	// isn't the absolute, clean path of a .app bundle.
	errNotABundle = errors.New("not the path of an app bundle")

	// errNoRequirement is returned when codesign names no designated requirement for the
	// app that runs.
	errNoRequirement = errors.New("codesign named no designated requirement")
)

// CopyArgs is the command that copies the app bundle at from into to: `ditto --noqtn`, so
// ditto marks nothing in the copy as quarantined itself. Each path is an argument of its
// own, and ditto copies into to, which the move made empty for it. ditto still copies the
// quarantine mark a download carries, as it copies every extended attribute, so the Mover
// takes it off the copy afterwards (unquarantine).
func CopyArgs(from, to string) []string {
	return []string{DittoBin, "--noqtn", from, to}
}

// RequirementArgs is the command that writes the designated requirement of of to its output:
// `codesign --display --requirements -`, of a running process by its ID (the app that runs,
// whatever has become of the path it ran from) or a bundle by its path, an argument of its
// own. For a release, signed with the release identity, it is `identifier "<bundle ID>" and
// certificate leaf = H"<SHA-1>"`, the same for every build; for a build signed ad hoc, the
// cdhash of that build alone.
func RequirementArgs(of string) []string {
	return []string{CodesignBin, "--display", "--requirements", "-", of}
}

// VerifyArgs is the command that checks the code signature of the bundle at bundle, every
// nested piece of code included, as strictly as codesign can, and that it meets requirement,
// the designated requirement of the app that runs (RequirementArgs): `codesign --verify
// --deep --strict`, the check the update's helper makes of a new version too, with
// `--test-requirement =<requirement>`, the requirement given as text, an argument of its own.
func VerifyArgs(bundle, requirement string) []string {
	return []string{CodesignBin, "--verify", "--deep", "--strict", "--test-requirement", "=" + requirement, bundle}
}

// designated reads the designated requirement from what RequirementArgs wrote: the text after
// "designated => ", which codesign marks as a comment ("# ") when the signature holds none of
// its own and it worked one out, as for a build signed ad hoc.
func designated(out []byte) (string, bool) {
	for line := range strings.SplitSeq(string(out), "\n") {
		line = strings.TrimPrefix(strings.TrimSpace(line), "# ")
		if requirement, ok := strings.CutPrefix(line, "designated => "); ok && strings.TrimSpace(requirement) != "" {
			return strings.TrimSpace(requirement), true
		}
	}

	return "", false
}

// Mover moves the app into Applications for the update's Move to Applications
// (update.Mover): it copies with ditto and checks the copy with codesign, through its
// runner, reads a copy's quarantine mark, and has the shell hold the app's quit while a move
// runs and open the copy once the app has quit the normal way.
type Mover struct {
	runner execpath.Runner
	shell  *Shell

	// self is the app that runs as codesign is given it (RequirementArgs): this process's
	// ID, whose code the kernel knows however its bundle was moved, or a bundle's path in
	// tests.
	self string

	// unmark takes the quarantine mark off a copy (unquarantine), and marked reads whether
	// macOS would translocate one (translocates); tests stand in for both.
	unmark func(bundle string) error
	marked func(bundle string) (bool, error)
}

// NewMover returns the Mover that runs ditto and codesign through runner, and holds, quits,
// and reopens through shell.
func NewMover(runner execpath.Runner, shell *Shell) *Mover {
	return &Mover{
		runner: runner, shell: shell, self: strconv.Itoa(os.Getpid()), unmark: unquarantine, marked: translocates,
	}
}

// HoldQuit holds every quit of the app while a move runs (Shell.HoldQuit).
func (m *Mover) HoldQuit() func() {
	return m.shell.HoldQuit()
}

// Translocates reports whether macOS would open the bundle at bundle from a hidden,
// read-only copy: its own folder carries the quarantine mark a download has, without the
// flag Finder adds as the user moves the app (translocates). It reads the mark and changes
// nothing.
func (m *Mover) Translocates(bundle string) (bool, error) {
	if !isBundle(bundle) {
		return false, fmt.Errorf("%w: read the mark of %q", errNotABundle, bundle)
	}

	return m.marked(bundle)
}

// Copy copies the bundle at from into to with ditto (CopyArgs), then takes the quarantine
// mark a download carries off every file of the copy (unquarantine), so it opens from
// Applications as the copy the user already opened: never translocated, and with no
// question from macOS again. Its error says why in ditto's words, or the mark's.
func (m *Mover) Copy(ctx context.Context, from, to string) error {
	if !isBundle(from) || !isBundle(to) {
		return fmt.Errorf("%w: copy %q into %q", errNotABundle, from, to)
	}

	if _, err := m.run(ctx, CopyArgs(from, to)); err != nil {
		return err
	}

	if err := m.unmark(to); err != nil {
		return uierr.New("the download mark couldn't be taken off the copy", err)
	}

	return nil
}

// Verify checks the code signature of the bundle at bundle, and that it meets the designated
// requirement of the app that runs, read from this process (RequirementArgs, then
// VerifyArgs): a copy signed otherwise, such as a build from source beside a release, fails,
// worded as such. When that requirement can't be read, the error holds
// update.ErrOwnSignature, since the copy was never judged. Its error says why in codesign's
// words otherwise.
func (m *Mover) Verify(ctx context.Context, bundle string) error {
	if !isBundle(bundle) {
		return fmt.Errorf("%w: verify %q", errNotABundle, bundle)
	}

	out, err := m.run(ctx, RequirementArgs(m.self))
	if err != nil {
		return uierr.New(strings.TrimPrefix(uierr.Message(err), m.self+": "),
			fmt.Errorf("%w: %w", update.ErrOwnSignature, err))
	}

	requirement, ok := designated(out)
	if !ok {
		return uierr.New("it names no requirement to check a copy against",
			fmt.Errorf("%w: %w", update.ErrOwnSignature, errNoRequirement))
	}

	_, err = m.run(ctx, VerifyArgs(bundle, requirement))
	if err != nil && execpath.ExitCode(err) == codesignUnmet && ctx.Err() == nil {
		return uierr.New("it isn't signed the way this copy is", err)
	}

	return err
}

// OpenAfterQuit quits the app the normal way and opens bundle once it has quit
// (Shell.OpenAfterQuit).
func (m *Mover) OpenAfterQuit(bundle string) {
	m.shell.OpenAfterQuit(bundle)
}

// run runs argv and returns what it wrote to its output, and words a failure with the first
// line the program wrote to its error output, or, when it wrote none, how it ended (failed).
func (m *Mover) run(ctx context.Context, argv []string) ([]byte, error) {
	stdout, stderr, err := m.runner.Run(ctx, argv[0], argv[1:]...)
	if err == nil {
		return stdout, nil
	}

	reason := execpath.FirstLine(stderr)

	switch name := filepath.Base(argv[0]); {
	case ctx.Err() != nil:
		reason = name + " took too long and was stopped"
	case reason == "":
		reason = failed(name, err)
	}

	return stdout, uierr.New(reason, err)
}

// failed words how the program name ended when it wrote nothing to say why: the status it
// exited with, that something else stopped it (a signal, which leaves no status), or that it
// couldn't be run at all.
func failed(name string, err error) string {
	exit, ok := errors.AsType[interface {
		error
		ExitCode() int
	}](err)

	switch {
	case !ok:
		return name + " couldn't be run"
	case exit.ExitCode() >= 0:
		return name + " exited with status " + strconv.Itoa(exit.ExitCode())
	default:
		return name + " was stopped before it finished"
	}
}

// OpenAfterQuit quits the app the normal way, which asks first while runs go in the app
// (ShouldQuit), and has it open the bundle at bundle once it has quit (RelaunchCommand): the
// copy a move to Applications made or found. While a move holds the quit (HoldQuit), as it
// does until it ends, the quit waits for it. Asked again, it opens one bundle, the last
// asked for, once; a quit the user keeps from going through (Keep running, event.QuitKept)
// forgets it, so a later quit opens nothing. Once the shell is stopping it does nothing.
func (s *Shell) OpenAfterQuit(bundle string) {
	if s.stopping.Load() {
		return
	}

	s.reopen.Store(&bundle)

	if s.heldForMove() {
		return
	}

	// The quit stops the shell, which waits for its tracked work, so the quit is not
	// tracked work itself.
	go s.app.Quit()
}

// HoldQuit holds every quit while a move to Applications runs, so its copy is never cut off
// partway: a quit asked for meanwhile (ShouldQuit, or OpenAfterQuit) waits, and goes ahead,
// once, when the last move under way lets go, which the func it returns does, once. A move
// ends within update.MoveLimit, so a quit waits that long at most. Only a restart to update,
// whose helper owns the reopening, and a shell already stopping, go through regardless.
func (s *Shell) HoldQuit() func() {
	s.mu.Lock()
	s.moves++
	s.mu.Unlock()

	var once sync.Once

	return func() {
		once.Do(s.letGoOfMove)
	}
}

// letGoOfMove ends one move's hold, and quits when it was the last and a quit waited for it.
func (s *Shell) letGoOfMove() {
	s.mu.Lock()
	s.moves--
	quit := s.moves == 0 && s.quitHeld

	if s.moves == 0 {
		s.quitHeld = false
	}
	s.mu.Unlock()

	if quit && !s.stopping.Load() {
		go s.app.Quit()
	}
}

// heldForMove reports whether a move holds the quit, and, when one does, notes the quit, to
// go ahead once the move lets go.
func (s *Shell) heldForMove() bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.moves == 0 {
		return false
	}

	s.quitHeld = true

	return true
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
