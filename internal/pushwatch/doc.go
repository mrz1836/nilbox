// Package pushwatch notices a push made on this Mac to a pull request the inbox lists, so
// its card catches up at once rather than at the next refresh.
//
// Git runs no hook after a push, but a push that lands leaves a trace in the clone: git
// moves the clone's own copy of the branch on the remote, refs/remotes/origin/<branch>, to
// the commit it pushed, whichever program ran the push (a terminal, an editor, an agent).
// A Watcher, a desktop worker, follows that ref for each open pull request whose head
// branch is in its own repository (Targets), in every clone whose origin is that
// repository, and when it moves to a commit GitHub hasn't reported for the pull request,
// hands the item to the inbox to read again (Rereader): one GraphQL point. A fetch that
// brings someone else's push moves the ref the same way, and the card catches up the same
// way. GitHub takes in a push's pull request a moment after the push, so the read waits
// Settle, and is made again, at most twice more, while GitHub still reports the old head.
//
// It costs little and changes nothing. Each Tick it stats the ref file of each pull
// request watched, and reads one only once it changed: no process starts, and nothing is
// asked of GitHub until a branch moves. A clone's origin is read with git once, and again
// only when the clone's config file changes; the clones are listed again every IndexEvery.
// A clone that keeps its refs in reftable, which has no file per ref, is read with one
// allowlisted rev-parse (Git, gitread's) once its table list changes. Nothing is written
// to a clone or installed in it: no hook, no config.
//
// It can't see a push made from another machine or on GitHub (the refresh brings those),
// a push to a URL rather than to origin, which moves no ref, a fork's branch, or a gh
// command that changes a pull request without git; a session the app launched is
// followed for those by the sessions' Watcher.
package pushwatch
