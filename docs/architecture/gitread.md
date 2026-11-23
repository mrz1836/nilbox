# internal/gitread

[Docs](../README.md) › [Architecture](../README.md#architecture) · Code: [`internal/gitread`](../../internal/gitread)

`internal/gitread/` is the read-only git that release, hooks, Repo scripts, and Deps share: an
exact-argv allowlist plus validating predicates (a branch, a tag, a commit, a `v*` pattern, an
absolute clean folder) that refuses anything else with `ErrGitCommandNotAllowed` before git runs,
and typed helpers over it (the current branch, origin's default branch as the clone last learned it
from its own `refs/remotes/origin/HEAD`, an operation in progress, uncommitted changes, each dirty
file with what the index holds for it, unpushed commits, the commits between two, tags here and on
origin, signing, the hooks folder, a commit's tree, the newest commit to change a manifest, a blob:
`OriginHead`, `HookPaths`, `IndexLocked`, `Tree`, `LastChange`, ...).

Every command runs as `git -C <dir> -c core.fsmonitor=false -c submodule.recurse=false` with
`GIT_OPTIONAL_LOCKS=0`, `GIT_TERMINAL_PROMPT=0`, `GIT_NO_LAZY_FETCH=1`, and an empty
`GIT_ALLOW_PROTOCOL` (no read reaches a transport, so a partial clone's missing object is never
fetched through a URL the clone names), without `GIT_CONFIG` (which would hide the clone's settings
from the guards' reads), never touches `core.hooksPath`, and an `ls-remote` gets a 15-second limit
and a batch-mode ssh unless the user's environment or own config picks one.

A `status`, which reads files and so would run their filter drivers, first reads the drivers with
`config --show-scope` and runs with every one the clone's own config defines turned off, Git LFS's
too, since git-lfs's clean writes into the clone (its clean, smudge, and process emptied and
required off), and with `--ignore-submodules=dirty`, so it runs no status inside a submodule, whose
own config could name one. A diff against the worktree (`WorktreeDiff`, the run view's Diff) reads
files too, and runs the same way (`readsFiles`), with `--no-ext-diff` and `--no-textconv`, which
every diff passes, so no external diff or textconv the clone names runs either.

An `ls-remote` likewise first reads, with `config --show-scope`, the settings it weighs, and
origin's URL with `ls-remote --get-url` (which asks origin nothing): a remote helper (a `<name>::`
URL after `insteadOf`, or `remote.origin.vcs`) is refused (`ErrRemoteHelper`), since git falls back
to an alias the clone defines; only the file, http, https, and ssh transports run, as the user's own
config and environment allow them, through `GIT_ALLOW_PROTOCOL`, which beats every config; the
clone's own ssh command, `core.askPass` (git runs it even with `GIT_TERMINAL_PROMPT=0`), and
credential helpers give way to the user's own; an http or https origin the clone's own config
reroutes (its TLS checks, certificates, proxy, `remote.origin.proxy`, or resolved address) is
refused (`ErrCloneRoutes`), since the user's credentials, cookies, and headers for the real host
could go elsewhere; no cookie is saved, and a cookie file the clone names is dropped; and origin is
asked for git's own upload-pack with `--upload-pack` (git keeps the first `uploadpack` it reads, so
a `-c` can't replace the clone's).

An `ls-remote` that fails after setting anything aside returns a `SetAsideError` naming it, which
`Message` (the words the Release sheet and Repo scripts' branch status share for every gitread
failure) gives as the likely cause and where to set the clone's own settings instead: the user's
global config, with an `includeIf` for the folder, which counts as the user's own. Only the system,
global, and environment (`command`) scopes are the user's. Reading a clone runs no program the clone
names (the user's own, such as Git LFS's, still run). Its allowlist test asserts that no entry holds
a verb `TestNoGitCommandChangesAClone` guards.

For the git commands that do change a clone, run elsewhere (Repo scripts' pull, and worktree
creation's fetches, `ls-remote --symref`, and `git worktree add`), it hands over what they run with
rather than running them: `RemoteSetup` (origin) and `RemoteSetupFor` (a named remote, whose own URL
and remote helper it weighs), for one that reaches a remote, and `CheckoutSetup`, for one that
writes files and reaches no remote. Each reads the clone's whole config once, includes followed,
with `config --show-scope --show-origin -z --list`, and weighs every setting of the clone's own (the
local and worktree scopes, and whatever they include) by the three classes in `cloneconfig.go`, the
one place they are listed.

