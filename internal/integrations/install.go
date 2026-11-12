package integrations

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/mrz1836/nilbox/internal/brand"
	"github.com/mrz1836/nilbox/internal/clonelock"
	"github.com/mrz1836/nilbox/internal/event"
	"github.com/mrz1836/nilbox/internal/integrationprefs"
	"github.com/mrz1836/nilbox/internal/integrations/internal/queries"
	"github.com/mrz1836/nilbox/internal/paths"
	"github.com/mrz1836/nilbox/internal/scriptrun"
	"github.com/mrz1836/nilbox/internal/toolinstall"
	"github.com/mrz1836/nilbox/internal/uierr"
)

const (
	// latestFreshFor is how old the latest release may be before an install looks again.
	latestFreshFor = time.Hour

	// smokeTimeout bounds one run of a downloaded program's --version.
	smokeTimeout = 30 * time.Second

	// progressEvery is how often a download's progress is told to the window.
	progressEvery = 250 * time.Millisecond

	// leftoverAge is how old an install's leftovers are before cleanup removes them.
	leftoverAge = 24 * time.Hour
)

// The install plan's actions.
const (
	ActionInstall   = "install"
	ActionUpdate    = "update"
	ActionReinstall = "reinstall"
)

var (
	// ErrSignedOut is returned by Releases when no one is signed in to GitHub. Core's
	// adapter returns it for the GitHub CLI's own signed-out errors.
	ErrSignedOut = errors.New("not signed in to GitHub")

	// ErrInstallRefused is returned for an install the plan doesn't allow, with the plan's
	// reason as its message.
	ErrInstallRefused = errors.New("the install was refused")

	// ErrNotACopy is returned by Trash and Reveal for a path that isn't a copy of the tool
	// the app found.
	ErrNotACopy = errors.New("not a copy of the tool the app found")
)

// InstallPlan says what installing the tool would do, for the card's button and the
// install dialog, or why it can't.
type InstallPlan struct {
	// Action is install, update, or reinstall.
	Action string `json:"action"`

	// Version and Archive are the latest release's version and its archive for this Mac,
	// "" until the release is known.
	Version string `json:"version"`
	Archive string `json:"archive"`

	// TargetDir and Target are where the program would go, with ~.
	TargetDir string `json:"targetDir"`
	Target    string `json:"target"`

	// Replaces is the version of the copy it would replace, and ReplacesPath where, or
	// "" when it replaces none. Foreign reports a copy the app didn't install, which the
	// user confirms replacing first.
	Replaces     string `json:"replaces"`
	ReplacesPath string `json:"replacesPath"`
	Foreign      bool   `json:"foreign"`

	// Blocked says why it can't be done, as a sentence, or "" when it can; BrewCommand is
	// the command that updates a Homebrew copy.
	Blocked     string `json:"blocked"`
	BrewCommand string `json:"brewCommand"`

	// Updater, for an update of a copy that updates itself, is the tool's own update
	// command, as the window shows it ("~/.local/bin/magex update:install"), which the
	// update runs in place of the app's download. It is "" when the app installs the
	// release itself.
	Updater string `json:"updater"`
}

// InstallProgress is how far an install of the tool has got.
type InstallProgress struct {
	// Step is downloading, verifying, trying, or installing, updating while the tool's own
	// updater runs, or "" when none runs.
	Step  string `json:"step"`
	Done  int64  `json:"done"`
	Total int64  `json:"total"`
}

// InstallResult is what an install did, and the tools as they are after it.
type InstallResult struct {
	// Message says what was done, as in "Installed go-pre-commit 1.13.1 in ~/.local/bin";
	// it is "" for an update the tool runs itself, whose run says so as it ends.
	Message string `json:"message"`

	// Run is the run of the tool's own updater an update started, which the window shows,
	// or 0 when the app installed the release itself.
	Run int64 `json:"run"`

	Tools Tools `json:"tools"`
}

// Install installs the latest release of a tool that has no copy, or whose copy it
// replaces, as its plan says, and returns what it did.
func (s *Service) Install(ctx context.Context, id integrationprefs.ID) (InstallResult, error) {
	return s.install(ctx, id)
}

