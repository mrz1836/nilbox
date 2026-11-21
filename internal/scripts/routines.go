package scripts

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"

	"github.com/mrz1836/nilbox/internal/integrationprefs"
	"github.com/mrz1836/nilbox/internal/scriptrun"
	"github.com/mrz1836/nilbox/internal/textutil"
	"github.com/mrz1836/nilbox/internal/uierr"
)

// Routines is how Autopilot's routines run the user's own scripts (Run one of my scripts):
// the scripts as a routine picks from them and checks them, each with its look, and a run
// of one in a clone, worked out exactly as Run works it out, always in the app, its end told
// back once recorded. The window's Run leaves Ask before running to the window; a routine's
// run is asked about by Autopilot, which checks the look the user confirmed, and Run
// refuses a script saved otherwise since. It is its own type, not a method of the Service,
// which the window binds, since a RoutineRun holds a function.
type Routines struct {
	s *Service
}

// NewRoutines returns the routines' way into s.
func NewRoutines(s *Service) Routines {
	return Routines{s: s}
}

// RoutineScript is one of the user's own scripts as a routine reads it: the script, its
// look (what a run compares, so what runs is what the user confirmed), and which clones it
// shows in, in words.
type RoutineScript struct {
	UserScript

	Look  string
	Shows string
}

// Mine returns the user's own scripts, in their order.
func (r Routines) Mine(ctx context.Context) ([]RoutineScript, error) {
	all, err := r.s.yours.all(ctx)
	if err != nil {
		return nil, uierr.New("Couldn't read your scripts.", err)
	}

	out := make([]RoutineScript, 0, len(all))
	for _, u := range all {
		out = append(out, RoutineScript{UserScript: u, Look: yoursScript(u).look, Shows: showsWords(u)})
	}

	return out, nil
}

// showsWords says which clones a script shows in.
func showsWords(u UserScript) string {
	switch u.Scope {
	case ScopeGo:
		return "Go repositories"
	case ScopeRepos:
		return textutil.Series(u.Repos)
	case ScopeAll:
	}

	return "every clone"
}

// RoutineRun is a routine's run of one of the user's scripts.
type RoutineRun struct {
	// ID is the script's, and Dir the clone it runs in, as the page names a clone, with ~.
	ID  int64
	Dir string

	// Look is the script as the routine checked it, which the run must still be: one
	// saved otherwise since is refused (ErrChanged), so a script whose Ask before running
	// the user answered runs only as they saw it. "" checks nothing more than Run does.
	Look string

	// Ended hears how the run ended once its end is recorded, never for one left going as
	// the app quit; nil hears nothing.
	Ended func(ctx context.Context, id int64, end RoutineEnd)
}

// RoutineEnd is how a routine's run of a script ended: its outcome as recorded, and what
// ended it when the run engine did, the user's Stop, its time limit, or the app quitting.
type RoutineEnd struct {
	Outcome scriptrun.Outcome

	Stopped  bool
	TimedOut bool
	Quit     bool
}

// endings keeps how a run ended, from its Finish, for its Recorded, which comes after.
type endings struct {
	mu  sync.Mutex
	end scriptrun.Ending
}

// keep records end, and returns out as the engine worked it out.
func (e *endings) keep(_ context.Context, end scriptrun.Ending, out scriptrun.Outcome) scriptrun.Outcome {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.end = end

	return out
}

// of returns how the run ended, with out, its outcome as recorded.
func (e *endings) of(out scriptrun.Outcome) RoutineEnd {
	e.mu.Lock()
	defer e.mu.Unlock()

	return RoutineEnd{Outcome: out, Stopped: e.end.Stopped, TimedOut: e.end.TimedOut, Quit: e.end.Quit}
}

// Run runs the script req names in its clone, in the app, and returns the run's ID: the
// run Run works out, whatever the script says of where it runs, with its end told to
// req.Ended. The script must show in the clone, as on the page, and never runs anyway: one
// that would bump a version or upgrade the dependencies is refused (ErrRelease, ErrDeps).
func (r Routines) Run(ctx context.Context, req RoutineRun) (int64, error) {
	s := r.s
	if s.runs == nil {
		return 0, ErrNotSetUp
	}

	c, err := s.cloneAt(ctx, req.Dir)
	if err != nil {
		return 0, err
	}

	cfg, err := s.config(ctx)
	if err != nil {
		return 0, err
	}

	in := Intent{Ref: yoursRef(req.ID), Inputs: map[string]string{}, Args: []string{}}

	spec, err := s.spec(ctx, c, in, integrationprefs.RunsInApp, cfg, confirmation{})
	if err != nil {
		return 0, err
	}

	if req.Look != "" {
		if err = r.sameLook(ctx, c, req); err != nil {
			return 0, err
		}
	}

	if req.Ended != nil {
		kept := &endings{}
		spec.Finish = kept.keep
		spec.Recorded = func(ctx context.Context, id int64, out scriptrun.Outcome) { req.Ended(ctx, id, kept.of(out)) }
	}

	id, err := s.runs.Start(ctx, spec)
	if err != nil {
		return id, err
	}

	s.logger.InfoContext(ctx, "a routine's script started", "run", id, "repo", c.display())

	return id, nil
}

// sameLook refuses req when its script isn't the one the routine checked: saved since
// with another name, command, place, or Ask before running.
func (r Routines) sameLook(ctx context.Context, c clone, req RoutineRun) error {
	sc, err := r.s.find(ctx, c, SourceYours, strconv.FormatInt(req.ID, 10), r.s.tool(ctx), true)
	if err != nil {
		return err
	}

	if sc.look == req.Look {
		return nil
	}

	return uierr.New(fmt.Sprintf("%q changed since you said to run it. Look at it again before it runs.",
		strings.TrimSpace(sc.entry.Name)), fmt.Errorf("%w: %s", ErrChanged, sc.entry.Ref))
}
