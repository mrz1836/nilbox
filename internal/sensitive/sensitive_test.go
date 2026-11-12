package sensitive_test

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/sensitive"
	"github.com/mrz1836/nilbox/internal/trust"
)

// prURL is a pull request's page, for diff links.
const prURL = "https://github.com/acme-org/example-repo/pull/7"

func TestDefaultsFlagTheFilesThatMatter(t *testing.T) {
	t.Parallel()

	m := sensitive.NewMatcher(sensitive.DefaultConfig())

	cases := map[string]string{
		".claude/settings.json":                 sensitive.CategoryAgent,
		".claude/hooks/start.sh":                sensitive.CategoryAgent,
		"tools/.claude/commands/x.md":           sensitive.CategoryAgent,
		".mcp.json":                             sensitive.CategoryAgent,
		"CLAUDE.md":                             sensitive.CategoryAgent,
		"pkg/api/CLAUDE.md":                     sensitive.CategoryAgent,
		"AGENTS.md":                             sensitive.CategoryAgent,
		".github/AGENTS.md":                     sensitive.CategoryAgent,
		".github/copilot-instructions.md":       sensitive.CategoryAgent,
		".cursor/rules/x.mdc":                   sensitive.CategoryAgent,
		".github/workflows/ci.yml":              sensitive.CategoryWorkflows,
		".github/workflows/sub/x.yaml":          sensitive.CategoryWorkflows,
		".github/actions/setup/action.yml":      sensitive.CategoryWorkflows,
		"tools/deploy/action.yaml":              sensitive.CategoryWorkflows,
		".github/env/10-mage-x.env":             sensitive.CategoryWorkflows,
		".goreleaser.yml":                       sensitive.CategoryWorkflows,
		"Makefile":                              sensitive.CategoryTooling,
		"magefiles/build.go":                    sensitive.CategoryTooling,
		"scripts/release.sh":                    sensitive.CategoryTooling,
		"hack/gen.sh":                           sensitive.CategoryTooling,
		".husky/pre-commit":                     sensitive.CategoryTooling,
		".vscode/tasks.json":                    sensitive.CategoryTooling,
		".envrc":                                sensitive.CategoryTooling,
		"frontend/package.json":                 sensitive.CategoryTooling,
		"Dockerfile":                            sensitive.CategoryTooling,
		"CODEOWNERS":                            sensitive.CategoryReview,
		".github/CODEOWNERS":                    sensitive.CategoryReview,
		".github/dependabot.yml":                sensitive.CategoryReview,
		"go.mod":                                sensitive.CategoryDependencies,
		"go.sum":                                sensitive.CategoryDependencies,
		"frontend/pnpm-lock.yaml":               sensitive.CategoryDependencies,
		"requirements-dev.txt":                  sensitive.CategoryDependencies,
		"vendor/github.com/acme-org/lib/lib.go": sensitive.CategoryDependencies,
		".gitmodules":                           sensitive.CategoryDependencies,
	}

	for path, want := range cases {
		cat, _, ok := m.Match(path)
		if assert.True(t, ok, path) {
			assert.Equal(t, want, cat.ID, path)
		}
	}

	for _, path := range []string{
		"README.md", "internal/inbox/service.go", "docs/desktop.md", ".github/ISSUE_TEMPLATE/bug.md",
		".github/pull_request_template.md", "frontend/src/App.tsx", "claude.go", "docs/claude-notes.md",
		"scripts.go", "go.modules.md", "makefile.md",
	} {
		_, _, ok := m.Match(path)
		assert.False(t, ok, path)
	}
}

func TestMatchingIgnoresCase(t *testing.T) {
	t.Parallel()

	m := sensitive.NewMatcher(sensitive.DefaultConfig())

	// A case-insensitive file system opens these for .claude/settings.json and friends.
	for _, path := range []string{".Claude/Settings.json", ".GITHUB/Workflows/CI.yml", "makefile", "Go.Mod"} {
		_, _, ok := m.Match(path)
		assert.True(t, ok, path)
	}
}

