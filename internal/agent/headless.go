package agent

import (
	"errors"
	"fmt"
	"math"
	"strings"
)

// Each agent's headless half: what it can do when Autopilot runs it in the background, and
// how a run's options become its flags. Everything above it (the runner, the run view, the
// ledger) is the app's own and never names an agent; an agent claims background runs by
// implementing headlessSpec, and is supported only once its spec and its stream parser
// pass the runner's conformance suite.

// Headless is what an agent can do in the background, as its spec declares it. The engine,
// the builder, and readiness read these, never an agent's name.
type Headless struct {
	// Background reports an agent that runs headless in the background at all.
	Background bool `json:"background"`

	// Budget reports an agent that keeps to a per-run budget in dollars of its own, so its
	// run may go on once the app quits; Cost one that reports what a run cost, and Usage
	// one that reports the tokens it used, which a cap in tokens counts.
	Budget bool `json:"budget"`
	Cost   bool `json:"cost"`
	Usage  bool `json:"usage"`

	// Structured reports an agent that returns the run's report in the shape asked.
	Structured bool `json:"structured"`

	// Resume reports an agent whose background run the user can take over in the terminal.
	Resume bool `json:"resume"`

	// MinVersion is the oldest version whose flags every background run uses, "" for an
	// agent with no background runs.
	MinVersion string `json:"minVersion"`
}

// HeadlessOptions are what a background run passes its agent, besides the prompt, which
// it reads on stdin.
type HeadlessOptions struct {
	// Model and Effort are "" for the agent's own default.
	Model  string
	Effort string

	// Access is the fence's (AccessFence), as the launcher's permission rule chose it, in
	// the run's folder.
	Access Access

	// SessionID is the ID the app minted for the run's session.
	SessionID string

	// Schema is the JSON schema of the report the agent returns, on one line, or "".
	Schema string

	// BudgetUSD is the run's budget in dollars, which an agent with Headless.Budget keeps
	// to itself; 0 sets none.
	BudgetUSD float64

	// SystemNote is the system note every unattended run gets, its fence's line included,
	// or "".
	SystemNote string

	// Extra are the profile's, the prompt's, and the launch default's extra arguments,
	// checked with CheckFenced.
	Extra []string
}

// headlessSpec is the headless half of an agent that runs in the background.
type headlessSpec interface {
	spec

	// headless declares what it can do in the background.
	headless() Headless

	// headlessArgs returns a background run's flags, in order.
	headlessArgs(o HeadlessOptions) []string

	// dropEnv names the variables that steer the agent itself, which a background run's
	// environment leaves out; one ending in "*" names every variable with that prefix.
	dropEnv() []string
}

var (
	// ErrNoBackground is returned for an agent that can't run in the background.
	ErrNoBackground = errors.New("the agent can't run in the background")

	// ErrInvalidBudget is returned for a budget that isn't a positive amount.
	ErrInvalidBudget = errors.New("invalid budget")

	// ErrInvalidHeadless is returned for headless options a run can't start with.
	ErrInvalidHeadless = errors.New("invalid background run")
)

// headlessFor returns the headless half of the agent with id, or ErrNoBackground.
func headlessFor(id ID) (headlessSpec, error) {
	s, err := specFor(id)
	if err != nil {
		return nil, err
	}

	h, ok := s.(headlessSpec)
	if !ok || !h.headless().Background {
		return nil, fmt.Errorf("%w: %s", ErrNoBackground, s.info().Name)
	}

	return h, nil
}

// HeadlessOf returns what the agent with id can do in the background: nothing for an
// agent with no headless half yet, and an error only for an agent this build doesn't know.
func HeadlessOf(id ID) (Headless, error) {
	s, err := specFor(id)
	if err != nil {
		return Headless{}, err
	}

	if h, ok := s.(headlessSpec); ok {
		return h.headless(), nil
	}

	return Headless{}, nil
}

// HeadlessArgs returns the flags a background run passes the agent with id, in order,
// once each option checks out: the stream, nobody to answer, the session, the model and
// effort, the fence's access (its preset's mode and tools, no MCP server, and its
// settings), the budget, the report's schema, the system note, and the extra arguments.
// The prompt comes on stdin, never on the command line.
func HeadlessArgs(id ID, o HeadlessOptions) ([]string, error) {
	h, err := headlessFor(id)
	if err != nil {
		return nil, err
	}

	if err = checkHeadless(id, o); err != nil {
		return nil, err
	}

	return h.headlessArgs(o), nil
}

// checkHeadless checks a background run's options for the agent with id.
func checkHeadless(id ID, o HeadlessOptions) error {
	if err := ValidateModel(o.Model); err != nil {
		return err
	}

	if err := ValidateEffort(id, o.Effort); err != nil {
		return err
	}

	if o.Access.Kind != AccessFence {
		return fmt.Errorf("%w: a background run is always fenced", ErrInvalidHeadless)
	}

	if err := validateAccess(id, o.Access); err != nil {
		return err
	}

	if err := checkHeadlessValues(o); err != nil {
		return err
	}

	if err := CheckAccess(id, o.Extra); err != nil {
		return err
	}

	return CheckFenced(id, o.Extra)
}

// checkHeadlessValues checks a background run's session, budget, schema, and system
// prompt.
func checkHeadlessValues(o HeadlessOptions) error {
	switch {
	case !ValidSessionID(o.SessionID):
		return fmt.Errorf("%w %q", ErrInvalidSessionID, o.SessionID)
	case o.BudgetUSD < 0 || math.IsNaN(o.BudgetUSD) || math.IsInf(o.BudgetUSD, 0):
		return fmt.Errorf("%w: %v dollars", ErrInvalidBudget, o.BudgetUSD)
	case strings.ContainsAny(o.Schema, "\n\x00") || strings.ContainsRune(o.SystemNote, 0):
		return fmt.Errorf("%w: the schema is one line, and nothing holds a NUL byte", ErrInvalidHeadless)
	default:
		return nil
	}
}

// HeadlessEnv returns env without the variables that steer the agent with id itself, as
// a session the agent runs inside would leave them for a child (Claude Code's CLAUDECODE,
// CLAUDE_CODE_*, and the like), so a background run never takes on another session's
// settings. env itself is never changed, and the copy is never nil.
func HeadlessEnv(id ID, env []string) []string {
	out := make([]string, 0, len(env))

	h, err := headlessFor(id)
	if err != nil {
		return append(out, env...)
	}

	drop := h.dropEnv()

	for _, e := range env {
		name, _, _ := strings.Cut(e, "=")
		if !dropped(name, drop) {
			out = append(out, e)
		}
	}

	return out
}

// dropped reports whether the variable name is among drop: by its whole name, or by a
// prefix that ends in "*".
func dropped(name string, drop []string) bool {
	for _, d := range drop {
		if prefix, ok := strings.CutSuffix(d, "*"); ok && strings.HasPrefix(name, prefix) || name == d {
			return true
		}
	}

	return false
}
