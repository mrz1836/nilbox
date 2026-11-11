package scripts

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/brand"
	"github.com/mrz1836/nilbox/internal/integrationprefs"
	"github.com/mrz1836/nilbox/internal/repo"
	"github.com/mrz1836/nilbox/internal/scriptrun"
	"github.com/mrz1836/nilbox/internal/uierr"
)

// The names of the user's scripts the tests save.
const (
	fixAllName = "Fix all"
	testName   = "Test"
	firstName  = "First"
)

// fixAll is a script of the user's for every clone.
func fixAll() UserScript {
	return UserScript{Name: fixAllName, Command: "magex format:fix && magex lint", Icon: IconPlay, Scope: ScopeAll}
}

// save saves u and returns it as saved.
func (r *rig) save(u UserScript) UserScript {
	r.t.Helper()

	saved, err := r.svc.Save(r.t.Context(), u)
	require.NoError(r.t, err)

	return saved
}

// order returns the names of the user's scripts, in their order.
func (r *rig) order() []string {
	r.t.Helper()

	all, err := r.svc.yours.all(r.t.Context())
	require.NoError(r.t, err)

	out := make([]string, 0, len(all))
	for _, u := range all {
		out = append(out, u.Name)
	}

	return out
}

// yours returns the names of the user's scripts the page lists for the clone at dir.
func (r *rig) yours(dir string) []string {
	r.t.Helper()

	sec := r.list(dir).Sections[0]
	require.Equal(r.t, SourceYours, sec.Source)

	out := make([]string, 0, len(sec.Entries))
	for _, e := range sec.Entries {
		out = append(out, e.Name)
	}

	return out
}

// named returns a script called name for every clone.
func named(name string) UserScript {
	return UserScript{Name: name, Command: "echo " + name, Icon: IconPlay, Scope: ScopeAll}
}

// specs is a Runs that keeps each spec it is handed and starts nothing.
type specs struct {
	mu   sync.Mutex
	got  []scriptrun.Spec
	next int64
}

func (s *specs) Start(_ context.Context, spec scriptrun.Spec) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.got = append(s.got, spec)
	s.next++

	return s.next, nil
}

func (s *specs) last() scriptrun.Spec {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.got[len(s.got)-1]
}

func TestSaveCreatesAndEdits(t *testing.T) {
	t.Parallel()

	r := newRig(t)

	saved := r.save(UserScript{
		Name: "  Fix all ", Command: " magex format:fix  ", Icon: IconLint, Scope: ScopeGo,
		Repos: []string{"acme-org/sdk"}, RunsIn: integrationprefs.RunsInTerminal, Confirm: true,
	})
	assert.Positive(t, saved.ID)
	assert.Equal(t, UserScript{
		ID: saved.ID, Name: fixAllName, Command: "magex format:fix", Icon: IconLint, Scope: ScopeGo, Repos: []string{},
		RunsIn: integrationprefs.RunsInTerminal, Confirm: true,
	}, saved, "trimmed, and a Go scope keeps no repositories")

	got, ok, err := r.svc.yours.get(t.Context(), saved.ID)
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, saved, got, "what Save returns is what it stored")

	saved.Name, saved.Confirm, saved.RunsIn = "Format", false, ""
	edited := r.save(saved)
	assert.Equal(t, saved, edited)

	got, _, err = r.svc.yours.get(t.Context(), saved.ID)
	require.NoError(t, err)
	assert.Equal(t, edited, got)
	assert.Equal(t, []string{"Format"}, r.order(), "an edit adds nothing")

	_, err = r.svc.Save(t.Context(), UserScript{ID: 99, Name: "x", Command: "x", Icon: IconPlay, Scope: ScopeAll})
	require.ErrorIs(t, err, ErrNotFound)
	assert.Equal(t, "The script was deleted.", uierr.Message(err))
}

