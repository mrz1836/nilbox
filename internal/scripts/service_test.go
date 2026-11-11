package scripts

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/integrations"
	"github.com/mrz1836/nilbox/internal/testutil"
	"github.com/mrz1836/nilbox/internal/uierr"
)

// thisRepo is a clone with this repository's own t3.json and tasks.json, and magefiles of
// its own.
func (r *rig) thisRepo() string {
	return r.clone("sdk", "acme-org/sdk", map[string]string{
		t3File: fixture(r.t, "t3.json"), tasksFile: fixture(r.t, "tasks.json"),
		"magefiles/main.go": magefile, "magefiles/types.go": magefileTypes,
	})
}

func TestListThisRepository(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	r.git.set("main")
	dir := r.thisRepo()

	p := r.list(dir)
	assert.Equal(t, "acme-org/sdk", p.Repo)
	assert.Equal(t, "acme-org/sdk", p.Name)
	assert.Equal(t, "~/projects/sdk", p.Dir)
	assert.Equal(t, "main", p.Branch)
	assert.Empty(t, p.FromBranch, "on the default branch")

	require.Len(t, p.Sections, 4)

	for i, want := range []struct {
		src   Source
		title string
		n     int
	}{{SourceYours, "Yours", 0}, {SourceT3, "From t3.json", 11}, {SourceTasks, "From .vscode/tasks.json", 38}} {
		sec := p.Sections[i]
		assert.Equal(t, want.src, sec.Source)
		assert.Equal(t, want.title, sec.Title)
		assert.Len(t, sec.Entries, want.n, want.title)
		assert.Empty(t, sec.Error)
	}

	for _, sec := range p.Sections[1:3] {
		assert.True(t, sec.Found)

		for _, e := range sec.Entries {
			assert.Equal(t, TrustAsk, e.Trust, "%s asks before its first run", e.Ref)
			assert.Len(t, e.SHA256, 64)
		}
	}

	magex := p.Sections[3]
	assert.Equal(t, SourceMagex, magex.Source)
	assert.True(t, magex.Found)
	assert.Contains(t, magex.Note, "run the repository's own code and its .github/env settings")
	assert.Equal(t, []string{
		"magex:format:fix", "magex:lint", "magex:lint:fix", "magex:test", "magex:test:race", "magex:build",
		"magex:deps:tidy", "magex:deps:update",
	}, magex.Common)

	assert.Equal(t, []string{"magex:deploy", "magex:pipeline:ci", "magex:pipeline:nightly", "magex:wait"},
		refs(magex.Entries[:4]), "the repository's own targets first")

	deploy := magex.Entries[0]
	assert.Equal(t, TrustAsk, deploy.Trust)
	assert.Equal(t, "This runs acme-org/sdk's own magefiles code.", deploy.TrustNote)

	builtin := magex.Entries[4]
	assert.Equal(t, "magex:bench", builtin.Ref)
	assert.Equal(t, TrustNone, builtin.Trust, "magex's own targets need no trust")
	assert.Empty(t, builtin.SHA256)

	dirs, argvs := r.help.calls()
	require.Len(t, dirs, 1)
	assert.True(t, strings.HasPrefix(dirs[0], r.scratch), "magex listed its targets in the app's own folder")
	assert.NotEqual(t, dir, dirs[0])
	assert.Equal(t, []string{"help:commands", "json=true"}, argvs[0][1:])
	assert.NoDirExists(t, dirs[0], "and the folder is gone")

	r.list(dir)
	dirs, _ = r.help.calls()
	assert.Len(t, dirs, 1, "the built-ins are kept per copy of magex")
	assert.Zero(t, r.recorded(), "listing ran nothing")

	r.magex("1.30.3", testutil.FakeRun{})
	r.list(dir)
	dirs, _ = r.help.calls()
	assert.Len(t, dirs, 2, "another copy lists its own")
}

func refs(entries []Entry) []string {
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		out = append(out, e.Ref)
	}

	return out
}

func TestListHonoursTheSwitches(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	dir := r.thisRepo()

	r.settings.sources(false, false)

	p := r.list(dir)
	for _, sec := range p.Sections[1:] {
		assert.True(t, sec.Off, sec.Title)
		assert.Empty(t, sec.Entries, sec.Title)
	}

	dirs, _ := r.help.calls()
	assert.Empty(t, dirs, "magex wasn't asked")

	_, err := r.svc.Run(t.Context(), RunRequest{Dir: r.shown(dir), Ref: "t3:Test"})
	require.ErrorIs(t, err, ErrCantRun)
	assert.Equal(t, "Commands from repository files are turned off in Settings → Integrations.", uierr.Message(err))

	r.settings.sources(true, false)
	p = r.list(dir)
	assert.True(t, p.Sections[1].Off)
	assert.False(t, p.Sections[3].Off)
	assert.NotEmpty(t, p.Sections[3].Entries)
}