// Update updates the tool's copy to its latest release, as its plan says: a copy that
// updates itself runs its own updater, as a run whose ID the result holds, and any other
// gets the latest release installed over it.
func (s *Service) Update(ctx context.Context, id integrationprefs.ID) (InstallResult, error) {
	return s.install(ctx, id)
}

// Reinstall installs the latest release again, as over a copy that won't run.
func (s *Service) Reinstall(ctx context.Context, id integrationprefs.ID) (InstallResult, error) {
	return s.install(ctx, id)
}

// install runs one install of a tool. A copy that updates itself is handed to its own
// updater (updateItself). Any other gets the latest release, refreshed first when it is
// more than an hour old, verified and placed by toolinstall where the plan says, then
// recorded, and the tools looked for again. Installs run one at a time, and none while
// the tool updates itself.
func (s *Service) install(ctx context.Context, id integrationprefs.ID) (InstallResult, error) {
	def, ok := s.definition(id)
	if !ok {
		return InstallResult{}, fmt.Errorf("%w: %q", integrationprefs.ErrUnknownIntegration, id)
	}

	// Until this run's first detection ends, the window shows the last one kept, whose
	// plan this run hasn't checked: an install waits for what is there now.
	if !s.hasDetected() {
		return InstallResult{}, uierr.New(brand.Name+" is still looking for the tools. Try again in a moment.",
			ErrInstallRefused)
	}

	self, ok, err := s.claimUpdate(ctx, def)

	switch {
	case err != nil:
		return InstallResult{}, err
	case ok:
		return s.updateItself(ctx, self)
	case s.installer == nil:
		return InstallResult{}, uierr.New(brand.Name+" can't install tools here.", ErrInstallRefused)
	}

	s.installMu.Lock()
	defer s.installMu.Unlock()

	if err = s.updatingError(def); err != nil {
		return InstallResult{}, err
	}

	s.checkUpdatesLogged(ctx, latestFreshFor) // a failure keeps the last answer, which the plan then says is missing

	plan, latest, _, err := s.planFor(ctx, def)
	if err != nil {
		return InstallResult{}, err
	}

	release, err := s.admit(def, plan)
	if err != nil {
		return InstallResult{}, err
	}

	placed, err := s.installer.Install(ctx, s.request(def, latest, plan))

	release()
	s.setProgress(def.ID, InstallProgress{})

	if err != nil {
		return InstallResult{}, s.installError(def, plan, err)
	}

	return s.finish(ctx, def, plan, placed)
}

// finish records what an install placed, with the version it replaced, looks for the
// tools again, and says what was done.
func (s *Service) finish(ctx context.Context, def Definition, plan InstallPlan, placed toolinstall.Placed,
) (InstallResult, error) {
	replaced := ""
	if placed.Replaced {
		replaced = plan.Replaces
	}

	s.record(ctx, def, placed, replaced)

	// The program is in place either way: a detection that fails is logged, and the list
	// shows the last one until the next.
	if err := s.detect(ctx); err != nil {
		s.logger.WarnContext(ctx, "installed a tool, but couldn't look for the tools again", "tool", def.Name, "error", err)
	}

	tools, err := s.List(ctx)
	if err != nil {
		return InstallResult{}, err
	}

	return InstallResult{Message: installMessage(def, plan, placed), Tools: tools}, nil
}

// hasDetected reports whether a detection has ended since the app started.
func (s *Service) hasDetected() bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.detected != nil
}

// admit refuses a plan that can't be carried out, then takes the lock of the folder it
// places the program in, which a tool's own update holds while it runs there, one from
// before the app restarted included, and returns its release. A folder another job holds
// refuses the install at once, in the lock's words.
func (s *Service) admit(def Definition, plan InstallPlan) (func(), error) {
	if err := refusal(def, plan); err != nil {
		return nil, err
	}

	dir, _ := paths.ExpandHome(plan.TargetDir, s.home)

	release, held, ok := s.locks.TryLock(dir, clonelock.Holder{
		Kind: clonelock.KindInstall, Repo: plan.TargetDir, Title: "Install " + def.Binary,
	})
	if !ok {
		busy := &clonelock.BusyError{Holder: held}

		return nil, uierr.New(busy.Error(), fmt.Errorf("%w: %w", ErrInstallRefused, busy))
	}

	return release, nil
}

