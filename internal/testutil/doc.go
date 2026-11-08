// Package testutil holds the helpers and fakes the test suites share: a fresh database,
// an isolated git, temporary files and agent homes, fakes for the small interfaces
// services take (a program runner, a program finder, a clock, and an event sink), and an
// in-memory log.
//
// It is test support only. Only _test.go files may import it, which a repoguard test
// enforces, so none of it reaches the app. It imports the store to open databases, so
// the store's own internal tests, and those of the packages the store imports, cannot
// use it.
package testutil
