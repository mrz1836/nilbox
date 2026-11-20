package devtools_test

import (
	"context"
	"encoding/json"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/devtools"
)

// window is a fake window: it records the scripts the page runs in it, and answers each
// evaluation as the agent would, from a goroutine of its own, with what answer makes of
// its code: the rest of a result message, as `"ok":true,"value":1`, or "" for none.
type window struct {
	mu     sync.Mutex
	page   *devtools.Page
	answer func(code string) string
	ran    []string
	codes  []string
}

// openWindow attaches a fake window to page.
func openWindow(page *devtools.Page, answer func(code string) string) *window {
	w := &window{page: page, answer: answer}
	page.Attach(w.exec)

	return w
}

// exec runs js: it answers an evaluation, and records anything else.
func (w *window) exec(js string) {
	m := regexp.MustCompile(`window\.__appDevtools\.run\((\d+), ("(?:[^"\\]|\\.)*")\)$`).FindStringSubmatch(js)

	w.mu.Lock()
	defer w.mu.Unlock()

	if m == nil {
		w.ran = append(w.ran, js)

		return
	}

	var code string

	_ = json.Unmarshal([]byte(m[2]), &code)
	w.codes = append(w.codes, code)

	if rest := w.answer(code); rest != "" {
		go w.page.Receive(devtools.Prefix + `{"t":"result","id":` + m[1] + `,` + rest + `}`)
	}
}

// scripts returns what the window ran that was not an evaluation, and the code of each
// evaluation.
func (w *window) scripts() ([]string, []string) {
	w.mu.Lock()
	defer w.mu.Unlock()

	return append([]string{}, w.ran...), append([]string{}, w.codes...)
}

func TestPageEval(t *testing.T) {
	t.Parallel()

	t.Run("runs code in the page, the agent first, and returns its value", func(t *testing.T) {
		t.Parallel()

		page := devtools.NewPage(10, 10, 10, now)

		var got []string

		page.Attach(func(js string) {
			got = append(got, js)

			go page.Receive(devtools.Prefix + `{"t":"result","id":1,"ok":true,"value":{"sum":2}}`)
		})

		value, err := page.Eval(t.Context(), `1 + "1"`)
		require.NoError(t, err)

		assert.JSONEq(t, `{"sum":2}`, string(value))
		require.Len(t, got, 1)
		assert.True(t, strings.HasPrefix(got[0], page.Agent()), "the agent comes first, in case the page lost it")
		assert.True(t, strings.HasSuffix(got[0], `window.__appDevtools.run(1, "1 + \"1\"")`), got[0])
	})

	t.Run("reads no value as null", func(t *testing.T) {
		t.Parallel()

		page := devtools.NewPage(10, 10, 10, now)
		openWindow(page, func(string) string { return `"ok":true` })

		value, err := page.Eval(t.Context(), "undefined")
		require.NoError(t, err)
		assert.Equal(t, "null", string(value))
	})

	t.Run("passes on what the page threw", func(t *testing.T) {
		t.Parallel()

		page := devtools.NewPage(10, 10, 10, now)
		openWindow(page, func(string) string { return `"ok":false,"error":"TypeError: nope"` })

		_, err := page.Eval(t.Context(), "nope()")
		require.ErrorIs(t, err, devtools.ErrScript)
		assert.ErrorContains(t, err, "TypeError: nope")
	})

	t.Run("needs the window", func(t *testing.T) {
		t.Parallel()

		_, err := devtools.NewPage(10, 10, 10, now).Eval(t.Context(), "1")
		require.ErrorIs(t, err, devtools.ErrNoWindow)
	})

	t.Run("gives up on a page that doesn't answer", func(t *testing.T) {
		t.Parallel()

		page := devtools.NewPage(10, 10, 10, now)
		openWindow(page, func(string) string { return "" })

		ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
		defer cancel()

		_, err := page.Eval(ctx, "1")
		require.ErrorIs(t, err, devtools.ErrNoAnswer)
		require.ErrorIs(t, err, context.DeadlineExceeded)

		// An answer that comes too late, or twice, is dropped.
		assert.True(t, page.Receive(devtools.Prefix+`{"t":"result","id":1,"ok":true,"value":1}`))
	})
}

func TestPageReceive(t *testing.T) {
	t.Parallel()

	page := devtools.NewPage(10, 10, 10, now)

	assert.False(t, page.Receive("wails:drag"), "a message that isn't the agent's")
	assert.True(t, page.Receive(devtools.Prefix+"{not json"), "the agent's, though it can't be read")

	page.Receive(devtools.Prefix + `{"t":"console","level":"error","text":"boom"}`)
	page.Receive(devtools.Prefix + `{"t":"trail","kind":"click","target":"button “Snooze”","where":"card 7"}`)
	page.Receive(devtools.Prefix + `{"t":"hello","url":"wails://wails/","title":"inbox","width":1200,"height":800,` +
		`"scale":2,"ua":"WebKit"}`)
	page.Receive(devtools.Prefix + `{"t":"mystery"}`)

	assert.Equal(t, []devtools.ConsoleEntry{{Time: fixedNow, Level: "error", Text: "boom"}}, page.Console().Last(0))
	assert.Equal(t, []devtools.TrailEntry{{Time: fixedNow, Kind: "click", Target: "button “Snooze”", Where: "card 7"}},
		page.Trail().Last(0))
	assert.Equal(t, devtools.PageInfo{
		Loaded: fixedNow, URL: "wails://wails/", Title: "inbox", Width: 1200, Height: 800, Scale: 2, UserAgent: "WebKit",
	}, page.Info())
}

