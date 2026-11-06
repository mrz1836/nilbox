// Package agents lists the coding tools a launch can start.
//
// Only Claude Code ships today. Launch defaults already store a tool per org or
// repository ("" meaning the app's default), so another tool can be added here without
// changing what is stored.
package agents

import (
	"errors"
	"fmt"
)

// Claude is the ID of Claude Code, the default tool.
const Claude = "claude"

// ErrUnknownAgent is returned for a tool ID this build does not know.
var ErrUnknownAgent = errors.New("unknown coding tool")

// Agent is a coding tool the launcher can start.
type Agent struct {
	// ID is what launch defaults store, such as "claude".
	ID string `json:"id"`

	// Name is what the UI shows, as in "Open in Claude".
	Name string `json:"name"`

	// Command is the program a launch script runs.
	Command string `json:"command"`
}

// All returns every tool, the default first.
func All() []Agent {
	return []Agent{{ID: Claude, Name: "Claude", Command: "claude"}}
}

// Default returns the tool used when no launch default names one.
func Default() Agent {
	return All()[0]
}

// Lookup returns the tool with id, or the default tool for "".
func Lookup(id string) (Agent, error) {
	if id == "" {
		return Default(), nil
	}

	for _, a := range All() {
		if a.ID == id {
			return a, nil
		}
	}

	return Agent{}, fmt.Errorf("%w %q", ErrUnknownAgent, id)
}
