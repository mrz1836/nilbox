package svcstatus

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/mrz1836/nilbox/internal/statusprefs"
)

// FuzzParse checks that no answer a page sends makes a parser panic, that every link a
// reading carries stays on the provider's host, and that judging a reading against every
// part never panics and never hands the window a nil list.
func FuzzParse(f *testing.F) {
	for _, seed := range []string{
		string(fixture(f, "github-operational.json")),
		string(fixture(f, "claude-incident.json")),
		string(fixture(f, "openai-incident.json")),
		claudeDown,
		`{"status": {"indicator": "none"}, "components": [{"id": "a", "group": true, "group_id": "a"}]}`,
		`{"status": {"indicator": "x"}, "incidents": [{"id": "/", "components": [{"group_id": "nope"}]}]}`,
		`{"ongoing_incidents": [{"url": "https://status.openai.com/../../x", "affected_components": [{}]}]}`,
		`{"ongoing_incidents": [], "in_progress_maintenances": [{"url": "//evil.example.com/"}]}`,
		`[]`, `null`, ``, `{`,
	} {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, body string) {
		for _, p := range statusprefs.Catalog() {
			if read, err := parse(p, []byte(body)); err == nil {
				checkReading(t, p, read)
			}
		}
	})
}

// checkReading checks that every link read carries stays on p's host, and that judging it
// against every part hands the window no nil list.
func checkReading(t *testing.T, p statusprefs.Provider, read reading) {
	t.Helper()

	for _, i := range read.incidents {
		assert.Regexp(t, `^https://`+p.Host+`(/|$)`, i.URL, "an incident's link stays on the host")
	}

	every := make([]statusprefs.PartID, 0, len(p.Parts))
	for _, part := range p.Parts {
		every = append(every, part.ID)
	}

	got := judge(p, read, every)
	assert.NotNil(t, got.Affected)
	assert.NotNil(t, got.Incidents)
	assert.NotNil(t, got.Unlisted)

	for _, i := range got.Incidents {
		assert.NotNil(t, i.Parts)
	}
}
