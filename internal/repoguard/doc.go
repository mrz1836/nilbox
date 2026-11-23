// Package repoguard holds repository-wide guard tests. They scan the source tree for
// patterns this project forbids: a hard-coded product name outside internal/brand,
// personal home-directory paths, a data layer that needs CGO, tests that would call the
// live GitHub API, any command that deletes a git branch, a clone created anywhere but the
// launcher's confirmed clone of a missing repository, any git command that changes an
// existing clone, a test-support package (internal/testutil, the runner harness
// internal/scriptrun/scriptruntest, Autopilot's simulator, or internal/github's fakes in
// internal/github/githubtest) imported anywhere but a _test.go file or another
// test-support package, production code that logs through slog's default logger or the
// standard log package instead of the logger it was given, frontend text sized in px,
// which the text size setting cannot scale, a frontend color, text size, dialog backdrop,
// or disabled fade written out rather than taken from the theme's tokens (a Tailwind
// palette utility such as text-white among them, which the theme resets to nothing), and
// a shell, Python, or other script file outside the directories go-broadcast syncs, where
// a magex target belongs instead.
//
// The data-layer guards keep SQL with the tables' owners, with no exceptions: no ORM
// import anywhere, no SQLite driver import outside internal/store, no SQL statement or
// query call outside internal/store and the owners' generated query packages
// (internal/<owner>/internal/queries), no schema change anywhere but a migration, no query
// file that touches another owner's table, no table without exactly one owner, and no
// generated query file from a sqlc version other than the pinned one.
//
// The boundary guard keeps package dependencies cut, as go list reports them for the
// macOS and the Linux build: settings, the launcher, and the worktree service never
// depend on the inbox or the GitHub client, and the launcher and the worktree service
// read their settings through their own narrow interfaces rather than the settings
// package.
//
// The guide guards keep the project guide, CLAUDE.md, under a size cap, since coding
// agents load it whole into every session, so a package's design goes in its file under
// docs/architecture/. The docs guards check every link of the docs (the guide, the README,
// and everything under docs/): each names a file or folder of the repository, and each
// anchor into a doc one of its headings. They keep docs/README.md the index of every doc,
// and a folder's README.md the index of its topics; open every doc with its title and a nav
// line back to the index; and cap each doc at 48 KiB, so a topic that outgrows one file
// becomes a folder.
//
// One guard is personal: a contributor can list their own private names (employers,
// clients, private organizations and repositories) in the clone's git directory, at
// .git/info/private-terms, and the guard fails wherever one appears in a file, the branch
// name, or an unpushed commit message. The list never enters the repository, so the guard
// skips wherever it is absent, CI included.
//
// The package has no production code; everything lives in its tests, which run with the
// rest of the suite on every platform.
package repoguard
