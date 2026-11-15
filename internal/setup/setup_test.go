package setup_test

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/appdata"
	"github.com/mrz1836/nilbox/internal/brand"
	"github.com/mrz1836/nilbox/internal/event"
	"github.com/mrz1836/nilbox/internal/github"
	"github.com/mrz1836/nilbox/internal/launcher/terminal"
	"github.com/mrz1836/nilbox/internal/prefs"
	"github.com/mrz1836/nilbox/internal/setup"
	"github.com/mrz1836/nilbox/internal/testutil"
	"github.com/mrz1836/nilbox/internal/uierr"
)

var (
	errFakeDisk    = errors.New("fake: the disk is full")
	errFakeNetwork = errors.New("fake: dial tcp: no route to host")
)

// memory returns the prefs of a fresh database, as core hands the setup.
func memory(t *testing.T) *prefs.Store {
	t.Helper()

	return prefs.New(testutil.DB(t), nil)
}

// viewer is the inbox's account, as Adopt reads it.
type viewer struct {
	login string
	err   error
}

func (v viewer) ViewerLogin(context.Context) (string, error) { return v.login, v.err }

// failingMemory fails every read and write.
type failingMemory struct{}

func (failingMemory) GetMany(context.Context, ...string) (map[string]string, error) {
	return nil, errFakeDisk
}

func (failingMemory) SetMany(context.Context, map[string]string) error { return errFakeDisk }

func TestAdoptDecidesOncePerDatabase(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		login string
		done  bool
	}{
		{name: "an install from before the setup", login: "octo-user", done: true},
		{name: "a fresh database", login: "", done: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			mem := memory(t)

			got, err := setup.Adopt(t.Context(), mem, viewer{login: tt.login})
			require.NoError(t, err)
			assert.Equal(t, setup.State{Done: tt.done, Step: setup.StepWelcome}, got)

			// The inbox signs in, or out, later: the decision stands.
			again, err := setup.Adopt(t.Context(), mem, viewer{login: "someone-else"})
			require.NoError(t, err)
			assert.Equal(t, got, again, "decided once, never again")

			again, err = setup.Adopt(t.Context(), mem, viewer{err: errFakeDisk})
			require.NoError(t, err, "a decided database never asks the inbox again")
			assert.Equal(t, got, again)
		})
	}
}

func TestAdoptStoresNothingItCannotDecide(t *testing.T) {
	t.Parallel()

	mem := memory(t)

	_, err := setup.Adopt(t.Context(), mem, viewer{err: errFakeDisk})
	require.ErrorIs(t, err, errFakeDisk)

	_, err = setup.Adopt(t.Context(), failingMemory{}, viewer{})
	require.ErrorIs(t, err, errFakeDisk)

	got, err := setup.Adopt(t.Context(), mem, viewer{})
	require.NoError(t, err)
	assert.False(t, got.Done, "the next start decides")
}

func TestTheStepsAndTheStateBetweenThem(t *testing.T) {
	t.Parallel()

	mem := memory(t)
	events := &testutil.Events{}
	svc := setup.NewService(setup.Config{Memory: mem, Events: events})

	got, err := svc.State(t.Context())
	require.NoError(t, err)
	assert.Equal(t, setup.State{Done: true, Step: setup.StepWelcome}, got, "undecided reads as done, never trapping a user")

	_, err = setup.Adopt(t.Context(), mem, viewer{})
	require.NoError(t, err)

	got, err = svc.SetStep(t.Context(), setup.StepClones)
	require.NoError(t, err)
	assert.Equal(t, setup.State{Step: setup.StepClones}, got)

	got, err = svc.State(t.Context())
	require.NoError(t, err)
	assert.Equal(t, setup.State{Step: setup.StepClones}, got, "a quit part way through opens there again")

	got, err = svc.Finish(t.Context())
	require.NoError(t, err)
	assert.Equal(t, setup.State{Done: true, Step: setup.StepWelcome}, got)
	assert.Equal(t, []event.Event{event.SetupFinished{}}, events.All(), "the shell asks for banners then")

	got, err = svc.Restart(t.Context())
	require.NoError(t, err)
	assert.Equal(t, setup.State{Step: setup.StepWelcome}, got, "run again from Welcome")
}