// refusal returns why a plan can't be carried out, worded, or nil.
func refusal(def Definition, plan InstallPlan) error {
	switch {
	case plan.Blocked != "":
		return uierr.New(plan.Blocked, ErrInstallRefused)
	case plan.Version == "":
		return uierr.New(fmt.Sprintf("%s couldn't find %s's latest release on GitHub.", brand.Name, def.Name),
			ErrInstallRefused)
	default:
		return nil
	}
}

// definition returns the tool id names.
func (s *Service) definition(id integrationprefs.ID) (Definition, bool) {
	for _, def := range s.defs {
		if def.ID == id {
			return def, true
		}
	}

	return Definition{}, false
}

// planFor works out a tool's plan from its last detection and latest release, and returns
// them with it.
func (s *Service) planFor(ctx context.Context, def Definition) (InstallPlan, latestRecord, detection, error) {
	stored, err := s.prefs.GetMany(ctx, latestKey(def.ID))
	if err != nil {
		return InstallPlan{}, latestRecord{}, detection{}, fmt.Errorf("read the latest release: %w", err)
	}

	latest := decodeLatest(stored[latestKey(def.ID)])

	s.mu.Lock()
	d := s.detected[string(def.ID)]
	s.mu.Unlock()

	return s.plan(def, d, latest, stateOf(def, d, latest)), latest, d, nil
}

// plan works out what installing would do for a tool in state, or why it can't.
func (s *Service) plan(def Definition, d detection, latest latestRecord, state State) InstallPlan {
	out := InstallPlan{Action: ActionReinstall, Version: latest.Version}

	switch state {
	case StateMissing:
		out.Action = ActionInstall
	case StateUpdate, StateTooOld:
		out.Action = ActionUpdate
	case StateFound, StateWontRun:
	}

	if latest.Version != "" {
		out.Archive = def.Asset(latest.Version, s.goos, s.goarch)
	}

	dir := s.localBin()
	if d.has {
		dir = s.placement(def, d, &out)
	}

	if dir == s.localBin() && out.Blocked == "" && out.ReplacesPath == "" {
		s.alreadyThere(def, d, &out)
	}

	out.TargetDir = paths.Tilde(dir, s.home)
	out.Target = paths.Tilde(filepath.Join(dir, def.Binary), s.home)

	if out.Blocked == "" {
		out.Blocked = s.blocked(def, latest, out)
	}

	return out
}

// placement works out where installing over the copy in use, d's, puts the program, and
// what the plan says about that copy: it updates itself in place (as the detection worked
// out), is replaced in place, is left alone for a new copy in ~/.local/bin, or isn't
// touched at all.
func (s *Service) placement(def Definition, d detection, out *InstallPlan) string {
	c := d.selected

	// Homebrew's copy is a link into its Cellar, so it is told apart first.
	switch {
	case c.Source == SourceHomebrew:
		out.BrewCommand = "brew upgrade " + def.Binary
		out.Blocked = "Homebrew manages this copy. Update it with " + out.BrewCommand + "."
	case c.Link:
		out.Blocked = fmt.Sprintf("%s is a link to %s, so %s won't replace it.", c.DisplayPath, c.Target, brand.Name)
	case out.Action == ActionUpdate && d.selfUpdates:
		// Its own updater replaces it, so the user isn't asked to let the app replace a copy
		// it didn't install.
		out.Updater = scriptrun.DisplayCommand(append([]string{c.Path}, def.UpdateArgs...), s.home)
		out.Replaces, out.ReplacesPath = c.Version.Raw, c.DisplayPath

		return filepath.Dir(c.Path)
	case c.Source == SourceGoInstall || !writable(filepath.Dir(c.Path)):
		// A new copy goes in ~/.local/bin; this one stays, and the card says when it comes
		// first on the PATH.
	default:
		out.Replaces, out.ReplacesPath = c.Version.Raw, c.DisplayPath
		out.Foreign = c.Source != SourceApp

		return filepath.Dir(c.Path)
	}

	return s.localBin()
}

