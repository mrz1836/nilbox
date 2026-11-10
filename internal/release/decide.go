package release

import (
	"cmp"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"

	"github.com/mrz1836/nilbox/internal/brand"
	"github.com/mrz1836/nilbox/internal/gitread"
	"github.com/mrz1836/nilbox/internal/integrationprefs"
	"github.com/mrz1836/nilbox/internal/integrations"
	"github.com/mrz1836/nilbox/internal/repo"
)

// CheckID names one of the release sheet's checks (§3.4.3).
type CheckID string

// The checks, in the order the sheet lists them.
const (
	// CheckTool is magex: there, on, and new enough.
	CheckTool CheckID = "tool"

	// CheckRepo is the repository on GitHub: read, shown to the user, not archived, with
	// commits.
	CheckRepo CheckID = "repo"

	// CheckOrigin is the clone: git reads it, and its origin is the repository.
	CheckOrigin CheckID = "origin"

	// CheckAccess is the user's access: enough to push a tag.
	CheckAccess CheckID = "access"

	// CheckInProgress is git in the middle of something: a rebase, a merge, a
	// cherry-pick, or a revert.
	CheckInProgress CheckID = "inProgress"

	// CheckDirty is uncommitted changes, which magex refuses.
	CheckDirty CheckID = "dirty"

	// CheckDetached is a clone on a bare commit rather than a branch.
	CheckDetached CheckID = "detached"

	// CheckWhere is the branch the clone is on, and whether git can switch to the
	// release's.
	CheckWhere CheckID = "where"

	// CheckBranch is the release's branch: one Release offers, and in the clone.
	CheckBranch CheckID = "branch"

	// CheckUpstream is the branch following origin's, which magex's pull needs.
	CheckUpstream CheckID = "upstream"

	// CheckUnpushed is commits on the branch that origin's lacks.
	CheckUnpushed CheckID = "unpushed"

	// CheckLocalOnly is a version tag only in the clone, which magex would skip.
	CheckLocalOnly CheckID = "localOnly"

	// CheckBehind is origin's branch having commits the clone's lacks.
	CheckBehind CheckID = "behind"

	// CheckReach is origin answering with the user's git credentials.
	CheckReach CheckID = "reach"

	// CheckNothingNew is a version already on origin's newest commit.
	CheckNothingNew CheckID = "nothingNew"

	// CheckChecks is the checks on the default branch's newest commit.
	CheckChecks CheckID = "checks"

	// CheckBusy is another job holding the clone.
	CheckBusy CheckID = "busy"

	// CheckSigning is git signing the tag, and whether it can without a terminal.
	CheckSigning CheckID = "signing"

	// CheckShell is the login shell's environment, read or not.
	CheckShell CheckID = "shell"

	// CheckHooks is the clone's hooks that a release runs.
	CheckHooks CheckID = "hooks"
)

// CheckState is how a check came out: ✓ and ℹ only inform, ⚠ warns, and ✗ blocks.
type CheckState string

// The states a check can be in.
const (
	CheckOK    CheckState = "ok"
	CheckInfo  CheckState = "info"
	CheckWarn  CheckState = "warn"
	CheckBlock CheckState = "block"
)

// Fix is an action the sheet offers beside a check.
type Fix string

// The fixes a check offers.
const (
	// FixOpenSettings opens Settings → Integrations.
	FixOpenSettings Fix = "openSettings"

	// FixUpdate updates magex.
	FixUpdate Fix = "update"

	// FixOpenTerminal opens the user's terminal in the clone, and runs nothing there.
	FixOpenTerminal Fix = "openTerminal"

	// FixCheckAgain gathers the facts again.
	FixCheckAgain Fix = "checkAgain"

	// FixView opens Check.Link.
	FixView Fix = "view"

	// FixWatch opens the run that holds the clone, Check.RunID.
	FixWatch Fix = "watch"
)

// Check is one line of the sheet's checks.
type Check struct {
	ID    CheckID    `json:"id"`
	State CheckState `json:"state"`
	Text  string     `json:"text"`

	// Fixes are the actions beside it, and Commands the commands it offers to copy, which
	// the app never runs.
	Fixes    []Fix    `json:"fixes"`
	Commands []string `json:"commands"`

	// Link is what FixView opens, and RunID the run FixWatch opens.
	Link  string `json:"link"`
	RunID int64  `json:"runId"`
}

// BranchChoice is a branch the sheet's menu offers (D10).
type BranchChoice struct {
	Name    string `json:"name"`
	Default bool   `json:"default"`
}

