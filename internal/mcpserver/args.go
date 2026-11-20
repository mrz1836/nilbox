// Package mcpserver is the app's tool mode: the app's own binary, run again by Claude Code
// as a stdio MCP server, which serves Describe it's tools to the in-app conversation and,
// through the plugin, to the user's own Claude Code. It reads the snapshot the app wrote
// (kit.Snapshot), answers with kit's words and checks, and writes nothing but a proposal,
// into a folder the app made. It starts nothing, opens no database, reads no environment,
// makes no network call, and writes no log.
package mcpserver

import "strings"

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
