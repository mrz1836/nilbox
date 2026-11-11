package release

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"time"

	"github.com/mrz1836/nilbox/internal/integrationprefs"
	"github.com/mrz1836/nilbox/internal/integrations"
	"github.com/mrz1836/nilbox/internal/paths"
	"github.com/mrz1836/nilbox/internal/repo"
	"github.com/mrz1836/nilbox/internal/scriptrun"
	"github.com/mrz1836/nilbox/internal/textutil"
	"github.com/mrz1836/nilbox/internal/uierr"
)

// FactsFor is how long the facts a sheet was shown serve the next Prepare or Start for the
// same clone and branch, so a choice on the sheet (a bump, the typed version, where it
// runs) asks GitHub and origin nothing. Prepare's Refresh reads them again at any age, and
// Start always reads them again under the clone's lock before anything runs.
const FactsFor = 2 * time.Minute

var (
	// ErrNotSetUp is returned by Prepare and Start on a Service built without what a
	// release needs: git, and the runner.
	ErrNotSetUp = errors.New("releases are not set up")

	// ErrInvalidRequest is returned for a request naming no repository, or a bump, a
	// version, or a place to run that isn't one.
	ErrInvalidRequest = errors.New("invalid release request")

	// ErrNoClone is returned when no clone under the clone root has the repository as its
	// origin.
	ErrNoClone = errors.New("no clone of the repository")

	// ErrNotReady is returned by Start when the facts, gathered again, block the release:
	// the sheet shows them again.
	ErrNotReady = errors.New("the release can't start")

	// ErrVersionMoved is returned by Start when the version the release would make is no
	// longer the one the button showed: the sheet shows the new one.
	ErrVersionMoved = errors.New("the version to release moved")

	// ErrRejected is returned by Start for a release the sheet should never have started:
	// a major without its typed confirmation, without the go.mod box ticked where the
	// module path needs it, or with a magex that can't be told which version to make. It
	// is logged as a bug.
	ErrRejected = errors.New("the release was refused")
)

// Runs starts a release's run: the scriptrun engine.
type Runs interface {
	Start(ctx context.Context, spec scriptrun.Spec) (int64, error)
}

// Where is where runs go, as the user set it: in the app or in the terminal, and, for a
// run there, the terminal's name, as in "iTerm2".
type Where struct {
	RunsIn   integrationprefs.RunsIn
	Terminal string
}

// Places reads where runs go: the integrations.runsIn setting, and the terminal chosen for
// launches. Core adapts the settings.
type Places interface {
	Where(ctx context.Context) (Where, error)
}

// PrepareRequest is what the sheet asks Prepare about: the repository and its clone, and
// the user's choice so far. Every choice is optional, and Decide reads them all, so a
// choice that changes what the sheet says is one more Prepare, served from the facts the
// sheet was last shown (FactsFor) unless Refresh is set.
type PrepareRequest struct {
	// Repo is the repository, "owner/name", and Dir its clone as the Release page lists
	// it, with ~, or "" for the first clone of it found.
	Repo string `json:"repo"`
	Dir  string `json:"dir"`

	// Branch is the branch to release, or "" for the default on GitHub; Bump the bump, or
	// "" for patch.
	Branch string `json:"branch"`
	Bump   Bump   `json:"bump"`

	// Typed is what the user typed to confirm a major, and AllowModulePath the box ticked
	// to release a major go.mod doesn't name.
	Typed           string `json:"typed"`
	AllowModulePath bool   `json:"allowModulePath"`

	// RunsIn is where this release runs, or "" for the setting.
	RunsIn integrationprefs.RunsIn `json:"runsIn"`

	// Refresh reads GitHub, origin, and the clone again, as opening the sheet and Check
	// again do.
	Refresh bool `json:"refresh"`
}

