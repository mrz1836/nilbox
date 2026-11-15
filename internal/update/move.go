package update

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/mrz1836/nilbox/internal/brand"
	"github.com/mrz1836/nilbox/internal/paths"
	"github.com/mrz1836/nilbox/internal/procs"
	"github.com/mrz1836/nilbox/internal/textutil"
	"github.com/mrz1836/nilbox/internal/uierr"
)

// MoveLimit bounds a move to Applications: the copy, and the check of its signature.
const MoveLimit = 5 * time.Minute

// leftoverAfter is how old a folder a move made beside its target must be before a later
// move removes it as a leftover when it can't tell whether the copy of the app that made it
// still runs (sweepLeftovers): older than a move, whose copy and check MoveLimit bounds, then
// takes, unless the Mac slept through it, which MoveLimit's timer doesn't count.
const leftoverAfter = MoveLimit + time.Minute

// stageMarker names the file a move writes into its hidden folder before it copies anything
// there: the app's bundle ID and the move's process ID, one to a line. It tells a later
// move the folder is a move's own, and whether the copy of the app that made it still runs,
// however far the copy had got (ditto writes the bundle's Info.plist almost last).
const stageMarker = ".moving"

// maxStageMarker bounds the read of a stage's marker, a few dozen bytes.
const maxStageMarker = 1 << 10

// maxInfoPlist bounds the read of a bundle's Info.plist, which is a few kilobytes.
const maxInfoPlist = 1 << 20

// stagePrefix names the hidden folder a move makes beside its target, in the same folder, to
// copy and check the app in before the copy takes the target's place: a random suffix
// follows it (os.MkdirTemp). So a copy cut off partway, or never checked, is never where the
// app goes, and a later move knows the leftover as its own.
const stagePrefix = "." + brand.Name + ".app.moving-"

var (
	// ErrCantMove is returned by a move where there is nothing to move: the app runs from
	// an Applications folder already, or from no bundle.
	ErrCantMove = errors.New("the app can't be moved from where it runs")

	// ErrNoMover is returned by a move in a build that can't move itself: no desktop
	// runtime attached a Mover.
	ErrNoMover = errors.New("this build can't move itself")

	// ErrMoving is returned by a move while another is under way.
	ErrMoving = errors.New("the app is moving already")

	// ErrUpdating is returned by a move while the app restarts into an update, which goes
	// first: the restart quits the app, and its helper opens the new version. A move while a
	// restart whose quit was held finishes is ErrFinishing too.
	ErrUpdating = errors.New("the app is restarting into an update")

	// ErrTargetTaken is returned by a move when something other than a copy of the app is
	// where it would go: a link, a file, another app, or a folder that isn't one.
	ErrTargetTaken = errors.New("something else is where the app would go")

	// ErrNoCopy is returned by OpenInApplications when no copy of the app is where a move
	// would put it.
	ErrNoCopy = errors.New("no copy of the app is in Applications")

	// ErrMoveFailed is returned by a move whose copy failed, or didn't pass its signature
	// check, and by OpenInApplications for a copy that doesn't pass it.
	ErrMoveFailed = errors.New("the move failed")

	// ErrQuarantined is returned by OpenInApplications for a copy macOS would open from a
	// hidden folder (Mover.Translocates), where it can't update itself.
	ErrQuarantined = errors.New("the copy in Applications is still marked as downloaded")

	// ErrOwnSignature is in the chain of a Mover's Verify that couldn't read the designated
	// requirement of the app that runs, which a copy is checked against: the copy itself
	// was never judged.
	ErrOwnSignature = errors.New("the app's own signature couldn't be read")
)