func TestSaveRefusesAScriptThatIsntOne(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	long := strings.Repeat("x", maxNameRunes+1)

	for _, tt := range []struct {
		change func(*UserScript)
		want   string
	}{
		{func(u *UserScript) { u.Name = " " }, "Name the script."},
		{func(u *UserScript) { u.Name = long }, "Keep the name to 100 characters."},
		{func(u *UserScript) { u.Name = "Fix\nall" }, "The name holds a character that can't be shown, such as a line break."},
		{func(u *UserScript) { u.Command = "\t" }, "Write the command it runs."},
		{func(u *UserScript) { u.Command = strings.Repeat("x", maxCommandBytes+1) }, "Keep the command under 8 KB."},
		{
			func(u *UserScript) { u.Command = "magex lint\nrm -rf ~" },
			"The command holds a character that hides what it runs, such as a line break or a text direction mark. " +
				"Write it on one line.",
		},
		{func(u *UserScript) { u.Command = "echo \u202eevil" }, ""},
		{func(u *UserScript) { u.Scope = "" }, "Choose the repositories it shows in."},
		{func(u *UserScript) { u.Scope = "everywhere" }, "Choose the repositories it shows in."},
		{func(u *UserScript) { u.RunsIn = "cloud" }, "Choose where it runs."},
		{func(u *UserScript) { u.Icon = "" }, "Choose its icon."},
		{func(u *UserScript) { u.Icon = "rocket" }, "Choose its icon."},
		{func(u *UserScript) { u.Scope, u.Repos = ScopeRepos, nil }, "Choose a repository for it to show in."},
		{
			func(u *UserScript) { u.Scope, u.Repos = ScopeRepos, []string{"acme-org/sdk", "not a repo"} },
			`"not a repo" isn't a repository: write it as owner/name.`,
		},
		{func(u *UserScript) { u.Scope, u.Repos = ScopeRepos, []string{"../etc"} }, ""},
	} {
		u := fixAll()
		tt.change(&u)

		_, err := r.svc.Save(t.Context(), u)
		require.ErrorIs(t, err, ErrInvalidScript, "%+v", u)

		if tt.want != "" {
			assert.Equal(t, tt.want, uierr.Message(err))
		}
	}

	assert.Empty(t, r.order(), "nothing was saved")

	wide := strings.Repeat("é", maxNameRunes)
	assert.Equal(t, wide, r.save(UserScript{Name: wide, Command: "x", Icon: IconPlay, Scope: ScopeAll}).Name,
		"the limit counts characters, not bytes")
}

func TestSaveReadsTheChosenRepositories(t *testing.T) {
	t.Parallel()

	r := newRig(t)

	u := fixAll()
	u.Scope, u.Repos = ScopeRepos, []string{" acme-org/web", "Acme-Org/SDK", "acme-org/sdk", "acme-org/Api"}

	assert.Equal(t, []string{"acme-org/Api", "Acme-Org/SDK", "acme-org/web"}, r.save(u).Repos,
		"each once, in any case, the first casing kept, sorted")

	u.Scope = ScopeAll
	assert.Equal(t, []string{}, r.save(u).Repos, "only a script for chosen repositories keeps them")
}

func TestMoveAtBothEnds(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	first, second, third := r.save(named(firstName)), r.save(named("Second")), r.save(named("Third"))
	ctx := t.Context()

	require.Equal(t, []string{firstName, "Second", "Third"}, r.order(), "each new one at the end")

	require.NoError(t, r.svc.Move(ctx, first.ID, MoveUp, ""))
	assert.Equal(t, []string{firstName, "Second", "Third"}, r.order(), "the first stays first")

	require.NoError(t, r.svc.Move(ctx, third.ID, MoveDown, ""))
	assert.Equal(t, []string{firstName, "Second", "Third"}, r.order(), "the last stays last")

	require.NoError(t, r.svc.Move(ctx, first.ID, MoveDown, ""))
	assert.Equal(t, []string{"Second", firstName, "Third"}, r.order())

	require.NoError(t, r.svc.Move(ctx, third.ID, MoveUp, ""))
	assert.Equal(t, []string{"Second", "Third", firstName}, r.order())

	require.NoError(t, r.svc.Move(ctx, second.ID, MoveDown, ""))
	require.NoError(t, r.svc.Move(ctx, second.ID, MoveDown, ""))
	assert.Equal(t, []string{"Third", firstName, "Second"}, r.order())

	rows, err := r.svc.yours.q.ListUserScripts(ctx)
	require.NoError(t, err)

	for i, row := range rows {
		assert.Equal(t, int64(i), row.Position, "positions numbered 0, 1, 2")
	}

	err = r.svc.Move(ctx, 99, MoveUp, "")
	require.ErrorIs(t, err, ErrNotFound)

	require.ErrorIs(t, r.svc.Move(ctx, first.ID, "sideways", ""), ErrInvalidRequest)
	require.ErrorIs(t, r.svc.Move(ctx, first.ID, MoveUp, "~/projects/nowhere"), ErrNoClone)
}