// alreadyThere fills in the copy a new copy in ~/.local/bin would replace, when one is
// there that isn't the copy in use (which stays where it is): a link there is refused,
// and a copy the app didn't install is replaced only once the user says so.
func (s *Service) alreadyThere(def Definition, d detection, out *InstallPlan) {
	target := filepath.Join(s.localBin(), def.Binary)

	info, err := os.Lstat(target)
	if err != nil {
		return
	}

	shown := paths.Tilde(target, s.home)

	if info.Mode()&os.ModeSymlink != 0 {
		out.Blocked = fmt.Sprintf("%s is a link, so %s won't replace it.", shown, brand.Name)

		return
	}

	out.ReplacesPath, out.Foreign = shown, true

	for _, c := range d.others {
		if paths.Same(c.Path, target) {
			out.Replaces, out.Foreign = c.Version.Raw, c.Source != SourceApp
		}
	}
}

// blocked says why an install can't run now, or "": a run uses the tool, no one is signed
// in to find the release, or the release has no build for this Mac. A tool updating itself
// finds its own release and picks its own archive, so neither of those holds it back.
func (s *Service) blocked(def Definition, latest latestRecord, plan InstallPlan) string {
	switch {
	case s.inUse(def.ID):
		return inUseText(def)
	case plan.Updater != "":
		return ""
	case latest.Version == "" && s.isSignedOut():
		return "Sign in to GitHub first, so " + brand.Name + " can find the latest release."
	case latest.Version == "":
		return "" // not looked up yet: an install looks it up first
	case !hasAsset(latest, plan.Archive):
		return fmt.Sprintf("%s %s has no build for this Mac (%s/%s).", def.Name, latest.Version, s.goos, s.goarch)
	default:
		return ""
	}
}

// isSignedOut reports that the last update check found no one signed in to GitHub.
func (s *Service) isSignedOut() bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.signedOut
}

// hasAsset reports whether the release lists a file named name.
func hasAsset(latest latestRecord, name string) bool {
	for _, a := range latest.Assets {
		if a.Name == name {
			return true
		}
	}

	return false
}

// localBin is the folder new copies go in: ~/.local/bin.
func (s *Service) localBin() string {
	return filepath.Join(s.home, ".local", "bin")
}

// request is the toolinstall request for a plan.
func (s *Service) request(def Definition, latest latestRecord, plan InstallPlan) toolinstall.Request {
	assets := make([]toolinstall.Asset, 0, len(latest.Assets))
	for _, a := range latest.Assets {
		assets = append(assets, toolinstall.Asset{Name: a.Name, URL: a.URL, Size: a.Size, Digest: a.Digest})
	}

	dir, _ := paths.ExpandHome(plan.TargetDir, s.home)

	return toolinstall.Request{
		Tool: string(def.ID), Binary: def.Binary, Version: latest.Version, Archive: plan.Archive,
		Checksums: def.Checksums(latest.Version), Assets: assets, Dir: dir,
		Smoke:    func(ctx context.Context, path string) error { return s.smoke(ctx, def, latest.Version, path) },
		Progress: s.progressFor(def.ID),
	}
}

// smoke runs the program at path for its version, which must be want.
func (s *Service) smoke(ctx context.Context, def Definition, want, path string) error {
	ctx, cancel := context.WithTimeout(ctx, smokeTimeout)
	defer cancel()

	env, _ := s.env.Env()

	stdout, stderr, err := s.run(ctx, path, def.VersionArgs, def.RunEnv(env, integrationprefs.RunsInApp))

	// The context ending kills the program too, so it is told apart from macOS refusing it.
	switch {
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		return fmt.Errorf("it didn't answer within %s: %w", smokeTimeout, ctx.Err())
	case ctx.Err() != nil:
		return ctx.Err() //nolint:wrapcheck // the install's own context ended
	case killed(err):
		return errRefusedToRun
	case err != nil:
		return fmt.Errorf("%s: %w", firstLine(stderr, nil, "it failed"), err)
	}

	v, err := def.ParseVersion(stdout)
	if err != nil {
		return fmt.Errorf("it printed no version: %w", err)
	}

	if v.Raw != want {
		return fmt.Errorf("%w: it says %s, not %s", errWrongVersion, v.Raw, want)
	}

	return nil
}

var (
	// errRefusedToRun is a downloaded program macOS killed on start.
	errRefusedToRun = errors.New("macOS refused to run it")

	// errWrongVersion is a downloaded program that reports another version.
	errWrongVersion = errors.New("it reports another version")
)

