// Package repoguard holds repository-wide guard tests. They scan the source tree for
// patterns this project forbids: a hard-coded product name outside internal/brand,
// personal home-directory paths, schema creation from models instead of migrations, a
// data layer that needs CGO, tests that would call the live GitHub API, any command that
// deletes a git branch, a clone created anywhere but the launcher's confirmed clone of a
// missing repository, any git command that changes an existing clone, and the
// test-support package (internal/testutil) imported anywhere but a _test.go file.
//
// The data-layer guards keep SQL with the tables' owners: no ORM import, no SQL statement
// or query call outside internal/store and the owners' generated query packages
// (internal/<owner>/internal/queries), no query file that touches another owner's table,
// no table without exactly one owner, and no generated query file from a sqlc version
// other than the pinned one. The first two allow the packages not yet moved to sqlc, from
// lists that fail once an entry has nothing left to allow.
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
