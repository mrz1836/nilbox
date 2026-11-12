package repoguard_test

import (
	"path"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// scriptExtensions are the extensions of shell, Python, and other interpreted scripts.
func scriptExtensions() []string {
	return []string{".sh", ".bash", ".zsh", ".fish", ".py", ".pyw", ".ps1", ".bat", ".cmd", ".pl", ".rb"}
}

// syncedScriptDirs hold the scripts go-broadcast syncs, which the next sync overwrites
// (CLAUDE.md, "Synced files"); they are fixed there, not here.
func syncedScriptDirs() []string {
	return []string{".github/env/", ".github/scripts/"}
}

// isScript reports whether a repository file is a script: it has a script's extension,
// or its text starts with a #! line.
func isScript(rel, text string) bool {
	ext := strings.ToLower(path.Ext(rel))
	if slices.Contains(scriptExtensions(), ext) {
		return true
	}

	return strings.HasPrefix(text, "#!")
}

func TestIsScript(t *testing.T) {
	t.Parallel()

	cases := []struct {
		rel, text string
		want      bool
	}{
		{rel: "scripts/gate.sh", text: "set -e\n", want: true},
		{rel: "tools/release.py", text: "import os\n", want: true},
		{rel: "build/Setup.PS1", text: "", want: true},
		{rel: "hack/run", text: "#!/usr/bin/env bash\necho hi\n", want: true},
		{rel: "magefiles/frontend.go", text: "//go:build mage\n", want: false},
		{rel: "frontend/package.json", text: "{}\n", want: false},
		{rel: "docs/desktop.md", text: "Run `magex frontend:gate`.\n", want: false},
	}

	for _, c := range cases {
		assert.Equal(t, c.want, isScript(c.rel, c.text), c.rel)
	}
}

func TestNoScripts(t *testing.T) {
	t.Parallel()

	var found []string

files:
	for _, rel := range repoFiles(t) {
		for _, dir := range syncedScriptDirs() {
			if strings.HasPrefix(rel, dir) {
				continue files
			}
		}

		text, _ := readText(t, rel)
		if isScript(rel, text) {
			found = append(found, rel)
		}
	}

	assert.Empty(t, found, "write build, check, and release steps as magex targets in magefiles/, not scripts")
}