// BumpOffer is one of the sheet's cards: the version a bump makes, and whether it is
// offered; when it isn't, Note may say why, with Fixes.
type BumpOffer struct {
	Bump    Bump   `json:"bump"`
	Version string `json:"version"`
	Offered bool   `json:"offered"`
	Note    string `json:"note"`
	Fixes   []Fix  `json:"fixes"`
}

// MajorOffer is the careful form a major takes (§3.4.4).
type MajorOffer struct {
	// Version is the major's version, and FirstStable reports v1.0.0, from v0.x.
	Version     string `json:"version"`
	FirstStable bool   `json:"firstStable"`

	// ModulePath is the clone's go.mod module, or "" when it isn't a Go module. When Go
	// couldn't install Version from it, ModuleWant is the path it needs, and ModuleText
	// says so; only Choice.AllowModulePath gets past it.
	ModulePath string `json:"modulePath"`
	ModuleWant string `json:"moduleWant"`
	ModuleText string `json:"moduleText"`

	// Confirmed reports the user typed Version.
	Confirmed bool `json:"confirmed"`
}

// Offer is what Decide works out: the sheet's branches, checks, and cards, whether the
// release can start, and what it would run.
type Offer struct {
	// Repo is the repository, "owner/name".
	Repo string `json:"repo"`

	// Branch is the release's branch, Branches those the menu offers, and BranchNote what
	// the sheet says of a branch that isn't the default.
	Branch     string         `json:"branch"`
	Branches   []BranchChoice `json:"branches"`
	BranchNote string         `json:"branchNote"`

	// Current is the branch the clone is on, "" when detached, and Head the commit
	// origin's branch is at, which the release tags.
	Current string `json:"current"`
	Head    string `json:"head"`

	// Checks are the sheet's checks, in order.
	Checks []Check `json:"checks"`

	// Bump is the chosen bump and Version the version it makes; Bumps are every card,
	// and Major the careful form a major takes.
	Bump    Bump        `json:"bump"`
	Version string      `json:"version"`
	Bumps   []BumpOffer `json:"bumps"`
	Major   MajorOffer  `json:"major"`

	// Ready reports the release can start, and Anyway that it would despite a warning.
	Ready  bool `json:"ready"`
	Anyway bool `json:"anyway"`

	// Button is the button's label, and Sentence what will happen, said above it.
	Button   string `json:"button"`
	Sentence string `json:"sentence"`

	// Argv is what the release runs, magex's path first, or empty without a magex to run.
	Argv []string `json:"argv"`
}

const (
	// magexName is the program Release runs, as the sheet names it.
	magexName = "magex"

	// remoteOrigin is the remote magex releases to.
	remoteOrigin = "origin"

	// shortOIDLen is how much of a commit's ID the sheet shows.
	shortOIDLen = 7

	// changesListed is how many uncommitted files the dirty check names.
	changesListed = 5

	// hookTime is the time magex gives each git command, any hook included.
	hookTime = "magex's 30 seconds"
)

// Decide works the release's offer out from f, and nothing else: every check, every
// bump's version and whether it is offered, whether the release can start, the argv, and
// the sentence. Prepare runs it to show the sheet, and Start runs it again on fresh facts
// before anything runs.
func Decide(f Facts) Offer {
	d := newDecider(f)
	checks := d.checks()
	bumps := d.bumps()
	major := d.major()
	anyway := hasState(checks, CheckWarn)

	return Offer{
		Repo: f.Repo.Name.String(), Branch: d.branch, Branches: d.branches(), BranchNote: d.branchNote(),
		Current: f.Clone.Branch, Head: d.head, Checks: checks, Bump: d.bump, Version: d.versionText(),
		Bumps: bumps, Major: major, Ready: d.ready(checks, bumps, major), Anyway: anyway,
		Button: d.button(anyway), Sentence: d.sentence(), Argv: d.argv(major),
	}
}

// decider holds what Decide works from: the facts, and what follows from them alone.
type decider struct {
	f Facts

	// branch is the release's branch, and head the commit origin's is at.
	branch string
	head   string

	// tags are every version tag, the clone's and origin's; bump is the chosen bump, and
	// version what it makes over tags.
	tags    []string
	bump    Bump
	version Version

	// terminal reports a run going to the user's terminal.
	terminal bool
}