func TestMoveInAClonePassesTheScriptsShownThere(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	sdk := r.clone("sdk", "acme-org/sdk", map[string]string{"go.mod": "module example.com/sdk\n"})
	web := r.clone("web", "acme-org/web", nil)

	fix := r.save(named(fixAllName))

	deploy := named("Deploy")
	deploy.Scope, deploy.Repos = ScopeRepos, []string{"acme-org/web"}
	r.save(deploy)

	test := named(testName)
	test.Scope = ScopeGo
	test = r.save(test)

	require.Equal(t, []string{fixAllName, testName}, r.yours(sdk))
	require.Equal(t, []string{fixAllName, "Deploy"}, r.yours(web))

	require.NoError(t, r.svc.Move(t.Context(), test.ID, MoveUp, r.shown(sdk)))
	assert.Equal(t, []string{testName, fixAllName}, r.yours(sdk), "one move shows on the page it was made on")
	assert.Equal(t, []string{testName, fixAllName, "Deploy"}, r.order())

	require.NoError(t, r.svc.Move(t.Context(), fix.ID, MoveDown, r.shown(sdk)))
	assert.Equal(t, []string{testName, fixAllName, "Deploy"}, r.order(), "Fix all is last in sdk already")

	require.NoError(t, r.svc.Move(t.Context(), fix.ID, MoveDown, r.shown(web)))
	assert.Equal(t, []string{"Deploy", fixAllName}, r.yours(web))
	assert.Equal(t, []string{testName, "Deploy", fixAllName}, r.order())
}

func TestDuplicatePlacesTheCopyRightAfter(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	first := named(firstName)
	first.Scope, first.Repos, first.RunsIn, first.Confirm, first.Icon = ScopeRepos, []string{"acme-org/sdk"},
		integrationprefs.RunsInTerminal, true, IconBuild
	first = r.save(first)
	r.save(named("Second"))

	made, err := r.svc.Duplicate(t.Context(), first.ID)
	require.NoError(t, err)

	want := first
	want.ID, want.Name = made.ID, "First copy"
	assert.Equal(t, want, made, "everything but its name and ID as the original's")
	assert.NotEqual(t, first.ID, made.ID)
	assert.Equal(t, []string{firstName, "First copy", "Second"}, r.order())

	got, _, err := r.svc.yours.get(t.Context(), made.ID)
	require.NoError(t, err)
	assert.Equal(t, made, got)

	long := r.save(named(strings.Repeat("y", maxNameRunes)))
	made, err = r.svc.Duplicate(t.Context(), long.ID)
	require.NoError(t, err)
	assert.Equal(t, strings.Repeat("y", maxNameRunes-5)+" copy", made.Name, "cut short to fit")
	assert.Equal(t, []string{firstName, "First copy", "Second", long.Name, made.Name}, r.order())

	_, err = r.svc.Duplicate(t.Context(), 99)
	require.ErrorIs(t, err, ErrNotFound)
}

func TestDelete(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	r.save(named(firstName))
	second := r.save(named("Second"))
	r.save(named("Third"))

	require.NoError(t, r.svc.Delete(t.Context(), second.ID))
	assert.Equal(t, []string{firstName, "Third"}, r.order())

	rows, err := r.svc.yours.q.ListUserScripts(t.Context())
	require.NoError(t, err)
	assert.Equal(t, int64(1), rows[1].Position, "the rest numbered again")

	err = r.svc.Delete(t.Context(), second.ID)
	require.ErrorIs(t, err, ErrNotFound)
	assert.Equal(t, "The script was deleted.", uierr.Message(err))
}

