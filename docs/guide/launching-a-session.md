# Launching a session

A launch writes two files under the data directory's `launch/` folder: the filled-in
[prompt](prompts.md), and a small `sh` script that changes into the folder the session starts in
(the item's worktree, or the clone) and runs the agent with the resolved flags. The script does
nothing else: no git, no logic, only `cd`, the environment, and `exec`. It reads the prompt from
its file, so the prompt is never part of a command line. A prompt that starts with `-` once
filled in (a title such as `--dangerously-skip-...` printed first, say) is refused, since the
agent would read it as an option. Each agent gets its arguments in its own order:

- **Claude Code** gets the prompt first, then `--model`, `--effort`, the permission flags,
  `--add-dir=<dir>` for each extra directory, the extra flags, a session name such as
  `--name='Review · example-repo#7'`, which it shows in its prompt box, its resume list, and the
  terminal's title, and `--session-id=<id>`, the ID the app [follows the session](following-a-session.md)
  by.
- **Codex** gets its flags first and the prompt last, after `--`, so the prompt can never be read
  as a flag or a subcommand: `codex --model <model> <permission flags> --add-dir=<dir> <extra
  flags> -- <prompt>`. Codex has no session names, and no flag for the effort level or Fast
  mode (see below).

**Codex's effort and Fast mode.** Codex takes these only as config overrides (`--config
model_reasoning_effort="<level>"` and `--config service_tier="fast"`), and runs a session
started with any config override outside its shared app server, where the app can't see that
it [waits for you](following-a-session.md). So by default a Codex launch passes neither: Codex
uses the effort and speed in its own config, and you change them in the session with `/model`
and `/fast`. With Codex chosen, the launch menu greys **Effort** out and says so. Two switches
under **Settings → Launching** change that, both off at first:

- **Set Codex's effort at launch** passes the effort a launch resolves to, as Claude Code gets
  it, and the launch menu offers it again.
- **Start Codex in Fast mode** presets the launch menu's **Fast mode** checkbox, which shows
  only for Codex and can be changed for one launch. Fast mode is never saved as a launch
  default.

Either one costs that session the **Needs approval** and **Needs input** states on its card,
which then says it runs outside Codex's app server. Your own `--config` flags in a profile's
extra flags cost the same.

**API keys.** An agent runs on its profile's own sign-in, the one you made with `claude auth
login` or `codex login` (a subscription through the browser, say), never on a key from your
shell's environment, unless you say so. So by default every launch unsets the variables that
would sign the agent in with a key instead: Claude Code's `ANTHROPIC_API_KEY` and
`ANTHROPIC_AUTH_TOKEN`, and Codex's `OPENAI_API_KEY`, `CODEX_API_KEY`, and
`CODEX_ACCESS_TOKEN` (either of the last two signs Codex in over a ChatGPT sign-in), which a
profile's own environment can't set either; the launch menu's Preview lists them (`unset
ANTHROPIC_API_KEY`).
**Use an API key when one is set**, under **Settings → Launching**, one switch per agent, off at
first, lets a key from your shell through for that agent's launches, billed to that key. The
same rule decides the environment of every launch: from the launch menu, a resume, and an
Autopilot routine's. Nothing logs a variable's value.

The terminal then runs the script:

- **iTerm2** (the default) opens a new tab in the frontmost window, or a new window when none is
  open.
- **Terminal.app** opens a new window (`open -a Terminal`).

Choose the terminal under **Settings → Launching**, and the clone root (the directory whose
children are your clones, initially `~/projects`) under **Settings → Repositories**. A launch
whose agent isn't installed says so, as in "Codex isn't installed (codex wasn't found)."

## Repository locations

The clone root is the primary: new clones go in it, and it is looked in first. Clones anywhere
else are found through the **other repository locations** under **Settings → Repositories**,
each one of two kinds, worked out once as you add it:

- **A folder** is looked into one level deep, as the clone root is: each folder inside it that
  holds a `.git` is a clone. No folder is searched any deeper.
- **A single clone** counts as that one repository, wherever it is.

Type a path or click **Choose…**. The folder is looked at before it is added, and the app
suggests a better choice where it finds one:

- **A folder with no clones directly inside,** such as `~/src` in a `~/src/<owner>/<repo>`
  layout: it names the folders inside it that have some, *~/src has no clones directly inside;
  ~/src/acme-org (4) and ~/src/tools (2) do*, and offers **Add those** or **Add ~/src anyway**.
  It looks at no more than 100 folders and 5,000 entries.
- **A clone whose folder holds other clones:** it offers that folder, *~/Desktop/exp is a clone.
  Its folder, ~/Desktop, holds 3 other clones*, or **Add only this clone**.
- **A folder the locations already cover** (the clone root, a listed location, or a clone directly
  inside one) is refused, and so are `/`, the whole disk, hidden folders, anything inside a
  `.nilbox` or `.git` folder, and a linked worktree, whose clone is the one to add. A folder
  added drops the single clones inside it, which it now covers.

The clone root shows how many clones sit directly inside it, *6 clones directly inside*, or a
warning such as *Not found*. Each other location shows what it is now: *folder · 6 clones*,
*clone · acme-org/example-repo*, or a warning such as *Not found* (a drive that isn't plugged
in), *Not answering* (a network share that hangs), *No longer a clone*, *Now a clone itself* (a
folder someone ran `git init` in, which is still looked into), or *Already in ~/projects*, with
**Check again**. Every page and launch reads every location at once, and a location that can't
be read is skipped and named: the launch's message says so, and the Worktrees view says which it
couldn't read. A location that doesn't answer within 5 seconds is skipped, and read again next
time. A clone two locations reach is listed once.

Removing a location leaves its clones and their worktrees as they are, but nothing lists, sweeps,
or cleans them up until you add it again, and their worktree records are kept. Worktrees stay
beside their clone, under its folder's `.nilbox/worktrees/<repo>`, wherever it is.

**Clones.** The item's clone is found by scanning the clone root and each folder location one
level deep, and each clone location, and matching each repository's git remotes: a clone whose
`origin` is the repository wins, and a tie goes to the clone root's. The match is remembered and
checked again on the next launch, while a location still holds it. To
find and check clones, the launcher runs only the read-only `git -C <clone> config --get-regexp`
remote lookup. No git command the app runs changes a clone's own checkout: no checkout, stash,
pull, reset, or branch switch, with one exception you click: Repo scripts' **Pull**, which
fast-forwards the branch a clone is on to origin's (`git pull --ff-only`). Otherwise only a tool
you turn on in Settings → Integrations does, when you click: a release's magex switches to the
branch, pulls, tags, and pushes the tag, and the app then checks where the tag and your clone
are. When a session needs a pull request's branch, the
starter prompts ask the agent to run `gh pr checkout <n>` inside the worktree. The only git
commands the app runs that change a clone are its [worktree creation](#worktrees) (a fetch into
the item's own refs, or into a pull request's base's remote-tracking branch, and `git worktree
add`), the [Worktrees view's](worktrees-view.md)
removals, prunes, and unlocks, with the [branch deletions](worktrees-view.md#branches) you confirm there, and
[Repo scripts'](repo-scripts.md) **Pull** you click, which fast-forwards the clone's branch.

**A repository with no clone.** When a launch finds no clone of the item's repository, the row
says so and where it looked, *No local clone of acme-org/example-repo in ~/projects or your other
repository locations*, and offers **Clone into ~/projects/&lt;name&gt;**: the clone root, then
the repository's name. It also offers **It's cloned elsewhere…**: pick the clone's folder, and the app checks it is
a clone of the repository (refusing, say, *~/x isn't a clone of acme-org/example-repo; its origin
is acme-org/other*), adds it as a repository location, or offers its folder when that holds other
clones, and continues the launch. A clone the app remembered that no location holds now, as after
the clone root changed, is offered as **Use ~/old/&lt;name&gt;**. Confirm the clone, and the app
runs `gh repo clone <owner>/<name> ~/projects/<name>` as a program
with arguments, never through a shell. It then remembers the new clone and continues the launch
with the same choices. The clone uses the GitHub CLI's own sign-in and its git protocol setting.
This is the only time the app creates a clone, and it never writes into an existing directory.
If anything is already at that path, such as a clone of another repository with the same name,
the row says so and offers no clone: clone the repository yourself, or change the clone root.

## Worktrees

The **Start in a worktree** launch option is on by default (the `launch.worktree` setting, under
**Settings → Launching**); launch defaults and prompts can turn it off, and turning it off in the
launch menu applies to that launch only. With it on, Nilbox makes the item's worktree itself, in
Go, before the terminal opens. No agent's own `--worktree` flag is used, so every agent works the
same way, and the launch script stays a plain `cd` and `exec`.

- **One worktree per item per clone,** shared by every agent: a session started with Claude Code
  and a later one with Codex work in the same folder.
- **Beside the clone, not in it:** `~/projects/.nilbox/worktrees/<repo>/pr-<n>` or `issue-<n>`,
  on the item's own branch, `worktree-pr-<n>` or `worktree-issue-<n>`. Outside the clone, an
  agent never finds the copy while searching or loading the clone's instructions, the clone needs
  no ignore rule, and a location's scan skips the hidden folder. A clone in another location
  keeps its worktrees beside it in the same way.

A launch finds the item's worktree, or makes it, in this order:

1. **Resume** the worktree the app recorded for the item, while git still has it registered
   there.
2. **Adopt** a worktree with the item's branch checked out anywhere, such as one Claude Code made
   under `<clone>/.claude/worktrees/pr-<n>` before Nilbox made worktrees itself, so the work
   started there carries on where it is.
3. **Attach** the item's branch, left from an earlier worktree that was removed, with `git
   worktree add`. Its commits are kept, and the branch is never moved or reset.
4. **Create** a pull request's worktree at its head: `git fetch --no-tags <remote>
   refs/pull/<n>/head:refs/heads/worktree-pr-<n>`, then `git worktree add`. `<remote>` is the
   clone's remote for the item's repository, so a fork's clone with an `upstream` remote gets the
   real head. When the launch checked the pull request's files, the head fetched must be the
   commit it checked: if the pull request was pushed to in between, the fetched branch is deleted
   again and the launch asks you to open it again, which checks the new commits.
5. **Create** an issue's worktree from the repository's default branch: its remote-tracking
   branch is fetched, then `git worktree add --no-track -b worktree-issue-<n>`, so a push never
   lands on the default branch. When the fetch fails (offline, say) and an earlier fetch left the
   branch, the worktree starts from that, and the launch says so.

Whichever of these a pull request's launch does, it also fetches the branch the pull request
merges into, such as `main`, into its remote-tracking branch, `origin/main`, the same fetch an
issue's worktree starts from. An agent working in the worktree then has the base as GitHub has it
now, to rebase onto, merge, or compare with, even when it doesn't fetch, or can't, as an agent
in the background can fetch only within its fence. The fetch moves only `origin/main`: never the
worktree's checkout, nor your own `main`. When it fails (offline, say), the launch goes ahead
and says so, as in *Couldn't fetch origin/main, so the worktree has the last fetched copy*.

Entries for worktrees whose folders are gone are pruned first. When something git doesn't know is
already at the worktree's path, the app prunes git's entries and tries once more, then says what
is in the way: it never deletes a folder. Git runs as an argument list with prompts off and a time
limit, and one clone's worktrees are made one at a time. Every git command the app runs on
worktrees runs with a hooks folder's hooks, the file system monitor, and submodule recursion
turned off, and never in a clone whose own config names a hook: a worktree holds the pull
request's own files, and a hook in your clone that runs a tool from the checkout would
otherwise run the pull request's code before any agent starts. The status the Worktrees view
and the cleanup read, and a removal, run with none of the clone's own filters (Git LFS's
either) and never in a clone, or a worktree, whose own config holds a setting that could make
git run a program ([below](#what-a-clones-own-config-may-hold)): such a worktree says why
instead, and is never offered a removal. Every other look at a clone's worktrees, branches, and
commits fetches nothing, even in a partial clone missing what it reads. A commit is always
checked out as itself, whatever replace refs the clone has. Its fetches, its
look at the remote's default branch, and `git worktree add` also run under the same guards as
Repo scripts' **Pull**, with the environment your login shell gives and the same git as every
other command the app runs there: a clone whose own git config holds a setting that could make
git run a program or write elsewhere ([below](#what-a-clones-own-config-may-hold)) is refused
before git runs, naming the settings, and so is a remote reached through a remote helper, by
name, though a worktree from the last fetched copy, or on a branch the clone already has, is
still made, since that checkout asks the remote nothing. The clone's own ssh command, password
program, and credential helpers give way to your own, and a fetch that fails without them says
which. No maintenance runs, the remote is asked for git's own upload-pack, and the clone's own
folder is named so its config can't send the checkout elsewhere. The clone's config is read
once a launch. A fetch moves only the ref it
names, whatever the clone's own refspecs say, so a launch never moves one of your branches. In
a partial clone, the checkout fetches the files it writes from the clone's promisor remote,
under the same guards. Every worktree made or adopted is
recorded with its item, and each session started in it with its agent and time, so the
[Worktrees view](worktrees-view.md) links it and its cleanup finds it. An
[upgrade from Deps](deps.md) is the exception: it has no item, so it makes a new worktree of its
own every time, from the default branch, which nothing resumes and no record names, and its
pushed branch links it to its pull request instead.

## What a clone's own config may hold

The git commands the app runs that change a clone (Repo scripts' **Pull**, and a launch's
fetches and `git worktree add`) first read the clone's own git config, `.git/config` and
whatever it includes, and run only when every setting there is one of these:

- what git, Git LFS, and editors write for themselves, which names no program to run and no
  place to write: what `git init` and `git clone` write (`core.repositoryformatversion`,
  `core.filemode`, `core.bare`, `core.logallrefupdates`, `core.ignorecase`,
  `core.precomposeunicode`, `core.symlinks`, `core.autocrlf`, `core.eol`), sparse checkout's
  switches; git's many-files mode, which users of large repositories turn on for speed
  (`feature.manyFiles`, and the settings it turns on, `index.version`, `index.skipHash`, and
  `core.untrackedCache`, each on its own too), which only changes how git writes its index; a
  remote's `url`, `fetch`, `pushurl`, `tagopt`, `prune`, `promisor`, and
  `partialclonefilter`, and the notes the GitHub CLI and other git clients keep there;
  anything about a branch but its `mergeoptions` (VS Code keeps `vscode-merge-base` there);
  the repository's format extensions; `user.name`, `user.email`, `user.signingkey`,
  `commit.gpgsign`, `tag.gpgsign`, and `gpg.format`; `pull.rebase`, `pull.ff`, and
  `fetch.prune`; a submodule's `url` and `active`; `lfs.repositoryformatversion`, what Git LFS
  keeps of a server (`access`, `locksverify`), and its own filter, exactly as `git lfs
  install --local` writes it; `rerere`'s switches; `log.excludeDecoration`, colors, and git
  gui's settings; `mergetool`, `difftool`, `merge.tool`, and `diff.tool`, which only `git
  mergetool` and `git difftool` read; how `git push`, `git rebase`, and `git status` behave
  (`push.*`, `rebase.*`, `status.*`); what `git flow init` writes (`gitflow.*`); and
  `init.defaultBranch`, `merge.ff`, and `merge.conflictStyle`;
- what the app sets aside or sets again for these commands, so the clone's own is never used:
  its ssh command, password program, and credential helpers, for your own (of its other
  credential settings, only the user name and `useHttpPath` are allowed); `url` rewrites,
  as the app checks each remote by the URL git will use; http settings (one that sends git
  through a proxy, trusts other certificates, or resolves a host elsewhere still refuses a
  command that reaches an https remote, or that Git LFS may run in); which transports git may
  use; `core.hooksPath`, `core.fsmonitor`, and `submodule.recurse` (so a husky folder doesn't
  stop them); and the maintenance settings `git maintenance` and scalar write, as no
  maintenance runs.

Anything else, such as a hook, another filter, `core.worktree`, an include or `includeIf`, an
alternate refs command, a gc setting but `gc.auto`, or one of Git LFS's transfer agents,
refuses the command, which names the settings. Set them for that folder in your global git
config with an `includeIf` instead: your own config, global or system, is never weighed.

## Workspace trust

Each agent asks once whether you trust a folder, and would stop a launched session at that
question, or start it with less access than the launch chose. So before each launch the app
marks the folder the session starts in trusted, and its clone too when that is a worktree, in the
config of the profile the session runs with:

- **Claude Code:** `~/.claude.json` for the default profile, or `.claude.json` in the profile's
  config directory. The app sets `projects["<folder>"].hasTrustDialogAccepted` to `true`, which
  is how the agent's own documentation says to trust a folder by hand. It takes the lock the
  agent takes on that file and changes nothing else in it.
- **Codex:** `config.toml` in the profile's `CODEX_HOME` (`~/.codex/config.toml` for the default
  profile). The app appends a `[projects."<folder>"]` table with `trust_level = "trusted"` and
  rewrites nothing else, so your comments and formatting stay exactly as they were. A folder
  that already has an entry is left alone, even one you marked untrusted, and a file that isn't
  valid TOML is never written; either is logged instead.

A folder that is already trusted is left alone, and a profile directory that doesn't exist yet
is left for the agent to set up. A project's agent settings (Claude's `.claude/settings.json`, or
Codex's project config) then apply in every session, as they would after you accept the
question, hooks included, which is why a launch on a pull request that changes agent
configuration [asks first](sensitive-files.md).

## macOS's Automation permission

The first launch in iTerm2 makes macOS ask once whether the app may control iTerm2 (macOS's
Automation permission, which has nothing to do with [Autopilot](autopilot/README.md)): "“Nilbox”
wants access to control “iTerm”", followed by the app's reason: Nilbox opens your terminal app
(such as iTerm2) to start coding sessions in your local clones. Choose **Allow**. Terminal.app
launches never ask. An Autopilot routine's run in your terminal, and **Take over**, open
iTerm2 the same way, so allow it before you leave a routine to run while you're away.

If you chose **Don't Allow**, the launch fails with a message that says so. Change it in
**System Settings → Privacy & Security → Automation**, under Nilbox. A build from source asks,
and is listed there, as Nilbox Dev, apart from the release.