// newDecider works out what follows from f alone.
func newDecider(f Facts) decider {
	d := decider{
		f: f, branch: f.branch(), bump: cmp.Or(f.Choice.Bump, BumpPatch),
		terminal: f.Choice.RunsIn == integrationprefs.RunsInTerminal,
	}

	d.head = f.remoteHead(d.branch)
	d.tags = append(slices.Clone(f.Clone.LocalTags), slices.Sorted(maps.Keys(f.Clone.RemoteTags))...)
	d.version = Next(d.tags, d.bump)

	return d
}

// checks works out the sheet's checks, in order. A check that reads the clone is left out
// when git couldn't read it, which CheckOrigin says.
func (d decider) checks() []Check {
	steps := []struct {
		check func() (Check, bool)
		clone bool
	}{
		{check: d.toolCheck},
		{check: d.repoCheck},
		{check: d.originCheck},
		{check: d.accessCheck},
		{check: d.inProgressCheck, clone: true},
		{check: d.dirtyCheck, clone: true},
		{check: d.detachedCheck, clone: true},
		{check: d.whereCheck, clone: true},
		{check: d.branchCheck, clone: true},
		{check: d.upstreamCheck, clone: true},
		{check: d.unpushedCheck, clone: true},
		{check: d.localOnlyCheck, clone: true},
		{check: d.behindCheck, clone: true},
		{check: d.reachCheck},
		{check: d.nothingNewCheck},
		{check: d.ciCheck},
		{check: d.busyCheck},
		{check: d.signingCheck, clone: true},
		{check: d.shellCheck},
		{check: d.hooksCheck, clone: true},
	}

	out := make([]Check, 0, len(steps))

	for _, s := range steps {
		if s.clone && d.f.Clone.Error != "" {
			continue
		}

		if c, ok := s.check(); ok {
			out = append(out, c)
		}
	}

	return out
}

// toolCheck: magex is there, on, and new enough.
func (d decider) toolCheck() (Check, bool) {
	t := d.f.Tool

	switch {
	case t.Usable():
		return Check{}, false
	case !t.Off && t.Path != "" && t.State == integrations.StateTooOld:
		text := fmt.Sprintf("%s %s is older than %s, which Release needs", magexName, t.Version, t.MinVersion)

		return checkBlock(CheckTool, text, FixUpdate), true
	default:
		return checkBlock(CheckTool, magexName+" isn't available", FixOpenSettings), true
	}
}

// repoCheck: GitHub was read, shows the repository, which isn't archived and has commits.
func (d decider) repoCheck() (Check, bool) {
	r := d.f.Repo
	name := r.Name.String()

	switch {
	case r.Error != "":
		return checkBlock(CheckRepo, r.Error, FixCheckAgain), true
	case !r.Found:
		return checkBlock(CheckRepo, "GitHub doesn't show "+name+" to you."), true
	case r.Archived:
		return checkBlock(CheckRepo, name+" is archived, so GitHub takes no new tags in it."), true
	case r.DefaultBranch == "" || r.HeadOID == "":
		return checkBlock(CheckRepo, name+" has no commits on GitHub yet."), true
	default:
		return Check{}, false
	}
}

// originCheck: git reads the clone, and its origin is the repository.
func (d decider) originCheck() (Check, bool) {
	c := d.f.Clone

	switch {
	case c.Error != "":
		return checkBlock(CheckOrigin, withReason("Couldn't read this clone with git", c.Error), FixCheckAgain), true
	case !c.OriginMatches:
		return checkBlock(CheckOrigin, d.f.Repo.Name.String()+"'s clone has a different origin"), true
	default:
		return Check{}, false
	}
}

// accessCheck: the user may push a tag. GitHub not saying isn't read-only: magex finds
// out when it pushes.
func (d decider) accessCheck() (Check, bool) {
	r := d.f.Repo
	if !r.Found || !slices.Contains(readOnlyPermissions(), r.Permission) {
		return Check{}, false
	}

	level := strings.ToLower(r.Permission)
	if r.Permission == "NONE" {
		level = "no"
	}

	text := fmt.Sprintf("You have %s access to %s. Pushing a tag needs write access.", level, r.Name)

	return checkBlock(CheckAccess, text), true
}

// inProgressCheck: git isn't in the middle of something.
func (d decider) inProgressCheck() (Check, bool) {
	var what string

	switch d.f.Clone.InProgress {
	case gitread.OperationNone:
		return Check{}, false
	case gitread.OperationRebasing:
		what = "A rebase"
	case gitread.OperationMerging:
		what = "A merge"
	case gitread.OperationCherryPicking:
		what = "A cherry-pick"
	case gitread.OperationReverting:
		what = "A revert"
	default:
		what = "A git operation"
	}

	return checkBlock(CheckInProgress, what+" is in progress in this clone. Finish or abort it in your terminal."), true
}