func TestTheYoursSectionByScope(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	sdk := r.clone("sdk", "acme-org/sdk", map[string]string{"go.mod": "module example.com/sdk\n"})
	web := r.clone("web", "acme-org/web", map[string]string{"go.mod/x": "a folder called go.mod"})
	notes := r.clone("notes", "", map[string]string{"go.mod": "module example.com/notes\n"})

	empty := r.list(sdk).Sections[0]
	assert.Equal(t, Section{
		Source: SourceYours, Title: "Yours", Entries: []Entry{}, Common: []string{},
	}, empty, "no scripts of the user's yet")

	all := r.save(fixAll())

	goOnly := named(testName)
	goOnly.Scope = ScopeGo
	r.save(goOnly)

	chosen := named("Deploy")
	chosen.Scope, chosen.Repos = ScopeRepos, []string{"ACME-ORG/SDK"}
	r.save(chosen)

	assert.Equal(t, []string{fixAllName, testName, "Deploy"}, r.yours(sdk), "a Go repository, and chosen in any case")
	assert.Equal(t, []string{fixAllName}, r.yours(web), "no go.mod file, and not chosen")
	assert.Equal(t, []string{fixAllName, testName}, r.yours(notes), "a Go folder that isn't on GitHub")

	e := r.list(sdk).Sections[0].Entries[0]
	mine := all
	assert.Equal(t, Entry{
		Ref: yoursRef(all.ID), Source: SourceYours, Name: fixAllName, Command: "magex format:fix && magex lint",
		Icon: "play", Runnable: true, Trust: TrustNone, Inputs: []Input{}, Mine: &mine,
	}, e, "the user's own script needs no trust")

	r.settings.scripts(integrationprefs.SwitchOn)
	r.settings.sources(false, false)
	assert.Equal(t, []string{fixAllName, testName, "Deploy"}, r.yours(sdk), "no switch turns the user's own off")

	sec := r.list(web).Sections[0]
	assert.True(t, sec.Found, "the user has scripts")

	none := r.clone("api", "acme-org/api", nil)
	require.NoError(t, r.svc.Delete(t.Context(), all.ID))

	sec = r.list(none).Sections[0]
	assert.Empty(t, sec.Entries)
	assert.True(t, sec.Found, "scripts that show elsewhere")
}

func TestAUserScriptRunsAsWrittenWithTheVariables(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	r.git.set("main")
	dir := r.clone("sdk", "acme-org/sdk", nil)
	command := `echo "${workspaceFolder}" '$` + Variables()[1] + `' && magex lint`
	u := r.save(UserScript{Name: "Echo", Command: command, Icon: IconTest, Scope: ScopeAll})

	id, err := r.svc.Run(t.Context(), RunRequest{Dir: r.shown(dir), Ref: yoursRef(u.ID)})
	require.NoError(t, err, "it needs no trust")

	run := r.ended(id)
	assert.Equal(t, scriptrun.StatusSucceeded, run.Status)
	assert.Equal(t, []string{r.shell, "-c", command}, run.Argv, "exactly as written, nothing filled in")
	assert.Equal(t, command, run.Command)
	assert.Equal(t, "Echo", run.Title)
	assert.Equal(t, "your scripts", run.Source)
	assert.Equal(t, integrationprefs.RunsInApp, run.RunsIn)

	got := &specs{}
	svc := r.service(got)

	_, err = svc.Run(t.Context(), RunRequest{Dir: r.shown(dir), Ref: yoursRef(u.ID)})
	require.NoError(t, err)

	prefix := strings.ToUpper(brand.Slug)
	assert.Equal(t, []string{prefix + "_REPO", prefix + "_REPO_DIR", prefix + "_DEFAULT_BRANCH"}, Variables())

	env := got.last().Env
	assert.Contains(t, env, prefix+"_REPO=acme-org/sdk")
	assert.Contains(t, env, prefix+"_REPO_DIR="+dir)
	assert.Contains(t, env, prefix+"_DEFAULT_BRANCH=main", "the default branch the app stored")
	assert.Contains(t, env, "HOME="+r.home, "with the login shell's environment")

	_, err = svc.Run(t.Context(), RunRequest{Dir: r.shown(dir), Ref: yoursRef(u.ID), RunsIn: integrationprefs.RunsInTerminal})
	require.NoError(t, err)
	assert.Equal(t, []string{prefix + "_REPO=acme-org/sdk", prefix + "_REPO_DIR=" + dir, prefix + "_DEFAULT_BRANCH=main"},
		got.last().Env, "in the terminal, only what the script exports")
}

