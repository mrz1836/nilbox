package setup

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/mrz1836/nilbox/internal/event"
	"github.com/mrz1836/nilbox/internal/prefs/prefval"
	"github.com/mrz1836/nilbox/internal/uierr"
)

// The setup.* preferences, which the setup alone writes.
const (
	// KeyDone says the setup is done: the user finished or skipped it, or Adopt adopted an
	// install from before it as done.
	KeyDone = "setup.done"

	// KeyStep is the step the user was on, where the setup opens again.
	KeyStep = "setup.step"
)

// ErrUnknownStep is returned for a step the setup doesn't have.
var ErrUnknownStep = errors.New("unknown setup step")

// Step is one of the setup's steps.
type Step string

// The steps, in their order. Tools shows only when the user's tools or clones make it fit,
// which the window decides.
const (
	StepWelcome       Step = "welcome"
	StepGitHub        Step = "github"
	StepClones        Step = "clones"
	StepAgents        Step = "agents"
	StepNotifications Step = "notifications"
	StepTools         Step = "tools"
	StepFinish        Step = "finish"
)

// Steps returns every step, in order.
func Steps() []Step {
	return []Step{StepWelcome, StepGitHub, StepClones, StepAgents, StepNotifications, StepTools, StepFinish}
}

// Valid reports whether s is one of the steps.
func (s Step) Valid() bool {
	return slices.Contains(Steps(), s)
}

// State is where the setup stands.
type State struct {
	// Done reports that the setup is done: the window shows the inbox, not the setup.
	Done bool `json:"done"`

	// Step is the step the setup opens on: the one the user was on when they left it, or
	// Welcome before any.
	Step Step `json:"step"`
}

// State returns where the setup stands. A database whose state Adopt never stored, which
// only a failed write leaves, reads as done, so a passing problem never traps a user in the
// setup.
func (s *Service) State(ctx context.Context) (State, error) {
	return readState(ctx, s.memory)
}

// SetStep remembers that the user is on step, so quitting part way through opens the
// setup there again, and returns the state with it.
func (s *Service) SetStep(ctx context.Context, step Step) (State, error) {
	if !step.Valid() {
		return State{}, uierr.New("That step isn't part of the setup.", fmt.Errorf("%w: %q", ErrUnknownStep, step))
	}

	if err := s.memory.SetMany(ctx, map[string]string{KeyStep: string(step)}); err != nil {
		return State{}, fmt.Errorf("remember the setup's step: %w", err)
	}

	return s.State(ctx)
}

// Finish marks the setup done, as the user finished or skipped it, and returns the state:
// the window shows the inbox from now on, and the desktop shell, which an
// event.SetupFinished tells, asks to allow notification banners if it hasn't. Its next
// run (Restart) opens on Welcome.
func (s *Service) Finish(ctx context.Context) (State, error) {
	err := s.memory.SetMany(ctx, map[string]string{KeyDone: prefval.EncodeBool(true), KeyStep: string(StepWelcome)})
	if err != nil {
		return State{}, fmt.Errorf("finish the setup: %w", err)
	}

	s.events.Emit(event.SetupFinished{})

	return s.State(ctx)
}

// Restart opens the setup again from Welcome, as Settings → About's Run setup again asks,
// and returns the state. Every step shows what is set already, so it doubles as a check of
// the setup.
func (s *Service) Restart(ctx context.Context) (State, error) {
	err := s.memory.SetMany(ctx, map[string]string{KeyDone: prefval.EncodeBool(false), KeyStep: string(StepWelcome)})
	if err != nil {
		return State{}, fmt.Errorf("run the setup again: %w", err)
	}

	return s.State(ctx)
}

// readState reads the setup's state from memory, as State says.
func readState(ctx context.Context, memory Memory) (State, error) {
	stored, err := memory.GetMany(ctx, KeyDone, KeyStep)
	if err != nil {
		return State{}, fmt.Errorf("read the setup's state: %w", err)
	}

	step := Step(stored[KeyStep])
	if !step.Valid() {
		step = StepWelcome
	}

	done, decided := stored[KeyDone]

	return State{Done: !decided || prefval.Bool(done, true), Step: step}, nil
}
