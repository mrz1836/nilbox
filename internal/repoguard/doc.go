// Package repoguard holds repository-wide guard tests. They scan the source tree for
// patterns this project forbids: a hard-coded product name outside internal/brand,
// personal home-directory paths, schema creation from models instead of migrations, a
// data layer that needs CGO, tests that would call the live GitHub API, any command that
// deletes a git branch, a clone created anywhere but the launcher's confirmed clone of a
// missing repository, and any git command that changes an existing clone.
//
// The package has no production code; everything lives in its tests, which run with the
// rest of the suite on every platform.
package repoguard
