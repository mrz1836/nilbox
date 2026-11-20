package describeit

import (
	"errors"
	"fmt"
	"strconv"

	"github.com/mrz1836/nilbox/internal/agent"
	"github.com/mrz1836/nilbox/internal/brand"
)

// agentWords is what the window calls the agent a conversation runs: Describe it talks
// with Claude Code alone (agent.Claude).
const agentWords = "Claude Code"

// Why a call is refused, each worded for the window with uierr.
var (
	// errBusy is returned while a turn runs, for what must wait for it.
	errBusy = errors.New("a turn is running")

	// errClosing is returned once the app quits.
	errClosing = errors.New("the app is quitting")

	// errEmpty is returned for a message with nothing to say.
	errEmpty = errors.New("the message is empty")

	// errTooLong is returned for a message past maxMessageRunes, or too many attachments.
	errTooLong = errors.New("the message is too long")

	// errNotReady is returned while Claude Code can't run a turn.
	errNotReady = errors.New("the agent can't run a turn")

	// errNoAgents is returned by a service built without Agents.
	errNoAgents = errors.New("describe it reaches no agent")

	// errNoAttachments is returned by a service built without Attachments, for a message
	// that attaches something.
	errNoAttachments = errors.New("describe it reads no attachments")

	// errAttachKind is returned for an attachment of a kind there isn't.
	errAttachKind = errors.New("no such kind of attachment")

	// errNoProfile is returned for a profile the agent doesn't have.
	errNoProfile = errors.New("no such profile")

	// errNoExecutable is returned when the app can't tell its own executable, which a turn's
	// tools run.
	errNoExecutable = errors.New("the app's executable is unknown")

	// errNoConversation is returned for a conversation or a turn that isn't there.
	errNoConversation = errors.New("no such conversation")

	// errNoCard is returned for a card that isn't there, or a part it doesn't have.
	errNoCard = errors.New("no such card")

	// errOutcome is returned for an outcome a part can't have.
	errOutcome = errors.New("not an outcome of that part")

	// errUnstable is returned by an install from an app that can't point the plugin's link
	// at itself.
	errUnstable = errors.New("the app doesn't run from a stable place")

	// errPlugin is returned for a plugin command that failed.
	errPlugin = errors.New("the plugin command failed")
)

// busyWords say a turn runs, for what waits for it.
const busyWords = "Claude is still working on your last message. Wait for it, or stop it first."

// closingWords say the app quits.
const closingWords = brand.Name + " is quitting, so Describe it won't start anything now."

// floorWords say Claude Code at version is older than Describe it needs.
func floorWords(version string) (problem, fix string) {
	return fmt.Sprintf("%s %s is older than Describe it needs (%s).", agentWords, version,
			agent.ConversationFloor(agent.Claude)),
		"Update " + agentWords + ", then try again."
}

// budgetWords say what a turn may spend.
func budgetWords() string {
	return "$" + strconv.FormatFloat(turnBudgetUSD, 'f', -1, 64)
}