func TestPageRun(t *testing.T) {
	t.Parallel()

	page := devtools.NewPage(10, 10, 10, now)
	require.ErrorIs(t, page.Run("location.reload()"), devtools.ErrNoWindow)

	w := openWindow(page, func(string) string { return "" })
	require.NoError(t, page.Run("location.reload()"))

	ran, _ := w.scripts()
	assert.Equal(t, []string{"location.reload()"}, ran)
}

// noteMessage is a note as the agent posts it, its words text.
func noteMessage(text string) string {
	return devtools.Prefix + `{"t":"note","text":"` + text + `","target":"button “Snooze”","where":"card 7",` +
		`"view":"Inbox","components":"ItemRow › TriageControls","source":"frontend/src/components/TriageControls.tsx:120",` +
		`"nearby":"Fix the cache","element":{"tag":"button","name":"Snooze","rect":{"x":1,"y":2,"width":3,"height":4},` +
		`"marks":{"data-verb":"snooze"},"drawn":true,"children":1},"width":1440,"height":900}`
}

// leaveNote has the agent post a note on page, its words text, and returns it as the page
// keeps it, once it does.
func leaveNote(page *devtools.Page, text string) devtools.Note {
	kept, stop := page.Notes().Follow()
	defer stop()

	page.Receive(noteMessage(text))

	return <-kept
}

func TestPageNotes(t *testing.T) {
	t.Parallel()

	t.Run("keeps a note with a picture of the window, puts it on the trail, and tells the agent", func(t *testing.T) {
		t.Parallel()

		page := devtools.NewPage(10, 10, 10, now)
		w := openWindow(page, func(string) string { return "" })
		page.AttachShot(func(context.Context) ([]byte, error) { return []byte("png"), nil })

		note := leaveNote(page, "Should say for how long")

		assert.Equal(t, devtools.Note{
			N: 1, Time: fixedNow, Text: "Should say for how long", Target: "button “Snooze”", Where: "card 7", View: "Inbox",
			Components: "ItemRow › TriageControls", Source: "frontend/src/components/TriageControls.tsx:120",
			Nearby: "Fix the cache",
			Element: devtools.Element{
				Tag: "button", Name: "Snooze", Rect: devtools.Rect{X: 1, Y: 2, Width: 3, Height: 4},
				Marks: map[string]string{"data-verb": "snooze"},
			},
			ViewportWidth: 1440, ViewportHeight: 900, Shot: []byte("png"),
		}, note)
		assert.Equal(t, []devtools.TrailEntry{{
			Time: fixedNow, Kind: "note", Target: "#1 “Should say for how long” on button “Snooze”", Where: "card 7",
		}}, page.Trail().Last(0))

		ran, _ := w.scripts()
		assert.Equal(t, []string{"window.__appDevtools?.noted(1)"}, ran, "the agent shows itself again")
	})

	t.Run("cuts a long note short on the trail, on one line", func(t *testing.T) {
		t.Parallel()

		page := devtools.NewPage(10, 10, 10, now)
		leaveNote(page, `first line\n`+strings.Repeat("é", 100))

		trail := page.Trail().Last(0)
		require.Len(t, trail, 1)
		assert.Equal(t, "#1 “first line "+strings.Repeat("é", 68)+"…” on button “Snooze”", trail[0].Target)
	})

	t.Run("says why a note has no picture", func(t *testing.T) {
		t.Parallel()

		page := devtools.NewPage(10, 10, 10, now)
		page.AttachShot(func(context.Context) ([]byte, error) { return nil, devtools.ErrNoWindow })

		note := leaveNote(page, "x")
		assert.Nil(t, note.Shot)
		assert.Equal(t, devtools.ErrNoWindow.Error(), note.ShotError)
	})

	t.Run("keeps a note without a picture when it has no way to take one", func(t *testing.T) {
		t.Parallel()

		note := leaveNote(devtools.NewPage(10, 10, 10, now), "x")
		assert.Nil(t, note.Shot)
		assert.Empty(t, note.ShotError)
	})

	t.Run("keeps the notes in the order they came, however long each picture takes", func(t *testing.T) {
		t.Parallel()

		synctest.Test(t, func(t *testing.T) {
			page := devtools.NewPage(10, 10, 10, now)
			release := make(chan struct{})

			var shots atomic.Int32

			page.AttachShot(func(context.Context) ([]byte, error) {
				if shots.Add(1) == 1 {
					<-release
				}

				return []byte("png"), nil
			})

			// The first note's picture is the first taken, and it takes until release.
			page.Receive(noteMessage("first"))
			synctest.Wait()
			page.Receive(noteMessage("second"))
			synctest.Wait()

			assert.Empty(t, page.Notes().Last(0), "the second waits on the first, whose picture is still being taken")

			close(release)
			synctest.Wait()

			notes := page.Notes().Last(0)
			require.Len(t, notes, 2)
			assert.Equal(t, []string{"first", "second"}, []string{notes[0].Text, notes[1].Text})
			assert.Equal(t, []int{1, 2}, []int{notes[0].N, notes[1].N})
		})
	})
}

func TestAgentPostsWhatThePageReceives(t *testing.T) {
	t.Parallel()

	agent := devtools.NewPage(1, 1, 1, now).Agent()

	assert.Contains(t, agent, "const prefix = '"+devtools.Prefix+"'", "the agent and the page agree on the prefix")
	assert.Contains(t, agent, "window.__appDevtools = Object.freeze({ run, nb, noted: reveal })",
		"Eval calls the agent's run, and a kept note its noted")
}
