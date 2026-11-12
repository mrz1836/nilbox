// Package hooks is the Hooks page's service: the clones in the repository locations with a
// go-pre-commit config, the pre-commit hook in each, and what go-pre-commit does in them
// on the user's click, installing, updating, replacing, or removing the hook, and running
// the checks.
//
// The scan reads, for each clone with a config inside it (a .github/env/*.env file, or
// .github/.env.base; go-pre-commit also searches the folders above, the app doesn't), where
// git runs its hooks (gitread's HookPaths, which leaves core.hooksPath alone), its
// pre-commit hook (missing, go-pre-commit's by its marker, or another tool's, named where
// its text tells), and ENABLE_GO_PRE_COMMIT and the checks' switches from the config files
// themselves. A hooks folder outside both the clone's git folder and its work tree is
// shared, and the scan counts the clones that use it. Whether go-pre-commit's own hook is
// current is a byte comparison with the hook that copy writes, which only `go-pre-commit
// status` makes; it runs in a scratch repository of the app's own that holds the hook's
// bytes, never in a clone, whose .env files go-pre-commit would load into its environment,
// PATH included, before it runs git. The answer is kept per copy and per hook's SHA-256, so
// status runs once for bytes no one judged, never once per clone. Four clones are scanned
// at once, and the scan runs no program a clone names.
//
// Every action and every checks run goes through scriptrun, which holds the clone's lock,
// and its spec's Before reads the clone again under that lock. A shared hooks folder blocks
// every action until the user confirms that folder. An action is judged afterwards from
// the hook file itself, never from the exit status alone: go-pre-commit 1.13 exits 0 even
// when another tool's hook stopped it. A checks run takes git's porcelain v2 status of the
// clone before and after, with every dirty file's SHA-256, so the result names each file
// the checks changed or staged, one that was already dirty included.
//
// go-pre-commit 1.13's output is read as text, from patterns recorded in
// testdata/go-pre-commit-1.13.1; a copy whose Definition.Features has StatusJSON or RunJSON
// is asked for JSON instead.
//
// Nothing here changes a clone, nor runs git that could: go-pre-commit does, on the user's
// click. Nothing here reaches the network.
package hooks

import (
	"errors"
	"slices"
	"time"

	"github.com/mrz1836/nilbox/internal/integrationprefs"
)

const (
	// toolID is go-pre-commit's integration, which a run names as the tool it uses.
	toolID = integrationprefs.GoPreCommit

	// hookPreCommit is the hook the page is about, and the one go-pre-commit installs by
	// default.
	hookPreCommit = "pre-commit"

	// hookCommitMsg is a hook type go-pre-commit installs beside pre-commit when asked.
	hookCommitMsg = "commit-msg"

	// marker is the line go-pre-commit's hook carries, in every version and every hook
	// type.
	marker = "Go Pre-commit Hook"

	// noColor turns go-pre-commit's colors off: status and install force them even when
	// their output isn't a terminal.
	noColor = "--no-color"

	// jsonFlag asks status and run for JSON, from a copy whose Definition.Features has
	// StatusJSON or RunJSON.
	jsonFlag = "--json"

	// backupPrefix starts the name go-pre-commit gives another tool's hook it replaces:
	// pre-commit.backup.<pid>.
	backupPrefix = hookPreCommit + ".backup."

	// FreshFor is how long a scan is served before Repos scans every clone again. A clone
	// an action or a check ran in is scanned again at once.
	FreshFor = 5 * time.Minute

	// ScanTimeout bounds a scan the page's callers share. A scan runs apart from the
	// caller that started it, so a window that asks again (as it does when a run starts or
	// ends) or closes doesn't throw away a scan half done: the caller stops waiting, and
	// the scan goes on and is kept for the next one. The app's quit ends it sooner (Scans).
	ScanTimeout = 5 * time.Minute

	// scanParallel is how many clones are scanned at once.
	scanParallel = 4

	// statusTimeout bounds one go-pre-commit status.
	statusTimeout = 15 * time.Second

	// checksRanKey is the preference the hooks package writes once a checks run has
	// ended, after which the page folds its note about the tools the checks install.
	checksRanKey = "hooks.checksRan"
)

// hookTypes are the hook types go-pre-commit installs, pre-commit first: the others carry
// the same script when the user installed them.
func hookTypes() []string {
	return []string{hookPreCommit, "pre-push", hookCommitMsg, "post-commit"}
}

// Check is one of go-pre-commit's checks.
type Check string

