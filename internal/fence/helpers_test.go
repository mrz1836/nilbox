package fence_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/fence"
)

// The run every test fences: a Dependabot pull request's worktree in a clone under the
// home folder, with the app's data and the run's folder beside it.
const (
	home      = "/Users/example"
	clone     = home + "/projects/example-repo"
	worktree  = clone + "/.worktrees/example-repo/pr-12"
	gitDir    = clone + "/.git/worktrees/pr-12"
	commonDir = clone + "/.git"
	dataDir   = home + "/Library/Application Support/App"
	runDir    = dataDir + "/agent-runs/00000000-0000-4000-8000-000000000012"
	configDir = home + "/.claude"
	branch    = "dependabot/go_modules/golang.org/x/net-0.33.0"
	repoName  = "acme-org/api"
)

// spec returns the test run's spec under a preset.
func spec(preset fence.Preset) fence.Spec {
	return fence.Spec{
		Preset: preset, Worktree: worktree, GitDir: gitDir, CommonDir: commonDir, Repo: repoName, Number: 12,
		Branch: branch, Remote: "origin", Tools: []string{"go", "gofmt", "magex"}, RunDir: runDir, Home: home,
		DataDir: dataDir, ConfigDir: configDir, Env: map[string]string{},
	}
}

// build builds the test run's fence under a preset.
func build(t *testing.T, preset fence.Preset) fence.Fence {
	t.Helper()

	f, err := fence.Build(spec(preset))
	require.NoError(t, err)

	return f
}

// fakeFS is a file system of the paths it holds, with links from one folder to another.
type fakeFS struct {
	exists map[string]bool
	links  map[string]string
	files  map[string]string
}

// checked is a file system in which the sandbox check has run and held.
func checked(f fence.Fence) *fakeFS {
	fs := &fakeFS{exists: map[string]bool{}, links: map[string]string{}, files: map[string]string{}}
	if f.Policy.Canary != nil {
		fs.exists[f.Policy.Canary.Proof] = true
	}

	return fs
}

func (f *fakeFS) Exists(path string) bool { return f.exists[path] }

func (f *fakeFS) Resolve(path string) string {
	for from, to := range f.links {
		if path == from || strings.HasPrefix(path, from+"/") {
			return filepath.Join(to, strings.TrimPrefix(path, from))
		}
	}

	return filepath.Clean(path)
}

func (f *fakeFS) ReadHead(path string, _ int64) ([]byte, bool) {
	data, ok := f.files[path]

	return []byte(data), ok
}

// bash is a Bash call in the worktree.
func bash(cmd string) fence.Call {
	return fence.Call{Tool: "Bash", Command: cmd, Dir: worktree}
}

// tool is a file tool's call on a path.
func tool(name, path string) fence.Call {
	return fence.Call{Tool: name, Path: path, Dir: worktree}
}
