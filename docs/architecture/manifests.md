# internal/manifests

`internal/manifests/` is the Deps page's service. `List` reads every clone in the repository
locations whose `origin` is on GitHub (core's `hooksClones`, the Hooks page's: the roots, the
clones in them, and each one's origin through `localrepos.Inspect`) and whose repository the
user didn't mute, itself or its org (its `RuleSource`, core's `releaseRules`, the rules Release
reads; rules that can't be read are logged, and the last ones read stand), and lists each with
its Go modules' `go.mod` and its JavaScript packages' `package.json` files and how long ago
their dependencies last changed, stalest first by when each one's age counts from
(`Clone.AgeFrom`, below), those with none last, a pinned one marked (`Clone.Pinned`) for the
window to put first. The muted clones, those whose origin isn't
on GitHub or can't be read, and those read that hold no manifest are counted (`Page.Muted`,
`WithoutGitHub`, `WithoutModules`), never listed; a clone that couldn't be read is listed with
why (`Clone.Error`). Everything is read from the clone's own git objects at one commit, never
its worktree or a package registry, through gitread's allowlist
([internal/gitread](gitread.md)), so reading a clone runs no program it names and reaches
no network, and nothing it reads is stored: what it reads is kept in memory alone, and all it
keeps in the database is the user's own marks up to date (below).

