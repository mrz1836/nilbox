# Hooks

**Hooks** lists every clone in the [repository locations](launching-a-session.md#repository-locations) with a
go-pre-commit config inside it (a `.github/env/*.env` file, or `.github/.env.base`), installs and
updates go-pre-commit's `pre-commit` hook in them, and runs the checks. The app itself never
writes a hook or fixes a file: go-pre-commit does, when you click a button that says what will
change. The page never asks GitHub, so it works offline and signed out.

**The page.**

- **Groups:** **Needs attention** (no `pre-commit` hook yet, go-pre-commit's from an older release,
  one that isn't executable, another tool's, or one in a hooks folder other repositories share),
  open; **Ready** (go-pre-commit's current hook), open; and **Turned off in the repository**
  (`ENABLE_GO_PRE_COMMIT=false` in its config), collapsed. **Not set up** lists the clones
  without a config, collapsed, with **Set up go-pre-commit** linking to its README. A group you
  open or fold stays that way, when you come back to the page and after a restart.
- **Each row** shows the repository and its hook, ✓ ⚠ or ✗ with words (*No hook yet*, *Outdated
  hook*, *Another tool's hook is there (husky)*, *Shared hooks folder ~/.githooks (6 repos)*),
  and how its last checks went, which opens their run. Below it, when true: the other hook types
  go-pre-commit installed (*Also installed: commit-msg*), the go-pre-commit the repository's CI
  pins when it isn't yours, why its hooks couldn't be read, and what holds the clone right now,
  with **Watch** for that run.
- **The button:** **Install**, **Update**, or **Reinstall** run at once. **Replace…** asks first,
  naming the other tool and where go-pre-commit moves its hook (`pre-commit.backup.<number>`,
  put back if you uninstall). Any of them in a hooks folder other repositories share
  (`core.hooksPath` outside the clone) asks first too, saying how many of your clones use it,
  and the request carries that folder, which the app checks again under the clone's lock before
  go-pre-commit runs. Ready rows offer **Run checks**.
- **Each row's ⋯ menu:** **Run checks on all files**, **Run checks on staged files**, **Run one
  check** (each check, *(off)* where the repository turns it off), **Reinstall the hook**,
  **Uninstall the hook…** (asks, naming the hook it puts back), **Open the config** (shows
  `.github/env/`, or the older `.github/.env.base`, in Finder), and **Open in terminal** (your
  terminal in the clone, running nothing). The menu says once that eof, fumpt, and whitespace fix files, and whitespace stages
  its fixes.
- **Install and update all** installs, updates, or reinstalls the hook in every row that needs
  it, one clone at a time, each its own run, and then lists what happened in each. It never
  replaces another tool's hook, never touches a shared hooks folder, and skips a busy clone.
- **Search** filters every group, opening each while it searches. **Scan again** reads every
  clone again now; otherwise the page serves its last scan for 5 minutes, reading again at once
  only the clones a run changed. A scan that fails keeps the last one, marked with its age.
- **The PATH warning** shows while the hook can't find go-pre-commit: the hook looks on your
  shell's PATH and in `$(go env GOPATH)/bin`, and the copy in use is in neither. **How to fix**
  shows the line for `~/.zprofile`, with **Copy**. It goes once a scan finds the folder there.
- **The tool-install note** says that the checks install gofumpt, golangci-lint, and gitleaks the
  first time they need them, without checking them. It shows unfolded until the first checks
  run ends in the app, then stays under **More**, and goes once go-pre-commit verifies its own
  downloads.

**Each run a row starts** opens in Runs, with its log and its result, in the app or your
terminal as **Runs in** says.

- **A hooks action's result** is read from the hook files once go-pre-commit ends, never from its
  exit status: *Installed*, *Updated*, *Already current*, *Replaced* (and where the other hook
  went), *Uninstalled* (and what came back), or *Nothing installed: another tool's hook is in the
  way*, even when go-pre-commit exited 0, with **Replace…**.
- **A checks result** heads the clone and the files, then says which checks passed, failed, were
  skipped, or are off, what go-pre-commit said went wrong and how to fix it, and every file the
  checks fixed or staged, including one that already had uncommitted changes, from git's status
  before and after the run. Run again runs the same checks again.

Agent sessions' worktrees share each clone's hooks, so an agent's commits run these checks too.