func TestPatternSyntax(t *testing.T) {
	t.Parallel()

	cases := []struct {
		pattern string
		match   []string
		miss    []string
	}{
		{
			pattern: "secrets.txt",
			match:   []string{"secrets.txt", "a/b/secrets.txt", "secrets.txt/inside"},
			miss:    []string{"my-secrets.txt", "secrets.txt.bak"},
		},
		{
			pattern: "*.tf",
			match:   []string{"main.tf", "infra/prod/main.tf"},
			miss:    []string{"main.tfvars", "tf"},
		},
		{
			pattern: "deploy/",
			match:   []string{"deploy/x.yml", "services/deploy/x.yml"},
			miss:    []string{"deploy", "deployment/x.yml"},
		},
		{
			pattern: "infra/prod",
			match:   []string{"infra/prod", "infra/prod/main.tf"},
			miss:    []string{"x/infra/prod/main.tf", "infra/production/main.tf"},
		},
		{
			pattern: "/Makefile",
			match:   []string{"Makefile"},
			miss:    []string{"sub/Makefile"},
		},
		{
			pattern: "deploy/**/*.tf",
			match:   []string{"deploy/a.tf", "deploy/x/y/a.tf"},
			miss:    []string{"other/deploy/a.tf", "deploy/a.yml"},
		},
		{
			pattern: "**/migrations/*.sql",
			match:   []string{"migrations/1.sql", "a/b/migrations/2.sql"},
			miss:    []string{"migrations/1.go"},
		},
		{
			pattern: "config?.y[a]ml",
			match:   []string{"config1.yaml", "x/configA.yaml"},
			miss:    []string{"config.yaml", "config12.yaml"},
		},
	}

	for _, tc := range cases {
		cfg := sensitive.DefaultConfig()
		cfg.Patterns = []string{tc.pattern}
		cfg.Off = []string{
			sensitive.CategoryWorkflows, sensitive.CategoryTooling, sensitive.CategoryReview, sensitive.CategoryDependencies,
		}

		m := sensitive.NewMatcher(cfg)

		for _, path := range tc.match {
			cat, raw, ok := m.Match(path)
			if assert.True(t, ok, "%s should match %s", tc.pattern, path) {
				assert.Equal(t, sensitive.CategoryCustom, cat.ID)
				assert.Equal(t, tc.pattern, raw)
			}
		}

		for _, path := range tc.miss {
			_, _, ok := m.Match(path)
			assert.False(t, ok, "%s should not match %s", tc.pattern, path)
		}
	}
}

func TestNormalizePatterns(t *testing.T) {
	t.Parallel()

	got, err := sensitive.NormalizePatterns([]string{"  deploy/ ", "", "*.TF", "*.tf", "deploy/"})
	require.NoError(t, err)
	assert.Equal(t, []string{"deploy/", "*.TF"}, got)

	got, err = sensitive.NormalizePatterns(nil)
	require.NoError(t, err)
	assert.Empty(t, got)
	assert.NotNil(t, got)
}

func TestNormalizePatternsRefusesUnusablePatterns(t *testing.T) {
	t.Parallel()

	for pattern, why := range map[string]string{
		"*":                      "matches every file",
		"**":                     "matches every file",
		"**/*":                   "matches every file",
		"/":                      "names no file",
		"!vendor/":               "exclusions",
		"a/../b":                 ".. isn't allowed",
		"x[":                     "not a valid name pattern",
		"a\tb":                   "control characters",
		strings.Repeat("a", 201): "longer than 200",
		"**/**/**/**/**/x":       "more than 4",
	} {
		_, err := sensitive.NormalizePatterns([]string{pattern})
		require.ErrorIs(t, err, sensitive.ErrInvalidPattern, pattern)
		assert.Contains(t, err.Error(), why, pattern)
	}

	many := make([]string, sensitive.MaxPatterns+1)
	for i := range many {
		many[i] = "file" + strings.Repeat("x", i)
	}

	_, err := sensitive.NormalizePatterns(many)
	require.ErrorIs(t, err, sensitive.ErrTooManyPatterns)
}

