// Package kit is what Describe it hands a coding agent, and how it reads what the agent
// hands back: the snapshot of the user's setup and of the names in their inbox that the
// agent may read (Snapshot), the proposal it writes (Proposal), the checks a proposal
// passes before the user sees it (Check), the risks it carries, worked out from what it
// changes rather than from the agent's words (Risk), and the words the agent reads: the
// guide, its topics, the proposal's schema, and the instructions, each generated from this
// build's catalog and spec, so an update of the app updates what the agent knows.
//
// It is a pure leaf: it reads no file, starts nothing, and keeps no clock; the snapshot a
// check reads and the moment a file is stamped with come with each call. The app writes
// the snapshot (internal/describeit, through core), and the app's own binary in its tool
// mode (internal/mcpserver) serves the words and writes the proposals, both through kit,
// so the two never disagree about a format.
//
// A snapshot holds names and definitions, never an item's title, body, comments, diff, or
// code, nor a prompt's text; and every name in it is data, never an instruction, which
// every text kit renders from it says.
package kit
