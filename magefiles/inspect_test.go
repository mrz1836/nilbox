//go:build mage

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/appdata"
	"github.com/mrz1836/nilbox/internal/devtools"
	"github.com/mrz1836/nilbox/internal/testutil"
)

// errNoResolve is what a resolve that must not be called returns.
var errNoResolve = errors.New("resolve was called")

// noResolve is a resolve for targets given data=, which must not look for the user's own.
func noResolve() (appdata.Paths, error) {
	return appdata.Paths{}, errNoResolve
}

// whitePNG is a white picture of a window 20 pixels square, as a Retina screen takes one
// of a viewport 10 CSS pixels wide.
func whitePNG(t *testing.T) []byte {
	t.Helper()

	img := image.NewRGBA(image.Rect(0, 0, 20, 20))
	draw.Draw(img, img.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)

	var buf bytes.Buffer
	require.NoError(t, png.Encode(&buf, img))

	return buf.Bytes()
}

// inspected starts an inspector in a fresh data directory, as a running build from source
// would, its log holding one record, its window a picture to take, and its page one note
// the user left, with a picture of a viewport 10 CSS pixels wide, and returns the
// directory.
func inspected(t *testing.T) string {
	t.Helper()

	dir := testutil.SocketDir(t)
	at := time.Date(2026, 6, 15, 12, 0, 0, 0, time.UTC)

	logs := devtools.NewFeed[devtools.LogEntry](10)
	logs.Add(devtools.LogEntry{
		Time: at, Level: "INFO", Message: "refreshed the inbox", Attrs: []devtools.Attr{{Key: "items", Value: "3"}},
	})

	page := devtools.NewPage(10, 10, 10, time.Now)
	page.AttachShot(func(context.Context) ([]byte, error) { return whitePNG(t), nil })

	kept, stop := page.Notes().Follow()
	page.Receive(devtools.Prefix + `{"t":"note","text":"Should say for how long","target":"button “Snooze”",` +
		`"where":"card 7","element":{"tag":"button","rect":{"x":1,"y":1,"width":3,"height":3}},"width":10,"height":10}`)
	<-kept
	stop()

	server := devtools.NewServer(devtools.Config{
		Logs:   logs,
		Events: devtools.NewFeed[devtools.EventEntry](10),
		Page:   page,
		Shot:   func(context.Context) ([]byte, error) { return []byte("png"), nil },
		About:  func() devtools.About { return devtools.About{Version: "v1", Started: at} },
	})

	ctx, cancel := context.WithCancel(t.Context())

	ln, err := devtools.Listen(ctx, devtools.SocketPath(dir))
	require.NoError(t, err)

	done := make(chan struct{})

	go func() {
		defer close(done)

		_ = server.Serve(ctx, ln)
	}()

	t.Cleanup(func() {
		cancel()
		<-done
	})

	return dir
}

// withArgs gives the rig's targets args, as magex hands them.
func withArgs(t *testing.T, g *rig, args ...string) {
	t.Helper()

	data, err := json.Marshal(args)
	require.NoError(t, err)

	g.setenv(mageArgsJSON, string(data))
}

