package worktrees

import (
	"context"
	"regexp"
	"slices"
	"strings"

	"github.com/mrz1836/nilbox/internal/gitread"
)

// Confirming an identical copy byte for byte.
//
// git's patch IDs, which rev-list --cherry-mark compares, leave out every space inside a
// changed line, a hunk's position, and the mark of a file's missing last newline, so a
// commit whose copy was only re-indented, or had a space dropped from a string, is marked =
// as if its change were identical. In Python, YAML, a Makefile, or a string that is a change
// all the same. So a = is only where looking starts: the commit's patch and its likely
// copy's are read (copyPatchesArgs) and compared whole, all but what may differ between two
// identical changes made on different commits, the blob IDs of a text file (the index line)
// and where each hunk starts (the line numbers of its @@ header, and the text git adds after
// it). A binary file's change is identical only when its blobs before and after are, as its
// full index line says. A patch that is empty, can't be told apart into files, or that git
// left out is no copy's. What a pair of commits compared as is kept for good, as commits
// never change.

// copyPatchesCmd starts the log of the changes the commits named after it make, each after
// a line of a NUL and its commit ID, a root commit's included, as git's own diff writes
// them: no diff program, no text conversion, no color, no rename, three lines of context,
// and every blob by its full ID.
func copyPatchesCmd() []string {
	return []string{
		"log", noSignatures, noMailmap, "--no-walk=unsorted", "--root", "-p", "--no-ext-diff", "--no-textconv",
		"--no-color", "--no-renames", "--full-index", "--unified=3", "--format=%x00%H",
	}
}

// copyPatchesArgs reads the changes oids make, in one run.
func copyPatchesArgs(oids []string) []string { return append(copyPatchesCmd(), oids...) }

// allowedCopyPatches reports a log of the changes of one or more full commit IDs and
// nothing else.
func allowedCopyPatches(args []string) bool {
	head := copyPatchesCmd()

	return len(args) > len(head) && joinArgs(args[:len(head)]) == joinArgs(head) && allCommitIDs(args[len(head):])
}

// copyPatches reads the change each of oids makes in the clone at dir, by commit ID, as
// git prints it (see copyPatchesCmd).
func (g git) copyPatches(ctx context.Context, dir string, oids []string) (map[string]string, error) {
	stdout, err := g.copyRead(ctx, dir, copyPatchesArgs(oids))
	if err != nil {
		return nil, err
	}

	patches := map[string]string{}

	for block := range strings.SplitSeq(string(stdout), "\x00") {
		oid, patch, _ := strings.Cut(block, "\n")
		if gitread.IsCommitID(oid) {
			patches[oid] = patch
		}
	}

	return patches, nil
}

// hunkHeader is a hunk's header in a text file's change: where it starts and how many lines
// it spans in each, then any text git adds.
var hunkHeader = regexp.MustCompile(`^@@ -\d+(,\d+)? \+\d+(,\d+)? @@`)

// samePatch reports two changes, as copyPatches reads them, that are the same byte for byte
// but for what may differ between identical changes made on different commits (see this
// file's doc). An empty or unreadable change is the same as none.
func samePatch(a, b string) bool {
	na, okA := normalizePatch(a)
	nb, okB := normalizePatch(b)

	return okA && okB && na == nb
}

// normalizePatch is a change with the blob IDs of each text file and the positions of its
// hunks left out, or false when it is empty or doesn't read as git's per-file diffs.
func normalizePatch(patch string) (string, bool) {
	patch = strings.Trim(patch, "\n")
	if !strings.HasPrefix(patch, "diff --git ") {
		return "", false
	}

	var out []string

	for file := range strings.SplitSeq("\n"+patch, "\ndiff --git ") {
		if file == "" {
			continue
		}

		lines, ok := normalizeFile(strings.Split("diff --git "+file, "\n"))
		if !ok {
			return "", false
		}

		out = append(out, lines...)
	}

	return strings.Join(out, "\n"), true
}

// normalizeFile normalizes one file's diff, its lines from its diff --git line on: a text
// file's index line keeps only its mode, and its hunk headers only their lengths. A binary
// file's lines stay whole, so its full blob IDs must match.
func normalizeFile(lines []string) ([]string, bool) {
	binary := slices.ContainsFunc(lines, func(l string) bool {
		return l == "GIT binary patch" || (strings.HasPrefix(l, "Binary files ") && strings.HasSuffix(l, " differ"))
	})
	if binary {
		return lines, true
	}

	out := make([]string, 0, len(lines))

	for _, l := range lines {
		switch {
		case strings.HasPrefix(l, "index "):
			_, mode, _ := strings.Cut(strings.TrimPrefix(l, "index "), " ")
			l = strings.TrimSpace("index " + mode)
		case strings.HasPrefix(l, "@@"):
			m := hunkHeader.FindStringSubmatch(l)
			if m == nil {
				return nil, false
			}

			l = "@@ -" + m[1] + " +" + m[2] + " @@"
		}

		out = append(out, l)
	}

	return out, true
}

// confirmCopies confirms byte for byte the copies cherry-mark marked identical, in the
// clone at dir: for each commit of marked, by its commit ID, its likely copies there, most
// likely first. It returns, for each commit with a copy whose change is the same (samePatch),
// that copy; a commit with none has an edited copy. Only the changes of commits whose pairs
// aren't kept in cache are read, all in one log. It fails when that log does, which counts
// as nothing confirmed.
func confirmCopies(ctx context.Context, g git, cache *copyCache, dir string, marked map[string][]string,
) (map[string]string, error) {
	same, unread := knownPairs(cache, marked)

	if len(unread) > 0 {
		slices.Sort(unread)

		patches, err := g.copyPatches(ctx, dir, slices.Compact(unread))
		if err != nil {
			return nil, err
		}

		readPairs(cache, patches, marked, same)
	}

	confirmed := map[string]string{}

	for oid, copies := range marked {
		if i := slices.IndexFunc(copies, func(c string) bool { return same[copyPair{oid, c}] }); i >= 0 {
			confirmed[oid] = copies[i]
		}
	}

	return confirmed, nil
}

// knownPairs returns the verdicts cache keeps on marked's pairs, and the commits of the
// others, whose changes must be read.
func knownPairs(cache *copyCache, marked map[string][]string) (map[copyPair]bool, []string) {
	same := map[copyPair]bool{}

	var unread []string

	for oid, copies := range marked {
		for _, c := range copies {
			if verdict, known := cache.samePair(copyPair{oid, c}); known {
				same[copyPair{oid, c}] = verdict
			} else {
				unread = append(unread, oid, c)
			}
		}
	}

	return same, unread
}

// copyPair is a commit and a likely copy of it.
type copyPair struct {
	commit, copy string
}

// readPairs compares the changes of each pair of marked not in same yet, from patches, and
// keeps the verdict in same and in cache. A pair one of whose changes git left out has no
// verdict, which counts as no copy and is read again next time.
func readPairs(cache *copyCache, patches map[string]string, marked map[string][]string, same map[copyPair]bool) {
	for oid, copies := range marked {
		for _, c := range copies {
			pair := copyPair{oid, c}
			if _, known := same[pair]; known {
				continue
			}

			a, okA := patches[oid]
			b, okB := patches[c]

			if okA && okB {
				same[pair] = samePatch(a, b)
				cache.keepPair(pair, same[pair])
			}
		}
	}
}
