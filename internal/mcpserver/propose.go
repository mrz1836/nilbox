package mcpserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/mrz1836/nilbox/internal/autopilot/kit"
	"github.com/mrz1836/nilbox/internal/brand"
	"github.com/mrz1836/nilbox/internal/textutil"
)

// propose answers autopilot_propose: the proposal raw holds, checked against the whole
// snapshot as the app checks its card, refused with any problem, and otherwise written
// for the user to review, into a folder the app made with room for it.
func (s *server) propose(ctx context.Context, raw json.RawMessage) answer {
	p, err := proposalOf(raw)
	if err != nil {
		return proposalRefusal(kit.ToolPropose, err)
	}

	dir, err := s.proposalsDir()
	if err == nil {
		err = s.room(dir)
	}

	if err != nil {
		return s.folderRefusal(err)
	}

	snap, err := s.snapshot()
	if err != nil {
		return snapshotRefusal(err)
	}

	checked := kit.Check(ctx, snap, p)
	if checked.Problems() > 0 {
		return refusal("Nothing was proposed: the user never sees a proposal with a problem.\n\n" + checked.Words())
	}

	id, err := s.write(dir, snap, p)
	if err != nil {
		return refusal(textutil.WithReason("Nothing was proposed: the proposal couldn't be written", reason(err)) +
			" Try again, and if it fails again, tell the user.")
	}

	return said(proposedWords(id, checked, s.where))
}

// folderRefusal refuses a proposal its folder can't take: err says why.
func (s *server) folderRefusal(err error) answer {
	switch {
	case errors.Is(err, errFull):
		return refusal(fmt.Sprintf("Nothing was proposed: %d proposals from Claude Code already wait for the user in "+
			"%s's Autopilot tab. Ask them to add or discard some there, then propose it again.",
			kit.MaxOutsidePending, brand.Name))
	case errors.Is(err, errNoFolder) && s.where == kit.InApp:
		return refusal("Nothing was proposed: this conversation's folder for proposals is gone. Ask the user to " +
			"start a new conversation in Describe it.")
	case errors.Is(err, errNoFolder):
		return refusal("Nothing was proposed: " + brand.Name + " hasn't made its folder for proposals from Claude " +
			"Code yet. Ask the user to open Describe it in " + brand.Name + "'s Autopilot tab once, then propose it " +
			"again.")
	default:
		return refusal(textutil.WithReason("Nothing was proposed: the folder for proposals can't be read", reason(err)) +
			" Try again, and if it fails again, tell the user.")
	}
}

// proposedWords says what the proposal id, checked as c, put in the Autopilot tab, and how
// the agent hears what the user did with it, where w says it runs.
func proposedWords(id string, c kit.Checked, w kit.Where) string {
	hear := " The user's next message says what they did with it."
	if w == kit.Outside {
		hear = " Read `" + kit.ToolProposalStatus + "` to hear what they did with it."
	}

	return "Proposed `" + id + "`: " + textutil.Series(checkedParts(c)) + ". It waits for the user in " +
		brand.Name + "'s Autopilot tab, where they add, test, change, or discard each part; nothing changes until " +
		"they apply it." + hear
}

// checkedParts says each part of a checked proposal, in the order a card applies them: a
// new routine by the name it is saved under, and a change by the routine's own.
func checkedParts(c kit.Checked) []string {
	parts := make([]string, 0, len(c.Prompts)+len(c.Routines)+2)

	for _, p := range c.Prompts {
		parts = append(parts, "the new prompt "+quoted(p.Prompt.Nickname))
	}

	for _, r := range c.Routines {
		if r.Before != nil {
			parts = append(parts, "a change to the routine "+quoted(r.Before.Name))
		} else {
			parts = append(parts, "the new routine "+quoted(r.Name))
		}
	}

	if c.Settings != nil {
		parts = append(parts, "a change to Autopilot's settings")
	}

	if c.Rules != nil {
		parts = append(parts, "a change to the rules routines lean on")
	}

	return parts
}

// quoted writes a name in curly quotes, one line of at most 80 characters.
func quoted(name string) string {
	return "“" + textutil.Truncate(textutil.OneLine(name), 80) + "”"
}