// dirtyCheck: nothing uncommitted, untracked files included, as magex has it.
func (d decider) dirtyCheck() (Check, bool) {
	changes := d.f.Clone.Changes
	if len(changes) == 0 {
		return checkOK(CheckDirty, "No uncommitted changes"), true
	}

	count, them := "1 uncommitted change", "it"
	if len(changes) > 1 {
		count, them = strconv.Itoa(len(changes))+" uncommitted changes", "them"
	}

	names := strings.Join(changes[:min(len(changes), changesListed)], ", ")
	if more := len(changes) - changesListed; more > 0 {
		names += fmt.Sprintf(", and %d more", more)
	}

	text := fmt.Sprintf("%s: %s. %s won't tag with uncommitted work. Commit or stash %s.", count, names, magexName, them)

	return checkBlock(CheckDirty, text, FixOpenTerminal, FixCheckAgain), true
}

// detachedCheck: the clone is on a branch, not a bare commit.
func (d decider) detachedCheck() (Check, bool) {
	if !d.f.Clone.Detached {
		return Check{}, false
	}

	return checkBlock(CheckDetached, "Your clone is on a detached commit. Switch to a branch first."), true
}

// whereCheck: where the clone is, and that git can switch it to the release's branch.
func (d decider) whereCheck() (Check, bool) {
	c := d.f.Clone

	switch {
	case c.Detached || c.Branch == "" || d.branch == "":
		return Check{}, false
	case c.Branch == d.branch:
		return checkOK(CheckWhere, "You're on "+d.branch), true
	case len(c.Elsewhere) > 0:
		text := fmt.Sprintf("%s is checked out in another worktree at %s, so git can't switch to it here.",
			d.branch, c.Elsewhere[0])

		return checkBlock(CheckWhere, text), true
	default:
		text := fmt.Sprintf("You're on %s. %s switches to %s, then back.", c.Branch, magexName, d.branch)

		return checkOK(CheckWhere, text), true
	}
}

// branchCheck: the release's branch is one Release offers, and the clone has it, here or
// as origin's: magex switches to it from either.
func (d decider) branchCheck() (Check, bool) {
	c, b := d.f.Clone, d.branch

	switch {
	case b == "":
		return Check{}, false
	case !d.offers(b):
		return checkBlock(CheckBranch, fmt.Sprintf("%s isn't a branch Release offers for %s.", b, d.f.Repo.Name)), true
	case c.LocalHead != "" || c.TrackingHead != "":
		return Check{}, false
	case c.Reached && c.RemoteHeads[b] == "":
		return checkBlock(CheckBranch, b+" doesn't exist in this clone or on origin."), true
	default:
		text := fmt.Sprintf("This clone doesn't have %s yet. Fetch origin in your terminal, then check again.", b)

		return checkBlock(CheckBranch, text, FixOpenTerminal, FixCheckAgain), true
	}
}

// upstreamCheck: the clone's branch follows origin's, which `pull --rebase origin` needs.
// A magex that only fast-forwards needs no upstream, nor does a branch magex makes from
// origin's.
func (d decider) upstreamCheck() (Check, bool) {
	c, b := d.f.Clone, d.branch

	switch {
	case b == "" || c.LocalHead == "" || d.f.Tool.Has(integrations.FeatureFastForwardPull):
		return Check{}, false
	case c.Upstream.Remote == remoteOrigin && c.Upstream.Ref == "refs/heads/"+b:
		return Check{}, false
	}

	tracked := remoteOrigin + "/" + b
	out := checkBlock(CheckUpstream, fmt.Sprintf("%s doesn't track %s, so %s can't pull it.", b, tracked, magexName))
	out.Commands = []string{"git branch --set-upstream-to=" + shellWord(tracked) + " " + shellWord(b)}

	return out, true
}

