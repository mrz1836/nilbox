package pushwatch

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/mrz1836/nilbox/internal/gitread"
	"github.com/mrz1836/nilbox/internal/paths"
)

// refLimit is the most of a ref file that is read: a SHA-256 commit ID and its newline,
// with room to spare.
const refLimit = 128

// fileSig is what a stat says of a file, enough to tell that it changed. Git never edits
// a ref file in place: it writes a lock file and renames it over the ref, so a ref that
// moved is a new file, with another inode and a later time.
type fileSig struct {
	exists  bool
	inode   uint64
	size    int64
	modTime int64
}

// statFile returns what Lstat says of the file at path. Anything but a regular file, a
// link included, reads as missing.
func statFile(path string) fileSig {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() {
		return fileSig{}
	}

	return fileSig{exists: true, inode: inodeOf(info), size: info.Size(), modTime: info.ModTime().UnixNano()}
}

// trackingRef is origin's branch as git names it.
func trackingRef(branch string) string {
	return "refs/remotes/origin/" + branch
}

// trackingPath returns the loose ref file of origin's branch in the common git folder
// common. branch is a valid branch name (gitread.ValidBranch), so the path stays inside
// the folder.
func trackingPath(common, branch string) string {
	return filepath.Join(common, "refs", "remotes", "origin", filepath.FromSlash(branch))
}

// tablesPath returns the table list of a common git folder that keeps its refs in
// reftable, which git replaces whole whenever any ref moves.
func tablesPath(common string) string {
	return filepath.Join(common, "reftable", "tables.list")
}

// configPath returns the config file of a common git folder.
func configPath(common string) string {
	return filepath.Join(common, "config")
}

// readLoose returns the commit the loose ref file at path holds, or "" when it holds
// none: gone (packed away, or the branch deleted), a symbolic ref, or not a ref at all.
// Only a regular file is read (paths.ReadHead), so a named pipe never hangs the read.
func readLoose(path string) string {
	data, err := paths.ReadHead(path, refLimit)
	if err != nil {
		return ""
	}

	return commitOf(data)
}

// readTable returns the commit origin's branch is at in the reftable clone at dir, read
// with one allowlisted rev-parse, or "" when git can't say: no Git, no such branch, or a
// failure.
func readTable(ctx context.Context, git Git, dir, branch string) string {
	if git == nil {
		return ""
	}

	out, _, err := git.Run(ctx, dir, "rev-parse", "--verify", "--quiet", trackingRef(branch))
	if err != nil {
		return ""
	}

	return commitOf(out)
}

// commitOf returns the full commit ID out holds alone, or "".
func commitOf(out []byte) string {
	oid := strings.TrimSpace(string(out))
	if !gitread.IsCommitID(oid) {
		return ""
	}

	return oid
}
