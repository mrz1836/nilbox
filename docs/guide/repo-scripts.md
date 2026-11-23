# Repo scripts

**Repo scripts** runs a clone's own commands: its `t3.json` scripts, its `.vscode/tasks.json`
tasks, and magex's targets. Listing them runs nothing in the clone: the files are read, and
magex's built-in targets are listed in a folder of the app's own. The page never asks GitHub's
API, so it works offline and signed out; only the branch check beside the chosen clone asks its
origin, with one `git ls-remote` whose answer is kept five minutes.

**The page.**

- **The picker** lists the clones in the [repository locations](launching-a-session.md#repository-locations), the ones
  a script ran in most recently first, and the page starts on the one you chose last, even after a
  restart. Two clones with the same name in different locations show their folder, as
  *example-repo (~/work)*. Beside it: the repository, the folder, and the branch, with *From
  branch feature-x* when the clone isn't on its default branch, since a branch can change what
  the files say.
- **The branch check** beside the branch says whether origin has newer commits on the branch it
  follows: **Up to date**, or **Update available** with **Pull**, which fast-forwards the branch
  (`git pull --ff-only`, never a merge or a rebase) as a run in the app, whose progress shows in
  the branch check, and reads the scripts again once it ends. It asks origin about that one branch, with one `git
  ls-remote`, and never fetches; the answer is kept five minutes, and its refresh button asks
  again, while a pull keeps the commit it fetched without counting as a new ask. A clone whose
  origin isn't on GitHub reads **Origin not checked** with a **Check** button: Nilbox asks no host
  but GitHub on its own, so that origin is asked only when you click. A branch ahead of
  origin, one that has diverged from it, one that tracks no branch on origin, a detached HEAD, or
  a merge or rebase in progress says so, and offers no pull. A pull refuses a clone that moved to
  another branch since the check, fails with git's own words on a branch with commits of its own,
  and says so if the clone is switched to another branch while it runs. A pull runs only in a
  clone whose own git config holds nothing that could make git run a program or write
  elsewhere (see [What a clone's own config may hold](launching-a-session.md#what-a-clones-own-config-may-hold)):
  such a setting, a hook or a filter, say, refuses the pull before git runs, naming the
  settings, and you can set them for that folder in your global git config with an `includeIf`
  instead, where they count as your own. The check says so, **Can't pull here**, with the
  settings in its tooltip, and offers no pull. The clone's own ssh command, password program,
  and credential helpers give way to your own, and a pull that fails without them says which.
  It runs with no hook, no file system monitor, no submodule, no signature program, and no
  maintenance, the clone's own folder named so its config can't move it, and it updates only
  the branch and origin's copy of it, whatever the clone's own refspecs say. In a partial
  clone, it fetches the contents of the files it writes from the clone's promisor remote,
  under the same guards. Checking asks origin once; pulling asks it only in git's own fetch. A
  pull can't be stopped, since stopping partway could leave the files half updated: it ends on
  its own within thirty minutes, and quitting leaves it to finish; after a restart it still can't
  be stopped, and says what it did once it ends. One its limit cuts off says it was cut off
  partway, where it left the branch, whether git's index is still locked, and to open the clone
  in a terminal and run `git status`. Nothing is checked or pulled while Scripts is off.
- **The sources, in order:** **Yours** (below), then **From t3.json**, **From
  .vscode/tasks.json**, and **magex**, each as Settings → Integrations turns it on (the Scripts
  card's **Show magex targets** and **Read t3.json and VS Code tasks**). A file that can't be
  read says why.
- **Each script** shows its command (or a task's detail) with **Run**, and a ⋯ menu that runs
  it in the other place: your terminal when runs go to the app, or the app when they go to your
  terminal. A task the app can't run, such as one that uses `${file}`, shows greyed with the
  reason. A background task says it runs until you stop it.
- **A version bump** (a command naming `version:bump` or another target that tags) shows
  *Release does this, with checks* and **Open Release** instead of Run; its ⋯ menu still has
  **Run it anyway**.
- **A dependency upgrade** (a command naming magex's `deps:update` or `mod:update`, `go get -u`,
  or a package manager's update, such as `pnpm update`, `npm update`, `yarn upgrade`, `bun
  update`, or `ncu -u`) shows *Deps upgrades in a fresh worktree and opens a pull request* and
  **Open Deps** instead of Run: run here, it would upgrade the clone itself, on whatever branch
  it's on, and leave the changes uncommitted. Its ⋯ menu still has **Run it anyway**, here or in
  the other place, and Run again runs one you ran anyway the same way. `deps:tidy` only tidies,
  and runs as before. See [Deps](deps.md).
- **magex:** the common targets as chips that run at once (one that bumps a version opens a
  menu instead, with **Open Release** and **Run it anyway**; `deps:update` isn't one, since Deps
  does it), then **This repository's targets** and **All magex commands**, folded and
  searched together, with an **Arguments** field for
  `key=value` pairs, which magex's own targets run with. A note says that magex targets run the
  repository's own code and its `.github/env` settings, as any build does.

**Your scripts.**

- **New script** saves a command of your own: its name; the command, one line as you'd type it
  in a terminal; the repositories it shows in, **All repositories**, **Go repositories** (the
  clones with a `go.mod` at their root), or **Chosen…** from your clones' GitHub repositories;
  where it runs, **As set in Settings**, Nilbox, or **Terminal**, for a command that asks you
  questions; **Ask before running**; and its icon.
- **They show first**, in the order you put them in, in each clone their repositories include;
  a clone with none of yours says so. They never ask for trust, since you wrote them, and run
  exactly as written, in your login shell, fish and tcsh included, in the clone, with
  `NILBOX_REPO` (owner/name), `NILBOX_REPO_DIR`, and `NILBOX_DEFAULT_BRANCH` set, so a
  command never has to quote a path. The default branch is the one Nilbox knows from GitHub,
  else the one the clone's `origin/HEAD` names, else `main` or `master` when the clone is on
  it. One that bumps a
  version points to Release, and one that upgrades the dependencies to Deps, like any other.
- **Each one's ⋯ menu** has **Edit…**, **Duplicate** (a copy right after it), **Move up** and
  **Move down** (past the nearest of yours shown in that clone), and **Delete…**, which asks
  first; its runs stay in Runs. Run again runs a script as it is saved now, and refuses one
  you deleted or set to ask before running, which you run from Repo scripts. A script you
  save again as its run is starting doesn't run, and says so.

**Running one.**

- **A script of yours set to ask before running** shows its command and where it runs, with
  **Run** and **Cancel**, before each run, its ⋯ menu's included.
- **A task with inputs** (VS Code's `pickString` and `promptString`) opens a small form first,
  with the defaults filled in; each choice shows the value it fills in beside its label.
- **A command from the repository asks first**, before its first run and whenever it changes:
  *Run "Test" from acme-org/sdk's t3.json?* shows the command exactly as it will run, inputs
  filled in, and that it runs in the clone with your permissions, with **Don't ask again for
  this command in acme-org/sdk** ticked. A changed command shows what you ran before above what
  it runs now, under *Changed since you last ran it*. The repository's own magex target asks the
  same way, and again whenever a file under `magefiles/` changes. magex's built-in targets
  never ask. A clone whose origin isn't on GitHub asks every time.
- **What runs is what you saw:** the app reads the command again under the clone's lock and
  refuses one that changed after you confirmed it, saying so rather than asking again.
- **The sections** (Yours, t3.json, VS Code tasks, and magex) each have a heading with how many
  commands they hold, which opens or folds the section; each stays as you left it, across
  restarts.
- **Each run** goes in the app or your terminal as **Runs in** says, and Runs lists it. The page
  says *The run started*, with **Show it**, which opens the run, with its log, beside the
  scripts, and **Always show runs here**; turn on **Open a run beside Repo scripts as it
  starts** in Settings → Integrations to have every run open there at once. Either way the page
  stays as you left it, the arguments and the search included.
- **A clone with nothing to show** says why: its `t3.json` or `.vscode/tasks.json` lists no
  commands, it has neither file, or the sources are turned off in Settings → Integrations, with
  **Open Settings**.