// unpushedCheck: the clone's branch has no commits origin's lacks, which magex would
// rebase and tag, or, with a magex that only fast-forwards, refuse.
func (d decider) unpushedCheck() (Check, bool) {
	c, b := d.f.Clone, d.branch

	var has, them string

	switch {
	case c.LocalHead == "" || c.Unpushed == 0:
		return Check{}, false
	case c.Unpushed == UnpushedUnknown:
		text := fmt.Sprintf("Can't tell whether %s has commits that aren't on origin: this clone has neither "+
			"origin/%s nor origin's newest commit. Fetch origin in your terminal, then check again.", b, b)

		return checkBlock(CheckUnpushed, text, FixOpenTerminal, FixCheckAgain), true
	case c.Unpushed == 1:
		has, them = "1 commit that isn't", "it"
	case c.Unpushed == UnpushedSome:
		has, them = "commits that aren't", "them"
	default:
		has, them = strconv.Itoa(c.Unpushed)+" commits that aren't", "them"
	}

	would := fmt.Sprintf("%s would rebase %s and tag a commit GitHub doesn't have.", magexName, them)
	if d.f.Tool.Has(integrations.FeatureFastForwardPull) {
		would = fmt.Sprintf("%s can't fast-forward %s, so it would tag nothing.", magexName, b)
	}

	text := fmt.Sprintf("%s has %s on origin. %s Push %s, or reset %s to origin, in your terminal.", b, has, would, them, b)

	return checkBlock(CheckUnpushed, text), true
}

// localOnlyCheck: no version tag above origin's highest is only in the clone. magex would
// skip it and leave a gap. It needs origin's tags, so it waits for origin to answer.
func (d decider) localOnlyCheck() (Check, bool) {
	c := d.f.Clone
	if !c.Reached {
		return Check{}, false
	}

	remote := make([]Tag, 0, len(c.RemoteTags))
	for name := range c.RemoteTags {
		remote = append(remote, Tag{Name: name})
	}

	only := localOnly(c.LocalTags, remote)
	if len(only) == 0 {
		return Check{}, false
	}

	is, it := "is tagged on this Mac but isn't", "it"
	if len(only) > 1 {
		is, it = "are tagged on this Mac but aren't", "them"
	}

	text := fmt.Sprintf("%s %s on GitHub. %s would skip %s and leave a gap. Push %s or delete %s in your terminal:",
		listed(only), is, magexName, it, it, it)

	out := checkBlock(CheckLocalOnly, text, FixOpenTerminal)
	for _, tag := range only {
		out.Commands = append(out.Commands, "git push "+remoteOrigin+" "+shellWord(tag))
	}

	for _, tag := range only {
		out.Commands = append(out.Commands, "git tag -d "+shellWord(tag))
	}

	return out, true
}

// behindCheck: origin's branch has commits the clone's lacks, which magex pulls first.
func (d decider) behindCheck() (Check, bool) {
	c := d.f.Clone
	if c.LocalHead == "" || !c.Behind {
		return Check{}, false
	}

	text := fmt.Sprintf("%s/%s has commits your clone doesn't. %s pulls them first.", remoteOrigin, d.branch, magexName)

	return checkOK(CheckBehind, text), true
}

// reachCheck: origin answers with the user's git credentials.
func (d decider) reachCheck() (Check, bool) {
	c := d.f.Clone
	if c.Reached {
		return checkOK(CheckReach, "Reaches origin with your git credentials"), true
	}

	text := withReason("Can't reach origin with your git credentials", c.ReachError)

	return checkBlock(CheckReach, text, FixOpenTerminal, FixCheckAgain), true
}

// nothingNewCheck: origin's newest commit has no version yet.
func (d decider) nothingNewCheck() (Check, bool) {
	tag, ok := d.versionOnHead()
	if !ok {
		return Check{}, false
	}

	text := fmt.Sprintf("Nothing new: %s is already on the newest commit of %s.", tag, d.branch)

	return checkBlock(CheckNothingNew, text), true
}

// ciCheck: the checks on the default branch's newest commit, as GitHub rolls them up. It
// only warns.
func (d decider) ciCheck() (Check, bool) {
	r := d.f.Repo
	if !r.Found || r.HeadOID == "" || d.branch != r.DefaultBranch {
		return Check{}, false
	}

	short := r.HeadOID[:min(len(r.HeadOID), shortOIDLen)]

	switch r.Checks {
	case ChecksSuccess:
		return checkOK(CheckChecks, "Checks passed on "+short), true
	case ChecksFailure, ChecksError:
		out := checkWarn(CheckChecks, "Checks are failing on "+short, FixView)
		out.Link = checksURL(r.Name, r.HeadOID)

		return out, true
	case ChecksPending, ChecksExpected:
		return checkWarn(CheckChecks, "Checks are still running on "+short), true
	case ChecksNone:
		return Check{}, false
	default:
		return Check{}, false
	}
}

