package spec_test

import (
	"flag"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/autopilot/spec"
)

// update makes the golden tests rewrite their golden files from the current output
// instead of comparing against them.
var update = flag.Bool("update", false, "rewrite the golden files from the current output") //nolint:gochecknoglobals // a test flag

// templateNames returns the names of the template fixtures under testdata/templates,
// sorted.
func templateNames(t testing.TB) []string {
	t.Helper()

	entries, err := os.ReadDir(filepath.Join("testdata", "templates"))
	require.NoError(t, err)

	names := make([]string, 0, len(entries))

	for _, e := range entries {
		if name, ok := strings.CutSuffix(e.Name(), ".json"); ok {
			names = append(names, name)
		}
	}

	slices.Sort(names)
	require.NotEmpty(t, names)

	return names
}

// templateJSON reads a template fixture's JSON.
func templateJSON(t testing.TB, name string) []byte {
	t.Helper()

	data, err := os.ReadFile(filepath.Join("testdata", "templates", name+".json")) //nolint:gosec // a fixture's name
	require.NoError(t, err)

	return data
}

// loadTemplate loads a template fixture, which must load.
func loadTemplate(t *testing.T, name string) spec.Spec {
	t.Helper()

	s, err := spec.Load(templateJSON(t, name))
	require.NoError(t, err, name)

	return s
}

// starterNames names the starter prompts the templates run, as the library names them.
func starterNames(p spec.PromptRef) string {
	return map[string]string{
		"fix-dependency-security": "Fix dependency security",
		"heal-dependabot-pr":      "Heal Dependabot PR",
		"review-pr":               "Review",
		"fix-checks":              "Fix checks",
		"address-review":          "Address review",
		"quick-look":              "Quick look",
		"work-on-issue":           "Work on it",
		"resolve-conflicts":       "Resolve conflicts",
		"rerun-failed-jobs":       "Re-run failed jobs",
	}[p.Starter]
}

// golden compares got with the golden file at path, after its header, or rewrites it
// with -update.
func golden(t *testing.T, path, header, got string) {
	t.Helper()

	text := header + got

	if *update {
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o750))
		require.NoError(t, os.WriteFile(path, []byte(text), 0o644)) //nolint:gosec // committed test data, world-readable
		require.NoError(t, os.Chmod(path, 0o644))                   //nolint:gosec // committed test data, world-readable
	}

	want, err := os.ReadFile(path) //nolint:gosec // a fixed case name under testdata
	require.NoError(t, err)
	require.Equal(t, string(want), text)
}

// ptr returns a pointer to v.
func ptr[T any](v T) *T {
	return &v
}
