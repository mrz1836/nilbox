package sensitive_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/sensitive"
)

func TestCompilePattern(t *testing.T) {
	t.Parallel()

	tests := []struct {
		pattern string
		path    string
		want    bool
	}{
		{pattern: "CLAUDE.md", path: "docs/claude.md", want: true},
		{pattern: ".github/workflows/", path: ".github/workflows/ci.yml", want: true},
		{pattern: ".github/workflows/", path: ".github/workflows", want: false},
		{pattern: "/Makefile", path: "tools/Makefile", want: false},
		{pattern: "/Makefile", path: "Makefile", want: true},
		{pattern: "deploy/**/*.tf", path: "deploy/prod/eu/main.tf", want: true},
		{pattern: "*.md", path: "README.md", want: true},
		{pattern: "*.md", path: "", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.pattern+" "+tt.path, func(t *testing.T) {
			t.Parallel()

			p, err := sensitive.CompilePattern(tt.pattern)
			require.NoError(t, err)
			assert.Equal(t, tt.want, p.Match(tt.path))
		})
	}
}

func TestCompilePatternRefusesWhatTheSettingsRefuse(t *testing.T) {
	t.Parallel()

	for _, pattern := range []string{"", "*", "**", "!keep.md", "a/../b", "[x"} {
		_, err := sensitive.CompilePattern(pattern)
		require.ErrorIs(t, err, sensitive.ErrInvalidPattern, pattern)
	}

	assert.False(t, sensitive.Pattern{}.Match("any/path"), "the zero pattern matches nothing")
}
