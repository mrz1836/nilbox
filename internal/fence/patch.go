package fence

import (
	"slices"
	"strings"
)

// An agent that edits through patches (Codex's apply_patch) hands the fence the patch's
// text, not a file's path: every file the patch adds, updates, deletes, or moves to is a
// write, decided as a file tool's is.

// The patch tool's names, as an agent's hook names it or a command line runs it.
const (
	toolApplyPatch = "apply_patch"
	toolApplyAlias = "applypatch"
)

// patchHeaders are the lines of a patch that name a file it writes, each followed by the
// file's path.
func patchHeaders() []string {
	return []string{"*** Add File: ", "*** Update File: ", "*** Delete File: ", "*** Move to: "}
}

// PatchFiles returns the files a patch writes, in order, each once: those it adds,
// updates, deletes, and moves to. A patch that isn't one, or names no file, writes none
// the fence can tell.
func PatchFiles(patch string) []string {
	var out []string

	for line := range strings.SplitSeq(patch, "\n") {
		line = strings.TrimRight(line, "\r")

		for _, header := range patchHeaders() {
			path, ok := strings.CutPrefix(line, header)
			if path = strings.TrimSpace(path); ok && path != "" && !slices.Contains(out, path) {
				out = append(out, path)
			}
		}
	}

	return out
}

// decidePatch decides a patch: each file it writes, as a write; a patch whose files the
// fence can't tell doesn't run.
func (p Policy) decidePatch(patch, dir string, fs FS) Verdict {
	files := PatchFiles(patch)
	if len(files) == 0 {
		return deny("the fence can't tell which files this patch writes, so it doesn't run")
	}

	for _, file := range files {
		if v := p.decideWrite(file, dir, fs); v.Deny {
			return v
		}
	}

	return Verdict{}
}
