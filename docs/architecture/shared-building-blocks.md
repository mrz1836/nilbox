# Shared building blocks

[Docs](../README.md) › [Architecture](../README.md#architecture) · Code: [`internal/agent`](../../internal/agent), [`internal/procs`](../../internal/procs), [`internal/execpath`](../../internal/execpath), [`internal/shellenv`](../../internal/shellenv), [`internal/clock`](../../internal/clock), [`internal/periodic`](../../internal/periodic), [`internal/flight`](../../internal/flight), [`internal/paths`](../../internal/paths), [`internal/progress`](../../internal/progress), [`internal/textutil`](../../internal/textutil), [`internal/toolsigns`](../../internal/toolsigns), [`internal/testutil`](../../internal/testutil) · Guide: [Your shell's environment](../guide/shell-environment.md), [Coding agents and profiles](../guide/agents-and-profiles.md)

Services share their building blocks rather than keep copies:

- `internal/repo` and `internal/item` name repositories, pull requests and issues, and their
  worktrees;
- `internal/agent` is the backend registry of the coding agents, one spec each (Claude Code, Codex)
  with its command, config variable and markers, effort levels, models, permission modes, reserved
  flags, and how a launch's options become its flags, a background run's fence included
  (`AccessFence`, which `claude_fence.go` turns into Claude Code's preset flags and `--settings`,
  and `codex_fence.go` into Codex's sandbox, writable folders, and `-c` fence hook; `CheckFenced`,
  the flags no background run's extras set; `KeepsPreset`, each spec's own: Codex keeps every preset
  but GitHub tasks only), its version, sign-in, and login commands with their parsers
  (`ParseVersion`, `ParseSignIn`, which keep no email), the API key variables a launch unsets unless
  the user's setting lets a key through (`KeyEnv`), the status page part it depends on
  (`StatusPart`), the conversation half Describe it talks with Claude Code through
  (`conversation.go`, `claude_conversation.go`: a turn's flags, `ConversationArgs`, which take no
  extra arguments, its MCP config, `ConversationFiles`, the plugin's files and commands,
  `PluginFiles` and `PluginArgs`, the tool names' prefixes, and `claudeConversationFloor`, 2.1.328,
  which `claudeConversationReviewed` holds every flag, command, and file key to;
  [internal/describeit](describeit.md) has the argv), and its headless half for a run in the
  background (`HeadlessArgs` around the fence's own flags, `HeadlessEnv`, `HeadlessFiles` for a
  schema read from a file in its strict form, `StrictSchema`, `Headless` with what it can do there,
  which `Agent.Background` shows the builder, and the version whose flags it needs, the fence's
  floor: Codex's `codex exec --ignore-user-config --ignore-rules`, so no branch's or user's Codex
  config, MCP server, or hook loads, and its Take over `codex resume <thread>`), so adding an agent
  adds a spec and the launcher, profiles, and editors read the rest from it
  (`TestOnlyTheAgentsHomesSpellTheirFlags` fails on an agent's flag or program name spelled anywhere
  else but its parsers and observers);
- `internal/procs` lists the running processes' working directories (`lsof` on macOS, `/proc` on
  Linux), which tell a worktree some session is in, reads one process's whole command line and
  whether it has a terminal (`DetailOf`, from the kernel's process table, never its environment),
  reads one variable of a process's environment and nothing else of it (`EnvOf(pid, name)`, for
  Warp's focus link: `kern.procargs2` on macOS, parsed by the pure, fuzzed `ParseProcEnv`, which
  skips the arguments so a `NAME=value` among them never counts; `/proc/<pid>/environ` on Linux;
  false elsewhere, and for a platform binary such as a shell, whose environment macOS hides; the
  buffer read is cleared before it returns, since the environment holds the process's secrets),
  tells a process the process manager adopted (`Adopted`), reads a process's start time (`StartedAt`),
  which tells a process from a stranger that reused its PID, and probes a PID as running, gone, or
  unknown (`Probe`, which never signals one `ValidPID` refuses and calls it unknown, so an agent's
  worktree lock naming one is kept);
- `internal/execpath` finds and runs external programs (`Finder`, `Runner`; `Search.Path` searches
  the login shell's PATH), holds a program's standard output to `Exec.Limit` bytes when that is set
  (one that prints more is stopped, and `Run` fails with `ErrOutputLimit`, returning none of it, so
  output a repository controls, as gitread's tree, log, and blob reads, can't grow the app's memory
  without bound), and starts a long-lived one in its own process group (`Start`, `Proc`), writing
  straight into a file the app tails and stopping the whole group with a signal ladder; nothing else
  starts a process (`TestOnlyExecpathStartsProcesses`, with named exceptions), and nothing runs `go
  install` or `sudo` (`TestNoGoInstall`, `TestNoSudo`);
- `internal/shellenv` reads the user's login shell's environment, after the first paint, and never
  logs a variable's name or value;
- and `internal/clock`, `internal/periodic`, `internal/flight`, `internal/paths`,
  `internal/progress`, and `internal/textutil` hold the clock (and `clock.Sleep`, the shared wait
  that ends with its context), the interval runner, one call per key at a time, `~` paths, a long
  read's progress told to the window a few times a second (the Release page's list, the window's
  worktrees listing, `event.WorktreesProgress`, the Deps page's read of the clones,
  `event.ManifestsProgress`, and every whole refresh of the inbox, `event.InboxProgress`, beside
  `inbox.Snapshot.Refreshing`), and text helpers, among them the app's one POSIX shell quoting:
  `textutil.ShellQuote`, which always quotes, for every word of a script the app writes, of the ssh
  command it hands git, and for the program of Repo scripts' chain line unless it is a path from
  `/`, and `textutil.ShellWord`, which leaves a word plain only when every POSIX shell reads it as
  itself (ASCII letters, digits, and `=-_./:,+@%`, not starting with `=`), for the commands the
  window shows or copies and the chain line's folders and arguments.

`internal/toolsigns` names the files that show a clone is set up for a tool, each a path from the
clone's top with slashes, a folder counting whatever it holds: `MageX`, mage-x's
(`.github/env/10-mage-x.env`, `.mage.yaml`, or a `magefiles` folder). It is a leaf that imports
nothing of the app's, so the setup's Tools step, which counts the clones that hold them, and Deps,
which finds them in the tree of the commit it reads, share the list without either importing the
other.

A file a repository or an agent controls (a clone's `go.mod`, `t3.json`, `tasks.json`, magefiles,
go-pre-commit config and hooks, the signing files, the `.git` file and `commondir` the sweep's count
of worktrees follows, an agent's transcript) is read only through `paths.OpenRegular` (`ReadHead`,
`ReadTail`): a regular file, or a link to one, opened without waiting and checked again once open,
so a named pipe swapped in is refused (`paths.ErrNotRegular`) rather than hanging the read.

`internal/testutil` holds the fakes and fixtures the test suites share (a fresh database, an
isolated git, `Commit` for a fixture file, git templates built once per test binary, the `Planter`,
which plants fake programs in a clone's own config for the guard tests, recorded runs of real tools,
a runner, finder, process lister, clock, event sink, log, fake tools, and `TerminalScript`, the
shell a fake terminal runs a run's command under); only tests may import it.
`internal/scriptrun/scriptruntest` holds the runner the hooks, scripts, and release suites share,
built on it: a real `scriptrun.Engine` and its Service over a fresh database, the next start over
the same history (`Restart`, `Next`), a fake terminal app that runs each run meant for the user's
terminal under `/bin/sh` (`Terminal`, through `WithTerminal`; `Close` hangs it up mid-run), and
`Ended` and `Await`, which look again only when the engine's `Changed` says the runs changed, never
on a timer (`AwaitShell`, for the shell the engine notes without telling, alone polls); only tests
may import it either. `internal/github/githubtest` holds the fakes of `internal/github` the suites
share, which `internal/testutil` cannot hold as `internal/github` imports it: `Tokens`, a token
source that lends in turn, fails as told, and records what it was told to forget; `Calls`, a fake
GitHub's requests counted by name and in order; and `BadCredentials`, GitHub refusing the token;
only tests may import it too. `internal/localrepos/localrepostest` holds the fake clone root the
hooks, scripts, and release suites share (`Clones`: the roots, the clones listed in them, and each
clone's origin, each read counted, failed, or hooked as a test says); only tests may import it as
well. `internal/assignees/assigneestest` holds the in-memory `Directory` both sheets' suites hand
their assignee Finder, which keeps each read and records who was assigned and which reads failed.
The repoguard guards on SQL and on the git verbs the app runs treat every test-support package as
they treat `_test.go` files.
