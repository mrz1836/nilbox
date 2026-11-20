// Package mcpserver is the app's tool mode: the app's own binary, run again by Claude Code
// as a stdio MCP server, which serves Describe it's tools to the in-app conversation and,
// through the plugin, to the user's own Claude Code. It reads the snapshot the app wrote
// (kit.Snapshot), answers with kit's words and checks, and writes nothing but a proposal,
// into a folder the app made. It starts nothing, opens no database, reads no environment,
// makes no network call, and writes no log.
//
// The protocol is JSON-RPC 2.0 over stdin and stdout, one message a line, each line at
// most 4 MB, answered as it comes: the agent's client never closes stdin, and ends the
// tool mode with a signal, so nothing waits for stdin to end and every proposal is written
// whole the moment it is made. It speaks MCP's dated versions up to 2025-11-25: it answers
// initialize, ping, tools/list, and tools/call; refuses every other request, the newer
// protocol's server/discover probe among them, as a method it doesn't have, which makes
// the client fall back to initialize; and answers no notification. A tool's answer is
// text, with isError for a refusal the agent reads and can act on, and never
// structuredContent, which the client would show the agent in place of the text.
//
// The flag names the describe folder and, for the in-app conversation, the conversation
// (Arg, ConversationArg). The conversation's agent (kit.InApp) reads the user's whole
// setup and the names in their inbox; a session in the user's own Claude Code
// (kit.Outside) reads the part of the setup Snapshot.Outside keeps, and never the inbox's
// names. Either way a proposal is checked against the whole snapshot, as the app checks it
// again on its card, and one with a problem is refused. One that passes is written into
// the conversation's proposals folder, or, from outside, into the outside folder, which
// holds at most kit.MaxOutsidePending proposals waiting for the user; the tool mode makes
// neither folder, and writes into one only when the app made it.
package mcpserver
