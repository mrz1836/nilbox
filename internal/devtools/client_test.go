package devtools_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/devtools"
	"github.com/mrz1836/nilbox/internal/store"
)

// line is a line of type typ holding v.
func line(t *testing.T, typ string, v any) devtools.Line {
	t.Helper()

	data, err := json.Marshal(v)
	require.NoError(t, err)

	return devtools.Line{Type: typ, Value: data}
}

// rendered is line as Render writes it.
func rendered(t *testing.T, l devtools.Line) string {
	t.Helper()

	var b strings.Builder
	require.NoError(t, devtools.Render(&b, l))

	return b.String()
}

func TestRender(t *testing.T) {
	t.Parallel()

	clock := fixedNow.Local().Format("15:04:05.000") //nolint:gosmopolitan // Render shows the user's own clock

	for _, c := range []struct {
		name string
		line devtools.Line
		want string
	}{
		{
			name: "a log record, its attributes after it",
			line: line(t, devtools.LineLog, devtools.LogEntry{
				Time: fixedNow, Level: "INFO", Message: "refreshed", Attrs: []devtools.Attr{{Key: "items", Value: "3"}},
			}),
			want: clock + " INFO  refreshed items=3\n",
		},
		{
			name: "an event and what it carried",
			line: line(t, devtools.LineEvent, devtools.EventEntry{Time: fixedNow, Name: "inbox:updated", Data: json.RawMessage(`{"a":1}`)}),
			want: clock + ` EVENT inbox:updated {"a":1}` + "\n",
		},
		{
			name: "a console line",
			line: line(t, devtools.LineConsole, devtools.ConsoleEntry{Time: fixedNow, Level: "error", Text: "boom"}),
			want: clock + " PAGE  ERROR boom\n",
		},
		{
			name: "a step of the trail, and where",
			line: line(t, devtools.LineTrail, devtools.TrailEntry{Time: fixedNow, Kind: "click", Target: "button “Snooze”", Where: "card 7"}),
			want: clock + " USER  click  button “Snooze” · card 7\n",
		},
		{
			name: "a step of the trail nowhere in particular",
			line: line(t, devtools.LineTrail, devtools.TrailEntry{Time: fixedNow, Kind: "key", Target: "⌘K"}),
			want: clock + " USER  key    ⌘K\n",
		},
		{
			name: "a query's rows as a table",
			line: line(t, devtools.LineTable, store.QueryResult{
				Columns: []string{"key", "value"}, Rows: [][]string{{"a", "one\ntwo"}, {"bb", "2"}}, Truncated: true,
			}),
			want: "key  value\na    one⏎two\nbb   2\n(2 rows; more left out (max= shows more))\n",
		},
		{
			name: "a long value, cut short",
			line: line(t, devtools.LineTable, store.QueryResult{Columns: []string{"v"}, Rows: [][]string{{strings.Repeat("é", 200)}}}),
			want: "v\n" + strings.Repeat("é", 160) + "… (200 chars)\n(1 row)\n",
		},
		{
			name: "one row",
			line: line(t, devtools.LineTable, store.QueryResult{Columns: []string{"n"}, Rows: [][]string{{"1"}}}),
			want: "n\n1\n(1 row)\n",
		},
		{name: "text as it is", line: line(t, devtools.LineText, "goroutine 1\n"), want: "goroutine 1\n"},
		{name: "a picture's size", line: line(t, devtools.LineShot, devtools.Shot{PNG: []byte("png")}), want: "A picture of 3 bytes.\n"},
		{name: "a value as indented JSON", line: line(t, devtools.LineResult, map[string]int{"a": 1}), want: "{\n  \"a\": 1\n}\n"},
		{name: "a value that is not JSON as it came", line: devtools.Line{Type: "result", Value: json.RawMessage("nope")}, want: "nope\n"},
	} {
		assert.Equal(t, c.want, rendered(t, c.line), c.name)
	}
}

func TestRenderANote(t *testing.T) {
	t.Parallel()

	clock := fixedNow.Local().Format("15:04:05.000") //nolint:gosmopolitan // Render shows the user's own clock

	full := devtools.Note{
		N: 3, Time: fixedNow, Text: "Should say\nfor how long", Target: "button “Snooze”", Where: "card 7 · in Yours",
		View: "Inbox", Components: "ItemRow › TriageControls", Source: "frontend/src/components/TriageControls.tsx:120",
		Nearby: "Fix the cache",
		Element: devtools.Element{
			Tag: "span", ID: "z", Role: "img", Name: "zz", Classes: "size-3 text-muted",
			Rect: devtools.Rect{X: 10, Y: 20, Width: 30, Height: 40}, Marks: map[string]string{"data-verb": "snooze", "aria-hidden": "true"},
		},
		ViewportWidth: 1440, Shot: []byte("png"),
	}

	assert.Equal(t, clock+" NOTE  #3 “Should say\n  for how long”\n"+
		"  on         button “Snooze” · card 7 · in Yours\n"+
		"  view       Inbox\n"+
		"  components ItemRow › TriageControls\n"+
		"  source     frontend/src/components/TriageControls.tsx:120 (the line approximate)\n"+
		`  element    span #z role=img “zz” at 10,20 30×40 · aria-hidden="true" data-verb="snooze"`+"\n"+
		"  classes    size-3 text-muted\n"+
		"  nearby     “Fix the cache”\n",
		rendered(t, line(t, devtools.LineNote, full)), "the picture is the client's to save")

	bare := devtools.Note{N: 1, Time: fixedNow, Text: "Grey?", Target: "div", ShotError: "the window has not opened yet"}

	assert.Equal(t, clock+" NOTE  #1 “Grey?”\n  on         div\n  picture    none: the window has not opened yet\n",
		rendered(t, line(t, devtools.LineNote, bare)), "what isn't known is left out")
}

func TestRenderRefusesALineItCannotRead(t *testing.T) {
	t.Parallel()

	for _, typ := range []string{
		devtools.LineLog, devtools.LineEvent, devtools.LineConsole, devtools.LineTrail, devtools.LineNote,
		devtools.LineTable, devtools.LineText, devtools.LineShot,
	} {
		require.Error(t, devtools.Render(&strings.Builder{}, devtools.Line{Type: typ, Value: json.RawMessage(`[`)}), typ)
	}
}
