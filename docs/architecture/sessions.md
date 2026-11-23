# internal/sessions

`internal/sessions/` follows the agent sessions launches start. It owns `agent_sessions`, the
ledger the launcher records each launch in (through its `SessionLedger`) under an ID minted
before the launch, which Claude takes as `--session-id`. A `Watcher` (a desktop worker)
reconciles what each agent reports into it and emits `event.SessionsUpdated`; one `Observer`
per agent turns the agent's own shapes into one `State`: `observe/claude` runs `claude agents
--json` and reads the transcript tail, and `observe/codex` talks to Codex's app-server daemon
over `codex app-server proxy` with a read-only JSON-RPC client (its `Allowed` methods only, over
a WebSocket it speaks itself, since only `internal/github` speaks HTTP), falling back to the
rollout files. Each observer degrades to `StateUnknown` rather than guess, and a session's
process says whether it still runs. Following never sends a session anything, and nothing here
reaches the network. A session that stops working (idle, waiting on the user, ended, or
failed) hands its item to the inbox's `Rereads` (a desktop worker, through the `Rereader`
interface core wires), which reads it again, one GraphQL point each, one item at a time. A
session still working hands it over too once its agent finishes a command that changes
GitHub (`Observation.Acted`, from `fence.ChangesGitHub`): the Claude observer follows each
listed session's transcript from where it last read it (`commandScan`, `commands.go`: only
what the file gained, from its last 4 MB the first time, at most 4 MB a poll, lines over
1 MB passed over, after the `Lstat` that refuses a link and through `paths.OpenRegular`),
noting a Bash `tool_use` stamped at or after the launch that isn't `run_in_background`, and
telling it once its `tool_result` arrives without `is_error`; the Codex observer weighs the
`commandExecution` items of the turn it reads anyway (`status` completed, `exitCode` 0, each
once by turn and ID, none from its first read, which ran before the session was followed).
The Watcher reads `Acted` from the observations as made, before `Apply` drops one that
changes nothing, and throttles per item (`heedActed`, `ActedGap`, 30 s): the first at once,
any more within the gap once at its end, and a session that stops working drops one waiting,
since its own read covers it. A background run's session is its run's to report
(`Ledger.RunState`), never an observer's, so it gets only the read as it stops working. A
single-item read (`RefreshItem`, the checks follower, the review sheet's `ReadPull`) takes a
pull request out of Review requested once GitHub no longer asks the user, by name or
through a team, and an item out of the assigned list once it is no longer assigned to them,
as the issue sheet's `ReadIssue` does too, stamped like the inbox's own edits
(`review_left_at`, `assign_left_at`), so a lagging search keeps it out until it moves, and
`RefreshItem` and `ReadPull` read the user's own verdict with
it at no extra point, so an approval or a request for changes the agent left with gh reaches
the card at once and is told once as the user's review (see
[internal/inbox](inbox.md)). `LastWords` reads a session's whole last message from the
agent's own files, on demand, for the review sheet, and `Usage` its token counts, for the
sessions panel (a `UsageReader` per agent: Claude Code's transcript, each assistant
message's usage once, read incrementally to 64 MB; Codex's last rollout token count; a
background run's session its run's own, through `RunTokens`, which core adapts over
Autopilot's ledger), never the network. Each record keeps what its launch
reported (the permissions it started with, what it did to the item's worktree, and what it
worked around), which the card's sessions panel shows in place of a line on the row after
the launch, and what came of its item on GitHub while it ran (`GitHubEvent`: approved,
changes requested, merged, or closed, with the review's commit and GitHub's time; migration
`0102_runs_and_caches`). That comes from the inbox's outcomes, never a request:
core's `sessionOutcomes`, the third sink of `outcomesFanOut`, late-bound once the sessions
are followed, turns each `inbox.Outcome` whose `GitHubFact` says a decided review, a merge,
or a close into a `GitHubNote` for `Ledger.NoteGitHub` (Go-only), which records it
(`NoteGitHubEvent`) on the item's sessions that ran at its time, launched by then and not
ended before, give or take `noteSkew` (10 s), keeping a later event over an earlier one, and
emits `event.SessionsUpdated` once when any changed. `ShowFolder` shows a session's folder in Finder through core's `open -R`
Revealer. The bound `Forget` deletes an ended session's record on the user's click (the
panel's ×), refusing one still running (`ErrLive`; failed and unknown count as running) and
counting one already gone as done, and emits `event.SessionsUpdated`; the prune is
otherwise the only thing that deletes a session.

**A repository's sessions.** A repository's launch (the Deps page's Upgrade) records its
session with no item: kind `''`, number 0, item 0, and the worktree its launch made, never
`''`, its title the prompt's name and its URL the repository's page, which the table's wider
checks take (`0112_repo_sessions`: `(kind = '') = (number = 0)`, and `kind <> '' OR (item_id = 0
AND worktree <> '')`). `Record.valid` takes either an item's record (a kind and a positive
number) or such a one (`validSubject`), so the launch's record is stored rather than dropped
with a log line. `Record.IsRepo` and `Session.IsRepo` tell one. It is followed as any session
is, found running in its worktree for a later launch's hold, but offers no resume (`CanResume`
false: the launcher resumes a session on its item), hands the inbox's rereads nothing whatever
its agent did on GitHub, gets no `GitHubEvent` (`NoteGitHub` passes it over), and is pruned
with its repository's own, its newest `KeepPerItem` kept apart from its items'. `Session.Label`
and `FullLabel` name what a session works on, as the menu bar's waiting rows and the session
banners show it: "example-repo #7" (or "acme-org/example-repo #7"), or, for a repository's,
the repository and the prompt's name, "example-repo · Upgrade dependencies", whose banner opens
the repository's page.