// The checks go-pre-commit 1.13 runs, in its own order.
const (
	CheckEOF        Check = "eof"
	CheckFumpt      Check = "fumpt"
	CheckGitleaks   Check = "gitleaks"
	CheckLint       Check = "lint"
	CheckModTidy    Check = "mod-tidy"
	CheckWhitespace Check = "whitespace"
)

// Checks returns every check, in go-pre-commit's order.
func Checks() []Check {
	return []Check{CheckEOF, CheckFumpt, CheckGitleaks, CheckLint, CheckModTidy, CheckWhitespace}
}

// Valid reports whether c is one of Checks.
func (c Check) Valid() bool {
	return slices.Contains(Checks(), c)
}

// Group is where the page lists a clone with a go-pre-commit config.
type Group string

// The groups.
const (
	// GroupAttention is a clone whose pre-commit hook is missing, outdated, not
	// executable, another tool's, or couldn't be read.
	GroupAttention Group = "attention"

	// GroupReady is a clone with go-pre-commit's current hook, executable.
	GroupReady Group = "ready"

	// GroupOff is a clone whose config turns go-pre-commit off (ENABLE_GO_PRE_COMMIT=false).
	GroupOff Group = "off"
)

// State is what a clone's pre-commit hook is.
type State string

// The states.
const (
	// StateMissing is no pre-commit hook.
	StateMissing State = "missing"

	// StateReady is go-pre-commit's hook, current and executable.
	StateReady State = "ready"

	// StateOutdated is go-pre-commit's hook from another release.
	StateOutdated State = "outdated"

	// StateNotExecutable is go-pre-commit's hook without its executable bit, which git
	// skips.
	StateNotExecutable State = "notExecutable"

	// StateOther is another tool's hook.
	StateOther State = "other"

	// StateUnknown is go-pre-commit's hook when no usable copy of go-pre-commit can say
	// whether it is current.
	StateUnknown State = "unknown"

	// StateUnreadable is a clone whose hooks folder or hook couldn't be read.
	StateUnreadable State = "unreadable"
)

// Action is what go-pre-commit is asked to do with a clone's hook.
type Action string

// The actions.
const (
	// ActionInstall installs the hook where there's none.
	ActionInstall Action = "install"

	// ActionUpdate writes the current hook over an outdated one of go-pre-commit's.
	ActionUpdate Action = "update"

	// ActionReinstall writes go-pre-commit's hook again.
	ActionReinstall Action = "reinstall"

	// ActionReplace moves another tool's hook aside, as pre-commit.backup.<number>, and
	// installs go-pre-commit's.
	ActionReplace Action = "replace"

	// ActionUninstall removes every hook of go-pre-commit's, and puts back the hook it
	// replaced.
	ActionUninstall Action = "uninstall"
)

// Actions returns every action.
func Actions() []Action {
	return []Action{ActionInstall, ActionUpdate, ActionReinstall, ActionReplace, ActionUninstall}
}

// Valid reports whether a is one of Actions.
func (a Action) Valid() bool {
	return slices.Contains(Actions(), a)
}

// Scope is which files a checks run checks.
type Scope string

// The scopes.
const (
	// ScopeAll checks every tracked file.
	ScopeAll Scope = "all"

	// ScopeStaged checks the staged files, as the hook does.
	ScopeStaged Scope = "staged"

	// ScopeOne runs one check on every tracked file.
	ScopeOne Scope = "one"
)

// Scopes returns every scope.
func Scopes() []Scope {
	return []Scope{ScopeAll, ScopeStaged, ScopeOne}
}

// Valid reports whether s is one of Scopes.
func (s Scope) Valid() bool {
	return slices.Contains(Scopes(), s)
}

var (
	// ErrNotSetUp is returned by an action or a run on a Service built without the runner,
	// or a clone with no go-pre-commit config.
	ErrNotSetUp = errors.New("hooks are not set up")

	// ErrNoTool is returned when no usable copy of go-pre-commit is found, or the user
	// turned it off.
	ErrNoTool = errors.New("go-pre-commit can't be used")

	// ErrNoClone is returned for a folder that isn't a clone in the repository locations.
	ErrNoClone = errors.New("not a clone in the repository locations")

	// ErrInvalidRequest is returned for an action, a scope, a check, or a place to run
	// that isn't one.
	ErrInvalidRequest = errors.New("invalid hooks request")

	// ErrSharedFolder is returned for an action in a clone whose hooks folder other
	// repositories share, unless the request confirms that folder.
	ErrSharedFolder = errors.New("the hooks folder is shared")

	// ErrNoResult is returned by ActionResult and ChecksResult for a run of another kind,
	// one still going, or one that ended without a result.
	ErrNoResult = errors.New("the run has no result to show")
)
