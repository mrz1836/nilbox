# Deps

**Deps** lists every clone in the [repository locations](launching-a-session.md#repository-locations) whose `origin`
is on GitHub, with each Go module's `go.mod` and each JavaScript package's `package.json` in it
and how long ago their dependencies last changed, and upgrades any of them with a coding agent
that works in a fresh worktree and opens a pull request. The page reads your clones' own git
history alone: it never asks GitHub or a package registry what is out of date, so it works
offline and signed out, and finding what is outdated is the agent's job.

**The page.**

- **What it reads:** each clone at its origin's default branch as the clone last fetched it,
  such as `origin/main`, so a clone that is behind, or on another branch, still shows what is on
  GitHub, at the commit an upgrade starts from. The default branch is the one Nilbox knows from
  GitHub, else the one the clone's `origin/HEAD` names, else `main` or `master`, whichever
  origin has. A clone without that branch from origin (one that never fetched it, say) is read
  at its HEAD, and its row says so and why. Your files on disk are never read, so a change you
  haven't pushed and fetched doesn't count.
- **How old:** a module's age is the newest commit, merges left out, that changed its manifest
  or its lockfile: a `go.mod`'s `go.sum` beside it, or the nearest `pnpm-lock.yaml`,
  `package-lock.json`, `yarn.lock`, or `bun.lock` to a `package.json`, in its folder or one
  above. A manifest under `vendor/`, `node_modules/`, or `testdata/` is another project's, and
  left out. A clone is as old as its stalest module that counts, or, once you mark it up to date,
  as old as that mark, whichever is later.
- **What counts:** a `go.mod` that requires nothing, a `package.json` with no dependencies of any
  kind, and any manifest under a `fixtures`, `__fixtures__`, `examples`, or `example` folder, at
  any depth, are listed in the clone's drawer, marked *not counted*, but don't make it stale: a
  small tool already on its newest versions, or a test's fixture, would otherwise head the list
  forever. A manifest that couldn't be read still counts. A clone none of whose modules counts has
  no age, says *none counted*, and goes in Fresh, where it stays, so it offers no mark.
- **Mark up to date:** once you've checked a clone's dependencies and found nothing to upgrade,
  the check on its row (or **Mark up to date** in its drawer) records that you checked it now,
  and the clone moves to Fresh at once, without reading anything again. It ages from then, into
  Aging after a month and Stale after three, unless one of its counted manifests or lockfiles
  changes later, which then counts instead. The row notes *checked 3d ago*, and the check turns
  green; a second click, or **Unmark up to date**, undoes it. A mark belongs to the repository, so
  every clone of it shares it, and it is kept on this Mac alone, in the app's database, like the
  rest of your triage. A clone that couldn't be read stays in Stale, marked or not, and offers no
  mark until it can be read, only an unmark when you marked it before.
- **Groups:** **Stale** (over three months), open, then **Aging** (one to three months) and
  **Fresh** (under a month), folded, each with its count, a month being 30 days. A clone whose
  age isn't known, as one that couldn't be read, goes in Stale too, last in the Oldest order, so
  nothing hides it. Each heading's legend says what its rows' edges mean: orange for *over 3
  months*, amber for *1–3 months*, green for *under a month* (a clone with nothing counted
  included), and faint grey for *age unknown*.
  Stale shows even with nothing in it (*No clone has stale dependencies.*); an empty Aging or
  Fresh doesn't. A group you open or fold stays that way, when you come back and after a
  restart. The rail's badge counts the clones in Stale, those whose age isn't known included.
- **Sort:** **Oldest** (the dependencies that changed longest ago first, to start with),
  **A–Z** (by `owner/name`, ignoring case), or **Modules** (the most `go.mod` and
  `package.json` files first) orders every group, and the repositories you
  [pinned](snooze-dismiss-pin.md#pinned-and-muted-repositories-and-orgs) come first in each, whichever you pick. The
  order you pick stays after a restart, though a [backup](backing-up.md) doesn't hold
  it.
- **Search** filters every group by `owner/name`, opening each while it searches. The **Go**
  and **JS** chips count the clones the search leaves with that kind of module, and a click
  keeps only those (*Show only the 12 clones with a Go module*); a second click shows every
  clone again.
- **The head** says how many clones and modules the page lists and when it read them (*175
  clones · 318 modules · read 2m ago*), and under it which clones it leaves out, the
  repositories you [muted](snooze-dismiss-pin.md#pinned-and-muted-repositories-and-orgs), those whose origin isn't on
  GitHub, and those with no manifest at the commit read (*14 muted and 3 not on GitHub left out
  · 37 with no go.mod or package.json*). While it reads, the counts give way to how far it has
  got (*Finding your clones…*, then *Reading go.mod and package.json · 45 of 151 clones*), so the
  rows never move.
- **Read again** reads every clone now. Otherwise the page serves its last read for five
  minutes, and reading again a clone whose commit hasn't moved costs only a look at its refs,
  so it stays quick across hundreds of clones. The Scripts tab reads it each time it shows,
  whichever page is open, so the rail's count is current. A read that fails keeps the last
  one, marked *Couldn’t read the clones · 14m old*, with why in its tooltip.

**Each row** is one clone on one line, its parts in columns its whole group shares, so they line
up down the list. When the list is narrow, the stalest manifest and the notes move to a second
line under the name, and narrower still the counts and the tools join them there. From the left:

- **The edge,** in its group's color, or faint grey when the clone's age isn't known.
- **The repository:** its owner, muted, and its name, with the clone's folder in its tooltip, and
  a pin after it when you pinned it.
- **Its modules,** counted by kind (*Go 3 · JS 1*), and **the tools** it is set up for (*magex ·
  pnpm*: magex by its files, a JavaScript package manager by the packages that use it).
- **Its stalest manifest,** the counted one whose dependencies changed longest ago (none when
  none counts), or, in amber, why the clone couldn't be read.
- **Its notes:** an upgrade an agent works on there, as in *Claude · working · 4m* (its prompt
  and worktree in its tooltip), then *checked 3d ago* while you have marked it up to date (when,
  and what the mark does now, in its tooltip), then how the clone was read: *none counted* (none
  of its modules counts toward its age), *age unknown* (no commit that changed its counted
  manifests was found, or why not), *read at HEAD* (and why), *shallow* (its history
  stops at its shallow commits, so its dependencies may be older than shown), and *not all read*
  (it holds more manifests than Deps reads, 200, and those nearest its top were read). Each says
  more in its tooltip.
- **The age** of that manifest's last change, a mark notwithstanding; its tooltip gives the date,
  the commit's subject, when you marked it up to date, and what was read (*As of origin/master,
  fetched 3d ago*, or *Read at HEAD* and why).
- **The check** marks the clone up to date, or, green while it is, unmarks it (see *Mark up to
  date*, above).
- **Upgrade…** opens the sheet. It stays quiet until you point at the row or move the focus into
  it.
- **The chevron** unfolds the row's drawer, and so does a click anywhere else on the row (a click
  that ends selecting its words opens nothing); from the keyboard, → unfolds it, ← folds it, and
  ↑ and ↓ move between the rows, across the groups. Several drawers can be open at once, and
  they stay open when the page reads again.

**The drawer** has a line for each module, those that count first, each part stalest first: its
kind (*GO* or *JS*), its manifest's path (its module path or package name, toolchain, lockfile,
and workspaces in its tooltip), *not counted* after it when it doesn't count (why in its
tooltip), its Go version (*go 1.22*) or package manager, its dependencies (*4 direct · 9
indirect*, or *48 deps · 12 dev*, with any peer and optional ones), the subject of the commit that
last changed it and its age, and a **Reveal in Finder** button that shows that manifest. A manifest over 1 MiB,
one that doesn't parse, or one whose history couldn't be read says so in amber under its line.
The clone's workspace files follow (*Workspaces: go.work · pnpm-workspace.yaml*), and at its foot
**Open in Terminal** opens your terminal in the clone, running nothing, **Mark up to date** (or
**Unmark up to date**) marks it as its row's check does, and **Upgrade dependencies…** opens the
Upgrade dialog.

