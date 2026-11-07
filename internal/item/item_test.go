package item_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/repo"
)

func TestParseKind(t *testing.T) {
	t.Parallel()

	for _, k := range item.Kinds() {
		got, err := item.ParseKind(string(k))
		require.NoError(t, err)
		assert.Equal(t, k, got)
		assert.True(t, k.Valid())
	}

	for _, s := range []string{"", "PR", "discussion", "../x"} {
		got, err := item.ParseKind(s)
		require.ErrorIs(t, err, item.ErrInvalidKind, s)
		assert.Empty(t, got)
		assert.False(t, item.Kind(s).Valid(), s)
	}

	_, err := item.ParseKind("discussion")
	assert.EqualError(t, err, `kind must be "pr" or "issue": "discussion"`)
}

func TestKindWords(t *testing.T) {
	t.Parallel()

	assert.Equal(t, []item.Kind{item.KindPR, item.KindIssue}, item.Kinds())
	assert.Equal(t, "pr", string(item.KindPR))
	assert.Equal(t, "issue", string(item.KindIssue))
	assert.Equal(t, "pull request", item.KindPR.Noun())
	assert.Equal(t, "issue", item.KindIssue.Noun())
	assert.Equal(t, "pull requests", item.KindPR.Plural())
	assert.Equal(t, "issues", item.KindIssue.Plural())
}

func TestRef(t *testing.T) {
	t.Parallel()

	ref := item.Ref{Repo: repo.Name{Owner: "Acme-Org", Repo: "Example-Repo"}, Number: 184, Kind: item.KindPR}

	assert.Equal(t, "Acme-Org/Example-Repo#184", ref.String())
	assert.Equal(t, "acme-org/example-repo#184", ref.Key())

	other := item.Ref{Repo: repo.Name{Owner: "acme-org", Repo: "example-repo"}, Number: 184}
	assert.Equal(t, ref.Key(), other.Key(), "the key ignores case and the kind")
}

func TestWorktreeName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		kind   item.Kind
		number int
		want   string
	}{
		{kind: item.KindPR, number: 7, want: "pr-7"},
		{kind: item.KindIssue, number: 12, want: "issue-12"},
		{kind: item.KindPR, number: 1234567890, want: "pr-1234567890"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			t.Parallel()

			name := item.WorktreeName(tt.kind, tt.number)
			assert.Equal(t, tt.want, name)

			kind, number, ok := item.ParseWorktreeName(name)
			require.True(t, ok)
			assert.Equal(t, tt.kind, kind)
			assert.Equal(t, tt.number, number)
		})
	}
}

func TestParseWorktreeNameRefusesOtherNames(t *testing.T) {
	t.Parallel()

	for _, name := range []string{
		"", "pr", "pr-", "pr-0", "pr-07", "pr-x", "pr-7a", "pr--7", "issue-12345678901", "PR-7", "discussion-3",
		"worktree-pr-7", "pr-7-copy", "#7",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			kind, number, ok := item.ParseWorktreeName(name)
			assert.False(t, ok)
			assert.Empty(t, kind)
			assert.Zero(t, number)
		})
	}
}
