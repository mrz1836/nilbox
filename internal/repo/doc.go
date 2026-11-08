// Package repo names GitHub repositories: an owner and a repository name, checked
// against GitHub's own rules, compared ignoring case, and written as "owner/name".
//
// Every package that reads, stores, or compares a repository's name goes through Name,
// so one rule set decides what a valid name is, and one key decides when two names are
// the same repository. The rules accept every name GitHub has issued and refuse anything
// that could escape a directory or read as a command-line flag. GitHub treats names case-insensitively, so maps of repositories
// are keyed by Key, never by the name as written.
package repo
