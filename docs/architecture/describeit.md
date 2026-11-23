# internal/describeit

`internal/describeit/` is Describe it, the Autopilot tab's conversation: the user says what
they want, and Claude Code proposes it as cards the user reviews and applies through the
window's own builder, prompts, and settings (what the user sees is `docs/desktop.md`'s
[Describe it](../guide/autopilot/describe-it.md)). It is a boundary: the repoguard test keeps it off the
store (and so off Autopilot's service and the background runner), the GitHub client, every
service, `prompts`, `profiles`, `launcher`, `scriptrun`, `desktop`, and `notify`, and it reaches
the rest of the app through the interfaces it declares (`Agents`, `Setup`, `Attachments`,
`Switch`), which `internal/core/adapters_describeit.go` adapts. It owns no table and no prefs key: what it keeps
is in the data folder's describe folder (`appdata.Paths.Describe`, which `Ensure` makes), every
folder the user's alone and every file written whole (`appdata.WriteFile`).

```text
describe/
  context.json            the snapshot the tool mode reads (kit.Snapshot)
  instructions.md         kit.SkillText(kit.InApp), each turn's --append-system-prompt-file
  work/                   the one empty folder every turn runs in
  agent.json              the agent new conversations start with: {profile, model, effort}, "" inherits
  bin/<slug>-mcp          the link the plugin runs: the app's executable, in its tool mode
  plugin/                 the plugin's local marketplace (agent.PluginFiles)
  conversations/<uuid>/   conversation.json, mcp.json (the turn's --mcp-config), and each turn's
                          turn-NNNN.prompt.md (its stdin) and turn-NNNN.jsonl (its stream)
  proposals/<uuid>/       a conversation's proposals (kit.ProposalFile) and outcomes.json
  proposals/outside/      the same, from the user's own Claude Code
```

- **The bound methods.** `Status` (Claude Code found, at least `agent.ConversationFloor`, and
  signed in, else why and the fix, each a sentence; the agent new conversations start with,
  Inherit resolved; whether a turn runs; and `Pending`, the cards from outside with a part no
  one dealt with, the nav's count), `Agent` and `SetAgent` (Settings' choice, `agent.json`,
  refusing a profile Claude Code doesn't have, or a model or effort the agent's own checks
  refuse), `Conversation`, `NewConversation`, `Send`, `Stop`, `Events`, `Settle`,
  `DiscardProposal`, `Proposals`, `Plugins`, `InstallPlugin`, and `RemovePlugin`. None reads
  GitHub, and every refusal is worded with `uierr`.
- **A conversation** is one current conversation, kept in its `conversation.json` (`record`:
  its agent as chosen and as resolved, whether its session started, the outcomes a turn already
  told the agent of, and its turns), whose UUID is the agent's session ID. One that started more
  than 30 days ago is removed with its proposals as it is read or sent to, or as the worker
  readies the folder; `NewConversation` sets the current one aside (refused while a turn runs), and a start
  over removes every conversation and proposal (core's `forgetConversations`).
- **A turn.** `Send` reserves the one turn that may run (another is refused while it does, and
  every send once the app quits), plans it before its agent starts (its conversation, or a new
  one with `Agent`'s choice; the agent through `Agents.Conversation`, held to the floor; the
  app's own executable; the attachments through `Attachments`; and the snapshot written fresh
  and the instructions), then writes the turn's files and starts it with `execpath.Start` in a
  process group of its own: `agent.ConversationArgs` with the conversation's ID (`--session-id`,
  then `--resume` once the session started), `<brand.Name>: Describe it` as the session's name,
  the conversation's model and effort with Inherit resolved to the profile's own (`record.resolve`,
  what the header shows; a profile with none passes no flag), the turn's MCP config (`agent.ConversationFiles`, its one
  server `brand.Slug`, the app's executable with `mcpserver.Arg` and `mcpserver.ConversationArg`),
  the instructions' file, and a $2 budget (`turnBudgetUSD`); the prompt on stdin, opened with
  `paths.OpenRegular`, and stdout and stderr into the turn's log, in `work/`, with the
  environment core builds as for a background run (`launcher.AgentEnv` over the login shell's,
  then `agent.HeadlessEnv`). The prompt is first what the user did with the cards since the last
  turn (`kit.OutcomeWords`; `record.Told` keeps an outcome from being told twice), then their
  words (at most 20,000 characters), then what they attached (at most 10, each within 64 KB).
  A turn takes no run slot, no clone lock, no history row, and no quit hold, and counts toward
  neither the routines' runs a day nor the daily cap.
- **The follower** (`follow`) reads the turn's log every 100 ms into `stream.Claude`, tells the
  window (`event.DescribeItUpdated`, `describeit:updated`, with the conversation and the turn)
  at most every 250 ms, and notes each proposal file that appears in the conversation's folder.
  As the init line comes it checks the turn's own setup (`initProblem`): the app's server
  `connected`, and every tool and server its own (`agent.ConversationToolPrefix`), else it stops
  the turn at once, since a turn whose server failed improvises and spends (the spike's
  `turn-mcp-failed.jsonl`). It stops the turn with `execpath.StopLadder()` once the result line
  is in, on the user's `Stop`, or as the app quits (`shutdown`, which kills the group after 3
  seconds). The end (`finish`) is judged by the result line, never the exit status: done;
  stopped, by the user or the quit, whose SIGINT still writes a result in error (the spike's
  S7); or failed, worded (the setup was wrong, the usage limit, the budget, the agent's error,
  or the last line it wrote that isn't the stream's). A turn's cost is the result's `TurnCost`
  and the conversation's the latest `Cost`, the session's running total. A turn that got as
  far as init leaves the session started, so the next resumes it; one whose agent ended before
  init, on its own, names the session the other way next, so a conversation never sticks on a
  session Claude Code refused. A turn a crash left running is closed from its log as the worker
  readies the folder, or before the conversation's next turn.
