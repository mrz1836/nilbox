# internal/agentrun/stream

[Docs](../../README.md) › [Architecture](../../README.md#architecture) · Code: [`internal/agentrun/stream`](../../../internal/agentrun/stream) · Guide: [Agents in the background](../../guide/autopilot/agents-in-the-background.md)

`internal/agentrun/stream/` reads a coding agent's stream as its run writes it, Claude Code's
`claude -p --output-format stream-json --verbose` and Codex's `codex exec --json`, for the
background runner ([Agents in the background](../autopilot/agents-in-the-background.md)) and Describe it's
turns alike. It is a leaf: `TestLeafImports` holds it to `runevent` alone, so its readers
reach no run engine or store. `Claude(dir)` and `Codex(dir)` each return a `Parser`, an
`io.Writer` fed the log in order that any goroutine may read: its events a page at a time
(`Page`), its progress, the agent's own end (`Result`), the provider's usage limit (`Limit`),
Codex's thread (`AgentSessionID`), and, for Claude Code, what its init line says of its own
setup (`Init`: its model, its version, where its key came from, the session, every tool, and
every MCP server with its status, neither list ever nil), which Describe it checks before a turn
goes on. `From` starts a parser at an offset, for the run view's pages, and `Over` says the
result line came. `agentrun` aliases `Result` and `Limit`, so nothing above it changed.

The conversation spike's recordings (`testdata/conversation/`) hold what a conversation's turns
add to a headless run's stream, which the parser reads alike: a `session_title_changed` note
before init when the session is named, which it notes and shows nothing of; several tool uses
of one message, each on a line of its own; the denials `dontAsk` makes of a tool no rule
allows, counted in `Result.Denials`; and, for a turn that resumes a conversation, a result whose
`total_cost_usd` and `modelUsage` are the conversation's running totals, so `Result.Cost` is
the session's, `Result.TurnCost` the turn's own (summed from
`model_usage_since_last_result`), and `Models` every turn's. A background run starts its own
session, so its two costs are one.
