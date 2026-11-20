package devtools_test

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/devtools"
	"github.com/mrz1836/nilbox/internal/testutil"
)

// syncScript is what the page runs to tell the agent state.
func syncScript(state string) string {
	return "window.__appDevtools?.sync(" + state + ")"
}

func TestInspectorDir(t *testing.T) {
	t.Parallel()

	assert.Equal(t, filepath.Join("data", "devtools"), devtools.InspectorDir("data"))
}

func TestInspectorPointingOutlivesTheApp(t *testing.T) {
	t.Parallel()

	dir := devtools.InspectorDir(t.TempDir())

	page := devtools.NewPage(1, 1, 10, now)
	require.NoError(t, page.Open(dir, nil))
	assert.Equal(t, devtools.InspectorState{Enabled: true}, page.State(), "pointing may start on fresh data")

	require.NoError(t, page.SetPointing(false))
	assert.False(t, page.State().Enabled)

	again := devtools.NewPage(1, 1, 10, now)
	require.NoError(t, again.Open(dir, nil))
	assert.False(t, again.State().Enabled, "the choice is kept")
}

func TestInspectorReadsSettingsItCannotUnderstandAsOn(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	testutil.WriteFile(t, filepath.Join(dir, "settings.json"), "{not json")

	page := devtools.NewPage(1, 1, 1, now)
	require.ErrorContains(t, page.Open(dir, nil), "read the inspector's settings")
	assert.True(t, page.State().Enabled)
}

func TestInspectorTellsTheWindowEachChange(t *testing.T) {
	t.Parallel()

	page := devtools.NewPage(1, 1, 10, now)
	w := openWindow(page, func(string) string { return "" })

	var heard []devtools.InspectorState

	page.OnChange(func(state devtools.InspectorState) { heard = append(heard, state) })

	page.Receive(devtools.Prefix + `{"t":"hello","url":"wails://wails/"}`)
	leaveNote(page, "grey")

	n, err := page.MarkDone(nil, true)
	require.NoError(t, err)
	assert.Equal(t, 1, n)

	n, err = page.ClearNotes()
	require.NoError(t, err)
	assert.Equal(t, 1, n)

	require.NoError(t, page.SetPointing(false))

	ran, _ := w.scripts()
	assert.Equal(t, []string{
		syncScript(`{"enabled":true,"open":0,"kept":0}`),
		"window.__appDevtools?.noted(1)",
		syncScript(`{"enabled":true,"open":1,"kept":1}`),
		syncScript(`{"enabled":true,"open":0,"kept":1}`),
		syncScript(`{"enabled":true,"open":0,"kept":0}`),
		syncScript(`{"enabled":false,"open":0,"kept":0}`),
	}, ran, "as the page loads, then after each change")
	assert.Equal(t, devtools.InspectorState{Enabled: false}, heard[len(heard)-1])
}

func TestInspectorCarriesOutTheUsersChoices(t *testing.T) {
	t.Parallel()

	page := devtools.NewPage(1, 1, 10, now)
	leaveNote(page, "grey")

	page.Receive(devtools.Prefix + `{"t":"inspector","enabled":false}`)
	assert.Equal(t, devtools.InspectorState{Enabled: false, Open: 1, Kept: 1}, page.State())

	page.Receive(devtools.Prefix + `{"t":"inspector","clear":true}`)
	assert.Equal(t, devtools.InspectorState{Enabled: false}, page.State())

	page.Receive(devtools.Prefix + `{"t":"inspector","enabled":true}`)
	assert.True(t, page.State().Enabled)
}

func TestInspectorStartsPointingOnlyWhileTheUserLetsIt(t *testing.T) {
	t.Parallel()

	page := devtools.NewPage(1, 1, 1, now)
	require.ErrorIs(t, page.StartPointing(), devtools.ErrNoWindow)

	w := openWindow(page, func(string) string { return "" })
	require.NoError(t, page.StartPointing())

	require.NoError(t, page.SetPointing(false))
	require.ErrorIs(t, page.StartPointing(), devtools.ErrPointingOff)

	ran, _ := w.scripts()
	assert.Equal(t, "window.__appDevtools?.nb.point(true)", ran[0])
	assert.NotContains(t, ran[1:], "window.__appDevtools?.nb.point(true)")
}