func TestListWithoutMagex(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	dir := r.thisRepo()
	r.tools.set(ToolFacts{State: integrations.StateMissing})
	r.tools.on(false, true) // go-pre-commit keeps the scripts on

	magex := r.list(dir).Sections[3]
	assert.False(t, magex.Found)
	assert.Equal(t, "magex isn't installed. Install it in Settings → Integrations.", magex.Note)
	assert.Empty(t, magex.Entries)

	_, err := r.svc.Run(t.Context(), RunRequest{Dir: r.shown(dir), Ref: "magex:test"})
	require.ErrorIs(t, err, ErrCantRun)
}

func TestListSaysWhatItCantRead(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	dir := r.clone("broken", "", map[string]string{t3File: "{", tasksFile: `{"tasks": [}`})

	p := r.list(dir)
	assert.Empty(t, p.Repo, "an origin not on GitHub")
	assert.Equal(t, "broken", p.Name)
	assert.True(t, p.Sections[1].Found)
	assert.Contains(t, p.Sections[1].Error, "t3.json isn't valid JSON")
	assert.Contains(t, p.Sections[2].Error, ".vscode/tasks.json isn't valid JSON")

	empty := r.clone("empty", "", nil)
	p = r.list(empty)
	assert.False(t, p.Sections[1].Found)
	assert.False(t, p.Sections[2].Found)
	assert.Empty(t, p.Sections[1].Error)
}

func TestListNamesAnotherBranch(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	sdk := r.thisRepo()
	other := r.clone("other", "acme-org/other", nil)

	r.git.set("feature-x")
	assert.Equal(t, "feature-x", r.list(sdk).FromBranch, "not the default branch the app stored")
	assert.Equal(t, "feature-x", r.list(other).FromBranch, "no default stored: neither main nor master")

	r.git.set("master")
	assert.Equal(t, "master", r.list(sdk).FromBranch, "the stored default is main")
	assert.Empty(t, r.list(other).FromBranch)

	r.git.set("")
	assert.Empty(t, r.list(sdk).FromBranch, "a detached HEAD")
}

func TestListRefusesAFolderOutsideTheCloneRoot(t *testing.T) {
	t.Parallel()

	r := newRig(t)

	_, err := r.svc.List(t.Context(), t.TempDir(), false)
	require.ErrorIs(t, err, ErrNoClone)
	assert.Contains(t, uierr.Message(err), "isn't a clone in ~/projects.")
}

func TestRepos(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	r.clone("beta", "", nil)
	r.clone("Alpha", "", nil)
	gamma := r.clone("gamma", "", map[string]string{t3File: `{"scripts": [{"name": "Hi", "command": "echo hi"}]}`})

	got, err := r.svc.Repos(t.Context())
	require.NoError(t, err)
	assert.Equal(t, []Clone{
		{Name: "Alpha", Dir: "~/projects/Alpha"}, {Name: "beta", Dir: "~/projects/beta"}, {Name: "gamma", Dir: "~/projects/gamma"},
	}, got, "by name")

	e := r.entry(gamma, "t3:Hi")
	id, err := r.svc.Run(t.Context(), RunRequest{Dir: r.shown(gamma), Ref: e.Ref, Confirmed: e.SHA256})
	require.NoError(t, err)
	r.ended(id)

	got, err = r.svc.Repos(t.Context())
	require.NoError(t, err)
	assert.Equal(t, []string{"gamma", "Alpha", "beta"}, []string{got[0].Name, got[1].Name, got[2].Name},
		"the clone a script ran in most recently first")
}

func TestTrustRecordsAreKeptPerRepository(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	text := `{"scripts": [{"name": "Test", "command": "magex test"}]}`
	one := r.clone("one", "acme-org/one", map[string]string{t3File: text})
	two := r.clone("two", "acme-org/two", map[string]string{t3File: text})

	e := r.entry(one, "t3:Test")
	id, err := r.svc.Run(t.Context(), RunRequest{Dir: r.shown(one), Ref: e.Ref, Confirmed: e.SHA256, Remember: true})
	require.NoError(t, err)
	r.ended(id)

	assert.Equal(t, TrustTrusted, r.entry(one, "t3:Test").Trust)
	assert.Equal(t, TrustAsk, r.entry(two, "t3:Test").Trust, "trusting it in one repository trusts nothing in another")

	moved := filepath.Join(r.root, "renamed")
	r.clone("renamed", "acme-org/one", map[string]string{t3File: text})
	assert.Equal(t, TrustTrusted, r.entry(moved, "t3:Test").Trust, "trust follows the repository, not the folder")
}
