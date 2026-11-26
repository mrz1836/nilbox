package setup

import (
	"context"

	"github.com/mrz1836/nilbox/internal/launcher/terminal"
)

// TerminalChoice is a terminal launches can open in, as the Agents step offers it.
type TerminalChoice struct {
	// ID is the terminal, as the terminal setting stores it, and Name what it is called.
	ID   terminal.ID `json:"id"`
	Name string      `json:"name"`

	// Installed reports that the terminal is on this Mac. Terminal always is.
	Installed bool `json:"installed"`
}

// Terminals returns the terminals launches can open in, and whether each is installed, in
// the order a choice prefers them (iTerm2, Warp, then Terminal), so the Agents step moves
// a user whose terminal isn't installed to the first one that is.
// It reads only whether each app is there; a system without terminal support has none.
func (s *Service) Terminals(context.Context) []TerminalChoice {
	out := []TerminalChoice{}
	if s.terminals == nil {
		return out
	}

	for _, c := range s.terminals() {
		out = append(out, TerminalChoice{ID: c.ID, Name: c.Name, Installed: c.Installed})
	}

	return out
}
