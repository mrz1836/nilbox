package worktrees

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/testutil"
)

// callPatches is the words that start the read of commits' changes, as gitCalls counts it.
const callPatches = "log --no-show-signature --no-mailmap --no-walk=unsorted --root -p"

// amendCopy writes content to file in the worktree at dir and amends its last commit with
// it, keeping its author, author time, and subject, as editing a copy does.
func amendCopy(t *testing.T, dir, file, content string) string {
	t.Helper()

	testutil.WriteFile(t, filepath.Join(dir, file), content)
	testutil.RunGit(t, dir, "add", file)
	testutil.RunGit(t, dir, "commit", "-q", "--amend", "--no-edit")

	return headOf(t, dir)
}

// cherryMark is how git's own rev-list --cherry-mark marks the agent's commit against tip.
func cherryMark(t *testing.T, r *copyRig, tip string) string {
	t.Helper()

	return strings.TrimSpace(testutil.RunGit(t, r.repo, copyCherryArgs(tip, headOf(t, r.agent))...))[:1]
}

// editedCase is a commit of the agent's and its copy's edit: file holds original in the
// commit and copy in its copy; with executable, the commit makes file executable and its
// copy doesn't. gitSaysTheSame marks an edit git's patch IDs don't see.
type editedCase struct {
	name           string
	file           string
	original, copy string
	executable     bool
	gitSaysTheSame bool
}

// copyEdited makes tt's commit in the agent's worktree, copies it onto feat/parent, edits
// the copy, and publishes it, returning the branch's commit.
func copyEdited(t *testing.T, r *copyRig, tt editedCase) string {
	t.Helper()

	testutil.WriteFile(t, filepath.Join(r.agent, tt.file), tt.original)
	testutil.RunGit(t, r.agent, "add", tt.file)

	if tt.executable {
		testutil.RunGit(t, r.agent, "update-index", "--chmod=+x", tt.file)
	}

	testutil.RunGit(t, r.agent, "commit", "-q", "-m", "Add "+tt.file, "--date", "@1780000000 +0000")
	copyOnto(t, r.parent, headOf(t, r.agent))

	if tt.executable {
		testutil.RunGit(t, r.parent, "update-index", "--chmod=-x", tt.file)
		testutil.RunGit(t, r.parent, "commit", "-q", "--amend", "--no-edit")
	} else {
		amendCopy(t, r.parent, tt.file, tt.copy)
	}

	tip := headOf(t, r.parent)
	r.publish(t, tip)

	return tip
}

func TestAnEditedCopyIsToldApartWhereGitsPatchIDsSeeNoChange(t *testing.T) {
	t.Parallel()

	for _, tt := range []editedCase{
		{
			name: "an indentation-only edit", file: "f.py", original: "def f():\n    y = 2\n", copy: "def f():\ny = 2\n",
			gitSaysTheSame: true,
		},
		{
			name: "spaces inside a string", file: "s.py", original: "s = \"a b c\"\n", copy: "s = \"abc\"\n",
			gitSaysTheSame: true,
		},
		{name: "a missing last newline", file: "n.txt", original: "x\n", copy: "x", gitSaysTheSame: true},
		{name: "another mode", file: "run.sh", original: "echo hi\n", copy: "echo hi\n", executable: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			r := newCopyRig(t, nil)
			tip := copyEdited(t, r, tt)

			if tt.gitSaysTheSame {
				require.Equal(t, "=", cherryMark(t, r, tip), "git's patch IDs take the edit for no change")
			}

			row := r.list(t)
			assert.Equal(t, Copies{Ref: "origin/" + parentBranch, Count: 1, Edited: 1, PR: 199}, row.Copies)
			assert.False(t, row.OnGitHub)
			assert.Nil(t, row.Item)
		})
	}
}

