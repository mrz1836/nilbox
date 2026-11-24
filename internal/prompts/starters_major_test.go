package prompts

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/agent"
)

// majorUpdateData is prompt data for Dependabot's major update pull request 230, which
// moves vite across a major in /frontend, with its dependency read from its title as a
// launch reads it (WithUpdate).
func majorUpdateData() Data {
	d := Data{
		Kind: "pull request", IsPR: true, Number: 230, Title: "Bump vite from 6.3.5 to 7.0.0 in /frontend",
		URL: "https://github.com/acme-org/example-repo/pull/230", Author: "dependabot[bot]", IsBot: true,
		Repo: "acme-org/example-repo", Owner: "acme-org", RepoName: "example-repo", Remote: "origin",
		Branch: "dependabot/npm_and_yarn/frontend/vite-7.0.0", HeadCommit: "9c1e8b7d6a5f4e3d2c1b0a9f8e7d6c5b4a394f2a",
		Checks: "passing",
	}

	return d.WithUpdate(d.Title)
}

// groupUpdateData is prompt data for Dependabot's update of a group, whose title names no
// one dependency.
func groupUpdateData() Data {
	d := majorUpdateData()
	d.Title, d.Dependency, d.FromVersion, d.ToVersion, d.UpdateType = "Bump the npm-major group in /frontend with 3 updates", "", "", "", ""

	return d.WithUpdate(d.Title)
}

// TestAuditMajorUpdateGoldens pins the major update audit's text for one dependency in a
// worktree at the pull request's head, and for a group's update in one left at an older
// commit. Rewrite them with go test ./internal/prompts -run TestAuditMajorUpdateGoldens -update.
func TestAuditMajorUpdateGoldens(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		data Data
	}{
		{name: "audit major update at the head", data: majorUpdateData().WithSession(true, true)},
		{name: "audit major update of a group", data: groupUpdateData().WithSession(true, false)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := starterTemplate(t, StarterAuditMajorUpdate).Render(tt.data)
			require.NoError(t, err)

			path := goldenPath(tt.name)
			if *update {
				writeGolden(t, path, got)
			}

			want, err := os.ReadFile(path) //nolint:gosec // a fixed case name under testdata
			require.NoError(t, err)
			assert.Equal(t, string(want), got)
		})
	}
}

// TestAuditMajorUpdateNamesWhatMoves checks the audit says what the update moves from its
// title, and words a group's update, which names no one dependency, without it.
func TestAuditMajorUpdateNamesWhatMoves(t *testing.T) {
	t.Parallel()

	tmpl := starterTemplate(t, StarterAuditMajorUpdate)

	out, err := tmpl.Execute(majorUpdateData())
	require.NoError(t, err)
	assert.Contains(t, out, "dependabot[bot] opened it, and it moves vite from 6.3.5 to 7.0.0, a major update. "+
		"Its checks are passing.")

	out, err = tmpl.Execute(groupUpdateData())
	require.NoError(t, err)
	assert.Contains(t, out, "dependabot[bot] opened it. It may move several dependencies at once, as a group's update does.")

	bare := majorUpdateData()
	bare.FromVersion, bare.UpdateType, bare.Checks = "", "", ""
	out, err = tmpl.Execute(bare)
	require.NoError(t, err)
	assert.Contains(t, out, "dependabot[bot] opened it, and it moves vite to 7.0.0. Passing checks")
}

// TestAuditMajorUpdateClosesOnlyABotsUpdate checks the audit tells only a bot's update to
// stop and closes it: a person's dependency update is reported, never commented on or
// closed, and no pull request is ever approved or merged.
func TestAuditMajorUpdateClosesOnlyABotsUpdate(t *testing.T) {
	t.Parallel()

	tmpl := starterTemplate(t, StarterAuditMajorUpdate)

	out, err := tmpl.Execute(majorUpdateData().WithSession(true, true))
	require.NoError(t, err)
	assert.Contains(t, out, "`@dependabot ignore this major version`")
	assert.Contains(t, out, "`@dependabot ignore DEPENDENCY_NAME major version`")
	assert.Contains(t, out, "`gh pr comment 230 --repo acme-org/example-repo`")
	assert.Contains(t, out, "`gh pr close 230 --repo acme-org/example-repo`")
	assert.Contains(t, out, "Never approve or merge it, and never force-push.")

	person := majorUpdateData()
	person.Author, person.IsBot = "octo-user", false

	out, err = tmpl.Execute(person.WithSession(true, true))
	require.NoError(t, err)
	assert.NotContains(t, out, "@dependabot")
	assert.NotContains(t, out, "gh pr comment")
	assert.NotContains(t, out, "gh pr close")
	assert.Contains(t, out, "post nothing and close nothing: octo-user isn't a bot")
	assert.Contains(t, out, "Never approve or merge it, and never force-push.")
}

// TestAuditMajorUpdatePushesOnlyTheUpdatesBranch checks the changes an update needs are
// pushed only to its own branch, by its full ref, and never for a fork's pull request.
func TestAuditMajorUpdatePushesOnlyTheUpdatesBranch(t *testing.T) {
	t.Parallel()

	tmpl := starterTemplate(t, StarterAuditMajorUpdate)

	out, err := tmpl.Execute(majorUpdateData().WithSession(true, true))
	require.NoError(t, err)
	assert.Contains(t, out, "`git push origin HEAD:refs/heads/dependabot/npm_and_yarn/frontend/vite-7.0.0`")

	fork := majorUpdateData()
	fork.OutsideContributor = true

	out, err = tmpl.Execute(fork.WithSession(true, true))
	require.NoError(t, err)
	assert.NotContains(t, out, "git push")
	assert.Contains(t, out, "but push nothing: this pull request comes from a fork")
}

// TestAuditMajorUpdateSettings checks the audit runs in a worktree of its own, out of plan
// mode, at high effort, for pull requests, with no model of its own.
func TestAuditMajorUpdateSettings(t *testing.T) {
	t.Parallel()

	st, ok := starterFor(StarterAuditMajorUpdate)
	require.True(t, ok)

	assert.Equal(t, []string{"Audit a major dependency update", "Audit major update"}, []string{st.Name, st.Nickname})
	assert.Equal(t, AppliesToPR, st.AppliesTo)
	assert.Equal(t, PurposeAgent, st.purpose())
	assert.Equal(t, "high", st.Effort)
	require.NoError(t, agent.ValidateEffortAny(st.Effort))
	require.NotNil(t, st.PlanMode)
	assert.False(t, *st.PlanMode)
	require.NotNil(t, st.Worktree)
	assert.True(t, *st.Worktree)
	assert.Empty(t, st.Models)
}
