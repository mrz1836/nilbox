package core

import (
	"bytes"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/appdata"
	"github.com/mrz1836/nilbox/internal/appinfo"
	"github.com/mrz1836/nilbox/internal/launchdefaults"
	"github.com/mrz1836/nilbox/internal/prompts"
	"github.com/mrz1836/nilbox/internal/scripts"
	"github.com/mrz1836/nilbox/internal/settings"
	"github.com/mrz1836/nilbox/internal/setup"
	"github.com/mrz1836/nilbox/internal/store"
	"github.com/mrz1836/nilbox/internal/testutil"
)

// askedAt is when the start-over tests ask for a reset.
func askedAt() time.Time { return time.Date(2026, time.November, 15, 9, 0, 0, 0, time.UTC) }

// usedCore starts a Core over opts and fills it as a user would: a prompt of their own, an
// org's launch defaults naming it, a script of their own, a theme, and the setup finished.
// It closes the Core and returns the prompt's nickname.
func usedCore(t *testing.T, opts Options) string {
	t.Helper()

	c := bootstrap(t, opts)
	ctx := t.Context()

	mine, err := c.Prompts.Create(ctx, prompts.Input{
		Name: "Security pass", Nickname: "Security", AppliesTo: prompts.AppliesToAny, Body: "Look at {{.URL}}",
	})
	require.NoError(t, err)

	_, err = c.LaunchDefaults.Save(ctx, launchdefaults.Input{Owner: boundOwner, PRPromptID: &mine.ID})
	require.NoError(t, err)

	_, err = c.Scripts.Save(ctx, scripts.UserScript{
		Name: "Fix all", Command: "magex format:fix", Icon: scripts.IconPlay, Scope: scripts.ScopeAll,
	})
	require.NoError(t, err)

	_, err = c.Settings.SetTheme(ctx, settings.ThemeDark)
	require.NoError(t, err)

	_, err = c.Setup.Finish(ctx)
	require.NoError(t, err)

	require.NoError(t, c.Close())

	return mine.Nickname
}

func TestBootstrapStartsOverAsTheUserAsked(t *testing.T) {
	t.Parallel()

	opts := testOptions(t, ".claude")
	nickname := usedCore(t, opts)

	require.NoError(t, appdata.RequestReset(opts.Paths.ResetRequest, appdata.ResetBySettings, askedAt()))

	var logs bytes.Buffer

	opts.Logger = slog.New(slog.NewTextHandler(&logs, nil))
	c := bootstrap(t, opts)
	ctx := t.Context()

	info := c.AppInfo.Info()
	require.NotNil(t, info.DataReset)
	assert.Equal(t, appinfo.ResetRequested, info.DataReset.Reason)
	assert.Regexp(t, `/data/`+regexp.QuoteMeta(filepath.Base(opts.Paths.DB))+`\.reset-\d{8}T\d{6}Z$`, info.DataReset.Path)

	require.NotNil(t, info.Restored, "the window is told what came back")
	assert.Equal(t, 1, info.Restored.LaunchDefaults)
	assert.Equal(t, 1, info.Restored.Scripts)
	assert.Positive(t, info.Restored.Prompts)

	// What the user made comes back.
	library, err := c.Prompts.List(ctx)
	require.NoError(t, err)
	assert.True(t, hasNickname(library, nickname), "the user's own prompt")

	defaults, err := c.LaunchDefaults.List(ctx)
	require.NoError(t, err)
	require.Len(t, defaults, 1)
	assert.Equal(t, boundOwner, defaults[0].Owner)

	yours, err := scripts.NewBackupPart(c.Scripts).PortableScripts(ctx)
	require.NoError(t, err)
	require.Len(t, yours.Scripts, 1)
	assert.Equal(t, "Fix all", yours.Scripts[0].Name)

	// Everything else starts over.
	got, err := c.Settings.Get(ctx)
	require.NoError(t, err)
	assert.Equal(t, settings.DefaultTheme, got.Theme)

	state, err := c.Setup.State(ctx)
	require.NoError(t, err)
	assert.Equal(t, setup.State{Step: setup.StepWelcome}, state, "the setup opens on a fresh start")

	assert.NoFileExists(t, opts.Paths.ResetRequest, "a reset is carried out once")
	assert.Contains(t, logs.String(), "level=INFO msg=\"started over, as the user asked\"")
	assert.NotContains(t, logs.String(), "level=WARN", "a reset the user asked for is no warning")
}

