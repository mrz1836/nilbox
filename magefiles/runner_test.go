//go:build mage

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/magefile/mage/mg"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/testutil"
)

func TestHostRunnerIsTheProcesss(t *testing.T) {
	t.Parallel()

	r := hostRunner()

	assert.Equal(t, ".", r.root(), "the working directory, the repository's root as magex runs a target")
	assert.Equal(t, filepath.FromSlash("bin/release"), r.path("bin/release"), "a path reads as the working directory reads it")
	assert.Empty(t, r.tmp, "scratch folders go in the system's temporary folder")
	assert.Equal(t, os.Stdout, r.stdout)
	assert.Equal(t, os.Stderr, r.stderr)
	assert.Equal(t, os.Stdin, r.stdin)
	assert.Equal(t, os.Getenv("PATH"), r.getenv("PATH"))

	wd, err := os.Getwd()
	require.NoError(t, err)

	got, err := r.workDir()
	require.NoError(t, err)
	assert.Equal(t, wd, got)

	want, wantErr := exec.LookPath("sh")
	found, foundErr := r.lookPath("sh")
	assert.Equal(t, want, found, "tools are found on PATH")
	assert.Equal(t, wantErr, foundErr)

	cmd := r.cmd(t.Context(), nil, "sh", "-c", "exit 0")
	assert.Equal(t, want, cmd.Path)
	assert.Empty(t, cmd.Dir, "the tools run in the working directory")
	assert.Nil(t, cmd.Env, "with the process's environment")
}

func TestRunnerPath(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	r := runner{dir: dir}

	assert.Equal(t, filepath.Join(dir, "bin", "release"), r.path("bin/release"))
	assert.Equal(t, filepath.Join(dir, "frontend"), r.path(frontendDir))
	assert.Equal(t, "/abs/elsewhere", r.path("/abs/elsewhere"), "an absolute path is its own")
	assert.Equal(t, dir, r.root())

	got, err := r.workDir()
	require.NoError(t, err)
	assert.Equal(t, dir, got)
}

func TestRunnerRunsTools(t *testing.T) {
	t.Parallel()

	t.Run("passes the arguments as they are and adds the step's environment", func(t *testing.T) {
		t.Parallel()

		g := newRig(t)
		g.fixed("go", testutil.FakeRun{ReportEnv: []string{"GOARCH"}})

		require.NoError(t, g.runner().runWithV([]string{"GOARCH=amd64"}, "go", "build", "$HOME", "a b"))

		assert.Equal(t, []toolCall{{name: "go", args: []string{"build", "$HOME", "a b"}}}, g.toolCalls(),
			"nothing in the arguments is expanded or split")
		assert.Equal(t, "GOARCH=amd64\n", g.out.String())
	})

	t.Run("a tool that fails exits with its code", func(t *testing.T) {
		t.Parallel()

		g := newRig(t)
		g.fixed("go", testutil.FakeRun{Stderr: "build failed\n", Exit: 3})

		err := g.runner().runV("go", "build", "./...")

		require.EqualError(t, err, `running "go build ./..." failed with exit code 3`)
		assert.Equal(t, 3, mg.ExitStatus(err), "mage exits with the tool's code")
		assert.Equal(t, "build failed\n", g.errs.String(), "the tool's errors reach the runner's")
	})

	t.Run("a tool that isn't there could not run", func(t *testing.T) {
		t.Parallel()

		err := newRig(t).runner().run("go", "build")

		require.ErrorIs(t, err, exec.ErrNotFound)
		assert.Contains(t, err.Error(), `failed to run "go build: `)
	})

	t.Run("output drops the last newline alone", func(t *testing.T) {
		t.Parallel()

		g := newRig(t)
		g.fixed("go", testutil.FakeRun{Stdout: "v1\n\n"})

		out, err := g.runner().output("go", "list")

		require.NoError(t, err)
		assert.Equal(t, "v1\n", out)
	})

	t.Run("a quiet run shows nothing of the tool's output", func(t *testing.T) {
		t.Parallel()

		if mg.Verbose() {
			t.Skip("mage runs verbose, which shows a quiet run's output")
		}

		g := newRig(t)
		g.fixed("plutil", testutil.FakeRun{Stdout: "chatter\n"})

		require.NoError(t, g.runner().run("plutil", "-lint"))

		assert.Empty(t, g.out.String(), "mage is not verbose in the tests")
	})
}