// busyCheck: no other job of the app's holds the clone.
func (d decider) busyCheck() (Check, bool) {
	c := d.f.Clone
	if !c.Busy {
		return Check{}, false
	}

	out := checkBlock(CheckBusy, c.Holder.Sentence())
	if c.Holder.RunID != 0 {
		out.Fixes, out.RunID = []Fix{FixWatch}, c.Holder.RunID
	}

	return out, true
}

// signingCheck: git can sign the tag here without asking in a terminal. A run in the
// terminal can ask, so there it only informs.
func (d decider) signingCheck() (Check, bool) {
	s := d.f.Clone.Signing

	switch s.Format {
	case SignSSH:
		return d.sshCheck(s), true
	case SignGPG:
		return d.gpgCheck(s), true
	case SignNone, SignX509:
		return Check{}, false
	default:
		return Check{}, false
	}
}

// sshCheck words ssh signing.
func (d decider) sshCheck(s SigningFacts) Check {
	switch {
	case s.AgentHasKey:
		return checkOK(CheckSigning, "Your git signs tags with ssh, and your ssh agent has the key")
	case s.OpenKey:
		return checkOK(CheckSigning, "Your git signs tags with ssh, from a key file that needs no passphrase")
	case d.terminal:
		return checkInfo(CheckSigning, "Your git signs tags with ssh, and your ssh agent doesn't have that key, "+
			"so your terminal will ask for its passphrase")
	default:
		out := checkWarn(CheckSigning, "Your git signs tags with ssh, but your ssh agent doesn't have that key, "+
			"so signing will fail here. Run ssh-add, or release in your terminal.")
		out.Commands = []string{"ssh-add"}

		return out
	}
}

// gpgCheck words gpg signing.
func (d decider) gpgCheck(s SigningFacts) Check {
	switch {
	case s.WindowPinentry:
		return checkOK(CheckSigning, "Your git signs tags with gpg, through a pinentry that can ask in a window")
	case d.terminal:
		return checkInfo(CheckSigning, "Your git signs tags with gpg, which asks for its passphrase in your terminal")
	default:
		return checkWarn(CheckSigning,
			"Your git signs tags with gpg, which asks for its passphrase in a terminal. Release in your terminal.")
	}
}

// shellCheck: the login shell's environment was read, so a run in the app has the user's
// ssh agent. A run in the terminal has the terminal's own environment.
func (d decider) shellCheck() (Check, bool) {
	if d.f.Clone.ShellRead || d.terminal {
		return Check{}, false
	}

	text := brand.Name + " couldn't read your shell's environment, so your ssh agent (or your password manager's) " +
		"may be missing. If the push fails, release in your terminal."

	return checkWarn(CheckShell, text, FixCheckAgain), true
}

// hooksCheck: the hooks the release runs, inside magex's time for each git command:
// pre-push when it pushes, and post-checkout when it switches branches.
func (d decider) hooksCheck() (Check, bool) {
	c := d.f.Clone
	push := slices.Contains(c.Hooks, hookPrePush)
	switches := slices.Contains(c.Hooks, hookPostCheckout) && c.Branch != d.branch

	var text string

	switch {
	case push && switches:
		text = fmt.Sprintf("This clone has %s and %s hooks. They run when %s switches branches and pushes, inside %s.",
			hookPrePush, hookPostCheckout, magexName, hookTime)
	case push:
		text = fmt.Sprintf("This clone has a %s hook. It runs when %s pushes, inside %s.", hookPrePush, magexName, hookTime)
	case switches:
		text = fmt.Sprintf("This clone has a %s hook. It runs when %s switches branches, inside %s.",
			hookPostCheckout, magexName, hookTime)
	default:
		return Check{}, false
	}

	return checkInfo(CheckHooks, text), true
}

// bumps works out the cards: each bump's version over every tag, offered unless there is
// nothing new, and Major only with a magex that can be told which version to make (O12).
func (d decider) bumps() []BumpOffer {
	_, nothingNew := d.versionOnHead()
	out := make([]BumpOffer, 0, len(allBumps()))

	for _, b := range allBumps() {
		o := BumpOffer{Bump: b, Version: Next(d.tags, b).String(), Offered: !nothingNew, Fixes: []Fix{}}
		if b == BumpMajor {
			d.gateMajor(&o)
		}

		out = append(out, o)
	}

	return out
}