// StartRequest is the release the user started on the sheet: the choice, and what the
// sheet showed when they clicked, which Start holds the fresh facts to.
type StartRequest struct {
	// Repo is the repository, "owner/name", and Dir its clone, as in PrepareRequest.
	Repo string `json:"repo"`
	Dir  string `json:"dir"`

	// Branch is the branch to release, or "" for the default on GitHub, and Bump the bump.
	Branch string `json:"branch"`
	Bump   Bump   `json:"bump"`

	// Version is the version the button showed (Offer.Version), Head the commit the sheet
	// showed (Offer.Head), and Original the branch the clone was on (Offer.Current).
	Version  string `json:"version"`
	Head     string `json:"head"`
	Original string `json:"original"`

	// Typed and AllowModulePath confirm a major, as in PrepareRequest.
	Typed           string `json:"typed"`
	AllowModulePath bool   `json:"allowModulePath"`

	// RunsIn is where this release runs, or "" for the setting.
	RunsIn integrationprefs.RunsIn `json:"runsIn"`
}

// Prepare gathers a release's facts and returns the offer Decide works out from them: the
// sheet's branches, checks, cards, and button. It finds the repository's clone among the
// Release page's clones, or by scanning the clone root. The facts are GitHub's (one
// query), origin's (one ls-remote), and the clone's (read-only git); a later Prepare for
// the same clone and branch within FactsFor decides again on them, with the new choice,
// unless req.Refresh is set. What can't be read is a check the offer shows, so it fails
// only for a request it can't serve.
func (s *Service) Prepare(ctx context.Context, req PrepareRequest) (Offer, error) {
	if s.gatherer == nil {
		return Offer{}, ErrNotSetUp
	}

	sh, err := s.open(ctx, req.Repo, req.Dir, Choice{
		Bump: req.Bump, Branch: req.Branch, Typed: req.Typed, AllowModulePath: req.AllowModulePath, RunsIn: req.RunsIn,
	})
	if err != nil {
		return Offer{}, err
	}

	f, err := s.factsFor(ctx, sh, req.Refresh)
	if err != nil {
		return Offer{}, err
	}

	return Decide(f), nil
}

// Start starts the release the user chose on the sheet and returns its run's ID. The
// runner takes the clone's lock (a busy clone fails at once, naming what holds it) and a
// slot, and only then, in the run's Before, are the facts gathered again and decided on:
// the clone and origin read afresh, and GitHub's facts reused from the sheet while they
// are under FactsFor old, so a click asks GitHub nothing. magex's argv comes from that
// decision. The release starts only when the offer is ready and makes the version the
// button showed; otherwise the error says why, in words: ErrNotReady with the new reason,
// ErrVersionMoved with the new version, or ErrRejected, for what the sheet should never
// have sent. Once magex ends, the run's Finish verifies the release.
func (s *Service) Start(ctx context.Context, req StartRequest) (int64, error) {
	if s.gatherer == nil || s.runs == nil {
		return 0, ErrNotSetUp
	}

	if _, ok := ParseVersion(req.Version); !ok || !req.Bump.Valid() {
		return 0, fmt.Errorf("%w: the bump %q to %q", ErrInvalidRequest, req.Bump, req.Version)
	}

	sh, err := s.open(ctx, req.Repo, req.Dir, Choice{
		Bump: req.Bump, Branch: req.Branch, Typed: req.Typed, AllowModulePath: req.AllowModulePath, RunsIn: req.RunsIn,
	})
	if err != nil {
		return 0, err
	}

	id, err := s.runs.Start(ctx, s.spec(req, sh))

	// The clone is about to change, or the recheck found it had: the sheet reads it again.
	s.forget(sh.dir)

	if err != nil {
		return id, err
	}

	s.logger.InfoContext(ctx, "a release started", "run", id, "repo", sh.name.String(), "bump", string(req.Bump),
		"in", string(sh.choice.RunsIn))

	return id, nil
}

