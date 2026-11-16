package fence

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// gitPush is the command most of the reader's cases hide.
const gitPush = "git push"

// programs returns every simple command of a line as its words joined, for a test to
// compare.
func programs(t *testing.T, line string) []string {
	t.Helper()

	s, err := readShell(line)
	require.NoError(t, err, line)

	out := make([]string, 0, len(s.commands))
	for _, c := range s.commands {
		out = append(out, strings.Join(texts(c.words), " "))
	}

	return out
}

func TestReadShellFindsEveryCommand(t *testing.T) {
	t.Parallel()

	tests := []struct {
		line string
		want []string
	}{
		{line: "go test ./...", want: []string{"go test ./..."}},
		{line: `git commit -m "fix: the push handler"`, want: []string{"git commit -m fix: the push handler"}},
		{line: "cd x && git push --force", want: []string{"cd x", "git push --force"}},
		{line: "a; b || c | d & e", want: []string{"a", "b", "c", "d", "e"}},
		{line: "echo $(git push -f origin x)", want: []string{"echo $(git push -f origin x)", "git push -f origin x"}},
		{line: "echo `git push`", want: []string{"echo `git push`", gitPush}},
		{line: "diff <(git show a) <(git show b)", want: []string{"git show a", "git show b", "diff"}},
		{line: "(cd x; git push)", want: []string{"cd x", gitPush}},
		{line: "f() { git push --force; }; f", want: []string{"f", "git push --force", "f"}},
		{line: "if true; then git push; fi", want: []string{"true", gitPush}},
		{line: "for b in a c; do git push origin $b; done", want: []string{"git push origin $b"}},
		{line: "'g''it' pu\\sh", want: []string{gitPush}},
		{line: "$'\\x67it' push", want: []string{gitPush}},
		{line: "$'\\u0067\\147it' push", want: []string{"ggit push"}},
		{line: "$'\\UFFFFFFFFgit' push", want: []string{gitPush}},
		{line: "git commit -F - <<'EOF'\nfix the push\nEOF\ngo test", want: []string{"git commit -F -", "go test"}},
		{line: "cat <<EOF\n$(git push)\nEOF", want: []string{"cat", gitPush}},
		{line: "ls # git push", want: []string{"ls"}},
		{line: "echo a \\\n  b", want: []string{"echo a b"}},
		{line: "time git push", want: []string{gitPush}},
		{line: "echo $((1 + $(git push)))", want: []string{gitPush, "echo $((1 + $(git push)))"}},
	}

	for _, tt := range tests {
		t.Run(tt.line, func(t *testing.T) {
			t.Parallel()

			assert.ElementsMatch(t, tt.want, programs(t, tt.line))
		})
	}
}

func TestReadShellMarksWhatTheShellExpands(t *testing.T) {
	t.Parallel()

	tests := []struct {
		line    string
		dynamic []bool
	}{
		{line: "git push", dynamic: []bool{false, false}},
		{line: "$G push", dynamic: []bool{true, false}},
		{line: "git pu?h", dynamic: []bool{false, true}},
		{line: "git {push,status}", dynamic: []bool{false, true}},
		{line: "find . -exec rm {} ;", dynamic: []bool{false, false, false, false, false}},
		{line: "echo '$HOME'", dynamic: []bool{false, false}},
		{line: `echo "$HOME"`, dynamic: []bool{false, true}},
		{line: "ls *.go", dynamic: []bool{false, true}},
	}

	for _, tt := range tests {
		t.Run(tt.line, func(t *testing.T) {
			t.Parallel()

			s, err := readShell(tt.line)
			require.NoError(t, err)
			require.Len(t, s.commands, 1)

			got := []bool{}
			for _, w := range s.commands[0].words {
				got = append(got, w.dynamic)
			}

			assert.Equal(t, tt.dynamic, got)
		})
	}
}

func TestReadShellReadsAssignmentsAndRedirections(t *testing.T) {
	t.Parallel()

	s, err := readShell("GIT_DIR=/x A=1 git status > out.txt 2>&1 < in")
	require.NoError(t, err)
	require.Len(t, s.commands, 1)

	c := s.commands[0]
	assert.Equal(t, []string{"GIT_DIR=/x", "A=1"}, texts(c.assigns))
	assert.Equal(t, []string{"git", "status"}, texts(c.words))
	require.Len(t, c.redirects, 3)
	assert.True(t, c.redirects[0].writes())
	assert.Equal(t, "out.txt", c.redirects[0].target.text)
	assert.False(t, c.redirects[2].writes())
	assert.True(t, s.compound)

	plain, err := readShell("go test ./...")
	require.NoError(t, err)
	assert.False(t, plain.compound, "one command alone is plain")

	notAssign, err := readShell("'A=1' git")
	require.NoError(t, err)
	assert.Empty(t, notAssign.commands[0].assigns, "a quoted name sets nothing")
}

func TestReadShellRefusesWhatItCantRead(t *testing.T) {
	t.Parallel()

	for _, line := range []string{
		"echo 'open", `echo "open`, "echo $(open", "echo `open", "echo >", "echo \xff",
		strings.Repeat("$(", maxShellDepth+2) + "x" + strings.Repeat(")", maxShellDepth+2),
	} {
		_, err := readShell(line)
		assert.ErrorIs(t, err, errShell, "%q", line)
	}
}

func FuzzReadShell(f *testing.F) {
	for _, seed := range []string{
		"git push origin HEAD:refs/heads/x", "a && b | c", "echo $(x `y` $((1+2)))", "cat <<'E'\nx\nE",
		"f() { a; }", "$'\\x41'", "{a,b}", "x=1 y=$(z) cmd > f 2>&1",
	} {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, line string) {
		s, err := readShell(line)
		if err != nil {
			return
		}

		for _, c := range s.commands {
			for _, w := range c.words {
				assert.NotContains(t, w.text, "\x00\x00\x00", "a word is never more than its line")
			}
		}
	})
}
