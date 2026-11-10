package bump_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/bump"
)

func TestParseDependabot(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		title string
		want  bump.Update
	}{
		{
			name:  "a prefix and a directory",
			title: "chore(deps-dev): bump typescript from 5.9.3 to 6.0.3 in /frontend",
			want: bump.Update{
				Name: "typescript", From: "5.9.3", To: "6.0.3", Dir: "/frontend", Count: 1, Level: bump.LevelMajor,
			},
		},
		{
			name:  "a Go module",
			title: "build(deps): bump golang.org/x/net from 0.30.0 to 0.31.0",
			want: bump.Update{
				Name: "golang.org/x/net", From: "0.30.0", To: "0.31.0", Count: 1, Level: bump.LevelMinor,
			},
		},
		{
			name:  "an action's major versions",
			title: "Bump actions/checkout from 3 to 4",
			want:  bump.Update{Name: "actions/checkout", From: "3", To: "4", Count: 1, Level: bump.LevelMajor},
		},
		{
			name:  "a security update",
			title: "[Security] Bump lodash from 4.17.20 to 4.17.21",
			want: bump.Update{
				Name: "lodash", From: "4.17.20", To: "4.17.21", Count: 1, Level: bump.LevelPatch, Security: true,
			},
		},
		{
			name:  "a security update after a prefix",
			title: "chore(deps): [security] bump lodash from 4.17.20 to 4.17.21",
			want: bump.Update{
				Name: "lodash", From: "4.17.20", To: "4.17.21", Count: 1, Level: bump.LevelPatch, Security: true,
			},
		},
		{
			name:  "in a group",
			title: "Bump github/codeql-action from 4.39.1 to 4.39.3 in the github-actions group",
			want: bump.Update{
				Name: "github/codeql-action", From: "4.39.1", To: "4.39.3", Group: "github-actions", Count: 1,
				Level: bump.LevelPatch,
			},
		},
		{
			name:  "in a group across a directory",
			title: "chore(deps-dev): bump browserslist from 4.28.4 to 4.28.9 in the npm_and_yarn group across 1 directory",
			want: bump.Update{
				Name: "browserslist", From: "4.28.4", To: "4.28.9", Group: "npm_and_yarn", Count: 1,
				Level: bump.LevelPatch,
			},
		},
		{
			name:  "in a group across directories",
			title: "Bump vite from 6.1.0 to 6.2.0 in the npm_and_yarn group across 2 directories",
			want: bump.Update{
				Name: "vite", From: "6.1.0", To: "6.2.0", Group: "npm_and_yarn", Count: 1, Level: bump.LevelMinor,
			},
		},
		{
			name:  "in a directory and a group",
			title: "Bump eslint from 9.1.0 to 9.2.0 in /frontend in the lint group",
			want: bump.Update{
				Name: "eslint", From: "9.1.0", To: "9.2.0", Dir: "/frontend", Group: "lint", Count: 1,
				Level: bump.LevelMinor,
			},
		},
		{
			name:  "the root directory",
			title: "Bump node from 18 to 20 in /",
			want:  bump.Update{Name: "node", From: "18", To: "20", Count: 1, Level: bump.LevelMajor},
		},
		{
			name:  "commit SHAs in backticks",
			title: "Bump vendor/lib from `1a2b3c4` to `5d6e7f8`",
			want:  bump.Update{Name: "vendor/lib", From: "1a2b3c4", To: "5d6e7f8", Count: 1},
		},
		{
			name:  "a name in backticks",
			title: "Bump `left-pad` from 1.0.0 to 1.0.1",
			want:  bump.Update{Name: "left-pad", From: "1.0.0", To: "1.0.1", Count: 1, Level: bump.LevelPatch},
		},
		{
			name:  "a requirement",
			title: "Update rails requirement from ~> 6.0 to ~> 7.0",
			want:  bump.Update{Name: "rails", From: "~> 6.0", To: "~> 7.0", Count: 1, Level: bump.LevelMajor},
		},
		{
			name:  "a requirement in a directory",
			title: "build(deps): update rubocop requirement from >= 1.50, < 2.0 to >= 1.60, < 2.0 in /tools",
			want: bump.Update{
				Name: "rubocop", From: ">= 1.50, < 2.0", To: ">= 1.60, < 2.0", Dir: "/tools", Count: 1,
				Level: bump.LevelMinor,
			},
		},
		{
			name:  "a group",
			title: "Bump the go-deps group with 5 updates",
			want:  bump.Update{Group: "go-deps", Count: 5},
		},
		{
			name:  "a group of one",
			title: "chore(deps): bump the minor group with 1 update",
			want:  bump.Update{Group: "minor", Count: 1},
		},
		{
			name:  "a group in a directory",
			title: "Bump the go-deps group in /tools with 5 updates",
			want:  bump.Update{Group: "go-deps", Dir: "/tools", Count: 5},
		},
		{
			name:  "a group across directories",
			title: "Bump the npm_and_yarn group across 2 directories with 3 updates",
			want:  bump.Update{Group: "npm_and_yarn", Count: 3},
		},
		{
			name:  "a group across one directory",
			title: "Bump the dev group across 1 directory with 12 updates",
			want:  bump.Update{Group: "dev", Count: 12},
		},
		{
			name:  "a security group",
			title: "[Security] Bump the npm_and_yarn group across 1 directory with 2 updates",
			want:  bump.Update{Group: "npm_and_yarn", Count: 2, Security: true},
		},
		{
			name:  "two dependencies",
			title: "Bump react and react-dom",
			want:  bump.Update{Count: 2},
		},
		{
			name:  "two dependencies in a directory",
			title: "build(deps): bump @types/react and @types/react-dom in /frontend",
			want:  bump.Update{Dir: "/frontend", Count: 2},
		},
		{
			name:  "three dependencies",
			title: "Bump eslint, prettier and typescript",
			want:  bump.Update{Count: 3},
		},
		{
			name:  "a level in parentheses wins",
			title: "Bump lodash from 4.17.20 to 4.17.21 (minor)",
			want:  bump.Update{Name: "lodash", From: "4.17.20", To: "4.17.21", Count: 1, Level: bump.LevelMinor},
		},
		{
			name:  "surrounding space",
			title: "  bump x/y from 1.0.0 to 1.0.1  ",
			want:  bump.Update{Name: "x/y", From: "1.0.0", To: "1.0.1", Count: 1, Level: bump.LevelPatch},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, ok := bump.Parse(tt.title)
			require.True(t, ok, tt.title)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestParseRenovate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		title string
		want  bump.Update
	}{
		{
			name:  "a dependency's major",
			title: "chore(deps): update dependency eslint to v9",
			want:  bump.Update{Name: "eslint", To: "v9", Count: 1, Level: bump.LevelMajor},
		},
		{
			name:  "a bare major without a v",
			title: "Update dependency eslint to 9",
			want:  bump.Update{Name: "eslint", To: "9", Count: 1, Level: bump.LevelMajor},
		},
		{
			name:  "a dependency's exact version",
			title: "Update dependency @types/node to 22.10.1",
			want:  bump.Update{Name: "@types/node", To: "22.10.1", Count: 1},
		},
		{
			name:  "a range",
			title: "Update dependency rails to ~> 7.1",
			want:  bump.Update{Name: "rails", To: "~> 7.1", Count: 1},
		},
		{
			name:  "a module",
			title: "fix(deps): update module golang.org/x/net to v0.31.0",
			want:  bump.Update{Name: "golang.org/x/net", To: "v0.31.0", Count: 1},
		},
		{
			name:  "an action",
			title: "Update actions/setup-go action to v5",
			want:  bump.Update{Name: "actions/setup-go", To: "v5", Count: 1, Level: bump.LevelMajor},
		},
		{
			name:  "a Docker tag",
			title: "Update node Docker tag to v20",
			want:  bump.Update{Name: "node", To: "v20", Count: 1, Level: bump.LevelMajor},
		},
		{
			name:  "a Docker tag in lower case",
			title: "chore(deps): update golang docker tag to v1.23",
			want:  bump.Update{Name: "golang", To: "v1.23", Count: 1},
		},
		{
			name:  "a Docker digest",
			title: "Update node Docker digest to abc1234",
			want:  bump.Update{Name: "node", To: "abc1234", Count: 1},
		},
		{
			name:  "a digest of digits only has no level",
			title: "Update actions/checkout digest to 1234567",
			want:  bump.Update{Name: "actions/checkout", To: "1234567", Count: 1},
		},
		{
			name:  "a monorepo",
			title: "Update react monorepo to v18",
			want:  bump.Update{Name: "react", To: "v18", Level: bump.LevelMajor},
		},
		{
			name:  "all non-major dependencies",
			title: "Update all non-major dependencies",
			want:  bump.Update{},
		},
		{
			name:  "all dependencies",
			title: "chore(deps): update all dependencies",
			want:  bump.Update{},
		},
		{
			name:  "a security update",
			title: "Update dependency lodash to v4.17.21 [SECURITY]",
			want:  bump.Update{Name: "lodash", To: "v4.17.21", Count: 1, Security: true},
		},
		{
			name:  "a level in parentheses",
			title: "Update dependency vite to v6.2.0 (minor)",
			want:  bump.Update{Name: "vite", To: "v6.2.0", Count: 1, Level: bump.LevelMinor},
		},
		{
			name:  "a level and a security marker",
			title: "Update dependency vite to v6.2.1 (patch) [security]",
			want:  bump.Update{Name: "vite", To: "v6.2.1", Count: 1, Level: bump.LevelPatch, Security: true},
		},
		{
			name:  "a security marker and a level",
			title: "Update dependency vite to v7 [SECURITY] (Major)",
			want:  bump.Update{Name: "vite", To: "v7", Count: 1, Level: bump.LevelMajor, Security: true},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, ok := bump.Parse(tt.title)
			require.True(t, ok, tt.title)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestParseRefusesOtherTitles(t *testing.T) {
	t.Parallel()

	for _, title := range []string{
		"",
		"   ",
		"Fix the login bug",
		"Update README",
		"update docs",
		"Update README to v2",
		"Bump version to 1.2.0",
		"Bump version from 1.2.0 to 1.3.0",
		"chore: bump versions from 1.2.0 to 1.3.0",
		"Bump version and changelog",
		"Bump the version from 1 to 2",
		"Bump coverage from high to higher",
		"Bump lodash from 4.17.20 to 4.17.21 in the end",
		"Bump lodash from 4.17.20 to 4.17.21 and more",
		"Bump lodash from `` to 4.17.21",
		"Bump `` from 1.0.0 to 1.0.1",
		"Update rails requirement from the old one to the new one",
		"Update the group with 3 updates",
		"Bump the go-deps group",
		"Bump the go-deps group with many updates",
		"Bump the go-deps group with 9999999999 updates",
		"Bump react and",
		"Bump react, and react-dom",
		"Update dependency eslint to latest",
		"Update dependency eslint to the next one 9",
		"Update dependency `` to v9",
		"Update node Docker digest to abc 123",
		"Update node image to v20",
		"Update all the things",
		"Update all minor dependencies",
		"[Security] Fix the login bug",
		"feat: add a bump command",
		"Revert \"Bump lodash from 4.17.20 to 4.17.21\"",
	} {
		_, ok := bump.Parse(title)
		assert.False(t, ok, title)
	}
}

func TestUpdateSingle(t *testing.T) {
	t.Parallel()

	tests := []struct {
		title string
		want  bool
	}{
		{title: "Bump actions/checkout from 3 to 4", want: true},
		{title: "Bump lodash from 4.17.20 to 4.17.21 in the npm group", want: true},
		{title: "Bump the go-deps group with 5 updates", want: false},
		{title: "Bump react and react-dom", want: false},
		{title: "Update dependency eslint to v9", want: false},
		{title: "Update react monorepo to v18", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.title, func(t *testing.T) {
			t.Parallel()

			u, ok := bump.Parse(tt.title)
			require.True(t, ok)
			assert.Equal(t, tt.want, u.Single())
		})
	}

	assert.False(t, bump.Update{}.Single())
}

func TestUpdateJSON(t *testing.T) {
	t.Parallel()

	u, ok := bump.Parse("[Security] Bump lodash from 4.17.20 to 4.17.21 in /web in the npm group")
	require.True(t, ok)

	raw, err := json.Marshal(u)
	require.NoError(t, err)
	assert.JSONEq(t, `{"name": "lodash", "from": "4.17.20", "to": "4.17.21", "group": "npm", "dir": "/web",
		"count": 1, "level": "patch", "security": true}`, string(raw))
}
