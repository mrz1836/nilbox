# Building and running the desktop app

Nilbox is a macOS desktop app: a Go core, a [Wails v3](https://wails.io) shell, and a React +
TypeScript + Vite + Tailwind frontend in `frontend/`. This guide covers the one-time setup, the
build targets, how the desktop shell is kept out of the Linux CI build, and what the app does
on your machine: where it keeps its data, how the window and the inbox cards are laid out, how
it finds your agent profiles, how it decides what an item may do, and how it treats git
worktrees.

Measured startup, refresh, launch, size, and API-budget numbers, and how they were taken, are
in [performance.md](performance.md).

<br>

## Prerequisites

- **macOS 13 or newer.** The app is built and supported on macOS only for now.
- **Xcode Command Line Tools** (`xcode-select --install`), which provide the C toolchain the
  desktop build needs.
- **Go** at the version in `go.mod`.
- **Node** at the major version in `frontend/.nvmrc` (Node 26).
- **pnpm**, installed once at the exact version pinned in `frontend/package.json`
  (`packageManager`). Node 26 no longer ships corepack, so install it with npm:

  ```bash
  npm install -g pnpm@12.16.1
  ```

  When `pnpm` is missing, the frontend targets stop with this same command, read from
  `packageManager`.
- **[MAGE-X](https://github.com/mrz1836/mage-x)** (`magex`) and
  **[go-pre-commit](https://github.com/mrz1836/go-pre-commit)**, as in the README.
- The **[GitHub CLI](https://cli.github.com)**, signed in with `gh auth login`. The app borrows
  its token and needs the `repo` and `read:org` scopes.
- To launch sessions: the **`claude`** CLI and **iTerm2** (the default terminal) or
  Terminal.app.

The Wails CLI is never installed. The targets run it with `go run` at the version `go.mod`
pins.

<br>

## Build targets

| Target | What it does |
|---|---|
| `magex frontend:install` | Installs the frontend dependencies exactly as `pnpm-lock.yaml` pins them |
| `magex frontend:build` | Type-checks the frontend and builds it into `frontend/dist` |
| `magex frontend:gate` | The frontend gate: frozen-lockfile install, format check, lint, typecheck, and tests with coverage (`frontend/scripts/gate.sh`, the script CI runs) |
| `magex brand:generate` | Regenerates the theme tokens, frontend brand constants, and `build/darwin/Info.plist` from `internal/brand` |
| `magex app:bindings` | Regenerates the committed TypeScript bindings in `frontend/bindings` |
| `magex app:bindingsCheck` | Regenerates the bindings into a temporary directory and fails on any drift |
| `magex app:lint` | Lints the desktop-tagged Go code (the desktop main and the frontend embed) |
| `magex app:dev` | Runs the app against the Vite dev server with hot reload |
| `magex app:build` | Builds the production bundle `bin/Nilbox.app` and signs it ad hoc for local use |
| `magex app:open` | Opens `bin/Nilbox.app`, building it first when it is missing (run `app:build` to pick up a change) |
| `magex app:check` | Runs `app:bindingsCheck`, `app:lint`, and `app:build` |
| `magex measure:startup` | Startup to the first frame on screen, over six launches |
| `magex measure:refresh` | Five live, read-only inbox refreshes and the API budget they imply |
| `magex measure:launch` | One real launch in iTerm2, timed until the agent runs in the clone |
| `magex measure:size` | The size of the app binary and the bundle |
| `magex measure:all` | Every measurement, in that order |

A typical first build:

```bash
cd ~/projects/nilbox
magex app:build
magex app:open
```

Before opening a pull request, run the Go gate, then the frontend gate, then (on a Mac) the
desktop checks:

```bash
magex format:fix && magex lint && magex test:race && go-pre-commit run --all-files
magex frontend:gate
magex app:check
```

**Signing.** `magex app:build` signs the bundle ad hoc (`codesign --force --sign -`), which is
enough to run it on the machine that built it. Distribution signing and notarization are not
part of the build.

**Firewalls.** The app talks only to GitHub: `api.github.com` on every refresh, and
`avatars.githubusercontent.com` to download your profile picture once (it is then cached). A
per-app firewall must allow `bin/Nilbox.app/Contents/MacOS/nilbox` to reach `api.github.com`,
or every refresh times out. Blocking the avatar host only means the toolbar shows your initial
instead of your picture.

<br>

## How the desktop shell is kept out of CI

Everything except the window is plain Go in untagged `internal/*` packages, built, tested, and
linted on Linux with `CGO_ENABLED=0`. Only the desktop bootstrap needs Wails, CGO, and WebKit,
and it sits behind the `desktop` build tag:

- `cmd/nilbox/main.go` (`//go:build !desktop`) is the headless build, which prints the product
  identity. `magex build` and every untagged `go build ./...` build it.
- `cmd/nilbox/main_desktop.go` (`//go:build desktop`) is the Wails app. macOS window options
  live in `window_darwin.go` (`desktop && darwin`).
- `frontend/assets_desktop.go` (`//go:build desktop`) embeds `frontend/dist`, so no untagged
  build needs a built frontend.
- MAGE-X discovers build tags and tests them in one combined pass, which would compile the
  desktop shell on Linux. `.github/env/90-project.env` therefore adds `desktop` (and Wails'
  `production` release tag) to `MAGE_X_AUTO_DISCOVER_BUILD_TAGS_EXCLUDE`. The same file leaves
  `frontend/` to Prettier, out of the Go formatter, and keeps `magefiles/` out of coverage.
- A repository test fails if anything outside the desktop bootstrap imports Wails.

CI runs the fortress workflows on ubuntu for the Go code, and `.github/workflows/frontend.yml`
on ubuntu for the frontend gate. No macOS runner is used, so `magex app:check` is a local step.

<br>

## Data location

The app keeps its database, launch files, and logs in one directory, created on first start
and readable only by you:

| OS | Directory |
|---|---|
| macOS | `~/Library/Application Support/Nilbox` |
| Linux (not yet supported) | `$XDG_DATA_HOME/nilbox`, else `~/.local/share/nilbox` |
| Windows (not yet supported) | `%AppData%\Nilbox` |

The database is `nilbox.db` (SQLite, WAL mode). Before a schema migration changes an existing
database, a copy of it is saved next to it, and the three newest copies are kept. GitHub tokens are never
stored there, nor anywhere else: the app borrows the token from `gh auth token` for each
refresh and keeps it in memory only.

<br>

## The window

A single toolbar runs beside the macOS window controls. It holds the **Inbox** and
**Worktrees** tabs and, on the right, the API budget gauge and your profile picture. Drag the
bar to move the window, and double-click it to zoom.

**Account menu.** Click your picture for who is signed in, **Settings** (also ⌘,), and **View
GitHub profile**. After a refresh, the picture is downloaded from
`avatars.githubusercontent.com` without your token and cached in the database, so it shows
offline. It is downloaded again only when GitHub reports a new picture, such as after you
change it or sign in as another account with `gh auth switch`. Until it arrives, the button
shows your initial.

**API budget gauge.** The dial follows whichever GitHub budget has the least left: GraphQL,
which the inbox spends, or REST and search, which `gh` and your agents spend from the same
account. Its needle points right when the budget is full and left when it is empty, and its
color follows the share left:

| Color | Share of the budget left |
|---|---|
| Green | More than 50% |
| Yellow | More than 25%, up to 50% |
| Orange | More than 10%, up to 25% |
| Red | 10% or less |

Click it for each budget's remaining points, when it resets, and what the last refresh cost.
The readings are taken on each refresh, and a budget whose reset time has passed counts as full
again.

**Settings** is a page of its own, with sections for your account, launching, agent profiles,
repositories, and the app's version. Each change is saved as you make it, except the clone
root, which is saved with **Save**. Escape or the back arrow returns to the view you came from.

<br>

## Inbox cards

Each pull request or issue in the inbox is a card three lines high:

1. Its title, with a **Draft** tag on a draft pull request and a **Conflicts** tag when it has
   merge conflicts with its base branch. A long title is cut short; hover it for all of it.
2. The repository and number, the author, how long ago it last changed, and an **outside
   contributor** tag when the [trust rule](#outside-contributors) applies.
3. Its details, in a fixed order that lines up from card to card. Hover any of them for the
   whole sentence, which is also what a screen reader reads.

| Detail | What it shows | Color |
|---|---|---|
| Checks | The latest commit's checks that passed, out of those that ran. Skipped checks count toward neither. | Green when all passed, yellow while any still run, red when any failed |
| Reviews | Reviewers who have reviewed, out of everyone asked or who reviewed. The author's own replies don't count, and a reviewer asked again owes a new review. | Green once approved, red when changes were requested |
| Comments | The conversation's comments, review comments included | None |
| Size | XS (under 10 changed lines), S (under 30), M (under 100), L (under 500), XL (under 1,000), or XXL; then the lines added and removed, the files, and the commits | Added lines green, removed lines red |
| Labels | The first three labels with their GitHub colors, then how many more | None |

An issue shows its comments and labels.

**Where the details come from.** Each refresh reads them with a second GraphQL query that asks
for the listed items by node ID. Asking for these fields inside the inbox query's three
searches would cost about 12 points a refresh, because GitHub prices nested lists per result
slot; asking for the listed items alone costs about one point per request. GitHub answers
these requests at roughly 150 ms per item, so they go out in batches of at least 10 items, up
to 4 at a time, and the details of the items already listed are read alongside the inbox
query rather than after it. Only items new to the lists wait for a request of their own, so a
refresh takes no longer than before once its items are known. The details are stored with each
item and read again on every refresh, since checks finish without the item itself changing.
A card whose details no refresh has read yet, right after updating the app for instance, says
so.

**Opening a session.** The **Open in Claude** button on the right of a card starts a session
with the card's choices. The arrow beside it names the profile and opens the choices: the
profile, **Start in a worktree**, and, on an outside contributor's item, **Use profile
permissions for this item**. A choice applies to the next launch only, and the arrow shows a
dot while any choice differs from the defaults. The launch's outcome appears below the card.

<br>

## Agent profiles

Profiles are discovered from your home directory on every start:

- `~/.claude` becomes the `default` profile. Its sessions run without `CLAUDE_CONFIG_DIR`, so
  the agent uses its default login.
- Each `~/.claude_<name>` or `~/.claude-<name>` directory that the agent has run with becomes
  profile `<name>`, and its sessions run with `CLAUDE_CONFIG_DIR` set to that directory. A
  directory counts only when it holds `.claude.json`, `settings.json`, or a `projects/`
  directory, which the agent writes into a config directory it uses. Other tools name their
  own folders `~/.claude-<name>` too, such as session managers and usage monitors, and those
  folders are left out. If both spellings exist for one name, the underscore form wins. Files
  such as `~/.claude.json` are ignored.
- A newly discovered profile starts with full access (`--dangerously-skip-permissions`), the
  agent's default model and effort, and plan mode off.

Discovery never changes a stored profile, so edits to a profile survive restarts, and a
profile whose directory disappears is kept. On each start the app removes only the stored
profiles that hold nothing worth keeping: ones whose directory is still there but holds no
agent config, and that were never edited. Their folders are left untouched. If such a folder
later becomes an agent config directory, it is discovered again. When the removed profile was
the default profile setting, the setting falls back to `default`.

**Settings → Profiles** lists every profile with the folder it runs from and what it sets for
a session (model, effort, plan mode, extras, and permissions), and marks any whose folder has
gone missing.

Each inbox card's **Open in Claude** button uses the default profile (the `profile.default`
setting, initially `default`, under **Settings → Launching**) unless you pick another in the
card's launch options, which applies to that launch only. Model, effort, plan mode, extra
directories, and extra arguments come from the profile.

<br>

## Outside contributors

An item is **trusted** only when its author's association with the repository is OWNER,
MEMBER, or COLLABORATOR and, for a pull request, its branch is in the same repository, not a
fork. Everything else is untrusted, including bots such as Dependabot. The rule applies to
issues as well, because the agent reads an issue body the same way it reads a pull request.

Untrusted items show an **outside contributor** tag and launch in plan mode
(`--permission-mode plan`), whatever the profile says, without the option to switch to full
access later.

On untrusted items only, the card's launch options hold the one override: an unchecked **Use
profile permissions for this item** checkbox. Its help text reads: "Lifts the
outside-contributor restriction; keeps the profile's plan and permission settings." It does
not force full access: the item is then treated as trusted, and the selected profile's own
plan-mode and permission settings decide.

A profile's extra arguments can never set permissions themselves. A launch whose profile passes
`--permission-mode`, `--dangerously-skip-permissions`, or `--allow-dangerously-skip-permissions`
is refused.

<br>

## Launching a session

A launch writes two files under the data directory's `launch/` folder: the starter prompt, and
a small `sh` script that changes into the clone and runs `claude` with the profile's flags. The
script reads the prompt from its file, so the prompt is never part of a command line. The
terminal then runs the script:

- **iTerm2** (the default) opens a new tab in the frontmost window, or a new window when none is
  open.
- **Terminal.app** opens a new window (`open -a Terminal`).

Choose the terminal under **Settings → Launching**, and the clone root (the directory whose
children are your clones, initially `~/projects`) under **Settings → Repositories**.

**Clones.** The item's clone is found by scanning the clone root one level deep and matching
each repository's git remotes. The match is remembered and checked again on the next launch. To
find and check clones, the app runs only the read-only `git -C <clone> config --get-regexp`
remote lookup. It never runs a git command that changes a clone: no checkout, stash, fetch,
branch switch, or worktree creation. When a session needs a pull request's branch, the starter
prompt asks the agent to run `gh pr checkout <n>`.

**Worktrees.** The **Start in a worktree** launch option is on by default (the
`launch.worktree` setting, under **Settings → Launching**); turning it off applies to that
launch only. With it on, the agent creates and owns the worktree with its own `--worktree`
flag, under `<clone>/.claude/worktrees/<name>` on a branch named `worktree-<name>`:

- A pull request whose repository is the clone's `origin` launches with `--worktree '#<n>'`, so
  the agent names the worktree `pr-<n>` and starts it at the pull request's head.
- Any other pull request launches with `--worktree pr-<n>`, and the prompt asks the agent to run
  `gh pr checkout <n>` inside the worktree.
- An issue launches with `--worktree issue-<n>`.

Launching on the same item again reuses the name, so the agent resumes the existing worktree
instead of creating another. The outside-contributor rule applies unchanged.

### The one-time Automation prompt

The first launch in iTerm2 makes macOS ask once whether the app may control iTerm2: "“Nilbox”
wants access to control “iTerm”", followed by the app's reason: Nilbox opens your terminal app
(such as iTerm2) to start coding sessions in your local clones. Choose **Allow**. Terminal.app
launches never ask.

If you chose **Don't Allow**, the launch fails with a message that says so. Change it in
**System Settings → Privacy & Security → Automation**, under Nilbox.

<br>

## The Worktrees view

The **Worktrees** tab lists every linked worktree of every git repository one level under the
clone root, including worktrees from sessions started outside the app. It loads when you open
it or click **Refresh**, not on the inbox's five-minute timer. Each row shows the repository,
the path, the branch (or "detached"), clean or dirty with the count of uncommitted files
(untracked files included), whether it is locked and why, whether its directory is gone, the
age of its last commit, and a **created by Claude** marker for worktrees under
`.claude/worktrees/`.

- **Remove** runs `git worktree remove` without `--force`. Git refuses a dirty or locked
  worktree, and the row shows why.
- **Remove all clean** removes every clean, unlocked worktree, after a confirmation that shows
  the count.
- **Force remove** is offered only on dirty, unlocked rows. It runs `git worktree remove
  --force` after a confirmation that shows the number of uncommitted files.
- **Locked** rows have no remove action at all. The agent locks its worktree while a session
  runs, so a locked worktree usually belongs to a live session. The row shows the lock reason
  and the `git worktree unlock` command to run yourself once the session has ended. When the
  lock names an agent process that is no longer running, the row also says the session appears
  to have ended (a stale lock). It is still not removable from the app.
- **Prune** appears for a repository with entries whose directory is gone, and runs
  `git worktree prune`.
- The main worktree is listed for context and is never removable.
- **Branches are never deleted.** Removing a worktree keeps its branch, including the
  `worktree-<name>` branches the agent creates.

Every git command runs as an argument list with `git -C <repository>`, never through a shell.