// gateMajor closes the Major card while magex lacks expect= (§5.11.8).
func (d decider) gateMajor(o *BumpOffer) {
	t := d.f.Tool

	switch {
	case t.Has(integrations.FeatureExpect):
		return
	case t.ExpectSince != "":
		o.Note, o.Fixes = fmt.Sprintf("Needs %s %s or later", magexName, t.ExpectSince), []Fix{FixUpdate}
	default:
		o.Note = "Not available yet: " + magexName + " can't be told which version to make."
	}

	o.Offered = false
}

// major works out the careful form a major takes: its version, the go.mod check for a Go
// module going to v2 or later, and the typed confirmation.
func (d decider) major() MajorOffer {
	v := Next(d.tags, BumpMajor)
	m := MajorOffer{Version: v.String(), FirstStable: v.Major == 1, ModulePath: d.f.Clone.ModulePath}
	m.Confirmed = strings.TrimSpace(d.f.Choice.Typed) == m.Version

	if v.Major < 2 || m.ModulePath == "" {
		return m
	}

	if want := moduleFor(m.ModulePath, v.Major); want != m.ModulePath {
		m.ModuleWant = want
		m.ModuleText = fmt.Sprintf("go.mod says module %s. Go can't install %s until it says %s.", m.ModulePath, m.Version, want)
	}

	return m
}

// majorAllowed reports a major the user confirmed in full, with a magex that makes only
// the version they typed.
func (d decider) majorAllowed(m MajorOffer) bool {
	return d.f.Tool.Has(integrations.FeatureExpect) && m.Confirmed && (m.ModuleWant == "" || d.f.Choice.AllowModulePath)
}

// ready reports the release can start: magex runs, nothing blocks, the chosen bump is
// offered, and a major is confirmed.
func (d decider) ready(checks []Check, bumps []BumpOffer, major MajorOffer) bool {
	if !d.f.Tool.Usable() || d.branch == "" || hasState(checks, CheckBlock) {
		return false
	}

	i := slices.IndexFunc(bumps, func(o BumpOffer) bool { return o.Bump == d.bump })
	if i < 0 || !bumps[i].Offered {
		return false
	}

	return d.bump != BumpMajor || d.majorAllowed(major)
}

// argv is what the release runs (§5.11.4): push=true, never the bare word, and each
// optional parameter only for a magex that reads it, since magex ignores a key it
// doesn't know.
func (d decider) argv(major MajorOffer) []string {
	t := d.f.Tool
	if !t.Usable() || d.branch == "" || !d.bump.Valid() {
		return []string{}
	}

	argv := []string{t.Path, "version:bump", "bump=" + string(d.bump), "branch=" + d.branch, "push=true"}

	if t.Has(integrations.FeatureExpect) {
		argv = append(argv, "expect="+d.version.String())
	}

	if t.Has(integrations.FeatureBumpJSON) {
		argv = append(argv, "json=true")
	}

	if t.Has(integrations.FeatureFastForwardPull) {
		argv = append(argv, "ff-only")
	}

	if d.bump == BumpMajor && d.majorAllowed(major) {
		argv = append(argv, "major-confirm")
	}

	return argv
}

// branches are the branches the menu offers (D10): the default, preselected, and main or
// master when origin has it.
func (d decider) branches() []BranchChoice {
	def := d.f.Repo.DefaultBranch
	if def == "" {
		return []BranchChoice{}
	}

	out := []BranchChoice{{Name: def, Default: true}}

	for _, b := range otherBranches() {
		if b != def && slices.Contains(d.f.Repo.Branches, b) {
			out = append(out, BranchChoice{Name: b})
		}
	}

	return out
}

// offers reports a branch the menu offers.
func (d decider) offers(branch string) bool {
	return slices.ContainsFunc(d.branches(), func(b BranchChoice) bool { return b.Name == branch })
}

// branchNote says the release's branch isn't the default, when it isn't.
func (d decider) branchNote() string {
	def := d.f.Repo.DefaultBranch
	if d.branch == "" || def == "" || d.branch == def {
		return ""
	}

	return fmt.Sprintf("%s isn't the default branch on GitHub (%s is).", d.branch, def)
}

// versionText is the chosen bump's version, or "" for a bump that isn't one.
func (d decider) versionText() string {
	if !d.bump.Valid() {
		return ""
	}

	return d.version.String()
}

// button names what the button does: the version, a major, a release despite a warning,
// and a run in the terminal.
func (d decider) button(anyway bool) string {
	if !d.bump.Valid() {
		return ""
	}

	label := "Release " + d.version.String()

	if d.bump == BumpMajor {
		label += " (major)"
	}

	if anyway {
		label += " anyway"
	}

	if d.terminal {
		label += " in " + d.terminalName()
	}

	return label
}

