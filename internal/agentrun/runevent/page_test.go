package runevent_test

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/agentrun/runevent"
)

// nilLists returns where v holds a nil list or map, walking its fields, what its pointers
// point to, and its lists' entries, so a list a page gains later is checked as well.
func nilLists(v reflect.Value, at string) []string {
	var found []string

	walkLists(v, at, func(path string) { found = append(found, path) })

	return found
}

// walkLists hands nilAt where v holds a nil list or map, as nilLists walks it.
func walkLists(v reflect.Value, at string, nilAt func(string)) {
	switch kind := v.Kind(); {
	case kind == reflect.Pointer && !v.IsNil():
		walkLists(v.Elem(), at, nilAt)
	case kind == reflect.Struct:
		for i := range v.NumField() {
			walkLists(v.Field(i), at+"."+v.Type().Field(i).Name, nilAt)
		}
	case (kind == reflect.Slice || kind == reflect.Map) && v.IsNil():
		nilAt(at)
	case kind == reflect.Slice:
		for i := range v.Len() {
			walkLists(v.Index(i), at+"[]", nilAt)
		}
	}
}

func TestNilListsFindsEveryNilList(t *testing.T) {
	t.Parallel()

	assert.Equal(t, []string{"page.Events", "page.Progress.Todos", "page.Ending.Models", "page.Ending.Changed", "page.Ending.Todos"},
		nilLists(reflect.ValueOf(runevent.Page{Ending: &runevent.Ending{}}), "page"),
		"the check sees every list a page holds, so a page that passes it has none nil")
	assert.Equal(t, []string{"m"}, nilLists(reflect.ValueOf(struct{ M map[string]int }{}).Field(0), "m"))
	assert.Empty(t, nilLists(reflect.ValueOf(map[string]int{}), "m"))
}

func TestEmptyPageHasEveryListAndNoEnding(t *testing.T) {
	t.Parallel()

	page := runevent.EmptyPage()

	assert.Equal(t, runevent.Page{Events: []runevent.Event{}, Progress: runevent.Progress{Todos: []runevent.Todo{}}}, page)
	assert.Empty(t, nilLists(reflect.ValueOf(page), "page"))
	assert.Nil(t, page.Ending, "a page with nothing on it hasn't ended")

	body, err := json.Marshal(page)
	require.NoError(t, err)
	assert.Contains(t, string(body), `"events":[]`)
	assert.Contains(t, string(body), `"todos":[]`)
	assert.Contains(t, string(body), `"ending":null`, "the one null is the ending a run has yet to reach")
}

func TestListedFillsEveryNilList(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		page runevent.Page
	}{
		{name: "a page going", page: runevent.Page{}},
		{name: "a page ended", page: runevent.Page{Done: true, Ending: &runevent.Ending{Summary: "Finished: fixed."}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			listed := tc.page.Listed()
			assert.Empty(t, nilLists(reflect.ValueOf(listed), "page"))

			body, err := json.Marshal(listed)
			require.NoError(t, err)

			if tc.page.Ending != nil {
				assert.NotContains(t, string(body), "null", "the window gets [] for every list")
				assert.Equal(t, tc.page.Ending.Summary, listed.Ending.Summary)
			}
		})
	}
}

func TestListedKeepsWhatThePageHolds(t *testing.T) {
	t.Parallel()

	page := runevent.Page{
		Events:   []runevent.Event{{Seq: 1, Kind: runevent.KindText, Text: "Reading the failed run's log"}},
		First:    1,
		Earlier:  true,
		Before:   42,
		Done:     true,
		Progress: runevent.Progress{Step: "$ pnpm test", Todos: []runevent.Todo{{Text: "fix", Status: "done"}}, Events: 1},
		Ending: &runevent.Ending{
			Summary: "Finished: fixed.", Turns: 3, Models: []string{"a-model"}, Commit: "abc123",
			Changed: []string{"main.go"}, Todos: []runevent.Todo{{Text: "fix", Status: "done"}}, Refused: 1,
		},
	}
	want := page
	wantEnding := *page.Ending
	want.Ending = &wantEnding

	assert.Equal(t, want, page.Listed())
}

func TestListedLeavesThePageAndItsEndingAsTheyWere(t *testing.T) {
	t.Parallel()

	ending := &runevent.Ending{Summary: "Out of budget ($0.50)."}
	page := runevent.Page{Ending: ending}

	listed := page.Listed()

	assert.Nil(t, page.Events, "the page passed in keeps its nil lists")
	assert.Nil(t, page.Progress.Todos)
	assert.Same(t, ending, page.Ending)
	assert.Equal(t, runevent.Ending{Summary: "Out of budget ($0.50)."}, *ending, "its Ending is not filled in place")
	assert.Nil(t, ending.Todos)
	assert.Nil(t, ending.Changed)
	assert.Nil(t, ending.Models)

	require.NotNil(t, listed.Ending)
	assert.NotSame(t, ending, listed.Ending, "the page returned has an Ending of its own")

	listed.Ending.Summary = "changed"
	assert.Equal(t, "Out of budget ($0.50).", ending.Summary, "so changing it changes no other page's")
}