// Mover moves the app into an Applications folder: the desktop shell's, which copies with
// ditto and checks the copy with codesign, both through execpath, holds the app's quit while
// a move runs, and opens a bundle once the app has quit. The engine works out where the copy
// goes, makes the folder it is copied into, and puts it in place; the Mover only runs the
// programs, reads the quarantine mark, and holds and relaunches.
type Mover interface {
	// HoldQuit holds every quit of the app while a move runs, so its copy is never cut off
	// partway: a quit asked for meanwhile waits, and goes ahead once the func it returns
	// lets go, which the move calls once, as it ends.
	HoldQuit() (release func())

	// Copy copies the app's bundle at from into to, an empty folder the engine has just
	// made for it, and takes the quarantine mark a download carries off the copy. Its error
	// says why in a few words (uierr.Message).
	Copy(ctx context.Context, from, to string) error

	// Verify checks the code signature of the bundle at bundle, deeply and strictly, and
	// that it meets the designated requirement of the app that runs, read from its process:
	// signed as this copy is. Its error says why in a few words, with ErrOwnSignature in its
	// chain when that requirement couldn't be read.
	Verify(ctx context.Context, bundle string) error

	// Translocates reports whether macOS would open the bundle at bundle from a hidden,
	// read-only copy (App Translocation), as it does an app still marked as downloaded that
	// the user never moved with Finder: one that can't update itself.
	Translocates(bundle string) (bool, error)

	// OpenAfterQuit quits the app the normal way, which asks first while runs go in the
	// app, and opens the bundle at bundle once it has quit. Asked again, it still opens one
	// bundle, once: the last it was asked to. A quit the user keeps from going through
	// opens nothing.
	OpenAfterQuit(bundle string)
}

// MoveOutcome is how a move to Applications, or an Open it, ended.
type MoveOutcome string

// The outcomes. A failure is an error, in words.
const (
	// MoveMoved is the app copied into Applications and checked: the app quits the normal
	// way, and the copy opens once it has.
	MoveMoved MoveOutcome = "moved"

	// MoveExists is a copy of the app already in Applications: nothing was copied or
	// replaced, and OpenInApplications opens that copy, which updates itself when it is
	// older.
	MoveExists MoveOutcome = "exists"

	// MoveOpening is the copy in Applications checked: the app quits the normal way, and
	// that copy opens once it has.
	MoveOpening MoveOutcome = "opening"
)

// MoveResult is what a move to Applications, or an Open it, did.
type MoveResult struct {
	// Outcome says what happened.
	Outcome MoveOutcome `json:"outcome"`

	// Target is the copy in Applications, with ~ for home.
	Target string `json:"target"`
}

// AttachMover hands the engine the Mover it moves the app with. The desktop shell calls it
// once the desktop runtime is created; until then, and in a build with none, a move
// refuses.
func (e *Engine) AttachMover(m Mover) {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.mover = m
}

// moveToApplications moves the app into Applications, as the move dialog's Move to
// Applications asks: where the app can't update itself, it copies the running bundle beside
// the target (the Applications folder every user shares when the user can write to it, else
// the one in home, made when missing), into a hidden folder of its own (stage), checks the
// copy's signature there, and only then renames it into the target, so a copy cut off
// partway, or never checked, is never where the app goes. It then has the Mover quit the app
// and open the copy. It never touches the bundle that runs, which the user may delete once
// the copy opens. The Mover holds the app's quit from the move's start to its end.
//
// A copy of the app at the target already, by its bundle ID, is never replaced: the result
// says so (MoveExists), for Open it, which opens that copy. Anything else there (a link, a
// file, another app, a folder that isn't an app) is ErrTargetTaken, as is something that
// appears there while the copy is made (renameNew). A copy that fails, doesn't pass its
// check, or can't be put in place is removed with its hidden folder, that folder alone.
func (e *Engine) moveToApplications(ctx context.Context) (MoveResult, error) {
	mover, done, err := e.startMove()
	if err != nil {
		return MoveResult{}, err
	}

	defer done()

	target, err := e.moveTarget()
	if err != nil {
		return MoveResult{}, err
	}

	switch found, findErr := e.find(ctx, target); {
	case findErr != nil:
		return MoveResult{}, findErr
	case found == foundOurs:
		e.logger.InfoContext(ctx, "a copy of the app is in Applications already; offering to open it",
			"target", e.tilde(target))

		return MoveResult{Outcome: MoveExists, Target: e.tilde(target)}, nil
	case found != foundNothing:
		return MoveResult{}, e.taken(target, found)
	}

	stage, err := e.stage(ctx, target)
	if err != nil {
		return MoveResult{}, err
	}

	// The copy runs to its end, or its limit, whatever happens to the window's call, so it
	// is never left half done.
	work, cancel := context.WithTimeout(context.WithoutCancel(ctx), MoveLimit)
	defer cancel()

	folder := e.tilde(filepath.Dir(target))
	staged := filepath.Join(stage, filepath.Base(target))
	// The programs name the copy where it is made, which the user never sees: by the target.
	said := func(err error) string {
		return e.plain(strings.ReplaceAll(uierr.Message(err), staged, target))
	}

	if err = mover.Copy(work, e.bundle, staged); err != nil {
		return MoveResult{}, e.discard(ctx, stage, "Couldn't copy "+brand.Name+" into "+folder+": "+said(err), err)
	}

	if err = mover.Verify(work, staged); err != nil {
		return MoveResult{}, e.discard(ctx, stage, unchecked(folder, said(err), err), err)
	}

	if err = e.place(ctx, stage, staged, target); err != nil {
		return MoveResult{}, err
	}

	e.logger.InfoContext(ctx, "moved the app to Applications; it opens from there once this copy quits",
		"from", e.tilde(e.bundle), "to", e.tilde(target))
	mover.OpenAfterQuit(target)

	return MoveResult{Outcome: MoveMoved, Target: e.tilde(target)}, nil
}