// progressFor returns the progress callback for one tool's install, which tells the
// window at each step, and during the download at most every quarter second.
func (s *Service) progressFor(id integrationprefs.ID) func(toolinstall.Progress) {
	var (
		last     time.Time
		lastStep toolinstall.Step
	)

	return func(p toolinstall.Progress) {
		now := time.Now()
		if p.Step == lastStep && now.Sub(last) < progressEvery && p.Done < p.Total {
			return
		}

		last, lastStep = now, p.Step

		s.setProgress(id, InstallProgress{Step: string(p.Step), Done: p.Done, Total: p.Total})
	}
}

// setProgress keeps a tool's install progress, zero when none runs, and tells the window.
func (s *Service) setProgress(id integrationprefs.ID, p InstallProgress) {
	s.mu.Lock()
	if s.installing == nil {
		s.installing = map[integrationprefs.ID]InstallProgress{}
	}

	if p.Step == "" {
		delete(s.installing, id)
	} else {
		s.installing[id] = p
	}
	s.mu.Unlock()

	s.events.Emit(event.ToolsUpdated{})
}

// record keeps what an install placed, so its copy reads as installed by the app. A
// failure is logged: the install itself is done.
func (s *Service) record(ctx context.Context, def Definition, placed toolinstall.Placed, replaced string) {
	if s.queries == nil {
		return
	}

	err := s.queries.RecordInstall(ctx, queries.RecordInstallParams{
		Tool: string(def.ID), Version: placed.Version, Path: placed.Path, Sha256: placed.SHA256, Archive: placed.Archive,
		Replaced: replaced, InstalledAt: s.clock.Now().UTC(),
	})
	if err != nil {
		s.logger.WarnContext(ctx, "couldn't record an install", "tool", def.Name, "error", err)
	}
}

// installMessage says what an install did, naming the folder as the plan did.
func installMessage(def Definition, plan InstallPlan, placed toolinstall.Placed) string {
	switch {
	case plan.Replaces != "" && plan.Replaces != placed.Version:
		return fmt.Sprintf("Updated %s to %s", def.Binary, placed.Version)
	case plan.ReplacesPath != "":
		return fmt.Sprintf("Reinstalled %s %s", def.Name, placed.Version)
	default:
		return fmt.Sprintf("Installed %s %s in %s", def.Name, placed.Version, plan.TargetDir)
	}
}

// nothingChanged ends a sentence about a refused install.
const nothingChanged = " Nothing on your Mac changed."

// installError words a failed install for the user, keeping the cause.
func (s *Service) installError(def Definition, plan InstallPlan, err error) error {
	unchanged := fmt.Sprintf(", so %s didn't install it.", brand.Name) + nothingChanged
	rolledBack := fmt.Sprintf("The new %s didn't run, so %s took it away again.", def.Binary, brand.Name)

	if plan.ReplacesPath != "" {
		old := strings.TrimSpace(def.Binary + " " + plan.Replaces)
		rolledBack = fmt.Sprintf("The new %s didn't run, so %s put %s back.", def.Binary, brand.Name, old)
	}

	// In order: the first cause in err's chain words it. errRefusedToRun comes before
	// toolinstall.ErrSmoke, which wraps it.
	wordings := []struct {
		cause error
		msg   string
	}{
		{context.Canceled, "The install stopped before it finished." + nothingChanged},
		{toolinstall.ErrChecksum, "The download didn't match the release's checksum" + unchanged},
		{toolinstall.ErrShortDownload, "The download was cut short" + unchanged},
		{toolinstall.ErrNoBuild, fmt.Sprintf("%s %s has no build for this Mac (%s/%s).", def.Name, plan.Version, s.goos, s.goarch)},
		{toolinstall.ErrNoChecksums, fmt.Sprintf("%s %s has no checksums file", def.Name, plan.Version) + unchanged},
		{toolinstall.ErrArchive, "The download wasn't the archive " + brand.Name + " expected" + unchanged},
		{errRefusedToRun, fmt.Sprintf("macOS refused to run the downloaded %s", def.Binary) + unchanged},
		{toolinstall.ErrSmoke, fmt.Sprintf("The downloaded %s didn't run", def.Binary) + unchanged},
		{toolinstall.ErrRollbackFailed, fmt.Sprintf("The new %s didn't run, and %s couldn't put the old copy back: %s",
			def.Binary, brand.Name, joinedCause(err, toolinstall.ErrRollbackFailed))},
		{toolinstall.ErrRolledBack, rolledBack},
		{toolinstall.ErrLinkTarget, fmt.Sprintf("%s is a link, so %s won't replace it.", plan.Target, brand.Name)},
		{toolinstall.ErrWrite, fmt.Sprintf("%s can't write to %s: %s. Nothing changed.", brand.Name, plan.TargetDir,
			joinedCause(err, toolinstall.ErrWrite))},
	}

	for _, w := range wordings {
		if errors.Is(err, w.cause) {
			return uierr.New(w.msg, err)
		}
	}

	return uierr.New(fmt.Sprintf("%s couldn't download %s %s.", brand.Name, def.Name, plan.Version)+nothingChanged, err)
}