- **Events** (`Events(id, turn, after)`) are a turn's events after a sequence number, a page at
  a time, as a background run's are: from the follower's parser while it runs (none, not done,
  while it gets ready), and once it ended, from its log read whole by a parser of its own, which
  numbers them alike.
- **Cards.** A card is a proposal file read as untrusted, since any process running as the user
  can write the folder: only a name the tool mode makes (`kit.ValidProposalID`), never a link
  (`Lstat`), within `kit.MaxProposalFileBytes`, through kit's strict decoder, naming its own ID
  and the folder it is in; then checked again with `kit.Check` against the live snapshot, so a
  card says what applying it would do now. A part the user dealt with is shown as it was dealt
  with instead (`asDealtWith`), since checked against the setup it changed itself it would find
  its own name taken or nothing left to change: no problem, warning, or risk, a routine it
  added or saved by that routine's name now, and settings and rules by what they set
  (`kit.SettingsSet`, `kit.RulesSet`). `Card.Proposal` is the proposal as written, which a
  part applies from where `Checked` holds nothing of its own (a description, the mode
  suggested, the template, a change's routine and base). `Settle` records what the user did
  with one part in the folder's `outcomes.json` (only a kind that fits the part, the first
  outcome standing), and `DiscardProposal` every part left; each tells the window
  (`describeit:updated` for a conversation's turn, `event.DescribeItProposals`,
  `describeit:proposals`, for one from outside). The window applies a part itself, through the
  bindings the builder, Prompts, and Settings use ([the Autopilot tab](autopilot/autopilot-tab.md)), so
  nothing here writes the setup.