// spec is the run of the release req starts: magex in the clone, with the shell's
// environment, its intent, its output reader and the progress it reads, its Finish, which
// verifies it, its Recorded, which tells of it once that is kept, and its Before, which
// decides, under the clone's lock, what magex runs, and completes the intent before the run
// starts, so the progress reads it whole.
func (s *Service) spec(req StartRequest, sh sheet) scriptrun.Spec {
	in := &Intent{Version: req.Version, Bump: req.Bump, Branch: req.Branch, Original: req.Original, Head: req.Head}
	github := s.sheetGitHub(sh.dir)
	reader := NewOutputReader()
	env, _ := s.env()

	return scriptrun.Spec{
		Kind: scriptrun.KindRelease, Title: "Release " + req.Version, Repo: sh.name, Dir: sh.dir,
		Env: runEnv(env, sh.choice.RunsIn), RunsIn: sh.choice.RunsIn, Tool: string(integrationprefs.MageX), Intent: in,
		Output:   reader,
		Progress: func() scriptrun.Progress { return reader.Progress(*in) },
		Before: func(ctx context.Context) ([]string, error) {
			gather := GatherRequest{Repo: sh.name, Dir: sh.dir, Choice: sh.choice, Locked: true, GitHub: github}

			return s.recheck(ctx, req, gather, in)
		},
		Finish:   s.finisher(sh.name, sh.dir, in, reader),
		Recorded: s.announcer(sh.name, in),
	}
}

// sheetGitHub returns what GitHub said of the repository when the clone at dir's sheet
// was last shown, while under FactsFor old, or nil to ask again.
func (s *Service) sheetGitHub(dir string) *RepoFacts {
	s.factsMu.Lock()
	kept, ok := s.facts[paths.Resolved(dir)]
	s.factsMu.Unlock()

	if age := s.clock.Now().Sub(kept.at); !ok || kept.facts.Repo.Error != "" || age < 0 || age >= FactsFor {
		return nil
	}

	github := kept.facts.Repo

	return &github
}

// recheck is a release's Before: under the clone's lock, it gathers the facts and decides
// on them, and lets the release start only when the offer is ready, for the version the
// button showed. It completes in from the facts and returns the argv to run.
func (s *Service) recheck(ctx context.Context, req StartRequest, gather GatherRequest, in *Intent,
) ([]string, error) {
	f, err := s.gatherer.Gather(ctx, gather)
	if err != nil {
		return nil, fmt.Errorf("check the release again: %w", err)
	}

	o := Decide(f)
	if err = s.approve(ctx, req, f, o); err != nil {
		return nil, err
	}

	in.Branch, in.Original, in.Head = o.Branch, cmp.Or(in.Original, f.Clone.Branch), cmp.Or(in.Head, o.Head)
	in.FastForward = slices.Contains(o.Argv, argFastForward)

	return o.Argv, nil
}

// approve holds an offer to what the user started: a magex that can make a major, the
// version the button showed, a major confirmed in full, and nothing blocking.
func (s *Service) approve(ctx context.Context, req StartRequest, f Facts, o Offer) error {
	// A magex that's gone isn't a bug: the offer says it isn't available.
	if req.Bump == BumpMajor && f.Tool.Usable() && !f.Tool.Has(integrations.FeatureExpect) {
		text := fmt.Sprintf("A major release needs %s %s or later, which can be told which version to make.",
			magexName, cmp.Or(f.Tool.ExpectSince, "a release that isn't out yet"))

		return s.reject(ctx, req, "magex lacks expect=", text)
	}

	if o.Version != req.Version {
		return movedError(req, f, o)
	}

	if req.Bump == BumpMajor {
		switch {
		case !o.Major.Confirmed:
			return s.reject(ctx, req, "the typed version doesn't match",
				fmt.Sprintf("Type %s to confirm the major release.", o.Major.Version))
		case o.Major.ModuleWant != "" && !req.AllowModulePath:
			return s.reject(ctx, req, "go.mod's module path wasn't accepted", o.Major.ModuleText)
		}
	}

	if !o.Ready {
		return uierr.New(notReadyReason(o), ErrNotReady)
	}

	return nil
}

