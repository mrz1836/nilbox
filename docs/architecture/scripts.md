# internal/scripts

[Docs](../README.md) › [Architecture](../README.md#architecture) · Code: [`internal/scripts`](../../internal/scripts) · Guide: [Repo scripts](../guide/repo-scripts.md)

`internal/scripts/` is Repo scripts' service. While the Scripts integration is off (its own switch,
whose automatic is on while mage-x or go-pre-commit is), `List` returns the page marked off and
`Run` and Run again refuse. Otherwise `List` reads a clone's sources in the page's order (the user's
own scripts, then `t3.json`'s `scripts`, the subset of `.vscode/tasks.json` the app can run, read
through `jsonc.go`, and magex's targets), as the `scripts.repoFiles` and `scripts.magexTargets`
switches in `integrationprefs` turn them on, and runs nothing in a clone: the files are read,
magex's built-ins come from `magex help:commands json=true` run in a scratch folder of the app's own
(a clone's `.github/env` files would override magex's environment, `PATH` included), kept per copy,
a list that failed too, until another copy or `List`'s refresh, and the repository's own targets
from parsing `magefiles/` with `go/parser` by magex's own rules, kept until a file there changes.

An entry the app can't run is listed greyed with the reason in words: an editor variable such as
`${file}`, `${command:…}` or `${config:…}`, a password input, another task type, a character that
hides what runs (a control character but tab, DEL, or a bidirectional text control, in any command,
argument, folder, variable, shell, or input value), a task shell outside the POSIX family (sh, bash,
zsh, dash, ksh, mksh) given arguments to quote, a process task setting variables for a program whose
path holds an `=`, magex's own updaters (`update`, `update:*`, `version:update`, and their aliases:
Settings → Integrations updates magex), and the custom targets while `magefiles/` holds a link to a
folder. A pickString option shows its value beside a label that differs (`Option.Shown`).

A command that tags a version (`version:bump`, `version:tag`, `git:tag`, `git:tagupdate`,
`git:tagremove`, or a magex alias of one) points to Release (`Anyway` runs it), checked again on the
command as it will run; goreleaser's `release:*` targets are plain built-ins. A command that
upgrades the repository's dependencies in the clone itself, on whatever branch it is on, leaving the
change uncommitted, points to Deps the same way (`Entry.Deps`, `ErrDeps`, whose words offer Deps or
running it anyway), since Deps does it in a fresh worktree, with a pull request: magex's
`deps:update` or `mod:update`, by name or a magex alias of one; `go get` with `-u` or `-u=patch`; a
package manager's own upgrade command, after any of its flags (npm's or pnpm's `update`, `up`, or
`upgrade`, Yarn's `upgrade`, `up`, or `upgrade-interactive`, and Bun's `update`, never its
`upgrade`, which upgrades Bun itself); and `ncu` or `npm-check-updates`, at any version, with `-u`,
`--upgrade`, `-i`, or `--interactive` (`guard.go`, each program by its name alone, quoted, or at the
end of a path; a script run by name, as `pnpm run update`, is the repository's own). `deps:tidy`,
which only tidies, stays a plain built-in, and `deps:update` left the page's chips
(`commonTargets`). `RunRequest.Anyway` runs either kind, and a run's intent keeps it, so Run again
runs a command run anyway the same way and refuses one recorded without it. Both checks read a
command's words as it runs joined into one line (`bumps`, `upgrades`), inputs and aliases resolved,
so a program and its arguments read as the same command written in a shell would.

A command from the repository's files asks before its first run and whenever it changes (D13): its
trust is the SHA-256 of the canonical JSON of its effective form, `${workspaceFolder}` and
`${env:…}` as written, a custom magex target's holding every file under `magefiles/` by its bytes
(only those: not `go.mod` or the packages it imports, the known limit), recorded in `script_trust`
under the clone's origin with the display text trusted; magex's built-ins need none. `Run` hands
`scriptrun` a `KindScript` run whose `Before`, under the clone's lock, reads the source again from
its bytes, refuses a command whose hash isn't the one worked out as the run was asked for, works the
command out, refuses a version bump or a dependency upgrade not run anyway, then one that isn't the
one the user confirmed (or, with no confirmation, the one they trusted), and only then remembers a
confirmation they asked to keep, so what runs is what they saw. A refusal that asks is a
`QuestionError` whose `Question` holds the command with the chosen inputs filled in;
`desktop.MarshalError` sends any error's `Detail` to the window beside its message.

A repository's shell command (a `t3.json` script or a shell task, which assume POSIX quoting, and
which the app quotes for one) runs as `[$SHELL, -c, command]` when the login shell is of the POSIX
family (sh, bash, zsh, dash, ksh, mksh), else under `/bin/sh` (a task's `options.shell` as written,
with `-c` after its arguments as VS Code adds it), a process as its argv, a magex target as `[magex,
target, key=value…]` with magex's `RunEnv`, and a `dependsOn` chain, or a part in another folder, as
one `/bin/sh -c` line of shell-quoted parts joined by `&&`. Its `Rerunner` works Run again out from
the run's intent and asks nothing, so it runs a repository command only while a trust record under
the clone's current origin matches it; anything else is run from the page, which asks.

