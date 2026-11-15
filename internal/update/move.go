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
	"strings"
	"time"

	"github.com/mrz1836/nilbox/internal/brand"
	"github.com/mrz1836/nilbox/internal/paths"
	"github.com/mrz1836/nilbox/internal/textutil"
	"github.com/mrz1836/nilbox/internal/uierr"
)

// MoveLimit bounds a move to Applications: the copy, and the check of its signature.
const MoveLimit = 5 * time.Minute

// maxInfoPlist bounds the read of a bundle's Info.plist, which is a few kilobytes.
const maxInfoPlist = 1 << 20

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
)

// Mover moves the app into an Applications folder: the desktop shell's, which copies with
// ditto and checks the copy with codesign, both through execpath, and opens a bundle once
// the app has quit. The engine works out where the copy goes and makes its folder; the
// Mover only runs the programs and the relaunch.
type Mover interface {
	// Copy copies the app's bundle at from into to, an empty folder the engine has just
	// made for it. Its error says why in a few words (uierr.Message).
	Copy(ctx context.Context, from, to string) error

	// Verify checks the code signature of the bundle at bundle, deeply and strictly. Its
	// error says why in a few words.
	Verify(ctx context.Context, bundle string) error

	// OpenAfterQuit quits the app the normal way, which asks first while runs go in the
	// app, and opens the bundle at bundle once it has quit. Asked again, as after a quit
	// the user held back, it still opens one bundle, once: the last it was asked to.
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
// Applications asks: where the app can't update itself, it copies the running bundle to
// the target (the Applications folder every user shares when the user can write to it,
// else the one in home, made when missing) and checks the copy's signature, then has the
// Mover quit the app and open the copy. It never touches the bundle that runs, which the
// user may delete once the copy opens.
//
// A copy of the app at the target already, by its bundle ID, is never replaced: the result
// says so (MoveExists), for Open it, which opens that copy. Anything else there (a link, a
// file, another app, a folder that isn't an app) is ErrTargetTaken. The target's folder is
// made first, so the copy goes into a folder nothing else holds, and a copy that fails or
// doesn't pass its check is removed, that folder alone.
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

	if err = e.claim(target); err != nil {
		return MoveResult{}, err
	}

	// The copy runs to its end, or its limit, whatever happens to the window's call, so it
	// is never left half done.
	work, cancel := context.WithTimeout(context.WithoutCancel(ctx), MoveLimit)
	defer cancel()

	folder := e.tilde(filepath.Dir(target))

	if err = mover.Copy(work, e.bundle, target); err != nil {
		return MoveResult{}, e.discard(ctx, target,
			"Couldn't copy "+brand.Name+" into "+folder+": "+e.plain(uierr.Message(err)), err)
	}

	if err = mover.Verify(work, target); err != nil {
		return MoveResult{}, e.discard(ctx, target,
			"The copy in "+folder+" didn't pass its signature check, so nothing was moved: "+e.plain(uierr.Message(err)), err)
	}

	e.logger.InfoContext(ctx, "moved the app to Applications; it opens from there once this copy quits",
		"from", e.tilde(e.bundle), "to", e.tilde(target))
	mover.OpenAfterQuit(target)

	return MoveResult{Outcome: MoveMoved, Target: e.tilde(target)}, nil
}

// openInApplications opens the copy of the app already in Applications, as Open it asks,
// once its signature checks out: the Mover quits the app the normal way, and opens that copy
// once it has. Nothing is copied or replaced.
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

	switch found, findErr := e.find(ctx, target); {
	case findErr != nil:
		return MoveResult{}, findErr
	case found == foundNothing:
		return MoveResult{}, uierr.New(brand.Name+" isn't in "+e.tilde(filepath.Dir(target))+" any more. Move it there "+
			"instead.", ErrNoCopy)
	case found != foundOurs:
		return MoveResult{}, e.taken(target, found)
	}

	work, cancel := context.WithTimeout(context.WithoutCancel(ctx), MoveLimit)
	defer cancel()

	if err = mover.Verify(work, target); err != nil {
		return MoveResult{}, uierr.New(textutil.Sentence("The copy in "+e.tilde(filepath.Dir(target))+
			" didn't pass its signature check, so it wasn't opened: "+e.plain(uierr.Message(err)))+
			" Move it to the Trash, then move this one there.", fmt.Errorf("%w: %w", ErrMoveFailed, err))
	}

	e.logger.InfoContext(ctx, "opening the copy of the app in Applications once this one quits", "target", e.tilde(target))
	mover.OpenAfterQuit(target)

	return MoveResult{Outcome: MoveOpening, Target: e.tilde(target)}, nil
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
// quit was held), with a Mover attached, and no move under way. It returns what lets go of
// the move. Where a move helps, nothing installs, so a restart under way is never met;
// were one, it goes first: its helper replaces this copy and opens the new version, which
// a move's quit and reopen would race.
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

	return mover, e.moving.Unlock, nil
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

	if running, statErr := os.Stat(e.bundle); statErr == nil && os.SameFile(running, info) {
		return 0, uierr.New(brand.Name+" already runs from Applications.", ErrCantMove)
	}

	return foundOurs, nil
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

// claim makes the target's folder, empty, for the copy: the Applications folder in home
// first, when the move goes there and it is missing. Making it fails when anything is
// there already, so the copy never goes into, and a failed copy's removal never touches,
// anything the move didn't make.
func (e *Engine) claim(target string) error {
	parent := filepath.Dir(target)
	if parent == filepath.Join(e.home, "Applications") {
		if err := os.Mkdir(parent, 0o755); err != nil && !errors.Is(err, fs.ErrExist) { //nolint:gosec // an Applications folder, which every app reads
			return uierr.New("Couldn't make "+e.tilde(parent)+", so nothing was moved.", fmt.Errorf("%w: %w", ErrMoveFailed, err))
		}
	}

	if err := os.Mkdir(target, 0o755); err != nil { //nolint:gosec // an app bundle, which the system reads
		if errors.Is(err, fs.ErrExist) {
			return uierr.New("Something appeared at "+e.tilde(target)+" just now, so nothing was moved. Try again.",
				fmt.Errorf("%w: %w", ErrTargetTaken, err))
		}

		return uierr.New("Couldn't copy "+brand.Name+" into "+e.tilde(parent)+", so nothing was moved.",
			fmt.Errorf("%w: %w", ErrMoveFailed, err))
	}

	return nil
}

// discard removes the copy at target, which claim made, after a copy that failed or didn't
// pass its check, and returns the failure worded as words says, logged whole. A copy that
// can't be removed is named, for the user to remove.
func (e *Engine) discard(ctx context.Context, target, words string, cause error) error {
	e.logger.WarnContext(ctx, "the move to Applications failed; removing the copy", "target", e.tilde(target),
		"error", cause)

	words = textutil.Sentence(words)

	if err := os.RemoveAll(target); err != nil {
		e.logger.WarnContext(ctx, "could not remove the copy a failed move left", "target", e.tilde(target),
			"error", err)

		words += " Move " + e.tilde(target) + " to the Trash before you try again."
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
