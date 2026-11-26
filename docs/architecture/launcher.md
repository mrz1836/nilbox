# internal/launcher

[Docs](../README.md) › [Architecture](../README.md#architecture) · Code: [`internal/launcher`](../../internal/launcher) · Guide: [Launching a session](../guide/launching-a-session.md), [Coding agents and profiles](../guide/agents-and-profiles.md), [Launch defaults](../guide/launch-defaults.md), [Prompts](../guide/prompts.md), [Outside contributors](../guide/outside-contributors.md), [Sensitive files](../guide/sensitive-files.md)

`internal/launcher/` resolves a launch (the agent first, then that agent's profile, model, and
flags), plans it, and writes its files. `resolvePermission` alone chooses a launch's access, a
background run's fence (`Request.Fence`) included, below the trust rule: an untrusted item without
the override runs Read only, a fixed preset brings its own mode whatever the profile says, and a
fenced plan takes none of the profile's extra directories. Its `FileChecker` is core's `agentFiles`:
the inbox's file check with what agent runs' commits changed merged in (`fence.MergeChanged`, the
inbox's `item_agent_changes`), so a run that wrote an agent file holds the next launch on its item
at that head as `HoldAgentFiles`, whoever opened it.

`internal/launcher/terminal/` renders the launch script, which only changes into the folder and runs
the agent, and opens it in the user's terminal app, one driver per terminal and OS.
`terminal.ScriptWriter` writes every script the app opens in a terminal into the launch folder, a
session's (`WriteLaunchFiles`, with its prompt file) and a followed run's (`terminal.WriteScript`)
alike (named `pr-`, `issue-`, or `run-<number>`, or a repository's
launch's `repo-<YYYYMMDD>`, pruned after seven days by those prefixes alone); a followed run's
script runs its command under `script(1)`, recording its output, its shell's PID, and its exit
status in the run's files. `launcher.CommandOpener`, which core hands the runs and never binds
(so the window can open neither a file nor a folder), opens such a script in the terminal chosen
in Settings, and opens that terminal in a folder where its driver can do so itself (below).

The macOS drivers: iTerm2's types the quoted script path into a new tab with AppleScript
(`osascript`, which needs macOS's Automation permission); Terminal.app's runs
`open -a Terminal <script>`; Warp's runs `open -b dev.warp.Warp-Stable <script>`, by bundle ID so
neither Warp Preview nor another app named Warp answers. Warp has no AppleScript dictionary, and
runs an executable `.command` file it is asked to open in a new tab of its frontmost window (a new
window when none is open; a Warp that is quit starts and restores its last window first),
starting the tab in the script's folder and typing the escaped path once its shell is ready, so a
followed run's script is its own process, as in iTerm2. Neither `open` asks for a permission, and
neither can tell when the terminal drops the script: Warp drops every open, a folder's and a
link's too, until its first-run welcome is finished, and `open` still succeeds. A Warp before
v0.2026.05.13 opens the script in its editor, which nothing can tell either. Warp's driver is
`warp.go` (the pure arguments and the focus link's check), `exec_darwin.go` (`Open` and `Focus`),
and `folder_darwin.go` (`OpenFolder`); no other OS registers a driver yet (`drivers_other.go`),
so Warp, like the others, is macOS's alone. `terminal.All` lists the terminals in the order a
choice prefers them, iTerm2, Warp, Terminal, and `terminal.Valid` reports one of them;
`Registry.Installed` (a bundle in `/Applications` or `~/Applications`, Terminal.app always)
lists them in that order, so the setup moves a terminal that isn't installed to the first one
that is, Autopilot's readiness reads the same, and `settings` accepts exactly `terminal.Valid`'s
IDs.

Show in terminal is `Registry.Focus(ctx, id, terminal.Tab)`, for a driver that is also a
`terminal.Focuser` (`Focus(ctx, Tab)`; any other is `ErrNoFocus`). A `Tab` carries the session's
tty and its agent's PID (`Tab{TTY, PID}`). iTerm2's and Terminal.app's `Focus` take the tty
(`validateTTY`) and run an AppleScript that walks their windows' tabs for it, `ErrNoSuchTab` when
none runs on it. Warp has no tty lookup: its `Focus` reads `WARP_FOCUS_URL`, the link Warp
(v0.2026.05.27 and newer) exports in every shell and the agent inherits, from the PID's
environment (`procs.EnvOf`, that one variable alone, through the driver's `env`), takes only
`^warp://session/[0-9a-f]{32}$` (`warpFocusLink`), and runs `open -b dev.warp.Warp-Stable <link>`,
which brings the tab's window forward and selects its pane, with no permission. An invalid PID, a
process with no link (an older Warp, a session not launched in Warp, a platform binary whose
environment macOS hides), or any other value is `ErrNoFocusLink`, whose text never repeats the
value, and nothing is opened. A closed tab, which Warp keeps about 60 seconds, still has a link,
and opening it only brings Warp forward while `open` succeeds, so nothing can tell.

Open in terminal is `CommandOpener.OpenFolder(ctx, dir) (opened bool, err error)`. Whichever
terminal is chosen, it checks the folder first (`terminal.CheckFolder`: absolute, no control
character, and a directory now, or `ErrInvalidFolder`, which a folder gone since its page listed
it turns into "<folder> isn't there anymore.", with `~` for home), then reads the chosen terminal
and, when its driver is a `terminal.FolderOpener`, opens the folder through it and reports true.
Warp's is: `open -b dev.warp.Warp-Stable <dir>`, after `validateFolder` again, opens a new tab
whose own shell starts in the folder, set up by Warp as any of its tabs, with its input editor
and blocks. iTerm2's and Terminal.app's aren't, so it returns false with no error, and the caller
(core's `terminalRuns`) writes the script that changes into the folder and execs the user's
login shell, and opens it. Warp gets no such script because it runs it as one long command in
its tab, whose nested shell has none of Warp's blocks.

`Resume` takes `ResumeRequest.Access`: a background run's session resumes under its run's fence
(`ResumableSession.Fence`, the preset and the run's folder), taken over (`Fence.TakenOver`,
`agent.Access.TakenOver`: for Claude Code, the same tools, rules, hook, and sandbox, a fixed preset
in `manual` rather than `dontAsk`, so the user is asked about the rest; for Codex, the same sandbox
and writable folders, no fence hook, and `--ask-for-approval on-request` rather than
`approval_policy="never"`), unless the user ticks the profile's own
(`profile`), under the trust rule; a fence whose settings are gone is `ErrNoFence`. The bound
`TakeOver` stops a background run first through its `TakeOvers` (core's `backgroundAgents`), then
resumes it so, in its worktree, held back as a launch is.

`launcher.AgentEnv` is the one rule for every agent start's environment: the profile's config
variable set (or unset for the default profile), and, unless the user's per-agent setting
(`settings.Settings.ClaudeAPIKey`, `CodexAPIKey`, off by default) lets a key through, the agent's
`KeyEnv` unset, so the profile's own sign-in is used.

An Autopilot routine's launch is a click's launch with `LaunchRequest.Routine` set (Go-only,
`json:"-"`), which only holds it back more: its files are checked under the unattended scope, only
`RoutineLaunch.Confirmed` confirms a hold, a running session always holds it, a dirty worktree holds
it (`HoldDirty`), `FullAccessOverride` is ignored, and its prompt names the routine. A launch for a
pull request's situation (`LaunchRequest.Situation`, a `Situation` from `situation.go`, refused when
unknown and ignored on an issue) resolves its prompt from the repository's launch defaults' column
for it, the org's, then the app's (`prompts.DefaultPrompts`), and with none set, from the pull
request's own chain, so an org's prompt for pull requests never beats the app's for a situation;
`Resolution.Situations` names the four for a pull request, only to label the card's agent button.
`promptData` fills `.ReviewedCommit` from the inbox's `viewer_review_commit` only when it is a
commit's hex ID (`prompts.CommitID`).

`internal/workspacetrust` marks the launch folder trusted in each agent's own config (Claude's
`.claude.json`, Codex's `config.toml`, appended to and never rewritten).

**A repository's launch** (`repo.go`) starts a session on a repository rather than on an inbox item,
as the Deps page's Upgrade does. The bound `ResolveRepo`, `PreviewRepo`, and `LaunchRepo` take a
`RepoLaunchRequest` (the repository, its clone's folder as the window names it, the ID of a prompt
for repositories, and the launch menu's choices, but never a worktree's name, which the launcher
works out from the user's day, `Config.Location`, the machine's time zone unless set) and return the
shapes an item's launch does, so the window's launch controls serve both: `RepoResolution` is a
`Resolution` (item 0, no kind, trusted, the worktree always on, and the prompt the launch's own,
inheriting none) with a `Notice`, and `PreviewRepo` a `Preview`, its worktree and prompt the ones it
would make and fill in now.

Each of the three checks the repository, its clone, and the prompt afresh (`ResolveRepo` leaves the
agent's program, the terminal, and the worktree to the other two). The repository, by what the inbox
last stored of it (its `RepoFacts`, core's `repoFacts` over the inbox's `Store.RepoFacts`, so the
launcher stays off the inbox): one GitHub says is archived, disabled, or locked (with its lock
reason in words), or where the user's access is read or triage alone, is refused (`ErrRepoClosed`,
`ErrNoPushAccess`), while what isn't known (a repository the inbox never saw, or a permission GitHub
didn't give) lets the launch go ahead, saying so in the `Notice`, which also says a fork's pull
request opens on the fork itself. The clone: one the repository locations hold
(`ErrNotInLocations`), the clone's own folder rather than a linked worktree
(`localrepos.CheckAddable`), with a remote that points at the repository (`cloneOf`, which the check
of a linked clone shares). The prompt: only a prompt for repositories, by its ID (`repoPrompt`:
none, one that is gone, or one for items is `ErrNoRepoPrompt`, worded).

It is planned as an item's launch is, the launch defaults keyed by the repository, and differs where
a repository has no item: it is trusted, as the user's own repository is, so no outside
contributor's limit applies; no file is checked, so only a session running in the folder it would
start in holds it back, which `Alongside` confirms; it always starts in a new worktree of its own
(`WorktreeRequest.Task`, `deps-<YYYYMMDD>`, which the maker takes or passes for the first free name
past it; see [internal/worktrees](worktrees.md)), and its prompt is filled in again once the
worktree is made (`fillRepo`: `prompts.RepoData` with the default branch the worktree starts from
and the clone's remote, then `WithWorktree`, no item's field), since a launch made meanwhile may
have taken the name planned; its files are named `repo-<YYYYMMDD>` (`Plan.Task`; `checkSubject`
holds a plan and its files to an item's kind and positive number, or to a task with neither); its
worktree has no record to `Touch`; and its session is recorded with kind `''` and number 0, the
prompt's name as its title, the repository's page as its URL, and the worktree made, and is named
`<nickname> · <name>` in the agent (`repoSessionName`).

A launch that fails once its worktree is made, as when the terminal can't open its script, has the
maker remove that worktree again with its own branch, where the maker can (`WorktreeDiscarder`,
core's over `worktrees.Maker.Discard`, the Worktrees tab's Remove, refused while a session works
there or it holds changes; a refusal is logged, and the launch's own error is what comes back),
since a retry makes another. `Resume` refuses such a session (`ErrNoItem`, by its record's kind and
number, `ResumableSession.Repo`), which has no item to plan its resumption on. The launcher itself
runs only the clone's read-only remote lookup; the worktree maker fetches the default branch and
makes the worktree.
