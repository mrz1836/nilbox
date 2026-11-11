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

// The fields before the path on each kind of `git status --porcelain=v2` line: an
// ordinary change ("1 XY sub mH mI mW hH hI path"), a rename or copy ("2 ... hI Xscore
// path", its old path in the next NUL-ended field), and an unmerged file ("u XY sub m1 m2
// m3 mW h1 h2 h3 path").
const (
	ordinaryFields = 7
	renamedFields  = 8
	unmergedFields = 9
)

// parseEntries reads `git status --porcelain=v2 -z`: one NUL-ended field per file, a
// rename's or copy's old path in the field after it. Lines of a shape it doesn't know, such
// as the headers --branch would add, are skipped.
func parseEntries(out []byte) []Entry {
	fields := strings.Split(string(out), "\x00")
	entries := make([]Entry, 0, len(fields))

	for i := 0; i < len(fields); i++ {
		e, renamed, ok := parseEntry(fields[i])

		if renamed {
			// The old path is the next field; a rename without it is cut short.
			if i++; i >= len(fields) {
				break
			}

			e.From = fields[i]
		}

		if ok {
			entries = append(entries, e)
		}
	}

	return entries
}

// parseEntry reads one field of `git status --porcelain=v2 -z`, and reports whether it is
// a rename or a copy, whose old path follows, and whether it is an entry at all.
func parseEntry(field string) (e Entry, renamed, ok bool) {
	kind, rest, _ := strings.Cut(field, " ")

	switch kind {
	case "1":
		e, ok = changedEntry(rest, ordinaryFields)
	case "2":
		e, ok = changedEntry(rest, renamedFields)

		return e, ok, ok
	case "u":
		e, ok = unmergedEntry(rest)
	case "?":
		e, ok = Entry{Code: "??", Path: rest}, rest != ""
	}

	return e, false, ok
}

// changedEntry reads an ordinary or renamed entry's fields after its kind: n fields, then
// the path, which may hold spaces. The staged object is the seventh field.
func changedEntry(rest string, n int) (Entry, bool) {
	parts := strings.SplitN(rest, " ", n+1)
	if len(parts) != n+1 || len(parts[0]) != 2 || parts[n] == "" {
		return Entry{}, false
	}

	return Entry{Code: parts[0], Index: parts[6], Path: parts[n]}, true
}

// unmergedEntry reads an unmerged entry's fields after its kind: its three stages' objects
// are the seventh to ninth fields.
func unmergedEntry(rest string) (Entry, bool) {
	parts := strings.SplitN(rest, " ", unmergedFields+1)
	if len(parts) != unmergedFields+1 || len(parts[0]) != 2 || parts[unmergedFields] == "" {
		return Entry{}, false
	}

	return Entry{Code: parts[0], Index: strings.Join(parts[6:9], ":"), Path: parts[unmergedFields]}, true
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