// reject refuses a release the sheet should never have started, and logs it as the bug it
// is.
func (s *Service) reject(ctx context.Context, req StartRequest, why, text string) error {
	s.logger.ErrorContext(ctx, "refused a release the sheet should not have started; this is a bug",
		"repo", req.Repo, "bump", string(req.Bump), "reason", why)

	return uierr.New(text, fmt.Errorf("%w: %s", ErrRejected, why))
}

// movedError says the version moved since the sheet showed it: the version released
// meanwhile and the one the bump makes now.
func movedError(req StartRequest, f Facts, o Offer) error {
	shown, _ := ParseVersion(req.Version)
	now, _ := ParseVersion(o.Version)

	text := fmt.Sprintf("The next %s is %s now, not %s.", o.Bump, o.Version, req.Version)

	if shown.Less(now) {
		taken, _ := Highest(newDecider(f).tags)
		text = fmt.Sprintf("%s was released a moment ago. The next %s is %s.", taken, o.Bump, o.Version)
	}

	return uierr.New(text, fmt.Errorf("%w: %s, not %s", ErrVersionMoved, o.Version, req.Version))
}

// notReadyReason says why an offer isn't ready: its first blocking check, or why the bump
// isn't offered.
func notReadyReason(o Offer) string {
	for _, c := range o.Checks {
		if c.State == CheckBlock {
			return textutil.EndSentence(c.Text)
		}
	}

	i := slices.IndexFunc(o.Bumps, func(b BumpOffer) bool { return b.Bump == o.Bump })
	if i >= 0 && !o.Bumps[i].Offered && o.Bumps[i].Note != "" {
		return textutil.EndSentence(o.Bumps[i].Note)
	}

	return "The release can't start yet. Check again."
}

// sheet is what Prepare and Start work from: the repository, its clone's folder, and the
// user's choice, with where runs go filled in.
type sheet struct {
	name   repo.Name
	dir    string
	choice Choice
}

// open reads a request: its repository, which it finds the clone of (the one shown, when
// set), and its choice, which it checks.
func (s *Service) open(ctx context.Context, repoText, shown string, c Choice) (sheet, error) {
	name, err := s.checkRequest(repoText, c.Bump, c.RunsIn)
	if err != nil {
		return sheet{}, err
	}

	dir, err := s.cloneFor(ctx, name, shown)
	if err != nil {
		return sheet{}, err
	}

	if c, err = s.choice(ctx, c); err != nil {
		return sheet{}, err
	}

	return sheet{name: name, dir: dir, choice: c}, nil
}

// checkRequest reads a request's repository, and checks its bump and place to run.
func (s *Service) checkRequest(repoText string, bump Bump, runsIn integrationprefs.RunsIn) (repo.Name, error) {
	name, err := repo.Parse(repoText)
	if err != nil {
		return repo.Name{}, fmt.Errorf("%w: %w", ErrInvalidRequest, err)
	}

	if bump != "" && !bump.Valid() {
		return repo.Name{}, fmt.Errorf("%w: the bump %q", ErrInvalidRequest, bump)
	}

	if runsIn != "" && !runsIn.Valid() {
		return repo.Name{}, fmt.Errorf("%w: the place %q", ErrInvalidRequest, runsIn)
	}

	return name, nil
}

// choice fills in the user's choice with where runs go, as set, unless the sheet chose for
// this release, and the terminal's name.
func (s *Service) choice(ctx context.Context, c Choice) (Choice, error) {
	where := Where{RunsIn: integrationprefs.RunsInApp}

	if s.places != nil {
		got, err := s.places.Where(ctx)
		if err != nil {
			return Choice{}, fmt.Errorf("read where runs go: %w", err)
		}

		where = got
	}

	c.RunsIn = cmp.Or(c.RunsIn, where.RunsIn, integrationprefs.RunsInApp)
	c.Terminal = where.Terminal

	return c, nil
}