// openInApplications opens the copy of the app already in Applications, as Open it asks,
// once it would open where it is and its signature checks out: the Mover quits the app the
// normal way, and opens that copy once it has. A copy macOS would open from a hidden folder,
// still marked as downloaded, is refused (ErrQuarantined), and so is one not signed as this
// copy is, such as a build from source, which never updates itself. Nothing is copied,
// replaced, or unmarked: the user never opened that copy.
func (e *Engine) openInApplications(ctx context.Context) (MoveResult, error) {
	mover, done, err := e.startMove()
	if err != nil {
		return MoveResult{}, err
	}

	defer done()

	target, err := e.moveTarget()
	if err != nil {
		return MoveResult{}, err
	}

	folder := e.tilde(filepath.Dir(target))

	switch found, findErr := e.find(ctx, target); {
	case findErr != nil:
		return MoveResult{}, findErr
	case found == foundNothing:
		return MoveResult{}, uierr.New(brand.Name+" isn't in "+folder+" any more. Move it there instead.", ErrNoCopy)
	case found != foundOurs:
		return MoveResult{}, e.taken(target, found)
	}

	switch marked, markErr := mover.Translocates(target); {
	case markErr != nil:
		e.logger.WarnContext(ctx, "could not read whether the copy in Applications is marked as downloaded; "+
			"checking its signature alone", "target", e.tilde(target), "error", markErr)
	case marked:
		e.logger.InfoContext(ctx, "the copy in Applications is still marked as downloaded; not opening it",
			"target", e.tilde(target))

		return MoveResult{}, uierr.New("The copy in "+folder+" is still marked as downloaded, so macOS would open it "+
			"from a hidden folder, where it can't update itself. Move it to the Trash, then move this one there.",
			ErrQuarantined)
	}

	work, cancel := context.WithTimeout(context.WithoutCancel(ctx), MoveLimit)
	defer cancel()

	if err = mover.Verify(work, target); err != nil {
		return MoveResult{}, e.unopened(folder, err)
	}

	e.logger.InfoContext(ctx, "opening the copy of the app in Applications once this one quits", "target", e.tilde(target))
	mover.OpenAfterQuit(target)

	return MoveResult{Outcome: MoveOpening, Target: e.tilde(target)}, nil
}

// unchecked words why a move's copy in folder didn't pass its check, which failed with err,
// whose words are said: the copy's own fault, or this app's signature that couldn't be read.
func unchecked(folder, said string, err error) string {
	if errors.Is(err, ErrOwnSignature) {
		return brand.Name + " couldn't read its own signature, so it couldn't check the copy in " + folder +
			", and nothing was moved: " + said
	}

	return "The copy in " + folder + " didn't pass its signature check, so nothing was moved: " + said
}

