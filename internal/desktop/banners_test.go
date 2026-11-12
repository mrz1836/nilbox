package desktop_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/desktop"
	"github.com/mrz1836/nilbox/internal/event"
	"github.com/mrz1836/nilbox/internal/notify"
)

func TestABannerClickRevealsItsItem(t *testing.T) {
	t.Parallel()

	banner := notify.Banner{ID: "review-7", Title: "Review requested", ItemID: 7, URL: exampleURL}
	want := event.RevealItem{ItemID: 7, URL: exampleURL}

	assert.Equal(t, want, desktop.BannerClicked(desktop.BannerData(banner)), "handed back as written")

	// The system hands the data back through JSON.
	encoded, err := json.Marshal(desktop.BannerData(banner))
	require.NoError(t, err)

	var decoded map[string]any
	require.NoError(t, json.Unmarshal(encoded, &decoded))

	assert.Equal(t, want, desktop.BannerClicked(decoded), "handed back through JSON")
}

func TestAFailedRunsBannerClickOpensTheRun(t *testing.T) {
	t.Parallel()

	banner := notify.Banner{ID: "run-6", Title: "Run failed", RunID: 42, RunStarted: "2026-11-10T16:00:00Z"}

	encoded, err := json.Marshal(desktop.BannerData(banner))
	require.NoError(t, err)

	var decoded map[string]any
	require.NoError(t, json.Unmarshal(encoded, &decoded))

	assert.Equal(t, event.RevealItem{RunID: 42, RunStarted: "2026-11-10T16:00:00Z"}, desktop.BannerClicked(decoded),
		"handed back through JSON, with when the run started")
}

func TestABannerClickReadsWhatItCan(t *testing.T) {
	t.Parallel()

	for name, c := range map[string]struct {
		data map[string]any
		want event.RevealItem
	}{
		"a summary banner": {data: desktop.BannerData(notify.Banner{}), want: event.RevealItem{}},
		"no data":          {data: nil, want: event.RevealItem{}},
		"an int id":        {data: map[string]any{"itemId": 8}, want: event.RevealItem{ItemID: 8}},
		"a text id":        {data: map[string]any{"itemId": "8", "url": exampleURL}, want: event.RevealItem{URL: exampleURL}},
		"a number url":     {data: map[string]any{"itemId": float64(9), "url": 12}, want: event.RevealItem{ItemID: 9}},
		"a run":            {data: map[string]any{"itemId": 0, "url": "", "runId": float64(42)}, want: event.RevealItem{RunID: 42}},
		"a text run id":    {data: map[string]any{"runId": "42"}, want: event.RevealItem{}},
		"a number start":   {data: map[string]any{"runId": 42, "runStarted": 7}, want: event.RevealItem{RunID: 42}},
	} {
		assert.Equal(t, c.want, desktop.BannerClicked(c.data), name)
	}
}