func TestSetCategory(t *testing.T) {
	t.Parallel()

	off, err := sensitive.SetCategory(nil, sensitive.CategoryDependencies, false)
	require.NoError(t, err)
	assert.Equal(t, []string{sensitive.CategoryDependencies}, off)

	off, err = sensitive.SetCategory(off, sensitive.CategoryReview, false)
	require.NoError(t, err)
	assert.Equal(t, []string{sensitive.CategoryDependencies, sensitive.CategoryReview}, off)

	off, err = sensitive.SetCategory(off, sensitive.CategoryDependencies, true)
	require.NoError(t, err)
	assert.Equal(t, []string{sensitive.CategoryReview}, off)

	_, err = sensitive.SetCategory(off, sensitive.CategoryAgent, false)
	require.ErrorIs(t, err, sensitive.ErrLockedCategory)

	_, err = sensitive.SetCategory(off, "nope", false)
	require.ErrorIs(t, err, sensitive.ErrUnknownCategory)

	_, err = sensitive.SetCategory(off, sensitive.CategoryCustom, false)
	require.ErrorIs(t, err, sensitive.ErrUnknownCategory)
}

func TestOffCategoriesAreNotMatched(t *testing.T) {
	t.Parallel()

	cfg := sensitive.DefaultConfig()
	cfg.Off = []string{sensitive.CategoryDependencies}

	_, _, ok := sensitive.NewMatcher(cfg).Match("go.mod")
	assert.False(t, ok)
}

func TestDecodeConfig(t *testing.T) {
	t.Parallel()

	assert.Equal(t, sensitive.DefaultConfig(), sensitive.DecodeConfig(nil))

	cfg := sensitive.DecodeConfig(map[string]string{
		sensitive.PrefScope:    string(sensitive.ScopeOutside),
		sensitive.PrefOff:      `["review","agent","nope","dependencies","review"]`,
		sensitive.PrefPatterns: `["deploy/","*","*.tf"]`,
	})

	assert.Equal(t, sensitive.Config{
		Scope:    sensitive.ScopeOutside,
		Off:      []string{sensitive.CategoryDependencies, sensitive.CategoryReview},
		Patterns: []string{"deploy/", "*.tf"},
	}, cfg)

	bad := sensitive.DecodeConfig(map[string]string{
		sensitive.PrefScope: "everyone", sensitive.PrefOff: "{", sensitive.PrefPatterns: "null",
	})
	assert.Equal(t, sensitive.DefaultConfig(), bad)
}

func TestChecked(t *testing.T) {
	t.Parallel()

	cases := []struct {
		scope sensitive.Scope
		class trust.Class
		assoc string
		mine  bool
		want  bool
	}{
		{sensitive.ScopeOutside, trust.ClassOutside, "NONE", false, true},
		{sensitive.ScopeOutside, trust.ClassOutside, "COLLABORATOR", false, true}, // a fork
		{sensitive.ScopeOutside, trust.ClassMember, "COLLABORATOR", false, false},
		{sensitive.ScopeCollaborators, trust.ClassMember, "COLLABORATOR", false, true},
		{sensitive.ScopeCollaborators, trust.ClassMember, " collaborator ", false, true},
		{sensitive.ScopeCollaborators, trust.ClassMember, "MEMBER", false, false},
		{sensitive.ScopeCollaborators, trust.ClassMember, "OWNER", false, false},
		{sensitive.ScopeCollaborators, trust.ClassBot, "NONE", false, false},
		{sensitive.ScopeCollaborators, trust.ClassOutside, "NONE", true, false},
		{sensitive.ScopeCollaborators, trust.ClassMember, "COLLABORATOR", true, false},
	}

	for _, tc := range cases {
		assert.Equal(t, tc.want, sensitive.Checked(tc.scope, tc.class, tc.assoc, tc.mine), "%+v", tc)
	}
}