func TestTheDefaultBranchVariable(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	web := r.clone("web", "acme-org/web", nil)
	notes := r.clone("notes", "", nil)
	u := r.save(fixAll())
	got := &specs{}
	svc := r.service(got)
	name := Variables()[2] + "="

	branch := func(dir string) string {
		_, err := svc.Run(t.Context(), RunRequest{Dir: r.shown(dir), Ref: yoursRef(u.ID)})
		require.NoError(t, err)

		for _, kv := range got.last().Env {
			if v, ok := strings.CutPrefix(kv, name); ok {
				return v
			}
		}

		return "unset"
	}

	r.git.set("master")
	assert.Equal(t, "master", branch(web), "none stored: the branch the clone is on, when it is main or master")

	r.git.set("feature-x")
	assert.Empty(t, branch(web), "can't tell")
	assert.Empty(t, branch(notes))

	r.git.setHead("trunk", nil)
	assert.Equal(t, "trunk", branch(web), "none stored: the one the clone's origin HEAD names, on any branch")

	r.git.setHead("", os.ErrPermission)
	assert.Empty(t, branch(web), "an origin HEAD that can't be read")

	r.git.setHead("", nil)
	r.git.set("main")
	assert.Equal(t, "main", branch(notes))

	repoName := Variables()[0] + "="
	assert.Contains(t, got.last().Env, repoName, "a clone whose origin isn't on GitHub has no repository")

	sdk := r.clone("sdk", "acme-org/sdk", nil)
	r.git.setHead("trunk", nil)
	assert.Equal(t, "main", branch(sdk), "the one the app stored comes first")

	none := NewService(Config{Clones: r.clones, Home: r.home})
	assert.Empty(t, none.defaultBranch(t.Context(), clone{dir: web}), "no git to read")

	unread := NewService(Config{Clones: r.clones, Branches: failingBranches{}, Home: r.home})
	assert.Empty(t, unread.storedDefault(t.Context(), clone{dir: web, name: repo.Name{Owner: "acme-org", Repo: "web"}}),
		"a stored branch that can't be read")
}

// failingBranches can't read the default branches the app stored.
type failingBranches struct{}

func (failingBranches) DefaultBranch(context.Context, repo.Name) (string, error) {
	return "", os.ErrPermission
}

func TestAScriptsRunsInOverridesTheSetting(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	dir := r.clone("sdk", "acme-org/sdk", nil)
	got := &specs{}
	svc := r.service(got)

	ownPlace := fixAll()
	ownPlace.RunsIn = integrationprefs.RunsInTerminal
	ownPlace = r.save(ownPlace)
	setting := r.save(named("Setting"))

	place := func(u UserScript, asked integrationprefs.RunsIn) integrationprefs.RunsIn {
		_, err := svc.Run(t.Context(), RunRequest{Dir: r.shown(dir), Ref: yoursRef(u.ID), RunsIn: asked})
		require.NoError(t, err)

		return got.last().RunsIn
	}

	assert.Equal(t, integrationprefs.RunsInTerminal, place(ownPlace, ""), "the script's own place, over the setting")
	assert.Equal(t, integrationprefs.RunsInApp, place(ownPlace, integrationprefs.RunsInApp), "run in the other place")
	assert.Equal(t, integrationprefs.RunsInApp, place(setting, ""), "the setting")

	r.settings.mu.Lock()
	r.settings.cfg.RunsIn = integrationprefs.RunsInTerminal
	r.settings.mu.Unlock()

	assert.Equal(t, integrationprefs.RunsInTerminal, place(setting, ""))

	ownPlace.RunsIn = integrationprefs.RunsInApp
	r.save(ownPlace)
	assert.Equal(t, integrationprefs.RunsInApp, place(ownPlace, ""), "the script's own place, over the setting")

	id, err := r.svc.Run(t.Context(), RunRequest{Dir: r.shown(dir), Ref: yoursRef(ownPlace.ID)})
	require.NoError(t, err)
	assert.Equal(t, integrationprefs.RunsInApp, r.ended(id).RunsIn)
}

