package mcpserver

import (
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/mrz1836/nilbox/internal/autopilot/kit"
	"github.com/mrz1836/nilbox/internal/textutil"
)

// The tool mode's flags. The protocol is versioned in the flag's name, so a session that
// outlives an update calls the new build in the protocol it started with, and a build that
// doesn't know a version refuses it rather than start as the app.
const (
	// Flag asks for the tool mode, protocol version 1, with the data folder's describe
	// folder as its value: --mcp-server-v1=<describe folder>.
	Flag = "--mcp-server-v1"

	// flagPrefix starts the tool mode's flag in every protocol version.
	flagPrefix = "--mcp-server"

	// ConversationFlag names the in-app conversation the tool mode serves, by its UUID:
	// --conversation=<uuid>. Without it, the tool mode serves the user's own Claude Code
	// (kit.Outside).
	ConversationFlag = "--conversation"
)

// errCall is a call of the tool mode this build can't serve: its flag names a protocol the
// build doesn't know, or no describe folder, or its conversation isn't one.
var errCall = errors.New("the tool mode can't serve this call")

// conversationPattern is what an in-app conversation's ID looks like, as kit holds a
// proposal file's conversation to it: a UUID, so the folder it names is one of the
// proposals folder's own.
var conversationPattern = regexp.MustCompile(
	`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`,
)

// Arg returns the tool mode's argument for the describe folder dir.
func Arg(dir string) string {
	return Flag + "=" + dir
}

// ConversationArg returns the argument naming the in-app conversation id.
func ConversationArg(id string) string {
	return ConversationFlag + "=" + id
}

// IsCall reports arguments that ask for the tool mode, in any protocol version.
func IsCall(args []string) bool {
	for _, a := range args {
		if strings.HasPrefix(a, flagPrefix) {
			return true
		}
	}

	return false
}

// call is what the tool mode's arguments ask for.
type call struct {
	// dir is the describe folder: the snapshot the app wrote, and the proposals' folders.
	dir string

	// conversation is the in-app conversation the tool mode serves, by its ID, "" for the
	// user's own Claude Code.
	conversation string
}

// where says where the agent the call serves runs: in the app for a conversation, else in
// the user's own Claude Code.
func (c call) where() kit.Where {
	if c.conversation != "" {
		return kit.InApp
	}

	return kit.Outside
}

// readCall reads the tool mode's arguments: its flag, in the one protocol version this
// build speaks, with the describe folder as an absolute path, and, in the app, the
// conversation's ID. Every other argument is left alone.
func readCall(args []string) (call, error) {
	var c call

	for _, a := range args {
		var err error

		switch {
		case strings.HasPrefix(a, flagPrefix):
			c.dir, err = folderValue(a, c.dir)
		case a == ConversationFlag || strings.HasPrefix(a, ConversationFlag+"="):
			c.conversation, err = conversationValue(a, c.conversation)
		}

		if err != nil {
			return call{}, err
		}
	}

	if c.dir == "" {
		return call{}, fmt.Errorf("%w: no describe folder was given", errCall)
	}

	return c, nil
}

// folderValue reads the describe folder from the tool mode's flag a, refusing a protocol
// this build doesn't know, a folder that isn't an absolute path, and a second flag beside
// the one that gave given.
func folderValue(a, given string) (string, error) {
	dir, ok := strings.CutPrefix(a, Flag+"=")

	switch {
	case !ok:
		return "", fmt.Errorf("%w: this build doesn't know the tool mode's protocol of %q", errCall,
			textutil.Truncate(strings.SplitN(a, "=", 2)[0], 40))
	case given != "":
		return "", fmt.Errorf("%w: the describe folder is given twice", errCall)
	case !filepath.IsAbs(dir):
		return "", fmt.Errorf("%w: the describe folder isn't an absolute path", errCall)
	}

	return filepath.Clean(dir), nil
}

// conversationValue reads the in-app conversation's ID from its flag a, refusing one that
// isn't a UUID, and a second flag beside the one that gave given.
func conversationValue(a, given string) (string, error) {
	id, ok := strings.CutPrefix(a, ConversationFlag+"=")

	switch {
	case !ok:
		return "", fmt.Errorf("%w: %s names no conversation", errCall, ConversationFlag)
	case given != "":
		return "", fmt.Errorf("%w: the conversation is given twice", errCall)
	case !conversationPattern.MatchString(id):
		return "", fmt.Errorf("%w: the conversation %q isn't one", errCall, textutil.Truncate(id, 40))
	}

	return id, nil
}