func TestACopyOntoAMovedBaseIsIdenticalWhereOnlyItsPlaceMoved(t *testing.T) {
	t.Parallel()

	lines := make([]string, 30)
	for i := range lines {
		lines[i] = "line " + string(rune('a'+i%26)) + strings.Repeat("!", i/26)
	}

	text := func(ls []string) string { return strings.Join(ls, "\n") + "\n" }

	tests := []struct {
		name string
		// moveMain changes main's copy of the file the agent changes at line 15: far from it, or
		// at line 18, which a diff of it shows as context.
		moveMain  func([]string) []string
		identical bool
	}{
		{
			name:      "main added lines far above",
			moveMain:  func(ls []string) []string { return append([]string{"new 1", "new 2", "new 3"}, ls...) },
			identical: true,
		},
		{
			name: "main changed a line of its context",
			moveMain: func(ls []string) []string {
				out := append([]string(nil), ls...)
				out[18] = "main's line"

				return out
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			r := newCopyRig(t, nil)

			base := commitAt(t, r.repo, "notes.txt", text(lines), "Add the notes", authoredAt-100)
			pushed(t, r.repo, mainBranch, base)
			testutil.RunGit(t, r.agent, "reset", "-q", "--hard", base)

			changed := append([]string(nil), lines...)
			changed[15] = "the agent's line"
			work := commitAt(t, r.agent, "notes.txt", text(changed), "Change the middle", authoredAt)

			moved := commitAt(t, r.repo, "notes.txt", text(tt.moveMain(lines)), "Move main", authoredAt+50)
			pushed(t, r.repo, mainBranch, moved)

			testutil.RunGit(t, r.parent, "reset", "-q", "--hard", moved)
			tip := copyOnto(t, r.parent, work)
			pushed(t, r.repo, parentBranch, tip)

			row := r.list(t)
			if tt.identical {
				assert.Equal(t, "=", cherryMark(t, r, tip))
				assert.Equal(t, Copies{Ref: "origin/" + parentBranch, Count: 1, Identical: 1}, row.Copies,
					"the same change, its hunk further down")
			} else {
				assert.Equal(t, "+", cherryMark(t, r, tip), "git's patch IDs hold the context lines")
				assert.Equal(t, Copies{Ref: "origin/" + parentBranch, Count: 1, Edited: 1}, row.Copies,
					"a change whose context differs is no identical copy")
			}
		})
	}
}

func TestABinaryCopyIsIdenticalOnlyWithTheSameBlobs(t *testing.T) {
	t.Parallel()

	for name, edit := range map[string]string{"the same bytes": "", "other bytes": "\x00\x01\x09"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			r := newCopyRig(t, nil)
			work := commitAt(t, r.agent, "logo.bin", "\x00\x01\x02", "Add the logo", authoredAt)
			tip := copyOnto(t, r.parent, work)

			if edit != "" {
				tip = amendCopy(t, r.parent, "logo.bin", edit)
			}

			r.publish(t, tip)

			row := r.list(t)
			if edit == "" {
				assert.Equal(t, Copies{Ref: "origin/" + parentBranch, Count: 1, Identical: 1, PR: 199}, row.Copies)
			} else {
				assert.Equal(t, Copies{Ref: "origin/" + parentBranch, Count: 1, Edited: 1, PR: 199}, row.Copies)
			}
		})
	}
}

func TestTheSweepLeavesAWorktreeWhoseCopyWasReindented(t *testing.T) {
	t.Parallel()

	r := newCopyRig(t, nil)
	work := commitAt(t, r.agent, "f.py", "def f():\n    return 2\n", "Add f", authoredAt)
	copyOnto(t, r.parent, work)
	tip := amendCopy(t, r.parent, "f.py", "def f():\nreturn 2\n")
	r.publish(t, tip)

	sweeper := r.e.linked(linkDeps{gh: r.gh, copies: r.log, auto: true})
	sweeper.run = r.run

	sweeper.Sweep(t.Context())
	r.merge(tip)
	r.squashAndDelete(t)

	report := sweeper.Sweep(t.Context())
	assert.Empty(t, report.Cleaned)
	assert.DirExists(t, r.agent)
	assert.Contains(t, branches(t, r.repo), ownAgentBranch)

	row := r.list(t)
	assert.False(t, row.OnGitHub)
	assert.Equal(t, 1, row.Unpushed)
	assert.Empty(t, r.records(t), "an edited copy is never recorded")
}

func TestConfirmedCopiesAreKeptForGood(t *testing.T) {
	t.Parallel()

	r, _, _ := copiedRig(t)

	r.list(t)
	assert.Equal(t, 1, gitCalls(r.run, callPatches)[callPatches], "one read of the changes for both copies")

	r.list(t)
	assert.Equal(t, 1, gitCalls(r.run, callPatches)[callPatches], "a listing in which nothing changed reads none")

	// A fetch elsewhere compares again, but the pairs it confirmed are kept.
	pushed(t, r.repo, "feat/elsewhere", r.main)
	assert.True(t, r.list(t).Copies.full())
	assert.Equal(t, 1, gitCalls(r.run, callPatches)[callPatches])
	assert.Equal(t, 2, r.copies()[callCherry])
}