// unopened words why Open it didn't open the copy in folder, whose check failed with err: a
// copy that didn't pass it is the copy's fault, and the user is told to move it to the Trash
// and move this one instead; but when this app's own signature couldn't be read, nothing is
// known of the copy, which the user may open from there themselves.
func (e *Engine) unopened(folder string, err error) error {
	cause := fmt.Errorf("%w: %w", ErrMoveFailed, err)

	if errors.Is(err, ErrOwnSignature) {
		return uierr.New(textutil.Sentence(brand.Name+" couldn't read its own signature, so it couldn't check the copy in "+
			folder+", and didn't open it: "+e.plain(uierr.Message(err)))+" Open that copy from "+folder+" yourself.", cause)
	}

	return uierr.New(textutil.Sentence("The copy in "+folder+" didn't pass its signature check, so it wasn't opened: "+
		e.plain(uierr.Message(err)))+" Move it to the Trash, then move this one there.", cause)
}

// showApp shows the running bundle in Finder, so the user can delete it once the copy in
// Applications opens. A translocated bundle runs from a folder macOS hides, which Finder
// would show in place of the copy the user opened, so it shows none.
func (e *Engine) showApp(ctx context.Context) error {
	switch {
	case e.bundle == "":
		return uierr.New("This copy of "+brand.Name+" doesn't run from an app bundle.", ErrCantMove)
	case e.location.Kind == LocationTranslocated:
		return uierr.New("macOS opened this copy from a hidden folder, so Finder can't show where it is.", ErrCantMove)
	case e.reveal == nil:
		return uierr.New("This build can't show files in Finder.", ErrCantMove)
	}

	return e.reveal.Reveal(ctx, e.bundle) //nolint:wrapcheck // the revealer words its own failure
}

// startMove checks a move can begin and takes the one move: a build that updates, running
// where moving it helps, not restarting into an update (installing, or finishing once its
// quit was held), with a Mover attached, and no move under way. It holds the app's quit
// (Mover.HoldQuit) and returns what lets go of both, once the move has ended. Where a move
// helps, nothing installs, so a restart under way is never met; were one, it goes first:
// its helper replaces this copy and opens the new version, which a move's quit and reopen
// would race.
func (e *Engine) startMove() (Mover, func(), error) {
	e.mu.Lock()
	mover, installing, finishing := e.mover, e.st.installing, e.st.finishing
	e.mu.Unlock()

	switch {
	case !e.Enabled():
		return nil, nil, uierr.New(e.off, ErrOff)
	case !e.location.Movable:
		return nil, nil, e.notMovable()
	case finishing:
		return nil, nil, uierr.New(brand.Name+" is quitting to finish an update. Move it once the update has opened.",
			fmt.Errorf("%w: %w", ErrUpdating, ErrFinishing))
	case installing:
		return nil, nil, uierr.New(brand.Name+" is restarting into an update. Move it once the update has opened.",
			ErrUpdating)
	case mover == nil:
		return nil, nil, uierr.New("This build can't move itself to Applications.", ErrNoMover)
	case !e.moving.TryLock():
		return nil, nil, uierr.New(brand.Name+" is already moving to Applications.", ErrMoving)
	}

	release := mover.HoldQuit()

	return mover, func() {
		e.moving.Unlock()
		release()
	}, nil
}

// notMovable says why the app isn't moved from where it runs.
func (e *Engine) notMovable() error {
	switch {
	case e.location.OK:
		return uierr.New(brand.Name+" already runs from Applications.", ErrCantMove)
	case e.bundle == "":
		return uierr.New("This copy of "+brand.Name+" doesn't run from an app bundle, so there's nothing to move.",
			ErrCantMove)
	default:
		return uierr.New(brand.Name+" is in an Applications folder already, which your account can't change.",
			ErrCantMove)
	}
}

// moveTarget is where a move puts the app now, worked out again as Location's Target is:
// the user's rights may have changed since the start.
func (e *Engine) moveTarget() (string, error) {
	target := moveTarget(e.home, e.applications, e.writable)
	if !filepath.IsAbs(target) {
		return "", uierr.New(brand.Name+" can't tell where your Applications folder is.",
			fmt.Errorf("%w: no home folder to move into", ErrCantMove))
	}

	return target, nil
}