func TestRunAgainOfAUserScript(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	dir := r.clone("sdk", "acme-org/sdk", nil)
	u := r.save(fixAll())

	id, err := r.svc.Run(t.Context(), RunRequest{Dir: r.shown(dir), Ref: yoursRef(u.ID)})
	require.NoError(t, err)
	r.ended(id)

	u.Command = "magex test"
	r.save(u)

	again, err := r.runs.RunAgain(t.Context(), id)
	require.NoError(t, err)
	assert.Equal(t, []string{r.shell, "-c", "magex test"}, r.ended(again).Argv, "as it is saved now")

	u.Scope, u.Repos = ScopeRepos, []string{"acme-org/web"}
	r.save(u)

	_, err = r.runs.RunAgain(t.Context(), id)
	require.ErrorIs(t, err, ErrNotFound)
	assert.Equal(t, `"Fix all" doesn't show in acme-org/sdk. Edit it to choose the repositories it shows in.`,
		uierr.Message(err))

	require.NoError(t, r.svc.Delete(t.Context(), u.ID))

	_, err = r.runs.RunAgain(t.Context(), id)
	require.ErrorIs(t, err, ErrNotFound, "a deleted script doesn't run again")
	assert.Equal(t, "The script was deleted.", uierr.Message(err))
	assert.Equal(t, 2, r.recorded())
}

func TestAUserScriptThatBumpsAVersionPointsToRelease(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	dir := r.clone("sdk", "acme-org/sdk", nil)
	u := named("Release")
	u.Command = "magex version:bump bump=minor push"
	u = r.save(u)

	e := r.list(dir).Sections[0].Entries[0]
	assert.True(t, e.Release, "a command naming version:bump")

	_, err := r.svc.Run(t.Context(), RunRequest{Dir: r.shown(dir), Ref: e.Ref})
	require.ErrorIs(t, err, ErrRelease)
	assert.Equal(t, "Release does this, with its checks. Open Release, or run it anyway.", uierr.Message(err))

	id, err := r.svc.Run(t.Context(), RunRequest{Dir: r.shown(dir), Ref: e.Ref, Anyway: true})
	require.NoError(t, err, "run it anyway")
	assert.Equal(t, []string{r.shell, "-c", u.Command}, r.ended(id).Argv)

	alias := named("Bump")
	alias.Command = "magex bumpit"
	alias = r.save(alias)

	r.help.answer(`{"commands": [{"name": "test"}, {"name": "version:bump", "aliases": ["bumpit"]}]}`, nil)
	r.svc.forgetBuiltins(r.tools.magex())

	_, err = r.svc.Run(t.Context(), RunRequest{Dir: r.shown(dir), Ref: yoursRef(alias.ID)})
	require.ErrorIs(t, err, ErrRelease, "an alias of a tagging target, caught on the command as it runs")
}

func TestUserScriptsThatCantRun(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	dir := r.clone("sdk", "acme-org/sdk", nil)
	u := r.save(fixAll())

	_, err := r.svc.yours.db.ExecContext(t.Context(), `UPDATE user_scripts SET command = 'magex lint'||char(10)||'rm -rf ~'`)
	require.NoError(t, err)

	e := r.list(dir).Sections[0].Entries[0]
	assert.False(t, e.Runnable, "a line break edited into the database")
	assert.Equal(t, hiddenWhy, e.Why)

	_, err = r.svc.Run(t.Context(), RunRequest{Dir: r.shown(dir), Ref: e.Ref})
	require.ErrorIs(t, err, ErrCantRun)

	_, err = r.svc.Run(t.Context(), RunRequest{Dir: r.shown(dir), Ref: yoursRef(u.ID + 1)})
	require.ErrorIs(t, err, ErrNotFound)
	assert.Equal(t, "The script was deleted.", uierr.Message(err))

	_, err = r.svc.Run(t.Context(), RunRequest{Dir: r.shown(dir), Ref: e.Ref, Args: []string{"x=1"}})
	require.ErrorIs(t, err, ErrCantRun, "greyed comes first")

	r.save(u)
	_, err = r.svc.Run(t.Context(), RunRequest{Dir: r.shown(dir), Ref: e.Ref, Args: []string{"x=1"}})
	require.ErrorIs(t, err, ErrInvalidRequest, "only magex's own targets take arguments")

	r.settings.scripts(integrationprefs.SwitchOff)
	_, err = r.svc.Run(t.Context(), RunRequest{Dir: r.shown(dir), Ref: e.Ref})
	require.ErrorIs(t, err, ErrCantRun, "nothing runs while Scripts is off")
	assert.Zero(t, r.recorded())
}