func TestEvaluate(t *testing.T) {
	t.Parallel()

	m := sensitive.NewMatcher(sensitive.DefaultConfig())
	at := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)

	assert.Equal(t, sensitive.Check{Status: sensitive.StatusPending, Groups: []sensitive.Group{}}, m.Evaluate(nil, "abc", prURL))

	flagged := &sensitive.Result{
		Head: "abc", Total: 4, Complete: true, CheckedAt: at,
		Files: []sensitive.File{
			{Path: "README.md", Change: "modified"},
			{Path: ".github/workflows/ci.yml", Change: "modified"},
			{Path: ".claude/settings.json", Change: "added"},
			{Path: ".claude/agents/x.md", Change: "added"},
		},
	}

	c := m.Evaluate(flagged, "ABC", prURL)
	assert.Equal(t, sensitive.StatusFlagged, c.Status)
	assert.Equal(t, "2026-05-01T12:00:00Z", c.CheckedAt)
	assert.Equal(t, 4, c.Checked)
	assert.Equal(t, 4, c.Total)
	require.Len(t, c.Groups, 2)
	assert.Equal(t, sensitive.CategoryAgent, c.Groups[0].Category)
	assert.Equal(t, sensitive.LevelDanger, c.Groups[0].Level)
	assert.Equal(t, []string{".claude/agents/x.md", ".claude/settings.json"}, paths(c.Groups[0]))
	assert.Equal(t, ".claude/", c.Groups[0].Files[0].Pattern)
	assert.Equal(t, sensitive.CategoryWorkflows, c.Groups[1].Category)
	assert.Equal(t,
		prURL+"/files#diff-b803fcb7f17ed9235f1e5cb1fcd2f5d3b2838429d4368ae4c57ce4436577f03f",
		c.Groups[1].Files[0].URL)

	agent, ok := c.Category(sensitive.CategoryAgent)
	assert.True(t, ok)
	assert.Len(t, agent.Files, 2)

	_, ok = c.Category(sensitive.CategoryTooling)
	assert.False(t, ok)

	// Pushed since: pending until checked again.
	assert.Equal(t, sensitive.StatusPending, m.Evaluate(flagged, "def", prURL).Status)

	clean := &sensitive.Result{Head: "abc", Total: 1, Complete: true, Files: []sensitive.File{{Path: "README.md"}}}
	assert.Equal(t, sensitive.StatusClear, m.Evaluate(clean, "abc", prURL).Status)

	partial := &sensitive.Result{Head: "abc", Total: 3500, Files: []sensitive.File{{Path: "README.md"}}}
	p := m.Evaluate(partial, "", prURL)
	assert.Equal(t, sensitive.StatusPartial, p.Status)
	assert.Equal(t, 1, p.Checked)
	assert.Equal(t, 3500, p.Total)

	failed := &sensitive.Result{Head: "abc", Error: "GitHub said no"}
	f := m.Evaluate(failed, "def", prURL)
	assert.Equal(t, sensitive.StatusFailed, f.Status)
	assert.Equal(t, "GitHub said no", f.Error)
}

func TestRenamesMatchEitherPath(t *testing.T) {
	t.Parallel()

	m := sensitive.NewMatcher(sensitive.DefaultConfig())

	r := &sensitive.Result{Head: "abc", Total: 2, Complete: true, Files: []sensitive.File{
		// Moving a workflow away disables it.
		{Path: "docs/old-codeql.yml", PreviousPath: ".github/workflows/codeql.yml", Change: "renamed"},
		// A tooling file moved into the agent's folder counts as the agent's.
		{Path: ".claude/hooks/x.sh", PreviousPath: "scripts/x.sh", Change: "renamed"},
	}}

	c := m.Evaluate(r, "abc", prURL)
	require.Len(t, c.Groups, 2)
	assert.Equal(t, sensitive.CategoryAgent, c.Groups[0].Category)
	assert.Equal(t, sensitive.CategoryWorkflows, c.Groups[1].Category)
	assert.Equal(t, ".github/workflows/codeql.yml", c.Groups[1].Files[0].PreviousPath)
	assert.Equal(t, ".github/workflows/", c.Groups[1].Files[0].Pattern)
}

