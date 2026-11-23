# internal/mcpserver

[Docs](../README.md) › [Architecture](../README.md#architecture) · Code: [`internal/mcpserver`](../../internal/mcpserver) · Guide: [Describe it](../guide/autopilot/describe-it.md), [Use Nilbox in your own Claude Code](../guide/autopilot/your-own-claude-code.md)

`internal/mcpserver/` is the app's tool mode: the app's own binary, run again by Claude Code as
a stdio MCP server (`cmd/nilbox`'s `main` calls `Serve` right after `fence.RunHook`), which
serves Describe it's tools to the in-app conversation and, through the plugin, to the user's own
Claude Code. It reads the snapshot the app wrote, answers with kit's words and checks, and writes
nothing but a proposal, into a folder the app made. It is both a leaf (`TestLeafImports`: `kit`,
`spec`, `appdata`'s whole-file writes, `paths`' readers, `brand`, and `textutil`) and a boundary
(no `execpath`, `scriptrun`, `launcher`, `logging`, store, or service), and
`TestTheToolServerRunsNothing` holds its own files to no `os/exec`, `execpath`, `net`, or
`database/sql`: it starts nothing, opens no database, reads no environment, makes no network
call, and writes no log.

- **The call.** `--mcp-server-v1=<describe folder>` (`Arg`), and in the app
  `--conversation=<uuid>` (`ConversationArg`), which makes it the conversation's (`kit.InApp`);
  without it, it serves the user's own Claude Code (`kit.Outside`), and names a proposal's
  `Where` by the base name of the folder the session runs in, as the system reports it, never
  the environment. The protocol's version is in the flag, and any `--mcp-server` flag is a call
  (`IsCall`), so a version this build doesn't know, a folder that isn't absolute, or a
  conversation that isn't a UUID is refused with a line on stderr and status 2 before it reads
  anything, never started as the app.
- **The protocol.** JSON-RPC 2.0 over stdin and stdout, one message a line of at most 4 MB (a
  longer one refused, and the next read whole), each answered as it comes, since the client
  never closes stdin; ids echoed as they came. It speaks MCP's dated versions up to 2025-11-25,
  the client's when it can; answers `initialize` (`capabilities: {tools: {}}`, `serverInfo` with
  `brand.Slug` and the protocol's version, `1`, and `kit.ServerInstructions`), `ping`,
  `tools/list` (one page), and `tools/call`; refuses every other request, `server/discover`
  among them, with `-32601`; and answers no notification. A tool's answer is text, with `isError`
  for a refusal the agent can act on, and never `structuredContent`.
  `TestTheRecordedClientIsServedInBothPlaces` replays the real client's lines.
- **The tools** are `kit.Tools(where)`: `autopilot_guide`, `autopilot_reference` (`{topic}`, one
  of `kit.Topics`), `autopilot_my_setup` (the whole setup, or `{routine}` for one),
  `autopilot_inbox_names` (in the app alone), `autopilot_check` and `autopilot_propose`
  (`{proposal}`), and `autopilot_proposal_status`. Each input schema stays small, since every
  request carries the list: a proposal is an object, its schema the `schema` topic. Arguments
  are read strictly (one object, the tool's own keys alone), and the snapshot is read again at
  each call (`paths.ReadHead` within `kit.MaxSnapshotBytes`, then `kit.DecodeSnapshot`); outside,
  the setup it gives is `Snapshot.Outside`'s, but a proposal is checked against the whole
  snapshot, as its card checks it again.
- **A proposal.** `autopilot_propose` decodes the proposal (`kit.DecodeProposal`), checks it
  (`kit.Check`), and refuses one with any problem, saying each; else it writes a
  `kit.ProposalFile` (its ID from the time and `crypto/rand`) with `appdata.WriteFile`, never
  over another file, into `proposals/<conversation>/` in the app or `proposals/outside/`, each
  only when the app made it and it is a folder, never a link. From outside it refuses one once
  `kit.MaxOutsidePending` (50) wait, counted as the app counts its cards: among the newest
  `kit.MaxOutsideRead` files, each one the app reads as a card from outside with a part the
  user hasn't dealt with. Its answer opens with the window's line (*Proposed a routine and a
  settings change.*), then gives the ID and says the proposal waits in the Autopilot tab.
  `autopilot_proposal_status` reads the folder's outcomes and says, newest first and within
  48 KiB, what the user did with each part and what still waits; outside, only of the proposals
  from a session in a folder of its own name.
- **Tests.** `cmd/nilbox`'s `toolmode_test.go` runs the binary in its tool mode with nothing but
  an empty home, in the app and from outside, and finds nothing created but the proposal; a
  describeit test serves the tool mode and checks that it refuses exactly when `Status.Pending`
  reaches 50.