// found is what is where a move would put the app.
type found int

const (
	foundNothing found = iota
	foundOurs
	foundLink
	foundFile
	foundOtherApp
	foundFolder
)

// find says what is at target: nothing, a copy of the app (a folder whose Info.plist names
// brand.BundleID), a link, a file, another app, or a folder that isn't an app. It never
// follows a link. A copy that is the running bundle itself, reached by another path, is
// ErrCantMove.
func (e *Engine) find(ctx context.Context, target string) (found, error) {
	info, err := os.Lstat(target)

	switch {
	case errors.Is(err, fs.ErrNotExist):
		return foundNothing, nil
	case err != nil:
		return 0, uierr.New(brand.Name+" can't look in "+e.tilde(filepath.Dir(target))+".",
			fmt.Errorf("%w: %w", ErrMoveFailed, err))
	case info.Mode()&fs.ModeSymlink != 0:
		return foundLink, nil
	case !info.IsDir():
		return foundFile, nil
	}

	id, err := bundleID(target)
	if err != nil {
		e.logger.InfoContext(ctx, "what is in Applications isn't an app the move can read", "target", e.tilde(target),
			"error", err)

		return foundFolder, nil
	}

	if id != brand.BundleID {
		e.logger.InfoContext(ctx, "another app is where the move would put this one", "target", e.tilde(target),
			"bundleID", id)

		return foundOtherApp, nil
	}

	if e.isSelf(info) {
		return 0, uierr.New(brand.Name+" already runs from Applications.", ErrCantMove)
	}

	return foundOurs, nil
}

// isSelf reports whether info, a folder's, is the bundle that runs: the folder it ran from
// as it started (self), which a bundle the user drags elsewhere on its disk while it runs
// still is, or what its path holds now.
func (e *Engine) isSelf(info fs.FileInfo) bool {
	if e.self != nil && os.SameFile(e.self, info) {
		return true
	}

	running, err := os.Stat(e.bundle)

	return err == nil && os.SameFile(running, info)
}

// taken refuses a move whose target holds something else, saying what.
func (e *Engine) taken(target string, f found) error {
	what := "something else"

	switch f {
	case foundLink:
		what = "a link"
	case foundFile:
		what = "a file"
	case foundOtherApp:
		what = "another app"
	case foundFolder:
		what = "a folder"
	case foundNothing, foundOurs:
	}

	return uierr.New(e.tilde(filepath.Dir(target))+" already has "+what+" named "+filepath.Base(target)+
		", so nothing was moved. Rename or remove it, then try again.", ErrTargetTaken)
}

// stage makes the hidden folder beside target that the copy is made and checked in before
// it takes target's place, and returns it: the Applications folder in home first, when the
// move goes there and it is missing; then, once what earlier moves left there is swept
// (sweepLeftovers), a folder of this move's own (stagePrefix and a random suffix), and in it
// the empty bundle, named as target is, that the copy goes into. Both are made new, so the
// copy never goes into, and a failed copy's removal never touches, anything the move didn't
// make, and the rename into place stays in one file system.
func (e *Engine) stage(ctx context.Context, target string) (string, error) {
	parent := filepath.Dir(target)
	refused := func(err error) error {
		return uierr.New("Couldn't copy "+brand.Name+" into "+e.tilde(parent)+", so nothing was moved.",
			fmt.Errorf("%w: %w", ErrMoveFailed, err))
	}

	if parent == filepath.Join(e.home, "Applications") {
		if err := os.Mkdir(parent, 0o755); err != nil && !errors.Is(err, fs.ErrExist) { //nolint:gosec // an Applications folder, which every app reads
			return "", uierr.New("Couldn't make "+e.tilde(parent)+", so nothing was moved.", fmt.Errorf("%w: %w", ErrMoveFailed, err))
		}
	}

	e.sweepLeftovers(ctx, parent)

	stage, err := os.MkdirTemp(parent, stagePrefix+"*")
	if err != nil {
		return "", refused(err)
	}

	marker := []byte(brand.BundleID + "\n" + strconv.Itoa(e.pid) + "\n")
	if err = os.WriteFile(filepath.Join(stage, stageMarker), marker, 0o600); err == nil {
		err = os.Mkdir(filepath.Join(stage, filepath.Base(target)), 0o755) //nolint:gosec // an app bundle, which the system reads
	}

	if err != nil {
		if removeErr := os.RemoveAll(stage); removeErr != nil {
			e.logger.WarnContext(ctx, "could not remove the folder a move made", "stage", e.tilde(stage), "error", removeErr)
		}

		return "", refused(err)
	}

	return stage, nil
}