**The Upgrade dialog** starts a coding agent on the repository itself rather than on a pull
request or an issue. It is the launch options' own dialog, as an item's **Launch options…** opens
it, and only as tall as what it holds. Under its title it names the clone, then its manifests by
kind (*Go: go.mod, tools/go.mod (magex) · JS: frontend/package.json (pnpm)*) and what the app
can't tell of the repository (below); then, in the launch options' sections:

- **Upgrade:** **Minor and patch**, chosen to start with (*Newest versions within each major; the
  PR lists the majors found.*), or **Majors too** (*Also moves to new majors, refactoring code
  where needed. Bigger, and it may break things.*), which keeps only the majors whose checks
  pass after a reasonable fix. Each is a starter in [Prompts](prompts.md), for repositories,
  offered while your library keeps it, and its **Edit** closes the dialog and opens that prompt
  on the Prompts tab, scrolled into sight in the library. **Another prompt** offers any other
  prompt for repositories you wrote. With no prompt for repositories at all, the dialog says so,
  with **Open the Prompts page**.
- **The agent, profile, model, and effort,** plan mode, and Codex's Fast mode, exactly as the
  launch menu offers them, starting from the repository's [launch defaults](launch-defaults.md) and
  the prompt (both starters start out of plan mode). There is no situation, and no outside
  contributor's override, since a repository's launch is trusted, and nothing is saved as a
  default, where its choices would change every launch on the repository's items too.