func TestSummary(t *testing.T) {
	t.Parallel()

	m := sensitive.NewMatcher(sensitive.DefaultConfig())

	c := m.Evaluate(&sensitive.Result{Head: "abc", Total: 3, Complete: true, Files: []sensitive.File{
		{Path: "docs/old.yml", PreviousPath: ".github/workflows/ci.yml", Change: "renamed"},
		{Path: ".claude/settings.json", Change: "added"},
		{Path: "README.md", Change: "modified"},
	}}, "abc", prURL)

	assert.Equal(t, ".claude/settings.json (Agent config), docs/old.yml (Workflows, moved from "+
		".github/workflows/ci.yml)", c.Summary())

	assert.Empty(t, sensitive.Check{}.Summary())

	files := make([]sensitive.File, 0, 23)
	for i := range 23 {
		files = append(files, sensitive.File{Path: fmt.Sprintf("s%02d.sh", i)})
	}

	many := m.Evaluate(&sensitive.Result{Head: "abc", Total: 23, Complete: true, Files: files}, "abc", prURL)
	assert.True(t, strings.HasSuffix(many.Summary(), "s19.sh (Build tooling), and 3 more"), many.Summary())
}

func TestLookup(t *testing.T) {
	t.Parallel()

	cfg := sensitive.DefaultConfig()
	cfg.Patterns = []string{"*.tf"}

	assert.Equal(t, sensitive.PathMatch{
		Sensitive: true, Category: sensitive.CategoryWorkflows, Label: "Workflows and CI",
		Level: sensitive.LevelDanger, Pattern: ".github/workflows/",
	}, sensitive.Lookup(cfg, " .github/workflows/ci.yml "))

	assert.Equal(t, sensitive.CategoryCustom, sensitive.Lookup(cfg, "infra/main.tf").Category)
	assert.Equal(t, sensitive.PathMatch{}, sensitive.Lookup(cfg, "README.md"))
	assert.Equal(t, sensitive.PathMatch{}, sensitive.Lookup(cfg, ""))
}

func TestCategoriesAreWellFormed(t *testing.T) {
	t.Parallel()

	seen := map[string]bool{}

	for _, c := range sensitive.Categories() {
		assert.False(t, seen[c.ID], c.ID)
		seen[c.ID] = true

		assert.NotEmpty(t, c.Label, c.ID)
		assert.NotEmpty(t, c.Short, c.ID)
		assert.NotEmpty(t, c.Description, c.ID)
		assert.Contains(t, []sensitive.Level{sensitive.LevelDanger, sensitive.LevelWarning}, c.Level, c.ID)
		assert.NotEmpty(t, c.Patterns, c.ID)

		// Every built-in pattern is one a user could enter.
		_, err := sensitive.NormalizePatterns(c.Patterns)
		require.NoError(t, err, c.ID)
	}

	assert.True(t, sensitive.Categories()[0].Locked, "the agent category comes first and is always on")
}

func paths(g sensitive.Group) []string {
	out := make([]string, 0, len(g.Files))
	for _, f := range g.Files {
		out = append(out, f.Path)
	}

	return out
}

func BenchmarkEvaluateLargePullRequest(b *testing.B) {
	m := sensitive.NewMatcher(sensitive.DefaultConfig())
	files := make([]sensitive.File, 3000)

	for i := range files {
		files[i] = sensitive.File{Path: "internal/pkg" + strings.Repeat("x", i%7) + "/sub/file.go", Change: "modified"}
	}

	r := &sensitive.Result{Head: "abc", Total: len(files), Files: files, Complete: true}

	for b.Loop() {
		m.Evaluate(r, "abc", prURL)
	}
}