func TestAUserScriptDeletedAsItStarts(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	dir := r.clone("sdk", "acme-org/sdk", nil)
	u := r.save(fixAll())

	svc := r.service(changeFirst{runs: r.engine, change: func() {
		require.NoError(t, r.svc.Delete(t.Context(), u.ID))
	}})

	_, err := svc.Run(t.Context(), RunRequest{Dir: r.shown(dir), Ref: yoursRef(u.ID)})
	require.ErrorIs(t, err, ErrNotFound, "read again under the clone's lock")
	assert.Zero(t, r.recorded())
}

func TestRepoNames(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	sdk := r.clone("sdk", "acme-org/sdk", map[string]string{".git/config": "[core]\n"})
	r.clone("sdk-2", "Acme-Org/SDK", nil)
	r.clone("web", "acme-org/web", nil)
	r.clone("notes", "", nil)
	r.clone("api", "acme-org/Api", nil)

	got, err := r.svc.RepoNames(t.Context())
	require.NoError(t, err)
	assert.Equal(t, []string{"acme-org/Api", "acme-org/sdk", "acme-org/web"}, got,
		"each repository once, in any case, sorted; a clone not on GitHub has none")

	moved := repo.Name{Owner: "acme-org", Repo: "moved"}
	r.clones.setOrigin(sdk, moved)

	got, err = r.svc.RepoNames(t.Context())
	require.NoError(t, err)
	assert.Contains(t, got, "acme-org/sdk", "kept while .git/config looks the same")

	writeText(t, filepath.Join(sdk, ".git", "config"), "[core]\n\tbare = false\n")

	later := time.Now().Add(time.Minute)
	require.NoError(t, os.Chtimes(filepath.Join(sdk, ".git", "config"), later, later))

	got, err = r.svc.RepoNames(t.Context())
	require.NoError(t, err)
	assert.Equal(t, []string{"acme-org/Api", "acme-org/moved", "Acme-Org/SDK", "acme-org/web"}, got, "read again")

	r.clones.root = filepath.Join(r.home, "missing")
	_, err = r.svc.RepoNames(t.Context())
	require.Error(t, err)
}

func TestOriginOfAnOriginThatCantBeRead(t *testing.T) {
	t.Parallel()

	dir := folder(t, map[string]string{".git/config": "[core]\n"})
	s := NewService(Config{Clones: &failingOrigins{}})

	assert.Equal(t, repo.Name{}, s.originOf(t.Context(), dir))
	assert.Empty(t, s.origins, "read again next time")
	assert.Empty(t, configLook(t.TempDir()), "no .git/config")
}

// failingOrigins is a Clones whose origins can't be read.
type failingOrigins struct {
	fakeClones
}

func (*failingOrigins) Origin(context.Context, string) (repo.Name, bool, error) {
	return repo.Name{}, false, os.ErrPermission
}

