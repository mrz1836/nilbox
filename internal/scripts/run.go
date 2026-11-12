package scripts

import (
	"cmp"
	"context"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/mrz1836/nilbox/internal/integrationprefs"
	"github.com/mrz1836/nilbox/internal/integrations"
	"github.com/mrz1836/nilbox/internal/paths"
	"github.com/mrz1836/nilbox/internal/scriptrun"
	"github.com/mrz1836/nilbox/internal/uierr"
)

// Limits on what a run request carries.
const (
	maxArgs     = 32
	maxArgBytes = 1024
	maxInput    = 4096
)

// argWord matches a magex argument: key=value, or a bare key, which magex reads as true.
// The key starts with a letter or a digit, so no argument reads as a flag.
var argWord = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]*(=[^\x00\r\n]*)?$`)

// RunRequest is a script the window asks to run in one clone.
type RunRequest struct {
	// Dir is the clone, as the page lists it (Page.Dir), with ~, and Ref the script
	// (Entry.Ref).
	Dir string `json:"dir"`
	Ref string `json:"ref"`

	// Inputs are the values the form filled in, by input ID; one left out takes its
	// default. Args are a magex target's key=value arguments.
	Inputs map[string]string `json:"inputs"`
	Args   []string          `json:"args"`

	// RunsIn is where it runs, or "" for where the script says, as one of the user's own
	// may, or else the setting.
	RunsIn integrationprefs.RunsIn `json:"runsIn"`

	// Confirmed is the SHA256 of the command the user confirmed in the trust question,
	// as the page showed it (Entry.SHA256), or "" when none was asked; Remember keeps it
	// trusted, the question's "Don't ask again".
	Confirmed string `json:"confirmed"`
	Remember  bool   `json:"remember"`

	// Anyway runs a command that bumps a version, which the page points to Release for.
	Anyway bool `json:"anyway"`
}

// Intent is what a script run sets out to do, written into its run's result as it starts,
// so Run again works it out again: the script, the values and arguments it ran with, and
// the hash of the command that ran, "" for one that needs no trust.
type Intent struct {
	Ref    string            `json:"ref"`
	Inputs map[string]string `json:"inputs"`
	Args   []string          `json:"args"`
	Anyway bool              `json:"anyway"`
	SHA256 string            `json:"sha256"`
}

// confirmation is what the user confirmed of a repository command: its hash, and whether
// to remember it; rerun marks Run again, which never confirms and asks nothing, so it runs
// only a command the user trusted.
type confirmation struct {
	hash     string
	remember bool
	rerun    bool
}

// Question is what the window asks before a repository command runs, sent with the error
// that refused it (QuestionError): the command as it will run, with the chosen inputs
// filled in, and what the user trusted before.
type Question struct {
	// Ref, Name, and Source name the script.
	Ref    string `json:"ref"`
	Name   string `json:"name"`
	Source Source `json:"source"`

	// Command is what will run, the chosen inputs filled in; the user's environment's
	// values are never shown.
	Command string `json:"command"`

	// Trust is why it asks, ask or changed; Trusted is the command trusted before, and
	// TrustNote what the question adds, as on the page's entry.
	Trust     Trust  `json:"trust"`
	Trusted   string `json:"trusted"`
	TrustNote string `json:"trustNote"`

	// SHA256 is the hash the user confirms, which a run sends back as Confirmed.
	SHA256 string `json:"sha256"`
}

// QuestionError refuses a run until the user answers its Question. Its message says why,
// and Detail hands the question to the desktop runtime, which sends it to the window.
type QuestionError struct {
	Question Question
	err      error
}

// Error returns the refusal's text.
func (e *QuestionError) Error() string {
	return e.err.Error()
}

// Unwrap returns the refusal: ErrNeedsTrust or ErrChanged, with the words for the user.
func (e *QuestionError) Unwrap() error {
	return e.err
}

// Detail returns the question, for the window.
func (e *QuestionError) Detail() any {
	return e.Question
}

// pending is a run as it was asked for, which its Before checks again under the clone's
// lock: the clone, the script, what it set out to do, what the user confirmed, and its
// variables.
type pending struct {
	c    clone
	src  Source
	name string
	in   Intent
	conf confirmation
	v    vars

	// title is the script's name as the run records it, and look what one of the user's
	// own scripts ran as then (script.look), which the Before compares.
	title string
	look  string
}

// Run runs the script req names in its clone, and returns the run's ID. A command from the
// repository's files runs only as the user confirmed it, or as they trusted it before:
// the run's Before, under the clone's lock, reads its source again and refuses a command
// whose hash isn't that one.
func (s *Service) Run(ctx context.Context, req RunRequest) (int64, error) {
	if s.runs == nil {
		return 0, ErrNotSetUp
	}

	if !validPlace(req.RunsIn) {
		return 0, fmt.Errorf("%w: the place %q", ErrInvalidRequest, req.RunsIn)
	}

	c, err := s.cloneAt(ctx, req.Dir)
	if err != nil {
		return 0, err
	}

	cfg, err := s.config(ctx)
	if err != nil {
		return 0, err
	}

	in := Intent{Ref: req.Ref, Inputs: req.Inputs, Args: req.Args, Anyway: req.Anyway}

	spec, err := s.spec(ctx, c, in, req.RunsIn, cfg, confirmation{hash: req.Confirmed, remember: req.Remember})
	if err != nil {
		return 0, err
	}

	id, err := s.runs.Start(ctx, spec)
	if err != nil {
		return id, err
	}

	s.logger.InfoContext(ctx, "a script started", "run", id, "source", spec.Source, "repo", c.display(),
		"in", string(spec.RunsIn))

	return id, nil
}

// spec is the run of the script in intent, in c: worked out now, for its title and its
// display form, and again by its Before under the clone's lock, which refuses it unless it
// is still what was worked out now and what the user trusts. It runs where asked, or else
// where the script says, or else where the setting sends runs.
func (s *Service) spec(ctx context.Context, c clone, in Intent, asked integrationprefs.RunsIn,
	cfg integrationprefs.Config, conf confirmation,
) (scriptrun.Spec, error) {
	src, name, ok := parseRef(in.Ref)
	if !ok {
		return scriptrun.Spec{}, fmt.Errorf("%w: the script %q", ErrInvalidRequest, in.Ref)
	}

	tools := s.toolsNow(ctx)

	if why := cmp.Or(scriptsOff(cfg, tools), sourceOff(cfg, src)); why != "" {
		return scriptrun.Spec{}, uierr.New(why, ErrCantRun)
	}

	sc, err := s.find(ctx, c, src, name, tools.Magex, false)
	if err != nil {
		return scriptrun.Spec{}, err
	}

	if conf.rerun && sc.asksFirst {
		return scriptrun.Spec{}, uierr.New(fmt.Sprintf("%q asks before each run, so run it from Repo scripts.",
			sc.entry.Name), fmt.Errorf("%w: %s", ErrAsksFirst, sc.entry.Ref))
	}

	if in.Inputs, err = check(sc, in); err != nil {
		return scriptrun.Spec{}, err
	}

	in.SHA256, in.Args = sc.hash, nonNil(in.Args)
	where := cmp.Or(asked, sc.place, cfg.RunsIn, integrationprefs.RunsInApp)

	base, _ := s.env()
	p := pending{
		c: c, src: src, name: name, in: in, conf: conf,
		v:     vars{dir: c.dir, home: s.home, env: base, inputs: in.Inputs},
		title: sc.entry.Name, look: sc.look,
	}

	spec := scriptrun.Spec{
		Kind: scriptrun.KindScript, Title: sc.entry.Name, Source: sourceFile(src), Repo: c.name, Dir: c.dir,
		Env: runEnv(src, base, where), Command: runDisplay(sc, p.v, in.Args), RunsIn: where, Intent: in,
		Before: func(ctx context.Context) ([]string, error) { return s.before(ctx, p) },
	}

	switch src {
	case SourceMagex:
		spec.Tool = string(integrationprefs.MageX)
	case SourceYours:
		spec.Env = withVars(spec.Env, s.repoVars(ctx, c))
	case SourceT3, SourceTasks:
	}

	return spec, nil
}

// repoVars are the Variables one of the user's scripts runs with in the clone c, KEY=value:
// its repository, its folder, and its repository's default branch, worked out as the
// page's From branch note works it out.
func (s *Service) repoVars(ctx context.Context, c clone) []string {
	names := Variables()

	return []string{
		names[0] + "=" + repoText(c.name),
		names[1] + "=" + c.dir,
		names[2] + "=" + s.defaultBranch(ctx, c),
	}
}

// before is a script run's Before: under the clone's lock, it reads the script's source
// again, from its bytes, and refuses it when its hash isn't the one worked out as the run
// was asked for, or, for one of the user's own scripts, when it was saved again since with
// another name, command, place, or Ask before running. It works the command out, refuses one that would tag a version unless
// asked to run it anyway, then one the user hasn't confirmed or trusted as it is now, and
// only then remembers a confirmation the user asked to keep, and returns the command.
func (s *Service) before(ctx context.Context, p pending) ([]string, error) {
	tool := s.tool(ctx)

	sc, err := s.find(ctx, p.c, p.src, p.name, tool, true)
	if err != nil {
		return nil, err
	}

	shown := runDisplay(sc, p.v, p.in.Args)

	if sc.look != p.look {
		return nil, uierr.New(fmt.Sprintf("%q changed as it was starting. Look at it again before it runs.", p.title),
			fmt.Errorf("%w: %s", ErrChanged, sc.entry.Ref))
	}

	if sc.hash != p.in.SHA256 {
		return nil, s.ask(ctx, p.c, sc, shown, fmt.Sprintf("%q changed in %s as it was starting. Look at it "+
			"again before it runs.", p.name, sourceFile(p.src)), ErrChanged)
	}

	if _, err = check(sc, p.in); err != nil {
		return nil, err
	}

	argv, err := s.command(ctx, p, sc, tool)
	if err != nil {
		return nil, err
	}

	remember, err := s.trusts(ctx, p.c, sc, p.conf, shown)
	if err != nil {
		return nil, err
	}

	if remember {
		key := trustKey{source: sc.entry.Source, name: sc.name}
		if err = s.trust.set(ctx, p.c.name, key, trustRecord{sha256: sc.hash, command: sc.entry.Command}); err != nil {
			return nil, err
		}
	}

	return argv, nil
}

// command works out the argv sc runs as p asks, with tool the copy of magex in use, and
// refuses one that would tag a version unless p asked to run it anyway: what it runs,
// inputs and aliases resolved, is checked, not only what the page showed.
func (s *Service) command(ctx context.Context, p pending, sc script, tool ToolFacts) ([]string, error) {
	parts := slices.Clone(sc.parts)
	if p.src == SourceMagex {
		parts[0].args = p.in.Args
	}

	argv, err := launcher{shell: s.shell(), magex: tool.Path, vars: p.v}.argv(parts)
	if err != nil {
		return nil, err
	}

	if !p.in.Anyway && bumps(argv, s.taggingAliases(ctx, tool)) {
		return nil, releaseError(sc)
	}

	return argv, nil
}

// taggingAliases are the other names the copy tool gives the targets that tag a version,
// from its built-ins as listed: none while magex can't run or couldn't list them.
func (s *Service) taggingAliases(ctx context.Context, tool ToolFacts) []string {
	if !tool.Usable() {
		return nil
	}

	b, err := s.builtinsOf(ctx, tool)
	if err != nil {
		return nil
	}

	return b.taggingAliases()
}

// trusts checks that the user trusts sc as it is now: the hash they confirmed, or else the
// one they trusted before; Run again takes only the latter. It reports whether to
// remember the confirmation, which the caller does once the command is worked out. A
// refusal carries the question to ask, with shown, the command as it will run.
func (s *Service) trusts(ctx context.Context, c clone, sc script, conf confirmation, shown string) (bool, error) {
	if !sc.needsTrust() {
		return false, nil
	}

	if conf.hash != "" {
		if conf.hash == sc.hash {
			return conf.remember, nil
		}

		return false, s.ask(ctx, c, sc, shown, fmt.Sprintf("%q changed since you confirmed it. Look at it again "+
			"before it runs.", sc.name), ErrChanged)
	}

	rec, ok, err := s.trust.get(ctx, c.name, trustKey{source: sc.entry.Source, name: sc.name})

	switch {
	case err != nil:
		return false, err
	case ok && rec.sha256 == sc.hash:
		return false, nil
	case conf.rerun:
		return false, s.ask(ctx, c, sc, shown, fmt.Sprintf("%q needs your OK to run again. Run it from Repo "+
			"scripts, which asks first.", sc.name), ErrNeedsTrust)
	case !ok:
		return false, s.ask(ctx, c, sc, shown, fmt.Sprintf("Confirm %q before it runs: it comes from %s, not "+
			"from you.", sc.name, sourceFile(sc.entry.Source)), ErrNeedsTrust)
	default:
		return false, s.ask(ctx, c, sc, shown, fmt.Sprintf("%q changed since you last ran it. Confirm it again "+
			"before it runs.", sc.name), ErrNeedsTrust)
	}
}

// ask refuses sc, saying msg, until the user answers its question: shown, the command as
// it will run, with what they trusted before, read under the clone's origin.
func (s *Service) ask(ctx context.Context, c clone, sc script, shown, msg string, why error) error {
	key := trustKey{source: sc.entry.Source, name: sc.name}
	records := map[trustKey]trustRecord{}

	if rec, ok, err := s.trust.get(ctx, c.name, key); err == nil && ok {
		records[key] = rec
	}

	e := withTrust(sc, records, c.display())

	return &QuestionError{
		Question: Question{
			Ref: e.Ref, Name: e.Name, Source: e.Source, Command: shown, Trust: e.Trust, Trusted: e.Trusted,
			TrustNote: e.TrustNote, SHA256: e.SHA256,
		},
		err: uierr.New(msg, fmt.Errorf("%w: %s", why, sc.entry.Ref)),
	}
}

// releaseError refuses a command that would tag a version: Release does that.
func releaseError(sc script) error {
	return uierr.New("Release does this, with its checks. Open Release, or run it anyway.",
		fmt.Errorf("%w: %s", ErrRelease, sc.entry.Ref))
}

// check returns why sc can't run as intent asks, or the values of its inputs: greyed, a
// version bump not asked to run anyway, arguments it doesn't take or that aren't words, or
// a value its input doesn't allow.
func check(sc script, in Intent) (map[string]string, error) {
	switch {
	case !sc.entry.Runnable:
		return nil, uierr.New(sc.entry.Why, fmt.Errorf("%w: %s", ErrCantRun, sc.entry.Ref))
	case sc.entry.Release && !in.Anyway:
		return nil, releaseError(sc)
	case len(in.Args) > 0 && !sc.entry.Args:
		return nil, uierr.New("Only magex's own targets take arguments.", ErrInvalidRequest)
	case len(in.Args) > maxArgs:
		return nil, uierr.New(fmt.Sprintf("A run takes up to %d arguments.", maxArgs), ErrInvalidRequest)
	}

	for _, a := range in.Args {
		if len(a) > maxArgBytes || !argWord.MatchString(a) {
			return nil, uierr.New(fmt.Sprintf("%q isn't an argument magex takes: write key=value, such as bump=minor.", a),
				ErrInvalidRequest)
		}
	}

	return inputValues(sc, in.Inputs)
}

// inputValues returns the value of each of sc's inputs: the one given, or its default. A
// pickString's must be one of its choices.
func inputValues(sc script, given map[string]string) (map[string]string, error) {
	out := map[string]string{}

	for _, in := range sortedInputs(sc.inputs) {
		v, ok := given[in.ID]
		if !ok {
			v = in.Default
		}

		switch {
		case len(v) > maxInput || hidden(v):
			return nil, uierr.New(fmt.Sprintf("The value for %s is too long, or holds a character it can't.", in.ID),
				ErrInvalidRequest)
		case in.Kind == InputPick && !slices.ContainsFunc(in.Options, func(o Option) bool { return o.Value == v }):
			return nil, uierr.New(fmt.Sprintf("%q isn't one of the choices for %s.", v, in.ID),
				fmt.Errorf("%w: input %s", ErrInvalidRequest, in.ID))
		}

		out[in.ID] = v
	}

	return out, nil
}

// runEnv is the environment a run gets: in the app, the login shell's, with magex's quiet
// settings for a magex target; in the terminal, only those settings, since the terminal's
// own shell sets the rest up. One of the user's scripts gets its Variables on top.
func runEnv(src Source, base []string, where integrationprefs.RunsIn) []string {
	if src == SourceMagex {
		return integrations.MageX().RunEnv(base, where)
	}

	if where == integrationprefs.RunsInTerminal {
		return nil
	}

	return base
}

// runDisplay is a run's display form: its parts as the page shows them, with the inputs
// filled in and a magex target's arguments after it, but never what the user's
// environment holds.
func runDisplay(sc script, v vars, args []string) string {
	shown := make([]string, 0, len(sc.parts))

	for i, p := range sc.parts {
		if i == 0 && p.kind == partMagex {
			p.args = args
		}

		shown = append(shown, v.expandInputs(p.display()))
	}

	return strings.Join(shown, " && ")
}

// nonNil returns list, or an empty list for nil.
func nonNil(list []string) []string {
	if list == nil {
		return []string{}
	}

	return list
}

// Rerunner works Run again out for script runs (scriptrun.Rerunner): the same script, with
// the same values and arguments, read again from its source, in the same clone. It asks
// nothing, so it runs a repository command only while the user trusts it as it is now, by
// a record kept under the clone's current origin; one confirmed once, or changed since, is
// refused, and the user runs it from the page, which asks. One of the user's own scripts
// runs as it is saved now, found by its ID, and is refused once deleted, and while it is
// set to ask before each run, which only the page asks. A pull pulls the same branch
// again, its Before checking everything afresh, and is refused once the clone is on
// another. Core registers it with the runner for scriptrun.KindScript and
// scriptrun.KindPull. It is its own type, not a method of the Service, which the window
// binds.
type Rerunner struct {
	s *Service
}

// NewRerunner returns the Rerunner of s's runs.
func NewRerunner(s *Service) Rerunner {
	return Rerunner{s: s}
}

// Rerun returns the spec of run again, where it ran.
func (r Rerunner) Rerun(ctx context.Context, run scriptrun.Record) (scriptrun.Spec, error) {
	switch run.Kind {
	case scriptrun.KindScript:
		return r.script(ctx, run)
	case scriptrun.KindPull:
		return r.pull(ctx, run)
	case scriptrun.KindRelease, scriptrun.KindHooks, scriptrun.KindChecks, scriptrun.KindUpdate:
	}

	return scriptrun.Spec{}, fmt.Errorf("%w: a %s run", ErrInvalidRequest, run.Kind)
}

// pull returns the spec of a pull again: the same branch, in the same clone.
func (r Rerunner) pull(ctx context.Context, run scriptrun.Record) (scriptrun.Spec, error) {
	if r.s.branchGit == nil {
		return scriptrun.Spec{}, ErrNotSetUp
	}

	in, err := scriptrun.Decode[PullIntent](run.Result.Intent)
	if err != nil {
		return scriptrun.Spec{}, fmt.Errorf("%w: the run's intent: %w", ErrInvalidRequest, err)
	}

	if in.Branch == "" {
		return scriptrun.Spec{}, fmt.Errorf("%w: a pull of no branch", ErrInvalidRequest)
	}

	if err = r.s.pullsOff(ctx); err != nil {
		return scriptrun.Spec{}, err
	}

	c, err := r.s.cloneAt(ctx, paths.Tilde(run.Dir, r.s.home))
	if err != nil {
		return scriptrun.Spec{}, err
	}

	return r.s.pullSpec(c, in.Branch), nil
}

// script returns the spec of a script again.
func (r Rerunner) script(ctx context.Context, run scriptrun.Record) (scriptrun.Spec, error) {
	in, err := scriptrun.Decode[Intent](run.Result.Intent)
	if err != nil {
		return scriptrun.Spec{}, fmt.Errorf("%w: the run's intent: %w", ErrInvalidRequest, err)
	}

	c, err := r.s.cloneAt(ctx, paths.Tilde(run.Dir, r.s.home))
	if err != nil {
		return scriptrun.Spec{}, err
	}

	cfg, err := r.s.config(ctx)
	if err != nil {
		return scriptrun.Spec{}, err
	}

	return r.s.spec(ctx, c, in, cmp.Or(run.RunsIn, integrationprefs.RunsInApp), cfg, confirmation{rerun: true})
}
