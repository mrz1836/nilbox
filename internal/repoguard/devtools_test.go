package repoguard_test

import (
	"go/build/constraint"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The inspector (internal/devtools) is for builds from source alone: a release must never
// carry it, nor a rehearsal of one. magex passes the devtools tag only to a build from
// source (buildTags in magefiles/bundle.go, which the magefiles' tests pin), so a file
// that imports the inspector must drop out of a build without that tag: its build
// constraint must be false for a release's tags.

// inspectorPackage is the inspector's import path under the module.
const inspectorPackage = "internal/devtools"

// releaseTags are the build tags a release's desktop binary is built with, with the
// system's: everything but devtools.
func releaseTags() []string {
	return []string{"desktop", "production", "darwin", "unix", "cgo", "arm64", "amd64"}
}

// inspectorScope reports whether the Go file rel is production code that must keep the
// inspector out of a release: the app's own, but not the inspector itself.
func inspectorScope(rel string) bool {
	return loggingScope(rel) && !strings.HasPrefix(rel, inspectorPackage+"/")
}

// importsInspector reports whether a file with these import paths imports the inspector.
func importsInspector(module string, imports []string) bool {
	return slices.Contains(imports, module+"/"+inspectorPackage)
}

// inRelease reports whether a Go file whose source is src builds into a release: it has no
// build constraint, or one a release's tags satisfy.
func inRelease(src string) (bool, error) {
	for line := range strings.SplitSeq(src, "\n") {
		line = strings.TrimSpace(line)

		if strings.HasPrefix(line, "package ") {
			break
		}

		if !constraint.IsGoBuild(line) {
			continue
		}

		expr, err := constraint.Parse(line)
		if err != nil {
			return false, err
		}

		return expr.Eval(func(tag string) bool { return slices.Contains(releaseTags(), tag) }), nil
	}

	return true, nil
}

func TestOnlyBuildsFromSourceCarryTheInspector(t *testing.T) {
	t.Parallel()

	module := modulePath(t, moduleRoot(t))

	var found []string

	for _, rel := range goFiles(t) {
		if !inspectorScope(rel) || !importsInspector(module, fileImports(t, rel)) {
			continue
		}

		src, _ := readText(t, rel)

		release, err := inRelease(src)
		require.NoError(t, err, rel)

		if release {
			found = append(found, rel)
		}
	}

	assert.Empty(t, found, "only a file built with the devtools tag may import "+inspectorPackage+
		", so no release carries the inspector")
}

func TestInRelease(t *testing.T) {
	t.Parallel()

	cases := []struct {
		src  string
		want bool
	}{
		{src: "package main\n", want: true},
		{src: "//go:build desktop\n\npackage main\n", want: true},
		{src: "//go:build desktop && devtools\n\npackage main\n", want: false},
		{src: "//go:build desktop && devtools && darwin\n\npackage main\n", want: false},
		{src: "//go:build desktop && !devtools\n\npackage main\n", want: true},
		{src: "//go:build devtools || desktop\n\npackage main\n", want: true},
		{src: "// A comment.\n\npackage main\n\n//go:build devtools\n", want: true},
	}

	for _, c := range cases {
		got, err := inRelease(c.src)
		require.NoError(t, err)
		assert.Equal(t, c.want, got, c.src)
	}

	_, err := inRelease("//go:build desktop &&\n\npackage main\n")
	require.Error(t, err)
}

func TestImportsInspector(t *testing.T) {
	t.Parallel()

	assert.True(t, importsInspector("example.com/m", []string{"fmt", "example.com/m/internal/devtools"}))
	assert.False(t, importsInspector("example.com/m", []string{"example.com/m/internal/devtools/sub", "fmt"}))
	assert.True(t, inspectorScope("cmd/app/main.go"))
	assert.False(t, inspectorScope("internal/devtools/server.go"))
	assert.False(t, inspectorScope("cmd/app/main_test.go"))
	assert.False(t, inspectorScope("magefiles/inspect.go"))
}