func TestSetStepRefusesAStepTheSetupDoesNotHave(t *testing.T) {
	t.Parallel()

	svc := setup.NewService(setup.Config{Memory: memory(t)})

	_, err := svc.SetStep(t.Context(), "dance")
	require.ErrorIs(t, err, setup.ErrUnknownStep)
	assert.Equal(t, "That step isn't part of the setup.", uierr.Message(err))

	for _, step := range setup.Steps() {
		got, setErr := svc.SetStep(t.Context(), step)
		require.NoError(t, setErr)
		assert.Equal(t, step, got.Step)
	}
}

func TestTheStateReportsAMemoryItCannotUse(t *testing.T) {
	t.Parallel()

	events := &testutil.Events{}
	svc := setup.NewService(setup.Config{Memory: failingMemory{}, Events: events})

	_, err := svc.State(t.Context())
	require.ErrorIs(t, err, errFakeDisk)

	_, err = svc.SetStep(t.Context(), setup.StepGitHub)
	require.ErrorIs(t, err, errFakeDisk)

	_, err = svc.Finish(t.Context())
	require.ErrorIs(t, err, errFakeDisk)
	assert.Zero(t, events.Count(), "a finish that wasn't stored tells nothing")

	_, err = svc.Restart(t.Context())
	require.ErrorIs(t, err, errFakeDisk)
}

// account lends a fixed sign-in, or fails.
type account struct {
	in  github.SignIn
	err error
}

func (a account) SignIn(context.Context) (github.SignIn, error) { return a.in, a.err }

// cli is the GitHub CLI's version.
type cli struct {
	version   string
	installed bool
}

func (c cli) GHVersion(context.Context) (string, bool) { return c.version, c.installed }