Inert (`inertKeys`): what git init, clone, remote, branch, submodule, sparse-checkout, and Git LFS
write, the notes editors and git clients keep, and git's many-files mode, which none of these
commands reads as a program, a place to write, or a way to reach a host:
`core.{repositoryformatversion, filemode, bare, logallrefupdates, ignorecase, precomposeunicode,
symlinks, autocrlf, eol, sparseCheckout, sparseCheckoutCone}`, `index.sparse`; `feature.manyFiles`
and the settings it turns on through git 2.56, `index.version`, `index.skipHash`, and
`core.untrackedCache` (each allowed alone too), which users of large repositories set for speed and
which, with any value, only choose how git writes the index in the clone's git folder
(`feature.experimental`, whose group changes from release to release, stays refused);
`remote.<n>.{url, fetch, pushurl, tagopt, prune, promisor, partialclonefilter, gh-resolved,
gtserviceaccountidentifier}`; `branch.<n>.<any>`; `extensions.{partialclone, worktreeconfig,
objectformat, refstorage}`; `user.{name, email, signingkey}`, `commit.gpgsign`, `tag.gpgsign`,
`gpg.format`; `pull.{rebase, ff}`, `fetch.prune`; `submodule.<n>.{url, active}`, `submodule.active`;
`lfs.repositoryformatversion`, `lfs.<url>.{access, locksverify}`; `rerere.{enabled, autoupdate}`;
`log.excludeDecoration`, `color.*`, `gui.*`; and `mergetool.*`, `difftool.*`, `merge.tool`,
`diff.tool` (and their gui ones), which only `git mergetool` and `git difftool` read, and which the
app never runs (`TestNoGitCommandRunsAMergeOrDiffTool`); `remote.<n>.github-pr-remote` among a
remote's notes; and what users and their tools write for commands the app never runs, `push.*`,
`rebase.*`, and `status.*` (no subsection; the worktrees' own status is `--porcelain` with its own
`--untracked-files`), `gitflow.*`, `init.defaultBranch`, `merge.ff` (the pull's `--ff-only` beats
it), and `merge.conflictStyle`, none of which names a program, a path to write, or a host
(`core.editor`, `sequence.editor`, and `diff.*` stay refused).

