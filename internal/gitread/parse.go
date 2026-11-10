package gitread

import (
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// worktree is one of a clone's worktrees: its folder, and the branch it has checked out,
// or "" when its HEAD is detached or it is the bare repository.
type worktree struct {
	path   string
	branch string
}

// parseStatus reads `git status --porcelain -z`: entries of "XY path", each ending in a
// NUL. A rename or a copy is followed by the path it came from, which is skipped.
func parseStatus(out []byte) []Change {
	fields := strings.Split(string(out), "\x00")
	changes := make([]Change, 0, len(fields))

	for i := 0; i < len(fields); i++ {
		field := fields[i]
		if len(field) < len("XY p") || field[2] != ' ' {
			continue
		}

		code := field[:2]
		changes = append(changes, Change{Code: code, Path: field[3:]})

		if strings.ContainsAny(code, "RC") {
			i++
		}
	}

	return changes
}

// parseWorktrees reads `git worktree list --porcelain -z`: attributes ending in a NUL, a
// "worktree <path>" starting each worktree and an empty one ending it. Attributes it
// doesn't need are skipped, so a newer git's still read.
func parseWorktrees(out []byte) []worktree {
	var list []worktree

	for attr := range strings.SplitSeq(string(out), "\x00") {
		key, val, _ := strings.Cut(attr, " ")

		switch {
		case key == "worktree" && val != "":
			list = append(list, worktree{path: val})
		case key == "branch" && len(list) > 0:
			list[len(list)-1].branch = strings.TrimPrefix(val, headsPrefix)
		}
	}

	return list
}

// parseRemoteRefs reads ls-remote's "<object ID>\t<ref>" lines into origin's branches and
// tags. An annotated tag's "<tag>^{}" line gives the commit it names; a lightweight tag
// names its own object. Lines of any other shape are skipped.
func parseRemoteRefs(out []byte) RemoteRefs {
	refs := RemoteRefs{Heads: map[string]string{}, Tags: map[string]RemoteTag{}}
	peeled := map[string]string{}

	for line := range strings.Lines(string(out)) {
		oid, ref, ok := strings.Cut(strings.TrimSuffix(line, "\n"), "\t")
		if ok && objectIDPattern.MatchString(oid) {
			addRemoteRef(refs, peeled, oid, ref)
		}
	}

	for tag, commit := range peeled {
		if t, ok := refs.Tags[tag]; ok {
			t.Commit = commit
			refs.Tags[tag] = t
		}
	}

	return refs
}

// addRemoteRef records that ref points at oid: a branch in refs, a tag in refs with oid as
// its commit until peeled says otherwise, or a tag's peeled commit in peeled. Any other
// ref is skipped.
func addRemoteRef(refs RemoteRefs, peeled map[string]string, oid, ref string) {
	if name, isHead := strings.CutPrefix(ref, headsPrefix); isHead {
		refs.Heads[name] = oid

		return
	}

	name, isTag := strings.CutPrefix(ref, tagsPrefix)
	tag, isPeeled := strings.CutSuffix(name, peelSuffix)

	switch {
	case !isTag:
	case isPeeled:
		peeled[tag] = oid
	default:
		refs.Tags[name] = RemoteTag{Object: oid, Commit: oid}
	}
}

// versionTagNames returns the names in tags that start with v, sorted. ls-remote matches
// a pattern against a ref's last parts, so "v*" also lists a tag such as release/v1.
func versionTagNames(tags map[string]RemoteTag) []string {
	names := make([]string, 0, len(tags))

	for _, name := range slices.Sorted(maps.Keys(tags)) {
		if strings.HasPrefix(name, versionTagPrefix) {
			names = append(names, name)
		}
	}

	return names
}

// firstLine returns out up to its first newline, spaces kept: a path may hold them.
func firstLine(out []byte) string {
	line, _, _ := strings.Cut(string(out), "\n")

	return line
}

// absolute returns path, which git printed relative to dir when it isn't absolute, as an
// absolute, clean path.
func absolute(dir, path string) string {
	if filepath.IsAbs(path) {
		return filepath.Clean(path)
	}

	return filepath.Join(dir, path)
}

// sameFolder reports whether path is the folder here describes, through any symbolic
// link. A nil here, or a path that can't be read, is never the same.
func sameFolder(here os.FileInfo, path string) bool {
	if here == nil {
		return false
	}

	info, err := os.Stat(path)

	return err == nil && os.SameFile(here, info)
}