// place renames the checked copy at staged into target, never over anything that appeared
// there since the move looked (renameNew), then removes stage, which it leaves holding its
// marker alone. A copy that can't be put in place is removed with stage.
func (e *Engine) place(ctx context.Context, stage, staged, target string) error {
	if err := renameNew(staged, target); err != nil {
		if errors.Is(err, fs.ErrExist) {
			return e.discard(ctx, stage, "Something appeared at "+e.tilde(target)+" just now, so nothing was moved. Try again.",
				fmt.Errorf("%w: %w", ErrTargetTaken, err))
		}

		return e.discard(ctx, stage, "Couldn't put the copy in "+e.tilde(filepath.Dir(target))+", so nothing was moved.", err)
	}

	if err := os.RemoveAll(stage); err != nil {
		e.logger.WarnContext(ctx, "could not remove the folder a move made", "stage", e.tilde(stage), "error", err)
	}

	return nil
}

// sweepLeftovers removes, from folder, what earlier moves left there, as a move cut off
// partway (a crash, a power cut, a force quit) leaves it: a folder named as a move's own
// (stagePrefix), not a link, marked by a move (stageOwner) and holding nothing else but the
// copy, however far it had got, whose maker no longer runs (leftover). Anything else is left
// as it is, and a leftover that can't be removed is logged: neither stops the move.
func (e *Engine) sweepLeftovers(ctx context.Context, folder string) {
	listed, err := os.ReadDir(folder)
	if err != nil {
		e.logger.WarnContext(ctx, "could not look for what earlier moves left", "folder", e.tilde(folder), "error", err)

		return
	}

	now := e.clock.Now()

	for _, item := range listed {
		if !strings.HasPrefix(item.Name(), stagePrefix) || item.Name() == stagePrefix || !item.IsDir() {
			continue
		}

		stage := filepath.Join(folder, item.Name())
		if !e.leftover(ctx, stage, now) {
			continue
		}

		if err = os.RemoveAll(stage); err != nil {
			e.logger.WarnContext(ctx, "could not remove what an earlier move left", "stage", e.tilde(stage), "error", err)

			continue
		}

		e.logger.InfoContext(ctx, "removed what an earlier move left", "stage", e.tilde(stage))
	}
}

// leftover reports whether stage, a folder named as a move's own, is one an earlier move
// left at now: marked by a move of this run, which no move runs in now (moves take turns),
// or of a process that is gone; or, when whether its maker runs can't be told, or nothing
// marks it (cut off before it was marked), older than leftoverAfter. A folder whose maker
// still runs is a move another copy of the app is making, and is left.
func (e *Engine) leftover(ctx context.Context, stage string, now time.Time) bool {
	info, err := os.Lstat(stage)
	if err != nil || !info.IsDir() {
		return false
	}

	inside, err := os.ReadDir(stage)
	if err != nil {
		return false
	}

	stale := now.Sub(info.ModTime()) >= leftoverAfter

	if len(inside) == 0 {
		return stale
	}

	owner, ok := stageOwner(stage, inside)
	if !ok {
		e.logger.InfoContext(ctx, "a folder named as a move's leftover holds something else; leaving it",
			"stage", e.tilde(stage))

		return false
	}

	if owner == e.pid {
		return true
	}

	switch e.probe(owner) {
	case procs.Gone:
		return true
	case procs.Running:
		return false
	case procs.Unknown:
		return stale
	}

	return stale
}

