package scripts

import (
	"cmp"
	"context"
	"encoding/json"
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

	// RunsIn is where it runs, or "" for the setting.
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
// to remember it.
type confirmation struct {
	hash     string
	remember bool
}

// Run runs the script req names in its clone, and returns the run's ID. A command from the
// repository's files runs only as the user confirmed it, or as they trusted it before:
// the run's Before, under the clone's lock, reads its source again and refuses a command
// whose hash isn't that one.
func (s *Service) Run(ctx context.Context, req RunRequest) (int64, error) {
	if s.runs == nil {
		return 0, ErrNotSetUp
	}

	if req.RunsIn != "" && !req.RunsIn.Valid() {
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

	where := cmp.Or(req.RunsIn, cfg.RunsIn, integrationprefs.RunsInApp)
	in := Intent{Ref: req.Ref, Inputs: req.Inputs, Args: req.Args, Anyway: req.Anyway}

	spec, err := s.spec(ctx, c, in, where, cfg, confirmation{hash: req.Confirmed, remember: req.Remember})
	if err != nil {
		return 0, err
	}

	id, err := s.runs.Start(ctx, spec)
	if err != nil {
		return id, err
	}

	s.logger.InfoContext(ctx, "a script started", "run", id, "source", spec.Source, "repo", c.display(),
		"in", string(where))

	return id, nil
}

// spec is the run of the script in intent, in c: worked out now, for its title and its
// display form, and again by its Before under the clone's lock, which refuses it unless it
// is still what was worked out now and what the user trusts.
func (s *Service) spec(ctx context.Context, c clone, in Intent, where integrationprefs.RunsIn,
	cfg integrationprefs.Config, conf confirmation,
) (scriptrun.Spec, error) {
	src, name, ok := parseRef(in.Ref)
	if !ok || src == SourceYours {
		return scriptrun.Spec{}, fmt.Errorf("%w: the script %q", ErrInvalidRequest, in.Ref)
	}

	if why := sourceOff(cfg, src); why != "" {
		return scriptrun.Spec{}, uierr.New(why, ErrCantRun)
	}

	sc, err := s.find(ctx, c, src, name, s.tool(ctx), false)
	if err != nil {
		return scriptrun.Spec{}, err
	}

	if in.Inputs, err = check(sc, in); err != nil {
		return scriptrun.Spec{}, err
	}

	in.SHA256, in.Args = sc.hash, nonNil(in.Args)

	base, _ := s.env()
	v := vars{dir: c.dir, home: s.home, env: base, inputs: in.Inputs}

	spec := scriptrun.Spec{
		Kind: scriptrun.KindScript, Title: sc.entry.Name, Source: sourceFile(src), Repo: c.name, Dir: c.dir,
		Env: runEnv(src, base, where), Command: runDisplay(sc, v, in.Args), RunsIn: where, Intent: in,
		Before: func(ctx context.Context) ([]string, error) { return s.before(ctx, c, src, name, in, conf, v) },
	}

	if src == SourceMagex {
		spec.Tool = string(integrationprefs.MageX)
	}

	return spec, nil
}

// before is a script run's Before: under the clone's lock, it reads the script's source
// again, from its bytes, and refuses it when its hash isn't the one worked out as the run
// was asked for, or when the user hasn't confirmed or trusted it as it is now; then it
// remembers a confirmation the user asked to keep, and returns the command.
func (s *Service) before(ctx context.Context, c clone, src Source, name string, in Intent, conf confirmation, v vars,
) ([]string, error) {
	tool := s.tool(ctx)

	sc, err := s.find(ctx, c, src, name, tool, true)
	if err != nil {
		return nil, err
	}

	if sc.hash != in.SHA256 {
		return nil, uierr.New(fmt.Sprintf("%q changed in %s as it was starting. Look at it again before it runs.",
			name, sourceFile(src)), fmt.Errorf("%w: %s", ErrChanged, in.Ref))
	}

	if _, err = check(sc, in); err != nil {
		return nil, err
	}

	if err = s.trusts(ctx, c, sc, conf); err != nil {
		return nil, err
	}

	parts := slices.Clone(sc.parts)
	if src == SourceMagex {
		parts[0].args = in.Args
	}

	return launcher{shell: s.shell(), magex: tool.Path, vars: v}.argv(parts)
}

// trusts checks that the user trusts sc as it is now: the hash they confirmed, or else the
// one they trusted before. A confirmation they asked to keep is remembered.
func (s *Service) trusts(ctx context.Context, c clone, sc script, conf confirmation) error {
	if !sc.needsTrust() {
		return nil
	}

	key := trustKey{source: sc.entry.Source, name: sc.name}

	if conf.hash != "" {
		if conf.hash != sc.hash {
			return uierr.New(fmt.Sprintf("%q changed since you confirmed it. Look at it again before it runs.", sc.name),
				fmt.Errorf("%w: %s", ErrChanged, sc.entry.Ref))
		}

		if conf.remember {
			return s.trust.set(ctx, c.name, key, trustRecord{sha256: sc.hash, command: sc.entry.Command})
		}

		return nil
	}

	rec, ok, err := s.trust.get(ctx, c.name, key)

	switch {
	case err != nil:
		return err
	case !ok:
		return uierr.New(fmt.Sprintf("Confirm %q before it runs: it comes from %s, not from you.", sc.name,
			sourceFile(sc.entry.Source)), fmt.Errorf("%w: %s", ErrNeedsTrust, sc.entry.Ref))
	case rec.sha256 != sc.hash:
		return uierr.New(fmt.Sprintf("%q changed since you last ran it. Confirm it again before it runs.", sc.name),
			fmt.Errorf("%w: %s", ErrNeedsTrust, sc.entry.Ref))
	default:
		return nil
	}
}

// check returns why sc can't run as intent asks, or the values of its inputs: greyed, a
// version bump not asked to run anyway, arguments it doesn't take or that aren't words, or
// a value its input doesn't allow.
func check(sc script, in Intent) (map[string]string, error) {
	switch {
	case !sc.entry.Runnable:
		return nil, uierr.New(sc.entry.Why, fmt.Errorf("%w: %s", ErrCantRun, sc.entry.Ref))
	case sc.entry.Release && !in.Anyway:
		return nil, uierr.New("Release does this, with its checks. Open Release, or run it anyway.",
			fmt.Errorf("%w: %s", ErrRelease, sc.entry.Ref))
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
		case len(v) > maxInput || strings.ContainsRune(v, 0):
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
// own shell sets the rest up.
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
// the same values and arguments, read again from its source, in the same clone. It runs
// only while the command's hash is the one that ran before; one that changed is refused,
// and the user looks at it again on the page. Core registers it with the runner for
// scriptrun.KindScript. It is its own type, not a method of the Service, which the window
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
	if run.Kind != scriptrun.KindScript {
		return scriptrun.Spec{}, fmt.Errorf("%w: a %s run", ErrInvalidRequest, run.Kind)
	}

	var in Intent
	if err := decodeIntent(run.Result.Intent, &in); err != nil {
		return scriptrun.Spec{}, err
	}

	c, err := r.s.cloneAt(ctx, paths.Tilde(run.Dir, r.s.home))
	if err != nil {
		return scriptrun.Spec{}, err
	}

	if !c.name.Valid() {
		c.name = run.Repo
	}

	cfg, err := r.s.config(ctx)
	if err != nil {
		return scriptrun.Spec{}, err
	}

	return r.s.spec(ctx, c, in, cmp.Or(run.RunsIn, integrationprefs.RunsInApp), cfg, confirmation{hash: in.SHA256})
}

// decodeIntent reads a run's intent back from its record, where it was decoded as plain
// JSON values.
func decodeIntent(recorded, into any) error {
	data, err := json.Marshal(recorded)
	if err == nil {
		err = json.Unmarshal(data, into)
	}

	if err != nil {
		return fmt.Errorf("%w: the run's intent: %w", ErrInvalidRequest, err)
	}

	return nil
}
