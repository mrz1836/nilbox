package scripts

import (
	"context"
	"maps"
	"os"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/integrationprefs"
	"github.com/mrz1836/nilbox/internal/repo"
	"github.com/mrz1836/nilbox/internal/scriptrun"
	"github.com/mrz1836/nilbox/internal/testutil"
	"github.com/mrz1836/nilbox/internal/uierr"
)

func TestTheFromBranchNoteReadsOriginHead(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	web := r.clone("web", "acme-org/web", nil)
	sdk := r.clone("sdk", "acme-org/sdk", nil)

	r.git.set("trunk")
	r.git.setHead("trunk", nil)
	assert.Empty(t, r.list(web).FromBranch, "on the default branch its origin HEAD names")

	r.git.set("main")
	assert.Equal(t, "main", r.list(web).FromBranch, "main isn't the default when origin HEAD names another")
	assert.Empty(t, r.list(sdk).FromBranch, "the stored default comes first")

	r.git.setHead("", nil)
	assert.Empty(t, r.list(web).FromBranch, "with neither, main or master is the default")

	assert.Equal(t, Variables(), r.list(web).Variables, "the page names the variables for the editor")
}

func TestRunAgainRefusesAScriptThatAsksFirst(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	dir := r.clone("sdk", "acme-org/sdk", nil)
	u := fixAll()
	u.Confirm = true
	u = r.save(u)

	id, err := r.svc.Run(t.Context(), RunRequest{Dir: r.shown(dir), Ref: yoursRef(u.ID)})
	require.NoError(t, err, "the window asked")
	r.ended(id)

	_, err = r.runs.RunAgain(t.Context(), id)
	require.ErrorIs(t, err, ErrAsksFirst)
	assert.Equal(t, `"Fix all" asks before each run, so run it from Repo scripts.`, uierr.Message(err))
	assert.Equal(t, 1, r.recorded(), "nothing ran again")

	u.Confirm = false
	r.save(u)

	again, err := r.runs.RunAgain(t.Context(), id)
	require.NoError(t, err, "once it no longer asks")
	assert.Equal(t, scriptrun.StatusSucceeded, r.ended(again).Status)
}

func TestAUserScriptSavedAgainAsItStarts(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	dir := r.clone("sdk", "acme-org/sdk", nil)
	u := r.save(fixAll())

	for _, change := range []func(*UserScript){
		func(s *UserScript) { s.Command = "rm -rf ~" },
		func(s *UserScript) { s.Name = "Format" },
		func(s *UserScript) { s.RunsIn = integrationprefs.RunsInTerminal },
		func(s *UserScript) { s.Confirm = true },
	} {
		edited := u
		change(&edited)

		svc := r.service(changeFirst{runs: r.engine, change: func() {
			_, err := r.svc.Save(t.Context(), edited)
			require.NoError(t, err)
		}})

		_, err := svc.Run(t.Context(), RunRequest{Dir: r.shown(dir), Ref: yoursRef(u.ID)})
		require.ErrorIs(t, err, ErrChanged, "%+v", edited)
		assert.Equal(t, `"Fix all" changed as it was starting. Look at it again before it runs.`, uierr.Message(err))

		r.save(u)
	}

	assert.Zero(t, r.recorded(), "nothing ran")

	recolored := u
	recolored.Icon = IconBuild

	svc := r.service(changeFirst{runs: r.engine, change: func() {
		_, err := r.svc.Save(t.Context(), recolored)
		require.NoError(t, err)
	}})

	id, err := svc.Run(t.Context(), RunRequest{Dir: r.shown(dir), Ref: yoursRef(u.ID)})
	require.NoError(t, err, "a new icon changes nothing that runs")
	assert.Equal(t, scriptrun.StatusSucceeded, r.ended(id).Status)
}

func TestAUserScriptRunsInTheLoginShellWhateverItIs(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	dir := r.clone("sdk", "acme-org/sdk", map[string]string{t3File: t3With(magexTest)})
	u := named("Greet")
	u.Command = "for f in *.go; echo $f; end"
	u = r.save(u)

	fish := r.fake("fish", testutil.FakeRun{Stdout: "fish ran\n"})
	r.mu.Lock()
	r.shell = fish
	r.mu.Unlock()

	id, err := r.svc.Run(t.Context(), RunRequest{Dir: r.shown(dir), Ref: yoursRef(u.ID)})
	require.NoError(t, err)
	assert.Equal(t, []string{fish, "-c", u.Command}, r.ended(id).Argv, "the user's own line, in their own shell")

	e := r.entry(dir, "t3:Test")
	id, err = r.svc.Run(t.Context(), RunRequest{Dir: r.shown(dir), Ref: e.Ref, Confirmed: e.SHA256})
	require.NoError(t, err)
	assert.Equal(t, []string{chainShell, "-c", magexTest}, r.ended(id).Argv,
		"a repository's command, quoted for POSIX shells, runs in sh")

	argv, err := launcher{shell: "fish"}.command(part{kind: partShell, command: "echo hi", raw: true}, dir)
	require.NoError(t, err)
	assert.Equal(t, []string{chainShell, "-c", "echo hi"}, argv, "a login shell that isn't a path")
}

func TestRepoNamesStopsOnceTheCallerHasGone(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	for _, name := range []string{"a", "b", "c", "d", "e", "f"} {
		r.clone(name, "acme-org/"+name, nil)
	}

	held := &heldOrigins{fakeClones: r.clones, started: make(chan string, 6), release: make(chan struct{})}
	svc := NewService(Config{Clones: held, Home: r.home})

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)

	go func() {
		_, err := svc.RepoNames(ctx)
		done <- err
	}()

	for range originParallel {
		<-held.started
	}

	cancel()
	close(held.release)

	require.ErrorIs(t, <-done, context.Canceled)
	assert.Empty(t, held.started, "no origin read was started once the caller had gone")

	gone, stop := context.WithCancel(t.Context())
	stop()

	_, err := svc.RepoNames(gone)
	require.ErrorIs(t, err, context.Canceled)
	assert.Empty(t, held.started, "none at all for a caller gone already")
}

// heldOrigins is a Clones whose origin reads say they started on started, then wait for
// release.
type heldOrigins struct {
	*fakeClones

	started chan string
	release chan struct{}
}

func (h *heldOrigins) Origin(_ context.Context, dir string) (repo.Name, bool, error) {
	h.started <- dir

	<-h.release

	return repo.Name{}, false, nil
}

func TestRepoNamesForgetsClonesNoLongerListed(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	sdk := r.clone("sdk", "acme-org/sdk", map[string]string{".git/config": "[core]\n"})
	web := r.clone("web", "acme-org/web", map[string]string{".git/config": "[core]\n"})

	_, err := r.svc.RepoNames(t.Context())
	require.NoError(t, err)
	require.Len(t, r.svc.origins, 2)

	require.NoError(t, os.RemoveAll(sdk))

	got, err := r.svc.RepoNames(t.Context())
	require.NoError(t, err)
	assert.Equal(t, []string{"acme-org/web"}, got)
	assert.Equal(t, []string{web}, slices.Collect(maps.Keys(r.svc.origins)), "the removed clone's origin is forgotten")
}
