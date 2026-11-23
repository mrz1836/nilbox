# internal/localrepos

[Docs](../README.md) › [Architecture](../README.md#architecture) · Code: [`internal/localrepos`](../../internal/localrepos) · Guide: [Launching a session](../guide/launching-a-session.md), [The Worktrees view](../guide/worktrees-view.md)

`internal/localrepos` finds the user's clones, in the clone root (`clones.root`, the primary,
which new clones go in) and the other repository locations (`clones.locations`, which
settings alone writes: each a folder looked into one level deep as the clone root is, or a
single clone, its kind worked out once as it is added). `Roots` holds them, each expanded
and its links resolved once; `Roots.Clones` lists the clones in all of them at once,
reading no git, a clone two locations reach listed once, the clone root's first, and an
extra location it can't read skipped and named (`Found.Skipped`), while a clone root that
can't be read is an error. A linked worktree in a location is left out for its clone, as
`CheckAddable` refuses one as a location: for the clone when the locations list it too,
wherever, or else for the first of its worktrees (a bare repository's included), which
stands for it, so each page lists a clone once however many of its worktrees sit beside it; `Holds` says whether the locations hold a clone, which every
service checks before it changes one; `Key` is the clone root alone while there are no
others, so a cache keyed by it stays valid. `Survey` previews a folder before it is added,
with what to add instead (the folders inside one with no clones of its own, at most 100
subfolders and 5,000 entries looked at, or the folder of a clone that holds others), and
`Check` says how each stored location reads now. `Candidates` suggests folders for the
setup's Clones step: every folder directly in home that holds clones, whatever its name,
or the folders one level down inside it that do, and the same in GOPATH's and ghq's
layouts, 20,000 entries at most in all, the common names first. It never reads a hidden
folder, a link in home, macOS's own folders (Desktop, Documents, Downloads, Library, and
the rest, which macOS guards or which hold no clones), or a folder a disk is mounted on.
It reads the mounts from the kernel's table with `MNT_NOWAIT` (`mountPoints`), so it
never asks a share, a cloud drive, or a virtual machine's files, and never makes macOS
ask the user. The Clones step looks in GitHub Desktop's `~/Documents/GitHub` only on the
user's click (`PreviewCloneLocation`), and `Classify` words a folder macOS keeps the app
out of (`EPERM`) with where to allow it. No location is searched recursively.
One `Reader`, which core builds and hands every service that reads the locations, bounds
each read of another location than the clone root, the resolution of its links included,
to `DefaultReadLimit` (5 s) and keeps one of each going per location: a share that hangs
reads as `StateNotAnswering` and is skipped, its hung read joined until it ends, never
started again beside it. The clone root is always read directly.