func TestACopyWhoseChangeCantBeReadIsNoCopy(t *testing.T) {
	t.Parallel()

	for name, answer := range map[string]reply{
		"git refuses":             {stderr: "fatal: missing blob object", err: exitError{code: 128}},
		"git leaves the copy out": {stdout: "\x00" + strings.Repeat("1", 40) + "\n\ndiff --git a/x b/x\n"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			r := newCopyRig(t, func(_ string, sub []string) (reply, bool) { return answer, startsWith(sub, callPatches) })
			oids := r.work(t, 1)
			r.publish(t, copyOnto(t, r.parent, oids...))

			row := r.list(t)
			assert.False(t, row.Copies.full())
			assert.Zero(t, row.Copies.Identical)
			assert.False(t, row.OnGitHub)

			if answer.err != nil {
				assert.Equal(t, "Couldn't compare its commits with the pushed branches. Missing blob object.", row.Copies.Note)
			} else {
				assert.Equal(t, 1, row.Copies.Edited, "a change git didn't print is no identical copy's")
			}
		})
	}
}

func TestCopyPatchesSplitsWhatGitPrints(t *testing.T) {
	t.Parallel()

	stdout := "\x00" + headSHA + "\n\ndiff --git a/f b/f\nindex 1..2 100644\n" +
		"\x00" + otherSHA + "\n\n" +
		"\x00not-a-commit\n\ndiff --git a/g b/g\n"

	patches, err := git{run: &testutil.Runner{Stdout: stdout}, bin: gitBin}.copyPatches(t.Context(), repoDir, []string{headSHA, otherSHA})
	require.NoError(t, err)
	assert.Equal(t, map[string]string{headSHA: "\ndiff --git a/f b/f\nindex 1..2 100644\n", otherSHA: "\n"}, patches,
		"an empty commit's change is empty, and what isn't a commit is left out")

	_, err = git{run: &testutil.Runner{Stdout: stdout, Stderr: "warning: x"}, bin: gitBin}.copyPatches(t.Context(), repoDir,
		[]string{headSHA})
	require.ErrorIs(t, err, errGitWarned)
}

func TestNormalizePatchKeepsAllButWhereAndWhichBlobs(t *testing.T) {
	t.Parallel()

	patch := func(index, hunk, body string) string {
		return "\ndiff --git a/f.txt b/f.txt\nindex " + index + "\n--- a/f.txt\n+++ b/f.txt\n" + hunk + "\n" + body + "\n"
	}

	a := patch(strings.Repeat("1", 40)+".."+strings.Repeat("2", 40)+" 100644", "@@ -10,3 +10,3 @@ func f() {", " x\n-y\n+z\n w")
	b := patch(strings.Repeat("3", 40)+".."+strings.Repeat("4", 40)+" 100644", "@@ -20,3 +20,3 @@ func g() {", " x\n-y\n+z\n w")

	assert.True(t, samePatch(a, b), "the blobs and the place differ, the change doesn't")
	assert.False(t, samePatch(a, patch(strings.Repeat("3", 40)+".."+strings.Repeat("4", 40)+" 100755",
		"@@ -20,3 +20,3 @@", " x\n-y\n+z\n w")), "the mode is the change's")
	assert.False(t, samePatch(a, patch(strings.Repeat("3", 40)+".."+strings.Repeat("4", 40)+" 100644",
		"@@ -20,3 +20,4 @@", " x\n-y\n+z\n+ \n w")), "a line of a space is a line")
	assert.False(t, samePatch(a, patch("1..2", "@@ bad @@", " x")), "a header git never writes")
	assert.False(t, samePatch("", ""), "an empty change is no copy's")
	assert.False(t, samePatch("\nnot a diff\n", "\nnot a diff\n"))

	binary := func(post string) string {
		return "\ndiff --git a/b.bin b/b.bin\nindex " + strings.Repeat("1", 40) + ".." + post + " 100644\n" +
			"Binary files a/b.bin and b/b.bin differ\n"
	}

	assert.True(t, samePatch(binary(strings.Repeat("2", 40)), binary(strings.Repeat("2", 40))))
	assert.False(t, samePatch(binary(strings.Repeat("2", 40)), binary(strings.Repeat("5", 40))),
		"a binary change is the same only with the same blobs")
}