// sentence says exactly what will happen (§3.9): the branch, the version, and where it
// runs.
func (d decider) sentence() string {
	if d.branch == "" || !d.bump.Valid() {
		return ""
	}

	current, v := d.f.Clone.Branch, d.version.String()

	var does string

	switch current {
	case d.branch:
		does = fmt.Sprintf("%s pulls %s, tags the newest commit as %s, and pushes the tag to origin.", magexName, d.branch, v)
	case "":
		does = fmt.Sprintf("%s switches to %s, pulls, tags the newest commit as %s, and pushes the tag to origin.",
			magexName, d.branch, v)
	default:
		does = fmt.Sprintf("%s switches to %s, pulls, tags the newest commit as %s, pushes the tag to origin, "+
			"and switches back to %s.", magexName, d.branch, v, current)
	}

	if d.terminal {
		return does + " It runs in " + d.terminalName() + "."
	}

	return does + " It runs here in " + brand.Name + "."
}

// terminalName names the terminal a run goes to.
func (d decider) terminalName() string {
	return cmp.Or(d.f.Choice.Terminal, "your terminal")
}

// versionOnHead returns the highest version tag on origin's newest commit, and whether
// there is one: then there is nothing new to release.
func (d decider) versionOnHead() (string, bool) {
	if d.head == "" {
		return "", false
	}

	var on []Tag

	for _, name := range slices.Sorted(maps.Keys(d.f.Clone.RemoteTags)) {
		if d.f.Clone.RemoteTags[name].Commit == d.head {
			on = append(on, Tag{Name: name, Commit: d.head})
		}
	}

	tag, ok := newestVersionTag(on)

	return tag.Name, ok
}

// allBumps are the bumps, in the order the cards show them.
func allBumps() []Bump {
	return []Bump{BumpPatch, BumpMinor, BumpMajor}
}

// moduleFor returns the module path Go needs for major version major of the module at
// path: path with its major suffix, /vN (.vN on gopkg.in), set to major.
func moduleFor(path string, major int) string {
	sep := "/"
	if strings.HasPrefix(path, "gopkg.in/") {
		sep = "."
	}

	base := path
	if i := strings.LastIndex(path, sep+"v"); i >= 0 && isDigits(path[i+len(sep)+1:]) {
		base = path[:i]
	}

	return base + sep + "v" + strconv.Itoa(major)
}

// isDigits reports a non-empty run of decimal digits.
func isDigits(s string) bool {
	return s != "" && strings.Trim(s, "0123456789") == ""
}

// listed joins names as a sentence lists them: "a", "a and b", "a, b, and c".
func listed(names []string) string {
	if len(names) < 3 {
		return strings.Join(names, " and ")
	}

	return strings.Join(names[:len(names)-1], ", ") + ", and " + names[len(names)-1]
}

// shellWord quotes s as one word for a POSIX shell, for a command the user copies: as it
// is when every character is plainly safe, else in single quotes, each ' inside written
// '\”. A branch or tag name can hold $(…) or backticks.
func shellWord(s string) string {
	if s != "" && strings.Trim(s, safeShellChars) == "" {
		return s
	}

	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// safeShellChars are the characters a shell reads as themselves anywhere in a word.
const safeShellChars = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789._/@:+=-"

// checksURL is the page of a commit's checks on GitHub.
func checksURL(name repo.Name, oid string) string {
	return "https://github.com/" + name.String() + "/commit/" + oid + "/checks"
}

// hasState reports whether any check is in state.
func hasState(checks []Check, state CheckState) bool {
	return slices.ContainsFunc(checks, func(c Check) bool { return c.State == state })
}

// checkOK, checkInfo, checkWarn, and checkBlock make a check in each state.
func checkOK(id CheckID, text string) Check { return newCheck(id, CheckOK, text) }

func checkInfo(id CheckID, text string) Check { return newCheck(id, CheckInfo, text) }

func checkWarn(id CheckID, text string, fixes ...Fix) Check {
	return newCheck(id, CheckWarn, text, fixes...)
}

func checkBlock(id CheckID, text string, fixes ...Fix) Check {
	return newCheck(id, CheckBlock, text, fixes...)
}

// newCheck makes a check, its lists never nil.
func newCheck(id CheckID, state CheckState, text string, fixes ...Fix) Check {
	return Check{ID: id, State: state, Text: text, Fixes: append([]Fix{}, fixes...), Commands: []string{}}
}