The user's own scripts live in `user_scripts`, which it owns, in their order (positions 0, 1, 2,
numbered again in the `store.InTx` of every create, delete, duplicate, or move), each under its
ID, which is this Mac's alone and given again once deleted (the table has no AUTOINCREMENT), and
its `uuid`, which names it for good ([Records named for good](store.md#records-named-for-good-uuids)):
`insert` keeps one it is given when canonical and free (`uuidFor`), a restore's or an import's,
and mints one otherwise, and `GiveUUIDs`, which core runs at startup beside the restore, gives a
row an older build inserted without one its own. The bound `Save`
creates one (ID 0, at the end, under a new UUID, never one the window sent) or edits one, keeping
its UUID, checking that the name and command are there, on one
line, with no character that hides what runs, that the scope (`all`, `go`, or `repos`), the place
(an `integrationprefs.RunsIn`, `''` for "As set in Settings"; `validPlace` checks it and a run
request's alike), and the icon (t3.json's five) are known, and that a `repos` scope names at least
one owner/name, kept once in any case; `Delete`, `Duplicate` ("<name> copy", right after the
original), and `Move` (up or down past the nearest script shown in the clone named, so the move
shows on that page) change the order, `Duplicate`'s copy under a UUID of its own. After a save, a
delete, or a copy it tells `Config.Changed`, so what names a script, a routine's step, follows it.
`RepoNames` lists the clones' GitHub repositories for the
editor's Chosen list and, through core, the launch defaults' targets (the Pinned and muted field and
Add defaults), reading origins four at a time and keeping each until the clone's `.git/config`
changes, forgetting a clone no longer listed, and starting no more reads once the window cancels the
call.

`List` shows the ones whose scope includes the clone: every clone, one with a `go.mod` at its root,
or its origin among the chosen repositories, in any case. They need no trust (D13), keep the hidden
character guard, point a version bump to Release and a dependency upgrade to Deps, and run exactly
as written, no `${…}` filled in, as `[$SHELL, -c, command]` in the user's login shell whatever its
family (fish and tcsh included, since the line is theirs and the app quotes none of it; `/bin/sh`
only for a login shell that isn't an absolute path), in the clone, with the run env plus
`Variables()`, which the page also carries (`Page.Variables`) for the editor: `<SLUG>_REPO`,
`<SLUG>_REPO_DIR`, and `<SLUG>_DEFAULT_BRANCH`, named from `brand.Slug`, set alone in the terminal.
The default branch, and so the page's *From branch* note, is the one the app stored, else the one
the clone's `refs/remotes/origin/HEAD` names (gitread's `OriginHead`, which never asks origin), else
`main` or `master` when the clone is on one, else '' (`gitread.DefaultBranch`, the rule Deps shares,
its guess here the branch the clone is on). Their own place beats the setting, and a request's place
beats both.

A run's `Before` compares the SHA-256 of what the saved script runs (its name, command, place, and
Ask before running) with the one worked out as the run was asked for, and refuses one saved again in
between ("changed as it was starting"). A run's intent records the script's UUID beside its ID
(`Intent.UUID`), and Run again works the command out afresh from the saved script by its ID and
refuses one deleted, one whose UUID isn't the intent's (its ID given since to another script:
`mayRun`; a run from before UUIDs has none, and finds its script by ID alone), no longer shown
there, or set to ask before running
(`ErrAsksFirst`), which only the page asks: Ask before running is the window's confirmation, which
`Run` leaves to the user.

The bound `BranchStatus` tells the header beside the branch where the clone's branch
stands against the branch it follows on origin (`BranchState`: up to date, behind, ahead,
diverged, detached, no upstream on origin, git in the middle of something, unknown,
refused, not asked, or off while the Scripts integration is, which reads nothing, with a
worded note), read through its `BranchGit` (gitread) from the clone's own refs and one
`RemoteBranch` ls-remote of the followed branch alone (`ls-remote --heads origin
refs/heads/<branch>`, never every branch and tag), never a fetch, and only for an origin
on GitHub unless refresh is set (the user's click): another host's origin is
`BranchNotAsked` until the user checks, then served from the kept answer; a clone the pull would
refuse for its own config (gitread's `CloneConfig`, read locally, before origin is asked)
is `BranchRefused`; every note built from a gitread failure goes through `gitNote`
(`uierr.New` of gitread's words, read with `uierr.Message`); origin's answer is kept per
clone and branch for `OriginKept` (five minutes) unless refreshed, the clone itself is
read on every call, and an origin commit the clone lacks counts as behind. Only behind is
offered **Pull**, and a refused pull's error is the whole sentence the window shows (it
names the branch), a run whose record can't be read letting go of the button.

The bound `Pull` (`pull.go`) returns a `scriptrun.KindPull` run's ID (thirty minutes' limit), in the
app, under the clone's lock, of the branch the last status was worked out on, an upstream
`gitread.ValidBranch` passes: its `Before` refuses a clone now on another branch, one whose branch
follows none on origin, and one git is in the middle of something in, and works out with
`gitread.RemoteSetup`, asking origin nothing (the pull's own fetch is the one round trip), what git
runs with: a clone whose own config holds a setting gitread refuses is refused, naming the settings;
otherwise gitread's own git, an ls-remote's environment (`GIT_TERMINAL_PROMPT=0`, the user's
transports in `GIT_ALLOW_PROTOCOL`, the user's ssh command or a batch-mode one, and
`GIT_NO_LAZY_FETCH=1` but in a partial clone, whose promisor remotes are weighed too), `GIT_DIR` and
`GIT_WORK_TREE`, and every setting (`gitread.Hardening`, no alternate refs command, no automatic
maintenance, the clone's own password program and credential helpers set aside, no cookie saved)
passed through `GIT_CONFIG_COUNT`.

Its intent (`PullIntent`) is recorded once `Before` ran, with the upstream and the branch's commit
it read. It runs `git pull --ff-only --no-rebase --refmap= --no-tags --no-autostash
--no-recurse-submodules --no-verify-signatures --no-stat --upload-pack=git-upload-pack origin
+refs/heads/<b>:refs/remotes/origin/<b>` as argv (which `TestNoGitCommandChangesAClone` parses and
holds to that exact list), nothing else in it, with `LC_ALL=C` so git speaks the window's language,
the environment handed to the runner through the spec's `BeforeEnv`: the fetch updates that one
remote-tracking ref, never one the clone's own `remote.origin.fetch` maps.

Stop is held back (`StopRefused`) for the whole run, since git writes its output only as it exits,
and so is a quit, which leaves it going for the next start; only the kind's limit cuts it off, and a
pull it cuts off is read again (`cutOff`: the branch it is on, its commit against the one before,
whether `index.lock` is left, through gitread's `IndexLocked`, and what git is in the middle of) and
said to be cut off partway, the clone perhaps partly updated, with `git status` in a terminal to see
where it stands. Its `Finish` checks the clone is still on the branch (a switch while git ran fails
the run, saying git may have pulled into the other), reads the branch back and words the summary
(updated from one commit to another, how many commits, already up to date, or git's last error line,
with any of the clone's own settings set aside, which may be why), and keeps the commit git fetched
as origin's for the next status, leaving when origin was asked as it was, or forgets origin's answer
when that can't be told. A pull whose end wasn't seen is settled from the branch alone: moved on
from the commit `Before` read is pulled. Core registers the `Verifier` (`scripts.NewVerifier`) for
pulls, so one left going at a quit comes back after a restart with Stop held again and that same
`Finish`. Run again pulls the same branch, its `Before` checking everything afresh.

## A routine's run of one of the user's scripts

`Routines` (`routines.go`, its own type, never bound, since a `RoutineRun` holds a function) is how
Autopilot's Run one of my scripts reaches the user's own scripts: `Mine` lists them, each with its
UUID and its look (unchanged by the UUID, so a look the user confirmed stays good) (the hash a run's `Before` compares, of its name, command, place, and Ask before running) and
where it shows, in words; `Run` works out the very spec `Run` does for one of them in a clone, with
the place always the app, whatever the script says, refuses it when its look isn't the one the
routine checked (`ErrChanged`), never runs one anyway (a version bump or a dependency upgrade is
refused, `ErrRelease` or `ErrDeps`, checked on the command as it runs, aliases resolved), and sets
its `Finish` (which keeps how the run ended, the user's Stop, its time limit, or the quit) and its
`Recorded`, which tells the routine the outcome once it is recorded (`RoutineEnd`). Ask before
running, which the window asks of the user for `Run`, is Autopilot's to ask before it calls this one
(**Schedules, the digest, the script step, and a routine's week**, under `internal/autopilot`).