// joinedCause returns the text of what err joins to sentinel, as fmt.Errorf("%w: %w",
// sentinel, cause) does, or err's own text.
func joinedCause(err, sentinel error) string {
	if joined, ok := errors.AsType[joinedError](err); ok {
		var parts []string

		for _, e := range joined.Unwrap() {
			if !errors.Is(e, sentinel) {
				parts = append(parts, e.Error())
			}
		}

		if len(parts) > 0 {
			return strings.Join(parts, ", ")
		}
	}

	return err.Error()
}

// joinedError is an error that joins others, as errors.Join and fmt.Errorf with several
// %w verbs make.
type joinedError interface {
	error
	Unwrap() []error
}

// hashOf returns the SHA-256 of the file at path in hex, from the cache while its size,
// time, and inode haven't changed.
func (s *Service) hashOf(path string) (string, bool) {
	st := statOf(path)
	if st.Size == 0 {
		return "", false
	}

	s.mu.Lock()
	cached, ok := s.hashes[path]
	s.mu.Unlock()

	if ok && cached.stat == st {
		return cached.sum, true
	}

	f, err := os.Open(path) //nolint:gosec // a copy of a tool detection found
	if err != nil {
		return "", false
	}

	defer func() { _ = f.Close() }()

	h := sha256.New()
	if _, err = io.Copy(h, f); err != nil {
		return "", false
	}

	sum := hex.EncodeToString(h.Sum(nil))

	s.mu.Lock()
	if s.hashes == nil {
		s.hashes = map[string]cachedHash{}
	}

	s.hashes[path] = cachedHash{stat: st, sum: sum}
	s.mu.Unlock()

	return sum, true
}

// cachedHash is a file's hash, as of its stat.
type cachedHash struct {
	stat fileStat
	sum  string
}

// markInstalled marks each copy the app installed, by the newest install recorded at its
// path, whose hash must still match the file's.
func (s *Service) markInstalled(ctx context.Context, d *detection) {
	if s.queries == nil || !d.has {
		return
	}

	mark := func(c *Copy) {
		// An install is recorded where it placed the program, in the folder its target's
		// folder resolves to.
		row, err := s.queries.LatestInstallAt(ctx, paths.Resolved(c.Path))
		if errors.Is(err, sql.ErrNoRows) || err != nil {
			return
		}

		if sum, ok := s.hashOf(c.Path); ok && sum == row.Sha256 {
			c.Source = SourceApp
			c.InstalledAt = stamp(row.InstalledAt)
			c.Archive = row.Archive
		}
	}

	mark(&d.selected)

	for i := range d.others {
		mark(&d.others[i])
	}
}

// cleanup removes what installs left behind: old download folders, and leftovers beside
// ~/.local/bin's copies and every copy's folder.
func (s *Service) cleanup() {
	if s.installer == nil {
		return
	}

	// Never beside an install: its -old link keeps the old copy's time, which looks old.
	s.installMu.Lock()
	defer s.installMu.Unlock()

	dirs := []string{s.localBin()}

	s.mu.Lock()
	for _, d := range s.detected {
		if d.has {
			dirs = append(dirs, filepath.Dir(d.selected.Path))
		}
	}
	s.mu.Unlock()

	s.installer.Cleanup(dirs, s.clock.Now(), leftoverAge)
}