func TestYourStoreWithoutADatabase(t *testing.T) {
	t.Parallel()

	s := NewService(Config{Clones: &fakeClones{}})

	all, err := s.yours.all(t.Context())
	require.NoError(t, err)
	assert.Equal(t, []UserScript{}, all)

	_, ok, err := s.yours.get(t.Context(), 1)
	require.NoError(t, err)
	assert.False(t, ok)

	_, err = s.Save(t.Context(), fixAll())
	require.ErrorIs(t, err, ErrNotSetUp)

	u := fixAll()
	u.ID = 1
	_, err = s.Save(t.Context(), u)
	require.ErrorIs(t, err, ErrNotSetUp)

	require.ErrorIs(t, s.Delete(t.Context(), 1), ErrNotSetUp)
	_, err = s.Duplicate(t.Context(), 1)
	require.ErrorIs(t, err, ErrNotSetUp)
}

func TestAStoredScriptThisBuildDoesntKnow(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	dir := r.clone("sdk", "acme-org/sdk", nil)
	u := r.save(fixAll())

	_, err := r.svc.yours.db.ExecContext(t.Context(),
		`UPDATE user_scripts SET icon = 'rocket', runs_in = 'cloud', scope_repos = 'nope', confirm = 1`)
	require.NoError(t, err)

	got, _, err := r.svc.yours.get(t.Context(), u.ID)
	require.NoError(t, err)
	assert.Equal(t, UserScript{
		ID: u.ID, Name: u.Name, Command: u.Command, Icon: IconPlay, Scope: ScopeAll, Repos: []string{},
		RunsIn: "", Confirm: true,
	}, got)

	_, err = r.svc.yours.db.ExecContext(t.Context(), `UPDATE user_scripts SET scope = 'later'`)
	require.NoError(t, err)
	assert.Empty(t, r.yours(dir), "a scope this build doesn't know shows nowhere")
}

func TestYourScriptsThatCantBeRead(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	dir := r.clone("sdk", "acme-org/sdk", nil)
	r.save(fixAll())

	_, err := r.svc.yours.db.ExecContext(t.Context(), `DROP TABLE user_scripts`)
	require.NoError(t, err)

	sec := r.list(dir).Sections[0]
	assert.Equal(t, "Couldn't read your scripts.", sec.Error)
	assert.Empty(t, sec.Entries)

	_, err = r.svc.Run(t.Context(), RunRequest{Dir: r.shown(dir), Ref: yoursRef(1)})
	require.Error(t, err)

	_, _, err = r.svc.yours.get(t.Context(), 1)
	require.Error(t, err)

	err = r.svc.yoursMissing(t.Context(), clone{dir: dir}, "1")
	require.Error(t, err)

	u := fixAll()
	u.ID = 1
	_, err = r.svc.Save(t.Context(), u)
	require.Error(t, err)

	_, err = r.svc.Save(t.Context(), fixAll())
	require.Error(t, err)
}

func TestYourStoreWritesThatFail(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	first := r.save(named(firstName))
	r.save(named("Second"))

	// Every write but the list fails: an insert, a delete, and a move's renumbering.
	for _, trigger := range []string{
		`CREATE TRIGGER no_insert BEFORE INSERT ON user_scripts BEGIN SELECT RAISE(ABORT, 'no'); END`,
		`CREATE TRIGGER no_delete BEFORE DELETE ON user_scripts BEGIN SELECT RAISE(ABORT, 'no'); END`,
		`CREATE TRIGGER no_update BEFORE UPDATE ON user_scripts BEGIN SELECT RAISE(ABORT, 'no'); END`,
	} {
		_, err := r.svc.yours.db.ExecContext(t.Context(), trigger)
		require.NoError(t, err)
	}

	_, err := r.svc.Save(t.Context(), fixAll())
	require.Error(t, err)

	_, err = r.svc.Duplicate(t.Context(), first.ID)
	require.Error(t, err)

	require.Error(t, r.svc.Delete(t.Context(), first.ID))
	require.Error(t, r.svc.Move(t.Context(), first.ID, MoveDown, ""))

	assert.Equal(t, []string{firstName, "Second"}, r.order(), "each change undone whole")
}

func TestWithVars(t *testing.T) {
	t.Parallel()

	assert.Equal(t, []string{"PATH=/bin", "A=new", "B="}, withVars([]string{"A=old", "PATH=/bin", "B=x"},
		[]string{"A=new", "B="}), "each set in place of the value it had")
	assert.Equal(t, []string{"A=1"}, withVars(nil, []string{"A=1"}))
}