func TestGitHubTellsEveryStateApart(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   github.SignIn
		err  error
		want setup.GitHub
	}{
		{
			name: "ready",
			in:   github.SignIn{Login: "octo-user", Name: "Octo User", Scopes: "repo, read:org, read:project, repo"},
			want: setup.GitHub{
				State: setup.GitHubReady, Login: "octo-user", Name: "Octo User",
				Scopes: []string{"repo", "read:org", "read:project"}, Missing: []string{}, Projects: setup.ProjectsGranted,
				CLIVersion: "2.83.0",
			},
		},
		{
			name: "a fine-grained token",
			in:   github.SignIn{Login: "octo-user"},
			want: setup.GitHub{
				State: setup.GitHubReady, Login: "octo-user", Scopes: []string{}, Missing: []string{}, Unverified: true,
				Projects: setup.ProjectsUnverified, CLIVersion: "2.83.0",
			},
		},
		{
			name: "missing scopes",
			in:   github.SignIn{Login: "octo-user", Scopes: "gist, workflow"},
			want: setup.GitHub{
				State: setup.GitHubMissingScopes, Login: "octo-user", Scopes: []string{"gist", "workflow"},
				Missing: []string{"repo", "read:org"}, Projects: setup.ProjectsMissing, CLIVersion: "2.83.0",
				Fix: setup.FixScopes,
				Message: "The GitHub CLI's token is missing the repo and read:org scope, which the inbox needs to see " +
					"private and organization repositories.",
			},
		},
		{
			name: "no GitHub CLI",
			err:  uierr.New("The GitHub CLI (gh) was not found.", github.ErrGHNotFound),
			want: setup.GitHub{
				State: setup.GitHubNoCLI, Scopes: []string{}, Missing: []string{}, Projects: setup.ProjectsUnverified,
				CLIVersion: "2.83.0", Fix: setup.FixInstall, Message: "The GitHub CLI (gh) was not found.",
			},
		},
		{
			name: "signed out",
			err:  uierr.New("The GitHub CLI is not signed in to github.com.", github.ErrNotLoggedIn),
			want: setup.GitHub{
				State: setup.GitHubSignedOut, Scopes: []string{}, Missing: []string{}, Projects: setup.ProjectsUnverified,
				CLIVersion: "2.83.0", Fix: setup.FixSignIn, Message: "The GitHub CLI is not signed in to github.com.",
			},
		},
		{
			name: "a refused token",
			err:  &github.StatusError{StatusCode: http.StatusUnauthorized, Message: "Bad credentials"},
			want: setup.GitHub{
				State: setup.GitHubRefused, Scopes: []string{}, Missing: []string{}, Projects: setup.ProjectsUnverified,
				CLIVersion: "2.83.0", Fix: setup.FixSignIn,
				Message: "GitHub refused the GitHub CLI's token: it was revoked or has expired. Sign in again.",
			},
		},
		{
			name: "GitHub unreachable",
			err:  uierr.New("Couldn't reach GitHub.", errFakeNetwork),
			want: setup.GitHub{
				State: setup.GitHubUnreachable, Scopes: []string{}, Missing: []string{}, Projects: setup.ProjectsUnverified,
				CLIVersion: "2.83.0", Message: "Couldn't reach GitHub.",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			svc := setup.NewService(setup.Config{
				Memory: memory(t), Account: account{in: tt.in, err: tt.err}, CLI: cli{version: "2.83.0", installed: true},
			})

			got, err := svc.GitHub(t.Context())
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestGitHubWithNoAccountReadsAsNoCLI(t *testing.T) {
	t.Parallel()

	got, err := setup.NewService(setup.Config{Memory: memory(t), CLI: cli{}}).GitHub(t.Context())
	require.NoError(t, err)
	assert.Equal(t, setup.GitHubNoCLI, got.State)
	assert.Empty(t, got.CLIVersion, "no version for a CLI that isn't installed")
}

func TestGitHubReportsACheckTheWindowGaveUpOn(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	_, err := setup.NewService(setup.Config{Memory: memory(t), Account: account{err: context.Canceled}}).GitHub(ctx)
	require.ErrorIs(t, err, context.Canceled)
}

func TestTerminals(t *testing.T) {
	t.Parallel()

	svc := setup.NewService(setup.Config{Memory: memory(t), Terminals: func() []terminal.Choice {
		return []terminal.Choice{
			{ID: terminal.ITerm2, Name: "iTerm2"},
			{ID: terminal.TerminalApp, Name: "Terminal", Installed: true},
		}
	}})

	assert.Equal(t, []setup.TerminalChoice{
		{ID: terminal.ITerm2, Name: "iTerm2"},
		{ID: terminal.TerminalApp, Name: "Terminal", Installed: true},
	}, svc.Terminals(t.Context()))

	none := setup.NewService(setup.Config{Memory: memory(t)}).Terminals(t.Context())
	assert.NotNil(t, none)
	assert.Empty(t, none)
}

// clones lists fixed clone folders, or fails.
type clones struct {
	dirs []string
	err  error
}

func (c clones) Clones(context.Context) ([]string, error) { return c.dirs, c.err }

func TestSignalsCountTheClonesThatHoldEachSign(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	clone := func(name string, files ...string) string {
		dir := filepath.Join(root, name)
		require.NoError(t, os.MkdirAll(dir, 0o700))

		for _, f := range files {
			path := filepath.Join(dir, f)
			require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
			testutil.WriteFile(t, path, "x")
		}

		return dir
	}

	dirs := []string{
		clone("fortress", "go.mod", ".github/env/10-mage-x.env", ".github/env/10-pre-commit.env",
			".github/workflows/fortress.yml"),
		clone("older", "go.mod", ".github/.env.base", ".mage.yaml"),
		clone("mage", "go.mod", "magefiles/magefile.go"),
		clone("go-only", "go.mod"),
		clone("web", "package.json"),
		filepath.Join(root, "gone"),
	}

	got, err := setup.NewService(setup.Config{Memory: memory(t), Clones: clones{dirs: dirs}}).Signals(t.Context())
	require.NoError(t, err)
	assert.Equal(t, setup.Signals{Clones: 6, GoModules: 4, MageX: 3, GoPreCommit: 2, Fortress: 1}, got)
}

func TestSignalsReportAListingThatFailed(t *testing.T) {
	t.Parallel()

	_, err := setup.NewService(setup.Config{Memory: memory(t), Clones: clones{err: errFakeDisk}}).Signals(t.Context())
	require.ErrorIs(t, err, errFakeDisk)

	got, err := setup.NewService(setup.Config{Memory: memory(t)}).Signals(t.Context())
	require.NoError(t, err)
	assert.Zero(t, got, "no clones, no signs")
}

// runs says whether runs are going.
type runs bool

func (r runs) Active() bool { return bool(r) }

// savedBeforeReset is where the fake saver says it saved the setup.
const savedBeforeReset = "~/data/backups/" + brand.Slug + " before reset 20261115T120000Z.json"

// saver saves the setup before a reset, or fails.
type saver struct {
	saved *int
	err   error
}

func (s saver) SaveBeforeReset(context.Context) (string, error) {
	if s.err != nil {
		return "", s.err
	}

	*s.saved++

	return savedBeforeReset, nil
}

// resetRig is a Service ready to reset, and what it reaches.
type resetRig struct {
	svc     *setup.Service
	request string
	saved   *int
	events  *testutil.Events
}

func newResetRig(t *testing.T, change ...func(*setup.Config)) resetRig {
	t.Helper()

	r := resetRig{
		request: appdata.NewPaths(t.TempDir()).ResetRequest, saved: new(int), events: &testutil.Events{},
	}

	cfg := setup.Config{
		Memory: memory(t), Runs: runs(false), Saver: saver{saved: r.saved}, ResetRequest: r.request,
		Clock: testutil.NewClock(time.Date(2026, time.November, 15, 12, 0, 0, 0, time.UTC)), Events: r.events,
	}

	for _, c := range change {
		c(&cfg)
	}

	r.svc = setup.NewService(cfg)

	return r
}

func TestResetSavesTheSetupThenAsksForTheReset(t *testing.T) {
	t.Parallel()

	r := newResetRig(t)

	got, err := r.svc.Reset(t.Context(), " reset ")
	require.NoError(t, err)
	assert.Equal(t, setup.ResetStarted{SavedTo: savedBeforeReset}, got)
	assert.Equal(t, 1, *r.saved)

	req, found, err := appdata.ReadResetRequest(r.request)
	require.NoError(t, err)
	assert.True(t, found)
	assert.Equal(t, appdata.ResetBySettings, req.By)
	assert.Equal(t, time.Date(2026, time.November, 15, 12, 0, 0, 0, time.UTC), req.RequestedAt)

	assert.Equal(t, []event.Event{event.ResetRequested{}}, r.events.All(), "the shell quits and opens the app again")
}

func TestResetRefuses(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		confirm string
		change  func(*setup.Config)
		is      error
		want    string
	}{
		{name: "no confirmation", confirm: "", is: setup.ErrNotConfirmed, want: "Type reset to confirm."},
		{name: "the wrong word", confirm: "Reset", is: setup.ErrNotConfirmed, want: "Type reset to confirm."},
		{
			name: "runs going", confirm: setup.ResetWord, change: func(c *setup.Config) { c.Runs = runs(true) },
			is: setup.ErrRunsGoing, want: "Runs are going in the app. Wait for them to end, or stop them, then reset.",
		},
		{
			name: "a backup that fails", confirm: setup.ResetWord,
			change: func(c *setup.Config) { c.Saver = saver{err: errFakeDisk} },
			is:     errFakeDisk, want: "Couldn't save a backup of your setup first, so nothing was reset.",
		},
		{
			name: "nowhere to ask", confirm: setup.ResetWord, change: func(c *setup.Config) { c.ResetRequest = "" },
			is: setup.ErrNoResetRequest, want: "The reset couldn't be asked for, so nothing changed.",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var changes []func(*setup.Config)
			if tt.change != nil {
				changes = append(changes, tt.change)
			}

			r := newResetRig(t, changes...)

			_, err := r.svc.Reset(t.Context(), tt.confirm)
			require.ErrorIs(t, err, tt.is)
			assert.Equal(t, tt.want, uierr.Message(err))
			assert.NoFileExists(t, r.request, "nothing is asked for")
			assert.Zero(t, r.events.Count(), "the app keeps running")
		})
	}
}

func TestResetReportsARequestItCannotWrite(t *testing.T) {
	t.Parallel()

	blocked := filepath.Join(t.TempDir(), "blocked")
	testutil.WriteFile(t, blocked, "a file where the data folder goes")

	r := newResetRig(t, func(c *setup.Config) { c.ResetRequest = filepath.Join(blocked, "reset-request.json") })

	_, err := r.svc.Reset(t.Context(), setup.ResetWord)
	require.Error(t, err)
	assert.Equal(t, "The reset couldn't be asked for, so nothing changed.", uierr.Message(err))
	assert.Zero(t, r.events.Count())
}