// cloneFor finds the clone of name: among the Release page's clones when it lists the
// clone root as set now, else by scanning the clone root. shown, when set, is the clone's
// folder as the page lists it, with ~, and picks one of several clones of name.
func (s *Service) cloneFor(ctx context.Context, name repo.Name, shown string) (string, error) {
	root, err := s.clones.Root(ctx)
	if err != nil {
		return "", fmt.Errorf("read the clone root: %w", err)
	}

	matches := func(dir string, origin repo.Name) bool {
		return origin.Key() == name.Key() && (shown == "" || shown == dir || shown == paths.Tilde(dir, s.home))
	}

	if dir, ok := s.listedClone(root, matches); ok {
		return dir, nil
	}

	all, err := s.clones.Clones(ctx, root)
	if err != nil {
		return "", fmt.Errorf("list the clones: %w", err)
	}

	slices.SortFunc(all, func(a, b Clone) int { return cmp.Compare(a.Dir, b.Dir) })

	if i := slices.IndexFunc(all, func(c Clone) bool { return c.OnHub && matches(c.Dir, c.Origin) }); i >= 0 {
		return all[i].Dir, nil
	}

	return "", uierr.New(fmt.Sprintf("There's no clone of %s in %s.", name, paths.Tilde(root, s.home)),
		fmt.Errorf("%w: %s", ErrNoClone, name))
}

// listedClone returns the first clone the Release page lists that matches, when the page
// was read from root.
func (s *Service) listedClone(root string, matches func(dir string, origin repo.Name) bool) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.has || s.root != root {
		return "", false
	}

	i := slices.IndexFunc(s.last.Repos, func(c Candidate) bool { return matches(c.dir, c.origin) })
	if i < 0 {
		return "", false
	}

	return s.last.Repos[i].dir, true
}

// keptFacts are the facts a clone's sheet was last shown, and when they were gathered.
type keptFacts struct {
	facts Facts
	at    time.Time
}

// factsFor returns the facts of the sheet's clone for its choice: the ones it was last
// shown, when they are for the same branch, under FactsFor old, and refresh isn't set;
// else they are gathered again and kept, one set per clone. Facts served again get choice,
// and the cheap facts read again: magex, the shell's environment, and the clone's lock.
func (s *Service) factsFor(ctx context.Context, sh sheet, refresh bool) (Facts, error) {
	choice := sh.choice
	key := paths.Resolved(sh.dir)
	now := s.clock.Now()

	s.factsMu.Lock()
	kept, ok := s.facts[key]
	s.factsMu.Unlock()

	// "" is the default branch, which the kept facts know.
	sameBranch := cmp.Or(choice.Branch, kept.facts.Repo.DefaultBranch) == kept.facts.branch()

	if ok && !refresh && sameBranch && now.Sub(kept.at) < FactsFor && !now.Before(kept.at) {
		f := kept.facts
		f.Choice = choice
		s.gatherer.freshen(ctx, &f)

		return f, nil
	}

	f, err := s.gatherer.Gather(ctx, GatherRequest{Repo: sh.name, Dir: sh.dir, Choice: choice})
	if err != nil {
		return Facts{}, err
	}

	s.factsMu.Lock()
	defer s.factsMu.Unlock()

	maps.DeleteFunc(s.facts, func(_ string, v keptFacts) bool { return now.Sub(v.at) >= FactsFor })
	s.facts[key] = keptFacts{facts: f, at: now}

	return f, nil
}

// forget drops the facts kept for the clone at dir: a release changes them.
func (s *Service) forget(dir string) {
	s.factsMu.Lock()
	defer s.factsMu.Unlock()

	delete(s.facts, paths.Resolved(dir))
}

// runEnv is the environment a release runs with: magex's, as every run of a tool gets it
// (integrations.Definition.RunEnv).
func runEnv(base []string, runsIn integrationprefs.RunsIn) []string {
	return integrations.MageX().RunEnv(base, runsIn)
}