Neutralized (`neutralizedKeys`), as the setup never lets the clone's own value reach git:
`core.sshCommand`, `core.askPass`, `credential.helper` (for every URL or one), and the cookie
settings, which the remote guard sets aside for the user's own in every setup (a checkout's Git LFS
asks for credentials too), a failure then saying which (`Setup.SetAside`, `SetAsideNote`), and of
the rest of `credential.*` only `username` and `useHttpPath` (`protectProtocol` and
`sanitizePrompt`, which a command-line setting can't beat for one URL, refuse, and every setup sets
both on again, `credentialHardening`); `url.*.insteadOf` and `pushInsteadOf`, as each remote reached
is weighed by the URL git will use; `http.*` and `remote.<n>.proxy`, whose rerouting keys
(`routeKeys`, a remote's proxy) refuse a command that reaches an http or https remote, or one Git
LFS may run in, whatever origin's scheme (`cloneRoutes`: git-lfs reaches its server over https with
the user's credentials), with `ErrCloneRoutes`; `protocol.*`, which `GIT_ALLOW_PROTOCOL` beats;
`core.hooksPath`, `core.fsmonitor`, and `submodule.recurse`, which every setup sets again (husky
writes the first); and `maintenance.*` and `gc.auto`, as every setup turns automatic maintenance
off.

Refused, before anything runs, with a `CloneConfigError` naming the settings and where each is set,
which `Message` words with where to set them instead (the user's global config, with an `includeIf`
for the folder): everything else, `refusedKeys` (`branch.<n>.mergeOptions`) among them, so a hook
(`hook.<n>.*` runs whatever it says), a filter but Git LFS's own with exactly its own commands
(`lfsSettings`, `--skip` ones too), `core.worktree`, `include.*` and `includeIf.*` (an `includeIf`
of `gitdir:**/.git/worktrees/**` or `onbranch:` reaches only a worktree being made),
`core.alternateRefsCommand`, `gc.*` but `gc.auto` (`gc.recentObjectsHook` runs a program),
`lfs.customtransfer.*`, `lfs.standalonetransferagent`, `lfs.url`, `remote.<n>.{uploadpack, vcs}`, a
merge or diff driver, and any key a later git adds. `CloneConfig` runs that check alone, for Repo
scripts' branch status. The user's own scopes (system, global, and the environment's) are never
weighed, so an `includeIf` in the user's global config still counts as theirs.

Past the allowlist, the setup checks that git takes the folder as its clone's top folder, comparing
the two as files (`os.SameFile`, so a clone root whose case differs from the disk's still matches;
`ErrWorkTree`), weighs the remotes the command reaches exactly as an ls-remote's guard does (a
`RefusedError` naming the remote), and returns a `Setup`: git as `GitFinder` finds it, the finder
core hands the worktrees too (`ErrNoGit` when it can't); an ls-remote's environment (for
`CheckoutSetup`, and a setup's `Checkout`, the same allowing no transport) with `GIT_DIR` and
`GIT_WORK_TREE` set to the clone's folders, which beat its config, and `GIT_NO_REPLACE_OBJECTS=1`,
so a replace ref can't have a checkout of the commit the launch checked write another's files; and
every setting, `Hardening` (`core.hooksPath=/dev/null`, where git finds no hook, so no hooks
folder's hook runs, and `hook.<event>.enabled=false` for every event git fires, `hookEvents`, which
git 2.56 documents as turning off every hook of the event, the user's own config hooks too, but
which a git whose config hooks predate that switch may read as one hook's switch: the clone's own
`hook.*` settings are refused regardless; no fsmonitor or submodule recursion), `fetchHardening`
(`core.alternateRefsCommand=true`, so a fetch from a clone with alternates runs nothing to learn
their refs, an empty one making git try to run "" on every fetch; `maintenance.auto=false` and
`gc.auto=0`, so no fetch or merge starts maintenance), `credentialHardening`
(`credential.protectProtocol` and `sanitizePrompt` on), the remote guard's (the clone's own password
program and credential helpers set aside, no cookie saved), and any filter driver of the clone's own
but Git LFS's own turned off, passed through `GIT_CONFIG_COUNT` after any the environment passes
(`Setup.Settings` lists them), so the command's argv holds only its own words.

`LocalSetup` works out a `Local` for a command in a clone or a worktree that reaches no remote, the
worktree service's own status, removal, prune, unlock, and branch deletion: a read's environment (no
transport, no lazy fetch, no optional lock, no replace ref), `Hardening`, `fetchHardening`, and
every filter driver of the clone's own off, Git LFS's too, with `Local.Refused` saying when the
clone's own config holds a setting a setup would refuse. In a partial clone
(`extensions.partialClone`, `remote.<r>.promisor`) either may fetch a missing file's contents: its
promisor remotes are weighed as a remote is, and the setup leaves `GIT_NO_LAZY_FETCH` out and allows
the user's transports (`Setup.Partial`). Only an ls-remote has `RemoteTimeout`, whose end is
`ErrRemoteTimeout`, which `Message` words as origin not answering in time. `ValidBranch` is the
branch rule a command's argument must pass.

For Deps it reads a commit's contents from the clone's objects alone, never its worktree:
`TrackingCommit` (`rev-parse --verify --quiet refs/remotes/origin/<branch>^{commit}`, origin's
branch as the clone last learned it, peeled, so the answer is always a commit's ID, or '' when
the clone has no such ref); `Tree` (`ls-tree -r -z -l --full-tree <commit>`: every file of the
commit's tree, each subfolder's included, with its mode, its object's type and ID, and a blob's
size, as `TreeEntry`, kept as the caller's `keep` says; `TreeEntry.Regular` is a blob that isn't
a link, a blob the clone doesn't hold, which git lists with the size `BAD` as a partial clone's
unfetched blobs are, is kept `Missing`, and an entry of a shape it doesn't know is skipped);
`LastChange` (`log -1 --no-merges
--no-show-signature --no-mailmap --no-ext-diff --no-textconv --format=%ct%x01%s <commit> --
:(literal)<lockfile> :(literal)<manifest>`, the lockfile left out when there is none: the newest
commit to change either, its committer time read as UTC and its subject, as `Commit`, or none,
and in a shallow clone never older than its shallow commits); `Blob` (`cat-file blob <oid>`,
through no filter); `Shallow` (`rev-parse --is-shallow-repository`); and `FetchHead`, where git
keeps the `FETCH_HEAD` every fetch writes (`rev-parse --git-path FETCH_HEAD`, now among
`gitPathNames`), whether or not it is there. None of them runs a program the clone names: no
signature is checked, and no external diff or textconv runs. A manifest's and a lockfile's
paths have slots of their own (`slotLiteralManifest`, `slotLiteralLockfile`), validated by
`ValidManifestPath` and `ValidLockfilePath`, which Deps holds what it reads to (`treePath`: a
`go.mod` or `package.json`, or a `go.sum`, `pnpm-lock.yaml`, `package-lock.json`, `yarn.lock`,
`bun.lock`, or `bun.lockb`, relative and clean, no part empty, `.` or `..`, or starting with `-`
or `:`, valid UTF-8 with no control character or backslash, at most 1,024 bytes), and each is
passed to git only after `:(literal)`, so no wildcard or other magic is read in it; the lockfile
comes first in the two-path form, so that form without its last word is no other entry. A
tree's listing, a log, and a blob's contents are the repository's to make as large as it likes,
so each is capped (`outputLimit`: 64 MiB, 64 KiB, and a mebibyte and 64 KiB): gitread's own
runner, an `execpath.Exec` given that `Limit`, stops git once it prints more, holding none of
the output, and any other runner, as a test's, is held to it once it returns (`limitedRunner`);
either way the read fails with `execpath.ErrOutputLimit`, which `Message` words as git printing
more than can be read here. `DefaultBranch` is the rule Repo scripts and Deps share for a
repository's default branch, asking no one: the one the app stored, else the one the clone's
`refs/remotes/origin/HEAD` names (`OriginHead`), else the first of `main` and `master` the
caller's guess accepts (Repo scripts': the one the clone is on; Deps': the one the clone has of
origin's), else ''.