// stageOwner returns the process a move's folder names as its maker: the folder holds its
// marker, a regular file, never a link, naming brand.BundleID and a process ID, and nothing
// else but the copy, a folder (never a link) named as the app is, however far it had got.
func stageOwner(stage string, inside []fs.DirEntry) (int, bool) {
	marked := false

	for _, item := range inside {
		switch {
		case item.Name() == stageMarker && item.Type().IsRegular():
			marked = true
		case item.Name() == brand.Name+".app" && item.IsDir():
		default:
			return 0, false
		}
	}

	if !marked {
		return 0, false
	}

	raw, err := paths.ReadHead(filepath.Join(stage, stageMarker), maxStageMarker)
	if err != nil {
		return 0, false
	}

	id, pid, ok := strings.Cut(strings.TrimSpace(string(raw)), "\n")
	if !ok || id != brand.BundleID {
		return 0, false
	}

	owner, err := strconv.Atoi(pid)

	return owner, err == nil
}

// discard removes stage, the hidden folder a move made, with the copy in it, after a copy
// that failed, didn't pass its check, or couldn't be put in place, and returns the failure
// worded as words says, logged whole. A folder that can't be removed is named, for the user
// to remove; the next move removes it too, once it can.
func (e *Engine) discard(ctx context.Context, stage, words string, cause error) error {
	e.logger.WarnContext(ctx, "the move to Applications failed; removing the copy", "stage", e.tilde(stage),
		"error", cause)

	words = textutil.Sentence(words)

	if err := os.RemoveAll(stage); err != nil {
		e.logger.WarnContext(ctx, "could not remove the copy a failed move left", "stage", e.tilde(stage),
			"error", err)

		words += " Move " + e.tilde(stage) + " to the Trash."
	}

	return uierr.New(words, fmt.Errorf("%w: %w", ErrMoveFailed, cause))
}

// plain words reason, a program's own words, with its paths as the window shows them: the
// bundle that runs by its name alone, since a translocated one's is a folder macOS hides,
// and home as ~.
func (e *Engine) plain(reason string) string {
	if e.bundle != "" {
		reason = strings.ReplaceAll(reason, e.bundle, filepath.Base(e.bundle))
	}

	if e.home != "" {
		reason = strings.ReplaceAll(reason, filepath.Clean(e.home)+string(filepath.Separator), "~/")
	}

	return reason
}

// infoPlist is the part of an XML property list a move reads: the top dictionary's entries,
// in order, each a key or a value.
type infoPlist struct {
	Dict plistDict `xml:"dict"`
}

// plistDict is a property list's dictionary: its entries, in order.
type plistDict struct {
	Entries []plistEntry `xml:",any"`
}

// plistEntry is one element of a dictionary, a key or a value: its name, and its text.
type plistEntry struct {
	XMLName xml.Name `xml:""`
	Text    string   `xml:",chardata"`
}

// errNoBundleID is returned for an Info.plist that names no bundle identifier.
var errNoBundleID = errors.New("the Info.plist names no bundle identifier")

// bundleID reads the CFBundleIdentifier of the app bundle at bundle from its
// Contents/Info.plist, an XML property list, as every build of the app has; a binary one,
// or a file that isn't one, is an error.
func bundleID(bundle string) (string, error) {
	raw, err := paths.ReadHead(filepath.Join(bundle, "Contents", "Info.plist"), maxInfoPlist)
	if err != nil {
		return "", fmt.Errorf("read the bundle's Info.plist: %w", err)
	}

	var plist infoPlist

	dec := xml.NewDecoder(bytes.NewReader(raw))
	if err = dec.Decode(&plist); err != nil {
		return "", fmt.Errorf("read the bundle's Info.plist: %w", err)
	}

	entries := plist.Dict.Entries
	for i := 0; i+1 < len(entries); i++ {
		if entries[i].XMLName.Local == "key" && entries[i].Text == "CFBundleIdentifier" &&
			entries[i+1].XMLName.Local == "string" {
			return entries[i+1].Text, nil
		}
	}

	return "", errNoBundleID
}
