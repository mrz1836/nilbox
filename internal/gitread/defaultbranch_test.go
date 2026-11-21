package gitread

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
)

var errFakeHead = errors.New("fake: git failed")

// masterBranch is the other name a default branch most often has.
const masterBranch = "master"

// Git reads origin's HEAD for DefaultBranch, as Repo scripts and Deps hand it.
var _ OriginHeadReader = (*Git)(nil)

// fakeHeads is origin's HEAD as a clone last learned it: head, or err, counting each read.
type fakeHeads struct {
	head  string
	err   error
	reads int
}

func (f *fakeHeads) OriginHead(context.Context, string) (string, error) {
	f.reads++

	return f.head, f.err
}

// guessing returns a guess that accepts the branches in yes, and the branches it was
// asked about, in order.
func guessing(yes ...string) (func(branch string) bool, *[]string) {
	asked := []string{}

	return func(branch string) bool {
		asked = append(asked, branch)

		return slices.Contains(yes, branch)
	}, &asked
}

func TestDefaultBranchTakesTheStoredOneFirst(t *testing.T) {
	t.Parallel()

	heads := &fakeHeads{head: "trunk"}
	guess, asked := guessing(mainBranch)

	assert.Equal(t, "develop", DefaultBranch(t.Context(), heads, repoDir, "develop", guess))
	assert.Zero(t, heads.reads, "git isn't read when the app stored one")
	assert.Empty(t, *asked)
}

func TestDefaultBranchThenReadsOriginsHead(t *testing.T) {
	t.Parallel()

	heads := &fakeHeads{head: "trunk"}
	guess, asked := guessing(mainBranch)

	assert.Equal(t, "trunk", DefaultBranch(t.Context(), heads, repoDir, "", guess))
	assert.Equal(t, 1, heads.reads)
	assert.Empty(t, *asked, "no guess while the clone names one")
}

func TestDefaultBranchThenGuessesMainBeforeMaster(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		heads *fakeHeads
		yes   []string
		want  string
		asked []string
	}{
		{name: mainBranch, heads: &fakeHeads{}, yes: []string{mainBranch, masterBranch}, want: mainBranch, asked: []string{mainBranch}},
		{name: masterBranch, heads: &fakeHeads{}, yes: []string{masterBranch}, want: masterBranch, asked: []string{mainBranch, masterBranch}},
		{name: "neither", heads: &fakeHeads{}, want: "", asked: []string{mainBranch, masterBranch}},
		{
			name: "after a read that failed", heads: &fakeHeads{head: "trunk", err: errFakeHead}, yes: []string{masterBranch},
			want: masterBranch, asked: []string{mainBranch, masterBranch},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			guess, asked := guessing(tt.yes...)

			assert.Equal(t, tt.want, DefaultBranch(t.Context(), tt.heads, repoDir, "", guess))
			assert.Equal(t, tt.asked, *asked)
		})
	}
}

func TestDefaultBranchWithoutGitOrAGuess(t *testing.T) {
	t.Parallel()

	guess, _ := guessing(masterBranch)

	assert.Equal(t, masterBranch, DefaultBranch(t.Context(), nil, repoDir, "", guess), "no git reads no HEAD")
	assert.Empty(t, DefaultBranch(t.Context(), &fakeHeads{}, repoDir, "", nil), "no guess guesses nothing")
	assert.Empty(t, DefaultBranch(t.Context(), nil, repoDir, "", nil))
}

// TestDefaultBranchKeepsRepoScriptsRule checks the rule Repo scripts reckons a clone's
// default branch by: the one the app knows, else main or master when the clone is on it.
func TestDefaultBranchKeepsRepoScriptsRule(t *testing.T) {
	t.Parallel()

	onBranch := func(current string) func(string) bool {
		return func(branch string) bool { return branch == current }
	}

	tests := []struct {
		name, stored, head, current, want string
	}{
		{name: "stored", stored: "develop", head: "trunk", current: mainBranch, want: "develop"},
		{name: "origin's head", head: "trunk", current: mainBranch, want: "trunk"},
		{name: "on main", current: mainBranch, want: mainBranch},
		{name: "on master", current: masterBranch, want: masterBranch},
		{name: "on a feature branch", current: "feature/x", want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := DefaultBranch(t.Context(), &fakeHeads{head: tt.head}, repoDir, tt.stored, onBranch(tt.current))
			assert.Equal(t, tt.want, got)
		})
	}
}