func TestInspect(t *testing.T) {
	t.Parallel()

	dir := inspected(t)

	t.Run("prints the answer as a terminal shows it", func(t *testing.T) {
		t.Parallel()

		g := newRig(t)
		withArgs(t, g, "data="+dir, "n=5")

		require.NoError(t, g.runner().inspect("logs", noResolve, "n"))

		assert.Contains(t, g.out.String(), "INFO  refreshed the inbox items=3\n")
	})

	t.Run("prints the answer's lines as JSON", func(t *testing.T) {
		t.Parallel()

		g := newRig(t)
		withArgs(t, g, "data="+dir, "json=true")

		require.NoError(t, g.runner().inspect("status", noResolve))

		var line devtools.Line
		require.NoError(t, json.Unmarshal([]byte(g.out.String()), &line))
		assert.Equal(t, devtools.LineResult, line.Type)
		assert.Contains(t, string(line.Value), `"version":"v1"`)
	})

	t.Run("finds the user's own app when data= names none", func(t *testing.T) {
		t.Parallel()

		g := newRig(t)

		require.NoError(t, g.runner().inspect("status", func() (appdata.Paths, error) {
			return appdata.NewPaths(dir), nil
		}))

		assert.Contains(t, g.out.String(), `"version": "v1"`)
	})

	t.Run("saves a picture where out= says", func(t *testing.T) {
		t.Parallel()

		g := newRig(t)
		withArgs(t, g, "data="+dir, "out=shots/window.png")

		require.NoError(t, g.runner().inspect("shot", noResolve))

		assert.Equal(t, "png", readFile(t, g.path("shots/window.png")))
		assert.Contains(t, g.out.String(), "Saved a picture of the window: ")
	})

	t.Run("saves a picture in bin/inspect unless told where", func(t *testing.T) {
		t.Parallel()

		g := newRig(t)
		withArgs(t, g, "data="+dir)

		require.NoError(t, g.runner().inspect("shot", noResolve))

		saved, err := filepath.Glob(g.path(shotsDir + "/shot-*.png"))
		require.NoError(t, err)
		assert.Len(t, saved, 1)
	})

	t.Run("prints each note, and saves its picture with the element boxed", func(t *testing.T) {
		t.Parallel()

		g := newRig(t)
		withArgs(t, g, "data="+dir)

		require.NoError(t, g.runner().inspect("notes", noResolve, "n", "follow", "wait", "clear"))

		saved := g.path(shotsDir + "/note-1.png")
		assert.Contains(t, g.out.String(), " NOTE  #1 “Should say for how long”\n  on         button “Snooze” · card 7\n")
		assert.Contains(t, g.out.String(), "  picture    "+saved+"\n")

		// Two pixels to a CSS pixel: the element spans pixels 2 to 8, and the box the 4
		// around it.
		img, err := png.Decode(strings.NewReader(readFile(t, saved)))
		require.NoError(t, err)

		boxed, plain := color.RGBAModel.Convert(img.At(0, 5)), color.RGBAModel.Convert(img.At(5, 5))
		assert.Equal(t, color.RGBA{R: 0xff, G: 0x2d, B: 0x95, A: 0xff}, boxed)
		assert.Equal(t, color.RGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff}, plain)
	})

	t.Run("prints a note as JSON without its picture, which it saves", func(t *testing.T) {
		t.Parallel()

		g := newRig(t)
		withArgs(t, g, "data="+dir, "json=true")

		require.NoError(t, g.runner().inspect("notes", noResolve, "n"))

		first, rest, _ := strings.Cut(g.out.String(), "\n")

		var line devtools.Line
		require.NoError(t, json.Unmarshal([]byte(first), &line))
		assert.Equal(t, devtools.LineNote, line.Type)
		assert.Contains(t, string(line.Value), `"text":"Should say for how long"`)
		assert.NotContains(t, string(line.Value), `"shot"`)
		assert.Equal(t, "  picture    "+g.path(shotsDir+"/note-1.png")+"\n", rest)
	})

	t.Run("passes on the inspector's refusal", func(t *testing.T) {
		t.Parallel()

		g := newRig(t)
		withArgs(t, g, "data="+dir)

		err := g.runner().inspect("sql", noResolve, "q")

		require.ErrorIs(t, err, devtools.ErrInspector)
		assert.ErrorContains(t, err, "missing argument: q")
	})

	t.Run("says when no app runs on the data", func(t *testing.T) {
		t.Parallel()

		g := newRig(t)
		withArgs(t, g, "data="+testutil.SocketDir(t))

		require.ErrorIs(t, g.runner().inspect("status", noResolve), devtools.ErrNotRunning)
	})

	t.Run("says when it can't find the user's data", func(t *testing.T) {
		t.Parallel()

		require.ErrorIs(t, newRig(t).runner().inspect("status", noResolve), errNoResolve)
	})
}

func TestInspectRequest(t *testing.T) {
	t.Parallel()

	t.Run("carries only the arguments the target reads and was given", func(t *testing.T) {
		t.Parallel()

		g := newRig(t)
		withArgs(t, g, "n=5", "grep=", "data=/x", "level=warn")

		got, err := g.runner().inspectRequest("logs", []string{"n", "level", "grep", "follow"})
		require.NoError(t, err)

		assert.Equal(t, devtools.Request{Command: "logs", Args: map[string]string{"n": "5", "level": "warn"}}, got)
	})

	t.Run("reads a script from file=", func(t *testing.T) {
		t.Parallel()

		g := newRig(t)
		testutil.WriteFile(t, g.path("probe.js"), "return nb.cards()")
		withArgs(t, g, "file=probe.js")

		got, err := g.runner().inspectRequest("js", []string{"code", "timeout"})
		require.NoError(t, err)

		assert.Equal(t, "return nb.cards()", got.Args["code"])
	})

	t.Run("says when the script can't be read", func(t *testing.T) {
		t.Parallel()

		g := newRig(t)
		withArgs(t, g, "file=missing.js")

		_, err := g.runner().inspectRequest("js", []string{"code"})
		require.ErrorIs(t, err, os.ErrNotExist)
	})
}

func TestBuildTagsKeepTheInspectorToBuildsFromSource(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "desktop,devtools", buildTags(""), "a build from source carries the inspector")
	assert.Equal(t, "desktop", buildTags(flavorRelease), "a release never does")
	assert.Equal(t, "desktop", buildTags(flavorRehearsal), "nor does a rehearsal of one")
	assert.Equal(t, []string{"desktop", "desktop,devtools"}, desktopTagSets())
}