- **Where it runs,** in place of the worktree choice: *Runs in a new worktree, deps-20261121,
  off origin/main. Your clone isn’t touched.* The dialog plans the launch as it opens, so this
  names the worktree it would make (`deps-20261121-2` for another the same day, and so on) and
  the branch it starts from, and anything that would refuse the launch, such as every name for
  today's worktree being taken, shows at once. The worktree is always new, on a branch of its
  own (`worktree-deps-20261121`), made from the default branch, fetched first, so an upgrade
  never builds on commits a merged pull request already took.
- **Preview the launch** shows exactly what it would run, the prompt filled in, without starting
  anything.
- **The notice** says what the app can't tell: that GitHub hasn't said whether you can push to
  the repository (if you can't, the agent stops at the push and says so), or that the repository
  is a fork, so the pull request opens on the fork itself. A repository GitHub says is archived,
  disabled, or locked, or where your access is read or triage alone, can't be upgraded: the
  dialog says why, and **Start upgrade** stays disabled, as it does while the settings are being
  worked out or the launch planned, or when either fails.
- **Start upgrade** starts it. As with any launch, a session already running in the folder it
  would start in holds it back (*A session is already running here*), with **Show in terminal**
  and **Start another**, which starts beside it. Once the upgrade starts, the dialog closes, as a
  launch from a card says nothing; it stays open only when the launch worked something around,
  to say so (*The upgrade started in deps-20261121.*, then what it worked around, and **Done**).

Only one sheet or dialog is open over the tab at a time: a release the app opens for you, or a
run it shows you, takes the Upgrade dialog's place.

**After it starts:**

1. Your terminal opens with the agent in the new worktree. It reads the repository's own rules
   and its recent pull requests, upgrades the Go modules (with `magex deps:update` where the
   repository uses magex) and the JavaScript packages (with the manager its lockfile or
   `packageManager` names), runs the repository's own checks, commits, pushes the branch under
   its own name (`git push -u <remote> HEAD`), and opens the pull request against the default
   branch (`gh pr create --repo <repo> --base <default>`): what moved, in a table per manifest,
   what it held back, the advisories it fixes, what it ran to check it, and the open Dependabot
   or Renovate pull requests it supersedes. It never force-pushes, pushes to the default branch,
   or merges, and when it's blocked, it stops and says what you need to do.
2. The [Worktrees view](worktrees-view.md) lists the worktree, and once its branch is pushed
   and the pull request open, links it to that pull request by its branch.
3. The pull request shows in the inbox under **Yours**.
4. After it merges, the worktree is cleaned up like any other the app made, while **Clean up
   after merging** is on, with the branches **Settings → Worktrees** deletes.

The session is followed like any other ([Following a session](following-a-session.md)): the
clone's row shows how it stands (the newest still running, or one that ended in the last day),
and the menu bar and the banners name it by its repository and prompt, as in **example-repo ·
Upgrade dependencies**; with no pull request or issue to start on, it can't be resumed from the
app. A `git fetch --prune` of your own that drops the branch's remote-tracking copy (as it does
once GitHub deletes the merged branch) before the cleanup sees the merge leaves the worktree
linked to nothing: remove it by hand on the Worktrees tab.
