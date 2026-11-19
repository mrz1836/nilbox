package devtools_test

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/devtools"
	"github.com/mrz1836/nilbox/internal/event"
	"github.com/mrz1836/nilbox/internal/testutil"
)

// fixedNow is the time the tests' clocks read.
var fixedNow = time.Date(2026, 6, 15, 12, 0, 0, 0, time.UTC) //nolint:gochecknoglobals // a fixed test time

func now() time.Time { return fixedNow }

func TestTeeKeepsEachEventThenHandsItOn(t *testing.T) {
	t.Parallel()

	feed := devtools.NewFeed[devtools.EventEntry](10)
	next := &testutil.Events{}
	sink := devtools.Tee(next, feed, now)

	sink.Emit(event.RevealItem{ItemID: 7})
	sink.Emit(event.ToolsUpdated{})

	entries := feed.Last(0)
	require.Len(t, entries, 2)
	assert.Equal(t, devtools.EventEntry{Time: fixedNow, Name: event.NameRevealItem, Data: entries[0].Data}, entries[0])
	assert.Contains(t, string(entries[0].Data), `"itemId":7`)
	assert.Equal(t, event.NameToolsUpdated, entries[1].Name)
	assert.Len(t, next.All(), 2, "every event reaches the sink after")
}

func TestTeeKeepsOnlyTheSizeOfALargeEvent(t *testing.T) {
	t.Parallel()

	large := event.RevealItem{URL: strings.Repeat("x", 9000)}
	data, err := json.Marshal(large)
	require.NoError(t, err)

	feed := devtools.NewFeed[devtools.EventEntry](10)
	devtools.Tee(nil, feed, now).Emit(large)

	entries := feed.Last(1)
	require.Len(t, entries, 1)
	assert.JSONEq(t, `{"bytes":`+strconv.Itoa(len(data))+`}`, string(entries[0].Data))
}
