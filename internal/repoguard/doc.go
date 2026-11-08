// Package repoguard holds repository-wide guard tests. They scan the source tree for
// patterns this project forbids: a hard-coded product name outside internal/brand,
// personal home-directory paths, a data layer that needs CGO, tests that would call the
// live GitHub API, any command that deletes a git branch, a clone created anywhere but the
// launcher's confirmed clone of a missing repository, any git command that changes an
// existing clone, the test-support package (internal/testutil) imported anywhere but a
// _test.go file, and production code that logs through slog's default logger or the
// standard log package instead of the logger it was given.
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
// One guard is personal: a contributor can list their own private names (employers,
// clients, private organizations and repositories) in the clone's git directory, at
// .git/info/private-terms, and the guard fails wherever one appears in a file, the branch
// name, or an unpushed commit message. The list never enters the repository, so the guard
// skips wherever it is absent, CI included.
//
// The package has no production code; everything lives in its tests, which run with the
// rest of the suite on every platform.
package repoguard