- **The snapshot.** Core's `describeSetup` builds it (`kit.NewSnapshot`) from Autopilot's `List`,
  `MyScripts`, and `Suggest`, the prompt library (no text, and none of the prompts for
  repositories, which no routine runs: `itemPrompts`), the profiles, the settings
  (Autopilot's, the trusted bots, the repository rules, the ignored workflows),
  `inbox.Store.KnownRepositories` (the stored repositories that aren't archived, most items
  first, at most 200), and the vocabulary (`autopilot.Gallery`, `prompts.Starters` but those for
  repositories, `itemStarters`, and `prompts.Variables`); a part that can't be read fails it
  whole. The service keeps the live
  snapshot 2 seconds, forgets it on any change, and falls back on the last one read when the
  setup can't be read. It writes `context.json` before every turn, as the worker readies the
  folder, and after each burst of changes while Autopilot is on: core's `describeOnChanges`
  passes the app's events on and wakes the worker on `event.AutopilotUpdated`,
  `AutopilotChanged`, `RepoRulesChanged`, and `InboxUpdated`, which writes once the burst has
  been quiet for 500 ms, or after 3 seconds in all, so a session in the user's own Claude Code
  reads it current. The prompt library, the scripts, the trusted bots, and the ignored
  workflows emit no event when they change, so such a change reaches that session's snapshot
  with the next inbox refresh; a turn in the app writes the snapshot fresh first.
- **Attachments.** Core's `describeAttachments` words a routine as `kit.RoutineWords` does, and
  a stored item by its address and short facts (`inbox.Store.FactsRows`: kind, repository,
  author, labels, and a pull request's failed checks' names at its head), under a line saying
  its names are data and nothing else of the item is shared.
- **The agent.** Core's `describeAgents` finds Claude Code as a background run's start does:
  readiness's own checks of its program, its version, and the profile's sign-in, asked again at
  once when one holds the turn back, so a fix shows at once; the launch defaults' Claude Code
  profile for Inherit; and the environment `agentEnv` builds, with no API key unless the user's
  setting lets one through. A profile on this Mac that is only signed out still gets its
  program and environment, not ready: a turn is held back, while the plugin's commands, which
  need no sign-in, run.
- **The plugin.** `Plugin(command, args)` is the plugin (`agent.PluginSpec`: `brand.Slug` in the
  marketplace `<slug>-app`, version 1.0.0, which changes only with the tool mode's protocol; its
  skill `autopilot`, `kit.SkillText(kit.Outside)`; its server `autopilot`), which `WritePlugin`
  writes into `plugin/`, each file only when it differs. The worker rewrites it as it readies the
  folder, and Claude Code loads it in place from there, so an installed plugin reads this
  build's words at its next session. Its server runs `bin/<slug>-mcp`, a link to the app's
  executable that only an app in a stable place re-points (`Config.Stable`, core's `stableApp`:
  the update engine's location check, never a build from source), through a link renamed over
  it, so a build from source never leaves it on a temporary binary. `InstallPlugin` writes the
  folder, points the link (refusing, worded, when the app can't and no stable app did), and runs
  `claude plugin install <plugin> --marketplace <dir> --json` with the profile's environment;
  `RemovePlugin` uninstalls (`not_installed` is no failure) and removes the marketplace; and
  `Plugins` reads `claude plugin list --json` for each profile. Each command is
  `agent.PluginArgs`, read with `agent.ParsePluginResult` or `ParsePluginList`, within a minute.
- **From outside.** `Proposals` reads the outside folder's newest `kit.MaxOutsideRead` files,
  each a card checked against the live snapshot, newest first, leaving out those settled more
  than a day ago. While Autopilot is on, the worker looks at the folder every 3 seconds (a hash
  of each file's name, size, and time) and tells the window `describeit:proposals` when it
  changed, and, as it readies the folder, removes the cards settled more than 30 days ago with
  their outcomes.
- **The worker** (`Worker`, a deferred desktop worker, `Core.DescribeItWorker`) works only while
  Autopilot (beta) is on, which it reads through `Switch` (core's `describeSwitch`, Autopilot's
  settings as stored; none to read, or one that can't be read, is off): as the app starts, and
  again after each change it hears, since the switch's own event reaches it as
  `event.AutopilotUpdated`. Off, as on a fresh database, it writes nothing and follows nothing.
  Once on, it catches up: it readies the folder (a turn a crash left running closed from its
  log, old conversations pruned, and a proposals folder no conversation keeps once it is 30
  days old, the instructions and the snapshot written), keeps the plugin and its link current,
  follows the outside folder, and writes the snapshot after each burst of changes, until a
  burst finds Autopilot off again. It stops a running turn as the app quits, on or off.
- **The eval suite** (`evals/`: two cases with their graders, and mocks of the plugin's tools)
  is the plugin's own, run by hand with `magex autopilot:evals` (Claude Code's `claude plugin
  eval` over this build's plugin in a temporary folder, the baseline arm left out, $5 unless
  `budget=` says), never by the gate; `EvalText` fills in the names it leaves for the build.

## What the conversation spike found

The conversation half was settled by a spike on Claude Code 2.1.328: real `claude -p` turns in
empty scratch folders against a stdio MCP server, recorded into the tests' fixtures
(`internal/agentrun/stream/testdata/conversation/`, `internal/agent/testdata/conversation/`, and
`internal/mcpserver/testdata/mcp-client.jsonl`). The turn's argv follows from it, which a golden
pins (`TestTheConversationsArgv`):

```text
claude -p --output-format stream-json --verbose --permission-prompts none
  --session-id <uuid> | --resume <uuid>   --name "<brand.Name>: Describe it"
  [--model <model>] [--effort <effort>]
  --tools "" --restricted --strict-mcp-config --mcp-config <conversation>/mcp.json
  --permission-mode dontAsk --allowedTools mcp__<slug>
  --max-budget-usd 2 --append-system-prompt-file <describe>/instructions.md
  --system-prompt-snapshot off
```

- **Tools and isolation.** `--tools "" --restricted --strict-mcp-config --mcp-config` leaves a
  turn the server's tools alone, connected. A planted project `CLAUDE.md`, project settings with
  hooks and allow rules, a project `.mcp.json`, and a project skill never reached it, nor the
  user's own `CLAUDE.md`: `--restricted` drops the user's, the project's, and the local
  settings, while managed settings and `--settings` still apply. `--strict-mcp-config` drops a
  `--plugin-dir` plugin's own server, and with no tools there is no Skill tool, so in the app the
  skill's text is the system prompt's addition.
- **Permissions.** Under `--permission-mode dontAsk` the server's own tools are denied without
  an allow rule; `--allowedTools mcp__<server>` allows every one of them. `--tools`,
  `--mcp-config`, and `--allowedTools` take several values, so the prompt comes on stdin.
- **Resume and money.** `--resume` takes a session up again in a new process, which takes every
  flag again; with `--system-prompt-snapshot off` the instructions are read fresh each turn, and
  the prompt cache still carries across. `--max-budget-usd` holds one process, so one turn, and
  a resumed turn's result gives the session's running totals beside the turn's own.
- **Storage.** A session's transcript is kept per working folder in the profile's config, one
  project each, so every turn runs in the one `work/`.
- **Stopping.** A SIGINT mid-turn writes a result in error (`error_during_execution`,
  `aborted_streaming`) and exits 0, the MCP server going with the process group, and the next
  `--resume` goes on.
- **The MCP client** probes `server/discover` first (`-32601` makes it fall back to
  `initialize`), reads the server's `instructions`, sends ids as strings or numbers, shows the
  agent a result's `structuredContent` in place of its text, and ends the server with a signal,
  never EOF, so the tool mode never sends `structuredContent` and writes each proposal whole as
  it is made.
- **The plugin.** `claude plugin install <plugin> --marketplace <dir> --json` adds a local
  marketplace and installs from it in one step, asking nothing, and the plugin loads in place
  from that folder. In a session, its tools are `mcp__plugin_<plugin>_<server>__<tool>`, its
  server starts with every session, each call asks first, and its skill triggers from its
  description.
- **Sizes.** A 57 KB tool list was taken but weighed on every request, so input schemas stay
  small and the whole schema is a topic; a 400 KB tool result never reached the agent, so every
  result stays under 60 KB.
- **Visibility.** A running turn shows in `claude agents --json`; the sessions watcher follows
  only the sessions the app recorded, so it ignores turns, and `--name` names them in the user's
  own lists.
- **A server that failed.** The turn's init showed it `failed` with no tools, and Claude
  improvised and spent all the same, so the follower stops such a turn at once.
