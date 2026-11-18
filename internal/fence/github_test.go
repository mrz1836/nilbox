package fence_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/mrz1836/nilbox/internal/fence"
)

func TestChangesGitHub(t *testing.T) {
	t.Parallel()

	tests := []struct {
		line string
		want bool
	}{
		// What changes a pull request or an issue.
		{line: "gh pr review 12 --approve", want: true},
		{line: "gh pr review 12 --request-changes --body 'Needs a test'", want: true},
		{line: "gh pr merge 12 --squash --delete-branch", want: true},
		{line: "gh pr close 12", want: true},
		{line: "gh pr reopen 12", want: true},
		{line: "gh pr comment 12 --body 'Fixed in the last push'", want: true},
		{line: "gh pr edit 12 --add-label bug", want: true},
		{line: "gh pr ready 12", want: true},
		{line: "gh pr update-branch 12", want: true},
		{line: "gh issue close 7 --reason completed", want: true},
		{line: "gh issue reopen 7", want: true},
		{line: "gh issue comment 7 --body 'On it'", want: true},
		{line: "gh issue edit 7 --add-assignee @me", want: true},
		{line: "/usr/local/bin/gh pr merge 12", want: true},
		{line: "git push", want: true},
		{line: "git push origin HEAD", want: true},
		{line: "git -c color.ui=never push --force-with-lease", want: true},
		{line: "git -C /srv/wt/example-repo push", want: true},
		{line: "git --no-pager push", want: true},
		{line: "git -C push status", want: false},

		// The API: a method that writes, fields gh posts, or a GraphQL mutation.
		{line: "gh api -X POST repos/acme-org/example-repo/pulls/12/reviews -f event=APPROVE", want: true},
		{line: "gh api --method=PATCH repos/acme-org/example-repo/issues/7 -f state=closed", want: true},
		{line: "gh api -XPUT repos/acme-org/example-repo/pulls/12/merge", want: true},
		{line: "gh api repos/acme-org/example-repo/issues/7/comments -f body=Done", want: true},
		{line: "gh api repos/acme-org/example-repo/issues/7/comments --raw-field=body=Done", want: true},
		{line: `gh api graphql -f query='mutation { addComment(input: {}) { clientMutationId } }'`, want: true},
		{line: "gh api repos/acme-org/example-repo/pulls/12", want: false},
		{line: "gh api -X GET search/issues -f q=is:open", want: false},
		{line: `gh api graphql -f query='query { viewer { login } }'`, want: false},

		// Past the wrappers, among other commands, and into a shell's line.
		{line: "cd /srv/wt/example-repo && gh pr merge 12", want: true},
		{line: "timeout 30 gh pr review 12 --approve", want: true},
		{line: "env PAGER=cat gh pr comment 12 --body ok", want: true},
		{line: "echo 12 | xargs -n1 gh pr close", want: true},
		{line: "/bin/zsh -lc 'gh pr review 12 --approve'", want: true},
		{line: `bash -c "git push origin HEAD"`, want: true},
		{line: "echo $(gh pr merge 12)", want: true},

		// What only reads, or only names gh or git.
		{line: "gh pr view 12 --comments", want: false},
		{line: "gh pr diff 12", want: false},
		{line: "gh pr checks 12", want: false},
		{line: "gh pr list --author @me", want: false},
		{line: "gh issue view 7", want: false},
		{line: "gh pr create --fill", want: false},
		{line: "git status", want: false},
		{line: "git log --oneline -5", want: false},
		{line: "git commit -m 'git push later'", want: false},
		{line: "echo gh pr merge 12", want: false},
		{line: "grep -rn 'gh pr review' docs", want: false},
		{line: "/bin/zsh -lc 'gh pr view 12'", want: false},
		{line: "gh pr $ACTION 12", want: false},

		// A line the reader can't read tells nothing.
		{line: "gh pr merge 'unbalanced", want: false},
		{line: "", want: false},
		{line: "gh pr merge 12 \xff", want: false},
		{line: strings.Repeat("sh -c '", 12) + "gh pr merge 12" + strings.Repeat("'", 12), want: false},
	}

	for _, tt := range tests {
		t.Run(tt.line, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, fence.ChangesGitHub(tt.line))
		})
	}
}

func FuzzChangesGitHub(f *testing.F) {
	for _, seed := range []string{
		"gh pr review 12 --approve", "git push", "/bin/zsh -lc 'gh pr merge 12'", "echo gh pr merge",
		"gh api graphql -f query='mutation { x }'", "timeout 30 gh issue close 7", "sh -c 'sh -c \"git push\"'",
	} {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, line string) {
		got := fence.ChangesGitHub(line)

		// A line that names neither gh nor git, nor runs another shell's line, changes
		// nothing on GitHub.
		if !strings.Contains(line, "gh") && !strings.Contains(line, "git") {
			assert.False(t, got, "%q", line)
		}
	})
}
