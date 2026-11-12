// Package paths turns the paths a user types into absolute ones and back: a leading ~
// expands to the home directory, and a path under home is shown as ~/.... It also opens
// the files a repository or an agent controls (OpenRegular, ReadHead, ReadTail), reading
// only a regular file, or a link to one, so a named pipe put there never hangs a read.
package paths
