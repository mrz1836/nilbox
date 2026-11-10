package bump_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/mrz1836/nilbox/internal/bump"
)

// levels are every Level a function may return.
func levels() []bump.Level {
	return []bump.Level{bump.LevelUnknown, bump.LevelPatch, bump.LevelMinor, bump.LevelMajor}
}

// FuzzParse checks that any title parses without a panic, and that an update read from
// one has a known level, a count that is not negative, and trimmed fields, while a title
// that is not an update yields the zero Update.
func FuzzParse(f *testing.F) {
	for _, seed := range []string{
		"chore(deps-dev): bump typescript from 5.9.3 to 6.0.3 in /frontend",
		"[Security] Bump lodash from 4.17.20 to 4.17.21",
		"Bump github/codeql-action from 4.39.1 to 4.39.3 in the github-actions group",
		"Bump the npm_and_yarn group across 2 directories with 3 updates",
		"Bump the go-deps group in /tools with 5 updates",
		"Update rails requirement from ~> 6.0 to ~> 7.0",
		"Bump eslint, prettier and typescript in /web",
		"chore(deps): update dependency eslint to v9 [SECURITY] (major)",
		"Update node Docker digest to abc1234",
		"Update all non-major dependencies",
		"Bump `` from `` to ``",
		"bump   from 1 to 2",
		"Fix the login bug",
		"\xff\xfe",
		"",
	} {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, title string) {
		u, ok := bump.Parse(title)
		if !ok {
			assert.Equal(t, bump.Update{}, u)

			return
		}

		assert.Contains(t, levels(), u.Level)
		assert.GreaterOrEqual(t, u.Count, 0)

		for _, field := range []string{u.Name, u.From, u.To, u.Group, u.Dir} {
			assert.Equal(t, strings.TrimSpace(field), field, "untrimmed field in %q", title)
		}

		if u.Name != "" {
			assert.NotEmpty(t, u.To, "a named update has a target in %q", title)
		}

		if u.Single() {
			assert.Equal(t, 1, u.Count)
		}
	})
}

// FuzzCompare checks that any two versions compare without a panic to a known level, the
// same level both ways, and to LevelUnknown against themselves.
func FuzzCompare(f *testing.F) {
	for _, seed := range [][2]string{
		{"5.9.3", "6.0.3"},
		{"0.30.0", "0.31.0"},
		{"4.17.20", "4.17.21"},
		{"~> 6.0", "~> 7.0"},
		{"v1.2.3-beta", "1.2.3"},
		{"abc1234", "def5678"},
		{"", ""},
		{"..", "1..2"},
		{"\xff", "1.\xff"},
	} {
		f.Add(seed[0], seed[1])
	}

	f.Fuzz(func(t *testing.T, from, to string) {
		level := bump.Compare(from, to)

		assert.True(t, slices.Contains(levels(), level), "unknown level %q", level)
		assert.Equal(t, level, bump.Compare(to, from), "%q and %q compare differently each way", from, to)
		assert.Equal(t, bump.LevelUnknown, bump.Compare(from, from))
	})
}

// FuzzFromCommit checks that any commit message reads without a panic to a known level,
// and that it reports finding one exactly when the level is known.
func FuzzFromCommit(f *testing.F) {
	for _, seed := range []string{
		"---\nupdated-dependencies:\n- dependency-name: lodash\n  update-type: version-update:semver-patch\n...\n",
		"  update-type: version-update:semver-major\n  update-type: version-update:semver-minor\n",
		"update-type: x:semver-\n",
		"",
	} {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, msg string) {
		level, ok := bump.FromCommit(msg)

		assert.Contains(t, levels(), level)
		assert.Equal(t, level != bump.LevelUnknown, ok)
	})
}
