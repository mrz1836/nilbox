//go:build mage

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/brand"
	"github.com/mrz1836/nilbox/internal/brandgen"
	"github.com/mrz1836/nilbox/internal/testutil"
)

// ranHere is the file a fake tool in these tests writes in the folder it runs in, so a
// test sees where a target ran it.
const ranHere = "ran-here"

// inWorkingDir makes a fresh folder the process's working directory, as magex runs a
// target from the repository's root, and gives the process a PATH that holds only tools,
// each a fake (testutil.FakeTool) that plays its run, its script named in the process's
// environment. It returns the folder. A target does its work with hostRunner, the
// process's own runner, so a test that runs one changes the process's working directory
// and environment, and runs alone.
func inWorkingDir(t *testing.T, tools map[string]testutil.FakeRun) string {
	t.Helper()

	dir := t.TempDir()
	t.Chdir(dir)

	path := make([]string, 0, len(tools))

	for name, run := range tools {
		link, entry := testutil.FakeTool(t, name, run)

		key, value, _ := strings.Cut(entry, "=")
		t.Setenv(key, value)

		path = append(path, filepath.Dir(link))
	}

	t.Setenv("PATH", strings.Join(path, string(os.PathListSeparator)))

	return dir
}

// writesRanHere is a fake tool's run that writes ranHere where it runs, after it checks
// it was started with args, when there are any.
func writesRanHere(args ...string) testutil.FakeRun {
	return testutil.FakeRun{WantArgs: args, Files: []testutil.FakeFile{{Path: ranHere}}}
}

// The targets check the system and read their arguments, then hand their work to their
// steps (tested on a rig of their own) with the process's runner: the working directory,
// the environment, and the tools on PATH, as magex runs them.
//
//nolint:paralleltest // the targets work in the process's working directory, with its PATH and environment
func TestTheTargetsWorkInTheWorkingDirectory(t *testing.T) {
	// sqlcCheckout is a working directory whose project env file pins fakeSQLCVersion, with
	// go as tools has it; the environment pins nothing.
	sqlcCheckout := func(t *testing.T, golang testutil.FakeRun) string {
		t.Helper()

		dir := inWorkingDir(t, map[string]testutil.FakeRun{"go": golang})
		t.Setenv(sqlcVersionKey, "")
		testutil.WriteFile(t, filepath.Join(dir, projectEnvFile), pinnedEnv)

		return dir
	}

	// frontendCheckout is a working directory with the frontend's folder, and pnpm as
	// tools has it.
	frontendCheckout := func(t *testing.T, pnpm testutil.FakeRun) string {
		t.Helper()

		dir := inWorkingDir(t, map[string]testutil.FakeRun{"pnpm": pnpm})
		mkdir(t, filepath.Join(dir, frontendDir))

		return dir
	}

	t.Run("db:generate runs sqlc generate at the checkout's pin", func(t *testing.T) {
		dir := sqlcCheckout(t, writesRanHere("run", sqlcCommand+"@"+fakeSQLCVersion, "generate"))

		require.NoError(t, DB{}.Generate())

		assert.FileExists(t, filepath.Join(dir, ranHere), "sqlc runs in the checkout's root")
	})

	t.Run("db:check runs sqlc at the checkout's pin", func(t *testing.T) {
		dir := sqlcCheckout(t, writesRanHere())

		require.NoError(t, DB{}.Check())

		assert.FileExists(t, filepath.Join(dir, ranHere), "sqlc runs in the checkout's root")
	})

	t.Run("frontend:install installs from the checkout's lockfile", func(t *testing.T) {
		dir := frontendCheckout(t, writesRanHere("install", "--frozen-lockfile"))

		require.NoError(t, Frontend{}.Install())

		assert.FileExists(t, filepath.Join(dir, frontendDir, ranHere), "pnpm runs in the frontend's folder")
	})

	t.Run("frontend:build builds the checkout's frontend", func(t *testing.T) {
		dir := frontendCheckout(t, writesRanHere())

		require.NoError(t, Frontend{}.Build())

		assert.FileExists(t, filepath.Join(dir, frontendDir, ranHere), "pnpm runs in the frontend's folder")
	})

	t.Run("frontend:gate runs the checkout's frontend checks", func(t *testing.T) {
		dir := frontendCheckout(t, writesRanHere())

		require.NoError(t, Frontend{}.Gate())

		assert.FileExists(t, filepath.Join(dir, frontendDir, ranHere), "pnpm runs in the frontend's folder")
	})

	t.Run("brand:generate writes the brand files into the checkout", func(t *testing.T) {
		dir := inWorkingDir(t, nil)

		require.NoError(t, Brand{}.Generate())

		require.NoError(t, brandgen.Check(dir), "every generated file is written and current")
	})

	t.Run("brand:icons generates the brand files and renders every icon into the checkout", func(t *testing.T) {
		onlyOnMacOS(t)

		// The fakes write the icons the checkout keeps, wherever a call names its output.
		renders := testutil.FakeRun{Files: []testutil.FakeFile{{Path: brandgen.DevIconPath}, {Path: brandgen.TrayIconPath}}}
		iconSet := testutil.FakeRun{Files: []testutil.FakeFile{{Path: brandgen.IconSetPath}}}

		dir := inWorkingDir(t, map[string]testutil.FakeRun{svgRenderer: renders, "iconutil": iconSet})
		mkdir(t, filepath.Join(dir, "cmd", brand.Slug))

		require.NoError(t, Brand{}.Icons())

		r, _ := realRunner(t, dir, nil)
		require.NoError(t, r.checkBrandFiles(), "the brand files and every icon are current")
	})
}