// hasNickname reports whether library holds a prompt with nickname.
func hasNickname(library []prompts.Prompt, nickname string) bool {
	for _, p := range library {
		if p.Nickname == nickname {
			return true
		}
	}

	return false
}

func TestBootstrapKeepsTheNewestResetCopies(t *testing.T) {
	t.Parallel()

	opts := testOptions(t, ".claude")
	require.NoError(t, os.MkdirAll(opts.Paths.Root, appdata.PrivateDirPerm))

	for _, stamp := range []string{"20261101T000000Z", "20261102T000000Z", "20261103T000000Z"} {
		testutil.WriteFile(t, opts.Paths.DB+".reset-"+stamp, "an old copy")
	}

	usedCore(t, opts)
	require.NoError(t, appdata.RequestReset(opts.Paths.ResetRequest, appdata.ResetByMagex, askedAt()))
	bootstrap(t, opts)

	copies, err := filepath.Glob(opts.Paths.DB + ".reset-*")
	require.NoError(t, err)
	assert.Len(t, copies, store.KeepResetCopies, "the newest three, the one just made among them")
	assert.NotContains(t, copies, opts.Paths.DB+".reset-20261101T000000Z")
}

func TestBootstrapKeepsTheRequestWhenItCannotStartOver(t *testing.T) {
	t.Parallel()

	opts := testOptions(t)
	require.NoError(t, os.MkdirAll(opts.Paths.DB, appdata.PrivateDirPerm), "a folder where the database goes")
	require.NoError(t, appdata.RequestReset(opts.Paths.ResetRequest, appdata.ResetBySettings, askedAt()))

	_, err := Bootstrap(t.Context(), opts)
	require.ErrorContains(t, err, "start over, as you asked")
	assert.FileExists(t, opts.Paths.ResetRequest, "the next start tries again")
}

func TestBootstrapDropsARequestItCannotRead(t *testing.T) {
	t.Parallel()

	opts := testOptions(t, ".claude")
	usedCore(t, opts)
	testutil.WriteFile(t, opts.Paths.ResetRequest, "start over, please")

	var logs bytes.Buffer

	opts.Logger = slog.New(slog.NewTextHandler(&logs, nil))
	c := bootstrap(t, opts)

	assert.Nil(t, c.AppInfo.Info().DataReset, "a request that can't be read is never carried out")
	assert.NoFileExists(t, opts.Paths.ResetRequest)
	assert.Contains(t, logs.String(), "could not read the request to start over")

	got, err := c.Settings.Get(t.Context())
	require.NoError(t, err)
	assert.Equal(t, settings.ThemeDark, got.Theme, "the data is as it was")
}

func TestBootstrapStartsOverOnADataFolderWithNoDatabase(t *testing.T) {
	t.Parallel()

	opts := testOptions(t)
	require.NoError(t, appdata.RequestReset(opts.Paths.ResetRequest, appdata.ResetByMagex, askedAt()))

	c := bootstrap(t, opts)
	assert.Nil(t, c.AppInfo.Info().DataReset, "there was nothing to set aside")
	assert.NoFileExists(t, opts.Paths.ResetRequest)
}

func TestTheSetupShowsOnAFreshDatabaseAlone(t *testing.T) {
	t.Parallel()

	opts := testOptions(t)
	c := bootstrap(t, opts)

	state, err := c.Setup.State(t.Context())
	require.NoError(t, err)
	assert.False(t, state.Done, "a new install shows the setup")

	pending, err := c.ShellSettings.SetupPending(t.Context())
	require.NoError(t, err)
	assert.True(t, pending)

	// An install from before the setup: its inbox signed in, and no setup.* key is stored.
	testutil.Exec(t, c.DB, "DELETE FROM prefs WHERE key LIKE 'setup.%'")
	testutil.Exec(t, c.DB, `INSERT INTO prefs (key, value, updated_at) VALUES ('inbox.viewer', 'octo-user', CURRENT_TIMESTAMP)`)
	require.NoError(t, c.Close())

	upgraded := bootstrap(t, opts)

	pending, err = upgraded.ShellSettings.SetupPending(t.Context())
	require.NoError(t, err)
	assert.False(t, pending, "an existing user never sees the setup")
}

func TestASandboxSaysSo(t *testing.T) {
	t.Parallel()

	opts := testOptions(t)
	assert.False(t, bootstrap(t, opts).AppInfo.Info().Sandbox)

	sandbox := testOptions(t)
	sandbox.Sandbox = true
	assert.True(t, bootstrap(t, sandbox).AppInfo.Info().Sandbox)
}
