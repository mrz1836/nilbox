// Package describeit is Describe it: the Autopilot tab's conversation, in which the user
// says what they want Autopilot to do and a coding agent, Claude Code, proposes it, as
// cards the user reviews and applies through the window's own builder, prompts, and
// settings.
//
// A conversation is one current conversation, kept in its own folder under the data
// folder's describe folder, that survives a restart. Each of its turns is one headless run
// of Claude Code, the first with the session's ID and each after it resumed: the agent's
// conversation half (agent.ConversationArgs) with no tool of its own but the app's own tool
// mode (internal/mcpserver), whose server reads the snapshot this service writes
// (context.json) and writes each proposal into the conversation's proposals folder. A
// small follower tails the turn's stream into the background runner's parser
// (agentrun/stream), tells the window as it grows, checks the turn's own setup from its
// first lines, and stops the turn once its result is in, when the user stops it, or when
// the app quits. A turn takes no run slot, no clone lock, and no quit hold, and counts
// toward neither the routines' runs a day nor the daily cap: it reads the setup's names
// and writes a proposal, and nothing else.
//
// The same tool mode serves the user's own Claude Code through a plugin this service
// installs per profile (agent.PluginFiles), whose proposals land in the outside folder and
// show at the top of Describe it, with where and when they came from.
//
// Every proposal file is untrusted: any process running as the user can write the folder.
// A card reads only file names the tool mode makes, refuses a link, reads each through
// paths.ReadHead and kit's strict decoder, and checks it again against the live snapshot
// as it is shown; nothing applies without the user's click, through the bindings the
// builder, Prompts, and Settings already use, and this service only records what the user
// did, which the next turn tells the agent.
//
// Its background worker works only while Autopilot (beta) is on, which it reads through
// Switch: off, it writes nothing and follows nothing, and it catches up once Autopilot is
// turned on.
//
// It reaches the rest of the app only through the interfaces it declares (Agents, Setup,
// Attachments, Switch), which core adapts: it imports no store, GitHub client, settings,
// inbox, prompts, profiles, launcher, run engine, or window shell.
package describeit
