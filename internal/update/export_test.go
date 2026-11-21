package update

import (
	"context"
	"time"

	"github.com/mrz1836/nilbox/internal/procs"
)

// Tick runs one of the Worker's looks at once.
func (e *Engine) Tick(ctx context.Context) error {
	return e.tick(ctx)
}

// Settle reads the last restart's outcome, cleans up, and loads the cache, as the Worker
// does first, and returns how long to wait before settling again.
func (e *Engine) Settle(ctx context.Context) time.Duration {
	return e.settle(ctx)
}

// CleanUp removes what updates leave behind, as the start does.
func CleanUp(bundle, tempDir string, pid int, now time.Time) []error {
	return cleanUp(bundle, tempDir, pid, now)
}

// HelperReason words why an install failed from the helper's log.
func HelperReason(log []byte) string {
	reason, _ := helperOutcome(log)

	return reason
}

// HelperRetryable reports whether trying the install the helper's log tells of again may
// work.
func HelperRetryable(log []byte) bool {
	_, payload := helperOutcome(log)

	return !payload
}

// Classify says where the bundle at bundle is, the system's Applications folder where it
// always is.
func Classify(bundle, home string) LocationKind {
	return classify(bundle, home, systemApplications)
}

// Locate works out where the bundle is, with writable standing in for the system's answer.
func Locate(bundle, home string, writable func(string) bool) Location {
	return locate(bundle, home, systemApplications, "", writable)
}

// MarkInstalling has the engine say a restart into an update is under way, as restart does
// before it installs.
func (e *Engine) MarkInstalling() {
	e.change(func(st *state) { st.installing = true })
}

// MarkFinishing has the engine say a restart whose quit was held is finishing, as finish
// does once the Installer returns with the app still running, and nothing else: no
// restart is marked installing.
func (e *Engine) MarkFinishing() {
	e.change(func(st *state) { st.finishing = true })
}

// StubProbe has the engine tell whether a process runs with probe, as procs.Probe does.
func (e *Engine) StubProbe(probe func(pid int) procs.Liveness) {
	e.probe = probe
}

// StageMarker is the name of the file a move marks its hidden folder with.
const StageMarker = stageMarker

// BundleID reads the bundle identifier of the app bundle at bundle from its Info.plist.
func BundleID(bundle string) (string, error) {
	return bundleID(bundle)
}

// RenameAfterLook renames from to to once nothing is at to, as a file system that can't
// rename exclusively does.
func RenameAfterLook(from, to string) error {
	return renameAfterLook(from, to)
}
