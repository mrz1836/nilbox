// Package item names the pull requests and issues the app works on: their kind, a
// reference to one item, and the name of the worktree a session on an item runs in.
//
// The inbox, the launcher, the prompt library, the launch records, and the worktree
// sweep all share these, so a kind stored by one is read the same way by the others, and
// a worktree made for an item is always found again under the same name.
package item
