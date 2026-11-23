# Fences: what a background agent may do

[Docs](../../README.md) › [User guide](../../README.md#user-guide) › [Autopilot](README.md) · Design: [`internal/fence`](../../architecture/fence.md)

An agent a routine starts in the background has nobody to answer it, so it runs inside a
fence: its **Tools** preset, which says what it may do whatever its profile allows.

| Preset | The agent may | It never |
|---|---|---|
| **Read only** | Read, search, and plan | Edits a file or runs a command |
| **GitHub tasks only** | Read, and run `gh` to read the item, comment on it, add or remove a label, and run its failed jobs again, on that item alone | Edits a file, or runs anything but `gh` and git's reads |
| **Edit and test, no push** | Edit the worktree, and run the repository's tools (from its files: `go`, `magex`, `pnpm`, `make`, ...), everyday commands, and git's own work in the worktree | Pushes |
| **Edit, test, and push to the PR's branch** | All of the above, and push once, exactly `git push <remote> HEAD:refs/heads/<branch>`: the pull request's own branch, on the clone's remote for the repository | Pushes any other way: no `--force`, no other branch or remote, no push inside another command |
| **The profile's own permissions** | What a click would let it, from its profile (the builder warns when that is nothing) | Crosses the hard lines below |

## The hard lines, under every preset

No edits outside the worktree, inside git's own folder, or to the repository's agent, workflow, and
tooling files (the [sensitive files](../sensitive-files.md) categories agent configuration,
workflows and CI, and build and dev tooling, whatever you turned off there); none of the network
tools (`curl`, `wget`, `ssh`, web fetch and search); no `gh` merge, review, close, release, or API
call, since Autopilot's own steps do what a routine allows; no git option that changes where git
runs or what it reads, no change to the clone's remotes, branches, tags, or config; no command a
shell is handed as text (`sh -c`, `eval`); no reading your credentials (`~/.ssh`, `~/.aws`, `gh`'s
sign-in, ...), the app's data, or the agent's own settings; and no commit with an AI co-author
trailer. Extra commands a routine allows add to what it may run, never past these lines; ones it
denies are refused too.

## How Claude Code keeps them

Every fixed preset drops your profile's own settings, which may allow anything outright, and its
project's (a branch's hooks and MCP servers never load), brings its own permission mode, in which
whatever its rules don't allow is refused, and loads no MCP server. A profile with full access
doesn't loosen a fence, and one with no permission mode doesn't break it. Two things hold it, beside
the rules:

- **The fence hook.** Before each of the agent's tool calls, Claude Code runs the app itself in
  its hook mode, which reads the call, decides it against the run's policy, and answers. It
  opens no window and touches none of your data. A call it can't read, or a policy it can't
  read, is refused, and a hook that fails or times out blocks its call.
- **The sandbox.** The edit presets' commands run in Claude Code's sandbox: they write only in
  the worktree, its git folders (never their hooks or config), and the tools' caches (Go's
  build and module caches, pnpm's store, npm's, ...), and reach only GitHub and the package
  registries. The exact push and fetch, and `gh`'s reads, run outside it, as one plain command
  each, since the sandbox would cut them off from your SSH key and `gh`'s sign-in. A sandbox
  setting Claude Code can't read stops the run before it starts. Before its first command, the
  agent runs the sandbox check the fence asks for: one `touch` that writes a file in the run's
  own folder, which the sandbox allows, and then one beside it, which it must refuse. No other
  command runs until it has, and if the second file is ever written, the fence refuses every
  call from then on.

Attribution is off in every fenced run, so a commit carries no AI co-author trailer.

## How Codex keeps them

Every preset but **GitHub tasks only**, which it doesn't keep (its sandbox cuts `gh` off from GitHub
and its sign-in). It reads none of your Codex config or rules, so neither yours nor a branch's MCP
servers, hooks, or notify program run, and asks no one. **Read only** runs in Codex's read-only
sandbox, reading through its shell; the edit presets in its workspace sandbox, which writes only in
the worktree, its git folders (so it can commit, never their hooks or config), and the temporary
folder. **Edit and test, no push** has no network; **Edit, test, and push** turns the network on for
its one push, and since Codex's sandbox can't keep that to GitHub, a command it runs may reach any
host (the fence still refuses the network tools themselves). The same fence hook decides every call,
Codex's patches by each file they write, so `AGENTS.md`, which its sandbox would let through, is
refused; and since Codex never shows a refused call, the run's outcome counts those the hook
refused.

## When a push can't be fenced

A pull request whose branch is in a fork waits as *Can't start yet: its branch is in a fork, which a
run can't push to*: the exact push goes to the repository's own branch, which a fork's isn't. One
whose branch or remote has a name that isn't plain can't start with a push either. An outside
contributor's item, when a person lets an agent at it, runs Read only, whatever the routine says.

## What a run changed

When a background run ends, the files its commits changed are checked against the sensitive
categories. A run that changed agent configuration (a hook, an MCP server, instructions) holds the
next launch on that pull request at that head, and the next background run on it, under **Needs your
OK**, as an outside contributor's pull request would, whoever opened it: the head the run started
from, whose worktree holds its commits, and its own last commit, which a push makes the pull
request's head.

## What a fence doesn't do

It reads command lines, which a determined command can disguise, so the boundaries are the agent's
own: the mode that refuses what no rule allows, and for the edit presets, the sandbox. The agent
still runs with your git and `gh` credentials, as a session you open does, and sends what it reads
to its model provider.