**The commit read.** A clone is read at origin's default branch as it last fetched it (gitread's
`TrackingCommit` of `refs/remotes/origin/<default>`; `Clone.Tip` says `origin/<default>`, and
`Commit` names the commit), so a clone that is behind, or on another branch, still shows what is
on GitHub, at the commit an upgrade starts from. The default branch is worked out by the rule
Repo scripts shares (`gitread.DefaultBranch`): the one the app stored (its `DefaultBranches`,
which core fills with the inbox's store), else the one the clone's `refs/remotes/origin/HEAD`
names, else the first of origin's `main` and `master` the clone has, that guess's own read kept
so it reads once. A clone with no such ref (one that never fetched the branch, say) or no
default branch known is read at HEAD, which its row says (`AtHead`, and `AtHeadWhy` in words).

**A clone's read.** One `git ls-tree -r -z -l --full-tree <commit>` (gitread's `Tree`) lists
every file with its object and size, and the picker (`tree.go`) keeps magex's signs
(`toolsigns.MageX`) and, outside every `vendor`, `node_modules`, and `testdata` folder at any
depth, each `go.mod`, `package.json`, `go.sum`, JavaScript lockfile, `go.work`, and
`pnpm-workspace.yaml`. Only a regular file counts (`TreeEntry.Regular`: never a link or a
submodule), and a manifest or lockfile whose path gitread won't pass to git
(`ValidManifestPath`, `ValidLockfilePath`) is left out. The manifests are read the shallowest
first, at most 200 (`maxModules`; a clone with more is `Truncated`). A Go module's lockfile is the
`go.sum` beside its `go.mod`; a package's is the nearest of `pnpm-lock.yaml`,
`package-lock.json`, `yarn.lock`, `bun.lock`, and `bun.lockb`, in its folder or one above, up to
the tree's top, the one its own manager writes first in each folder when its `packageManager`
field names one. The modules are read four at a time (`moduleParallel`), so a monorepo's
hundred histories fit the clone's time. A module's age (`ChangedAt`, with its commit's subject,
one line of at most 200 runes) is the newest commit, merges left out, to change its manifest or
its lockfile, one `git log -1` each (gitread's `LastChange`). Its manifest, read with `git
cat-file blob` (`Blob`) only when the clone holds it and the tree gives it at most a mebibyte
(`maxManifest`), gives a Go module's module path, `go`
directive, toolchain, and how many modules it requires directly and as `// indirect`, and a
package's name, package manager (its `packageManager` field's, else its lockfile's writer),
workspace patterns, and how many dependencies, dev, peer, and optional dependencies it names:
counts, never the names. `go.mod` is parsed by hand, line by line by go.mod's grammar, the
single-line and block forms alike (`gomod.go`: golang.org/x/mod is no dependency of the app's,
and a directive it doesn't read is skipped, its block too, so a newer Go's `go.mod` still reads),
and `package.json` with `encoding/json` (`packagejson.go`: a byte order mark read past, each
field kept raw, so one of a type npm doesn't use reads as unset, and at most 100 workspace
patterns); every word kept is one line of plain text, capped, and both parsers are fuzzed
(`FuzzParseGoMod`, `FuzzParsePackageJSON`). What can't be read is said in the module's `Problem`
(a manifest too large to read, one a partial clone never fetched, whose history is still read
but whose contents no read here fetches, a `package.json` that isn't a JSON object, a `go.mod`
that names no module, a log or a blob git refused, or one the clone's time ran out before). The
clone's tools (`Tools`: magex by its signs, and pnpm,
npm, Yarn, and Bun by the managers its packages use, pnpm's workspace file counting for pnpm)
and its workspace files (`Workspaces`, its `go.work` and `pnpm-workspace.yaml`) come from the
same tree. A shallow clone (`Shallow`, gitread's `Shallow`) has dates that stop at its shallow
commits, and when the clone last fetched (`FetchedAt`) is the modification time of its
`FETCH_HEAD` (gitread's `FetchHead` says where git keeps it, which `os.Lstat` looks at, never
into).

**What counts.** A clone is as old as its stalest module that counts (`Module.Uncounted`, set
as the module is read, and kept with it): a manifest under a `fixtures` or `__fixtures__` folder
(`UncountedFixture`), or else an `examples` or `example` folder (`UncountedExample`), at any
depth, by its path alone (`uncountedAt`, beside the picker's skipped folders), and a manifest read
that names no dependency of any kind (`UncountedNoDeps`: a `go.mod` with no `require`, direct or
`// indirect`, a `package.json` with no dependencies, dev, peer, or optional dependencies) are
still listed, and still read in full, but don't count. A manifest that couldn't be read (too
large, one a partial clone lacks, one that doesn't parse, a `go.mod` that names no module, a blob
git refused, or one the clone's time ran out before) still counts, unless its folder says
otherwise, since nothing says it names nothing. A clone's modules are sorted with those that count
first, then the rest, each part stalest first and those whose age isn't known last; `Counted`
says how many count, and `Oldest` is the stalest counted one's `ChangedAt`, "" when none counts.
A clone none of whose modules counts has no age: the window puts it in Fresh, for good, since
nothing in it can go stale, rather than in Stale with the ages unknown.

**Marks up to date.** The user may mark a repository up to date after checking it
(`MarkChecked(ctx, "owner/name")`, a row's or a drawer's Mark up to date; `ClearChecked` undoes
it), a triage of their own, as snoozing an item is. The marks are the one table the package owns,
`deps_checks` (`0113_deps_checks`: the repository, case-insensitive, and `checked_at`, one row
each, an unmark deleting it), written and read through its own sqlc queries
(`internal/manifests/internal/queries`) over the database core hands it (`Config.DB`; without
one nothing is kept), never through the store package. A mark is the repository's, so every
clone of it shares it. It is never part of a read of the clones: every page served, by `List`,
`MarkChecked`, or `ClearChecked`, reads the marks again (one query) and puts them on the last
read's page (`withChecks`): `Clone.CheckedAt`, and `AgeFrom`, the later of `Oldest` and the mark,
so a mark makes the clone fresh and it ages back into Aging and Stale a month and three after,
while a counted manifest that changes after the mark wins; then sorts the clones by `AgeFrom`.
A clone that couldn't be read has no `AgeFrom`, marked or not, so a read that failed stays in
Stale. Marks that can't be read are logged, and the last ones read stand (`lastChecks`), as the
pinned and muted repositories do; a mark or an unmark that can't be written fails with the
repository in its words, and a name that isn't `owner/name` with `ErrInvalidRequest`. Marking
reads no clone, so the page the window gets back moves the clone at once.

**What is kept.** What a clone's read found is kept in memory (`keptClone`) by its folder
resolved (`paths.Resolved`) and the commit read, with where its `FETCH_HEAD` is and its time, so
reading the clone again at the same commit runs no git beyond what found the commit: one
rev-parse when the app stored the default branch, and one more, origin's HEAD, when it didn't.
Whether it is shallow is read again only once it fetched since. A read in which a git read
failed in a way the next may not is not kept, but one git cut off for printing more than its cap
(`execpath.ErrOutputLimit`) is, since it would again at that commit; a clone no longer read
keeps nothing. The page itself is served as it is for `FreshFor` (five minutes) while the
repository locations and the pinned and muted repositories are as it was read under; `List`
with refresh set reads every clone again, as does any call once the page is older or either
changed.

**A read of the clones.** One runs per set of locations at a time (a `flight.Group`; a caller
that comes meanwhile gets its answer), apart from the window's call that started it, within
`ScanTimeout` (five minutes): a caller whose context ends stops waiting, and the read goes on and
is kept for the next. It reads four clones at a time (`scanParallel`), each within a minute
(`cloneTimeout`), its origin's read included, so one on a disk that stopped answering doesn't
hold up the rest; a clone whose time runs out keeps the modules it read, says of each other one
that it wasn't read, and is read again whole by the next scan, its read not kept. It tells the
window how far it has got (`event.ManifestsProgress`,
`manifests:progress`, through `internal/progress`, at most four times a second: finding the
clones, then reading them, counted). A read that fails keeps the last page, with why
(`Page.Error`), unless there is none to keep or the locations changed; one cut short keeps
nothing. `Scans`, a desktop worker (core's `ManifestsScans`), holds every `List` call and every
read to the app's lifetime: the quit ends every one under way and refuses later ones, and the
worker returns once they have, so the shell never leaves one reading behind it.

**The terminal and Finder.** A row's **Open in terminal** (`OpenTerminal(dir)`) opens the
terminal chosen for launches in a clone the repository locations hold (`localrepos.ByFolder`,
the clone named by its folder with `~`), through its `FolderOpener` (core's
`worktreeTerminal`), running nothing there, or fails with `ErrNoTerminal` without one. A
module's **Reveal in Finder** (`Reveal(dir, path)`) shows a clone's manifest or lockfile in
Finder through its `Revealer` (core's `open -R`; off macOS there is none, and it fails with
`ErrNoFinder`): the path must pass `ValidManifestPath` or `ValidLockfilePath`, checked before
anything is read, and the file must be in the clone's checkout now and inside the clone once
its links are resolved; Finder only shows it, so nothing reads or runs it, and a named pipe
there is shown, never opened. A row's Upgrade is a repository's launch
([internal/launcher](launcher.md)). The repoguard boundary test keeps the package off the
store, the GitHub client, settings, the inbox, core, scriptrun, the launcher, the worktrees,
release, hooks, scripts, setup, integrations, the desktop shell, and notify: it reads the clones
through gitread and its own interfaces, which core adapts, keeps its marks through its own
queries rather than the store, starts no run, and asks nothing of GitHub or a registry.
